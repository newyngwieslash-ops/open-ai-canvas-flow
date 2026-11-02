---
comet_change: add-remote-storage-physical-settlement
role: technical-design
canonical_spec: openspec
---

# 远端存储物理结算与完整清理技术设计文档

## 1. 背景与目标

在现有资源生命周期安全模型中，`FinishResourceProduction` 仅允许 `Provider == "local"` 且具备 `"local-writer-returned"` 或 `"ffmpeg-wait-returned"` 证明的操作直接进入 `PhysicalState == "settled"`；远端对象存储由于网络延迟、SDK 重试、Multipart 会话残留及版本桶 `DeleteMarker` 等特性，不能仅凭单次客户端响应或超时判定所有已接收请求均已终态结算。

本设计在 `backend/internal/repository/remote_storage_settlement.go` 与 `backend/internal/app/remote_storage_settlement.go` 中实现**具名远端端点合同校验、全写入尝试终态核证、版本/分片物理穷尽清理与配置/权限漂移防护**，在不放宽现有 `unknown` 保守阻断的前提下，为具备完整合同与客观穷尽证据的远端资源提供可重入、可审计的物理结算与清理闭环。

## 2. 核心数据结构

### 2.1 具名端点合同 (`RemoteStorageEndpointContract`)
```go
type RemoteStorageEndpointContract struct {
	ContractID           string   `json:"contractId"`
	Provider             string   `json:"provider"`        // "s3" | "aliyun-oss" | "tencent-cos" | "qiniu-kodo"
	ProviderProduct      string   `json:"providerProduct"` // e.g. "aws-s3", "minio-release", "aliyun-oss-standard"
	ProviderVersion      string   `json:"providerVersion"`
	Endpoint             string   `json:"endpoint"`
	Bucket               string   `json:"bucket"`
	DedicatedPrefix      string   `json:"dedicatedPrefix"`
	VersioningStatus     string   `json:"versioningStatus"` // "disabled" | "enabled" | "suspended"
	AllowedOperations    []string `json:"allowedOperations"`
	MaxRequests          int      `json:"maxRequests"`
	MaxBytes             int64    `json:"maxBytes"`
	MaxDurationSeconds   int      `json:"maxDurationSeconds"`
	CredentialRef        string   `json:"credentialRef"` // 仅允许引用标识，禁止包含明文 Secret
	CleanupOwner         string   `json:"cleanupOwner"`
	AuthorizationTicket  string   `json:"authorizationTicket"`
	IsLocalCounterexample bool    `json:"isLocalCounterexample"`
}
```

### 2.2 写入尝试与未知启动核证 (`RemoteWriteAttemptEvidence` & `RemoteProducerSettlementProof`)
```go
type RemoteWriteAttemptEvidence struct {
	AttemptID                string   `json:"attemptId"`
	Kind                     string   `json:"kind"` // "put_object" | "multipart_init" | "upload_part" | "complete_multipart"
	UploadID                 string   `json:"uploadId"`
	PartNumbers              []int    `json:"partNumbers"`
	ResponseLost             bool     `json:"responseLost"`
	TerminalState            string   `json:"terminalState"` // "settled" | "unknown" | "in_flight"
	Outcome                  string   `json:"outcome"`       // "success" | "failed" | "aborted"
	AbortConfirmed           bool     `json:"abortConfirmed"`
	VerifiedAbsentAfterAbort bool     `json:"verifiedAbsentAfterAbort"`
	ProducedVersionID        string   `json:"producedVersionId"`
}

type RemoteProducerSettlementProof struct {
	Contract                 RemoteStorageEndpointContract `json:"contract"`
	ResourceID               string                        `json:"resourceId"`
	UserID                   string                        `json:"userId"`
	Variant                  string                        `json:"variant"` // "original"
	Provider                 string                        `json:"provider"`
	Endpoint                 string                        `json:"endpoint"`
	Bucket                   string                        `json:"bucket"`
	ObjectKey                string                        `json:"objectKey"`
	WriterStopBarrier        bool                          `json:"writerStopBarrier"`
	UnknownInitPossible      bool                          `json:"unknownInitPossible"`
	PostBarrierListComplete  bool                          `json:"postBarrierListComplete"`
	TruncatedPagesRemaining  int                           `json:"truncatedPagesRemaining"`
	DiscoveredOrphanUploads  []string                      `json:"discoveredOrphanUploads"`
	AbortedOrphanUploads     []string                      `json:"abortedOrphanUploads"`
	Attempts                 []RemoteWriteAttemptEvidence  `json:"attempts"`
}
```

