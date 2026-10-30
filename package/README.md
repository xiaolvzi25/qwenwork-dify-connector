# Dify CLI 连接器 Mac 桌面试验版

版本 0.1.0。用于千问办公开放平台的 CLI（远程连接）类型，投放端为“仅桌面”，鉴权选择 oauth（桌面自管）。不填写 MCP URL，不创建 Web 平台 OAuth Application ID。本包是待联合验收的适配原型，不是已经通过阿里审核的官方市场连接器。

## 安装包

.qwenwork-plugin/plugin.json：平台声明。
bin/darwin_arm64/dify-qwen：Python 3 桌面适配入口。
vendor/darwin_arm64/difyctl：未修改的 Dify 官方 1.18.0 macOS arm64 稳定版二进制。
skills/dify-qwen/SKILL.md：平台随连接器安装的技能。
DIFY-LICENSE.txt：上游许可证。

仅宣称 Mac Apple Silicon 目标；尚未完成千问办公的下载分发、入口解析和最终用户认证联调。不支持 Windows、Intel Mac 或 Web Sandbox。依赖 Python 3.9 及以上、系统默认浏览器。Python 需要在客户端子进程 PATH 中可用。上架之前由平台确认目标设备具备解释器；若要求完全独立二进制，应改用原生编译适配器再发布。本包不会静默安装 Python、禁用 TLS 或绕过系统提示。

CLI 入口和官方业务 CLI 相对包路径定位。无需在系统全局安装 CLI。包内官方二进制 SHA-256 在适配器启动时检查，值与 GitHub 1.18.0 Release 资产 digest 一致。请不要单独替换文件。

## 用户连接流程

1. 平台分发本 ZIP 与 Skill。客户端调用 `dify-qwen --version`。
2. 用户点击连接/认证，客户端调用 `dify-qwen auth init`。
3. 适配器打开仅绑定 127.0.0.1 随机端口的浏览器向导，填写 HTTPS Dify 控制台地址。示例默认值为用户提供的 https://console-platform-dev.dify.dev，可替换。
4. 用户点击获取鉴权码。适配器调用官方 difyctl login --no-browser --no-wait --no-keyring，向导显示鉴权码和 Dify 的同源验证链接。
5. 用户自行进入 Dify 页面授权。适配器按服务端 interval（不短于 5 秒）调用 login --resume，随后调用 describe account 验证真实身份。向导显示已连接，auth init 退出 0。
6. 千问办公通过 `dify-qwen auth show --status` 再次查询状态。已连接返回严格 JSON {"status":"authorized"}；正常未登录返回 {"status":"unauthorized"} 且退出 0。最终连接器卡片状态由客户端刷新机制显示。

设备码不会依赖千问办公展示 stdout/stderr，而是在本机浏览器显示。此设计针对手册说明的设备码显示缺口；平台是否接受这一浏览器向导方式必须在真实客户端验收。auth init 最长 15 分钟；平台需要提供足够进程超时，或明确支持的后台认证方式。本包没有伪造客户端状态写回接口。

## 凭据与账号隔离

默认配置目录 ~/.config/qwenwork-dify-connector，目录 0700、设置文件 0600。底层 difyctl 使用该目录下独立 profile，与原有 ~/.config/difyctl 分开；认证显式使用官方 --no-keyring，将会话凭据保存在独立目录内，避免不同 CLI 共用 Keychain 项的潜在影响。本包不主动读取、复制、打印 Token；同一 OS 用户或有权限的进程仍可访问这些文件。继承环境中的 DIFY_TOKEN、DIFY_SERVER、DIFY_WORKSPACE_ID 等会移除，防止混入其他身份。

需要多 QwenWork 账号时，平台应为各账号提供独立、持久的 QWEN_DIFY_CONFIG_DIR，或者由不同 OS 账号隔离。手册未定义可靠的 QwenWork 用户标识注入方式，因此不能宣称自动按千问办公账号隔离；在同一 OS 账号下切换千问办公账户之前，应退出本连接器。撤销 Dify 会话后状态必须重新验证。配置目录不得指向会被公共上传或共享的路径。

`auth logout` 只退出此独立 profile，不删除系统全局 CLI 的登录，不删除连接器平台条目。多个服务器目前仅支持一个显式选定服务器；新授权开始时旧授权对本适配器业务调用关闭，失败后需重新认证。

## 业务调用

`dify-qwen cli help` 获取命令树；然后按返回说明调用，例如 cli get workspace、cli get console_app、cli describe console_app、cli run console_app workflow。不改造业务权限，始终以服务端鉴权为准。传入的业务字段不是可信身份。适配器禁止原始配置、登录和调试输出透传；不支持 --stream，使用官方折叠 JSON。长任务超时 600 秒，不保证无限时长的交互。

## 状态与错误

状态检查调用真实账号接口，不能只凭文件存在、退出码 0 或历史缓存判断。401/未登录为 unauthorized；403 为 E-0005；网络错误为 E-0004 且非 0。错误输出为脱敏 JSON。账号接口字段兼容顶层、data、account 包裹；如果实际接口不符合则失败关闭，待适配，不伪造已连接。授权验证码与验证 URL 只保留在内存，不写业务日志。向导使用随机路径、CSRF、Origin/Host 检查、no-store/no-referrer，未对局域网开放。关闭窗口本身不能可靠通知 CLI，请用向导关闭按钮或等待超时。

## 上架前尚需完成

平台包上传与架构分发；auth init 进程超时；真实 Dify 授权响应与账号响应；独立文件 profile 隔离；取消/过期/撤权/续期；普通业务账号 FIN/OPS 的只读应用列表；不同 QwenWork 账号隔离；客户端已连接状态刷新。测试报告在交付包外，不属于安装 ZIP。
