# Brainstorm Summary: add-remote-storage-physical-settlement

## Core Problem
远端对象存储（S3 / Aliyun OSS / Tencent COS / Qiniu Kodo）在客户端超时、网络断开或本地进程退出时，已发出的请求、SDK 自动重试、未拿到 UploadID 的 Multipart 初始化（Unknown Init）或并发 Part/Complete/Abort 请求仍可能在远端迟到物化；版本桶（Versioned Bucket）上的普通 DELETE 只会产生 `DeleteMarker` 而未清除历史 Payload 版本；配置切换或权限撤销时若按当前默认桶/配置继续删除会导致跨桶误删或静默丢弃历史义务。

## Approved Architecture & Decisions
1. **具名端点合同与实验授权边界 (`RemoteStorageEndpointContract`)**：
   - 绑定具名 `ProviderProduct`、`ProviderVersion`、`Endpoint`、`Bucket`、`KeyPrefix`、`VersioningState`（`Disabled | Enabled | Suspended`）、显式 `AllowedAPIOperations`、预算上限（`MaxRequests`、`MaxBytes`、`MaxDurationSeconds`）、凭据引用 `CredentialRef`（禁止明文密钥入报告）与 `CleanupOwner`。
   - 缺少显式授权票据、超出预算或仅有本地模拟端点而无具名端点合同时，拒绝签发远端物理结算凭证，保持 `unknown` 保守阻断。
2. **全 Attempt 终态证明与 Unknown Init 屏障 (`RemoteProducerSettlementProof`)**：
   - 写前绑定资源物理身份（`Provider / Endpoint / Bucket / ObjectKey`），跟踪每一次 SDK/HTTP 尝试（`AttemptID`、`Kind`、`UploadID`、`PartNumbers`、`TerminalState`、`AbortConfirmed`）。
   - 若存在响应丢失或未知 Multipart 启动（`UnknownInitPossible: true`），必须证明在本地全部写入器建立硬停写屏障后（`WriterStopBarrierEstablished: true`），完成了覆盖该对象前缀的完整分页会话枚举（`ListUploadsPaginatedComplete: true`、`TruncatedPagesRemaining == 0`），且枚举发现的所有 `UploadID` 均已确认 `AbortMultipartUpload` 成功并在后续复检中确认无残留分片（`VerifiedAbsentAfterAbort: true`）。绝不接受凭超时或最后一次 HTTP 200 响应直接抹除早期未知 attempt。
3. **版本桶与物理穷尽清理 (`RemotePhysicalExhaustionPlan` & `SettleRemoteResourceDeletion`)**：
   - 区分 `DeleteMarker` 与真实 Payload 版本：当桶启用版本控制或存在 `VersionIDs` 时，仅存在 `DeleteMarker` 绝不视为物理清理完成，必须穷尽删除全部非共享的 Payload 版本与残留 Multipart 会话，同时保留其他存活资源共享的物理 alias。
   - 在配置切换（当前系统 OSS 已改为其他 `Endpoint`/`Bucket`）或权限撤销（403/503）时，严格锁定资源自身的历史 `Provider / Endpoint / Bucket / ObjectKey`，拒绝向新配置桶发起误删请求，并在物理清理未穷尽时保留 `Resource` 记录与 `ResourceDeletionJob` 以便权限/配置恢复后重入核账。
