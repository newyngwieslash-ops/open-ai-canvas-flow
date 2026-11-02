# Brainstorm Summary: add-legacy-resource-evidence-maintenance

## Core Problem
旧版本创建的 `Resource` 记录缺少 `ResourceProducerOperation` 物理结算证据，或存在 `ThumbnailStatus == "ready" || ThumbnailSize > 0` 但缺少可核验缩略图对象身份的历史行。在现有保守安全合同下，这些资源在 `stageResourceDeletion` 和 `PrepareResourceDeletion` 中会被 `ErrResourceProducerUnsettled` 或 `"thumbnail object identity unavailable"` 阻断删除。不能通过猜测对象 key、凭当前配置推断历史位置、或轻信手写 `trusted: true` 标记来批量补造 `settled` 状态。

## Approved Architecture & Decisions
1. **可核验历史证据清单 (`LegacyResourceEvidenceManifest`)**：
   - 包含清单来源元数据（导出批次 ID、来源类型、只读授权票据、清单摘要）、逐资源快照（`ResourceID`、`UserID`、`Status`、`Provider`、`Endpoint`、`Bucket`、`ObjectKey`、`PlaybackObjectKey`、`PlaybackStatus`、`ThumbnailStatus`、`ThumbnailSize` 及期望快照摘要）、变体证据（`original`、`playback`、`thumbnail` 的历史身份与对象状态）、以及全部历史 writer 的客观停写证据（非空 `WriterID`、`Protocol`、`TerminalState`、`StopWriteProof`，不接受仅凭 `Trusted: true` 布尔值或空口确认绕过校验）。
2. **隔离 Dry-Run 评估 (`EvaluateLegacyResourceMaintenance`)**：
   - 只读对比当前隔离副本数据库状态、引用关系、物理 alias 与 manifest 证据，逐对象输出 `eligible`、`blocked` 或 `conflict` 分类、机器可读原因码（如 `unverified_provenance`、`untrusted_self_declaration`、`missing_writer_stop_proof`、`unsettled_writer_attempt`、`unknown_thumbnail_variant`、`snapshot_drift`、`cross_user_mismatch`、`shared_alias_conflict`、`active_business_reference`）以及预计变更差异（`ExpectedDiff` 与 `DiffDigest`）。
   - Dry-Run 绝不写入或修改任何数据库记录或物理文件。
3. **逐项授权接管与真实入口验证 (`ApplyLegacyResourceMaintenance`)**：
   - 仅当传入显式维护授权票据（包含精确批准的 `ApprovedResourceIDs`、与 Dry-Run 一致的 `ExpectedDiffDigest`、明确的 `Operation`（`register_evidence` 或 `delete_after_evidence`）、备份恢复确认 `BackupConfirmed`、写入屏障确认 `WriteBarrierConfirmed`）时，才在事务内对复核仍为 `eligible` 的对象执行证据补登记（写入或收敛 `ResourceProducerOperation` 的 `settled` 历史证据，并在已核实缩略图无残留或已在隔离环境验证清理后归零未知缩略图阻断字段，同时写入 `AdminAuditEvent` 审计记录）。
   - 若授权模式为 `delete_after_evidence`，补登记后必须调用真实的资源删除入口（`DeleteUserResource` / `DeleteDetachedResources` + `drainResourceDeletionJobs`），由真实入口执行权限、引用、物理锁、alias 保留与部分失败回滚校验；任何一步失败均保持记录不丢失。
