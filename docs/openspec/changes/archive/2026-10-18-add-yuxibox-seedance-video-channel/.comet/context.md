# Comet Design Handoff

- Change: add-yuxibox-seedance-video-channel
- Phase: design
- Mode: compact
- Context hash: df51da5199da231c2c061dae3b3eb1be7ead48911f09b3e39047e4a3b47ea8d7

Generated-by: comet-handoff.sh
Task hash policy: task-content-v1. Read tasks.md for live completion; excerpts are design-time context.

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## docs/openspec/changes/add-yuxibox-seedance-video-channel/proposal.md

- Source: docs/openspec/changes/add-yuxibox-seedance-video-channel/proposal.md
- Lines: 1-38
- SHA256: 2f5e3e4c1ba92db4714e477531cabbf2bb758ef2bf7ed823afa414354e3c6a92

```md
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

```

## docs/openspec/changes/add-yuxibox-seedance-video-channel/design.md

- Source: docs/openspec/changes/add-yuxibox-seedance-video-channel/design.md
- Lines: 1-53
- SHA256: 70a8d7749286a9efb59b2193870cad1e878cf948afde397351127a18a2603a94

```md
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

```

## docs/openspec/changes/add-yuxibox-seedance-video-channel/tasks.md

- Source: docs/openspec/changes/add-yuxibox-seedance-video-channel/tasks.md
- Lines: 1-30
- SHA256: 0b546d31411a33caa251ed2c876b2f51f57761b721428a77ecb185965309ca6c

```md
# Tasks

## 1. 插件包与生成管线

- [ ] 1.1 在 `plugin-packages/generate-catalog.mjs` 新增 `yuxibox-seedance-video` 插件定义：provider（id `yuxibox-seedance-video`、baseUrl `https://yuxibox.cn`、bearer 鉴权、`requiresPublicMediaUrls`、model 精确 `seedance2.5-yx-count`、prompt 1–16000 校验、seconds 4–30 默认 30、ratio 七档校验、参考上限 30 图/10 视频/10 音频校验、create `POST /v1/videos` JSON：`model`/`prompt`/`seconds`/`ratio`/`reference_images`/`videos`/`audios`、poll `GET /v1/videos/{{taskId}}`、响应多字段 coalesce 提取视频 URL）与 workflow（defaults：model `seedance2.5-yx-count`、seconds 30、ratio `16:9`）。验证：`node plugin-packages/generate-catalog.mjs` 生成 manifest 且包含新插件
- [ ] 1.2 编写 `plugin-packages/yuxibox-seedance-video/README.md` 与 `docs/interface.md`（协议身份、配置字段 apiKey、统一字段映射、请求示例、响应与错误、计费与兼容边界），运行 `node plugin-packages/embed-documentation.mjs yuxibox-seedance-video` 回填 manifest documentation。验证：manifest documentation 包含两份文档全文
- [ ] 1.3 运行 `sh plugin-packages/build-packages.sh` 生成 `yuxibox-seedance-video.yingce-plugin`。验证：`plugin-packages/` 出现该包且 `unzip -l` 含 manifest.json、README.md、docs/interface.md

## 2. 后端注册与能力默认值

- [ ] 2.1 在 `backend/internal/generation/registry.go` 的 `OfficialDeclarativeVideoInterface` 注册 `yuxibox-seedance-video` → 显示名"yuxibox Seedance"。验证：`go -C backend build ./...` 通过
- [ ] 2.2 在 `backend/internal/app/model_capability_defaults.go` 为 `yuxibox-seedance-video` 添加能力默认值：Duration range 4–30 step 1 默认 30；Ratios `["adaptive","16:9","9:16","1:1","4:3","3:4","21:9"]` 默认 `16:9`；Resolutions 仅 `["720p"]`；参考上限 MaxImages 30 / MaxVideos 10 / MaxAudios 10（字节上限沿用宿主默认）；GenerateAudio/Watermark 不支持。验证：`go -C backend build ./...` 通过
- [ ] 2.3 新增 `backend/internal/protocol/yuxibox_seedance_video_test.go`：仿 `metaso_minimax_video_test.go` 用 `officialPackageAdapter` 断言插件包契约（身份与 workflow defaults、模型精确 seedance2.5-yx-count、时长/比例/数量校验拒绝、create JSON 体含 reference_images/videos/audios 与 seconds 字符串化、poll 路径与 Bearer、响应 URL coalesce、resultEphemeral）。验证：`go -C backend test ./internal/protocol/ -run YuxiboxSeedance`

