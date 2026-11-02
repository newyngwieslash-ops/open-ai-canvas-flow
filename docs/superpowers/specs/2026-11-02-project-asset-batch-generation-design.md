---
comet_change: add-project-asset-batch-generation
role: technical-design
canonical_spec: openspec
archived-with: 2026-11-02-add-project-asset-batch-generation
status: final
---

# 项目多个资产单候选批量生成技术设计文档

## 1. 概述与架构边界

本设计深化 `docs/openspec/changes/add-project-asset-batch-generation/design.md`，为项目内的角色（`character`）、场景（`environment`）与道具（`prop`）提供服务端可追踪、可恢复、幂等防重扣的单候选批量生成闭环：

- **单资产单逻辑项单候选**：同一批次内每个选中资产仅对应一个逻辑生成项（`count = 1`、`adoptionMode = "manual"`），生成成功后产出一个待确认候选（`assetImageCandidate`），不自动覆盖资产主版本，不创造第二套任务队列或计费引擎。
- **预检与整批原子重验**：提交前经 `preflight` 返回逐项基准版本、参考来源（`original` 有序多图、`current` 当前成品、`none` 无参考）、模型能力校验与费用估算（或 `priceUnknown: true` 显式标记）；正式提交时整批重验，任一资产越权、重复、能力不符或版本/价格摘要变化均整批拒绝，绝不静默跳过部分项。
- **服务端持久幂等与安全快照**：通过数据库级 `(user_id, project_id, idempotency_key)`、`(batch_id, asset_id)`、`(batch_id, position)` 与 `submission_key` 唯一约束保证断线重放、多实例并发领取不重复创建任务或重复扣费；批次快照仅保存脱敏参数与资源引用 ID，不落盘明文 API Key。
- **停止未提交与取消已提交分离**：`stop` 仅原子停止 `pending` 未提交项；`cancel-tasks` 仅针对已关联 `taskId` 的活动任务发起取消并如实展示上游取消确认状态（`cancel_requested` / `cancelled`）。

---

## 2. 数据库模型与迁移（Schema V62）

在 `backend/internal/model/models_project.go`（或独立模型文件）新增两张表，并在 `backend/internal/database/migrations.go` 新增迁移版本 `62`（`project_asset_generation_batches`）：

### 2.1 `ProjectAssetGenerationBatch`（表 `project_asset_generation_batches`）

| 字段 | 类型 | 约束与说明 |
| --- | --- | --- |
| `ID` | `string` (`size:36`) | 主键 |
| `UserID` | `string` (`size:36`) | 所属用户；联合唯一索引 `idx_pag_batches_user_project_idem` (`user_id, project_id, idempotency_key`) |
| `ProjectID` | `string` (`size:80`) | 所属项目；索引 `idx_pag_batches_project_created` (`project_id, created_at`) |
| `IdempotencyKey` | `string` (`size:120`) | 客户端提交幂等键 |
| `RequestHash` | `string` (`size:64`) | 规范化请求清单与配置 SHA-256 摘要（同键异参返回 `409 Conflict`） |
| `PreflightDigest` | `string` (`size:64`) | 预检快照摘要（绑定每项 `baseVersionId`、参考图列表、模型能力与计价快照） |
| `Status` | `string` (`size:32`) | 聚合状态：`pending` / `running` / `completed` / `partial_failed` / `stopped` / `reconcile_required` |
| `ConfigSummaryJSON` | `string` (`type:text`) | 脱敏模型/渠道/尺寸摘要 JSON（不含密钥与敏感头部） |
| `TotalKnownMicrocredits` | `int64` | 已知计费总额（微积分） |
| `PriceUnknownCount` | `int` | 价格未知项数量 |
| `StopRequestedAt` | `*time.Time` | 用户请求停止未提交项的时间戳 |
| `CreatedAt` / `UpdatedAt` | `time.Time` | 时间戳 |

### 2.2 `ProjectAssetGenerationBatchItem`（表 `project_asset_generation_batch_items`）

