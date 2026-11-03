package app

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strings"
	"time"

	"yingce/backend/internal/model"
)

type RemoteStorageEndpointContract struct {
	ContractID            string   `json:"contractId"`
	Provider              string   `json:"provider"`
	ProviderProduct       string   `json:"providerProduct"`
	ProviderVersion       string   `json:"providerVersion"`
	Endpoint              string   `json:"endpoint"`
	Bucket                string   `json:"bucket"`
	DedicatedPrefix       string   `json:"dedicatedPrefix"`
	VersioningStatus      string   `json:"versioningStatus"`
	AllowedOperations     []string `json:"allowedOperations"`
	MaxRequests           int      `json:"maxRequests"`
	MaxBytes              int64    `json:"maxBytes"`
	MaxDurationSeconds    int      `json:"maxDurationSeconds"`
	CredentialRef         string   `json:"credentialRef"`
	CleanupOwner          string   `json:"cleanupOwner"`
	AuthorizationTicket   string   `json:"authorizationTicket"`
	IsLocalCounterexample bool     `json:"isLocalCounterexample"`
}

type RemoteWriteAttemptEvidence struct {
	AttemptID                string `json:"attemptId"`
	Kind                     string `json:"kind"`
	UploadID                 string `json:"uploadId"`
	PartNumbers              []int  `json:"partNumbers"`
	ResponseLost             bool   `json:"responseLost"`
	TerminalState            string `json:"terminalState"`
	Outcome                  string `json:"outcome"`
	AbortConfirmed           bool   `json:"abortConfirmed"`
	VerifiedAbsentAfterAbort bool   `json:"verifiedAbsentAfterAbort"`
	ProducedVersionID        string `json:"producedVersionId"`
}

type RemoteProducerSettlementProof struct {
	Contract                RemoteStorageEndpointContract `json:"contract"`
	ResourceID              string                        `json:"resourceId"`
	UserID                  string                        `json:"userId"`
	Variant                 string                        `json:"variant"`
	Provider                string                        `json:"provider"`
	Endpoint                string                        `json:"endpoint"`
	Bucket                  string                        `json:"bucket"`
	ObjectKey               string                        `json:"objectKey"`
	WriterStopBarrier       bool                          `json:"writerStopBarrier"`
	UnknownInitPossible     bool                          `json:"unknownInitPossible"`
	PostBarrierListComplete bool                          `json:"postBarrierListComplete"`
	TruncatedPagesRemaining int                           `json:"truncatedPagesRemaining"`
	DiscoveredOrphanUploads []string                      `json:"discoveredOrphanUploads"`
	AbortedOrphanUploads    []string                      `json:"abortedOrphanUploads"`
	Attempts                []RemoteWriteAttemptEvidence  `json:"attempts"`
}

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

type RemoteContractDeletionResult struct {
	ContractID             string `json:"contractId"`
	ResourceID             string `json:"resourceId"`
	Finalized              bool   `json:"finalized"`
	RetainedPhysicalObject bool   `json:"retainedPhysicalObject"`
}