## 3. 前端渠道预设与能力映射

- [ ] 3.1 在 `web/src/stores/use-config-store.ts` 新增 `createYuxiboxChannel()`：Base URL `https://yuxibox.cn`、模型 `seedance2.5-yx-count`、protocol `yuxibox-seedance-video`、`billingMode: "fixed_request"`、unitPriceMicrocredits 0、描述"yuxibox seedance2.5：4–30 秒默认 30、固定 720P、按条计费 4.5 元/条（实际以 yuxibox 控制台与令牌分组倍率为准）"。验证：`bun run build` 通过
- [ ] 3.2 在 `web/src/pages/settings/channel-settings-pane.tsx` 的 addChannel preset 增加 `"yuxibox"` 分支与"添加 yuxibox Seedance"按钮。验证：`bun run build` 通过，设置页出现按钮
- [ ] 3.3 在 `web/src/lib/model-capabilities.ts` 为 `yuxibox-seedance-video` 协议补能力默认值（时长 4–30 默认 30、八档比例默认 16:9、resolutions ["720P"]、参考上限 30/10/10）。验证：`bun run build` 通过，渠道模型能力卡片档位正确

## 4. 文档与登记

- [ ] 4.1 在渠道协议专题文档（`docs/content/docs/backend/` 对应页面）补充 yuxibox 协议说明（端点、鉴权、档位、计费模式）。验证：`cd docs && bun run types:check` 通过
- [ ] 4.2 在 `docs/content/docs/progress/pending-test.mdx` 登记"yuxibox seedance2.5 渠道待真实 Key 浏览器验收"。验证：文件已更新

## 5. 集成验证

- [ ] 5.1 后端：`go -C backend test ./internal/protocol/ ./internal/generation/ ./internal/app/`（新增用例与既有用例通过）。验证：全绿
- [ ] 5.2 前端构建：`cd web && bun run build`。验证：构建成功无类型错误
- [ ] 5.3 `openspec validate add-yuxibox-seedance-video-channel`（docs 根下执行）。验证：通过

```

## docs/openspec/changes/add-yuxibox-seedance-video-channel/.openspec.yaml

- Source: docs/openspec/changes/add-yuxibox-seedance-video-channel/.openspec.yaml
- Lines: 1-2
- SHA256: dc08c29816def5c12ac71497ed005c1c0d6d779aa76f03fa6cd1d3ed38d91ec9

```md
schema: spec-driven
created: 2026-10-18

```

## docs/openspec/changes/add-yuxibox-seedance-video-channel/specs/yuxibox-seedance-video-provider/spec.md

- Source: docs/openspec/changes/add-yuxibox-seedance-video-channel/specs/yuxibox-seedance-video-provider/spec.md
- Lines: 1-73
- SHA256: 81801a37181edd741ac9b7d53986434bd4c1f4cd31f007cda6eb20787a81cda8

```md
# Spec Delta

## Purpose

yuxibox seedance2.5 视频生成渠道能力：定义渠道协议身份、任务创建与查询合同、参数档位约束、参考素材映射与计费边界，使画布与创作页能通过 yuxibox 渠道完成文生视频和多模态参考生视频。

## ADDED Requirements

### Requirement: 渠道协议身份

系统 SHALL 提供声明式协议插件 `yuxibox-seedance-video`，默认 Base URL 为 `https://yuxibox.cn`，Bearer API Key 鉴权，创建端点为 `POST /v1/videos`（JSON 请求体），查询端点为 `GET /v1/videos/{task_id}`。管理员 SHALL 能在渠道配置中一键创建该渠道预设，预设模型为 `seedance2.5-yx-count`。

