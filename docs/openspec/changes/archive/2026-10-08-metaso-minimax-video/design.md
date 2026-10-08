# Design：密塔 H3 视频供应商接入

## Context

本仓库视频生成已沉淀两条 MiniMax V2 同构接入先例：

- 官方 `minimax-video` 插件（`plugin-packages/minimax-hailuo-video-v2/`）：纯声明式，后端零专属代码，前端有独立 provider。
- `fisher-h3-video` 插件：同协议但带 audio_control/首尾帧扩展，后端有宿主边界校验（`provider_fisher.go`）与前端专属控件。

密塔（metaso.cn）是 MiniMax V2 的协议中转：创建/查询端点路径与官方完全一致，差异仅在域名与参数档位（用户澄清：480P/768P/1080P/2K 可选、默认 768P；4–15 秒；六档固定比例）。见 proposal.md。

## Goals / Non-Goals

**Goals:**

- 以最小改动新增一个可用的密塔视频渠道：管理员可配渠道，用户在画布/创作页可选密塔模型生成视频。
- 三种工作模式可用：文生视频、图生视频（first_frame）、多模态参考生视频（reference_video + reference_audio）。
- 复用声明式协议宿主：密塔无宿主专属代码，任务调度、轮询、SSRF、结果下载全部走既有通道。

**Non-Goals:**

- 不接密塔的文本/图片等其他能力（本渠道仅 video capability）。
- 不实现 Fisher 式 audio_control、drive_audio、start_time_seconds 扩展（密塔支持性未知）。
- 不暴露 adaptive 档位（用户澄清第一期不进档位；2K 已确认开放）。
- 不改数据库 schema；不需要迁移。

## Decisions

1. **新插件 `metaso-minimax-video`，而非复用 `minimax-video` 配渠道**。
   独立插件能在协议清单中显示"密塔"身份与专属默认值（768P、档位、渠道描述），避免管理员把密塔误配成官方 MiniMax；也允许日后密塔协议偏离官方时独立演进。备选"仅建渠道"被否：UI 会显示为 MiniMax 且无法表达专属约束。插件与 Fisher-H3 同构：manifest + README + docs/interface.md，经 `generate-catalog.mjs` → `embed-documentation.mjs` → `build-packages.sh` 构建。

2. **后端零专属宿主代码**。
   密塔不需要 Fisher 式边界校验（无 audio_control/驱动音频语义），走纯声明式路径：`generation/registry.go` 注册接口名、`model_capability_defaults.go` 提供能力默认值（档位、操作类型、参考素材上限）即可，创建/轮询/下载由 `provider_protocol.go` 既有宿主完成。备选"加专属校验"被否：没有需要宿主强制的厂商扩展参数。

3. **前端新增轻量 provider `video-provider-metaso.ts`**。
   沿用 `video-provider-minimax.ts` 的结构（content[] 组装 + 查询归一），减去密塔不支持的 `aigc_watermark` 等字段；`video.ts` 增加 create/poll 两个分发行。备选"复用 minimax provider 改 Base URL"被否：provider 标识会混淆任务归属与轮询分发。

4. **素材公网 URL 直接透传**。
   插件声明 `requiresPublicMediaUrls: true`，参考图片/视频/音频在上传后由既有 `resolveVideoReferenceMediaUrl` 链路解析为公网 URL；与 Fisher-H3/MiniMax 一致，不做额外转存。

## Risks / Trade-offs

- [密塔查询响应结构与官方有细微差异] → 响应映射使用与官方插件相同的 `$coalesce` 兜底链（`task.content.url`、`task.error.message`、`base_resp.status_msg` 等），并用真实 Key 冒烟验证查询端点（用户已提供查询示例确认路径）。
- [密塔对 `reference_image` 角色的支持未在用户示例中出现] → 第一期仅按已确认的三种模式构建；`reference_image` 保留在 role 映射中，真实渠道验收时确认，不支持则由上游报错呈现。
- [adaptive 档位日后开放] → 档位集中在插件 manifest 校验 + `model_capability_defaults.go` 一处数组，加一档是纯数据改动。
- [免费/低价中转的限流与稳定性未知] → 沿用宿主既有超时、轮询与"结果不确定"错误语义，不自动重发付费任务。

## Migration Plan

纯新增，无数据迁移。发布即随镜像携带新 `.yingce-plugin` 包，启动时 `bootstrapBuiltInPlugins` 自动注册；回滚仅需移除插件包与注册点。

## Open Questions

- 密塔对 `reference_image`（纯参考图，无首帧）的实际支持度——留待真实渠道验收确认，不阻塞本期。
