# Comet Design Handoff

- Change: add-project-asset-batch-generation
- Phase: design
- Mode: compact
- Context hash: 94a5c65975e9687c0a2f5698da09567dc0f2bf3de6e5fd5dc90e78205550a732

Generated-by: comet-handoff.sh
Task hash policy: task-content-v1. Read tasks.md for live completion; excerpts are design-time context.

OpenSpec remains the canonical capability spec. This handoff is a deterministic, source-traceable context pack, not an agent-authored summary.

## docs/openspec/changes/add-project-asset-batch-generation/proposal.md

- Source: docs/openspec/changes/add-project-asset-batch-generation/proposal.md
- Lines: 1-28
- SHA256: 624cd3a4bb12d715e733686d22ac492ffdc790e7fe5d1ac6c701e8890c46d205

```md
# Proposal

## Why

逐个为项目角色、场景和道具提交图片任务操作重复，浏览器临时分组无法保证刷新、断线与重复提交后的批次一致性。需要服务端可恢复的批次合同，而非前端循环请求。

## What Changes

- 批量明确为多个资产各一个逻辑生成项、单任务单候选；不做同资产多方案，不自动采用。
- 提交前展示选中清单、基准版本、参考来源、模型能力和费用/未知费用说明；服务端重验并幂等持久化批次。
- 展示逐项状态、整体进度、部分成功和失败恢复；安全重试仅针对已确定可重试项，费用或提交状态不确定项先对账。
- 分开“停止未提交项”和“取消已提交任务”，保留成功候选供逐项手动采用。
- 非目标：新建生成引擎、绕过额度、全批次自动采用、跨项目批次、浏览器分组充当服务端记录。

## Capabilities

### New Capabilities
- `project-asset-batch-generation`: 项目多个资产单候选生成的预检、幂等批次提交、恢复与取消。

### Modified Capabilities
无；沿用既有成品绑定与单任务合同。

## Impact

- 硬依赖首版 `add-project-reference-image-assets`。排在多参考变更之后，但多参考是可接入输入能力，不是批次持久化的硬依赖。
- 涉及项目资产选择 UI、API、任务提交/取消/队列/额度、repository 与最小持久批次模型。建议新增 batch 与 item 两张表，字段和迁移同步 backend-database.mdx。
- 核心验收：多个资产各一张、预检阻断、服务端重复提交不重复收费、重启恢复、部分失败、停止与取消分离、CAS 采用及资源保护。
- 两表模型、默认批次数量安全界限与费用未知提示为待批准设计建议；本轮仅规划，无 DDL/真实付费调用/产品测试，内容待统一确认。

```

## docs/openspec/changes/add-project-asset-batch-generation/design.md

- Source: docs/openspec/changes/add-project-asset-batch-generation/design.md
- Lines: 1-50
- SHA256: 5079f8735aefd829fc31031639842c91bccfdc96d9cf7f7f54861a2a2a41fc4f

