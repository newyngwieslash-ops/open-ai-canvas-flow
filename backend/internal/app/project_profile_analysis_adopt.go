package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type AdoptProfileAnalysisRequest struct {
	ExpectedVersionID string   `json:"expectedVersionId"`
	SelectedFields    []string `json:"selectedFields"`
	IdempotencyKey    string   `json:"idempotencyKey"`
}
type AdoptProfileAnalysisResponse struct {
	AdoptedVersionID string              `json:"adoptedVersionId"`
	Replayed         bool                `json:"replayed"`
	Asset            ProjectAssetSummary `json:"asset"`
}

func profileDigest(value any) string {
	raw, _ := json.Marshal(value)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
func profileKeyHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func profileSelectedFields(req AdoptProfileAnalysisRequest, category model.AssetCategory) ([]string, error) {
	if !validProfileID(req.ExpectedVersionID, 36) || len(req.IdempotencyKey) < 16 || len(req.IdempotencyKey) > 64 || len(req.SelectedFields) == 0 {
		return nil, profileError(400, "invalid_request")
	}
	for _, r := range req.IdempotencyKey {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return nil, profileError(400, "invalid_request")
		}
	}
	allowed := map[string]bool{}
	for _, field := range profileFields(category) {
		allowed[field] = true
	}
	fields := append([]string(nil), req.SelectedFields...)
	seen := map[string]bool{}
	for _, field := range fields {
		if !allowed[field] || seen[field] {
			return nil, profileError(400, "invalid_request")
		}
		seen[field] = true
	}
	sort.Strings(fields)
	return fields, nil
}
func profileReceiptReplay(receipt *ProfileAdoptionReceipt, keyHash, digest string) (string, error) {
	if receipt.IdempotencyKeyHash != keyHash {
		return "", profileError(409, "already_adopted")
	}
	if receipt.RequestDigest != digest {
		return "", profileError(409, "idempotency_conflict")
	}
	return receipt.AdoptedVersionID, nil
}
func (s *Service) AdoptProjectProfileAnalysis(userID, projectID, assetID, taskID string, req AdoptProfileAnalysisRequest) (AdoptProfileAnalysisResponse, error) {
	out := AdoptProfileAnalysisResponse{}
	asset, err := s.profileAsset(userID, projectID, assetID)
	if err != nil {
		return out, err
	}
	if !validProfileID(taskID, 36) {
		return out, profileError(400, "invalid_request")
	}
	fields, err := profileSelectedFields(req, asset.Category)
	if err != nil {
		return out, err
	}
	digest := profileDigest(map[string]any{"schemaVersion": 1, "userId": userID, "projectId": projectID, "assetId": assetID, "taskId": taskID, "expectedVersionId": req.ExpectedVersionID, "selectedFields": fields})
	keyHash := profileKeyHash(req.IdempotencyKey)
	err = s.repo.WithStoryboardAssetWrite(projectID, assetID, func(owned *repository.Repository) error {
	return owned.ProfileWriteTransaction(func(tx *repository.Repository) error {
		current, err := tx.LockProfileAsset(userID, assetID)
		if err != nil {
			return err
		}
		if _, err := tx.ProjectForUser(userID, projectID); err != nil {
			return profileError(404, "not_found")
		}
		linked, err := tx.ProjectAssetLinked(projectID, assetID)
		if err != nil {
			return err
		}
		if !linked {
			return profileError(404, "not_found")
		}
		task, err := tx.LockImageCandidateTask(userID, taskID)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			versions, err := tx.AssetVersions(assetID)
			if err != nil {
				return err
			}
			for _, version := range versions {
				var def struct {
					Receipt *ProfileAdoptionReceipt `json:"_profileAnalysisAdoption"`
				}
				if json.Unmarshal([]byte(version.DefinitionJSON), &def) != nil || def.Receipt == nil {
					continue
				}
				r := def.Receipt
				if r.AdoptedVersionID != version.ID || r.TaskID != taskID || r.AssetID != assetID || r.ProjectID != projectID || r.SchemaVersion != 1 || r.Operation != profileAnalysisOperation {
					continue
				}
				out.AdoptedVersionID, err = profileReceiptReplay(r, keyHash, digest)
				out.Replayed = err == nil
				return err
			}
			return profileError(404, "not_found")
		}
		if err != nil {
			return err
		}
		if task.ProjectID != projectID || task.Operation != profileAnalysisOperation || task.Type != "canvas_text" {
			return profileError(404, "not_found")
		}
		b, err := profileBinding(task.InputJSON)
		if err != nil {
			return err
		}
		if b.ProjectID != projectID || b.AssetID != assetID || b.Category != current.Category {
			return profileError(404, "not_found")
		}
		if task.Status != model.TaskStatusSucceeded {
			return profileError(409, "not_adoptable")
		}
		result, err := validatedProfileResult(*task)
		if err != nil {
			return err
		}
		if result.Adoption != nil {
			out.AdoptedVersionID, err = profileReceiptReplay(result.Adoption, keyHash, digest)
			out.Replayed = err == nil
			return err
		}
		base, definition, err := profileBaselineDefinition(tx, current.ID, b.BaseVersionID)
		if err != nil {
			return err
		}
		if base == nil {
			return profileBaselineUnavailable()
		}
		if b.BaseVersionID != req.ExpectedVersionID || current.PrimaryVersionID != req.ExpectedVersionID {
			return projectAssetVersionConflict()
		}
		if err := tx.RequireProfileSource(task, true); err != nil {
			return err
		}
		for _, field := range fields {
			if result.Draft[field] == "" {
				return profileError(409, "not_adoptable")
			}
		}
		reps, err := tx.AssetRepresentations(base.ID)
		if err != nil {
			return err
		}
		voice, err := tx.CharacterVoiceBinding(base.ID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		next := *base
		next.ID = newID()
		next.Version++
		next.CreatedAt = time.Now()
		next.UpdatedAt = next.CreatedAt
		receipt := &ProfileAdoptionReceipt{SchemaVersion: 1, Operation: profileAnalysisOperation, TaskID: task.ID, TaskCreatedAt: task.CreatedAt, ProjectID: projectID, AssetID: assetID, BaseVersionID: base.ID, AdoptedVersionID: next.ID, Source: b.Source, SelectedFields: fields, IdempotencyKeyHash: keyHash, RequestDigest: digest, DraftDigest: profileDigest(result.Draft), AdoptedAt: next.CreatedAt}
		for _, field := range fields {
			definition[field], _ = json.Marshal(result.Draft[field])
		}
		definition[profileAdoptionKey], _ = json.Marshal(receipt)
		raw, err := json.Marshal(definition)
		if err != nil {
			return err
		}
		next.DefinitionJSON = string(raw)
		updated := *current
		updated.PrimaryVersionID = next.ID
		updated.UpdatedAt = next.UpdatedAt
		var payload map[string]json.RawMessage
		if json.Unmarshal([]byte(current.PayloadJSON), &payload) != nil || payload == nil {
			return profileError(409, "not_adoptable")
		}
		payload["primaryVersionId"], _ = json.Marshal(next.ID)
		payload["updatedAt"], _ = json.Marshal(next.UpdatedAt.Format(time.RFC3339Nano))
		if current.Kind == "entity" {
			var data map[string]json.RawMessage
			_ = json.Unmarshal(payload["data"], &data)
			if data == nil {
				data = map[string]json.RawMessage{}
			}
			data["definition"] = raw
			payload["data"], _ = json.Marshal(data)
		}
		raw, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		updated.PayloadJSON = string(raw)
		for i := range reps {
			reps[i].ID = newID()
			reps[i].AssetVersionID = next.ID
			reps[i].CreatedAt = next.CreatedAt
		}
		if voice != nil {
			copy := *voice
			copy.ID = newID()
			copy.AssetVersionID = next.ID
			voice = &copy
		}
		result.Adoption = receipt
		encoded, err := json.Marshal(map[string]any{"mode": "text", "assetProfileAnalysis": result})
		if err != nil {
			return err
		}
		if err := tx.CommitProfileAnalysisVersion(projectID, base.ID, &updated, &next, reps, voice, task, string(encoded)); err != nil {
			return err
		}
		out.AdoptedVersionID = next.ID
		out.Replayed = false
		return nil
	})
	})
	if err != nil {
		return out, profileTransactionError(err)
	}
	latest, err := s.repo.AssetForUser(userID, assetID)
	if err != nil {
		return out, err
	}
	out.Asset, err = s.projectAssetSummary(userID, projectID, latest)
	return out, err
}
