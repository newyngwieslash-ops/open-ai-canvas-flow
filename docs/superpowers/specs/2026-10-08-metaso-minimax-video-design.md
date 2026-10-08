---
comet_change: metaso-minimax-video
role: technical-design
canonical_spec: openspec
archived-with: 2026-10-08-metaso-minimax-video
status: final
---

# Design Doc：密塔 H3 视频供应商接入

## 1. 概述

按 open 阶段确定的方案 A（后端纯声明式 + 前端专属 provider）深化实现设计。密塔（metaso.cn）是 MiniMax V2 协议中转，创建/查询端点路径与官方 `minimax-video` 插件一致，差异仅在 Base URL 与参数档位。设计原则：协议知识全部收敛进插件 manifest，宿主与调度零改动，前端只做轻量 provider。

## 2. 插件包设计（协议唯一来源）

### 2.1 generate-catalog.mjs 新增定义

在 `plugin-packages/generate-catalog.mjs` 中仿 `fisherParams` 段新增：

```js
const metasoParams = videoParams // 复用统一 video 参数面：model/prompt/images/videos/audios/duration/aspectRatio/resolution/providerOptions
```

插件定义要点：

| 字段 | 值 |
| --- | --- |
| id / providerId | `metaso-minimax-video` |
| name（workflow label） | `MiniMax-H3 (Metaso)` |
| vendor（author） | `Metaso / 影策` |
| baseUrl | `https://metaso.cn/api/minimax` |
| auth | `bearer` → `apiKey` |
| requiresPublicMediaUrls | `true` |
| scopes | `admin.system-channel, user.custom-channel, canvas, creation, agent`（与 Fisher/MiniMax 一致） |
| defaults | `{ model: "MiniMax-H3", resolution: "768P", duration: 5, aspectRatio: "16:9" }` |

### 2.2 create 请求映射

```
POST /v2/video_generation
body:
  model:      request.model
  content:    [text(prompt)] ++ images→{type:image_url, image_url:{url}, role:coalesce(media.role,"first_frame")}
                       ++ videos→{type:video_url, video_url:{url}, role:"reference_video"}
                       ++ audios→{type:audio_url, audio_url:{url}, role:coalesce(media.role,"reference_audio")}
  resolution: $upper(request.resolution)        // 768P / 2K 均原样合法
  duration:   request.duration
  ratio:      coalesce(request.aspectRatio, "16:9")
```

与 Fisher 的差异：无 `audio_control`、无 `start_time_seconds`（密塔不支持，第一期不发送）。

### 2.3 poll 与响应映射

```
GET /v2/query/video_generation/{{taskId}}
response:
  taskId:   coalesce(response.task_id, taskId)
  status:   coalesce(response.task.status, "pending")
  message:  coalesce(response.task.error.message, response.error.message, response.base_resp.status_msg, response.task.message)
  videos:   response.task.content.url
  errorPaths: ["task.error.code", "error.type"]
  resultEphemeral: true   // 宿主立即下载持久化，规避中转 URL 过期
```

### 2.4 校验链（manifest validations）

1. `model == "MiniMax-H3"`
2. prompt 非空且 ≤ 10000 字符
3. `$upper(resolution) ∈ {480P, 768P, 1080P, 2K}`
4. duration ∈ [4, 15] 整数
5. ratio ∈ {21:9, 16:9, 4:3, 1:1, 3:4, 9:16}；无媒体且未选比例时报错（文生视频必须指定比例）
6. first_frame ≤ 1、reference_image ≤ 9、videos ≤ 3、audios ≤ 3；图片/音频 role 白名单校验

### 2.5 文档与打包

- `README.md`（插件定位、密塔中转说明）+ `docs/interface.md`（协议身份、配置字段 apiKey、统一字段映射表、请求/响应示例、兼容边界）。
- `node plugin-packages/embed-documentation.mjs metaso-minimax-video` 回填 manifest documentation。
- `sh plugin-packages/build-packages.sh` 产出 `metaso-minimax-video.yingce-plugin`。

## 3. 后端改动（2 处 case + 测试）

### 3.1 generation/registry.go

```go
case "metaso-minimax-video":
    return "密塔 H3", true
```

### 3.2 app/model_capability_defaults.go

```go
case "metaso-minimax-video":
    video.Operations = append(video.Operations, "reference_to_video")
    video.References.MaxImages, video.References.MaxVideos, video.References.MaxAudios = 9, 3, 3
    video.References.MaxVideoDuration = 15
    video.Duration = VideoDurationConfig{Selection: "enum", Values: []int{4,...,15}, Default: 5}
    video.Ratios = []string{"21:9", "16:9", "4:3", "1:1", "3:4", "9:16"}
    video.Resolutions, video.DefaultResolution = []string{"480P", "768P", "1080P", "2K"}, "768P"
```

