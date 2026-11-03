# Comet Design Handoff

- Change: add-character-turnaround-splitting
- Phase: design
- Mode: compact
- Context hash: 888c8e89e046112c9bd026e80c6e0dcbb7517d765dccc6fcebf40bff00f4a982

Generated-by: comet-handoff.sh
Task hash policy: task-content-v1. Read tasks.md for live completion; excerpts are design-time context.

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## docs/openspec/changes/add-character-turnaround-splitting/proposal.md

- Source: docs/openspec/changes/add-character-turnaround-splitting/proposal.md
- Lines: 1-28
- SHA256: 3df4bb977db80bd5bfe01e4ed6e6a0e640ed1024654523e14b850e5879f4eb3b

```md
# Proposal

## Why

已有同一角色正侧背整图不能方便地作为独立视角查看和使用。需要将“识别建议”与“原图裁切”分开，让用户确认框和标签后保存真实像素，而不是让生成模型补出不存在的视角。

## What Changes

- 对已保存的同角色整图进行视觉识别，建议 front/side/back 裁框和标签；所有建议须用户确认或调整。
- 从 owned ready 源资源原像素裁切，安全保存三个角色表现；原整图、primary、声音、定义和其他既有表现保持不变。
- 缺视角、多人、严重重叠或不可判断时转人工，不伪造框、置信度或视角；不能确认完整三视图时不自动提交三张。
- 非目标：从单人像生成缺失侧背面、背景补画、改变全局默认引用策略、自动把拆分三图送入下游生成。

## Capabilities

### New Capabilities
- `character-turnaround-splitting`: 角色整图的视角裁框审阅、原像素裁切及版本化保存。

### Modified Capabilities
无；不改变既有通用成品绑定及角色默认引用行为。

## Impact

- 依赖首版角色原图/成品角色区分及 expectedVersionId；按确认顺序第四项，可独立于多参考和批次实现。
- 涉及真实角色详情/裁框组件、多模态文本任务、服务端安全图片处理、资源持久化和角色版本提交。
- 现有浏览器 crop 可用于预览，但正式保存建议服务端读取 owned ready 源图裁切，避免信任客户端重传图片。服务端裁切与原子提交方案随文档待确认。
- 核心验收：正确识别→人工调整→三图保存；越权、伪造框/源、缺视角、失败/取消/重放/CAS、像素限制、原图保留及下游优先级回归。
- 实施时同步 HTTP API、资源/数据库 JSON 说明与 pending-test；本轮仅规划，不执行真实识图收费调用或产品测试，状态保持 open。

```

## docs/openspec/changes/add-character-turnaround-splitting/design.md

- Source: docs/openspec/changes/add-character-turnaround-splitting/design.md
- Lines: 1-51
- SHA256: 413aee827f1df7d565c377018c318721c58a374228de0f6e43704dfd0ccb7479

