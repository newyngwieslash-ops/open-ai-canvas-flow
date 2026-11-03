---
comet_change: add-yuxibox-seedance-video-channel
role: technical-design
canonical_spec: openspec
archived-with: 2026-10-18-add-yuxibox-seedance-video-channel
status: final
---

# Design Doc：yuxibox seedance2.5 视频渠道接入（深度技术设计）

本文件是 open 阶段 `design.md`（高层框架）的深度技术细化。规格以 OpenSpec delta spec `specs/yuxibox-seedance-video-provider/spec.md` 为准（canonical）。

## 1. 已确认决策（用户 2026-10-18 确认）

| # | 决策点 | 结论 |
| --- | --- | --- |
| D1 | 接入方案 | **声明式插件** `yuxibox-seedance-video`（metaso/fisher 同构），后端零专属宿主代码 |
| D2 | 画幅策略 | **统一 ratio**：能力层暴露七档 ratio，插件把 `aspectRatio` 单字段映射为上游 `ratio`，不发 `size` |
| D3 | 计费建模 | **按条计费** `fixed_request`；描述注明 4.5 元/条基础价与令牌分组倍率；unitPriceMicrocredits = 0 |

## 2. 组件与数据流

```
画布/创作页（选模型 seedance2.5-yx-count）
   │  任务创建（生成参数：prompt/时长/比例/参考素材）
   ▼
后端 provider_video.go: ensureOfficialProtocolAdapter("yuxibox-seedance-video")
   │  命中官方声明式注册表 → 走声明式协议宿主（runProtocolAdapterTaskWithPolicy）
   ▼
插件 manifest（yuxibox-seedance-video.yingce-plugin）
   ├─ validations：model 精确 / prompt 1–16000 / seconds 4–30 / ratio 七档 / 素材上限 30-10-10
   ├─ create：POST {base}/v1/videos  JSON {model, prompt, seconds, ratio, reference_images[], videos[], audios[]}
   ├─ poll：GET {base}/v1/videos/{taskId}
   └─ response：taskId←id/task_id/taskId；status←status；videos←url/video_url/videoUrl/result_url/output.url/data.*；resultEphemeral
   ▼
宿主轮询/下载/持久化（既有通道，SSRF 出站策略、任务恢复合同均复用）
```

### 2.1 插件定义（generate-catalog.mjs 新增块）

- `id/providerId`: `yuxibox-seedance-video`；`name`: "yuxibox Seedance"；`vendor`: "yuxibox"；`capability`: video。
- `baseUrl`: `https://yuxibox.cn`；`auth`: bearer（apiKey）；`requiresPublicMediaUrls: true`。
- `params`：复用共享 `videoParams` 但过滤掉 `resolution`（固定 720P 不暴露）、`generateAudio`、`watermark`（上游合同无这些字段）；保留 model/prompt/images/videos/audios/duration/aspectRatio/providerOptions。
- `validations`（全部走表达式引擎，错误消息中文）：
  - `model == "seedance2.5-yx-count"`
  - `0 < len(trim(prompt)) <= 16000`
  - `4 <= duration <= 30`
  - `coalesce(aspectRatio, "16:9") ∈ ["adaptive","16:9","9:16","1:1","4:3","3:4","21:9"]`
  - `len(images) <= 30`、`len(videos) <= 10`、`len(audios) <= 10`
- `create`（jsonCreate `/v1/videos`）：
  ```js
  {
    model: ref("request.model"),
    prompt: ref("request.prompt"),
    seconds: { $toString: ref("request.duration") },   // 上游示例 "seconds": "30" 字符串
    ratio: coalesce(ref("request.aspectRatio"), "16:9"),
    reference_images: omit(map({ $sortByOrder: ref("request.images") }, "media", ref("media.value"))),
    videos:       omit(map({ $sortByOrder: ref("request.videos") }, "media", ref("media.value"))),
    audios:       omit(map({ $sortByOrder: ref("request.audios") }, "media", ref("media.value")))
  }
  ```
  `omit` 语义：数组为空时整个字段不发送（宿主 manifest_request 既有行为）。
