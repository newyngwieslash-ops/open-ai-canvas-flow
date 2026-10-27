package app

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type ProjectReferenceImage struct {
	ResourceID string `json:"resourceId"`
}
type CreateProjectAssetEntityRequest struct {
	Name            string                 `json:"name"`
	Category        model.AssetCategory    `json:"category"`
	Definition      map[string]any         `json:"definition"`
	ReferenceImage  *ProjectReferenceImage `json:"referenceImage,omitempty"`
	ImageResourceID string                 `json:"imageResourceId,omitempty"`
	FolderID        *string                `json:"folderId,omitempty"`
}
type SetProjectReferenceImageRequest struct {
	ResourceID        string `json:"resourceId"`
	ExpectedVersionID string `json:"expectedVersionId"`
}

func projectAssetVersionConflict() error {
	return &AppError{Status: 409, Code: 409, Reason: "project_asset_version_changed", Message: "资产版本已变化，请刷新后重试"}
}
func projectAssetWriteError(err error) error {
	if errors.Is(err, repository.ErrProjectAssetVersionChanged) {
		return projectAssetVersionConflict()
	}
	return err
}

func referenceImageFromDefinition(raw string) *ProjectReferenceImage {
	var value struct {
		ReferenceImage *ProjectReferenceImage `json:"referenceImage"`
	}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return nil
	}
	return value.ReferenceImage
}

func preserveReferenceDefinition(currentJSON, nextJSON string) (string, error) {
	var current, next map[string]any
	if json.Unmarshal([]byte(currentJSON), &current) != nil || json.Unmarshal([]byte(nextJSON), &next) != nil || next == nil {
		return "", BadAuthRequest("资产设定必须是 JSON 对象")
	}
	if proposed, exists := next["_profileAnalysisAdoption"]; exists && !reflect.DeepEqual(proposed, current["_profileAnalysisAdoption"]) {
		return "", BadAuthRequest("分析采用收据只能由服务端写入")
	}
	if receipt, exists := current["_profileAnalysisAdoption"]; exists {
		next["_profileAnalysisAdoption"] = receipt
	} else {
		delete(next, "_profileAnalysisAdoption")
	}
	if proposed, exists := next["referenceImage"]; exists && !reflect.DeepEqual(proposed, current["referenceImage"]) {
		return "", BadAuthRequest("原始参考图只能通过专用接口修改")
	}
	if ref, exists := current["referenceImage"]; exists {
		next["referenceImage"] = ref
	} else {
		delete(next, "referenceImage")
	}
	encoded, err := json.Marshal(next)
	return string(encoded), err
}

func (s *Service) projectImageResource(userID, id string) (*model.Resource, error) {
	if strings.TrimSpace(id) == "" {
		return nil, BadAuthRequest("图片资源不能为空")
	}
	resource, err := s.repo.ResourceForUser(userID, strings.TrimSpace(id))
	if err != nil {
		return nil, err
	}
	if resource.Kind != "image" || resource.Status != model.ResourceStatusReady || resource.Width <= 0 || resource.Height <= 0 || !strings.HasPrefix(resource.MimeType, "image/") {
		return nil, BadAuthRequest("图片未就绪或缺少有效图片尺寸")
	}
	return resource, nil
}

func (s *Service) projectImageAsset(userID, projectID, assetID string) (*model.Asset, error) {
	if _, err := s.repo.ProjectForUser(userID, projectID); err != nil {
		return nil, err
	}
	asset, err := s.repo.AssetForUser(userID, assetID)
	if err != nil {
		return nil, err
	}
	linked, err := s.repo.ProjectAssetLinked(projectID, assetID)
	if err != nil {
		return nil, err
	}
	if !linked || (asset.Category != model.AssetCategoryCharacter && !projectVisualCategory(asset.Category)) {
		return nil, BadAuthRequest("请选择当前项目的角色、场景或道具")
	}
	version, err := s.repo.AssetVersion(asset.PrimaryVersionID)
	if err != nil {
		return nil, err
	}
	if version.AssetID != asset.ID {
		return nil, BadAuthRequest("资产主版本不属于当前资产")
	}
	return asset, nil
}

func (s *Service) CreateProjectAssetEntity(userID, projectID string, req CreateProjectAssetEntityRequest) (ProjectAssetSummary, error) {
	if !projectVisualCategory(req.Category) {
		return ProjectAssetSummary{}, BadAuthRequest("仅支持场景和道具实体")
	}
	asset, err := s.createProjectImageEntity(userID, projectID, req.Category, req.Name, req.Definition, req.ReferenceImage, req.ImageResourceID, "", req.FolderID)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	return s.projectAssetSummary(userID, projectID, asset)
}