| 字段 | 类型 | 约束与说明 |
| --- | --- | --- |
| `ID` | `string` (`size:36`) | 主键 |
| `BatchID` | `string` (`size:36`) | 所属批次；联合唯一索引 `idx_pag_batch_items_batch_asset` (`batch_id, asset_id`) 与 `idx_pag_batch_items_batch_pos` (`batch_id, position`) |
| `UserID` | `string` (`size:36`) | 冗余所属用户，便于按用户直接扫描资源引用 |
| `ProjectID` | `string` (`size:80`) | 冗余所属项目 |
| `Position` | `int` | 批次内稳定序号（`0..N-1`） |
| `AssetID` | `string` (`size:36`) | 目标资产 ID |
| `AssetName` | `string` (`size:160`) | 提交时资产名称快照 |
| `AssetCategory` | `string` (`size:32`) | `character` / `environment` / `prop` |
| `BaseVersionID` | `string` (`size:36`) | 提交时锁定的资产主版本 ID |
| `ReferenceSource` | `string` (`size:24`) | `original` / `current` / `none` |
| `InputSnapshotJSON` | `string` (`type:text`) | 安全输入快照 JSON（含 `prompt`、`operation`、`referenceResourceIds`、脱敏 `config`；严禁包含 `apiKey` / 认证头） |
| `Status` | `string` (`size:32`) | `pending` / `running` / `succeeded` / `failed` / `stopped` / `cancel_requested` / `cancelled` / `reconcile_required` |
| `TaskID` | `string` (`size:36;index`) | 当前绑定的生成任务 ID |
| `Attempt` | `int` | 当前尝试次数（初始为 `1`，显式重试递增） |
| `SubmissionKey` | `string` (`size:64;uniqueIndex`) | 当前尝试唯一提交键（`sha256(batchId + ":" + itemId + ":" + attempt)` 前缀格式化为 36 字符稳定 ID，同步作为 `Task.CreationSubmissionID`） |
| `AttemptHistoryJSON` | `string` (`type:text`) | 历史尝试记录 JSON（`[{ attempt, taskId, status, errorReason, updatedAt }]`） |
| `KnownMicrocredits` | `int64` | 该项已知预估费用 |
| `PriceUnknown` | `bool` | 该项是否价格未知 |
| `ErrorReason` | `string` (`size:80`) | 机器可读错误原因 |
| `ErrorMessage` | `string` (`type:text`) | 脱敏用户可读错误说明 |
| `CreatedAt` / `UpdatedAt` | `time.Time` | 时间戳 |

---

## 3. API 合同与核心业务流

### 3.1 清单预检：`POST /api/projects/:id/asset-generation-batches/preflight`

- **入参**：`items`（`[{ assetId, expectedVersionId, referenceSource, prompt, operation }]`，长度 `1..50`）与共享生成参数（`provider`、`model`、`logicalModelId`、`routeId`、`channelModelId`、`config`、`capabilityConfig`）。
- **校验与计算**：
  1. 校验项目归属、`1 <= len(items) <= MaxProjectAssetBatchItems (50)`、无重复 `assetId`。
  2. 对每一项调用与单资产生成一致的校验逻辑（资产存在且属于项目、分类支持生图且与 `operation` 匹配、`expectedVersionId == asset.PrimaryVersionID`、按 `referenceSource` 解析有序 `referenceImages` 或 `current` 成品图、校验 `min(16, maxImages)` 及图片格式/字节预算）。
  3. 计算每项计费报价：若命中系统计费路由则返回精确 `knownMicrocredits` 并检查用户当前可用余额是否足以覆盖全部已知费用项；若为自定义渠道或未配置固定价格则标记 `priceUnknown: true`。
  4. 计算包含所有项 `(assetId, baseVersionId, referenceSource, referenceResourceIds, model, knownMicrocredits, priceUnknown)` 的确定性 `preflightDigest` 并返回预检详情。

### 3.2 幂等提交与派发：`POST /api/projects/:id/asset-generation-batches`

- **入参**：在预检入参基础上增加 `idempotencyKey`（必填）与 `preflightDigest`（必填）。
- **幂等与重验**：
  1. 计算规范化 `requestHash`（不含瞬态凭据值，但绑定模型、渠道、清单与参数）。
  2. 查询 `(user_id, project_id, idempotency_key)`：若已存在且 `RequestHash` 一致，直接同步最新任务状态并返回已有批次（HTTP 200）；若 `RequestHash` 不一致，返回 `409 Conflict`（`reason: "batch_idempotency_conflict"`）。
  3. 重新执行完整预检校验并核对新计算的 `preflightDigest` 与客户端传入值完全一致；若期间任一资产版本、参考图、模型能力或价格发生变化，返回 `409 Conflict`（`reason: "batch_preflight_stale"`）。
  4. 在数据库事务内创建 `ProjectAssetGenerationBatch`（`status: "running"`）与全部 `ProjectAssetGenerationBatchItem`（`status: "pending"`, `attempt: 1`, 生成唯一 `submissionKey`）。
  5. 顺序领取 `pending` 项并调用 `CreateTask`（传入 `CreationSubmissionID = item.SubmissionKey`）：
     - 每次领取前在事务内检查 `batch.StopRequestedAt == nil && item.Status == "pending"`；若已被 `stop` 则跳过。
     - 若 `CreateTask` 因活动任务上限（`task_active_limit_exceeded`）或余额不足明确拒绝且未创建任务，将该项标记为 `failed`（记录对应 `errorReason`），继续处理或保留供用户后续显式重试；若在绑定 `taskId` 前发生未知错误，通过 `CreationSubmissionID` 反查任务，若不确定则标记为 `reconcile_required`。

### 3.3 状态聚合与恢复查询

