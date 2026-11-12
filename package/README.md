# dify

Run, build and publish Dify apps. For Dify Cloud or self-hosted, log in with your console URL.

Version 1.0.1 adds Windows and Linux desktop compatibility while retaining the existing Dify device authorization and command interface. The single online installation ZIP contains platform-native adapters and bundled skills; it downloads the unmodified official difyctl 1.18.0 for the current machine on first use, checks the pinned SHA-256 and exact length, then uses a separate local cache.

## Desktop platforms

| Platform | Architecture | Entry | Requirements |
| --- | --- | --- | --- |
| Windows | x64 / AMD64 | bin/dify-qwen.exe | Windows 10 or later, installed QwenWork desktop, default browser |
| Linux | x64 / AMD64, ARM64 | bin/dify-qwen | QwenWork Linux desktop, graphical session, glibc 2.28+, xdg-open, system POSIX sh |
| macOS | Intel x64, Apple Silicon ARM64 | bin/dify-qwen | Installed QwenWork desktop and default browser |

QwenWork platform requirements: [Windows](https://help.aliyun.com/zh/qwenwork/windows-install), [Linux](https://help.aliyun.com/zh/qwenwork/cn-install-linux). Windows ARM64 native, 32-bit systems and Web Sandbox are excluded. No Python, Node.js, Go or PowerShell installation script is required by end users.

## Why this is an online package

The whitepaper permits one installation ZIP of at most 50 MiB. Bundling five complete upstream binaries would exceed that limit. This package instead ships five native adapters, a POSIX top-level dispatcher, and a separate Windows native top-level executable. The first authorized connection downloads only the current platform's official CLI. Initial auth status on a new installation returns unauthorized immediately, without a download.

The release URL, version, byte length and SHA-256 of every upstream executable are pinned in src/upstream-catalog.json from the [official 1.18.0 release](https://github.com/langgenius/dify/releases/tag/1.18.0). Downloads require HTTPS, follow only the named GitHub release/CDN hosts, verify the complete file before atomic installation, and use an OS lock to serialize concurrent installs. Failed or cancelled transfers never become executable cache files. There is no automatic upgrade to latest and no downloaded shell script is executed.

First use needs network access to github.com and release-assets.githubusercontent.com (or the legacy GitHub release CDN hosts in the pinned downloader policy), plus approximately 100 MB free space for the current upstream executable. Further business calls use the installed cache. A receipt checks file size and modification time without rehashing the entire binary on every call; missing or changed receipts trigger full SHA-256 verification. This does not defend against another process with the same OS-user privileges deliberately changing both executable and receipt.

The whitepaper describes multi-platform entries but does not explicitly certify this first-use download mechanism. Marketplace parsing, host command discovery and the first-use network path must still be accepted in the actual QwenWork client.

## Marketplace upload

1. Import dify-qwen-listing-config-1.0.1.zip into the connector configuration field.
2. Upload dify-qwen-cli-desktop-multiplatform-online-1.0.1.zip into the separate CLI ZIP installation field.
3. Set the form version to 1.0.1, delivery to desktop, and authentication to OAuth with the desktop CLI managing login. Keep the existing marketplace resource identifier when updating a listing.
4. Test installation, Connect, a real authorized read-only business call, disconnect and token refresh on each target client before broad distribution.

The configuration declares connector_kind=cli and clients=[desktop]. It has no MCP URL, MCP declaration or embedded user credentials. Do not select Web delivery for this desktop device flow. Previous 0.2.1/1.0.0 marketplace upload results do not establish acceptance of this new online multi-platform package.

## Connect and use

1. Install from the QwenWork connector distribution path.
2. Click Connect. The client runs dify-qwen auth init and the system browser opens a local setup page.
3. Enter your HTTPS Dify console URL. The adapter prepares the pinned official CLI, displays the device code and verification link, and waits for your own approval in Dify.
4. Official difyctl completes authorization; the adapter verifies the account API and QwenWork checks dify-qwen auth show --status.
5. Use dify-qwen help --json to discover server operations, then read command help before running the user-authorized operation.

The existing auth init, auth show --status, auth logout, identity whoami and --version interface remains. dify-qwen --version prints 1.0.1; dify-qwen version --json delegates to the official CLI version command. Authentication does not grant any additional Dify permissions.

## Runtime and credentials

Business arguments, stdin, stdout, stderr, streamed results, file paths and exit status pass through without JSON re-encoding. POSIX replaces the process with exec. Windows cannot replace a process; native launchers retain the child exit status and streams while a kill-on-close Job Object owns the full child tree. The Windows child starts suspended and is attached to the Job before it runs. Ctrl+C/Ctrl+Break cancellation and host termination clean up owned descendants. No cmd.exe or PowerShell command string is used.

The browser is opened with macOS open, Linux xdg-open, or Windows ShellExecuteW. The authorization portal remains loopback-only with a random URL, Host/Origin/CSRF checks, same-origin Dify verification URLs, and no token output. Cross-process OS locks prevent simultaneous authorization/logout conflicts.

Profiles default to ~/.config/qwenwork-dify-connector on macOS/Linux and %LOCALAPPDATA%/qwenwork-dify-connector on Windows. QWEN_DIFY_CONFIG_DIR can select a different profile. The official CLI receives only that profile's DIFY_CONFIG_DIR; inherited DIFY_* settings are removed (case-insensitively on Windows). A profile belongs to the OS user, not automatically to a QwenWork account. Disconnect before switching QwenWork accounts, or have the host select an account-specific profile. Tokens and refresh behavior remain owned by official difyctl.

## Build and verify

Build tooling needs Go 1.25+ and Python 3.9+; runtime users do not. There are no third-party Go modules.

```sh
python3 build.py --go /absolute/path/to/go --output dist
GO_BINARY=/absolute/path/to/go python3 tests/test_passthrough.py
/absolute/path/to/go test ./src ./internal/process ./cmd/launcher
```

Default builds compile all five OS/architecture adapters. --target linux_amd64 (or another catalog target) builds a single platform. --official-cli /path/to/verified/difyctl additionally includes an offline upstream binary and requires exactly one --target; the build rejects a mismatched pinned hash. Build artifacts, downloaded upstream binaries, cache directories and credentials are excluded from the source ZIP.

The CI workflow runs regression tests on native Windows and Linux runners, verifies the real pinned upstream download/version and checks that a fresh unauthorized status does not download. Reports distinguish build checks, simulated account/authorization behavior, and real client acceptance. Cross-compilation alone is not a native execution test. Local or CI mock results do not prove real Dify login, token refresh, FIN/OPS RBAC or marketplace installation.

## Notices

The official CLI is unmodified and belongs to its upstream authors. See package/DIFY-LICENSE.txt and package/THIRD_PARTY_NOTICES.txt. This integration does not claim official Alibaba or Dify certification. No new license is assigned here to the repository owner's adapter source.
