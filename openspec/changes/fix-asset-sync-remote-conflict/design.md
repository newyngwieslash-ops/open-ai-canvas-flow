# Design

## Context

见 proposal.md — Why。同步模块现状：`acknowledgedAssets`（基线快照）只在登录快照、`acceptRemoteAssets`（远端拉取）、通用 upsert 成功后、force 覆盖四处更新；角色域接口响应没有进入其中任何一处。校验在 `saveRemoteUserDataBatch` 中先于所有推送执行，抛错即中断整批。

## Goals / Non-Goals

**Goals:**
- 角色域服务端写入（已通过响应投影回流到 store）不再被误判为远端冲突。
- entity 资产冲突判定基于单调版本 `primaryVersionId`，不依赖纳秒时间戳字符串相等。
- 单素材校验失败只隔离该素材，其余素材与画布照常推送。
- entity 投影不再经通用 `PUT /assets/:id` 覆盖服务端角色 payload。

**Non-Goals:**
- 不改后端任何接口/数据结构。
- 不重构通用媒体素材同步与 `acceptRemoteAssets` 的现有语义。
- 不实现"隔离素材"的自动恢复 UI（本轮仅保证不阻断，恢复手段仍是刷新页面重建基线）。

## Decisions

1. **新增 `acknowledgeRemoteAssets(assets: Asset[])` 而非复用 `acceptRemoteAssets`**。
   `acceptRemoteAssets` 带"本地已脏则跳过"逻辑，语义是拉取合并；本场景调用方刚从服务端权威响应构建了本地实体，必须无条件采纳为基线，两者语义相反。入口内部：`acknowledgedAssets.set(id, asset)` + `verifiedAssets.add(id)`。

2. **entity 校验比较 `primaryVersionId`，其他 kind 维持 updatedAt 比较**。
   角色每次版本化操作（`SaveCharacterVersion`）必换 `primaryVersionId`，是服务端权威单调版本。实现：基线与服务端记录都存在 `primaryVersionId` 时比较它；任一方缺失时回退 updatedAt 比较（容忍旧数据）。仅对 `kind === "entity"` 启用该分型；通用媒体的服务端列更新不写 payload 内嵌时间戳，现状安全，不引入行为变化。

3. **校验失败隔离改为"跳过该素材 + 上抛聚合提示"**。
   原：`throw new Error("素材远端版本已变化…")` 中断一切。改为：收集校验失败的素材 ID，跳过它们与其后续 upsert，其余 dirtyAssets/dirtyProjects 正常推送；若存在失败项，推送完成后抛出明确的聚合错误（含素材名），让 UI 提示而非静默。画布保存（dirtyProjects 为空、仅素材失败时）返回成功语义不变——调用方拿到的是"画布已保存，素材 X 需要处理"。

4. **entity 跳过通用 upsert，推送循环中直接 `acknowledge` 并告警**。
   角色投影（`syncPersonalCharacterProjection` 产物）与后端 `characterAssetPayload` 格式不一致（缺 representations 元数据等），经 `PUT /assets/:id` 会用列表投影覆盖服务端完整 payload；此前靠 domain 表兜底。改为：dirtyAssets 推送循环遇到 `kind === "entity"` 时不再调用 `upsertRemoteAsset`，记录 console.warn（entity 由角色域接口权威维护），并 acknowledge 本地投影（与基线对齐，避免下轮反复判脏）。

## Risks / Trade-offs

- [entity 投影 acknowledge 后，若服务端角色操作与本地编辑真实并发冲突，不再有"停止覆盖"保护] → 可接受：投影本身来自服务端最新响应，本地不提供角色 payload 编辑通道（角色编辑全部走角色域接口）。
- [primaryVersionId 回退 updatedAt 时旧数据仍可能误报] → 回退路径仅覆盖未迁移的历史 entity；下一次角色域操作会带入新版本 ID 自愈。
- [聚合错误文案变化] → 保留原文案子串"素材远端版本已变化"便于既有测试/用户认知延续。

## Migration Plan

纯前端行为修复，无数据迁移。部署即生效；已"中毒"的会话需刷新页面（与现状一致）。

## Open Questions

无。