### 2.3 物理穷尽清理证据 (`RemotePhysicalExhaustionEvidence`)
```go
type RemoteObjectVersionItem struct {
	VersionID      string `json:"versionId"`
	IsDeleteMarker bool   `json:"isDeleteMarker"`
	Deleted        bool   `json:"deleted"`
}

type RemotePhysicalExhaustionEvidence struct {
	Contract                RemoteStorageEndpointContract `json:"contract"`
	ResourceID              string                        `json:"resourceId"`
	UserID                  string                        `json:"userId"`
	ActiveMultipartUploads  []string                      `json:"activeMultipartUploads"`
	AbortedMultipartUploads []string                      `json:"abortedMultipartUploads"`
	Versions                []RemoteObjectVersionItem     `json:"versions"`
	PostDeleteListComplete  bool                          `json:"postDeleteListComplete"`
}
```

## 3. 安全不变量与判定逻辑

1. **端点合同与授权校验 (`ValidateRemoteEndpointContract`)**：
   - 若 `IsLocalCounterexample == true` 或 `AuthorizationTicket == ""` 或 `ProviderProduct == ""` 或 `ProviderVersion == ""` 或 `CleanupOwner == ""`，拒绝作为可信端点合同；
   - 若 `CredentialRef` 包含明文密钥特征（如 `sk-`、`secret`、`AKIA` 或长度超限的原始憑证）则立即拒绝，防止报告或审计日志泄露密钥；
   - 资源对象的 `ObjectKey` 必须位于 `DedicatedPrefix` 范围内，且 `Provider / Endpoint / Bucket` 必须与合同及数据库中的历史记录完全一致。
2. **全 Attempt 物理结算判定 (`SettleRemoteResourceProduction`)**：
   - 必须满足 `WriterStopBarrier == true` 且 `Attempts` 非空；
   - 每一条 `Attempt` 必须 `TerminalState == "settled"`；若 `ResponseLost == true` 或 `Kind == "multipart_init"` 未完成提交，必须同时具备 `AbortConfirmed == true` 与 `VerifiedAbsentAfterAbort == true`；
   - 若 `UnknownInitPossible == true`（或任意 attempt 发生 `ResponseLost`），必须满足 `PostBarrierListComplete == true`、`TruncatedPagesRemaining == 0`，且 `DiscoveredOrphanUploads` 中的每一个 `UploadID` 均包含在 `AbortedOrphanUploads` 中；
   - 校验通过后，在 `ResourceLifecycleTransaction` 中将该资源的 `ResourceProducerOperation` 从 `writing`/`unknown` 原子更新为 `PhysicalState: "settled"`，并写入包含合同 ID 与 attempt 摘要的 `Proof`。
3. **版本/分片物理穷尽与配置/权限漂移防护 (`VerifyRemotePhysicalExhaustion` & `ExecuteRemoteContractDeletion`)**：
   - 核对系统或用户存储配置：若当前活动 OSS 配置的 `Provider / Endpoint / Bucket` 已偏离资源自身的 `Provider / Endpoint / Bucket` 且无匹配的历史存储位置凭据，立即阻断删除并保留 `Resource` 与 `ResourceDeletionJob`，绝不向新桶发送删除请求；
   - 对启用版本控制的端点，若 `Versions` 中存在仅添加了 `IsDeleteMarker: true` 而仍有 `IsDeleteMarker == false && Deleted == false` 的真实 Payload 版本，或者 `ActiveMultipartUploads` 未全部包含于 `AbortedMultipartUploads`，或者 `PostDeleteListComplete == false`，一律返回未穷尽错误，保留数据库记录供重试；
   - 当共享物理对象的其他 `Resource`（alias）仍存活时，保留远端物理对象（`retained = true`）仅完成当前记录收尾；无共享 alias 且全部版本、分片与主对象均已穷尽删除后，才完成 `FinishResourceDeletion`。