```md
# Design

## Context

动机见 proposal.md。`canvas-image-data.ts:13–42` 已有浏览器裁切，适合即时预览但不能证明上传成品来自被授权源图；本次调查未发现通用服务端 crop。`project_character.go:350–387` 的 ReplaceProjectCharacterRepresentations 接收的是整组表现，直接提交三张会丢掉整图/primary/表情等。`canvas-character-reference.ts:38` 和 `cloud_agent_character.go` 保持 turnaround_sheet→primary→front。

本文件为 Open 阶段待批准建议，不是完成实施或 design check 的证据。

## Goals / Non-Goals

**Goals:** 识图建议不具有写权限；正式输出可从源像素复现，资源与角色版本一致生效。

**Non-Goals:** 不做新视角生成、背景补画、通用在线图像编辑器或全局引用策略重构。

## Decisions

### 1. 识别与人工确认分离
建议 `POST /projects/:id/characters/:assetId/turnaround-split-plans` 接收 expectedVersionId、源 resourceId 和识图模型。服务端校验源属于该角色基准版本的明确整图来源；普通 portrait 不默认当三视图。复用 text+referenceImages 任务，只将 schema 校验后的标签/框/缺失原因暴露为草稿。模型不能识别就留空/转人工，无伪造 confidence。

前端在真实角色详情打开裁框组件，显示原图、三个标签、坐标和逐框确认；允许修正建议或完全手工框选。缺任一真实视角时停留人工状态，不强迫凑齐，不执行保存。模型识别取消使用既有任务取消，关闭弹窗不等于取消收费任务。

### 2. 服务端源图裁切，浏览器仅预览
选择服务端读取资源裁切，而非信任客户端裁切后上传；后者虽然复用代码更多，却无法验证来源与像素对应。建议首版只支持静态 PNG/JPEG，输出无损 PNG；拒绝 SVG、动画和未知编码。先读头验证格式、宽高、字节与像素预算，再解码；建议初始限制源 32 MiB、40 MP、最长边 16,384，三框总像素不超过源像素，均是需随方案批准的安全预算，不是用户指定常数。

确认请求绑定 sourceResourceId、源实际尺寸、expectedVersionId、三个唯一标签及整数像素框；预览缩放只影响 UI，转换到原始像素后再次展示边界。JPEG 解码方向统一为既有资源显示方向（包括 EXIF），记录转换后的尺寸，客户端与服务端同一坐标系；不 resize、不补画，PNG 像素与解码后的源区域一致。非法/重叠无法独立裁切的框拒绝并回到人工；对轻微画面交叠不承诺分离遮挡内容。

### 3. 保存操作、资源暂存及原子版本提交
建议 `POST /projects/:id/characters/:assetId/turnaround-splits` 创建可取消的本地处理任务，输入幂等键与确认框摘要，复用 Task 记录/租约作为操作事实（新增本地操作类型，不走生成模型）。任务唯一创建身份可避免重放重复裁切；同键异参冲突。

处理步骤：校验拥有的 ready 源及版本→按稳定资源锁顺序确认引用→在有界工作进程裁切并写暂存对象→事务再次检查未取消、CAS、资源有效性→创建三个 ready 资源记录、合并完整现有表现、创建版本并更新任务结果。数据库提交不能与对象存储真正原子，采用“先暂存对象，后数据库原子可见”方案；提交失败不暴露资源，孤儿对象进入可追踪清理，不先删有引用记录。成功后任务结果绑定 adoptedVersionId 和资源 ID；重放先查原结果再做 CAS，避免因已成功推进版本而错误冲突。

合并不是把三张直接传给旧整组替换接口：在服务端读取现有 representations，按 front/side/back 替换，其余全部保留；声音/definition 由版本复制保留。确认后 CAS 冲突不自动重试新版本，用户重新核对。取消与最终提交共享条件状态事务，提交先胜则返回完成，不能显示已取消又偷偷保存。

### 4. 下游与资源保护
不修改默认引用函数，不将三图自动加入多参考集合。资源 metadata 记录源 ID、框、坐标尺寸、操作 ID；纳入任务/历史版本引用保护，清理仅处理明确无引用暂存物。识图与裁切错误分开记录，公共错误必须脱敏。

## Risks / Trade-offs

- [模型把多人误判同角色] → 草稿与人工确认，不能声称模型身份判断可靠；缺失/歧义转人工。
- [解码内存膨胀] → 头部/总像素/字节限制、有界并发，测试解码炸弹与格式伪装。
- [数据库与对象存储无分布式事务] → 暂存+原子引用提交+可恢复清理，失败不产生半套可见结果。
- [EXIF/预览缩放偏移] → 统一显示方向和原像素坐标，像素 fixture 验证，不依赖截图猜测。

## Migration Plan

优先复用 Task/Resource/AssetVersion，无计划新增表列；新增本地操作、资源 metadata 和 API 同步数据库与 HTTP 专题。先服务端安全裁切/事务，再 UI；关闭新入口可回退，保留原图与已提交版本。无需改写已有角色表现或迁移默认引用策略。

## Open Questions

- PNG/JPEG 解码实现优先现有依赖或 Go 标准库；需要方向处理时选择最小 helper，不扩成通用裁图平台。
- 手工框的拖拽/键盘步进与最小可操作尺寸在真实组件验收中收敛，不改变整数原像素合同。

```

