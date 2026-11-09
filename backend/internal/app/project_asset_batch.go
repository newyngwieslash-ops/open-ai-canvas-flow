package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

const MaxProjectAssetBatchItems = 50

type ProjectAssetBatchItemInput struct {
	AssetID           string `json:"assetId"`
	ExpectedVersionID string `json:"expectedVersionId"`
	ReferenceSource   string `json:"referenceSource"`
	Prompt            string `json:"prompt"`
	Operation         string `json:"operation,omitempty"`
}

type PreflightProjectAssetBatchRequest struct {
	Provider       string                       `json:"provider"`
	Model          string                       `json:"model"`
	LogicalModelID string                       `json:"logicalModelId,omitempty"`
	Config         map[string]any               `json:"config,omitempty"`
	Items          []ProjectAssetBatchItemInput `json:"items"`
}

type ProjectAssetBatchPreflightItem struct {
	Position             int      `json:"position"`
	AssetID              string   `json:"assetId"`
	AssetName            string   `json:"assetName"`
	AssetCategory        string   `json:"assetCategory"`
	BaseVersionID        string   `json:"baseVersionId"`
	ReferenceSource      string   `json:"referenceSource"`
	ReferenceCount       int      `json:"referenceCount"`
	ReferenceResourceIDs []string `json:"referenceResourceIds"`
	Operation            string   `json:"operation"`
	Prompt               string   `json:"prompt"`
	KnownMicrocredits    int64    `json:"knownMicrocredits"`
	PriceUnknown         bool     `json:"priceUnknown"`
}

type ProjectAssetBatchPreflightResult struct {
	ProjectID              string                           `json:"projectId"`
	Model                  string                           `json:"model"`
	Provider               string                           `json:"provider"`
	TotalKnownMicrocredits int64                            `json:"totalKnownMicrocredits"`
	PriceUnknownCount      int                              `json:"priceUnknownCount"`
	PreflightDigest        string                           `json:"preflightDigest"`
	Items                  []ProjectAssetBatchPreflightItem `json:"items"`
}

type CreateProjectAssetBatchRequest struct {
	IdempotencyKey  string `json:"idempotencyKey"`
	PreflightDigest string `json:"preflightDigest"`
	PreflightProjectAssetBatchRequest
}

