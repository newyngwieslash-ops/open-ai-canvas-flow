package repository

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/assets"
	"yingce/backend/internal/model"
)

// Caller holds the lifecycle lock before any business row locks. Explicit IDs and
// arbitrary documents share the same collector and deterministic resource order.
func (r *Repository) requireReferenceResources(userID string, global bool, explicitIDs []string, documents ...string) error {
	ids := map[string]struct{}{}
	for _, id := range explicitIDs {
		if id = strings.TrimSpace(id); id != "" {
			ids[id] = struct{}{}
		}
	}
	for _, raw := range documents {
		if err := assets.CollectOwnedDocumentReferences(raw, ids); err != nil {
			return err
		}
	}
	for _, id := range assets.SortedIDs(ids) {
		query := r.db.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND status = ?", id, model.ResourceStatusReady)
		if !global {
			query = query.Where("user_id = ?", userID)
		}
		var resource model.Resource
		if err := query.First(&resource).Error; err != nil {
			return err
		}
	}
	return nil
}

// Logs also accept ordinary diagnostic text. JSON-shaped malformed payloads are
// rejected instead of silently dropping references; plain text remains plain text.
func referencePayloadDocument(raw string) (string, error) {
	text := strings.TrimSpace(raw)
	if text == "" || json.Valid([]byte(text)) {
		return raw, nil
	}
	if strings.HasPrefix(text, "{") || strings.HasPrefix(text, "[") {
		return "", gorm.ErrInvalidData
	}
	encoded, err := json.Marshal(raw)
	return string(encoded), err
}

func (r *Repository) requireResultResources(result *model.Result) error {
	payload, err := referencePayloadDocument(result.Payload)
	if err != nil {
		return err
	}
	return r.requireReferenceResources(result.UserID, false, nil, referenceURLDocument(result.URL), payload)
}

// The compatibility API is used by logs and storage migration. Handle typed
// resource-bearing values here as well as slices, rather than trusting callers.
func rawReferenceBearing(value reflect.Value) bool {
	if !value.IsValid() {
		return false
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
		for i := 0; i < value.Len(); i++ {
			if rawReferenceBearing(value.Index(i)) {
				return true
			}
		}
		return false
	}
	switch value.Interface().(type) {
	case model.Task, model.Result, model.TaskLog, model.Asset, model.CanvasProject, model.Resource,
		model.Project, model.StyleProfile, model.AssetVersion, model.AssetRepresentation, model.VoiceProfile, model.ShotArtifact, model.ShotRevision, model.ShotAssetReference, model.ProjectAssetLink, model.ProjectAssetCandidate, model.WorkflowTemplateVersion, model.WorkflowStepInstance, model.CreationRun, model.CreationSubmission, model.Tool, model.WorkbenchTool, model.WorkbenchPreset, model.SystemSetting, model.ArkPrivateAssetBinding, model.CloudAgentResourceLease:
		return true
	}
	return false
}

func (r *Repository) rawReferenceWrite(value any, save bool) error {
	return r.resourceLifecycleTransaction(func(tx *Repository) error {
		if err := tx.requireRawReferences(reflect.ValueOf(value)); err != nil {
			return err
		}
		if save {
			return tx.db.Save(value).Error
		}
		return tx.db.Create(value).Error
	})
}

