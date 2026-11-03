package app

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type ProducerRecoveryEnvironmentContract struct {
	EnvironmentID        string `json:"environmentId"`
	Platform             string `json:"platform"`
	ImageSHA256          string `json:"imageSha256"`
	IsolatedRootPath     string `json:"isolatedRootPath"`
	AdminOwner           string `json:"adminOwner"`
	NamespaceUIDIsolated bool   `json:"namespaceUidIsolated"`
	NoNewPrivileges      bool   `json:"noNewPrivileges"`
	CustodyVerified      bool   `json:"custodyVerified"`
	AuthorizationTicket  string `json:"authorizationTicket"`
	CandidateOnly        bool   `json:"candidateOnly"`
}

type ProducerDomainRecoveryProof struct {
	Environment                   ProducerRecoveryEnvironmentContract `json:"environment"`
	OperationID                   string                              `json:"operationId"`
	ResourceID                    string                              `json:"resourceId"`
	UserID                        string                              `json:"userId"`
	ExpectedControlOwner          string                              `json:"expectedControlOwner"`
	OriginalGenerationID          string                              `json:"originalGenerationId"`
	ProofPlatform                 string                              `json:"proofPlatform"`
	AllControllersExited          bool                                `json:"allControllersExited"`
	BorrowedLegacyHandle          bool                                `json:"borrowedLegacyHandle"`
	RecreatedEmptyDomain          bool                                `json:"recreatedEmptyDomain"`
	SameNameJobAssumed            bool                                `json:"sameNameJobAssumed"`
	AllOriginalHandlesLost        bool                                `json:"allOriginalHandlesLost"`
	ParentFilesStructContinuous   bool                                `json:"parentFilesStructContinuous"`
	LateJoinSealed                bool                                `json:"lateJoinSealed"`
	AllWritersExited              bool                                `json:"allWritersExited"`
	VerifiedEncoderSuccessReceipt bool                                `json:"verifiedEncoderSuccessReceipt"`
	EncoderReceiptLost            bool                                `json:"encoderReceiptLost"`
	DecodableOutputFileExists     bool                                `json:"decodableOutputFileExists"`
}

type ProducerDomainRecoveryResult struct {
	OperationID    string `json:"operationId"`
	ResourceID     string `json:"resourceId"`
	PhysicalState  string `json:"physicalState"`
	PlaybackStatus string `json:"playbackStatus"`
	PublishedReady bool   `json:"publishedReady"`
}

// ValidateProducerRecoveryEnvironment enforces the named environment, custody,
// privilege, and platform isolation prerequisites before any producer domain
// recovery can run. Candidate-only environments are rejected fail-closed.
func ValidateProducerRecoveryEnvironment(env ProducerRecoveryEnvironmentContract) error {
	if env.CandidateOnly {
		return BadAuthRequest("候选环境尚未完成实际绑定与执行授权，保持未执行状态")
	}
	if strings.TrimSpace(env.EnvironmentID) == "" ||
		strings.TrimSpace(env.ImageSHA256) == "" ||
		strings.TrimSpace(env.IsolatedRootPath) == "" ||
		strings.TrimSpace(env.AdminOwner) == "" ||
		strings.TrimSpace(env.AuthorizationTicket) == "" {
		return BadAuthRequest("恢复环境合同缺少必填的环境标识、镜像摘要、隔离路径、管理员或授权凭据")
	}
	if ! filepath.IsAbs(env.IsolatedRootPath) {
		return BadAuthRequest("恢复环境隔离根路径必须为绝对路径")
	}
	if !env.CustodyVerified || !env.NoNewPrivileges {
		return BadAuthRequest("恢复环境必须具备持续保管核验（custodyVerified）与禁止提权（noNewPrivileges）保证")
	}
	platform := strings.ToLower(strings.TrimSpace(env.Platform))
	switch platform {
	case "linux":
		if !env.NamespaceUIDIsolated {
			return BadAuthRequest("Linux C2-A/C2-B 恢复环境必须启用独占 Namespace 与 UID 隔离")
		}
	case "windows":
	default:
		return BadAuthRequest("不支持的生产者恢复平台类型")
	}
	return nil
}