- `poll`: `{ method: "GET", path: "/v1/videos/{{taskId}}" }`。
- `response`: `asyncResponse("video")` 基座（taskId ← response.id/task_id/taskId/data.id；status ← response.status/state/data.status 缺省 pending；message ← error.message/message/fail_reason；videos ← video_url/videoUrl/result_url/url/data.video_url/output.url；errorPaths ["error.code"]；resultEphemeral true）。
- `workflows[0].defaults`: `{ model: "seedance2.5-yx-count", duration: 30, aspectRatio: "16:9" }`。
- `notes`：协议身份、档位、计费、不重复 POST 恢复合同、input_reference 不发送的边界说明（渲染进 manifest documentation）。

### 2.2 后端注册（两处）

1. `backend/internal/generation/registry.go` `OfficialDeclarativeVideoInterface`：`case "yuxibox-seedance-video": return "yuxibox Seedance", true`。
2. `backend/internal/app/model_capability_defaults.go` `DefaultModelCapabilityConfigForModel`：
   ```go
   case "yuxibox-seedance-video":
       video.Operations = append(video.Operations, "reference_to_video", "audio_to_video")
       video.References.PromptMaxChars = 16000
       video.References.MaxImages, video.References.MaxVideos, video.References.MaxAudios = 30, 10, 10
       video.Duration = VideoDurationConfig{Selection: "range", Min: 4, Max: 30, Step: 1, Default: 30}
       video.Ratios = []string{"adaptive", "16:9", "9:16", "1:1", "4:3", "3:4", "21:9"}
       video.DefaultRatio = "16:9"
       video.Resolutions, video.DefaultResolution = []string{"720p"}, "720p"
   ```
   GenerateAudio/Watermark 保持基座默认（Supported false）。Operations 含 audio_to_video 的依据：上游 audios 参考音频独立成数组，允许音频驱动素材组合（与 fisher/metaso 的 reference_to_video + 纯音频限制不同——yuxibox 文档未要求"音频必须配图"，纯音频+文本是否可行未实测；第一期按含 reference_to_video + audio_to_video 开放，验收时若上游拒绝纯音频再收紧）。

### 2.3 前端（三处）

1. `web/src/stores/use-config-store.ts`：
   ```ts
   export function createYuxiboxChannel(): ModelChannel {
       return createModelChannel({ name: "yuxibox Seedance", baseUrl: "https://yuxibox.cn", apiKey: "", models: ["seedance2.5-yx-count"],
           modelCosts: [{ model: "seedance2.5-yx-count", displayName: "Seedance 2.5 (yuxibox)", capability: "video", protocol: "yuxibox-seedance-video",
               billingMode: "fixed_request", unitPriceMicrocredits: 0,
               capabilityConfig: defaultModelCapabilityConfig("yuxibox-seedance-video", "seedance2.5-yx-count"),
               description: "yuxibox seedance2.5：4–30 秒（默认 30）、固定 720P、七档比例；按条计费基础价 4.5 元/条，实际以 yuxibox 控制台与令牌分组倍率为准。" }] });
   }
   ```
2. `web/src/pages/settings/channel-settings-pane.tsx`：`addChannel` preset 类型扩展 `"yuxibox"`；按钮 `<Button onClick={() => addChannel("yuxibox")}>添加 yuxibox Seedance</Button>`（放在"添加密塔 H3"之后）。
3. `web/src/lib/model-capabilities.ts`：`defaultModelCapabilityConfig` 内 `if (protocol === "yuxibox-seedance-video")` 分支，与后端能力默认值逐字段对齐（promptMaxChars 16000、maxImages 30、maxVideos 10、maxAudios 10、duration range 4–30 default 30、ratios 八值含 adaptive、defaultRatio 16:9、resolutions ["720P"]、operations 含 reference_to_video/audio_to_video）。

### 2.4 不需要前端 provider

`video.ts` 的专属 provider 分发行（newapi-channel-2/gemini-veo 等）服务前端直连遗留路径；本插件在官方声明式注册表中，后端 `provider_video.go:67-70` 直接走适配器路径，前端任务创建由后端代理执行。metaso 接入时同样未加前端 provider 分发（它最终有独立 provider 是因为其前端直连需求，本渠道无此需求）。

