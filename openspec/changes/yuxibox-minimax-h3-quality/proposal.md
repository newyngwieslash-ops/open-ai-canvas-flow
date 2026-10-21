# Proposal

## Why

`yuxibox.cn` 新增 `minimax-h3-quality` 视频模型（OpenAI Video 协议，`POST /v1/videos`），管理后台当前只有 `yuxibox Seedance` 一个可选协议，无法承载该模型的差异化参数（`resolution`、`seed`）和"不支持参考视频"的约束。运营需要在系统渠道里直接接入该模型，按秒计费并区分高峰/空闲时段价格。

## What Changes

- 新增插件 `yuxibox-minimax-h3-quality`（能力 `video`），独立于现有 `yuxibox-seedance-video`，避免改动已验收的 Seedance 链路。
  - 创建 `POST /v1/videos`（`application/json`），查询 `GET /v1/videos/{{taskId}}`，下载复用响应中的 `video_url` / `download_url`（`resultEphemeral`）。
  - 请求体字段：`model`、`prompt`、`seconds`（1–15）、`resolution`（480p/768p，`720p` 别名映射为 `768p`）、`ratio`（16:9/9:16/1:1，默认 9:16）、`reference_images`（1–9 公网 HTTPS）、`audios`（0–3 公网 HTTPS）、`seed`。
  - **不发送** `videos`（上游不支持参考视频），对应参数声明为 `unsupported`，DTO 层直接拒绝而非静默丢弃。
  - 响应映射：`taskId` ← `id`、`status` ← `queued|in_progress|completed|failed`、`videos` ← `video_url` / `download_url`。
- 能力配置：`references.maxImages=9`、`maxAudios=3`、`maxVideos=0`、`duration` 1–15 秒、`resolutions=[480P,768P]`、`ratios=[16:9,9:16,1:1]`。
- 计费：`per_second` 按 480p/768p 两档配置基准价（高峰价），空闲时段暂按同一价目表结算，**时段差价需运营在管理后台手工调整价格档**。

## Goals

- 管理后台「新增系统渠道 / 模型编辑」的协议下拉里出现 `yuxibox minimax-h3-quality`，选择后按上述能力上限校验。
- 插件契约测试覆盖请求体逐字段、`720p → 768p` 别名、`videos` 被拒绝、响应状态映射。

## Non-Goals

- **不做时段差异化自动计费**：现有 `priceTiers` 只支持 `vquality` / `duration` 等输入选择器，没有时间窗口维度。本 change 不引入新的计费维度（属于跨模块改动，需独立 change 与财务口径确认）。
- 不实现 `input_reference` multipart 图片上传分支（宿主统一走公网 URL 提交，`requiresPublicMediaUrls=true`）。
- 不实现 `/v1/videos/{taskId}/content` 二进制下载通道（`video_url` 短链 7 天有效，宿主 `resultEphemeral` 会立即持久化）。
- 不修改 `yuxibox-seedance-video` 插件与 `yuxibox` 远程目录条目。

## Scope

- 新增 `plugin-packages/yuxibox-minimax-h3-quality/`（`manifest.json`、`README.md`、`docs/interface.md`）及生成的 `.yingce-plugin` 包。
- 注册到 `plugin-packages/generate-catalog.mjs`。
- 插件契约测试：`backend/internal/protocol/yuxibox_minimax_h3_quality_test.go`。
- 文档：`docs/content/docs/plugins/yuxibox-minimax-h3-quality.mdx`，并在 `pending-test.mdx` 登记待验收项。

## Key Unknowns

- 上游 `resolution` 是否接受大写 `480P`/`768P`（文档写小写 `480p`/`768p`，需按小写发送并验证）。
- `audios` 字段在 `minimax-h3-quality` 是否真的接受公网音频 URL（文档写 0–3 个公开 HTTPS 音频地址）。
- 时段价差的实际运维口径（高峰 08:00–24:00、空闲 00:00–08:00，跨时段完成不改单价）。

## Draft Acceptance Scenarios

1. **协议可选**：管理后台模型编辑的协议下拉出现 `yuxibox minimax-h3-quality`，选择后 `能力=视频`、请求 `POST /v1/videos`、`application/json`。
2. **720p 别名**：选择分辨率 720P 时请求体 `resolution` 发送 `768p`。
3. **不支持参考视频**：配置参考视频时宿主在准入阶段拒绝，不发出上游请求。
4. **字段上限**：10 张参考图或 4 条音频被拒绝；16 秒时长被拒绝。
5. **响应映射**：`queued / in_progress` 归一为进行中，`completed` 取 `video_url` 并持久化，`failed` 呈现上游错误信息。
