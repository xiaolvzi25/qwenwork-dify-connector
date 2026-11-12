---
name: dify
name_en: dify
description: 运行、搭建和发布 Dify 应用。支持 Dify Cloud 和私有部署，登录时输入你的 Dify 控制台地址即可
description_en: Run, build and publish Dify apps. For Dify Cloud or self-hosted, log in with your console URL.
---

# Dify 企业版连接器

本连接器调用命令 `dify-qwen`。Unix 顶层入口为 `bin/dify-qwen`，Windows 为 `bin/dify-qwen.exe`，自动选择本机系统与架构的原生适配器。首次使用下载固定版本官方 difyctl 并校验 SHA-256，之后使用本机缓存；用户不需要安装 Python、Node.js 或 Go。它是 CLI 连接器，安装包同时提供适配器和本 Skill。

## 连接与身份

先运行 `dify-qwen auth show --status`。正常未登录为严格 JSON `{"status":"unauthorized"}`，退出码 0。网络错误或权限不足不能当作已连接。未连接时引导用户在连接器页面点击认证，不因业务未授权而自动登录。

用户明确要求连接时，可运行 `dify-qwen auth init`。它打开浏览器连接向导：用户填写 HTTPS Dify 控制台地址、看到设备码并自行在 Dify 页面批准，程序自动检测结果。可以用 `--server https://企业控制台域名` 预填地址。不依赖终端输入，不向用户索取令牌，不读取凭据文件。不要在 Web Sandbox 运行本包。

认证后用 `dify-qwen identity whoami` 确认身份。若同一台电脑切换千问账号，先确认 Dify 身份并退出旧连接；默认 profile 按操作系统用户保存，不会自动随千问账号分区。退出仅在用户明确请求时运行 `dify-qwen auth logout`；它只影响本连接器独立 profile，活动认证向导需先关闭。

## 发现与调用

始终显式带 `--json`。业务命令与官方 difyctl 的参数结构相同；适配器不增加参数、不改写输入、不缓冲或重编码输出。

1. `dify-qwen help --json` 发现当前服务端命令树。服务端操作随版本变化，不猜命令。
2. `dify-qwen help <命令词> --json` 查询描述符、输入 schema、effect 和实际示例。
3. 从可见空间与应用开始，读取应用输入 schema，再准备文件或 JSON 请求；使用描述符中真实的命令、变量和资源 ID。
4. 根据官方命令使用文件输入与输出；流式选项按实际帮助执行，逐条消费事件，不把首条事件当作完整结果。
5. 以返回的业务状态和产物验证结果。失败、挂起、等待人工输入、未完成都不能宣称执行成功。

旧写法 `dify-qwen cli <官方参数>` 仍可使用；新增写法直接使用 `dify-qwen <官方参数>`。不要把操作 ID 的点分结构自行拆改。不要调用全局 difyctl 绕过本连接器身份。

## 构建与修改

发现可用模型、工具、插件、知识库后再构建；按切片调试草稿，验证完整输入输出，再依据用户请求发布。运行应用、修改草稿、安装插件和发布是不同操作，执行前阅读 effect 并确认当前任务已授权。移除或撤权先征得用户确认。不得修改角色、冒用员工身份、切换全局 profile 或绕过服务端 RBAC。

不要读取或输出令牌、密钥、本机配置、凭据路径内容和完整调试环境。遇到 403 应报告权限不足；不回退共享账号，也不伪装为空列表。

## 官方参考

构建、测试或发布前按需读取 [官方 difyctl Skill](references/official-difyctl/SKILL.md) 及其引用。参考中的 `difyctl <参数>` 一律通过本连接器的 `dify-qwen <参数>` 执行；认证使用上文千问生命周期命令。官方业务命令未被本适配器重写。`dify-qwen --version` 返回连接器版本 1.0.1，官方版本通过 `dify-qwen version --json` 查询。

## 当前范围

支持 Windows x64、Linux x64/ARM64 和 macOS Intel/Apple Silicon 桌面端。首次连接需访问 GitHub 官方发布下载地址，下载失败应检查网络，不改用未校验程序。Linux 需图形桌面、glibc 2.28+ 和 xdg-open。Dify 必须支持固定版本官方 difyctl 的设备授权与相应服务端操作目录。网页端需要独立的平台 OAuth 接入，本包不提供。Windows ARM64 原生与 32 位系统不在本包范围。自动化兼容性测试不等于真实客户端安装、授权刷新和业务权限验收。