## docs/openspec/changes/add-character-turnaround-splitting/tasks.md

- Source: docs/openspec/changes/add-character-turnaround-splitting/tasks.md
- Lines: 1-25
- SHA256: 4938fbdd6e7c3552911c2d38b36fec90c4dd6031ad12f52bde3c6e847ac7e2ba

```md
# Tasks

仅规划；待统一内容确认，不执行产品测试、构建或真实识图调用。

## 1. 识别和人工确认合同

- [ ] 1.1 实现角色整图识别入口、来源/版本/权限及视觉能力准入；专项测试覆盖伪造源、普通人像、无视觉模型、多人/缺视角与取消，更新 HTTP API。
- [ ] 1.2 实现标签/框 schema 和安全草稿投影；测试覆盖未知字段、非有限坐标、无效标签和不确定留空，验证不伪造置信度或自动保存。

## 2. 服务端裁切与原子保存

- [ ] 2.1 实现 owned ready 源资源读取、PNG/JPEG 头部/像素预算和方向一致化裁切；像素 fixture 测试覆盖缩放坐标、EXIF、边界、超限/格式伪装，证明没有重绘补画。
- [ ] 2.2 实现幂等本地处理任务与暂存输出；故障注入测试覆盖重放、同键异参、取消/提交竞争和对象存储失败，同步任务/API 合同。
- [ ] 2.3 实现完整表现合并及资源/版本 CAS 提交；SQLite/PostgreSQL 测试覆盖版本冲突、部分写失败、声音/定义/整图/primary/表情保留，验证旧整组接口不会丢表现。
- [ ] 2.4 实现来源 metadata、历史引用保护及孤儿清理；资源测试验证源图有引用不删、无引用暂存可回收、物理删除失败保留记录，同步 backend-database 与资源说明。

## 3. 真实角色裁框界面

- [ ] 3.1 在真实角色详情接入原图预览、建议框、标签调整、键盘移动和逐框确认；组件测试覆盖模糊/多人/缺视角转人工、取消、未确认禁用和原像素转换。
- [ ] 3.2 接入处理进度/刷新恢复/CAS/重放提示及保存后视角展示；组件/API 测试覆盖失败不半套显示，更新功能和 pending-test 文档。

## 4. 集成与默认引用回归

- [ ] 4.1 使用可控识图 mock 在真实页面完成框选→裁切→保存，检查明暗主题、滚动和键盘；像素对照证明输出来源，未获收费授权不得真实模型调用。
- [ ] 4.2 回归默认 turnaround_sheet→primary→front 与下游单图输入，执行最小专项测试及必要类型/构建检查，记录实际证据和未验证项，不把静态页面当真实验收。

```

## docs/openspec/changes/add-character-turnaround-splitting/.openspec.yaml

- Source: docs/openspec/changes/add-character-turnaround-splitting/.openspec.yaml
- Lines: 1-2
- SHA256: 0ad492a63d87e00a8bc135b0a1a2b19270dbb9ffd997a69b89fd4ab72015a944

```md
schema: spec-driven
created: 2026-10-22

```

## docs/openspec/changes/add-character-turnaround-splitting/specs/character-turnaround-splitting/spec.md

- Source: docs/openspec/changes/add-character-turnaround-splitting/specs/character-turnaround-splitting/spec.md
- Lines: 1-65
- SHA256: bf6fb2af111c1822384a0fe2e98774a9fb6912aa2b32ea164671d1a4d56a8ba3