// ValidateRemoteEndpointContract enforces that a remote storage contract binds
// a named provider product/version, endpoint, bucket, prefix, budget, cleanup
// owner, and non-leaking credential reference, rejecting local counterexamples.
func ValidateRemoteEndpointContract(contract RemoteStorageEndpointContract) error {
	if contract.IsLocalCounterexample {
		return BadAuthRequest("本地反例不能作为可信远端端点合同")
	}
	if strings.TrimSpace(contract.ContractID) == "" ||
		strings.TrimSpace(contract.ProviderProduct) == "" ||
		strings.TrimSpace(contract.ProviderVersion) == "" ||
		strings.TrimSpace(contract.Endpoint) == "" ||
		strings.TrimSpace(contract.Bucket) == "" ||
		strings.TrimSpace(contract.DedicatedPrefix) == "" ||
		strings.TrimSpace(contract.CleanupOwner) == "" ||
		strings.TrimSpace(contract.AuthorizationTicket) == "" {
		return BadAuthRequest("远端端点合同缺少必填的具名产品版本、端点、专用前缀、清理责任人或授权凭据")
	}
	if !supportedResourceDeleteProvider(contract.Provider) || normalizedResourceProvider(contract.Provider) == "local" {
		return BadAuthRequest("远端端点合同必须指定受支持的远端对象存储类型")
	}
	switch strings.ToLower(strings.TrimSpace(contract.VersioningStatus)) {
	case "disabled", "enabled", "suspended":
	default:
		return BadAuthRequest("远端端点合同必须明确桶版本控制状态（disabled/enabled/suspended）")
	}
	if contract.MaxRequests <= 0 || contract.MaxBytes <= 0 || contract.MaxDurationSeconds <= 0 || len(contract.AllowedOperations) == 0 {
		return BadAuthRequest("远端端点合同必须明确允许的 API 操作列表及请求数、字节数、时长预算上限")
	}
	cred := strings.TrimSpace(contract.CredentialRef)
	if cred == "" || strings.Contains(cred, ":") && !strings.HasPrefix(cred, "vault://") && !strings.HasPrefix(cred, "env://") && !strings.HasPrefix(cred, "secret-ref://") {
		return BadAuthRequest("远端端点合同仅允许登记凭据引用标识（vault://、env://、secret-ref://），禁止填入明文密钥")
	}
	lowerCred := strings.ToLower(cred)
	if strings.Contains(lowerCred, "akia") || strings.Contains(lowerCred, "secretkey") || strings.Contains(lowerCred, "accesskeysecret") {
		return BadAuthRequest("远端端点合同检测到疑似明文密钥内容，已拒绝")
	}
	return nil
}

