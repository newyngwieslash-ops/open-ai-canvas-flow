# Proposal：接入 yuxibox seedance2.5 视频生成渠道

## Why

用户需要在影策渠道配置里接入 yuxibox（yuxibox.cn）中转的 seedance2.5-yx-count 视频模型。该渠道提供 OpenAI Video 风格异步协议（`POST /v1/videos` 创建 + `GET /v1/videos/{id}` 查询），支持文生视频、多参考图（0–30 张）、参考视频（0–10 条）、参考音频（0–10 条）、4–30 秒时长与多种比例/尺寸，且各时长均按一条视频计费（4.5 元/条）。现有协议清单中没有与该上游字段合同完全匹配的插件：内置 `newapi`（OpenAI Videos）是 multipart 请求且 `videos`/`audios` 标记为不支持；`newapi-channel-2` 路径与字段不同。需要新增一个专属协议插件并在渠道配置中可一键创建。

## What Changes

- 新增官方协议插件 `yuxibox-seedance-video`（`plugin-packages/generate-catalog.mjs` 定义 + `plugin-packages/yuxibox-seedance-video/` 源码目录 + `.yingce-plugin` 包）：
  - Base URL `https://yuxibox.cn`，Bearer API Key 鉴权。
  - 创建：`POST /v1/videos`，JSON 请求体：`model`、`prompt`、`seconds`（4–30，默认 30，整数或字符串）、`size`（1280x720 / 720x1280 / 720x720）或 `ratio`（adaptive / 16:9 / 9:16 / 1:1 / 4:3 / 3:4 / 21:9，使用 ratio 时省略 size）、`reference_images`（0–30）、`videos`（0–10）、`audios`（0–10）、`input_reference`。
  - 查询：`GET /v1/videos/{task_id}`，按上游 `status` 归一（queued→等待、succeeded/completed→成功、failed→失败），成功时从响应 URL 字段提取视频地址。
  - 插件校验：prompt 1–16000 字符、seconds 4–30、参考素材数量上限。
- 后端注册：`backend/internal/generation/registry.go`（官方声明式接口清单 + 显示名"yuxibox Seedance"）、`backend/internal/app/model_capability_defaults.go`（能力默认值：时长 4–30 枚举/范围、比例档位、参考上限 30 图/10 视频/10 音频、固定 720P 输出）。
- 前端：`web/src/lib/model-capabilities.ts` 能力映射、`web/src/stores/use-config-store.ts` 新增 `createYuxiboxChannel()` 预设（含默认计费 `fixed_request` 按条计费 4.5 元）、`web/src/pages/settings/channel-settings-pane.tsx` 新增"添加 yuxibox Seedance"按钮。
- 测试：插件协议契约测试（仿 `metaso_minimax_video_test.go`）+ 前端能力映射测试。
- 文档：插件 README/interface 文档与 `docs/content/docs/progress/pending-test.mdx` 登记（真实 Key 浏览器验收待用户执行）。

## Capabilities

### New Capabilities

- `yuxibox-seedance-video-provider`：yuxibox seedance2.5 视频生成渠道能力——渠道配置与鉴权、任务创建与查询协议、参数档位约束（4–30 秒、720P 固定输出、size/ratio 二选一）、参考素材映射（reference_images / videos / audios）、按条计费边界。

### Modified Capabilities

（无既有规格需求变化。）

## Impact

- **插件包**：`plugin-packages/generate-catalog.mjs`（新增插件定义）+ `plugin-packages/yuxibox-seedance-video/`（manifest.json、README.md、docs/interface.md）+ 重新生成的 `yuxibox-seedance-video.yingce-plugin`。
- **后端**：`backend/internal/generation/registry.go`、`backend/internal/app/model_capability_defaults.go`；新增 `backend/internal/protocol/yuxibox_seedance_video_test.go`。声明式协议宿主（provider_protocol.go / 轮询器 / SSRF 出站策略）复用，无专属宿主代码；无数据库 schema 变化。
- **前端**：`web/src/lib/model-capabilities.ts`、`web/src/stores/use-config-store.ts`、`web/src/pages/settings/channel-settings-pane.tsx`。
- **风险与边界**：
  - 上游"提交后保存 id，不要为同一视频重复 POST"——插件沿用宿主既有恢复合同（resumedProviderRequestID 只查询不重建）。
  - `input_reference` 为 multipart 文件字段，与 JSON `reference_images` 并存；第一期统一走 JSON `reference_images`（宿主素材可解析为 HTTPS/data URL），不发送 multipart `input_reference`。
  - 输出固定 720P，能力档位不暴露分辨率选择。
  - 计费 4.5 元/条与令牌分组倍率相关，UI 描述中注明"最终费用受令牌分组倍率影响"。
