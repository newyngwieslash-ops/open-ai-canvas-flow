# Proposal：接入密塔 H3 视频生成供应商

## Why

影策的视频生成能力需要更多可用的 MiniMax-H3 渠道来源。密塔（metaso.cn）提供 MiniMax V2 协议中转（`https://metaso.cn/api/minimax`），与本仓库已有的官方 MiniMax 插件、Fisher-H3 插件同构，接入成本低，能直接为画布与创作页增加一个文生视频 / 图生视频 / 多模态参考生视频渠道。

## What Changes

- 新增官方协议插件 `metaso-minimax-video`（源码 `plugin-packages/metaso-minimax-video/`，构建为 `.yingce-plugin` 包）：
  - 创建：`POST https://metaso.cn/api/minimax/v2/video_generation`，Bearer 认证。
  - 查询：`GET /v2/query/video_generation/{{taskId}}`（用户提供的查询示例确认同官方 MiniMax V2）。
  - 请求体：`model` + `content[]`（`text` / `image_url`（role `first_frame`/`reference_image`）/ `video_url`（role `reference_video`）/ `audio_url`（role `reference_audio`））+ `resolution` / `duration` / `ratio`。
  - 支持三种工作模式：文生视频、图生视频（首帧）、多模态参考生视频（参考视频 + 参考音频）。
- 后端注册该声明式接口（`generation/registry.go`、`model_capability_defaults.go` 能力默认值、渠道协议清单）。
- 前端新增 provider（`video-provider-metaso.ts`）并接入 `video.ts` 分发、能力档位（`model-capabilities.ts`）、默认 Base URL（`use-config-store.ts`）。
- 参数档位（已与用户澄清）：
  - `resolution`：可选 `480P` / `768P` / `1080P` / `2K`，默认 `768P`（生成时可选，不锁定单档；用户确认密塔支持 2K 且要求开放）。
  - `duration`：4–15 整数秒，默认 5。
  - `ratio`：`21:9` / `16:9` / `4:3` / `1:1` / `3:4` / `9:16` 六档固定比例，默认 `16:9`。
  - `adaptive` 不在第一期档位内。
- 测试：插件协议契约测试（对齐 Fisher-H3 测试模式）+ 前端 payload 构建测试。
- 文档：渠道协议文档与 `pending-test.mdx` 登记。

## Capabilities

### New Capabilities

- `metaso-minimax-video-provider`：密塔 H3 视频生成渠道能力——渠道配置、任务创建与查询协议、参数档位约束、参考素材角色映射（first_frame / reference_video / reference_audio）。

### Modified Capabilities

（无既有规格需求变化；`upstream-sync` 与本变更无关。）

## Impact

- **插件包**：`plugin-packages/metaso-minimax-video/`（manifest.json、README.md、docs/interface.md）+ `plugin-packages/generate-catalog.mjs`（新增插件定义）+ 重新生成的 `.yingce-plugin` 包。
- **后端**：`backend/internal/generation/registry.go`（接口注册）、`backend/internal/app/model_capability_defaults.go`（能力默认值）。
- **前端**：`web/src/services/api/video-provider-metaso.ts`（新增）、`web/src/services/api/video.ts`（分发）、`web/src/lib/model-capabilities.ts`、`web/src/stores/use-config-store.ts`（默认渠道与 Base URL）。
- **不改动**：声明式协议宿主 `provider_protocol.go`、轮询器、SSRF/出站策略（密塔为公网域名，走既有出站通道）；不需要数据库 schema 变化。
- **风险**：密塔对 `audio_control` 等 MiniMax 扩展字段的支持未知，第一期不发送这些字段；参考素材需公网可访问 URL（沿用 `requiresPublicMediaUrls` 约束）。