func (r *Repository) requireRawReferences(value reflect.Value) error {
	if !value.IsValid() {
		return gorm.ErrInvalidData
	}
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return gorm.ErrInvalidData
		}
		value = value.Elem()
	}
	if value.Kind() == reflect.Slice || value.Kind() == reflect.Array {
		for i := 0; i < value.Len(); i++ {
			if err := r.requireRawReferences(value.Index(i)); err != nil {
				return err
			}
		}
		return nil
	}
	switch row := value.Interface().(type) {
	case model.Task:
		if err := r.requireUnchangedMediaCheckpoint(&row); err != nil {
			return err
		}
		return r.requireReferenceResources(row.UserID, false, nil, row.InputJSON, row.ResultJSON)
	case model.Result:
		return r.requireResultResources(&row)
	case model.TaskLog:
		payload, err := referencePayloadDocument(row.Payload)
		if err != nil {
			return err
		}
		return r.requireLiveDocumentResources(row.UserID, payload)
	case model.Asset:
		current, err := r.AssetForUser(row.UserID, row.ID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if row.Status == model.AssetVersionStatusDeleting || (current != nil && current.Status == model.AssetVersionStatusDeleting) {
			return ErrResourceDeleting
		}
		return r.requireLiveDocumentResources(row.UserID, row.PayloadJSON)
	case model.CanvasProject:
		return r.requireLiveDocumentResources(row.UserID, row.PayloadJSON)
	case model.Resource, model.Project, model.StyleProfile, model.AssetVersion, model.AssetRepresentation, model.VoiceProfile, model.ShotArtifact, model.ShotRevision, model.ShotAssetReference, model.ProjectAssetLink, model.ProjectAssetCandidate, model.WorkflowTemplateVersion, model.WorkflowStepInstance, model.CreationRun, model.CreationSubmission, model.Tool, model.WorkbenchTool, model.WorkbenchPreset, model.SystemSetting, model.ArkPrivateAssetBinding, model.CloudAgentResourceLease:
		// These models have explicit business writers. No production compatibility
		// caller needs raw mutation; reject rather than bypass their state machines.
		return gorm.ErrInvalidData
	}
	return nil
}

func referenceURLDocument(urls ...string) string {
	raw, _ := json.Marshal(map[string]any{"referenceUrls": urls})
	return string(raw)
}

func (r *Repository) requireProjectReferenceResources(projectID string, ids []string, docs ...string) error {
	var project model.Project
	if err := r.db.Select("user_id").First(&project, "id = ?", projectID).Error; err != nil {
		return err
	}
	return r.requireReferenceResources(project.UserID, false, ids, docs...)
}

func (r *Repository) requireLiveAsset(assetID string) (*model.Asset, error) {
	var asset model.Asset
	if err := r.db.Clauses(clause.Locking{Strength: "UPDATE"}).First(&asset, "id = ?", assetID).Error; err != nil {
		return nil, err
	}
	if asset.Status == model.AssetVersionStatusDeleting {
		return nil, ErrResourceDeleting
	}
	return &asset, nil
}

func (r *Repository) requireAssetVersionReferences(version *model.AssetVersion) error {
	asset, err := r.requireLiveAsset(version.AssetID)
	if err != nil {
		return err
	}
	if version.Status == model.AssetVersionStatusDeleting {
		return ErrResourceDeleting
	}
	return r.requireReferenceResources(asset.UserID, false, nil, version.DefinitionJSON)
}

func (r *Repository) requireToolResources(tool *model.Tool, global bool) error {
	var extra []string
	if strings.TrimSpace(tool.ExtraInfoJSON) != "" {
		if err := json.Unmarshal([]byte(tool.ExtraInfoJSON), &extra); err != nil {
			return err
		}
	}
	return r.requireReferenceResources(tool.OwnerID, global, nil, referenceURLDocument(append(extra, tool.Cover, tool.MediaURL)...))
}

func (r *Repository) requireWorkbenchResources(tool *model.WorkbenchTool) error {
	return r.requireReferenceResources("", true, []string{tool.IconResourceID, tool.CoverResourceID}, tool.CapabilityJSON, tool.GuideJSON)
}

func (r *Repository) requireCharacterResources(asset *model.Asset, version *model.AssetVersion, representations []model.AssetRepresentation) error {
	if asset == nil || version == nil {
		return gorm.ErrInvalidData
	}
	if asset.Status == model.AssetVersionStatusDeleting || version.Status == model.AssetVersionStatusDeleting {
		return ErrResourceDeleting
	}
	ids := []string{}
	docs := []string{asset.PayloadJSON, version.DefinitionJSON}
	for _, rep := range representations {
		ids = append(ids, rep.ResourceID)
		docs = append(docs, rep.MetadataJSON)
	}
	return r.requireReferenceResources(asset.UserID, false, ids, docs...)
}
