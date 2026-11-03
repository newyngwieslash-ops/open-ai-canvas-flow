---
archived-with: 2026-11-02-add-legacy-resource-evidence-maintenance
status: final
---
# Legacy Resource Evidence & Maintenance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task with TDD (`tdd_mode: tdd`).

**Goal:** 为缺少 `ResourceProducerOperation` 结算证明或含未知旧缩略图状态的旧资源提供可核验历史证据清单（Manifest）、隔离 Dry-Run 分类评估（`eligible | blocked | conflict`）、以及逐项授权的维护接管与真实删除入口验证。

**Architecture:** 在 `backend/internal/repository/legacy_resource_maintenance.go` 与 `backend/internal/app/legacy_resource_maintenance.go` 中实现历史证据清单摘要校验、只读快照导出（需显式只读授权凭据）、隔离 Dry-Run 判定、以及绑定精确对象/差异摘要/写入屏障/备份确认的逐项接管，并通过 `DeleteUserResource` / `DeleteDetachedResources` / `drainResourceDeletionJobs` 真实入口完成隔离验证。

**Tech Stack:** Go 1.25, GORM (SQLite in-memory/tempdir isolated replicas), `yingce/backend/internal/app`, `yingce/backend/internal/repository`, `yingce/backend/internal/service`.

---

## Task Checklist

- [x] 1.1 取得维护方可信脱敏manifest/历史writer/停写来源，限定对象及快照范围；如需读取原库，先取得精确只读授权。 <!-- comet-task:b4525ff4-fda3-4419-be0f-c0b153ca726b -->
- [x] 1.2 细化并审批隔离dry-run及最小接管工具，覆盖来源完整性、归属/变体/alias、未知项和过期快照判定。 <!-- comet-task:ad1dd2b6-cc9f-4443-9e59-3f783ab4ddbf -->
- [x] 2.1 获批后实现逐对象eligible/blocked/conflict及差异清单，完成伪造来源/跨用户/未知writer/共享alias/重复执行/失败恢复等隔离验证。 <!-- comet-task:79f3a2b7-f177-49df-96e5-dd98197c9393 -->
- [x] 2.2 在可丢弃副本和合成对象通过实际入口验证权限、引用、事务和部分失败，经独立审查并完成最终Verify，不写原业务库。 <!-- comet-task:f3c92bbb-1515-41fc-8304-c9c3bcc27944 -->
- [x] 2.3 如确需live维护，提交精确对象/差异、备份恢复、窗口、写入屏障及分项权限审批；未获批保持blocked，不自动执行。 <!-- comet-task:df9542ac-8816-4baa-8e2b-7cd8538bb4c9 -->

---

### Task 1: 可信证据模型、只读快照范围限定与 Dry-Run 规则设计 (Tasks 1.1 & 1.2)

**Files:**
- Create: `backend/internal/app/legacy_resource_maintenance_test.go`
- Create: `backend/internal/repository/legacy_resource_maintenance.go`
- Create: `backend/internal/app/legacy_resource_maintenance.go`
- Modify: `backend/internal/service/aliases_types.go`

**Steps:**
1. 先编写失败测试 `TestLegacyResourceEvidenceMaintenance_ManifestProvenanceAndDryRun`，验证：
   - 未提供精确只读授权凭据时拒绝导出快照；
   - 仅设置 `Trusted: true` 但缺少有效 `ProvenanceSignature`、缺少 `ReadOnlyAuthTicket` 或使用非客观 `StopWriteProof`（如 `"operator-ack"`、`"process-exited"`、空字符串）时被判为 `blocked`；
   - 快照漂移（`Status`/`ObjectKey`/`ThumbnailSize` 变化）、跨用户归属不符（`cross_user_mismatch`）、未知旧缩略图（`ThumbnailStatus == "ready"` 或 `ThumbnailSize > 0` 但 `thumbnail` 变体为 `unknown` 或缺少证据）以及存在未覆盖共享物理 alias 时分别被准确判为 `blocked` 或 `conflict`；
   - Dry-Run 过程零数据库写操作、零物理文件改动。

### Task 2: 逐对象差异计算、重复执行幂等、失败恢复与真实删除入口验证 (Tasks 2.1, 2.2 & 2.3)

**Files:**
- Modify: `backend/internal/repository/legacy_resource_maintenance.go`
- Modify: `backend/internal/app/legacy_resource_maintenance.go`
- Modify: `backend/internal/app/legacy_resource_maintenance_test.go`
- Modify: `docs/content/docs/overview/features.mdx`
- Modify: `docs/content/docs/progress/pending-test.mdx`

**Steps:**
1. 编写失败测试 `TestLegacyResourceEvidenceMaintenance_ApprovalGatesAndRealEntrypoints`，验证：
   - 未获批或缺少 `BackupConfirmed` / `RecoveryDrillVerified` / `WriteBarrierConfirmed` / `MaintenanceWindow`、或 `ExpectedDiffDigest` 不匹配、或请求包含非 `eligible` 资源时拒绝执行，保持 `blocked` 且不修改任何记录；
   - `register_evidence` 授权仅补登记 `settled` `ResourceProducerOperation` 证据（及已证实 `verified-absent` 的旧缩略图字段归零）并写入 `AdminAuditEvent`，重复执行幂等，且绝不自动触发删除；
   - `delete_after_evidence` 授权在隔离副本和合成物理对象上通过真实入口（`DeleteUserResource` / `DeleteDetachedResources` + `drainResourceDeletionJobs`）验证：
     - 存在画布/资产业务引用的资源被真实入口拦截保留（`ResourceDeletionReferencedError`），物理文件与数据库记录均不丢失；
     - 共享同一物理对象且另一 alias 仍存活时，当前资源记录完成清理但共享物理文件被安全保留（`retained`）；
     - 物理删除部分失败时（例如模拟底层对象删除报错），删除作业保留可重试状态，不清除 `Resource` 记录或伪造结算；
     - 无引用、无共享 alias 且证据完备的合成对象通过真实入口完成物理文件删除与记录清理。
2. 运行 `go -C backend test ./internal/app -run TestLegacyResourceEvidenceMaintenance -v -count=1` 确认全部通过。