说明：官方 minimax-video 用 enum 时长（4–15 逐值列出）而非 range；密塔对齐官方语义用 enum。`Operations` 只加 `reference_to_video`（Fisher 的 `audio_to_video` 依赖驱动音频语义，密塔无）。

### 3.3 零改动确认

- `provider.go` 媒体 URL 策略：声明式适配器 `RequiresPublicMediaURLs=true` 自动 requireURL+preferURL（`provider.go:289`）。
- `provider_video.go:66-72`：声明式适配器优先调度，未注册时报“密塔 H3 视频插件未安装”。
- `provider_protocol.go` 宿主：创建/轮询/下载/恢复（X-Task-Id 语义）全部复用。
- 无数据库 schema 变化；无 Fisher 式宿主校验层。

## 4. 前端改动

### 4.1 纯函数层 `web/src/lib/metaso-minimax-video.ts`

```ts
export const METASO_PROTOCOL = "metaso-minimax-video";
export function buildMetasoPayload(input: MetasoInput): Record<string, unknown>
```

- 校验：model 固定、prompt ≤10000、resolution ∈ 档位集、duration 4–15 整数、ratio 六档、素材数量上限、首帧唯一。
- payload 与 manifest 同构（前端先行校验给出友好错误，后端 manifest 校验兜底）。
- 无 Fisher 的 options/driveAudioId 概念。

### 4.2 provider 层 `web/src/services/api/video-provider-metaso.ts`

仿 `video-provider-minimax.ts`：
- `createMetasoVideoTask(deps, config, model, prompt, references, videoReferences, audioReferences, options)`：解析素材公网 URL（`resolveVideoReferenceImageUrl` / `resolveVideoReferenceMediaUrl`）、`buildMetasoPayload`、POST `/video_generation`、task_id 校验（缺失时报“创建结果不确定，请勿直接重发付费任务”）。
- `pollMetasoVideoTask(...)`：GET `/query/video_generation/{id}`，状态归一（succeeded→completed 取 `content.url`、failed/cancelled→failed 带上游错误、queued/running→pending、未知状态抛错）。
- URL 拼接沿用 `metasoVideoUrl(config, path)`：base 以 `/v2` 结尾则直接拼，否则补 `/v2` 前缀。

### 4.3 分发与类型

- `video.ts`：create 行 `if (requestConfig.interfaceType === "metaso-minimax-video") return createMetasoVideoTask(...)`；poll 行 `if (task.provider === "metaso-minimax") ...`。
- `video-contracts.ts:51`：provider 联合类型加 `"metaso-minimax"`。

### 4.4 能力与渠道配置

- `model-capabilities.ts`：`if (protocol === "metaso-minimax-video")` 块，与 3.2 后端默认值一致（枚举时长、档位、素材上限）。
- `use-config-store.ts`：`defaultBaseUrlForChannelInterface` 加 `https://metaso.cn/api/minimax`；新增 `createMetasoChannel()`（name "密塔 H3"、models ["MiniMax-H3"]、protocol "metaso-minimax-video"、billingMode "per_second"、unitPriceMicrocredits 0、capabilityConfig 用 defaultModelCapabilityConfig、description 说明价格待确认）。
- `channel-settings-pane.tsx`：addChannel preset 加 `"metaso"`，按钮“添加密塔 H3”。

## 5. 测试策略

| 层 | 文件 | 断言 |
| --- | --- | --- |
| 插件契约 | `backend/internal/protocol/metaso_minimax_video_test.go` | 包↔manifest 一致；身份（provider.Label/BaseURL/requiresPublicMediaURLs）；三种模式 content[] 组装（文生仅 text、首帧 role、视频+音频 role）；档位校验（拒 2160p、拒 3 秒、拒 adaptive）；创建/查询路径与 Bearer；响应 taskId/status/videos/message 映射；resultEphemeral |
| 前端纯函数 | `web/src/lib/metaso-minimax-video.test.ts` | payload 三种模式；档位拒绝；首帧唯一校验；错误消息 |
| 集成 | `go test ./...` + `bun run build` | 新增用例与既有用例全过、构建无类型错误 |
| 浏览器验收 | `pending-test.mdx` 登记 | 真实 Key 下三种模式生成、查询、结果入库 |

## 6. 边界与降级

- 创建响应缺 task_id：前端报“创建结果不确定”，不自动重发（对齐 Fisher 付费安全语义）。
- 轮询 4xx/5xx/超时：宿主既有重试与 `routeDispatchUncertainError` 语义，不伪装成功。
- 素材无公网 URL：宿主 requireURL 策略直接拒绝，不静默转存。
- 密塔不支持的扩展字段（audio_control 等）从源头不发送；若上游仍报错，错误经 message 链呈现给用户。

## 7. 非目标

- 不接密塔文本/图片能力；不做 audio_control/drive_audio/start_time_seconds；不暴露 adaptive；无宿主专属校验；无数据库变更。
