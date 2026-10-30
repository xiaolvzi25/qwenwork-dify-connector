---
name: dify-qwen
description: 通过 Dify 官方 CLI 在当前用户获准的企业空间发现和运行应用，并按授权构建、测试与发布工作流。用于企业 Dify 应用、财务和运维等专业流程。仅适用于 Mac Apple Silicon 桌面连接器。
---

# Dify 企业版连接器

本连接器命令名为 `dify-qwen`，由适配器调用随包提供的官方 difyctl；不是 MCP 服务器。插件安装会同时分发 CLI 和本 Skill，无需往其他智能体的 skills 根目录写文件。

## 连接

先运行 `dify-qwen auth show --status`。正常未登录返回 `{"status":"unauthorized"}`，并非业务成功。此时引导用户到连接器页面点击认证；不要自动登录、读取配置文件或索取令牌。
用户明确要求连接时，可运行 `dify-qwen auth init`，由系统浏览器显示连接向导、服务器输入和鉴权码。用户自行在 Dify 授权页批准，适配器自动检查结果。需要指定服务器时使用 `dify-qwen auth init --server https://企业控制台域名`。不要在 Web Sandbox 运行桌面认证。

## 发现并执行业务

1. `dify-qwen identity whoami` 确认当前身份。
2. `dify-qwen cli help` 发现当前服务端命令树；不能猜测命令。需要参数时查询 `dify-qwen cli help <命令词>`。
3. `dify-qwen cli get workspace` 查询可见空间；然后 `dify-qwen cli get console_app --workspace-id <UUID>` 查询应用。
4. `dify-qwen cli describe console_app --app-id <ID>` 获取 input_schema，按实际变量名准备输入文件。
5. 按应用模式调用，例如 `dify-qwen cli run console_app workflow --input @request.json`。适配器自动增加 --json；暂不支持 --stream 和逐行事件透传。默认返回折叠的 JSON 结果。
6. 检查实际返回 status：ended 为正常结束，suspended 等待人工输入，incomplete 不可宣称完成。网络或权限错误不能伪装为空列表。

## 修改边界

运行、修改草稿、安装插件和发布是不同授权；读取目标命令的 effect 后再操作。执行写入必须有用户明确需求；移除和撤权先征得用户确认。未经用户确认不得发布、变更生产资源或代替业务审批人批准。不要通过改 role、指定他人 employee_id 或切换空间绕过服务端鉴权。构建前先检查可用模型、工具和知识库；测试后再按用户要求发布。

不要读取或输出本机配置目录、令牌或 Keychain 内容。调用业务优先使用此连接器，不使用另一个 difyctl 全局登录绕过当前连接器身份。用户主动请求退出时可执行 `dify-qwen auth logout`，仅影响此连接器的独立 Dify profile。

## 官方构建指南

随包包含官方 difyctl 技能与工作流构建参考，见 [官方技能](references/official-difyctl/SKILL.md)。构建、测试和发布前读取相应参考；其中的 `difyctl <业务命令>` 在此连接器中统一映射为 `dify-qwen cli <业务命令>`，不得绕过独立 profile 调用系统全局 difyctl。登录与退出仍使用本技能的 auth 命令。
