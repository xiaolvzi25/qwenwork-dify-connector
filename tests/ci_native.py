"""Build and verify one real CI host; no Dify login or credentials are used.

The regression suite uses a native mock. The separate smoke test downloads the
pinned official CLI into a temporary isolated profile and checks its real hash.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]


def digest(path):
    h = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            h.update(block)
    return h.hexdigest()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--go", default="go")
    parser.add_argument("--output", type=Path, default=ROOT / "native-test-reports")
    args = parser.parse_args()
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    goos = {"win32": "windows", "darwin": "darwin"}.get(sys.platform, "linux")
    arch = {"aarch64": "arm64", "arm64": "arm64", "x86_64": "amd64", "amd64": "amd64"}[platform.machine().lower()]
    target = goos + "_" + arch
    suffix = ".exe" if goos == "windows" else ""
    env = dict(os.environ, GO_BINARY=args.go, PYTHONUTF8="1", PYTHONUNBUFFERED="1")
    env.setdefault("GOCACHE", str(ROOT / ".build-cache"))
    report = {"host_target": target, "host_platform": platform.platform(), "checks": {}, "scope": "Native CLI processes and official distribution smoke only; no live Dify login or QwenWork client acceptance"}

    def run(name, command, *, call_env=env, timeout=600):
        started = time.monotonic()
        result = subprocess.run([str(word) for word in command], cwd=ROOT, env=call_env,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=timeout)
        log = result.stdout.decode("utf-8", errors="replace") + result.stderr.decode("utf-8", errors="replace")
        (output / (name + ".txt")).write_text(log, encoding="utf-8")
        report["checks"][name] = {"exit_code": result.returncode, "duration_seconds": round(time.monotonic() - started, 3)}
        if name == "regression":
            count = re.search(r"Ran (\d+) tests?", log)
            skipped = re.search(r"skipped=(\d+)", log)
            if count:
                report["checks"][name]["tests_run"] = int(count.group(1))
                report["checks"][name]["skipped"] = int(skipped.group(1)) if skipped else 0
        print(log, flush=True)
        if result.returncode:
            raise RuntimeError(name + " failed with exit code " + str(result.returncode))
        return result

    try:
        run("build", [sys.executable, "build.py", "--go", args.go, "--target", target, "--output", "dist"], timeout=900)
        report["binaries"] = {path.relative_to(ROOT).as_posix(): {"sha256": digest(path), "bytes": path.stat().st_size}
                              for path in (ROOT / "package/bin").rglob("*") if path.is_file()}
        (output / "binary-sha256.json").write_text(json.dumps(report["binaries"], indent=2) + "\n", encoding="utf-8")
        run("go-tests", [args.go, "test", "-v", "./src", "./internal/process", "./cmd/launcher"], timeout=300)
        run("regression", [sys.executable, "tests/test_passthrough.py"], timeout=300)
        catalog = json.loads((ROOT / "src/upstream-catalog.json").read_text(encoding="utf-8"))
        asset = catalog["platforms"][target]
        with tempfile.TemporaryDirectory(prefix="qwen-real-official-smoke-") as temp:
            isolated = Path(temp)
            # A fresh package with no bundled vendor forces the online install
            # path even if a developer has made an offline build in this checkout.
            shutil.copytree(ROOT / "package/bin", isolated / "package/bin")
            launcher = isolated / "package/bin" / ("dify-qwen" + suffix)
            profile = isolated / "profile"
            smoke_env = {key: value for key, value in env.items() if not key.startswith("DIFY_")}
            smoke_env.pop("QWENWORK_SANDBOX_ID", None)
            smoke_env["QWEN_DIFY_CONFIG_DIR"] = str(profile)
            fresh = run("fresh-status", [launcher, "auth", "show", "--status"], call_env=smoke_env, timeout=20)
            assert json.loads(fresh.stdout) == {"status": "unauthorized"}
            cache = profile / "runtime" / catalog["version"] / target / ("difyctl" + suffix)
            assert not cache.exists(), "Fresh status must not download the CLI"
            version = run("official-version", [launcher, "version", "--json"], call_env=smoke_env, timeout=720)
            data = json.loads(version.stdout)
            assert catalog["version"] in json.dumps(data), "Unexpected official CLI version"
            assert cache.is_file(), "Online smoke did not populate its isolated runtime cache"
            assert cache.stat().st_size == asset["bytes"]
            actual = digest(cache)
            assert actual == asset["sha256"], "Official runtime cache SHA256 mismatch"
            report["official_cli"] = {"version": catalog["version"], "source": asset["url"], "sha256": actual, "bytes": cache.stat().st_size}
            checked = run("installed-unauthorized-status", [launcher, "auth", "show", "--status"], call_env=smoke_env, timeout=30)
            assert json.loads(checked.stdout) == {"status": "unauthorized"}
        report["result"] = "passed"
        return 0
    except Exception as error:
        report["result"] = "failed"
        report["failure"] = str(error)
        print(str(error), file=sys.stderr)
        return 1
    finally:
        (output / "native-results.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


if __name__ == "__main__":
    raise SystemExit(main())
