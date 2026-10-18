# Design：yuxibox seedance2.5 视频渠道接入

## Context

本仓库视频渠道已有两类接入先例：

- **声明式插件**（metaso-minimax-video、fisher-h3-video、newapi-channel-1/2 等）：manifest 声明创建/查询/响应映射，构建为 `.yingce-plugin` 包，后端零专属宿主代码，任务调度、轮询、SSRF、结果下载全部走声明式协议宿主（`provider_protocol.go`）。
- **内置 host: 适配器**（`builtin.go` 的 `newapi` / OpenAI Videos）：multipart 请求，`videos`/`audios` 参数标记 unsupported。

yuxibox（yuxibox.cn）提供 seedance2.5-yx-count 的 JSON 风格 `/v1/videos` 异步合同（用户已提供完整 API 文档）：创建 POST JSON（`model`/`prompt`/`seconds`/`size` 或 `ratio`/`reference_images[]`/`videos[]`/`audios[]`/`input_reference`），查询 GET 同路径任务资源，4–30 秒默认 30，固定 720P，按条计费 4.5 元。与内置 `newapi`（multipart、无参考视频/音频）和 `newapi-channel-2`（路径 `/v1/video/generations`、字段 `image_urls` 等）均不匹配，需要独立插件。见 proposal.md。

## Goals / Non-Goals

**Goals:**

- 新增声明式插件 `yuxibox-seedance-video`：管理员可一键创建渠道预设，用户在画布/创作页选 seedance2.5-yx-count 生成视频。
- 支持文生视频与多模态参考生视频（最多 30 图 / 10 视频 / 10 音频，HTTPS/data URL）。
- 时长 4–30 秒（默认 30）与画幅（size 或 ratio 二选一）在宿主能力层约束，档位外请求提交前拒绝。
- 复用声明式协议宿主：创建、轮询、下载、SSRF、恢复合同（已有任务 ID 只查询不重建）全部走既有通道。

**Non-Goals:**

- 不接 yuxibox 的其他模型或其他能力（本插件仅 video、仅 seedance2.5-yx-count）。
- 不发送 multipart `input_reference` 文件字段（统一走 JSON `reference_images`；上游同时支持两者，JSON 路径覆盖全部用例）。
- 不实现取消接口（上游文档未提供 DELETE）。
- 不改数据库 schema；不加后端专属宿主代码。

## Decisions

1. **新插件 `yuxibox-seedance-video`，而非复用内置 `newapi` 或 `newapi-channel-2` 配渠道**。
   内置 `newapi`（OpenAI Videos）是 multipart 合同且不支持参考视频/音频，`videos`/`audios` 映射为 unsupported，直接复用会丢参考素材；`newapi-channel-2` 路径与字段（`image_urls`/`video_urls`/`audio_urls`、`aspect_ratio`）与 yuxibox 合同（`reference_images`/`videos`/`audios`、`size`/`ratio`）不同。独立插件保证协议身份、字段映射与档位默认值准确表达，且与 metaso 先例一致：`generate-catalog.mjs` 定义 → `embed-documentation.mjs` 回填文档 → `build-packages.sh` 构建包。

2. **画幅发送策略：优先 `size` 像素值，宿主 ratio 档位映射到上游允许值**。
   上游 `size` 仅三值（1280x720/720x1280/720x720），`ratio` 七值（adaptive/16:9/9:16/1:1/4:3/3:4/21:9）。能力层 ratios 暴露上游 ratio 档位全集（16:9/9:16/1:1/4:3/3:4/21:9/adaptive），插件 create 模板把 `aspectRatio` 映射为 `ratio` 字段（对 16:9/9:16/1:1 三档也可落 size，但统一走 ratio 减少分支）；不暴露分辨率档位（固定 720P）。`size` 与 `ratio` 由插件模板单字段映射天然互斥，不存在同时发送。

3. **参考素材走 `requiresPublicMediaUrls: true` + JSON 数组映射**。
   `images -> reference_images`、`videos -> videos`、`audios -> audios`，各为 media[] map 到 `media.value`；声明 requiresPublicMediaUrls 后宿主把素材解析为公网 URL（data URL 上游同样接受，沿用既有解析链路）。数量上限（30/10/10）在插件 validations 与能力默认值（MaxImages/MaxVideos/MaxAudios）双层声明——validations 保证协议层硬边界，能力默认值驱动前端 UI 档位展示。

4. **后端零专属宿主代码**。
   与 metaso 同构：`generation/registry.go` 注册接口名 → 显示名"yuxibox Seedance"，`model_capability_defaults.go` 提供能力默认值（时长 range 4–30 step 1 默认 30；ratios 八档默认 16:9；resolutions 仅 720p 单值；参考上限 30/10/10；generateAudio/watermark 不支持）。创建/轮询/下载/恢复由声明式宿主完成，超时或 5xx 不自动重建，宿主保留任务 ID 后查询（对齐上游"不要为同一视频重复 POST"）。

5. **前端预设 + 能力映射，新增独立 provider 不必要**。
   该插件走声明式协议宿主（后端执行），前端只需：`use-config-store.ts` 新增 `createYuxiboxChannel()`（Base URL `https://yuxibox.cn`、模型 `seedance2.5-yx-count`、`billingMode: "fixed_request"` 按条计费、描述注明 4.5 元/条与分组倍率）、`channel-settings-pane.tsx` 加"添加 yuxibox Seedance"按钮、`model-capabilities.ts` 按协议名补能力默认值。前端 `video.ts` 的专属 provider 分发行（newapi-channel-2、gemini-veo 等）只服务"前端直连"遗留路径；系统渠道/声明式协议任务由后端 `provider_protocol.go` 执行，不需要前端 provider。渠道预设走用户自定义渠道（scope user），管理员也可在管理后台建系统渠道。

6. **时长默认 30 秒**。
   上游默认 30 秒且按条计费（时长不影响单价），能力默认值 Duration.Default = 30；用户可下调至 4–30 任意整数。枚举逐值列出（4–30）与 range 声明均可，选 range 4–30 step 1 避免超长枚举数组。

## Risks / Trade-offs

- **真实上游行为未实测**：响应 URL 字段名（文档示例只有 status/seconds/size，未给完成态字段）按 `/v1/videos` 生态惯例用 `url`/`video_url`/`result_url`/`output.url` 多字段 coalesce 提取；真实 Key 验收时若字段不同，只需改 manifest 响应映射。已在 pending-test 登记。
- **分组倍率**：4.5 元/条是基础价，"最终费用受令牌分组倍率及请求规格影响"，渠道描述注明实际价格以 yuxibox 控制台为准，unitPrice 留 0 待管理员确认。
- **720P 固定**：能力层不暴露分辨率选择，用户无法误发 1080p 档位；若上游未来开放更高分辨率需更新插件与能力默认值。
- **请求规格计费影响**：文档"最终费用受请求规格影响"表述模糊，第一期按条计费（fixed_request）建模；若验收发现规格（如比例/时长）影响价格，再调整计费模式。