type ProjectAssetBatchAttemptRecord struct {
	Attempt      int       `json:"attempt"`
	TaskID       string    `json:"taskId,omitempty"`
	Status       string    `json:"status"`
	ErrorReason  string    `json:"errorReason,omitempty"`
	ErrorMessage string    `json:"errorMessage,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ProjectAssetBatchItemView struct {
	ID                string                           `json:"id"`
	BatchID           string                           `json:"batchId"`
	Position          int                              `json:"position"`
	AssetID           string                           `json:"assetId"`
	AssetName         string                           `json:"assetName"`
	AssetCategory     string                           `json:"assetCategory"`
	BaseVersionID     string                           `json:"baseVersionId"`
	ReferenceSource   string                           `json:"referenceSource"`
	Status            string                           `json:"status"`
	TaskID            string                           `json:"taskId,omitempty"`
	Attempt           int                              `json:"attempt"`
	AttemptHistory    []ProjectAssetBatchAttemptRecord `json:"attemptHistory,omitempty"`
	KnownMicrocredits int64                            `json:"knownMicrocredits"`
	PriceUnknown      bool                             `json:"priceUnknown"`
	ErrorReason       string                           `json:"errorReason,omitempty"`
	ErrorMessage      string                           `json:"errorMessage,omitempty"`
	CanRetry          bool                             `json:"canRetry"`
	OutputURL         string                           `json:"outputUrl,omitempty"`
	Candidate         *AssetImageCandidate             `json:"candidate,omitempty"`
	UpdatedAt         time.Time                        `json:"updatedAt"`
}

type ProjectAssetBatchView struct {
	ID                     string                      `json:"id"`
	ProjectID              string                      `json:"projectId"`
	IdempotencyKey         string                      `json:"idempotencyKey"`
	Status                 string                      `json:"status"`
	TotalCount             int                         `json:"totalCount"`
	TotalKnownMicrocredits int64                       `json:"totalKnownMicrocredits"`
	PriceUnknownCount      int                         `json:"priceUnknownCount"`
	StopRequestedAt        *time.Time                  `json:"stopRequestedAt,omitempty"`
	Items                  []ProjectAssetBatchItemView `json:"items"`
	CreatedAt              time.Time                   `json:"createdAt"`
	UpdatedAt              time.Time                   `json:"updatedAt"`
}

type projectAssetBatchItemSnapshot struct {
	AssetID              string         `json:"assetId"`
	BaseVersionID        string         `json:"baseVersionId"`
	ReferenceSource      string         `json:"referenceSource"`
	ReferenceResourceIDs []string       `json:"referenceResourceIds"`
	Operation            string         `json:"operation"`
	Prompt               string         `json:"prompt"`
	Provider             string         `json:"provider"`
	Model                string         `json:"model"`
	LogicalModelID       string         `json:"logicalModelId,omitempty"`
	Config               map[string]any `json:"config,omitempty"`
	EncryptedConfig      string         `json:"encryptedConfig,omitempty"`
}

type preparedBatchItem struct {
	Summary  ProjectAssetBatchPreflightItem
	TaskReq  CreateTaskRequest
	Snapshot projectAssetBatchItemSnapshot
}

func (s *Service) encodeBatchItemSnapshot(item preparedBatchItem) (string, error) {
	config, err := json.Marshal(item.TaskReq.Input["config"])
	if err != nil {
		return "", err
	}
	// 等待项尚无 task 可恢复凭据；整份配置加密也覆盖自定义 headers。
	item.Snapshot.EncryptedConfig, err = s.encryptSettingSecret(string(config))
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(item.Snapshot)
	return string(raw), err
}

func sanitizeBatchConfigForPersistence(raw map[string]any) map[string]any {
	if raw == nil {
		return map[string]any{}
	}
	cleaned := make(map[string]any, len(raw))
	for k, v := range raw {
		lower := strings.ToLower(strings.TrimSpace(k))
		if lower == "apikey" || lower == "api_key" || lower == "secretkey" || lower == "secret_key" ||
			lower == "authorization" || lower == "headers" || lower == "token" || lower == "accesskey" {
			continue
		}
		if subMap, ok := v.(map[string]any); ok {
			cleaned[k] = sanitizeBatchConfigForPersistence(subMap)
			continue
		}
		cleaned[k] = v
	}
	return cleaned
}

func batchSubmissionKey(batchID string, position int, attempt int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s:%d:%d", batchID, position, attempt)))
	return "pab-" + hex.EncodeToString(sum[:])[:32]
}

func computeBatchHash(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func extractAppErrorReason(err error) string {
	if err == nil {
		return ""
	}
	var appErr *AppError
	if errors.As(err, &appErr) && string(appErr.Reason) != "" {
		return string(appErr.Reason)
	}
	return ""
}

func (s *Service) evaluateProjectAssetBatch(userID, projectID string, req PreflightProjectAssetBatchRequest) (*ProjectAssetBatchPreflightResult, []preparedBatchItem, error) {
	if _, err := s.repo.ProjectForUser(userID, projectID); err != nil {
		return nil, nil, err
	}
	if len(req.Items) == 0 {
		return nil, nil, BadAuthRequest("请至少选择一个项目资产")
	}
	if len(req.Items) > MaxProjectAssetBatchItems {
		return nil, nil, BadAuthRequest(fmt.Sprintf("单次批量生成最多支持 %d 个资产", MaxProjectAssetBatchItems))
	}

	seenAssets := make(map[string]struct{}, len(req.Items))
	prepared := make([]preparedBatchItem, 0, len(req.Items))
	var totalKnown int64
	priceUnknownCount := 0

	for idx, item := range req.Items {
		assetID := strings.TrimSpace(item.AssetID)
		if assetID == "" {
			return nil, nil, BadAuthRequest("批次中包含空白资产 ID")
		}
		if _, dup := seenAssets[assetID]; dup {
			return nil, nil, BadAuthRequest("同一批次内不能包含重复的资产")
		}
		seenAssets[assetID] = struct{}{}

		asset, err := s.projectImageAsset(userID, projectID, assetID)
		if err != nil {
			return nil, nil, err
		}
		expectedVersionID := strings.TrimSpace(item.ExpectedVersionID)
		if expectedVersionID == "" || expectedVersionID != asset.PrimaryVersionID {
			return nil, nil, projectAssetVersionConflict()
		}

		operation := strings.TrimSpace(item.Operation)
		if operation == "" {
			if asset.Category == model.AssetCategoryCharacter {
				operation = "character_turnaround"
			} else {
				operation = "project_asset_image"
			}
		}
		if (operation == "character_turnaround") != (asset.Category == model.AssetCategoryCharacter) {
			return nil, nil, BadAuthRequest("生成操作与资产分类不匹配")
		}

		refSource := strings.TrimSpace(item.ReferenceSource)
		if refSource == "" {
			refSource = "none"
		}
		if refSource != "original" && refSource != "current" && refSource != "none" {
			return nil, nil, BadAuthRequest("不支持的参考图来源")
		}

		version, err := s.repo.AssetVersion(asset.PrimaryVersionID)
		if err != nil {
			return nil, nil, err
		}

		var refResourceIDs []string
		var refImageMaps []map[string]any
		switch refSource {
		case "original":
			refs := referenceImagesFromDefinition(version.DefinitionJSON)
			if len(refs) == 0 {
				return nil, nil, BadAuthRequest(fmt.Sprintf("资产「%s」当前版本没有原始参考图", asset.Title))
			}
			for _, ref := range refs {
				refResourceIDs = append(refResourceIDs, ref.ResourceID)
				refImageMaps = append(refImageMaps, map[string]any{
					"id":         ref.ResourceID,
					"storageKey": "resource:" + ref.ResourceID,
					"url":        "/api/resources/" + ref.ResourceID + "/file",
				})
			}
		case "current":
			reps, repErr := s.repo.AssetRepresentations(version.ID)
			if repErr != nil {
				return nil, nil, repErr
			}
			resID := ""
			for _, rep := range reps {
				if rep.Role == "primary" && rep.ResourceID != "" {
					resID = rep.ResourceID
					break
				}
			}
			if resID == "" {
				return nil, nil, BadAuthRequest(fmt.Sprintf("资产「%s」当前版本没有成品图片", asset.Title))
			}
			refResourceIDs = []string{resID}
			refImageMaps = []map[string]any{{
				"id":         resID,
				"storageKey": "resource:" + resID,
				"url":        "/api/resources/" + resID + "/file",
			}}
		case "none":
			refResourceIDs = []string{}
			refImageMaps = []map[string]any{}
		}

		prompt := strings.TrimSpace(item.Prompt)
		if prompt == "" {
			prompt = strings.TrimSpace(version.Prompt)
		}
		if prompt == "" {
			prompt = strings.TrimSpace(asset.Title)
		}
		if prompt == "" {
			return nil, nil, BadAuthRequest("生成提示词不能为空")
		}

		cfgCopy := maps.Clone(req.Config)
		if cfgCopy == nil {
			cfgCopy = map[string]any{}
		}
		if cfgCopy["channelId"] == nil {
			cfgCopy["channelId"] = ""
		}
		cfgCopy["count"] = "1"
		if strings.TrimSpace(req.Model) != "" && cfgCopy["model"] == nil {
			cfgCopy["model"] = strings.TrimSpace(req.Model)
		}

		metaMap := map[string]any{
			"operation":             operation,
			"adoptionMode":          "manual",
			"projectAssetVersionId": asset.PrimaryVersionID,
			"referenceSource":       refSource,
		}
		if operation == "character_turnaround" {
			metaMap["characterAssetId"] = asset.ID
		} else {
			metaMap["projectAssetId"] = asset.ID
		}

		inputMap := map[string]any{
			"mode":            "image",
			"prompt":          prompt,
			"config":          cfgCopy,
			"metadata":        metaMap,
			"referenceImages": refImageMaps,
		}

		prep := &creationTaskPreparation{}
		taskReq := CreateTaskRequest{
			creationPrepare: prep,
			ProjectID:       projectID,
			Type:            "canvas_image",
			Operation:       operation,
			Prompt:          prompt,
			Provider:        strings.TrimSpace(req.Provider),
			Model:           strings.TrimSpace(req.Model),
			LogicalModelID:  strings.TrimSpace(req.LogicalModelID),
			Input:           inputMap,
		}
		if taskReq.Provider == "" {
			taskReq.Provider = "custom"
		}

		_, err = s.CreateTask(userID, taskReq)
		if err != nil {
			return nil, nil, err
		}

		var knownMicrocredits int64
		priceUnknown := true
		if prep.Order != nil {
			knownMicrocredits = prep.Order.AmountMicrocredits
			priceUnknown = false
			totalKnown += knownMicrocredits
		} else {
			priceUnknownCount++
		}

		summary := ProjectAssetBatchPreflightItem{
			Position:             idx,
			AssetID:              asset.ID,
			AssetName:            asset.Title,
			AssetCategory:        string(asset.Category),
			BaseVersionID:        asset.PrimaryVersionID,
			ReferenceSource:      refSource,
			ReferenceCount:       len(refResourceIDs),
			ReferenceResourceIDs: refResourceIDs,
			Operation:            operation,
			Prompt:               prompt,
			KnownMicrocredits:    knownMicrocredits,
			PriceUnknown:         priceUnknown,
		}

		runTaskReq := taskReq
		runTaskReq.creationPrepare = nil

		prepared = append(prepared, preparedBatchItem{
			Summary: summary,
			TaskReq: runTaskReq,
			Snapshot: projectAssetBatchItemSnapshot{
				AssetID:              asset.ID,
				BaseVersionID:        asset.PrimaryVersionID,
				ReferenceSource:      refSource,
				ReferenceResourceIDs: refResourceIDs,
				Operation:            operation,
				Prompt:               prompt,
				Provider:             runTaskReq.Provider,
				Model:                runTaskReq.Model,
				LogicalModelID:       runTaskReq.LogicalModelID,
				Config:               sanitizeBatchConfigForPersistence(cfgCopy),
			},
		})
	}

	if totalKnown > 0 {
		account, err := s.repo.CreditAccount(userID)
		if err != nil {
			return nil, nil, err
		}
		if account.AvailableMicrocredits < totalKnown {
			return nil, nil, BadAuthRequest("积分不足，当前可用积分无法覆盖批次已知生成费用")
		}
	}

	itemsOut := make([]ProjectAssetBatchPreflightItem, len(prepared))
	for i := range prepared {
		itemsOut[i] = prepared[i].Summary
	}
	digest := computeBatchHash(map[string]any{
		"projectId":              projectID,
		"model":                  strings.TrimSpace(req.Model),
		"logicalModelId":         strings.TrimSpace(req.LogicalModelID),
		"totalKnownMicrocredits": totalKnown,
		"priceUnknownCount":      priceUnknownCount,
		"items":                  itemsOut,
	})

	return &ProjectAssetBatchPreflightResult{
		ProjectID:              projectID,
		Model:                  strings.TrimSpace(req.Model),
		Provider:               strings.TrimSpace(req.Provider),
		TotalKnownMicrocredits: totalKnown,
		PriceUnknownCount:      priceUnknownCount,
		PreflightDigest:        digest,
		Items:                  itemsOut,
	}, prepared, nil
}

func (s *Service) PreflightProjectAssetBatch(userID, projectID string, req PreflightProjectAssetBatchRequest) (*ProjectAssetBatchPreflightResult, error) {
	res, _, err := s.evaluateProjectAssetBatch(userID, projectID, req)
	return res, err
}

func computeBatchRequestHash(req CreateProjectAssetBatchRequest) string {
	return computeBatchHash(map[string]any{
		"provider":       strings.TrimSpace(req.Provider),
		"model":          strings.TrimSpace(req.Model),
		"logicalModelId": strings.TrimSpace(req.LogicalModelID),
		"config":         sanitizeBatchConfigForPersistence(req.Config),
		"items":          req.Items,
	})
}

func (s *Service) CreateProjectAssetBatch(userID, projectID string, req CreateProjectAssetBatchRequest) (*ProjectAssetBatchView, error) {
	idemKey := strings.TrimSpace(req.IdempotencyKey)
	if idemKey == "" || len(idemKey) > 120 {
		return nil, BadAuthRequest("缺少有效的批次幂等键 idempotencyKey")
	}
	if strings.TrimSpace(req.PreflightDigest) == "" {
		return nil, BadAuthRequest("请先完成批次清单预检")
	}

	requestHash := computeBatchRequestHash(req)

	// Fast path: check existing batch by idempotency key BEFORE re-running preflight so replays succeed even if assets later evolved
	if existingBatch, existingItems, err := s.repo.FindProjectAssetGenerationBatchByIdempotency(userID, projectID, idemKey); err == nil && existingBatch != nil {
		if existingBatch.RequestHash != requestHash {
			return nil, &AppError{
				Status:  409,
				Code:    409,
				Reason:  "batch_idempotency_conflict",
				Message: "幂等键已绑定不同的批量生成请求清单",
			}
		}
		return s.reconcileAndBuildBatchView(userID, existingBatch, existingItems)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	preflight, prepared, err := s.evaluateProjectAssetBatch(userID, projectID, req.PreflightProjectAssetBatchRequest)
	if err != nil {
		return nil, err
	}
	if preflight.PreflightDigest != strings.TrimSpace(req.PreflightDigest) {
		return nil, &AppError{
			Status:  409,
			Code:    409,
			Reason:  "batch_preflight_stale",
			Message: "批次预检摘要已过期（资产版本、参考图或计费配置已变化），请重新预检确认",
		}
	}

	now := time.Now()
	batchID := newID()
	cfgSummaryBytes, _ := json.Marshal(map[string]any{
		"provider":       strings.TrimSpace(req.Provider),
		"model":          strings.TrimSpace(req.Model),
		"logicalModelId": strings.TrimSpace(req.LogicalModelID),
		"config":         sanitizeBatchConfigForPersistence(req.Config),
	})

	batchModel := &model.ProjectAssetGenerationBatch{
		ID:                     batchID,
		UserID:                 userID,
		ProjectID:              projectID,
		IdempotencyKey:         idemKey,
		RequestHash:            requestHash,
		PreflightDigest:        preflight.PreflightDigest,
		Status:                 "running",
		ConfigSummaryJSON:      string(cfgSummaryBytes),
		TotalKnownMicrocredits: preflight.TotalKnownMicrocredits,
		PriceUnknownCount:      preflight.PriceUnknownCount,
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	itemModels := make([]model.ProjectAssetGenerationBatchItem, len(prepared))
	for i, p := range prepared {
		snapshot, err := s.encodeBatchItemSnapshot(p)
		if err != nil {
			return nil, err
		}
		itemModels[i] = model.ProjectAssetGenerationBatchItem{
			ID:                 newID(),
			BatchID:            batchID,
			UserID:             userID,
			ProjectID:          projectID,
			Position:           p.Summary.Position,
			AssetID:            p.Summary.AssetID,
			AssetName:          p.Summary.AssetName,
			AssetCategory:      p.Summary.AssetCategory,
			BaseVersionID:      p.Summary.BaseVersionID,
			ReferenceSource:    p.Summary.ReferenceSource,
			InputSnapshotJSON:  snapshot,
			Status:             "pending",
			Attempt:            1,
			SubmissionKey:      batchSubmissionKey(batchID, p.Summary.Position, 1),
			AttemptHistoryJSON: "[]",
			KnownMicrocredits:  p.Summary.KnownMicrocredits,
			PriceUnknown:       p.Summary.PriceUnknown,
			CreatedAt:          now,
			UpdatedAt:          now,
		}
	}

	savedBatch, savedItems, reused, err := s.repo.CreateProjectAssetGenerationBatch(batchModel, itemModels)
	if err != nil {
		return nil, err
	}
	if reused {
		if savedBatch.RequestHash != requestHash {
			return nil, &AppError{
				Status:  409,
				Code:    409,
				Reason:  "batch_idempotency_conflict",
				Message: "幂等键已绑定不同的批量生成请求清单",
			}
		}
		return s.reconcileAndBuildBatchView(userID, savedBatch, savedItems)
	}

	limit := len(savedItems)
	if s.batchDispatchLimitForTest > 0 && s.batchDispatchLimitForTest < limit {
		limit = s.batchDispatchLimitForTest
	}
	for i := 0; i < limit; i++ {
		if err := s.dispatchBatchItem(userID, savedBatch.ID, &savedItems[i], &prepared[i].TaskReq); err != nil {
			if extractAppErrorReason(err) == string(ReasonTaskActiveLimitExceeded) {
				break
			}
			return nil, err
		}
	}
	s.wakeTaskDispatcher()

	freshBatch, freshItems, err := s.repo.ProjectAssetGenerationBatchWithItems(userID, projectID, savedBatch.ID)
	if err != nil {
		return nil, err
	}
	return s.reconcileAndBuildBatchView(userID, freshBatch, freshItems)
}

func (s *Service) dispatchBatchItem(userID, batchID string, item *model.ProjectAssetGenerationBatchItem, taskReq *CreateTaskRequest) error {
	allowed, err := s.repo.CanDispatchProjectAssetBatchItem(batchID, item.ID, item.Attempt)
	if err != nil || !allowed {
		return err
	}
	if existingTask, findErr := s.repo.TaskByCreationSubmissionID(userID, item.SubmissionKey); findErr == nil && existingTask != nil {
		return s.repo.BindProjectAssetBatchItemTask(batchID, item.ID, item.Attempt, existingTask.ID, "running", "", "")
	} else if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
		return findErr
	}
	var createdTask *model.Task
	var createErr error
	if taskReq == nil {
		var prepared *preparedBatchItem
		prepared, createErr = s.preparePersistedBatchItem(userID, item)
		if createErr == nil {
			if prepared.Summary.PriceUnknown != item.PriceUnknown || prepared.Summary.KnownMicrocredits != item.KnownMicrocredits {
				createErr = &AppError{Status: 409, Code: 409, Reason: "batch_preflight_stale", Message: "等待期间模型价格已变化，请重新预检确认"}
			} else {
				taskReq = &prepared.TaskReq
			}
		}
	}
	if createErr == nil {
		taskReq.creationSubmissionID = item.SubmissionKey
		createdTask, createErr = s.CreateTask(userID, *taskReq)
	}
	if createErr != nil {
		if existingTask, findErr := s.repo.TaskByCreationSubmissionID(userID, item.SubmissionKey); findErr == nil && existingTask != nil {
			return s.repo.BindProjectAssetBatchItemTask(batchID, item.ID, item.Attempt, existingTask.ID, "running", "", "")
		} else if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}
		if errors.Is(createErr, repository.ErrBatchItemAlreadyDispatched) {
			return nil
		}
		reason := extractAppErrorReason(createErr)
		if reason == string(ReasonTaskActiveLimitExceeded) {
			return createErr
		}
		if reason == "" {
			reason = "task_submission_failed"
		}
		return s.repo.BindProjectAssetBatchItemTask(batchID, item.ID, item.Attempt, "", "failed", reason, sanitizeBatchErrorMessage(createErr.Error()))
	}
	return s.repo.BindProjectAssetBatchItemTask(batchID, item.ID, item.Attempt, createdTask.ID, "running", "", "")
}

func (s *Service) dispatchPendingProjectAssetBatchItems(ctx context.Context) error {
	policy, err := s.RuntimePolicy()
	if err != nil {
		return err
	}
	items, err := s.repo.WithContext(ctx).PendingProjectAssetBatchItems(MaxProjectAssetBatchItems, policy.Task.ActiveTaskLimit)
	if err != nil {
		return err
	}
	fullUsers := make(map[string]bool)
	for i := range items {
		if ctx.Err() != nil || s.IsDraining() {
			return ctx.Err()
		}
		item := &items[i]
		if fullUsers[item.UserID] {
			continue
		}
		if err := s.dispatchBatchItem(item.UserID, item.BatchID, item, nil); err != nil {
			if extractAppErrorReason(err) == string(ReasonTaskActiveLimitExceeded) {
				fullUsers[item.UserID] = true
				continue
			}
			if !errors.Is(err, repository.ErrBatchItemAlreadyDispatched) {
				return err
			}
		}
	}
	return nil
}

func sanitizeBatchErrorMessage(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "生成任务失败"
	}
	// Strip potential secret or bearer fragments
	for _, marker := range []string{"sk-", "Bearer ", "apiKey", "secretKey"} {
		if strings.Contains(trimmed, marker) {
			return "上游生成请求失败，请检查模型配置后重试"
		}
	}
	return trimmed
}

func (s *Service) ListProjectAssetBatches(userID, projectID string) ([]ProjectAssetBatchView, error) {
	if _, err := s.repo.ProjectForUser(userID, projectID); err != nil {
		return nil, err
	}
	batches, err := s.repo.ListProjectAssetGenerationBatches(userID, projectID, 20)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectAssetBatchView, 0, len(batches))
	for i := range batches {
		_, items, itemErr := s.repo.ProjectAssetGenerationBatchWithItems(userID, projectID, batches[i].ID)
		if itemErr != nil {
			continue
		}
		view, viewErr := s.reconcileAndBuildBatchView(userID, &batches[i], items)
		if viewErr != nil {
			continue
		}
		out = append(out, *view)
	}
	return out, nil
}

func (s *Service) GetProjectAssetBatch(userID, projectID, batchID string) (*ProjectAssetBatchView, error) {
	batch, items, err := s.repo.ProjectAssetGenerationBatchWithItems(userID, projectID, strings.TrimSpace(batchID))
	if err != nil {
		return nil, err
	}
	return s.reconcileAndBuildBatchView(userID, batch, items)
}

func (s *Service) StopProjectAssetBatch(userID, projectID, batchID string) (*ProjectAssetBatchView, error) {
	batch, items, err := s.repo.StopUnsubmittedProjectAssetBatchItems(userID, projectID, strings.TrimSpace(batchID))
	if err != nil {
		return nil, err
	}
	return s.reconcileAndBuildBatchView(userID, batch, items)
}

func (s *Service) CancelProjectAssetBatchTasks(ctx context.Context, userID, projectID, batchID string) (*ProjectAssetBatchView, error) {
	batch, items, err := s.repo.ProjectAssetGenerationBatchWithItems(userID, projectID, strings.TrimSpace(batchID))
	if err != nil {
		return nil, err
	}
	for i := range items {
		item := &items[i]
		if item.TaskID == "" {
			continue
		}
		task, taskErr := s.repo.Task(item.TaskID)
		if taskErr != nil || task == nil {
			continue
		}
		if task.Status == model.TaskStatusQueued || task.Status == model.TaskStatusRunning {
			_, _ = s.CancelTask(ctx, userID, task.ID)
		}
	}
	freshBatch, freshItems, err := s.repo.ProjectAssetGenerationBatchWithItems(userID, projectID, batch.ID)
	if err != nil {
		return nil, err
	}
	return s.reconcileAndBuildBatchView(userID, freshBatch, freshItems)
}

func (s *Service) RetryProjectAssetBatchItem(userID, projectID, batchID, itemID string) (*ProjectAssetBatchView, error) {
	batch, items, err := s.repo.ProjectAssetGenerationBatchWithItems(userID, projectID, strings.TrimSpace(batchID))
	if err != nil {
		return nil, err
	}
	// First reconcile with latest task truth
	view, err := s.reconcileAndBuildBatchView(userID, batch, items)
	if err != nil {
		return nil, err
	}
	var targetModel *model.ProjectAssetGenerationBatchItem
	var targetView *ProjectAssetBatchItemView
	for i := range items {
		if items[i].ID == strings.TrimSpace(itemID) {
			targetModel = &items[i]
			break
		}
	}
	for i := range view.Items {
		if view.Items[i].ID == strings.TrimSpace(itemID) {
			targetView = &view.Items[i]
			break
		}
	}
	if targetModel == nil || targetView == nil {
		return nil, gorm.ErrRecordNotFound
	}
	if targetView.Status != "failed" || !targetView.CanRetry {
		return nil, BadAuthRequest("仅已确认失败且无待核对账单或候选的生成项支持重试")
	}

	prepared, err := s.preparePersistedBatchItem(userID, targetModel)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.encodeBatchItemSnapshot(*prepared)
	if err != nil {
		return nil, err
	}

	history := append([]ProjectAssetBatchAttemptRecord(nil), targetView.AttemptHistory...)
	history = append(history, ProjectAssetBatchAttemptRecord{
		Attempt:      targetModel.Attempt,
		TaskID:       targetModel.TaskID,
		Status:       targetView.Status,
		ErrorReason:  targetView.ErrorReason,
		ErrorMessage: targetView.ErrorMessage,
		UpdatedAt:    time.Now(),
	})
	historyBytes, _ := json.Marshal(history)
	nextSubmissionKey := batchSubmissionKey(batch.ID, targetModel.Position, targetModel.Attempt+1)

	updatedItem, err := s.repo.PrepareRetryProjectAssetBatchItem(userID, projectID, batch.ID, targetModel.ID, targetModel.Attempt, nextSubmissionKey, string(historyBytes), snapshot)
	if err != nil {
		return nil, err
	}

	if err := s.dispatchBatchItem(userID, batch.ID, updatedItem, &prepared.TaskReq); err != nil && extractAppErrorReason(err) != string(ReasonTaskActiveLimitExceeded) {
		return nil, err
	}
	s.wakeTaskDispatcher()

	freshBatch, freshItems, err := s.repo.ProjectAssetGenerationBatchWithItems(userID, projectID, batch.ID)
	if err != nil {
		return nil, err
	}
	return s.reconcileAndBuildBatchView(userID, freshBatch, freshItems)
}

func (s *Service) preparePersistedBatchItem(userID string, item *model.ProjectAssetGenerationBatchItem) (*preparedBatchItem, error) {
	var snap projectAssetBatchItemSnapshot
	if err := json.Unmarshal([]byte(item.InputSnapshotJSON), &snap); err != nil {
		return nil, BadAuthRequest("批次项快照损坏，无法派发")
	}
	config := maps.Clone(snap.Config)
	if snap.EncryptedConfig != "" {
		raw, err := s.decryptSettingSecret(snap.EncryptedConfig)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &config); err != nil {
			return nil, BadAuthRequest("批次项配置损坏，无法派发")
		}
	}
	if config == nil {
		config = map[string]any{}
	}
	if item.TaskID != "" {
		if origTask, err := s.repo.Task(item.TaskID); err == nil && origTask != nil {
			var origInput map[string]any
			if json.Unmarshal([]byte(origTask.InputJSON), &origInput) == nil {
				if origCfg, ok := origInput["config"].(map[string]any); ok {
					for _, key := range []string{"apiKey", "secretKey", "headers", "runningHubWalletApiKey", "runningHubUploadApiKey"} {
						if val, exists := origCfg[key]; exists && config[key] == nil {
							config[key] = val
						}
					}
				}
			}
		}
	}
	_, prepared, err := s.evaluateProjectAssetBatch(userID, item.ProjectID, PreflightProjectAssetBatchRequest{
		Provider: snap.Provider, Model: snap.Model, LogicalModelID: snap.LogicalModelID, Config: config,
		Items: []ProjectAssetBatchItemInput{{
			AssetID: snap.AssetID, ExpectedVersionID: snap.BaseVersionID, ReferenceSource: snap.ReferenceSource,
			Prompt: snap.Prompt, Operation: snap.Operation,
		}},
	})
	if err != nil {
		return nil, err
	}
	return &prepared[0], nil
}

func extractTaskOutputImageURL(raw string) string {
	var parsed struct {
		URL    string `json:"url"`
		Images []struct {
			URL        string `json:"url"`
			ResourceID string `json:"resourceId"`
		} `json:"images"`
	}
	if json.Unmarshal([]byte(raw), &parsed) != nil {
		return ""
	}
	if strings.TrimSpace(parsed.URL) != "" {
		return strings.TrimSpace(parsed.URL)
	}
	if len(parsed.Images) > 0 {
		if strings.TrimSpace(parsed.Images[0].URL) != "" {
			return strings.TrimSpace(parsed.Images[0].URL)
		}
		if strings.TrimSpace(parsed.Images[0].ResourceID) != "" {
			return "/api/resources/" + strings.TrimSpace(parsed.Images[0].ResourceID) + "/file"
		}
	}
	return ""
}

func (s *Service) reconcileAndBuildBatchView(userID string, batch *model.ProjectAssetGenerationBatch, items []model.ProjectAssetGenerationBatchItem) (*ProjectAssetBatchView, error) {
	views := make([]ProjectAssetBatchItemView, len(items))
	dirty := false

	pendingCount := 0
	runningCount := 0
	succeededCount := 0
	failedCount := 0
	stoppedCount := 0
	cancelledCount := 0
	reconcileCount := 0

	for i := range items {
		item := &items[i]
		origStatus := item.Status
		origTaskID := item.TaskID
		origErrReason := item.ErrorReason
		origErrMsg := item.ErrorMessage

		if item.TaskID == "" && item.SubmissionKey != "" {
			if recoveredTask, err := s.repo.TaskByCreationSubmissionID(userID, item.SubmissionKey); err == nil && recoveredTask != nil {
				item.TaskID = recoveredTask.ID
				if item.Status == "pending" {
					item.Status = "running"
				}
			}
		}

		var candidate *AssetImageCandidate
		outputURL := ""

		if item.TaskID != "" {
			task, err := s.repo.Task(item.TaskID)
			if err != nil || task == nil {
				item.Status = "reconcile_required"
				item.ErrorReason = "task_record_missing"
				item.ErrorMessage = "已绑定任务记录缺失，需核对执行与计费状态"
			} else {
				outputURL = extractTaskOutputImageURL(task.ResultJSON)
				candidate = candidateFromResult(task.ResultJSON)
				hasMediaCheckpoint := strings.TrimSpace(task.MediaRecoveryJSON) != "" || task.CanRecoverMedia

				switch task.Status {
				case model.TaskStatusQueued, model.TaskStatusRunning:
					if task.CancellationRequestedAt != nil || task.ProviderCancelStatus == model.ProviderCancelStatusRequested || task.ProviderCancelStatus == model.ProviderCancelStatusUncertain {
						item.Status = "cancel_requested"
						if task.ProviderCancelStatus == model.ProviderCancelStatusUncertain {
							item.ErrorReason = "provider_cancel_unsupported"
							item.ErrorMessage = "已请求取消，但上游渠道不支持即时中断，请等待最终结果与账单结算"
						} else {
							item.ErrorReason = "cancel_pending_confirmation"
							item.ErrorMessage = "已向上游发起取消请求，等待确认"
						}
					} else {
						item.Status = "running"
						item.ErrorReason = ""
						item.ErrorMessage = ""
					}
				case model.TaskStatusSucceeded:
					// 成功任务会保留媒体检查点用于重放去重，不代表待恢复；只看候选是否落地。
					if candidate == nil {
						item.Status = "reconcile_required"
						item.ErrorReason = "candidate_or_media_reconcile_required"
						item.ErrorMessage = "上游已返回结果但媒体或候选状态待恢复核对，禁止盲目重发"
					} else {
						item.Status = "succeeded"
						item.ErrorReason = ""
						item.ErrorMessage = ""
					}
				case model.TaskStatusCancelled:
					item.Status = "cancelled"
					item.ErrorReason = "task_cancelled"
					item.ErrorMessage = "生成任务已取消"
				case model.TaskStatusFailed:
					if hasMediaCheckpoint || candidate != nil {
						item.Status = "reconcile_required"
						item.ErrorReason = "media_recovery_pending"
						item.ErrorMessage = "任务存在待恢复媒体检查点或未决结果，请先在任务中心恢复或核对账单"
					} else {
						item.Status = "failed"
						if item.ErrorReason == "" {
							item.ErrorReason = "task_failed"
						}
						item.ErrorMessage = sanitizeBatchErrorMessage(task.Error)
					}
				default:
					item.Status = "reconcile_required"
					item.ErrorReason = "unknown_task_status"
					item.ErrorMessage = "任务状态待核对"
				}
			}
		}

		if item.Status != origStatus || item.TaskID != origTaskID || item.ErrorReason != origErrReason || item.ErrorMessage != origErrMsg {
			dirty = true
		}

		switch item.Status {
		case "pending":
			pendingCount++
		case "running", "cancel_requested":
			runningCount++
		case "succeeded":
			succeededCount++
		case "failed":
			failedCount++
		case "stopped":
			stoppedCount++
		case "cancelled":
			cancelledCount++
		case "reconcile_required":
			reconcileCount++
		}

		var history []ProjectAssetBatchAttemptRecord
		_ = json.Unmarshal([]byte(item.AttemptHistoryJSON), &history)

		canRetry := item.Status == "failed" && candidate == nil

		views[i] = ProjectAssetBatchItemView{
			ID:                item.ID,
			BatchID:           item.BatchID,
			Position:          item.Position,
			AssetID:           item.AssetID,
			AssetName:         item.AssetName,
			AssetCategory:     item.AssetCategory,
			BaseVersionID:     item.BaseVersionID,
			ReferenceSource:   item.ReferenceSource,
			Status:            item.Status,
			TaskID:            item.TaskID,
			Attempt:           item.Attempt,
			AttemptHistory:    history,
			KnownMicrocredits: item.KnownMicrocredits,
			PriceUnknown:      item.PriceUnknown,
			ErrorReason:       item.ErrorReason,
			ErrorMessage:      item.ErrorMessage,
			CanRetry:          canRetry,
			OutputURL:         outputURL,
			Candidate:         candidate,
			UpdatedAt:         item.UpdatedAt,
		}
	}

	nextBatchStatus := batch.Status
	if runningCount > 0 || pendingCount > 0 {
		nextBatchStatus = "running"
	} else if reconcileCount > 0 {
		nextBatchStatus = "reconcile_required"
	} else if succeededCount == len(items) && len(items) > 0 {
		nextBatchStatus = "completed"
	} else if stoppedCount == len(items) && len(items) > 0 {
		nextBatchStatus = "stopped"
	} else if succeededCount > 0 && (failedCount > 0 || stoppedCount > 0 || cancelledCount > 0) {
		nextBatchStatus = "partial_failed"
	} else if failedCount > 0 || cancelledCount > 0 {
		nextBatchStatus = "partial_failed"
	} else if stoppedCount > 0 {
		nextBatchStatus = "stopped"
	}

	if nextBatchStatus != batch.Status {
		batch.Status = nextBatchStatus
		dirty = true
	}

	if dirty {
		if err := s.repo.UpdateProjectAssetBatchAndItems(batch, items); err != nil {
			return nil, err
		}
	}

	return &ProjectAssetBatchView{
		ID:                     batch.ID,
		ProjectID:              batch.ProjectID,
		IdempotencyKey:         batch.IdempotencyKey,
		Status:                 batch.Status,
		TotalCount:             len(items),
		TotalKnownMicrocredits: batch.TotalKnownMicrocredits,
		PriceUnknownCount:      batch.PriceUnknownCount,
		StopRequestedAt:        batch.StopRequestedAt,
		Items:                  views,
		CreatedAt:              batch.CreatedAt,
		UpdatedAt:              batch.UpdatedAt,
	}, nil
}