// SettleRemoteResourceProduction verifies all write attempts, post-barrier
// paginated multipart upload scans, and orphan upload aborts under a validated
// endpoint contract before transitioning the resource's producer obligation to settled.
func (s *Service) SettleRemoteResourceProduction(actor *model.User, proof RemoteProducerSettlementProof) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	if err := ValidateRemoteEndpointContract(proof.Contract); err != nil {
		return err
	}
	if !proof.WriterStopBarrier || len(proof.Attempts) == 0 {
		return BadAuthRequest("远端物理结算必须先建立本地写入屏障并提供全部 attempt 终态证据")
	}
	if normalizedResourceProvider(proof.Provider) != normalizedResourceProvider(proof.Contract.Provider) ||
		strings.TrimRight(strings.TrimSpace(proof.Endpoint), "/") != strings.TrimRight(strings.TrimSpace(proof.Contract.Endpoint), "/") ||
		strings.TrimSpace(proof.Bucket) != strings.TrimSpace(proof.Contract.Bucket) {
		return BadAuthRequest("资源存储位置与具名远端端点合同不一致")
	}
	if !strings.HasPrefix(strings.TrimLeft(proof.ObjectKey, "/"), strings.TrimLeft(proof.Contract.DedicatedPrefix, "/")) {
		return BadAuthRequest("资源 ObjectKey 超出远端端点合同授权的专用前缀范围")
	}
	variant := strings.TrimSpace(proof.Variant)
	if variant == "" {
		variant = "original"
	}

	requiresPostBarrierScan := proof.UnknownInitPossible
	finalOutcome := "failed"
	attemptDigests := make([]string, 0, len(proof.Attempts))
	for _, att := range proof.Attempts {
		if strings.TrimSpace(att.AttemptID) == "" || att.TerminalState != "settled" {
			return BadAuthRequest("存在未进入 settled 终态的远端写入 attempt，保持 unknown 阻断")
		}
		if att.ResponseLost {
			requiresPostBarrierScan = true
		}
		if att.ResponseLost || att.Kind == "multipart_init" || att.Outcome == "aborted" {
			if !att.AbortConfirmed || !att.VerifiedAbsentAfterAbort {
				return BadAuthRequest(fmt.Sprintf("attempt %s 存在响应丢失或未完成分片会话，必须确认 Abort 并验证分片已清除", att.AttemptID))
			}
		}
		if att.Outcome == "success" {
			finalOutcome = "success"
		}
		attemptDigests = append(attemptDigests, fmt.Sprintf("%s:%s:%s:%s", att.AttemptID, att.Kind, att.UploadID, att.Outcome))
	}

	if requiresPostBarrierScan {
		if !proof.PostBarrierListComplete || proof.TruncatedPagesRemaining != 0 {
			return BadAuthRequest("存在未知启动或响应丢失风险时，必须在停写屏障后完成全分页 ListMultipartUploads 扫描（truncatedPagesRemaining=0）")
		}
		for _, orphanID := range proof.DiscoveredOrphanUploads {
			if !slices.Contains(proof.AbortedOrphanUploads, orphanID) {
				return BadAuthRequest(fmt.Sprintf("发现的在途分片上传会话 %s 尚未完成 Abort 确认", orphanID))
			}
		}
	}

	sum := sha256.Sum256([]byte(strings.Join(attemptDigests, "|")))
	proofToken := fmt.Sprintf("remote-contract:%s:%s", proof.Contract.ContractID, hex.EncodeToString(sum[:12]))

	auditEvent, err := newAdminAuditEvent(
		actor,
		"resource.remote_settlement.settle",
		"resource",
		proof.ResourceID,
		"登记具名远端端点物理结算证据",
		map[string]any{
			"contractId":          proof.Contract.ContractID,
			"providerProduct":     proof.Contract.ProviderProduct,
			"providerVersion":     proof.Contract.ProviderVersion,
			"endpoint":            proof.Endpoint,
			"bucket":              proof.Bucket,
			"objectKey":           proof.ObjectKey,
			"attemptCount":        len(proof.Attempts),
			"unknownInitPossible": proof.UnknownInitPossible,
			"proofToken":          proofToken,
		},
	)
	if err != nil {
		return err
	}

	return s.repo.SettleRemoteProducerObligation(
		proof.ResourceID,
		proof.UserID,
		variant,
		proof.Provider,
		proof.Endpoint,
		proof.Bucket,
		proof.ObjectKey,
		finalOutcome,
		proofToken,
		*auditEvent,
	)
}

// VerifyRemotePhysicalExhaustion checks that all active multipart uploads have
// been aborted and, on versioned buckets, that every non-delete-marker payload
// version has been deleted (never treating a DeleteMarker as payload removal).
func VerifyRemotePhysicalExhaustion(evidence RemotePhysicalExhaustionEvidence) error {
	if err := ValidateRemoteEndpointContract(evidence.Contract); err != nil {
		return err
	}
	if !evidence.PostDeleteListComplete {
		return BadAuthRequest("远端物理清理必须完成删除后的对象/版本与分片复检枚举")
	}
	for _, mpID := range evidence.ActiveMultipartUploads {
		if !slices.Contains(evidence.AbortedMultipartUploads, mpID) {
			return BadAuthRequest(fmt.Sprintf("远端对象仍存在未终止的分片上传会话 %s", mpID))
		}
	}
	if strings.EqualFold(evidence.Contract.VersioningStatus, "enabled") || strings.EqualFold(evidence.Contract.VersioningStatus, "suspended") || len(evidence.Versions) > 0 {
		if len(evidence.Versions) == 0 {
			return BadAuthRequest("版本桶物理清理必须提供版本穷尽清单")
		}
		for _, v := range evidence.Versions {
			if !v.IsDeleteMarker && !v.Deleted {
				return BadAuthRequest(fmt.Sprintf("版本桶真实数据版本 %s 尚未清除，DeleteMarker 不能替代物理删除", v.VersionID))
			}
		}
	}
	return nil
}

