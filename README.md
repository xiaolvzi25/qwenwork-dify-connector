# Dify CLI connector for QwenWork

Version 1.0.0 targets QwenWork desktop on macOS Apple Silicon. It packages the unmodified official difyctl 1.18.0 and a native Go adapter for QwenWork's connector lifecycle. The stable top-level entry is `bin/dify-qwen`; it immediately executes `bin/darwin_arm64/dify-qwen` on the supported Mac architecture. No Python, Node.js or Go installation is needed by end users.

## Architecture

QwenWork requires `auth init`, `auth show --status`, `auth logout`, `identity whoami` and a package version. Official difyctl has a different login command structure, so a small compatibility adapter remains necessary. The Workbuddy reference uses its own `cli.json` lifecycle protocol, which QwenWork does not use.

Only the lifecycle branches inspect JSON. All other commands replace the adapter process with the packaged official CLI using `exec`, preserving arguments, stdin, stdout, stderr, streaming, files, signals and exit codes. `dify-qwen cli <args>` remains a compatible alias for `dify-qwen <args>`. The adapter does not translate operation IDs, add flags or impose a business-call timeout. Pass `--json` explicitly as the official Skill requires.

## Connect and use

1. Install the runtime ZIP through the QwenWork CLI connector distribution path.
2. Click Connect. The client invokes `dify-qwen auth init`, opening a browser setup page.
3. Enter your HTTPS Dify console URL. Follow the displayed device code/link and approve in Dify yourself.
4. The portal polls official difyctl, validates the account API, then exits. QwenWork checks `dify-qwen auth show --status` to synchronize connection state.
5. Discover capabilities with `dify-qwen help --json`, then the relevant command help. Run only operations requested by the user and permitted by the Dify account.

`dify-qwen --version` is the connector version; `dify-qwen version --json` delegates the official CLI version command. `dify-qwen identity whoami` returns identity fields only. `dify-qwen auth logout` logs out this connector profile.

## Marketplace submission

Upload `dify-qwen-listing-config-1.0.0.zip` through the configuration import control at the top of the QwenWork open-platform publishing form. Its `publish/connector.json` source declares `connector_kind: "cli"`, `clients: ["desktop"]` and `auth_type: "oauth"`. The desktop flow uses the CLI's local authorization. Review the imported names, provider, category and version, then upload `dify-qwen-cli-desktop-arm64-1.0.0.zip` through the separate **CLI ZIP package** control below. The listing version and runtime `plugin.json.version` must both be `1.0.0`.

On 2026-11-12, the listing configuration and runtime ZIP were successfully uploaded through QwenWork and the submission entered review. Real client installation, Dify device authorization, connected-state synchronization and token refresh still require acceptance.

## Credentials and permissions

The official CLI owns authentication and refresh. The adapter uses `~/.config/qwenwork-dify-connector/difyctl`, separately from an existing global difyctl login. `QWEN_DIFY_CONFIG_DIR` can select another connector profile. An incomplete reconnect blocks access to an old identity. No default enterprise URL or credentials are shipped.

The default profile belongs to the current macOS user. It is not automatically partitioned by QwenWork login. When changing QwenWork accounts on one OS account, disconnect first or have the host select a distinct profile. Dify's server enforces RBAC; this connector does not grant new Dify roles.

The portal listens on loopback only, uses a random path plus CSRF/Host/Origin checks and never returns tokens. Production business output follows the official CLI's output contract unchanged; do not request debug output or credential files in agent conversations.

## Build, test and update

Requires macOS arm64, Go 1.25+ and Python 3 for build/test tooling only. No third-party Go modules are used.

```
python3 build.py --go /absolute/path/to/go --official-cli /absolute/path/to/verified/difyctl
python3 tests/test_passthrough.py package/bin/darwin_arm64/dify-qwen
```

`build.py` verifies the pinned upstream SHA-256 once, compiles the native top-level launcher and adapter, preserves executable modes and validates the runtime ZIP. It separately generates the listing configuration ZIP from validated `publish/` sources. The source-and-tests ZIP includes `cmd/launcher`, listing sources and both test reports. Generated archives, upstream binaries and credentials are excluded from the source archive. Vendor verification is deliberately not repeated on every business call. Build records and checksums stay outside the runtime ZIP.

To update difyctl, update the pinned version/hash in `build.py`, review its official Skill, rebuild, run the passthrough/auth tests and repeat the real client acceptance flow. Keep the upstream CLI binary unmodified. Replacing an upstream binary without testing authorization and stream behavior is not a supported upgrade method.

## Scope and acceptance

Only macOS arm64 binaries are included. Web Sandbox, Intel Mac and Windows are not supported by this package. A Web edition needs a separately registered platform OAuth client and sandbox credential consumption; the desktop device flow cannot substitute for that integration.

Automated mock tests verify the compatibility layer, not a real enterprise login. Marketplace distribution, real device authorization, token refresh, QwenWork state synchronization and FIN/OPS permission behavior require acceptance against the actual host/server. A historical demonstration recording may illustrate the business flow without proving this version's installation/authorization.

The regression suite contains 17 mock black-box tests, including top-level launch, process replacement and exact input/output forwarding. See `test-results.json` and `test-results.txt` for the tested binary hashes, results and scope. Real QwenWork installation and device authorization have not yet been accepted.

## Notices

Official difyctl is redistributed without modification; see `package/DIFY-LICENSE.txt` and `package/THIRD_PARTY_NOTICES.txt`. The adapter is a QwenWork integration project and does not change the official CLI's command structure. No new license is assigned here to the repository owner's adapter source.
