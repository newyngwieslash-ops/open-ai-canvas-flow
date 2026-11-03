package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

const MaxProjectAssetReferenceImages = 16

type ProjectReferenceImage struct {
	ResourceID string `json:"resourceId"`
}
type CreateProjectAssetEntityRequest struct {
	Name            string                  `json:"name"`
	Category        model.AssetCategory     `json:"category"`
	Definition      map[string]any          `json:"definition"`
	ReferenceImages []ProjectReferenceImage `json:"referenceImages,omitempty"`
	ImageResourceID string                  `json:"imageResourceId,omitempty"`
	FolderID        *string                 `json:"folderId,omitempty"`
}
type SetProjectReferenceImagesRequest struct {
	ReferenceImages   []ProjectReferenceImage `json:"referenceImages"`
	ExpectedVersionID string                  `json:"expectedVersionId"`
}
type ProjectReferenceMigrationReport struct {
	MigratedVersions int `json:"migratedVersions"`
	MigratedPayloads int `json:"migratedPayloads"`
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

func referenceImagesFromDefinition(raw string) []ProjectReferenceImage {
	var value struct {
		ReferenceImages []ProjectReferenceImage `json:"referenceImages"`
		ReferenceImage  *ProjectReferenceImage  `json:"referenceImage"`
	}
	if json.Unmarshal([]byte(raw), &value) != nil {
		return nil
	}
	if len(value.ReferenceImages) > 0 {
		out := make([]ProjectReferenceImage, 0, len(value.ReferenceImages))
		for _, item := range value.ReferenceImages {
			if id := strings.TrimSpace(item.ResourceID); id != "" {
				out = append(out, ProjectReferenceImage{ResourceID: id})
			}
		}
		if len(out) > 0 {
			return out
		}
		return nil
	}
	if value.ReferenceImage != nil && strings.TrimSpace(value.ReferenceImage.ResourceID) != "" {
		return []ProjectReferenceImage{{ResourceID: strings.TrimSpace(value.ReferenceImage.ResourceID)}}
	}
	return nil
}

func hasReferenceImageInDefinition(raw, resourceID string) bool {
	resourceID = strings.TrimSpace(resourceID)
	if resourceID == "" {
		return false
	}
	for _, item := range referenceImagesFromDefinition(raw) {
		if item.ResourceID == resourceID {
			return true
		}
	}
	return false
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
	if proposed, exists := next["referenceImages"]; exists {
		currentRefs, currentExists := current["referenceImages"]
		if !currentExists || !reflect.DeepEqual(proposed, currentRefs) {
			return "", BadAuthRequest("原始参考图只能通过专用接口修改")
		}
	}
	if _, exists := next["referenceImage"]; exists {
		return "", BadAuthRequest("原始参考图只能通过专用接口修改")
	}
	delete(next, "referenceImage")
	if refs, exists := current["referenceImages"]; exists {
		next["referenceImages"] = refs
	} else if parsed := referenceImagesFromDefinition(currentJSON); len(parsed) > 0 {
		next["referenceImages"] = parsed
	} else {
		delete(next, "referenceImages")
	}
	encoded, err := json.Marshal(next)
	return string(encoded), err
}

func (s *Service) validateProjectReferenceImages(userID string, references []ProjectReferenceImage) ([]ProjectReferenceImage, error) {
	if len(references) > MaxProjectAssetReferenceImages {
		return nil, BadAuthRequest("原始参考图最多支持 16 张")
	}
	seen := make(map[string]struct{}, len(references))
	normalized := make([]ProjectReferenceImage, 0, len(references))
	for _, item := range references {
		resource, err := s.projectImageResource(userID, item.ResourceID)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[resource.ID]; exists {
			return nil, BadAuthRequest("原始参考图不能重复")
		}
		seen[resource.ID] = struct{}{}
		normalized = append(normalized, ProjectReferenceImage{ResourceID: resource.ID})
	}
	return normalized, nil
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
	asset, err := s.createProjectImageEntity(userID, projectID, req.Category, req.Name, req.Definition, req.ReferenceImages, req.ImageResourceID, "", req.FolderID)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	return s.projectAssetSummary(userID, projectID, asset)
}

func (s *Service) createProjectImageEntity(userID, projectID string, category model.AssetCategory, name string, definition map[string]any, references []ProjectReferenceImage, imageID, imageRole string, folder *string) (*model.Asset, error) {
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
	normalizedRefs, err := s.validateProjectReferenceImages(userID, references)
	if err != nil {
		return nil, err
	}
	if len(normalizedRefs) > 0 {
		def["referenceImages"] = normalizedRefs
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

func (s *Service) SetProjectReferenceImages(userID, projectID, assetID string, req SetProjectReferenceImagesRequest) (ProjectAssetSummary, error) {
	if strings.TrimSpace(req.ExpectedVersionID) == "" || req.ReferenceImages == nil {
		return ProjectAssetSummary{}, BadAuthRequest("参考图列表与预期版本不能为空")
	}
	asset, err := s.projectImageAsset(userID, projectID, assetID)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	if asset.PrimaryVersionID != req.ExpectedVersionID {
		return ProjectAssetSummary{}, projectAssetVersionConflict()
	}
	normalizedRefs, err := s.validateProjectReferenceImages(userID, req.ReferenceImages)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	currentVersion, err := s.repo.AssetVersion(asset.PrimaryVersionID)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	currentRefs := referenceImagesFromDefinition(currentVersion.DefinitionJSON)
	if len(currentRefs) == len(normalizedRefs) && (len(currentRefs) == 0 || reflect.DeepEqual(currentRefs, normalizedRefs)) {
		return s.projectAssetSummary(userID, projectID, asset)
	}
	// Preparation copies the current voice and representations. Only the referenceImages field is then changed.
	nextAsset, next, reps, voice, err := s.prepareNextCharacterVersion(asset, asset.Title, "", nil, nil, false)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	var definition map[string]any
	if err := json.Unmarshal([]byte(next.DefinitionJSON), &definition); err != nil {
		return ProjectAssetSummary{}, err
	}
	delete(definition, "referenceImage")
	if len(normalizedRefs) > 0 {
		definition["referenceImages"] = normalizedRefs
	} else {
		definition["referenceImages"] = []ProjectReferenceImage{}
	}
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

func (s *Service) MigrateLegacyProjectReferenceImages() (ProjectReferenceMigrationReport, error) {
	var report ProjectReferenceMigrationReport
	err := s.repo.Transaction(func(tx *repository.Repository) error {
		checkResource := func(resourceID string) error {
			resourceID = strings.TrimSpace(resourceID)
			if resourceID == "" {
				return errors.New("空参考图资源 ID")
			}
			resource, err := tx.ResourceByID(resourceID)
			if err != nil {
				return fmt.Errorf("参考图资源 %s 不存在：%w", resourceID, err)
			}
			if resource.Kind != "image" {
				return fmt.Errorf("参考图资源 %s 不是图片类型", resourceID)
			}
			return nil
		}
		canonicalizeMap := func(doc map[string]any, label string) (bool, error) {
			rawSingle, hasSingle := doc["referenceImage"]
			rawMulti, hasMulti := doc["referenceImages"]
			var singleID string
			if hasSingle && rawSingle != nil {
				singleMap, ok := rawSingle.(map[string]any)
				if !ok {
					return false, fmt.Errorf("%s 的 referenceImage 格式非法", label)
				}
				id, _ := singleMap["resourceId"].(string)
				if err := checkResource(id); err != nil {
					return false, fmt.Errorf("%s：%w", label, err)
				}
				singleID = strings.TrimSpace(id)
			}
			var multiRefs []ProjectReferenceImage
			if hasMulti && rawMulti != nil {
				items, ok := rawMulti.([]any)
				if !ok {
					return false, fmt.Errorf("%s 的 referenceImages 格式非法", label)
				}
				seen := map[string]struct{}{}
				multiRefs = make([]ProjectReferenceImage, 0, len(items))
				for _, item := range items {
					entry, ok := item.(map[string]any)
					if !ok {
						return false, fmt.Errorf("%s 的 referenceImages 元素格式非法", label)
					}
					id, _ := entry["resourceId"].(string)
					if err := checkResource(id); err != nil {
						return false, fmt.Errorf("%s：%w", label, err)
					}
					id = strings.TrimSpace(id)
					if _, dup := seen[id]; dup {
						return false, fmt.Errorf("%s 的 referenceImages 存在重复资源 %s", label, id)
					}
					seen[id] = struct{}{}
					multiRefs = append(multiRefs, ProjectReferenceImage{ResourceID: id})
				}
				if len(multiRefs) > MaxProjectAssetReferenceImages {
					return false, fmt.Errorf("%s 的 referenceImages 超过 16 张上限", label)
				}
			}
			if hasSingle && rawSingle != nil && hasMulti {
				if len(multiRefs) != 1 || multiRefs[0].ResourceID != singleID {
					return false, fmt.Errorf("%s 同时存在不一致的 referenceImage 与 referenceImages", label)
				}
			}
			if !hasSingle && hasMulti && rawMulti != nil {
				return false, nil
			}
			delete(doc, "referenceImage")
			if hasMulti && rawMulti != nil {
				doc["referenceImages"] = multiRefs
			} else if singleID != "" {
				doc["referenceImages"] = []ProjectReferenceImage{{ResourceID: singleID}}
			} else {
				doc["referenceImages"] = []ProjectReferenceImage{}
			}
			return true, nil
		}

		versions, err := tx.AllAssetVersionsOrdered()
		if err != nil {
			return err
		}
		for _, version := range versions {
			if strings.TrimSpace(version.DefinitionJSON) == "" {
				continue
			}
			var def map[string]any
			if err := json.Unmarshal([]byte(version.DefinitionJSON), &def); err != nil || def == nil {
				return fmt.Errorf("资产版本 %s definition_json 损坏", version.ID)
			}
			changed, err := canonicalizeMap(def, "资产版本 "+version.ID)
			if err != nil {
				return err
			}
			if !changed {
				continue
			}
			encoded, err := json.Marshal(def)
			if err != nil {
				return err
			}
			if err := tx.UpdateAssetVersionDefinition(version.ID, string(encoded)); err != nil {
				return err
			}
			report.MigratedVersions++
		}

		assets, err := tx.LegacyReferencePayloadAssets()
		if err != nil {
			return err
		}
		for _, asset := range assets {
			var payload map[string]any
			if err := json.Unmarshal([]byte(asset.PayloadJSON), &payload); err != nil || payload == nil {
				return fmt.Errorf("资产 %s payload_json 损坏", asset.ID)
			}
			changed := false
			if _, hasTop := payload["referenceImage"]; hasTop {
				c, err := canonicalizeMap(payload, "资产 "+asset.ID)
				if err != nil {
					return err
				}
				changed = changed || c
			}
			if data, ok := payload["data"].(map[string]any); ok {
				if def, ok := data["definition"].(map[string]any); ok {
					if _, hasNested := def["referenceImage"]; hasNested {
						c, err := canonicalizeMap(def, "资产 "+asset.ID+" data.definition")
						if err != nil {
							return err
						}
						changed = changed || c
					}
				}
			}
			if !changed {
				continue
			}
			encoded, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			if err := tx.UpdateAssetPayloadJSON(asset.ID, string(encoded)); err != nil {
				return err
			}
			report.MigratedPayloads++
		}
		return nil
	})
	return report, err
}
