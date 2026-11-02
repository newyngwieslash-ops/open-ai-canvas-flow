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