func (s *Service) createProjectImageEntity(userID, projectID string, category model.AssetCategory, name string, definition map[string]any, reference *ProjectReferenceImage, imageID, imageRole string, folder *string) (*model.Asset, error) {
	if _, err := s.repo.ProjectForUser(userID, projectID); err != nil {
		return nil, err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, BadAuthRequest("资产名称不能为空")
	}
	raw, err := normalizedCharacterDefinition(definition)
	if err != nil {
		return nil, err
	}
	rawString, err := preserveReferenceDefinition("{}", string(raw))
	if err != nil {
		return nil, err
	}
	var def map[string]any
	_ = json.Unmarshal([]byte(rawString), &def)
	if reference != nil {
		resource, err := s.projectImageResource(userID, reference.ResourceID)
		if err != nil {
			return nil, err
		}
		def["referenceImage"] = &ProjectReferenceImage{ResourceID: resource.ID}
	}
	raw, err = json.Marshal(def)
	if err != nil {
		return nil, err
	}
	if imageRole != "" && imageRole != "portrait" && imageRole != "turnaround_sheet" {
		return nil, BadAuthRequest("不支持的角色图片用途")
	}
	if imageRole != "" && strings.TrimSpace(imageID) == "" {
		return nil, BadAuthRequest("声明图片用途时必须选择图片")
	}
	folderID, err := s.resolveProjectAssetFolderID(projectID, folder)
	if err != nil {
		return nil, err
	}
	position, err := s.repo.NextProjectAssetPosition(projectID, folderID)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	assetID, versionID := newID(), newID()
	asset := &model.Asset{ID: assetID, UserID: userID, Kind: "entity", Category: category, Title: name, Status: model.AssetVersionStatusConfirmed, PrimaryVersionID: versionID, CreatedAt: now, UpdatedAt: now}
	payload := map[string]any{"id": assetID, "kind": "entity", "category": category, "title": name, "status": asset.Status, "primaryVersionId": versionID, "data": map[string]any{"definition": def}, "metadata": map[string]any{"projectImageEntity": true}, "createdAt": now.Format(time.RFC3339Nano), "updatedAt": now.Format(time.RFC3339Nano)}
	reps := []model.AssetRepresentation{}
	if strings.TrimSpace(imageID) != "" {
		resource, err := s.projectImageResource(userID, imageID)
		if err != nil {
			return nil, err
		}
		op := newID()
		reps = append(reps, model.AssetRepresentation{ID: newID(), TaskID: op, AssetVersionID: versionID, ResourceID: resource.ID, MediaType: "image", Role: "primary", MetadataJSON: "{}", CreatedAt: now})
		if category == model.AssetCategoryCharacter && imageRole == "turnaround_sheet" {
			rep := reps[0]
			rep.ID = newID()
			rep.Role = "turnaround_sheet"
			reps = append(reps, rep)
		}
		if category != model.AssetCategoryCharacter {
			asset.Kind = "image"
			payload["kind"] = "image"
			payload["coverUrl"] = resourceFileURL(resource.ID)
			payload["data"] = projectImagePayload(resource)
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	asset.PayloadJSON = string(encoded)
	version := model.AssetVersion{ID: versionID, AssetID: assetID, Version: 1, Status: asset.Status, DefinitionJSON: string(raw), CreatedAt: now, UpdatedAt: now}
	link := model.ProjectAssetLink{ID: newID(), ProjectID: projectID, AssetID: assetID, FolderID: folderID, Position: position, CreatedAt: now}
	if err := s.repo.CreateProjectEntity(asset, &version, &link, reps); err != nil {
		return nil, err
	}
	return asset, nil
}

func projectImagePayload(resource *model.Resource) map[string]any {
	return map[string]any{"storageKey": "resource:" + resource.ID, "dataUrl": resourceFileURL(resource.ID), "width": resource.Width, "height": resource.Height, "mimeType": resource.MimeType, "bytes": resource.Size}
}

func (s *Service) SetProjectReferenceImage(userID, projectID, assetID string, req SetProjectReferenceImageRequest) (ProjectAssetSummary, error) {
	if strings.TrimSpace(req.ExpectedVersionID) == "" || strings.TrimSpace(req.ResourceID) == "" {
		return ProjectAssetSummary{}, BadAuthRequest("图片资源与预期版本不能为空")
	}
	asset, err := s.projectImageAsset(userID, projectID, assetID)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	if asset.PrimaryVersionID != req.ExpectedVersionID {
		return ProjectAssetSummary{}, projectAssetVersionConflict()
	}
	resource, err := s.projectImageResource(userID, req.ResourceID)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	// Preparation copies the current voice and representations. Only the reference field is then changed.
	nextAsset, next, reps, voice, err := s.prepareNextCharacterVersion(asset, asset.Title, "", nil, nil, false)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	var definition map[string]any
	if err := json.Unmarshal([]byte(next.DefinitionJSON), &definition); err != nil {
		return ProjectAssetSummary{}, err
	}
	definition["referenceImage"] = &ProjectReferenceImage{ResourceID: resource.ID}
	encoded, err := json.Marshal(definition)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	next.DefinitionJSON = string(encoded)
	nextAsset.Kind = asset.Kind
	nextAsset.Category = asset.Category
	var payload map[string]any
	if err := json.Unmarshal([]byte(asset.PayloadJSON), &payload); err != nil {
		return ProjectAssetSummary{}, err
	}
	payload["primaryVersionId"] = next.ID
	payload["updatedAt"] = next.UpdatedAt.Format(time.RFC3339Nano)
	if asset.Kind == "entity" {
		data, _ := payload["data"].(map[string]any)
		if data == nil {
			data = map[string]any{}
		}
		data["definition"] = definition
		payload["data"] = data
	}
	encoded, err = json.Marshal(payload)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	nextAsset.PayloadJSON = string(encoded)
	if err := s.repo.CommitAssetReferenceVersion(projectID, req.ExpectedVersionID, &nextAsset, &next, reps, voice); err != nil {
		return ProjectAssetSummary{}, projectAssetWriteError(err)
	}
	return s.projectAssetSummary(userID, projectID, &nextAsset)
}