// RecoverAndSettleProducerOperation verifies the named environment, rejects
// cross-platform extrapolation or Windows lost-handle / recreated-empty-domain
// assumptions, acquires the launch gate and original/playback physical locks,
// and settles the operation while strictly separating physical stop from ready publication.
func (s *Service) RecoverAndSettleProducerOperation(
	actor *model.User,
	proof ProducerDomainRecoveryProof,
) (res *ProducerDomainRecoveryResult, err error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if err := ValidateProducerRecoveryEnvironment(proof.Environment); err != nil {
		return nil, err
	}
	envPlatform := strings.ToLower(strings.TrimSpace(proof.Environment.Platform))
	proofPlatform := strings.ToLower(strings.TrimSpace(proof.ProofPlatform))
	if envPlatform != proofPlatform {
		return nil, BadAuthRequest("禁止跨平台外推生产者恢复证据（环境平台与证据平台不一致）")
	}
	if strings.TrimSpace(proof.OperationID) == "" ||
		strings.TrimSpace(proof.ResourceID) == "" ||
		strings.TrimSpace(proof.UserID) == "" ||
		strings.TrimSpace(proof.ExpectedControlOwner) == "" ||
		strings.TrimSpace(proof.OriginalGenerationID) == "" {
		return nil, repository.ErrResourceProducerUnsettled
	}

	// Windows full-handle loss or same-name Job / recreated empty domain assumptions
	// cannot prove that all original writers have stopped. Fail closed with ErrResourceProducerUnsettled.
	if envPlatform == "windows" && (proof.AllOriginalHandlesLost || proof.SameNameJobAssumed) {
		return nil, repository.ErrResourceProducerUnsettled
	}
	if !proof.AllControllersExited ||
		proof.BorrowedLegacyHandle ||
		proof.RecreatedEmptyDomain ||
		proof.SameNameJobAssumed ||
		!proof.ParentFilesStructContinuous ||
		!proof.LateJoinSealed ||
		!proof.AllWritersExited {
		return nil, repository.ErrResourceProducerUnsettled
	}

	row, err := s.repo.ResourceForUser(proof.UserID, proof.ResourceID)
	if err != nil {
		return nil, err
	}
	playIdentity := repository.ResourcePhysicalIdentity(row, "playback")
	origIdentity := repository.ResourcePhysicalIdentity(row, "original")
	gateIdentity := repository.PlaybackControlGateIdentity(proof.OperationID)

	locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{gateIdentity, origIdentity, playIdentity})
	if err != nil {
		return nil, err
	}
	defer func() {
		err = errors.Join(err, locks.Close())
	}()

	// Lost encoder receipt MUST NEVER publish ready, even if a decodable output file exists on disk.
	publishReady := proof.VerifiedEncoderSuccessReceipt && !proof.EncoderReceiptLost
	proofToken := fmt.Sprintf("c2-recovered:%s:%s:%s", proof.Environment.EnvironmentID, proof.OriginalGenerationID, envPlatform)
	errorText := ""
	if !publishReady {
		errorText = "producer domain settled after controller loss; encoder receipt unavailable"
	}

	auditEvent, err := newAdminAuditEvent(
		actor,
		"resource.producer_recovery.settle",
		"resource",
		proof.ResourceID,
		"执行生产者全控制者丢失原代际恢复结算",
		map[string]any{
			"environmentId":             proof.Environment.EnvironmentID,
			"platform":                  envPlatform,
			"operationId":               proof.OperationID,
			"originalGenerationId":      proof.OriginalGenerationID,
			"publishedReady":            publishReady,
			"encoderReceiptLost":        proof.EncoderReceiptLost,
			"decodableOutputFileExists": proof.DecodableOutputFileExists,
		},
	)
	if err != nil {
		return nil, err
	}

	repoWithLocks := s.repo.WithResourcePhysicalLocks(locks)
	if err := repoWithLocks.RecoverControllerLostPlaybackOperation(
		proof.OperationID,
		proof.ResourceID,
		proof.UserID,
		proof.ExpectedControlOwner,
		publishReady,
		proofToken,
		errorText,
		*auditEvent,
	); err != nil {
		return nil, err
	}

	playbackStatus := model.PlaybackStatusFailed
	if publishReady {
		playbackStatus = model.PlaybackStatusReady
	}
	return &ProducerDomainRecoveryResult{
		OperationID:    proof.OperationID,
		ResourceID:     proof.ResourceID,
		PhysicalState:  "settled",
		PlaybackStatus: playbackStatus,
		PublishedReady: publishReady,
	}, nil
}
