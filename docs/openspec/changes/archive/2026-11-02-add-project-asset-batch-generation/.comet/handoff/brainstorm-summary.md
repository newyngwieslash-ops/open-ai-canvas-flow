# Brainstorm Summary

- Change: add-project-asset-batch-generation
- Date: 2026-11-02

## Confirmed Technical Approach

1. **双表持久批次模型（Schema V62 `project_asset_generation_batches`）**：
   - `ProjectAssetGenerationBatch`（表 `project_asset_generation_batches`）：`id`、`user_id`、`project_id`、`idempotency_key`、`request_hash`、`status`（`pending`/`running`/`completed`/`partial_failed`/`stopped`/`reconcile_required`）、`stop_requested_at`、`created_at`、`updated_at`；唯一索引 `(user_id, project_id, idempotency_key)`。
   - `ProjectAssetGenerationBatchItem`（表 `project_asset_generation_batch_items`）：`id`、`batch_id`、`position`、`asset_id`、`asset_category`、`base_version_id`、`reference_source`、`input_snapshot_json`（仅保存脱敏安全快照与引用的 `resourceId` 列表，绝不落盘明文上游 API Key）、`status`（`pending`/`running`/`succeeded`/`failed`/`stopped`/`cancel_requested`/`cancelled`/`reconcile_required`）、`task_id`、`attempt`、`submission_key`（唯一索引）、`attempt_history_json`、`error_reason`、`error_message`、`created_at`、`updated_at`；唯一索引 `(batch_id, asset_id)` 与 `(batch_id, position)`。
2. **预检、幂等提交与恢复 API**：
   - `POST /api/projects/:id/asset-generation-batches/preflight`：校验最多 `MaxProjectAssetBatchItems = 50` 项无重复、归属当前项目、版本 CAS（`expectedVersionId == asset.PrimaryVersionID`）、参考来源（`original` 复用有序 `referenceImages`、`current`、`none`）、模型参考图上限与格式预算，计算已知费用估算或标记 `priceUnknown: true` 并生成 `preflightDigest`。
   - `POST /api/projects/:id/asset-generation-batches`：携带 `idempotencyKey`、`preflightDigest` 与逐项清单；同键同 `requestHash` 幂等返回已有批次，同键异参返回 `409 Conflict`；任一资产版本、价格或能力变化返回 `409`/`400` 整批拒绝，不静默跳过。
   - `GET /api/projects/:id/asset-generation-batches`（最近活动/历史批次列表）与 `GET /api/projects/:id/asset-generation-batches/:batchId`（聚合最新 `Task` 状态、候选 `assetImageCandidate` 与取消进展）。
3. **原子绑定、部分失败重试与停止/取消分离**：
   - 逐项派发通过 `submissionKey` 与 `Task.CreationSubmissionID` 原子关联，防止断线重放或跨实例重复扣费。
   - `POST /api/projects/:id/asset-generation-batches/:batchId/stop`：仅将 `status = "pending"` 的未提交项条件更新为 `stopped`，不影响已提交 `taskId` 的项。
   - `POST /api/projects/:id/asset-generation-batches/:batchId/cancel-tasks`：对已绑定 `taskId` 的活动任务调用既有任务取消流程，如实反映 `providerCancelStatus`（`cancel_requested` vs `cancelled`），不伪造已取消。
   - `POST /api/projects/:id/asset-generation-batches/:batchId/items/:itemId/retry`：仅当原任务明确 `failed` 且无成功候选、无待核对账单时允许递增 `attempt` 重试，保留历史 `taskId` 审计记录；状态不确定项标记 `reconcile_required` 禁止盲目重发。
4. **资源引用保护与前端真实批量工作流**：
   - 扩展资源删除引用扫描覆盖 `project_asset_generation_batch_items.input_snapshot_json`（未停止/活动项）；停止后的未提交项解除批次独占引用，但资产版本历史引用仍受保护。
   - 前端 `web/src/pages/projects/detail/assets.tsx` 新增「批量生成图片」入口与 `ProjectAssetBatchModal`，支持角色/场景/道具多选、清单预检、费用边界确认、幂等提交、刷新/重进自动恢复、停止未提交项、取消运行中任务、单项重试以及逐项调用既有单候选预览与手动采用（`adoptProjectAssetImageCandidate`）。

## Key Trade-offs and Risks

- **两表模型 vs 单 JSON 字段**：两表模型支持 `(batch_id, asset_id)` 与 `submission_key` 数据库级唯一约束及条件状态更新，避免并发派发与停止竞争时的整包覆写。
- **停止未提交 vs 取消已提交分离**：明确区分零上游开销的“停止排队项”与可能产生真实上游调用的“请求取消运行中任务”，杜绝虚假取消承诺。

## Testing Strategy

- **后端**：SQLite + PostgreSQL 测试覆盖 Schema V62 迁移、同键幂等/异参冲突、预检阻断、派发与 `stop` 并发竞争、`cancel-tasks` 状态透传、部分失败重试资格、`reconcile_required` 保护及资源删除引用保护。
- **前端**：Bun 组件与 API 测试覆盖多资产选择、预检弹窗、幂等提交、断线/刷新恢复、停止/取消分离、逐项手动采用及 CAS 冲突保留候选。

## Spec Patches

None
