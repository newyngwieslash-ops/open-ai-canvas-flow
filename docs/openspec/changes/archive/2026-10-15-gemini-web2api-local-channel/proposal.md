# Proposal

## Why

本机 `gemini-web2api-go`（`http://127.0.0.1:8083`，网页端 Gemini 反代）已在上游 `ee04849` 落地 OpenAI Images / OpenAI Videos 形状的适配层，可作为零成本本地图片与视频生成上游。影策当前没有指向它的渠道，需要通过管理后台配置接入，让工作台 / 画布能直接使用本地 Gemini 图片与视频生成能力。

约束：**只改影策运行时配置（数据库中的渠道与模型记录），不改影策代码，不改 gemini-web2api-go 代码。**

依据：`F:\AI_APIS\gemini-web2api-go\docs\YINGCE_HANDOFF.md`。

## What Changes

- 在影策管理后台新增一条渠道 `local-gemini-web2api`，Base URL `http://127.0.0.1:8083/v1`，使用上游面板 / kv 取出的 `sk-gemini-...` API Key。
- 同一渠道下新增两个模型：
  - 图片：capability=`image`、protocol=`openai-image`、providerModelKey=`gemini-image`、modelKey 如 `gemini-nano-banana`；关闭蒙版 / 参考图 / 透明底。
  - 视频：capability=`video`、protocol=`newapi`（OpenAI Videos）、providerModelKey=`gemini-video`、modelKey 如 `gemini-veo-web`；不勾参考视频 / 参考音频。
- 验收：无参考图的图片任务出图；无参考媒体的视频任务建单进入轮询（完成与否取决于 Google 账号是否有 Veo，建单成功即达标）。

明确不做（上游不支持，接了必失败）：

- 不选影策自带 `gemini-image` / `gemini-veo` 协议（打 Google 官方 v1beta，不是本上游）。
- 不接蒙版编辑、参考图、参考视频（上游无 `/images/edits`，不读参考媒体字段）。
- 不改 `provider_image.go` / `protocol/builtin.go` 等任何代码。

## Capabilities

### New Capabilities

（无——本 change 是纯运行时配置，不改任何代码路径与 spec 级行为。）

### Modified Capabilities

（无。在 `.openspec.yaml` 中设置 `skip_specs: true`：配置数据变化不构成 spec 级行为变化。）

## Impact

- 影策数据库：新增 1 条 `ModelChannel` + 2 条 `ChannelModel` 记录（经管理后台 API 写入，非手工改库）。
- 无代码、无依赖、无文档结构变化。本地开发后端由 `restart.bat`（`scripts/restart-local.ps1`）启动，已自动设置 `CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS='localhost,127.0.0.1'`，私网上游放行无需额外操作。
- 若渠道连接失败，排错依据为上游日志 `F:\AI_APIS\gemini-web2api-go\docs\local\dev-server.log` 与影策任务错误。