#### Scenario: 管理员一键创建渠道

- **WHEN** 管理员在个人渠道设置中点击"添加 yuxibox Seedance"按钮
- **THEN** 系统创建一个 Base URL 为 `https://yuxibox.cn`、协议为 `yuxibox-seedance-video`、含模型 `seedance2.5-yx-count` 的渠道，管理员只需填入 API Key 即可使用

#### Scenario: 模型名单校验

- **WHEN** 通过该渠道提交的生成请求未携带模型 `seedance2.5-yx-count`
- **THEN** 请求在提交上游前被拒绝，并提示该渠道模型必须为 `seedance2.5-yx-count`

### Requirement: 参数档位约束

系统 SHALL 将 yuxibox seedance2.5 的生成参数约束为：`prompt` 1–16000 字符；`seconds` 为 4–30 的整数秒且默认 30；画幅二选一——`size`（`1280x720` / `720x1280` / `720x720`）或 `ratio`（`adaptive` / `16:9` / `9:16` / `1:1` / `4:3` / `3:4` / `21:9`），二者不可同时发送；输出分辨率固定 720P，系统 MUST NOT 暴露分辨率档位选择。超出档位的请求 MUST 在提交上游前被拒绝，不得产生计费请求。

#### Scenario: 时长档位校验

- **WHEN** 用户提交时长为 3 秒或 31 秒的生成请求
- **THEN** 请求在提交上游前被拒绝，并提示时长范围为 4–30 秒

#### Scenario: 默认时长

- **WHEN** 用户未显式选择时长提交生成请求
- **THEN** 系统按上游默认 30 秒发送 `seconds`

#### Scenario: 画幅映射

- **WHEN** 用户选择横屏 16:9 画幅
- **THEN** 系统发送 `size: "1280x720"`（或对应 ratio 档位），且不同时发送 size 与 ratio 两个字段

### Requirement: 参考素材映射

系统 SHALL 把统一参考素材映射为上游 JSON 字段：参考图片映射为 `reference_images`（最多 30 条），参考视频映射为 `videos`（最多 10 条），参考音频映射为 `audios`（最多 10 条）；素材以公网 HTTPS URL 或 data URL 提交。超出数量上限的请求 MUST 在提交上游前被拒绝。

#### Scenario: 多参考图图生视频

- **WHEN** 用户连接 3 张参考图提交生成请求
- **THEN** 系统发送 `reference_images` 数组包含 3 个素材 URL

#### Scenario: 参考数量上限

- **WHEN** 用户连接 31 张参考图或 11 条参考视频提交生成请求
- **THEN** 请求在提交上游前被拒绝，并提示该渠道参考素材数量上限

### Requirement: 异步任务查询与结果归一

系统 SHALL 以 `GET /v1/videos/{task_id}` 轮询任务状态，将上游 `status` 归一为本系统状态（`queued`/`pending` 等为等待，`succeeded`/`completed` 为成功，`failed`/`cancelled` 为失败），成功时从响应 URL 字段提取视频地址并持久化，失败时向上呈现上游错误消息。系统 MUST NOT 为同一视频重复提交创建请求：恢复任务已有任务 ID 时只查询不重建。创建响应缺失任务 ID 时 MUST 按"结果不确定"处理，不得引导用户直接重发付费任务。

#### Scenario: 创建与轮询

- **WHEN** 创建请求返回 `{"id": "task_example", "status": "queued"}`
- **THEN** 系统保存任务 ID 并以 `GET /v1/videos/task_example` 轮询，直至状态归一为成功或失败

#### Scenario: 恢复任务不重复计费

- **WHEN** 任务在创建后因轮询失败进入恢复流程且已有任务 ID
- **THEN** 系统只继续查询该任务，不再次调用创建端点

#### Scenario: 结果不确定语义

- **WHEN** 创建请求 HTTP 成功但响应缺失任务 ID
- **THEN** 系统按"结果不确定"报告错误，不自动重发创建请求

```
