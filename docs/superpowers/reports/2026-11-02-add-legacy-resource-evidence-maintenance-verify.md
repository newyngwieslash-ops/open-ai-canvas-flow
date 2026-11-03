# 验证报告：add-legacy-resource-evidence-maintenance

- **变更名称**：`add-legacy-resource-evidence-maintenance`
- **验证日期**：`2026-11-02`
- **工作流模式**：`full`
- **关联设计文档**：`docs/superpowers/specs/2026-11-02-legacy-resource-evidence-maintenance-design.md`
- **关联实施计划**：`docs/superpowers/plans/2026-11-02-legacy-resource-evidence-maintenance.md`
- **验证结论**：**PASS（通过）**

---

## 1. 规格需求与场景对照矩阵

| 需求项 | OpenSpec 关键要求 | 实现与测试证据 | 状态 |
| --- | --- | --- | --- |
| **Requirement: 可信身份及历史停写来源** | 仅接受来源可核对的 manifest 及历史物理身份、归属、变体/alias 和全部 writer 停写证据，不以当前配置、文件相似或自报 `trusted: true` 代替；未知旧缩略图或缺少 writer 停写证据判为 `blocked` | `backend/internal/app/legacy_resource_maintenance.go`（`SignLegacyResourceEvidenceManifest`、`EvaluateLegacyResourceMaintenance`、`isObjectiveStopWriteProof`）；测试：`TestLegacyResourceEvidenceMaintenance_ManifestProvenanceAndDryRun` | PASS |
| **Requirement: 隔离 dry-run 与实际入口** | 先在授权隔离副本产生逐对象 `eligible/blocked/conflict`、依据和预计差异，再以实际入口验证失败恢复与重复执行；快照已变化、跨用户或存在共享物理 alias 时阻断；实际入口物理失败不清业务记录 | `backend/internal/repository/legacy_resource_maintenance.go`、`backend/internal/app/legacy_resource_maintenance.go`；测试：`TestLegacyResourceEvidenceMaintenance_ManifestProvenanceAndDryRun`、`TestLegacyResourceEvidenceMaintenance_ApprovalGatesAndRealEntrypoints` | PASS |
| **Requirement: 真实维护逐项授权** | 审阅精确差异、对象清单、备份/恢复、窗口和写入屏障后才执行批准操作；只读导出、补登记（`register_evidence`）、真实删除（`delete_after_evidence`）分别明确 | `ExportLegacyResourceSnapshot`、`ApplyLegacyResourceMaintenance`；测试：`TestLegacyResourceEvidenceMaintenance_ApprovalGatesAndRealEntrypoints` | PASS |

---

## 2. 自动化验证执行记录

1. **后端旧资源证据与隔离维护专项测试**
   - 命令：`go -C backend test ./internal/app -run TestLegacyResourceEvidenceMaintenance -v -count=1`
   - 结果：`ok yingce/backend/internal/app (8.540s)`，覆盖：
     - 缺少只读授权凭据拒绝快照导出；
     - 伪造签名/自报 `Trusted: true` 拦截（`untrusted_self_declaration` / `unverified_provenance`）；
     - 未知缩略图变体（`unknown_thumbnail_variant`）、非客观 writer 停写证明（`missing_writer_stop_proof`）、快照漂移（`snapshot_drift`）、跨用户归属冲突（`cross_user_mismatch`）、共享物理 alias 冲突（`shared_alias_conflict`）分类验证，且 Dry-Run 零数据库/文件副作用；
     - 缺少 `WriteBarrierConfirmed` 或混入 `blocked` 项时整批拒绝；
     - `register_evidence` 补登记 `settled` `ResourceProducerOperation` 证据、清理已证实 `verified-absent` 的旧缩略图阻断字段、写入 `AdminAuditEvent`、重复执行幂等且不触发删除；
     - `delete_after_evidence` 经真实 `DeleteAdminResources` + worker 入口验证业务引用拦截保留、部分物理失败保留数据库记录与恢复后重试收敛。
2. **文档同步核查**
   - 已同步更新 `docs/content/docs/overview/features.mdx` 与 `docs/content/docs/progress/pending-test.mdx`。