```md
# Design

## Context

动机见 proposal.md。`web/src/services/api/generation-task.ts` 的 count/batchIndex 只是多任务客户端展开，不提供服务端批次。`backend/internal/model/models_task.go` 已有 CreationSubmissionID、任务租约、取消来源、ProviderCancelStatus、费用订单与恢复字段，可复用执行和对账，不能因此假定已有批次合同。首版已有 manual 单候选和采用 CAS。

以下为待批准规划，Comet 保持 open；不执行数据库迁移、停任务或付费调用。

## Goals / Non-Goals

**Goals:** 原子记录清单、逐项提交身份和费用边界，复用既有任务引擎；单项失败不回滚其他成功候选。

**Non-Goals:** 不实现第二套 worker/计费；不把一次请求包装成一个多输出任务；不做同资产多个并行方案。

## Decisions

### 1. 最小持久批次模型
建议新增 `project_asset_generation_batches`（id/user/project/idempotencyKey/requestHash/status/stopRequestedAt/timestamps）和 `project_asset_generation_batch_items`（id/batchId/position/assetId/baseVersionId/inputSnapshot/status/taskId/attempt/submissionKey/errorReason/timestamps）。唯一约束包括 user+project+idempotencyKey、batch+asset、batch+position、当前 submissionKey；taskId 关联真实任务。输入快照只含安全引用与配置标识，凭据沿现有加密任务机制，不明文落批次 JSON。

单一 batch JSON 更少表，但难以跨实例领取、条件更新和逐项唯一约束；仅 Task metadata 无法表示尚未提交或停止的项。因此采用两表，不另建候选表。DDL、索引、保留/删除策略必须同步 backend-database.mdx。

### 2. 预检与提交 API
建议 `POST /projects/:id/asset-generation-batches/preflight` 返回逐项摘要、版本/能力/计价快照与有效期；`POST /projects/:id/asset-generation-batches` 接收同一清单、确认快照和幂等键；`GET .../:batchId` 返回安全分页项状态。预检不收费，提交重验全清单；不合格整批拒绝，用户可删去失败项后重新确认，不能隐式部分启动。

建议首版每批最多 50 项作为工程安全上限（非用户指定；可随内容审视调整），运行并发仍用现有用户/系统配额而非一次发出 50 请求。成本已知展示估算与授权边界，未知明确提示而非填写零；价格/版本变化要求重新确认。多参考已落地时调用其准备函数，否则沿首版单参考合同，不能绕过能力检查。

### 3. 幂等派发与部分恢复
事务创建 batch/items 后由现有调度入口领取待提交项；按 item 当前 attempt 生成稳定 submissionKey，并复用/映射现有 CreationSubmissionID。任务创建与 item.taskId/状态绑定须处于同一数据库提交边界，或使用可查询的唯一提交记录保证恢复可关联；不得先调用收费上游再写 taskId。任务 worker 仍负责真正上游执行、额度和租约。

item 的执行状态由任务事实驱动，batch 进度由逐项聚合，不能由前端计数“假完成”。响应丢失查 submissionKey；上游结果/费用不确定标为待核对并调用现有恢复/对账能力，不自动开新尝试。显式失败重试仅在确认旧任务无成功候选且费用已结清/可确定后递增 attempt，新映射保留旧 taskId 审计历史；同一逻辑项最多一个活跃任务。任务重试与恢复不等于新增资产方案。

### 4. 停止、取消与采用
建议 `POST .../:batchId/stop` 原子停止未提交项；领取与 stop 使用相同 batch/item 条件事务，已提交项保留 taskId。`POST .../:batchId/cancel-tasks` 单独将已提交活动任务转现有取消请求，并逐项报告确认结果；不许把不支持取消写成已取消。成功项继续可审阅。采用完全复用首版接口，不做 batch adopt，旧版本冲突不触发自动生成。

资源保护覆盖 pending item 安全输入快照、实际任务及候选；停止未提交项后按明确保留策略解除独占输入引用，但历史资产引用仍保护资源。错误通过 reason 和安全文案，避免批次摘要泄露上游凭据。

## Risks / Trade-offs

- [批次原子确认不保证后来额度充足] → 每项派发仍由既有额度事务检查，后续不足呈现部分失败/待处理，不超支。
- [跨实例重复领取] → 数据库唯一提交身份和条件状态迁移；Redis 只协调，不作唯一事实。
- [停机时无法判断已收费] → 待核对状态阻止自动重发，用户可见真实不确定性。

## Migration Plan

先新增两表和索引、专项测试及数据库文档，再接 API/队列，再真实项目批量 UI。部署迁移另需环境授权；关闭新批次入口可回退，保留表与可恢复映射，不能在有活动批次时直接删表。历史单任务无需回填为批次。

## Open Questions

- 逐项进度使用轮询还是复用现有事件通知可在实现时按现有任务中心负载选择；持久 GET 恢复必须独立可用。
- 批次列表保留期和分页默认值随现有任务保留策略收敛，活动映射和未采用候选不能被提前清理。

```

## docs/openspec/changes/add-project-asset-batch-generation/tasks.md

- Source: docs/openspec/changes/add-project-asset-batch-generation/tasks.md
- Lines: 1-26
- SHA256: 72e39443ffb16c9f7f049fbebbfc4a392144809bc9e3ffbeb13ec0c8b5404d8f

```md
# Tasks

仅规划；多个资产各单任务单候选，不执行迁移、产品测试或收费生成。

## 1. 持久合同与预检

- [ ] 1.1 新增 batch/item 模型、唯一索引及迁移；SQLite/PostgreSQL 测试验证同键重放/异参冲突、重复资产和安全快照，同步 backend-database.mdx。
- [ ] 1.2 实现预检/提交/恢复 API 与整清单重验；测试覆盖权限、基准、能力、额度、费用未知及价格变化，并同步 HTTP API 和错误 reason。
- [ ] 1.3 实现安全输入快照与资源引用保护；资源测试覆盖 pending 项删除竞争、停止后清理和历史资产保留，核对不保存明文密钥。

## 2. 派发恢复与取消

- [ ] 2.1 复用队列/额度并原子绑定 item 与唯一任务提交身份；并发及故障注入测试覆盖重复领取、响应丢失、重启和禁止重复收费。
- [ ] 2.2 实现状态聚合、部分失败重试资格和待核对状态；测试覆盖部分成功、费用不确定禁止重发、重试历史及每项最多一活跃任务。
- [ ] 2.3 分离停止未提交与取消已提交 API；竞争测试覆盖 stop/领取边界、取消不支持/未确认、迟到完成和成功候选保留，同步取消语义文档。

## 3. 真实批量界面

- [ ] 3.1 实现真实项目资产多选、清单预检、费用确认和幂等提交；组件/API 测试覆盖三分类、重复点击、无效项、模型不支持及版本过期。
- [ ] 3.2 实现持久批次进度/恢复、逐项错误/重试与两种取消操作；组件测试覆盖刷新、换账号、部分成功及不确定项，更新功能/pending-test。
- [ ] 3.3 接入既有单候选预览采用；测试确认无批量自动采用、CAS 冲突保留候选、采用重放不重复版本且声音/参考不变。

## 4. 跨层验收

- [ ] 4.1 在真实页面用可控任务 mock 验证三个资产各一候选、部分失败、断线重载、停止/取消及手动采用，记录明暗主题/滚动/键盘证据；无收费授权不调真实模型。
- [ ] 4.2 执行最小相关测试与必要类型/构建检查，演练数据库迁移及跨实例故障恢复；记录实际结果和费用对账未覆盖项，不宣称未执行的端到端通过。

```

