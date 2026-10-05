package app

import (
	"encoding/json"
	"strings"
	"time"

	"infinite-canvas/backend/internal/model"

	"gorm.io/gorm"
)

// 通用项目资产图片绑定：场景/道具等非角色分类复用 asset_representations 表
// 与角色链路的事务化版本写入，但不含角色定义/声音继承语义。
// 角色资产必须继续走 ReplaceProjectCharacterRepresentations。

type ReplaceAssetRepresentationsRequest struct {
	Representations []CharacterRepresentationInput `json:"representations"`
}

// replaceableAssetCategories 返回允许通用图片绑定的业务分类；角色走专属接口。
func replaceableAssetCategories() map[model.AssetCategory]struct{} {
	return map[model.AssetCategory]struct{}{
		model.AssetCategoryEnvironment: {},
		model.AssetCategoryProp:        {},
		model.AssetCategoryMaterial:    {},
		model.AssetCategoryOther:       {},
	}
}

// ReplaceProjectAssetRepresentations 把图片资源绑定为资产新版本（primary 表现），
// 并把首张图写入 payload 的 data.storageKey 以驱动列表与预览展示。
func (s *Service) ReplaceProjectAssetRepresentations(userID string, projectID string, assetID string, req ReplaceAssetRepresentationsRequest) (ProjectAssetSummary, error) {
	if _, err := s.repo.ProjectForUser(userID, projectID); err != nil {
		return ProjectAssetSummary{}, err
	}
	asset, err := s.repo.AssetForUser(userID, strings.TrimSpace(assetID))
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	linked, err := s.repo.ProjectAssetLinked(projectID, asset.ID)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	if !linked {
		return ProjectAssetSummary{}, BadAuthRequest("素材尚未加入当前项目")
	}
	if asset.Category == model.AssetCategoryCharacter {
		return ProjectAssetSummary{}, BadAuthRequest("角色请使用角色形象接口")
	}
	if _, allowed := replaceableAssetCategories()[asset.Category]; !allowed {
		return ProjectAssetSummary{}, BadAuthRequest("该资产分类不支持图片绑定")
	}
	if len(req.Representations) == 0 || len(req.Representations) > 8 {
		return ProjectAssetSummary{}, BadAuthRequest("资产图片数量必须在 1 到 8 之间")
	}
	operationID := newID()
	representations := make([]model.AssetRepresentation, 0, len(req.Representations))
	var coverResourceID string
	for _, input := range req.Representations {
		resourceID := strings.TrimSpace(input.ResourceID)
		resource, resourceErr := s.repo.ResourceForUser(userID, resourceID)
		if resourceErr != nil || resource.Kind != "image" || resource.Status != model.ResourceStatusReady {
			return ProjectAssetSummary{}, BadAuthRequest("资产图片资源不可用")
		}
		if coverResourceID == "" {
			coverResourceID = resourceID
		}
		metadata, marshalErr := json.Marshal(map[string]any{"source": "asset_image_binding"})
		if marshalErr != nil {
			return ProjectAssetSummary{}, BadAuthRequest("资产图片元数据格式无效")
		}
		representations = append(representations, model.AssetRepresentation{ID: newID(), TaskID: operationID, ResourceID: resourceID, MediaType: "image", Role: "primary", MetadataJSON: string(metadata), CreatedAt: time.Now()})
	}

	current, err := s.repo.AssetVersion(asset.PrimaryVersionID)
	if err != nil || current == nil {
		if err != nil && err != gorm.ErrRecordNotFound {
			return ProjectAssetSummary{}, err
		}
		return ProjectAssetSummary{}, BadAuthRequest("资产当前版本不可用")
	}
	versions, err := s.repo.AssetVersions(asset.ID)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	nextNumber := 1
	if len(versions) > 0 {
		nextNumber = versions[0].Version + 1
	}
	now := time.Now()
	next := model.AssetVersion{ID: newID(), AssetID: asset.ID, Version: nextNumber, Status: model.AssetVersionStatusConfirmed, DefinitionJSON: current.DefinitionJSON, Prompt: current.Prompt, Note: current.Note, CreatedAt: now, UpdatedAt: now}
	for index := range representations {
		representations[index].AssetVersionID = next.ID
	}

	nextAsset := *asset
	nextAsset.Status = model.AssetVersionStatusConfirmed
	nextAsset.PrimaryVersionID = next.ID
	nextAsset.UpdatedAt = now
	payload, payloadErr := assetPayloadWithCoverStorageKey(asset.PayloadJSON, coverResourceID)
	if payloadErr != nil {
		return ProjectAssetSummary{}, payloadErr
	}
	nextAsset.PayloadJSON = payload

	if err := s.repo.SaveCharacterVersion(projectID, &nextAsset, &next, representations, nil); err != nil {
		return ProjectAssetSummary{}, err
	}
	return s.projectAssetSummary(userID, projectID, &nextAsset)
}

// assetPayloadWithCoverStorageKey 在既有 payload 上推进版本号与封面资源引用；
// 非角色资产的 data 是通用 map，只改必要键，不重建整份记录。
func assetPayloadWithCoverStorageKey(payloadJSON string, resourceID string) (string, error) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil || payload == nil {
		payload = map[string]any{}
	}
	payload["updatedAt"] = time.Now().UTC().Format(time.RFC3339Nano)
	data, ok := payload["data"].(map[string]any)
	if !ok {
		data = map[string]any{}
	}
	data["storageKey"] = "resource:" + resourceID
	data["mediaType"] = "image"
	payload["data"] = data
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}