```md
# Spec Delta

## Purpose

将已有同一角色正侧背整图安全拆成可独立查看的三张表现图，通过视觉建议和人工确认保证裁框及标签可解释；裁切必须来自原始像素，保留原整图、声音、定义与版本历史，不把缺失视角伪装成真实观察。

## ADDED Requirements

### Requirement: 受权来源和识别草稿
系统 SHALL 只对当前项目角色的 owned ready 源图进行识别，绑定来源资源、基准版本和尺寸；模型必须具备视觉输入能力。识别仅输出待审阅的 front/side/back 框及标签，不能直接创建表现或伪造置信度。

#### Scenario: 合法整图识别
- **WHEN** 用户选择角色已有整图并使用可识图模型分析
- **THEN** 展示建议框、标签和原图，不保存新表现，用户可调整后确认

#### Scenario: 伪造源或能力
- **WHEN** 请求使用他人资源、非该角色基准图、任意 URL、错误尺寸或无视觉能力模型
- **THEN** 服务端拒绝且不调用上游，不退化为猜测框

### Requirement: 歧义转人工和确认边界
系统 SHALL 在缺视角、多人、重叠或无法确定时留空/标为待确认，并允许用户手工修正；只有用户确认三个唯一有效标签和相应框后才能提交三视图保存。系统 MUST NOT 生成不存在视角、补背景或用猜测填满三框。

#### Scenario: 只有正面或多人
- **WHEN** 原图缺少侧背面或存在无法区分的人物
- **THEN** 明确提示转人工，缺失框保持未确认；用户无法确认完整三视图时不提交保存，不调用生成补图

### Requirement: 原像素裁切与输入限制
系统 SHALL 从服务端拥有且就绪的原资源读取像素，按确认的源尺寸和整数框裁切；拒绝越界、非有限数、空框、重复标签、超像素预算及不支持格式，不信任客户端上传的裁切成品。

#### Scenario: 正确裁切
- **WHEN** 用户确认三组有效框并提交
- **THEN** 输出尺寸与框对应，像素来自源图区域，不缩放重绘、不生成背景，保存来源和裁框溯源

#### Scenario: 恶意或超限输入
- **WHEN** 请求含伪造资源、超大解码尺寸、动画/矢量或越界坐标
- **THEN** 整体失败并返回安全原因，不产生可见半套表现或读取任意网络地址

### Requirement: 保留已有表现和版本原子提交
系统 SHALL 合并现有表现，仅替换用户确认的 front/side/back 角色，保留 turnaround_sheet、primary、声音、定义及其他表现；提交要求 expectedVersionId，资源记录与新版本关联必须原子生效。

#### Scenario: 已有声音和其他表现
- **WHEN** 对已有整图、primary、表情图和声音的角色保存三视图
- **THEN** 新版本同时拥有这些原有内容和新的三视图，源整图不被替换或删除

#### Scenario: CAS 或存储失败
- **WHEN** 裁切期间角色版本已变化或任一输出保存失败
- **THEN** 不推进主版本、不显示半套结果，保留原资产，未引用临时输出进入安全清理

### Requirement: 幂等取消与生命周期
系统 SHALL 持久记录保存操作身份及确认框摘要，相同请求重放不重复创建资源/版本；取消与提交有明确先后，取消成功后不能迟到保存。源图和有效输出受历史版本/任务引用保护，错误脱敏且可追踪。

#### Scenario: 重放或取消竞争
- **WHEN** 保存响应丢失后重放，或用户在提交中请求取消
- **THEN** 重放返回原操作结果；取消先成功则不提交，提交先完成则返回已完成而不假报取消

#### Scenario: 清理与引用
- **WHEN** 清理失败操作输出或请求删除被历史角色版本引用的源图
- **THEN** 仅删除无引用临时对象；源图仍受保护，物理删除失败不能先删资源记录

### Requirement: 下游默认引用不变
系统 SHALL 保持下游默认优先使用 turnaround_sheet，再 primary，再 front；新增三视图不自动变成三个模型输入。

#### Scenario: 拆分后继续生成
- **WHEN** 用户拆分成功后按既有默认角色引用发起下游操作
- **THEN** 仍按既有优先级选择单一默认图，不自动传入 front/side/back 三图

```
