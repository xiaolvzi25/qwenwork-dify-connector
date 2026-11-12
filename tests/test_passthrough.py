"""Black-box regression tests for the native QwenWork compatibility adapter.

Only the test harness uses Python. The runtime package does not require Python.
Run: python3 tests/test_passthrough.py [path/to/native/dify-qwen]
"""
import json
import os
from pathlib import Path
import re
import selectors
import shutil
import signal
import subprocess
import sys
import tempfile
import time
import unittest
import urllib.error
import urllib.parse
import urllib.request


DEFAULT_BINARY = Path(__file__).resolve().parents[1] / "package/bin/darwin_arm64/dify-qwen"
DEFAULT_LAUNCHER = Path(__file__).resolve().parents[1] / "package/bin/dify-qwen"
BINARY = Path(sys.argv.pop(1)).resolve() if len(sys.argv) > 1 and not sys.argv[1].startswith("-") else DEFAULT_BINARY

MOCK = r'''import json, os, signal, sys, time
mode = os.environ.get("MOCK_MODE", "inspect")
args = sys.argv[1:]
if os.environ.get("MOCK_LOG"):
    with open(os.environ["MOCK_LOG"], "a") as log:
        log.write(json.dumps(args) + "\n")
if mode == "inspect":
    print(json.dumps({"args": args, "env": {k:v for k,v in os.environ.items() if k.startswith("DIFY_")}}, ensure_ascii=False))
elif mode == "raw":
    sys.stdout.buffer.write(b'\x00raw\xff\n{"event":1}\n{"event":2}\n')
    sys.stdout.flush()
    sys.stderr.buffer.write(b'error\xff\n')
    sys.exit(23)
elif mode == "stdin":
    sys.stdout.buffer.write(sys.stdin.buffer.read())
elif mode == "stream":
    print('{"event":"first"}', flush=True)
    time.sleep(2)
    print('{"event":"last"}', flush=True)
elif mode == "signal":
    print(os.getpid(), flush=True)
    time.sleep(30)
elif mode == "account":
    print(os.environ["MOCK_ACCOUNT"])
    sys.exit(int(os.environ.get("MOCK_EXIT", "0")))
elif mode == "portal":
    if "--no-wait" in args:
        print(json.dumps({"verification_uri": os.environ.get("MOCK_VERIFY_URI", "https://dify.example.invalid/activate"), "user_code":"TEST-CODE", "expires_in":30, "interval":5}))
    elif "--resume" in args:
        print('{"status":"pending"}')
    else:
        print('{"id":"mock-user","name":"Mock test"}')
else:
    sys.exit(91)
'''


class NativeAdapterTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if not BINARY.is_file():
            raise RuntimeError("Build the native adapter before testing: " + str(BINARY))
        if not DEFAULT_LAUNCHER.is_file():
            raise RuntimeError("Build the native top-level launcher before testing: " + str(DEFAULT_LAUNCHER))

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="qwen-dify-test-")
        self.root = Path(self.temp.name)
        self.package = self.root / "package"
        self.entry = self.package / "bin/darwin_arm64/dify-qwen"
        self.entry.parent.mkdir(parents=True)
        shutil.copy2(BINARY, self.entry)
        self.entry.chmod(0o755)
        self.launcher = self.package / "bin/dify-qwen"
        shutil.copy2(DEFAULT_LAUNCHER, self.launcher)
        self.launcher.chmod(0o755)
        self.vendor = self.package / "vendor/darwin_arm64/difyctl"
        self.vendor.parent.mkdir(parents=True)
        self.vendor.write_text("#!" + sys.executable + "\n" + MOCK)
        self.vendor.chmod(0o755)
        self.profile = self.root / "connector-profile"
        self.decoy = self.root / "unrelated-dify-profile"
        self.decoy.mkdir()
        (self.decoy / "sentinel").write_text("DO NOT CHANGE")
        self.env = os.environ.copy()
        self.env.update({
            "QWEN_DIFY_CONFIG_DIR": str(self.profile),
            "DIFY_CONFIG_DIR": str(self.decoy),
            "DIFY_TOKEN": "DO_NOT_INHERIT_TOKEN",
            "DIFY_SERVER": "https://wrong.invalid",
            "DIFY_API_KEY": "DO_NOT_INHERIT_KEY",
            "MOCK_MODE": "inspect",
        })
        self.env.pop("QWENWORK_SANDBOX_ID", None)

    def tearDown(self):
        self.temp.cleanup()

    def run_adapter(self, args, *, mode=None, account=None, exit_code=0, input_data=None, entry=None):
        env = self.env.copy()
        if mode:
            env["MOCK_MODE"] = mode
        if account is not None:
            env.update(MOCK_MODE="account", MOCK_ACCOUNT=json.dumps(account), MOCK_EXIT=str(exit_code))
        return subprocess.run([str(entry or self.entry), *args], env=env, input=input_data,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=10)

    def test_connector_version(self):
        p = self.run_adapter(["--version"])
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(p.stdout.strip(), b"1.0.0")

    def test_direct_and_legacy_cli_arguments_are_unchanged(self):
        args = ["example.with.dot", "--argument", "合同 a'b $(echo x)\n", "--json"]
        for prefix in ([], ["cli"]):
            with self.subTest(prefix=prefix):
                p = self.run_adapter(prefix + args)
                self.assertEqual(p.returncode, 0, p.stderr)
                self.assertEqual(json.loads(p.stdout)["args"], args)

    def test_stdout_stderr_and_exit_code_are_byte_exact(self):
        p = self.run_adapter(["cli", "run", "app", "--stream"], mode="raw")
        self.assertEqual(p.returncode, 23)
        self.assertEqual(p.stdout, b'\x00raw\xff\n{"event":1}\n{"event":2}\n')
        self.assertEqual(p.stderr, b"error\xff\n")

    def test_stdin_is_forwarded_byte_exact(self):
        payload = b'\x00input\xff\n{"a":"value"}\n'
        p = self.run_adapter(["run", "example", "--input", "@-"], mode="stdin", input_data=payload)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(p.stdout, payload)

    def test_stream_emits_before_upstream_exits(self):
        env = dict(self.env, MOCK_MODE="stream")
        with subprocess.Popen([str(self.entry), "run", "example", "--stream"], env=env,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE) as p:
            sel = selectors.DefaultSelector()
            try:
                sel.register(p.stdout, selectors.EVENT_READ)
                self.assertTrue(sel.select(timeout=1.5), "First event was buffered until process exit")
                self.assertEqual(p.stdout.readline(), b'{"event":"first"}\n')
                self.assertIsNone(p.poll(), "Stream must arrive while official CLI is still running")
                out, err = p.communicate(timeout=5)
                self.assertEqual(out, b'{"event":"last"}\n')
                self.assertEqual(p.returncode, 0, err)
            finally:
                sel.close()
                if p.poll() is None:
                    p.kill()
                    p.communicate()

    def test_process_is_replaced_and_sigterm_reaches_upstream(self):
        self.assert_exec_pid_and_signal(self.entry)

    def assert_exec_pid_and_signal(self, entry):
        env = dict(self.env, MOCK_MODE="signal")
        with subprocess.Popen([str(entry), "get", "workspace"], env=env,
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE) as p:
            sel = selectors.DefaultSelector()
            try:
                sel.register(p.stdout, selectors.EVENT_READ)
                self.assertTrue(sel.select(timeout=3), "Official CLI did not start")
                self.assertEqual(int(p.stdout.readline()), p.pid, "Wrapper must exec, not spawn/capture the CLI")
                p.send_signal(signal.SIGTERM)
                p.communicate(timeout=3)
                self.assertEqual(p.returncode, -signal.SIGTERM)
            finally:
                sel.close()
                if p.poll() is None:
                    p.kill()
                    p.communicate()

    def test_top_level_entry_preserves_arguments_and_binary_io(self):
        args = ["example.with.dot", "--input", "合同 a'b $(echo x)\n", "--json"]
        p = self.run_adapter(args, entry=self.launcher)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(json.loads(p.stdout)["args"], args)
        p = self.run_adapter(["--version"], entry=self.launcher)
        self.assertEqual(p.stdout.strip(), b"1.0.0")
        p = self.run_adapter(["run", "example", "--stream"], mode="raw", entry=self.launcher)
        self.assertEqual(p.returncode, 23)
        self.assertEqual(p.stdout, b'\x00raw\xff\n{"event":1}\n{"event":2}\n')
        self.assertEqual(p.stderr, b"error\xff\n")
        payload = b"\x00stdin\xff\n"
        p = self.run_adapter(["run", "example", "--input", "@-"], mode="stdin", input_data=payload, entry=self.launcher)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(p.stdout, payload)

    def test_top_level_entry_exec_signal_and_safe_missing_target(self):
        self.assert_exec_pid_and_signal(self.launcher)
        self.entry.unlink()
        p = self.run_adapter(["--version"], entry=self.launcher)
        self.assertNotEqual(p.returncode, 0)
        self.assertEqual(p.stdout, b"")
        self.assertEqual(json.loads(p.stderr)["code"], "E-0004")
        self.assertNotIn(str(self.root).encode(), p.stderr)

    def test_profile_isolation_and_parent_dify_environment_removed(self):
        p = self.run_adapter(["get", "workspace", "--json"])
        self.assertEqual(p.returncode, 0, p.stderr)
        env = json.loads(p.stdout)["env"]
        self.assertEqual(env.get("DIFY_CONFIG_DIR"), str(self.profile / "difyctl"))
        self.assertNotIn("DIFY_TOKEN", env)
        self.assertNotIn("DIFY_SERVER", env)
        self.assertNotIn("DIFY_API_KEY", env)
        self.assertEqual((self.decoy / "sentinel").read_text(), "DO NOT CHANGE")
        self.assertEqual(list(self.decoy.iterdir()), [self.decoy / "sentinel"])

    def test_not_logged_in_is_strict_unauthorized_json_with_exit_zero(self):
        for error in ({"code": "not_logged_in"}, {"http_status": 401}):
            with self.subTest(error=error):
                p = self.run_adapter(["auth", "show", "--status"], account={"error": error}, exit_code=4)
                self.assertEqual(p.returncode, 0, p.stderr)
                self.assertEqual(json.loads(p.stdout), {"status": "unauthorized"})

    def test_forbidden_and_network_errors_are_not_logged_out(self):
        errors = [({"http_status": 403, "code": "forbidden"}, 4),
                  ({"code": "connection_error", "message": "PRIVATE_DEBUG_TOKEN"}, 1)]
        for error, code in errors:
            with self.subTest(error=error):
                p = self.run_adapter(["auth", "show", "--status"], account={"error": error}, exit_code=code)
                self.assertNotEqual(p.returncode, 0)
                self.assertNotIn(b'"status":"authorized"', p.stdout.replace(b" ", b""))
                self.assertNotIn(b'"status":"unauthorized"', p.stdout.replace(b" ", b""))
                self.assertNotIn(b"PRIVATE_DEBUG_TOKEN", p.stdout + p.stderr)

    def test_unknown_identity_cannot_claim_authorized(self):
        p = self.run_adapter(["auth", "show", "--status"], account={"unexpected": True})
        self.assertNotEqual(p.returncode, 0)
        self.assertNotIn(b"authorized", p.stdout)

    def test_safe_identity_whitelist_and_account_wrappers(self):
        identity = {"id": "user-123", "name": "财务测试员", "email": "test@example.invalid", "status": "active"}
        unsafe = dict(identity, access_token="SECRET_ACCESS", refresh_token="SECRET_REFRESH", nested={"token": "SECRET_NESTED"})
        for response in (unsafe, {"data": unsafe}, {"account": unsafe}, {"data": {"account": unsafe}}):
            with self.subTest(response_shape=list(response)):
                p = self.run_adapter(["identity", "whoami"], account=response)
                self.assertEqual(p.returncode, 0, p.stderr)
                actual = json.loads(p.stdout)
                self.assertEqual({k: actual[k] for k in ("id", "name", "email")}, {k: identity[k] for k in ("id", "name", "email")})
                self.assertTrue(set(actual).issubset({"id", "name", "email", "status"}))
                self.assertNotIn(b"SECRET_", p.stdout + p.stderr)
        p = self.run_adapter(["auth", "show", "--status"], account=unsafe)
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(json.loads(p.stdout), {"status": "authorized"})

    def test_incomplete_auth_blocks_stale_identity_and_business(self):
        self.profile.mkdir(parents=True, exist_ok=True)
        (self.profile / "authorization-required").write_text("")
        p = self.run_adapter(["auth", "show", "--status"], account={"id": "stale-user"})
        self.assertEqual(p.returncode, 0, p.stderr)
        self.assertEqual(json.loads(p.stdout), {"status": "unauthorized"})
        p = self.run_adapter(["get", "workspace"])
        self.assertNotEqual(p.returncode, 0)
        self.assertNotIn(b'"args"', p.stdout, "Business CLI must not run under an unfinished authorization")

    def portal_request(self, url, *, data=None, headers=None):
        opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
        request = urllib.request.Request(url, data=data, headers=headers or {})
        return opener.open(request, timeout=3)

    def start_portal(self, *, verification_uri=None):
        env = dict(self.env, MOCK_MODE="portal", MOCK_LOG=str(self.root / "mock-calls.jsonl"))
        if verification_uri:
            env["MOCK_VERIFY_URI"] = verification_uri
        p = subprocess.Popen([str(self.entry), "auth", "init", "--no-open"], env=env,
                             stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        self.addCleanup(lambda: self.stop_process(p))
        sel = selectors.DefaultSelector()
        try:
            sel.register(p.stdout, selectors.EVENT_READ)
            self.assertTrue(sel.select(timeout=5), "Portal did not announce the local URL")
            line = p.stdout.readline()
            if not line:
                self.fail("Portal exited before announcing URL: " + p.stderr.read().decode(errors="replace"))
            record = json.loads(line)
        finally:
            sel.close()
        base = record["local_setup_url"]
        self.assertEqual(urllib.parse.urlsplit(base).hostname, "127.0.0.1")
        page = self.portal_request(base).read().decode()
        csrf = re.search(r"\bcsrf\s*=\s*(['\"])([^'\"]+)\1", page)
        self.assertIsNotNone(csrf, "Portal must expose a CSRF token to its own script")
        origin = urllib.parse.urlunsplit((*urllib.parse.urlsplit(base)[:2], "", "", ""))
        headers = {"Origin": origin, "X-CSRF": csrf.group(2), "Content-Type": "application/json"}
        return p, base, page, headers

    @staticmethod
    def stop_process(p):
        if p.poll() is None:
            p.kill()
        p.communicate(timeout=3)

    def wait_stage(self, base, wanted):
        deadline = time.monotonic() + 5
        state = {}
        while time.monotonic() < deadline:
            state = json.load(self.portal_request(base + "/state"))
            if state.get("stage") in wanted:
                return state
            time.sleep(0.05)
        self.fail("Expected portal stage " + repr(wanted) + ", got " + repr(state))

    def test_portal_rejects_host_origin_csrf_and_duplicate_start(self):
        p, base, page, headers = self.start_portal()
        state = json.load(self.portal_request(base + "/state"))
        self.assertEqual(state.get("server", ""), "", "Do not ship a private default Dify environment")
        self.assertNotIn("console-platform-dev.dify.dev", page)
        with self.assertRaises(urllib.error.HTTPError) as e:
            self.portal_request(base + "/state", headers={"Host": "attacker.invalid"})
        self.assertEqual(e.exception.code, 403)
        body = json.dumps({"server": "https://dify.example.invalid"}).encode()
        for bad_headers in ({}, {"Origin": headers["Origin"]}, {**headers, "Origin": "https://attacker.invalid"}):
            with self.subTest(headers=bad_headers):
                with self.assertRaises(urllib.error.HTTPError) as e:
                    self.portal_request(base + "/start", data=body, headers=bad_headers)
                self.assertEqual(e.exception.code, 403)
        self.portal_request(base + "/start", data=body, headers=headers).close()
        state = self.wait_stage(base, {"pending", "error"})
        self.assertEqual(state["stage"], "pending", state)
        self.assertEqual(state["user_code"], "TEST-CODE")
        with self.assertRaises(urllib.error.HTTPError) as e:
            self.portal_request(base + "/start", data=body, headers=headers)
        self.assertEqual(e.exception.code, 409)
        self.portal_request(base + "/close", data=b"{}", headers=headers).close()
        p.communicate(timeout=4)
        self.assertIsNotNone(p.returncode, "Closing portal must release the waiting client")
        calls = (self.root / "mock-calls.jsonl").read_text()
        self.assertNotIn("--resume", calls, "Closing before the first poll must cancel polling")

    def test_portal_rejects_cross_origin_verification_url(self):
        p, base, _, headers = self.start_portal(verification_uri="https://attacker.invalid/activate")
        body = json.dumps({"server": "https://dify.example.invalid"}).encode()
        self.portal_request(base + "/start", data=body, headers=headers).close()
        state = self.wait_stage(base, {"error", "pending", "authorized"})
        self.assertEqual(state["stage"], "error", state)
        self.assertFalse(state.get("verification_uri"))
        self.assertNotIn("--resume", (self.root / "mock-calls.jsonl").read_text())
        self.portal_request(base + "/close", data=b"{}", headers=headers).close()
        p.communicate(timeout=4)

    def test_logout_cannot_succeed_while_authorization_keeps_running(self):
        p, base, _, headers = self.start_portal()
        logout = self.run_adapter(["auth", "logout"], mode="portal")
        if logout.returncode == 0:
            # A logout implementation may cancel the wizard before it logs out.
            try:
                p.communicate(timeout=3)
            except subprocess.TimeoutExpired:
                self.fail("Logout reported success while an authorization wizard can still reconnect")
        else:
            # The desktop compatibility implementation may instead reject busy logout.
            self.assertEqual(json.loads(logout.stderr)["code"], "E-0003")
            self.portal_request(base + "/close", data=b"{}", headers=headers).close()
            p.communicate(timeout=4)


if __name__ == "__main__":
    unittest.main(verbosity=2)