## docs/openspec/changes/add-project-asset-batch-generation/.openspec.yaml

- Source: docs/openspec/changes/add-project-asset-batch-generation/.openspec.yaml
- Lines: 1-2
- SHA256: 0ad492a63d87e00a8bc135b0a1a2b19270dbb9ffd997a69b89fd4ab72015a944

```md
schema: spec-driven
created: 2026-10-22

```

## docs/openspec/changes/add-project-asset-batch-generation/specs/project-asset-batch-generation/spec.md

- Source: docs/openspec/changes/add-project-asset-batch-generation/specs/project-asset-batch-generation/spec.md
- Lines: 1-58
- SHA256: 832e9f9efc75bb03d8c7f67da54a4257985b985306446e25f17ab4a8ce255d60

```md
# Spec Delta

## Purpose

为项目多个角色、场景和道具提供可追踪、可恢复的单候选批量生成，明确提交、收费、停止和取消边界；让部分失败不损失成功结果，防止断线重放导致重复任务或费用，并保持用户逐项采用的控制权。

## ADDED Requirements

### Requirement: 清单预检和确认
系统 SHALL 在提交前显示每项资产、版本、参考来源、模型能力与费用说明，预检项目归属、重复资产、输入有效性及额度；费用未知必须明确标识。提交时 MUST 再验证，存在不合格项不得悄悄跳过后启动其余项。

#### Scenario: 预检成功
- **WHEN** 用户选择三个合法资产并确认清单
- **THEN** 显示三个单候选项与费用边界，确认后才允许服务端提交

#### Scenario: 越权或预检过期
- **WHEN** 清单含越权资产、重复资产、伪造能力，或确认后版本/价格授权已变化
- **THEN** 提交拒绝并指出可安全公开的原因，不能先收费再提示预检失败

### Requirement: 服务端持久批次和幂等
系统 SHALL 持久记录用户、项目、清单、幂等键与逐项任务映射；每个资产的逻辑项仅产生一个当前生成任务和一个候选，同键同内容返回原批次，同键不同内容返回冲突。

#### Scenario: 提交响应丢失
- **WHEN** 服务端接受批次但客户端未收到响应，随后重放相同请求
- **THEN** 返回原批次和既有任务，不创建第二批任务或重复扣费

#### Scenario: 断线和服务重启
- **WHEN** 批次部分任务已提交后浏览器断线或服务重启
- **THEN** 从持久状态恢复同一批次与任务映射，不依赖浏览器分组或重新发起全部任务

### Requirement: 逐项进度与部分失败恢复
系统 SHALL 展示待提交、运行、成功、失败、停止、取消请求中、取消及待核对状态与可追踪原因；成功候选独立保留，不自动采用。只允许对已确认未成功且费用边界明确的失败项显式重试，不能盲目重发提交或费用不确定项。

#### Scenario: 部分成功
- **WHEN** 三项中一项成功、一项明确失败、一项上游提交结果不确定
- **THEN** 成功候选可审阅，失败项可按资格显式重试，不确定项进入核对且无自动重发

### Requirement: 停止未提交和取消已提交分离
系统 SHALL 提供停止未提交项与取消已提交任务两个明确操作；停止不声称撤销已经提交的费用，取消请求须追踪真实上游结果，成功项不被删除。

#### Scenario: 停止与提交竞争
- **WHEN** 用户停止批次时某项正与任务创建竞争
- **THEN** 该项原子地归为未提交停止或已有 taskId 的已提交状态，不能丢失映射，也不能被停止后重新派发

#### Scenario: 取消不被上游确认
- **WHEN** 用户取消已提交任务但上游不支持或尚未确认
- **THEN** 显示取消请求真实状态与费用说明，不假报已取消或触发补发任务

### Requirement: 单项采用和安全生命周期
系统 SHALL 按既有单任务候选合同逐项采用，检查归属、基准 CAS 和幂等，保留定义、声音和参考；批次和任务引用存在时保护输入/输出资源。错误不得暴露密钥、敏感 URL 或其他用户资产。

#### Scenario: 成功候选冲突和采用重放
- **WHEN** 成功项的资产版本已变化，或已采用请求被再次提交
- **THEN** 旧基准返回冲突并保留候选，相同成功采用幂等返回，不自动覆盖或重复生成

#### Scenario: 历史批次资源删除
- **WHEN** 请求删除仍被批次有效任务、候选或历史资产版本引用的资源
- **THEN** 删除被阻止并返回安全来源提示；取消/失败清理不能破坏其他项或已采用资产

```
