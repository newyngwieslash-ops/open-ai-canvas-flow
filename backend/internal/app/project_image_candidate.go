package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type AssetImageCandidate struct {
	AssetID          string `json:"assetId"`
	BaseVersionID    string `json:"baseVersionId"`
	State            string `json:"state"`
	AdoptedVersionID string `json:"adoptedVersionId,omitempty"`
}
type AdoptProjectImageCandidateRequest struct {
	ExpectedVersionID string `json:"expectedVersionId"`
}
type projectImageTaskMetadata struct {
	Operation        string `json:"operation"`
	AdoptionMode     string `json:"adoptionMode"`
	AssetID          string `json:"projectAssetId"`
	CharacterAssetID string `json:"characterAssetId"`
	VersionID        string `json:"projectAssetVersionId"`
	ReferenceSource  string `json:"referenceSource"`
}

func (m projectImageTaskMetadata) targetID() string {
	if m.Operation == "character_turnaround" {
		return m.CharacterAssetID
	}
	return m.AssetID
}
func (m projectImageTaskMetadata) supported() bool {
	return m.Operation == "character_turnaround" || m.Operation == "project_asset_image"
}
func parseProjectImageMetadata(raw string) (projectImageTaskMetadata, error) {
	var input struct {
		Metadata projectImageTaskMetadata `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(raw), &input); err != nil {
		return input.Metadata, err
	}
	if input.Metadata.AdoptionMode != "" && input.Metadata.AdoptionMode != "auto" && input.Metadata.AdoptionMode != "manual" {
		return input.Metadata, BadAuthRequest("不支持的图片采用模式")
	}
	return input.Metadata, nil
}

func candidateFromResult(raw string) *AssetImageCandidate {
	var value struct {
		Candidate *AssetImageCandidate `json:"assetImageCandidate"`
	}
	if json.Unmarshal([]byte(raw), &value) != nil || value.Candidate == nil {
		return nil
	}
	c := value.Candidate
	if c.AssetID == "" || c.BaseVersionID == "" || (c.State != "pending_confirmation" && c.State != "adopted") {
		return nil
	}
	return c
}

func (s *Service) prepareProjectImageTask(userID, projectID, taskType string, input map[string]any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	meta, err := parseProjectImageMetadata(string(raw))
	if err != nil {
		return err
	}
	if !meta.supported() {
		if meta.AdoptionMode != "" {
			return BadAuthRequest("图片采用模式仅适用于项目图片任务")
		}
		return nil
	}
	if meta.AdoptionMode == "manual" {
		metadata, _ := input["metadata"].(map[string]any)
		for _, values := range []map[string]any{input, metadata} {
			for _, key := range []string{"workflowStepId", "assetVersionId"} {
				if value, exists := values[key]; exists && value != nil && strings.TrimSpace(fmt.Sprint(value)) != "" {
					return BadAuthRequest("手动图片候选不能携带自动产物绑定信息")
				}
			}
		}
	}
	// Legacy automatic callers keep their input contract. Explicit reference selection uses the new contract.
	if meta.AdoptionMode != "manual" && meta.ReferenceSource == "" {
		return nil
	}
	if taskType != "canvas_image" || input["mode"] != "image" {
		return BadAuthRequest("项目图片候选必须使用图片任务")
	}
	asset, err := s.projectImageAsset(userID, projectID, meta.targetID())
	if err != nil {
		return err
	}
	if (meta.Operation == "character_turnaround") != (asset.Category == model.AssetCategoryCharacter) {
		return BadAuthRequest("生成操作与资产分类不匹配")
	}
	if meta.VersionID == "" || meta.VersionID != asset.PrimaryVersionID {
		return projectAssetVersionConflict()
	}
	var typed canvasGenerationInput
	if err := json.Unmarshal(raw, &typed); err != nil {
		return BadAuthRequest("图片任务输入格式无效")
	}
	if len(typed.ReferenceImages) > 1 || len(typed.ReferenceVideos) > 0 || len(typed.ReferenceAudios) > 0 || typed.Mask != nil {
		return BadAuthRequest("项目资产仅支持单张参考图")
	}
	config, _ := input["config"].(map[string]any)
	if config == nil {
		config = map[string]any{}
		input["config"] = config
	}
	outputOptions := []map[string]any{config, typed.Config.ImageParameters}
	// Check the same provider namespace objects consumed by the shipped protocols,
	// without discarding unrelated options such as seed, watermark or reference settings.
	if providers, ok := typed.Metadata["providerOptions"].(map[string]any); ok {
		for _, value := range providers {
			if options, ok := value.(map[string]any); ok {
				outputOptions = append(outputOptions, options)
			}
		}
	}
	for _, values := range outputOptions {
		if mode, ok := values["sequential_image_generation"]; ok && fmt.Sprint(mode) != "" && fmt.Sprint(mode) != "disabled" {
			return BadAuthRequest("项目资产仅支持单张输出")
		}
		if raw, exists := values["sequential_image_generation_options"]; exists {
			options, ok := raw.(map[string]any)
			if !ok {
				return BadAuthRequest("项目资产仅支持单张输出")
			}
			if maximum, exists := options["max_images"]; exists && fmt.Sprint(maximum) != "1" {
				return BadAuthRequest("项目资产仅支持单张输出")
			}
		}
		for _, key := range []string{"count", "n", "batchSize", "num_images", "sample_count"} {
			if value, ok := values[key]; ok && fmt.Sprint(value) != "" && fmt.Sprint(value) != "1" {
				return BadAuthRequest("项目资产仅支持单张输出")
			}
		}
	}
	if options, ok := input["capabilityOptions"].(map[string]any); ok {
		for _, key := range []string{"count", "n", "batchSize"} {
			if value, exists := options[key]; exists && fmt.Sprint(value) != "1" {
				return BadAuthRequest("项目资产仅支持单张输出")
			}
		}
	}
	config["count"] = "1"
	version, err := s.repo.AssetVersion(meta.VersionID)
	if err != nil {
		return err
	}
	resourceID := ""
	switch meta.ReferenceSource {
	case "original":
		ref := referenceImageFromDefinition(version.DefinitionJSON)
		if ref == nil {
			return BadAuthRequest("当前版本没有原始参考图")
		}
		resourceID = ref.ResourceID
	case "current":
		reps, err := s.repo.AssetRepresentations(version.ID)
		if err != nil {
			return err
		}
		for _, rep := range reps {
			if rep.Role == "primary" && rep.MediaType == "image" {
				resourceID = rep.ResourceID
				break
			}
		}
		if resourceID == "" {
			return BadAuthRequest("当前版本没有成品图片")
		}
	case "none":
		if len(typed.ReferenceImages) > 0 {
			return BadAuthRequest("无参考模式不能携带参考图片")
		}
	default:
		return BadAuthRequest("必须明确选择原图、当前图或无参考")
	}
	if resourceID != "" {
		resource, err := s.projectImageResource(userID, resourceID)
		if err != nil {
			return err
		}
		if len(typed.ReferenceImages) == 1 {
			ref := typed.ReferenceImages[0]
			if ref.StorageKey != "resource:"+resourceID || (ref.URL != "" && ref.URL != resourceFileURL(resourceID)) || (ref.DataURL != "" && ref.DataURL != resourceFileURL(resourceID)) {
				return BadAuthRequest("参考图片与所选版本来源不一致")
			}
		}
		input["referenceImages"] = []any{map[string]any{"id": resource.ID, "type": "image", "storageKey": "resource:" + resource.ID, "mimeType": resource.MimeType, "bytes": resource.Size, "width": resource.Width, "height": resource.Height}}
	} else {
		input["referenceImages"] = []any{}
	}
	return nil
}

// Validate after routing, against the resolved system profile or declared custom protocol profile.
func (s *Service) validateProjectImageReferenceCapability(input map[string]any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	meta, err := parseProjectImageMetadata(string(raw))
	if err != nil {
		return err
	}
	if !meta.supported() || (meta.AdoptionMode != "manual" && meta.ReferenceSource == "") {
		return nil
	}
	var typed canvasGenerationInput
	if err := json.Unmarshal(raw, &typed); err != nil {
		return err
	}
	if len(typed.ReferenceImages) == 0 {
		return nil
	}
	if isWorkflowProviderInterface(typed.Config.InterfaceType) {
		return BadAuthRequest("无法确认当前工作流的参考图能力")
	}
	channelID := strings.TrimSpace(typed.Config.ChannelID)
	if channelID == "" {
		channelID = systemChannelIDFromBaseURL(typed.Config.BaseURL)
	}
	var profile *ImageCapabilityConfig
	if channelID != "" {
		item, err := s.repo.ChannelModelByKey(channelID, providerChannelModelKey(typed.Config))
		if err != nil {
			return err
		}
		capability, err := normalizedChannelModelCapability(item, s.protocolRegistry())
		if err != nil {
			return err
		}
		if capability != nil {
			profile = capability.Image
		}
	} else {
		// Custom channels must have a known protocol; client declarations cannot invent support.
		switch model.ChannelInterfaceType(typed.Config.InterfaceType) {
		case model.ChannelInterfaceOpenAIImage, model.ChannelInterfaceGeminiImage, model.ChannelInterfaceGrokImage, model.ChannelInterfaceVolcengineArkImage, model.ChannelInterfaceVolcengineArkAgentPlanImage, model.ChannelInterfaceVolcengineJiMengImage, "km-kemei-seedream":
		default:
			return BadAuthRequest("当前模型的参考图能力尚未确认")
		}
		if model.ChannelInterfaceType(typed.Config.InterfaceType) == model.ChannelInterfaceOpenAIImage {
			name := strings.ToLower(strings.TrimSpace(typed.Config.Model))
			if strings.HasPrefix(name, "dall-e") || (!strings.HasPrefix(name, "gpt-image") && (typed.Config.CapabilityConfig == nil || typed.Config.CapabilityConfig.Image == nil)) {
				return BadAuthRequest("当前模型不支持参考图或能力尚未确认")
			}
		}
		profile = DefaultImageCapabilityConfig(typed.Config.InterfaceType, typed.Config.Model)
		if profile == nil || profile.References.MaxImages < 1 {
			return BadAuthRequest("当前模型不支持参考图或能力尚未确认")
		}
		if typed.Config.CapabilityConfig != nil && typed.Config.CapabilityConfig.Image != nil {
			profile = typed.Config.CapabilityConfig.Image
		}
	}
	if profile == nil || profile.References.MaxImages < 1 {
		return BadAuthRequest("当前模型不支持参考图或能力尚未确认")
	}
	return nil
}

func (s *Service) finalizeManualProjectImageTask(task model.Task, result map[string]interface{}) (bool, error) {
	if !strings.Contains(task.InputJSON, "character_turnaround") && !strings.Contains(task.InputJSON, "project_asset_image") {
		return false, nil
	}
	raw, err := s.decryptTaskInputJSON(task.InputJSON)
	if err != nil {
		return false, err
	}
	meta, err := parseProjectImageMetadata(raw)
	if err != nil {
		return false, err
	}
	if !meta.supported() || meta.AdoptionMode != "manual" {
		return false, nil
	}
	if meta.VersionID == "" || meta.targetID() == "" {
		return true, BadAuthRequest("图片候选缺少目标或基准版本")
	}
	asset, err := s.projectImageAsset(task.UserID, task.ProjectID, meta.targetID())
	if err != nil {
		return true, err
	}
	if (meta.Operation == "character_turnaround") != (asset.Category == model.AssetCategoryCharacter) {
		return true, BadAuthRequest("生成操作与资产分类不匹配")
	}
	version, err := s.repo.AssetVersion(meta.VersionID)
	if err != nil {
		return true, err
	}
	if version.AssetID != asset.ID {
		return true, BadAuthRequest("候选基准版本不属于目标资产")
	}
	resource, err := s.projectCandidateResource(task.UserID, result)
	if err != nil {
		return true, err
	}
	if err := s.repo.LockAssetImageResources(task.UserID, version.DefinitionJSON, []model.AssetRepresentation{{ResourceID: resource.ID}}); err != nil {
		return true, err
	}
	candidate := &AssetImageCandidate{AssetID: asset.ID, BaseVersionID: meta.VersionID, State: "pending_confirmation"}
	if existing := candidateFromResult(task.ResultJSON); existing != nil && existing.State == "adopted" {
		candidate = existing
	}
	result["assetImageCandidate"] = candidate
	return true, nil
}

func (s *Service) projectCandidateResource(userID string, result map[string]interface{}) (*model.Resource, error) {
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	var output characterTurnaroundTaskResult
	if err := json.Unmarshal(encoded, &output); err != nil {
		return nil, err
	}
	if len(output.Images) != 1 {
		return nil, BadAuthRequest("图片候选必须返回一张已保存图片")
	}
	return s.projectImageResource(userID, output.Images[0].ResourceID)
}

func (s *Service) AdoptProjectImageCandidate(userID, projectID, assetID, taskID string, req AdoptProjectImageCandidateRequest) (ProjectAssetSummary, error) {
	if strings.TrimSpace(req.ExpectedVersionID) == "" {
		return ProjectAssetSummary{}, BadAuthRequest("预期资产版本不能为空")
	}
	err := s.repo.WithStoryboardAssetWrite(projectID, assetID, func(owned *repository.Repository) error {
	return owned.ProfileWriteTransaction(func(repo *repository.Repository) error {
		if _, err := repo.LockProfileAsset(userID, assetID); err != nil {
			return err
		}
		writer := &Service{repo: repo, dataDir: s.dataDir}
		asset, err := writer.projectImageAsset(userID, projectID, assetID)
		if err != nil {
			return err
		}
		task, err := repo.LockImageCandidateTask(userID, taskID)
		if err != nil {
			return err
		}
		meta, err := parseProjectImageMetadata(task.InputJSON)
		if err != nil {
			return err
		}
		if task.ProjectID != projectID || task.Status != model.TaskStatusSucceeded || !meta.supported() || meta.AdoptionMode != "manual" || meta.targetID() != assetID || (meta.Operation == "character_turnaround") != (asset.Category == model.AssetCategoryCharacter) {
			return BadAuthRequest("任务不是当前资产的成功手动图片候选")
		}
		candidate := candidateFromResult(task.ResultJSON)
		if candidate == nil || candidate.AssetID != assetID || candidate.BaseVersionID != meta.VersionID {
			return BadAuthRequest("任务候选记录无效")
		}
		var result map[string]interface{}
		if err := json.Unmarshal([]byte(task.ResultJSON), &result); err != nil {
			return err
		}
		resource, err := writer.projectCandidateResource(userID, result)
		if err != nil {
			return err
		}
		roles := []string{"primary"}
		if asset.Category == model.AssetCategoryCharacter {
			roles = append(roles, "turnaround_sheet")
		}
		adoptedVersion, err := repo.RegisteredMediaVersion(assetID, task.ID, resource.ID, candidate.AdoptedVersionID, roles)
		if err != nil {
			return err
		}
		if adoptedVersion == "" {
			if meta.VersionID == "" || meta.VersionID != req.ExpectedVersionID || asset.PrimaryVersionID != req.ExpectedVersionID {
				return projectAssetVersionConflict()
			}
			if asset.Category == model.AssetCategoryCharacter {
				now := time.Now()
				reps := []model.AssetRepresentation{}
				for _, role := range []string{"primary", "turnaround_sheet"} {
					reps = append(reps, model.AssetRepresentation{ID: newID(), TaskID: task.ID, ResourceID: resource.ID, Role: role, MediaType: "image", MetadataJSON: `{"source":"character_turnaround"}`, CreatedAt: now})
				}
				version, err := writer.createNextCharacterVersion(projectID, asset, asset.Title, "", reps, nil, false)
				if err != nil {
					return err
				}
				adoptedVersion = version.ID
			} else {
				if _, err := writer.bindProjectAssetImage(userID, projectID, assetID, BindProjectAssetImageRequest{ResourceID: resource.ID, ExpectedVersionID: req.ExpectedVersionID}, task.ID); err != nil {
					return err
				}
				latest, err := repo.AssetForUser(userID, assetID)
				if err != nil {
					return err
				}
				adoptedVersion = latest.PrimaryVersionID
			}
		}
		candidate.State = "adopted"
		candidate.AdoptedVersionID = adoptedVersion
		result["assetImageCandidate"] = candidate
		encoded, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return repo.UpdateImageCandidateResult(userID, task.ID, string(encoded))
	})
	})
	if err != nil {
		return ProjectAssetSummary{}, projectAssetWriteError(err)
	}
	asset, err := s.projectImageAsset(userID, projectID, assetID)
	if err != nil {
		return ProjectAssetSummary{}, err
	}
	return s.projectAssetSummary(userID, projectID, asset)
}