- `GET /api/projects/:id/asset-generation-batches`：返回当前项目最近的批次摘要列表。
- `GET /api/projects/:id/asset-generation-batches/:batchId`：加载批次与全部项，关联查询对应的 `Task` 记录，实时同步每项状态：
  - `Task.Status == queued / running` → 若已请求取消但上游未确认则为 `cancel_requested`，否则为 `running`；
  - `Task.Status == succeeded` → 若包含有效 `assetImageCandidate` 则为 `succeeded`（附带候选 `state: pending_confirmation / adopted` 及输出图片 URL），若 `canRecoverMedia` 或缺失候选则为 `reconcile_required`；
  - `Task.Status == failed` → 若存在未结清账单或状态不明则为 `reconcile_required`，否则为 `failed`；
  - `Task.Status == cancelled` → `cancelled`。
  - 聚合批次整体状态（全部终态且全成功为 `completed`，含成功/失败混合终态为 `partial_failed`，含待核对项为 `reconcile_required`，全部未提交项停止且无运行项为 `stopped`）。

### 3.4 停止未提交项、取消已提交任务与安全重试

- `POST /api/projects/:id/asset-generation-batches/:batchId/stop`：在事务内设置 `batch.StopRequestedAt = now`，并将所有 `status = "pending" AND task_id = ''` 的项更新为 `status = "stopped"`；已绑定 `task_id` 的项保持原状态不变。
- `POST /api/projects/:id/asset-generation-batches/:batchId/cancel-tasks`：遍历批次内 `task_id != ''` 且处于 `queued`/`running`/`cancel_requested` 的项，调用 `CancelTask`；根据返回后的 `Task.Status` 与 `Task.ProviderCancelStatus` 将项状态更新为 `cancelled`（已确认取消）或 `cancel_requested`（上游不支持或待确认），绝不把未确认取消谎报为 `cancelled`。
- `POST /api/projects/:id/asset-generation-batches/:batchId/items/:itemId/retry`：校验该项当前状态为 `failed`（严禁对 `reconcile_required`、`running`、`succeeded`、`cancel_requested` 重试），确认旧任务（若有）无成功候选且无未决扣费后，将旧尝试追加至 `AttemptHistoryJSON`，递增 `Attempt`，生成新 `SubmissionKey`，创建新任务并绑定 `TaskID`。

---

## 4. 资源引用保护与安全边界

- 在 `backend/internal/repository/project_reference_image.go`（资源删除引用扫描）中增加对 `project_asset_generation_batch_items` 的扫描：当项状态为 `pending`、`running`、`cancel_requested` 或 `reconcile_required` 时，其 `input_snapshot_json` 中记录的 `referenceResourceIds` 视为活动业务引用，阻止物理删除并返回安全来源说明；当项变为 `stopped`（未提交即停止）或已终态失败/取消时，解除该批次项的独占输入引用（但资产历史版本与任务本身仍按既有规则保护资源）。
- `InputSnapshotJSON` 与 `ConfigSummaryJSON` 在持久化前执行敏感字段剥离（移除 `apiKey`、`authorization`、`headers`、`customEndpoint` 敏感查询串等），所有错误提示经过脱敏处理。

---

## 5. 前端真实界面设计

- 在 `web/src/pages/projects/detail/assets.tsx` 工具栏新增「批量生成图片」按钮，并在资产卡片上支持批量勾选（或弹窗内按角色/场景/道具分类勾选）。
- 新增 `web/src/pages/projects/detail/project-asset-batch-modal.tsx`（及纯逻辑辅助模块 `project-asset-batch.ts`）：
  1. **选择与配置阶段**：支持勾选多个角色/场景/道具资产，统一选择参考来源（`original` / `current` / `none`）与图片模型，点击「预检清单」。
  2. **预检确认阶段**：展示每项资产名称、分类、当前版本、参考图数量、模型兼容性及预估费用（或“自定义渠道/价格未知”提示）；存在不兼容项时明确标红原因并支持一键移除不合格项后重新预检；确认无误后使用稳定 `idempotencyKey` 提交批次。
  3. **执行、恢复与逐项采用阶段**：打开页面或弹窗时自动从服务端加载当前项目最近批次并轮询进度；分离提供「停止未提交项」与「取消已提交任务」按钮；对 `failed` 项提供「重试」按钮，对 `reconcile_required` 项展示待核对说明并禁用盲目重发；对 `succeeded` 项提供预览与「采用为当前成品」按钮（调用既有 `adoptProjectAssetImageCandidate` 接口，处理 409 版本冲突与幂等重放）。

---

## 6. 测试策略

1. **后端测试（`backend/internal/app/project_asset_batch_test.go` & `handler` 测试）**：
   - Schema V62 迁移与唯一约束测试（同键同参幂等返回、同键异参 409、重复资产拒绝、安全快照不含明文密钥）。
   - 预检与提交重验测试（越权资产、版本过期 409、多参考超模型上限、余额不足、价格未知标记）。
   - 派发、重启恢复、部分失败重试資格、`reconcile_required` 禁止重发、停止未提交与领取并发竞争、取消已提交任务（已确认 vs 未确认）测试。
   - 批次活动项的资源删除引用保护与停止后引用释放测试。
2. **前端测试（`web/test/project-asset-batch.test.tsx`）**：
   - 挂载真实批量生成组件，覆盖三分类多选、预检、幂等提交、刷新恢复、部分成功、停止/取消分离、逐项手动采用及 409 冲突保留候选。
