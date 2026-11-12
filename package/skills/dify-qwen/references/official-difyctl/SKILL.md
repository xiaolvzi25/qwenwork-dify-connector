---
name: difyctl
description: 'Operate a Dify server through difyctl: find and run any operation, and build, test, publish and change Workflow and Chatflow apps. Use when the task involves difyctl, a Dify server, or Dify apps.'
display_name: "dify"
display_name_en: "dify"
description_zh: "运行、搭建和发布 Dify 应用。支持 Dify Cloud 和私有部署，登录时输入你的 Dify 控制台地址即可"
description_en: "Run, build and publish Dify apps. For Dify Cloud or self-hosted, log in with your console URL."
author: "LangGenius"
examples_zh:
  - "列出我在 Dify 上的应用"
  - "运行 Dify 里的客服工作流，问题是……"
  - "在 Dify 上建一个把网页总结成三点的工作流并发布"
examples_en:
  - "List my apps on Dify"
  - "Run the customer support workflow in Dify. The question is..."
  - "Build a workflow on Dify that summarizes a web page in three points, then publish it"
version: "1.0.0"
---

> **QwenWork 接入注记（官方业务参考）**：本目录参考来自 LangGenius 的 WorkBuddy 连接器 1.0.0；这里的 `version: "1.0.0"` 表示参考来源版本，并非当前千问连接器版本。业务示例中的 `difyctl <参数>` 在本连接器中统一执行为 `dify-qwen <参数>`，不得调用全局 `difyctl` 绕过连接器身份。登录、状态检查和退出一律遵循[外层 QwenWork Skill](../../SKILL.md) 的 `auth init`、`auth show --status`、`auth logout` 生命周期；下方 WorkBuddy 的登录检查与人工回复策略仅保留作上游参考，不能直接替代千问连接器的鉴权流程。

# Dify in WorkBuddy

This connector installs difyctl but does not log it in. Before the first Dify task in a conversation, check the login:

1. Run `difyctl login --resume --json`. If it prints an `email`, you are logged in. Go on with the task.
2. If it fails with `not_logged_in`, ask the human for their Dify console address: the URL they open in a browser to use Dify. For Dify Cloud it is `https://cloud.dify.ai`. For self-hosted Dify it is their own address, such as `https://dify.example.com`. Never guess one.
3. Log in to that address as [`references/setup.md`](references/setup.md) describes: start with `--no-wait`, give the human the link and the code, end your turn, and run `--resume` once when they say they approved.

Talk to the human in their language.

# difyctl

difyctl talks to a Dify server. Business commands are not built in: the server publishes a catalog of operations, and each one is a command you discover at run time.

Always pass `--json`. Help and errors are text on a terminal and JSON in a pipe; `--json` (or `DIFY_OUTPUT=json`) forces JSON. Results and streams are always JSON.

Commands whose effect is destructive can't be undone and have no confirmation prompt. Check effect in help and confirm with the human first. Also confirm a server operation that shows no effect.

## Finding what the CLI can do

An operation id is dotted, `run.console_app.chat`, and is typed as words: `difyctl run console_app chat`. Ids come from the server and can change, so look them up instead of assuming them.

Four views under `help`, covering local commands and server operations as one tree:

1. `difyctl help --json`: the map. Start here.
2. `difyctl help <namespace> --json`: everything under one namespace (`difyctl help get`). The bare namespace, `difyctl get --json`, prints the same.
3. `difyctl help <words> --json`: search by plain words (`difyctl help upload file`), top 20 entries plus `total`. Widen the words when nothing fits.
4. `difyctl help <id> --json`: one descriptor, dotted or spaced id. For an operation: `input` (JSON Schema), `bind`, `kind`, `effect`, `examples` and `pins` (values difyctl fills in, currently `workspace_id`). `<command> --help` prints the same.

`--all` adds operations the server marks internal. `--full` on the bare `help` prints every descriptor; rarely needed.

Not logged in, or the server unreachable, gives the local commands plus one line on stderr.

## Where to read next

- Read [`references/setup.md`](references/setup.md) to check login or log in, switching server or workspace, or running in a sandbox or CI.
- Read [`references/operations.md`](references/operations.md) before running any operation: flags, inputs, files, results, hints, errors and exit codes.
- Read [`references/build-workflow.md`](references/build-workflow.md) when asked to create, change, test or publish a Workflow or Chatflow app.
- Read [`references/plugins.md`](references/plugins.md) to find tools, models and knowledge bases before building, when a plugin, model, tool or credential is missing, or when the human asks to install or remove a plugin.
- Read [`references/workspace-skills.md`](references/workspace-skills.md) to list, read, create, change, roll back or publish a workspace skill.
