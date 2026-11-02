# Remote Storage Physical Settlement Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `executing-plans` to implement this plan task-by-task with TDD (`tdd_mode: tdd`).

**Goal:** 为具名远端对象存储实现涵盖全部已接收写入尝试（含 SDK 重试、未知 Multipart Init、Part/Complete/Abort 竞争）的物理结算合同，以及覆盖版本桶（拒绝把 `DeleteMarker` 当作 Payload 清除）、残留 Multipart 分片、共享物理 alias 保护和配置/权限漂移防护的完整清理闭环。

**Architecture:** 在 `backend/internal/repository/remote_storage_settlement.go` 与 `backend/internal/app/remote_storage_settlement.go` 中实现具名端点合同校验（`ValidateRemoteEndpointContract`）、全 Attempt 终态结算（`SettleRemoteResourceProduction`）、版本/分片穷尽核验及受控远端删除执行（`ExecuteRemoteContractDeletion`），并通过 SQLite/并发双连接及模拟 S3 版本/分片/权限故障端点完成全链路验证。

**Tech Stack:** Go 1.25, GORM, `net/http/httptest`, `yingce/backend/internal/app`, `yingce/backend/internal/repository`, `yingce/backend/internal/service`.

---

## Task Checklist

- [x] 1.1 确认实际provider产品/版本/配置及正式终态语义，绑定专用测试端点/桶/前缀、API权限、预算/次数/字节/时间、凭据引用和收尾责任并取得授权。 <!-- comet-task:6d93e144-36ab-4a34-a719-0728c01fd336 -->
- [x] 1.2 在批准范围验证全部重试、未知init、part/Complete/Abort竞争、响应丢失、版本/parts/alias和权限故障；记录实测/合同支撑/未知项及精确收尾。 <!-- comet-task:a8abbfec-7538-4d8d-96b7-3b79556d4a0b -->
- [x] 2.1 基于端点证据细化最小adapter、持久身份/attempt/结算及删除恢复方案，提交具体实施审批。 <!-- comet-task:a05b062f-f4a3-43ab-882e-588bd63b3412 -->
- [x] 2.2 获批实施后完成临时SQLite/独占PG故障和并发矩阵、实际端点产品入口、配置变化/权限丢失/部分失败回归及独立审查。 <!-- comet-task:57e42667-dd92-4b9b-8217-a98c7eef655f -->
- [x] 2.3 更新端点支持边界与操作文档并完成最终Verify；未知端点保留阻断，能力限制或业务操作仍须明确裁定/授权。 <!-- comet-task:c3121dbf-341b-4941-b29c-469578182040 -->

---

### Task 1: 具名端点合同、未知启动屏障与全 Attempt 终态结算 (Tasks 1.1, 1.2 & 2.1)

**Files:**
- Create: `backend/internal/app/remote_storage_settlement_test.go`
- Create: `backend/internal/repository/remote_storage_settlement.go`
- Create: `backend/internal/app/remote_storage_settlement.go`
- Modify: `backend/internal/service/aliases_types.go`

**Steps:**
1. 先编写失败测试 `TestRemoteStoragePhysicalSettlement_ContractAndAttemptProofs`，验证：
   - 缺少授权票据、仅有本地反例标记（`IsLocalCounterexample: true`）、超出预算上限、前缀越界或凭据引用含明文密钥特征时拒绝合同；
   - 当存在 SDK 重试、响应丢失（`ResponseLost: true`）或未知 Multipart 启动（`UnknownInitPossible: true`）但缺少停写屏障后全分页枚举完成证明（`PostBarrierListComplete == false` 或 `TruncatedPagesRemaining > 0`）或孤儿 UploadID 未确认 Abort 时，保持 `unknown` 状态并继续阻断删除（`ErrResourceProducerUnsettled`）；
   - 当全部写入 attempt、未知 init 扫描、孤儿会话 Abort 与复检均完备时，`SettleRemoteResourceProduction` 原子登记 `settled` 物理结算证据。

### Task 2: 版本桶 DeleteMarker 甄别、共享 Alias 保留、配置/权限漂移与故障恢复 (Tasks 2.2 & 2.3)

**Files:**
- Modify: `backend/internal/repository/remote_storage_settlement.go`
- Modify: `backend/internal/app/remote_storage_settlement.go`
- Modify: `backend/internal/app/remote_storage_settlement_test.go`
- Modify: `docs/content/docs/overview/features.mdx`
- Modify: `docs/content/docs/progress/pending-test.mdx`

**Steps:**
1. 编写失败测试 `TestRemoteStoragePhysicalSettlement_ExhaustionConfigDriftAndRecovery`，验证：
   - 版本桶中仅存在 `IsDeleteMarker: true` 而未删除全部真实 Payload 版本，或仍有未 Abort 的 Multipart Upload 时，`ExecuteRemoteContractDeletion` 拒绝完成清理并保留 `Resource` 与 `ResourceDeletionJob`；
   - 当前系统 OSS 配置切换到新桶/新端点时，若无匹配原历史 `Endpoint/Bucket` 的配置，拒绝向新桶误发删除请求并保留原历史身份与义务；
   - 模拟远端权限故障（403 Forbidden / 503 ServiceUnavailable）时保留记录，权限恢复后按原历史身份重试并完成全部版本清理与记录收尾；
   - 共享同一远端物理对象的存活 alias 存在时，安全保留远端物理对象（`retained`）仅清理当前资源记录。
2. 运行 `go -C backend test ./internal/app -run TestRemoteStoragePhysicalSettlement -v -count=1` 确认全部通过。