// ExecuteRemoteContractDeletion verifies physical exhaustion evidence and historical
// endpoint configuration parity before staging or re-driving the real deletion job.
func (s *Service) ExecuteRemoteContractDeletion(
	actor *model.User,
	evidence RemotePhysicalExhaustionEvidence,
) (*RemoteContractDeletionResult, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if err := VerifyRemotePhysicalExhaustion(evidence); err != nil {
		return nil, err
	}

	row, retained, finalized, err := s.repo.RemoteResourceDeletionStatus(evidence.ResourceID)
	if err != nil {
		return nil, err
	}
	if finalized {
		return &RemoteContractDeletionResult{
			ContractID:             evidence.Contract.ContractID,
			ResourceID:             evidence.ResourceID,
			Finalized:              true,
			RetainedPhysicalObject: retained,
		}, nil
	}

	if normalizedResourceProvider(row.Provider) != normalizedResourceProvider(evidence.Contract.Provider) ||
		strings.TrimRight(strings.TrimSpace(row.Endpoint), "/") != strings.TrimRight(strings.TrimSpace(evidence.Contract.Endpoint), "/") ||
		strings.TrimSpace(row.Bucket) != strings.TrimSpace(evidence.Contract.Bucket) {
		return nil, BadAuthRequest("资源历史存储位置与端点合同不匹配，已阻断删除")
	}

	// Verify that the active or historical storage setting for this resource still
	// matches the resource's exact historical Provider / Endpoint / Bucket so a
	// configuration switch never misdirects deletion to a different bucket.
	if err := s.requireHistoricalRemoteStorageParity(row.UserID, row); err != nil {
		return nil, err
	}

	if row.Status != model.ResourceStatusDeleting {
		delRes, err := s.DeleteAdminResources(actor, AdminResourceDeleteRequest{
			ResourceIDs: []string{row.ID},
		})
		if err != nil {
			return nil, err
		}
		if len(delRes.Blocked) > 0 {
			return nil, BadAuthRequest(fmt.Sprintf("资源 %s 被删除门禁阻断：%s", row.ID, delRes.Blocked[0].Reason))
		}
		for deadline := time.Now().Add(2 * time.Second); s.ActiveWorkerTasks() > 0 && time.Now().Before(deadline); {
			time.Sleep(10 * time.Millisecond)
		}
	}

	s.drainResourceDeletionJobs(4)

	afterRow, afterRetained, afterFinalized, err := s.repo.RemoteResourceDeletionStatus(evidence.ResourceID)
	if err != nil {
		return nil, err
	}
	if !afterFinalized || afterRow != nil {
		return nil, fmt.Errorf("远端物理删除尚未完成，已保留资源记录 %s 与删除意图等待权限或端点恢复后重试", evidence.ResourceID)
	}
	return &RemoteContractDeletionResult{
		ContractID:             evidence.Contract.ContractID,
		ResourceID:             evidence.ResourceID,
		Finalized:              true,
		RetainedPhysicalObject: afterRetained,
	}, nil
}

func (s *Service) requireHistoricalRemoteStorageParity(userID string, resource *model.Resource) error {
	if resource == nil {
		return BadAuthRequest("资源为空")
	}
	if resource.StorageSettingID != "" {
		_, setting, err := s.storageLocationValue(resource.StorageSettingID)
		if err != nil {
			_, setting, err = s.readUserOSSSettingByID(userID, resource.StorageSettingID)
		}
		if err != nil || !resourceStorageMatches(setting, resource) {
			return BadAuthRequest("资源绑定的历史存储位置配置不可用或已漂移，已保留原身份并阻断删除")
		}
		return nil
	}
	if setting, err := s.userOSSSettingForResource(userID, resource); err == nil && resourceStorageMatches(setting, resource) {
		return nil
	}
	_, sysSetting, err := s.readOSSSetting()
	if err != nil || !sysSetting.Enabled || !resourceStorageMatches(sysSetting, resource) {
		return BadAuthRequest("当前存储配置与资源历史存储位置（provider/endpoint/bucket）不一致，已保留原身份并阻断跨桶删除")
	}
	return nil
}
