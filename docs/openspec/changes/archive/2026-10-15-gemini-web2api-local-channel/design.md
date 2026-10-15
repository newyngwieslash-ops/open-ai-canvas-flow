# Design

精简实现说明（tweak preset，无方案对比）。

## 配置拓扑

一条渠道、两个模型（同一 Base URL / Key），符合 YINGCE_HANDOFF.md 第 3 节建议：

| 层 | 记录 | 关键字段 |
|---|---|---|
| 渠道 | `ModelChannel` | name=`local-gemini-web2api`，baseUrl=`http://127.0.0.1:8083/v1`，apiKey=上游 `sk-gemini-...`，enabled |
| 图片模型 | `ChannelModel` | capability=`image`，protocol=`openai-image`，providerModelKey=`gemini-image`（裸名），modelKey=`gemini-nano-banana` |
| 视频模型 | `ChannelModel` | capability=`video`，protocol=`newapi`，providerModelKey=`gemini-video`（裸名），modelKey=`gemini-veo-web` |

## 关键判定依据

- 协议枚举：`backend/internal/model/models.go:64`（`openai-image`）、`:73`（`newapi`）。
- 图片无参考图走 `POST {base}/images/generations` JSON，响应认 `b64_json`/`url`；有参考图会打 `/images/edits`（上游没有，必 404）→ 能力配置必须关蒙版/参考图。
- 视频 `newapi` 默认 multipart `POST {base}/videos`，轮询 `GET {base}/videos/{id}`，完成后拉 `/content`；上游同时吃 JSON 和 multipart。
- 私网放行：本地启动脚本 `scripts/restart-local.ps1:355` 已设 `CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS='localhost,127.0.0.1'`；`backend/internal/outbound/outbound.go:496` 按主机名精确匹配，无需改环境。
- Base URL 必须带 `/v1`；不要用 `localhost:10100`（opencodex）。

## 执行方式

经影策管理后台（渠道页 `web/src/pages/admin/channels/channels-page.tsx`）的 HTTP API 写入配置，不直接改库；管理员登录后用渠道 CRUD 接口创建渠道与两个模型。价格按本地习惯填 0（若环境强制则填最低档）。

## 验证方式

见 tasks.md：上游自检（models/images/videos 探针）→ 影策内真实图片任务出图 → 视频任务建单轮询。失败先查上游 `docs/local/dev-server.log`。