## 3. 关键边界与错误语义

| 边界 | 行为 | 依据 |
| --- | --- | --- |
| 同一视频不重复 POST | 宿主恢复合同：已有 provider_request_id 只查询不重建 | 上游错误处理章节 + 宿主既有语义（provider_video.go:231 注释） |
| 创建响应缺任务 ID | 按"结果不确定"失败上抛，不自动重发 | spec 场景"结果不确定语义"；asyncResponse taskId 提取失败即报错 |
| HTTP 400/401/403/429/5xx | 真实失败上抛（错误消息从 error.message/message/fail_reason 提取） | 上游错误处理章节 |
| 参考素材 URL | 仅 HTTPS/data URL（requiresPublicMediaUrls），裸 Base64/内网地址不可用 | 上游参数表 + 宿主渲染合同 |
| seconds 类型 | 发字符串（上游示例 `"seconds": "30"`，参数表 integer/string 均接受） | 上游请求报文示例 |
| 不发 input_reference | 统一 JSON reference_images 路径 | D1 附带决策：JSON 路径覆盖全部用例，避免 multipart 分支 |
| 无取消接口 | manifest 不声明 cancel | 上游文档未提供 DELETE |

## 4. 测试策略

### 4.1 后端契约测试（yuxibox_seedance_video_test.go）

- **TestYuxiboxSeedancePackage**：包内 manifest 与源码 manifest 逐字节一致；provider 身份（id/baseUrl/bearer/requiresPublicMediaUrls）；workflow defaults（model/seconds 30/ratio 16:9）；workflow parameters 与 provider parameters 一致。
- **TestYuxiboxSeedanceCreateAssembly**：`BuildCreate` 输出 JSON 体断言：`model == "seedance2.5-yx-count"`、`seconds == "30"`（字符串）、`ratio == "16:9"`、`reference_images`/`videos`/`audios` 数组按序映射 URL；空素材组不出现字段键；`Content-Type: application/json`。
- **TestYuxiboxSeedanceValidation**：表格驱动拒绝用例——model 换名、prompt 空/16001 字符、duration 3/31、ratio "2:3"（非档位）、31 张图、11 条视频、11 条音频；每例 `BuildCreate` 必须返回错误且不产出请求 spec。
- **TestYuxiboxSeedancePollAndResponse**：poll 路径 `/v1/videos/task-123` GET；响应解析：`{"id":"t1","status":"queued"}` → taskId t1/pending；`{"status":"succeeded","url":"https://..."}` → 成功且视频 URL 命中 coalesce 链；`resultEphemeral` 为 true。

### 4.2 前端验证

- `bun run build`：类型覆盖 `createYuxiboxChannel` 返回结构、preset 分支类型、能力映射字段。
- 手动验收（登记 pending-test）：设置页"添加 yuxibox Seedance"按钮出现 → 渠道卡片档位（时长 4–30 默认 30、八档比例、无分辨率选择、参考上限 30/10/10）→ 填 Key 后画布发起真实生成。

### 4.3 集成

`go -C backend test ./internal/protocol/ ./internal/generation/ ./internal/app/`；`cd web && bun run build`；`openspec validate add-yuxibox-seedance-video-channel`（docs 根）。

## 5. 风险与缓解

| 风险 | 缓解 |
| --- | --- |
| 完成态响应字段未实测（文档只有 queued 占位） | 多字段 coalesce 兜底；验收差异只改 manifest 响应映射一处 + 测试 |
| "请求规格影响费用"表述模糊 | 描述注明以控制台为准；验收核对账单后可补 unitPrice |
| 纯音频生视频是否被上游接受未知 | 能力层第一期开放 audio_to_video；验收若拒绝则收紧 Operations 并同步 spec |
| 分组倍率导致实际扣费 ≠ 4.5 元 | 描述已声明倍率影响；不预设 unitPrice 死值 |

## 6. Spec Patches

无。delta spec 4 需求 12 场景已覆盖；D2（统一 ratio）是"画幅映射"场景允许范围内的实现选择（场景原文"发送 size: 1280x720（或对应 ratio 档位）"）。
