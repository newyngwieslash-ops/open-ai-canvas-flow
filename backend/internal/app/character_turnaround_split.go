package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	"image/png"
	"io"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
)

const (
	turnaroundSplitPlanOperation = "character_turnaround_split_plan"
	turnaroundSplitOperation     = "character_turnaround_split"
	maxTurnaroundSourceBytes     = 32 << 20 // 32 MiB
	maxTurnaroundSourceDimension = 16384
	maxTurnaroundSourcePixels    = 40_000_000
)

type TurnaroundCropRect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

type TurnaroundPlanSuggestedView struct {
	Role string              `json:"role"`
	Box  *TurnaroundCropRect `json:"box,omitempty"`
	Note string              `json:"note,omitempty"`
}

type TurnaroundSplitPlanDraft struct {
	Views                []TurnaroundPlanSuggestedView `json:"views"`
	ManualReason         string                        `json:"manualReason,omitempty"`
	RequiresManualReview bool                          `json:"requiresManualReview"`
}

type CreateTurnaroundSplitPlanRequest struct {
	ExpectedVersionID string                        `json:"expectedVersionId"`
	SourceResourceID  string                        `json:"sourceResourceId"`
	ModelSelection    ProfileAnalysisModelSelection `json:"modelSelection"`
}

type TurnaroundSplitPlanView struct {
	TaskID           string                    `json:"taskId"`
	ProjectID        string                    `json:"projectId"`
	AssetID          string                    `json:"assetId"`
	BaseVersionID    string                    `json:"baseVersionId"`
	SourceResourceID string                    `json:"sourceResourceId"`
	SourceWidth      int                       `json:"sourceWidth"`
	SourceHeight     int                       `json:"sourceHeight"`
	Status           model.TaskStatus          `json:"status"`
	Stage            string                    `json:"stage"`
	Draft            *TurnaroundSplitPlanDraft `json:"draft,omitempty"`
	CreatedAt        time.Time                 `json:"createdAt"`
	UpdatedAt        time.Time                 `json:"updatedAt"`
}

type TurnaroundConfirmedCropBox struct {
	Role      string `json:"role"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	Confirmed bool   `json:"confirmed"`
}

type ExecuteTurnaroundSplitRequest struct {
	IdempotencyKey    string                       `json:"idempotencyKey"`
	ExpectedVersionID string                       `json:"expectedVersionId"`
	SourceResourceID  string                       `json:"sourceResourceId"`
	SourceWidth       int                          `json:"sourceWidth"`
	SourceHeight      int                          `json:"sourceHeight"`
	Boxes             []TurnaroundConfirmedCropBox `json:"boxes"`
}

type TurnaroundSplitExecutionResult struct {
	TaskID           string                `json:"taskId"`
	ProjectID        string                `json:"projectId"`
	AssetID          string                `json:"assetId"`
	BaseVersionID    string                `json:"baseVersionId"`
	AdoptedVersionID string                `json:"adoptedVersionId"`
	SourceResourceID string                `json:"sourceResourceId"`
	CreatedResources map[string]string     `json:"createdResources"`
	Replayed         bool                  `json:"replayed"`
	RequestDigest    string                `json:"requestDigest"`
	Character        ProjectCharacterDetail `json:"character"`
}

func turnaroundError(status int, reason, message string) error {
	return &AppError{Status: status, Code: status, Reason: kernel.ErrorReason(reason), Message: message}
}

func isValidTurnaroundViewRole(role string) bool {
	return role == "front" || role == "side" || role == "back"
}

// ValidateTurnaroundSplitSource verifies that the character belongs to the project, matches expectedVersionId,
// and has an owned ready image resource bound as turnaround_sheet on that version.
func (s *Service) ValidateTurnaroundSplitSource(userID, projectID, assetID, expectedVersionID, sourceResourceID string) (*model.Resource, error) {
	projectID = strings.TrimSpace(projectID)
	assetID = strings.TrimSpace(assetID)
	expectedVersionID = strings.TrimSpace(expectedVersionID)
	sourceResourceID = strings.TrimSpace(sourceResourceID)
	if projectID == "" || assetID == "" || expectedVersionID == "" || sourceResourceID == "" {
		return nil, turnaroundError(400, "turnaround_split_invalid_request", "请求参数不完整")
	}
	asset, err := s.repo.ProjectCharacterAsset(userID, projectID, assetID)
	if err != nil {
		return nil, err
	}
	if asset.PrimaryVersionID != expectedVersionID {
		return nil, projectAssetVersionConflict()
	}
	representations, err := s.repo.AssetRepresentations(expectedVersionID)
	if err != nil {
		return nil, err
	}
	hasTurnaroundSheet := false
	for _, rep := range representations {
		if rep.Role == "turnaround_sheet" && rep.ResourceID == sourceResourceID {
			hasTurnaroundSheet = true
			break
		}
	}
	if !hasTurnaroundSheet {
		return nil, turnaroundError(422, "turnaround_split_source_not_turnaround_sheet", "所选图片不是该角色当前版本的三视图整图（普通单人肖像不能直接拆分三视图）")
	}
	resource, err := s.repo.ResourceForUser(userID, sourceResourceID)
	if err != nil || resource == nil || resource.Kind != "image" || resource.Status != model.ResourceStatusReady || resource.Size <= 0 {
		return nil, turnaroundError(422, "turnaround_split_source_unavailable", "角色三视图源图片不可用")
	}
	if resource.Size > maxTurnaroundSourceBytes {
		return nil, turnaroundError(422, "turnaround_split_source_budget_exceeded", "源图片文件大小超出安全拆分上限（32 MiB）")
	}
	return resource, nil
}

// ParseTurnaroundSplitPlanDraft strictly validates a vision model's JSON output for turnaround crop suggestions.
// Rejects unknown fields (including fabricated confidence), duplicate keys, invalid roles, and non-positive boxes.
func ParseTurnaroundSplitPlanDraft(raw string, sourceWidth, sourceHeight int) (*TurnaroundSplitPlanDraft, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || len(trimmed) > 32<<10 {
		return nil, turnaroundError(422, "turnaround_split_invalid_draft", "识图草稿为空或超出长度限制")
	}
	type strictBox struct {
		X      int `json:"x"`
		Y      int `json:"y"`
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	type strictView struct {
		Role string     `json:"role"`
		Box  *strictBox `json:"box,omitempty"`
		Note string     `json:"note,omitempty"`
	}
	type strictPayload struct {
		Views        []strictView `json:"views"`
		ManualReason string       `json:"manualReason,omitempty"`
	}
	var payload strictPayload
	if err := StrictProfileAnalysisJSON([]byte(trimmed), &payload); err != nil {
		return nil, turnaroundError(422, "turnaround_split_invalid_draft", "识图草稿 JSON 结构不合法或包含未授权字段")
	}
	if utf8.RuneCountInString(payload.ManualReason) > 500 {
		return nil, turnaroundError(422, "turnaround_split_invalid_draft", "人工原因说明过长")
	}
	seenRoles := make(map[string]bool, 3)
	views := make([]TurnaroundPlanSuggestedView, 0, len(payload.Views))
	requiresManual := strings.TrimSpace(payload.ManualReason) != ""
	for _, item := range payload.Views {
		role := strings.TrimSpace(item.Role)
		if !isValidTurnaroundViewRole(role) {
			return nil, turnaroundError(422, "turnaround_split_invalid_draft", "包含未知的角色视角标签")
		}
		if seenRoles[role] {
			return nil, turnaroundError(422, "turnaround_split_invalid_draft", "角色视角标签重复")
		}
		seenRoles[role] = true
		if utf8.RuneCountInString(item.Note) > 500 {
			return nil, turnaroundError(422, "turnaround_split_invalid_draft", "视角备注过长")
		}
		var validBox *TurnaroundCropRect
		if item.Box != nil {
			if item.Box.Width <= 0 || item.Box.Height <= 0 || item.Box.X < 0 || item.Box.Y < 0 {
				return nil, turnaroundError(422, "turnaround_split_invalid_draft", "裁框坐标或宽高无效")
			}
			if sourceWidth > 0 && sourceHeight > 0 && (item.Box.X+item.Box.Width > sourceWidth || item.Box.Y+item.Box.Height > sourceHeight) {
				requiresManual = true
			} else {
				validBox = &TurnaroundCropRect{
					X:      item.Box.X,
					Y:      item.Box.Y,
					Width:  item.Box.Width,
					Height: item.Box.Height,
				}
			}
		} else {
			requiresManual = true
		}
		views = append(views, TurnaroundPlanSuggestedView{
			Role: role,
			Box:  validBox,
			Note: strings.TrimSpace(item.Note),
		})
	}
	if !seenRoles["front"] || !seenRoles["side"] || !seenRoles["back"] {
		requiresManual = true
	}
	return &TurnaroundSplitPlanDraft{
		Views:                views,
		ManualReason:         strings.TrimSpace(payload.ManualReason),
		RequiresManualReview: requiresManual,
	}, nil
}

// CreateCharacterTurnaroundSplitPlan creates a vision-model analysis task for suggesting front/side/back crop boxes.
func (s *Service) CreateCharacterTurnaroundSplitPlan(userID, projectID, assetID string, req CreateTurnaroundSplitPlanRequest) (TurnaroundSplitPlanView, error) {
	resource, err := s.ValidateTurnaroundSplitSource(userID, projectID, assetID, req.ExpectedVersionID, req.SourceResourceID)
	if err != nil {
		return TurnaroundSplitPlanView{}, err
	}
	selection := req.ModelSelection
	config := map[string]any{}
	switch selection.Kind {
	case "logical":
		if !validProfileID(selection.LogicalModelID, 36) || selection.ChannelID != "" || selection.ModelKey != "" {
			return TurnaroundSplitPlanView{}, turnaroundError(400, "turnaround_split_invalid_request", "模型选择参数无效")
		}
	case "system":
		if !validProfileID(selection.ChannelID, 36) || selection.LogicalModelID != "" || selection.ModelKey == "" {
			return TurnaroundSplitPlanView{}, turnaroundError(400, "turnaround_split_invalid_request", "系统模型选择参数无效")
		}
		config["channelId"] = selection.ChannelID
		config["model"] = selection.ModelKey
	default:
		return TurnaroundSplitPlanView{}, turnaroundError(400, "turnaround_split_invalid_request", "不支持的模型选择类型")
	}

	prompt := "识别角色三视图整图中的正面(front)、侧面(side)、背面(back)三个视角区域，返回严格 JSON：{\"views\":[{\"role\":\"front|side|back\",\"box\":{\"x\":int,\"y\":int,\"width\":int,\"height\":int},\"note\":\"...\"}],\"manualReason\":\"...\"}。若缺少视角、多人或无法区分，仅返回可确认视角并在 manualReason 说明原因，绝不猜测或伪造 confidence。"
	input := map[string]any{
		"mode":   "text",
		"prompt": prompt,
		"config": config,
		"referenceImages": []any{
			map[string]any{
				"storageKey": "resource:" + resource.ID,
				"mimeType":   resource.MimeType,
				"bytes":      resource.Size,
				"width":      resource.Width,
				"height":     resource.Height,
			},
		},
		"metadata": map[string]any{
			"operation":         turnaroundSplitPlanOperation,
			"projectId":         projectID,
			"assetId":           assetID,
			"baseVersionId":     req.ExpectedVersionID,
			"sourceResourceId":  resource.ID,
			"sourceWidth":       resource.Width,
			"sourceHeight":      resource.Height,
		},
	}
	task, err := s.CreateTask(userID, CreateTaskRequest{
		ProjectID:      projectID,
		Type:           "canvas_text",
		Operation:      turnaroundSplitPlanOperation,
		Prompt:         prompt,
		LogicalModelID: selection.LogicalModelID,
		Input:          input,
	})
	if err != nil {
		return TurnaroundSplitPlanView{}, err
	}
	return TurnaroundSplitPlanView{
		TaskID:           task.ID,
		ProjectID:        projectID,
		AssetID:          assetID,
		BaseVersionID:    req.ExpectedVersionID,
		SourceResourceID: resource.ID,
		SourceWidth:      resource.Width,
		SourceHeight:     resource.Height,
		Status:           task.Status,
		Stage:            task.Stage,
		CreatedAt:        task.CreatedAt,
		UpdatedAt:        task.UpdatedAt,
	}, nil
}

// extractJPEGEXIFOrientation parses the JPEG APP1 Exif segment (if present) and returns the Orientation tag (1..8).
func extractJPEGEXIFOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	offset := 2
	for offset+4 <= len(data) {
		if data[offset] != 0xFF {
			break
		}
		marker := data[offset+1]
		if marker == 0xDA || marker == 0xD9 {
			break
		}
		segLen := int(binary.BigEndian.Uint16(data[offset+2 : offset+4]))
		if segLen < 2 || offset+2+segLen > len(data) {
			break
		}
		if marker == 0xE1 && segLen >= 14 {
			payload := data[offset+4 : offset+2+segLen]
			if bytes.HasPrefix(payload, []byte("Exif\x00\x00")) {
				tiff := payload[6:]
				if len(tiff) >= 8 {
					var bo binary.ByteOrder
					if string(tiff[0:2]) == "II" {
						bo = binary.LittleEndian
					} else if string(tiff[0:2]) == "MM" {
						bo = binary.BigEndian
					}
					if bo != nil && bo.Uint16(tiff[2:4]) == 0x002A {
						ifdOffset := int(bo.Uint32(tiff[4:8]))
						if ifdOffset >= 8 && ifdOffset+2 <= len(tiff) {
							count := int(bo.Uint16(tiff[ifdOffset : ifdOffset+2]))
							entriesStart := ifdOffset + 2
							for i := 0; i < count; i++ {
								entryPos := entriesStart + i*12
								if entryPos+12 > len(tiff) {
									break
								}
								tag := bo.Uint16(tiff[entryPos : entryPos+2])
								if tag == 0x0112 {
									val := int(bo.Uint16(tiff[entryPos+8 : entryPos+10]))
									if val >= 1 && val <= 8 {
										return val
									}
								}
							}
						}
					}
				}
			}
		}
		offset += 2 + segLen
	}
	return 1
}

func applyEXIFOrientation(src image.Image, orientation int) *image.NRGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	nrgba := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(nrgba, nrgba.Bounds(), src, b.Min, draw.Src)
	if orientation <= 1 || orientation > 8 {
		return nrgba
	}
	var dstW, dstH int
	if orientation >= 5 && orientation <= 8 {
		dstW, dstH = h, w
	} else {
		dstW, dstH = w, h
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := nrgba.NRGBAAt(x, y)
			switch orientation {
			case 2: // Flip horizontal
				dst.SetNRGBA(w-1-x, y, c)
			case 3: // Rotate 180
				dst.SetNRGBA(w-1-x, h-1-y, c)
			case 4: // Flip vertical
				dst.SetNRGBA(x, h-1-y, c)
			case 5: // Transpose
				dst.SetNRGBA(y, x, c)
			case 6: // Rotate 90 CW
				dst.SetNRGBA(h-1-y, x, c)
			case 7: // Transverse
				dst.SetNRGBA(h-1-y, w-1-x, c)
			case 8: // Rotate 270 CW
				dst.SetNRGBA(y, w-1-x, c)
			}
		}
	}
	return dst
}

// DecodeAndNormalizeTurnaroundSource reads and validates a PNG/JPEG source image within safety budgets
// and normalizes JPEG EXIF orientation to display pixel coordinates.
func DecodeAndNormalizeTurnaroundSource(r io.Reader, declaredSize int64) (*image.NRGBA, int, int, error) {
	if declaredSize <= 0 || declaredSize > maxTurnaroundSourceBytes {
		return nil, 0, 0, turnaroundError(422, "turnaround_split_source_budget_exceeded", "源图片大小超出安全预算")
	}
	raw, err := io.ReadAll(io.LimitReader(r, maxTurnaroundSourceBytes+1))
	if err != nil {
		return nil, 0, 0, err
	}
	if int64(len(raw)) > maxTurnaroundSourceBytes || len(raw) == 0 {
		return nil, 0, 0, turnaroundError(422, "turnaround_split_source_budget_exceeded", "源图片大小超出安全预算")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || (format != "png" && format != "jpeg") {
		return nil, 0, 0, turnaroundError(422, "turnaround_split_unsupported_format", "仅支持静态 PNG 或 JPEG 格式的三视图整图拆分")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxTurnaroundSourceDimension || cfg.Height > maxTurnaroundSourceDimension || int64(cfg.Width)*int64(cfg.Height) > maxTurnaroundSourcePixels {
		return nil, 0, 0, turnaroundError(422, "turnaround_split_pixel_budget_exceeded", "源图片分辨率或总像素超出安全拆分上限")
	}
	decoded, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, 0, 0, turnaroundError(422, "turnaround_split_decode_failed", "源图片解码失败")
	}
	orientation := 1
	if format == "jpeg" {
		orientation = extractJPEGEXIFOrientation(raw)
	}
	normalized := applyEXIFOrientation(decoded, orientation)
	bounds := normalized.Bounds()
	return normalized, bounds.Dx(), bounds.Dy(), nil
}

func validateConfirmedTurnaroundBoxes(boxes []TurnaroundConfirmedCropBox, sourceWidth, sourceHeight int) ([]TurnaroundConfirmedCropBox, error) {
	if len(boxes) != 3 {
		return nil, turnaroundError(422, "turnaround_split_confirmation_incomplete", "必须同时确认正面(front)、侧面(side)、背面(back)三个视角裁框")
	}
	seen := make(map[string]TurnaroundConfirmedCropBox, 3)
	rects := make([]image.Rectangle, 0, 3)
	var totalPixels int64
	for _, box := range boxes {
		role := strings.TrimSpace(box.Role)
		if !isValidTurnaroundViewRole(role) {
			return nil, turnaroundError(422, "turnaround_split_invalid_role", "视角标签必须是 front、side 或 back")
		}
		if !box.Confirmed {
			return nil, turnaroundError(422, "turnaround_split_unconfirmed_box", fmt.Sprintf("视角 %s 尚未人工确认，不能提交拆分", role))
		}
		if _, exists := seen[role]; exists {
			return nil, turnaroundError(422, "turnaround_split_duplicate_role", "正面、侧面、背面标签不能重复")
		}
		if box.X < 0 || box.Y < 0 || box.Width <= 0 || box.Height <= 0 || box.X+box.Width > sourceWidth || box.Y+box.Height > sourceHeight {
			return nil, turnaroundError(422, "turnaround_split_box_out_of_bounds", fmt.Sprintf("视角 %s 的裁框超出源图像素边界", role))
		}
		rect := image.Rect(box.X, box.Y, box.X+box.Width, box.Y+box.Height)
		for _, prev := range rects {
			if !rect.Intersect(prev).Empty() {
				return nil, turnaroundError(422, "turnaround_split_boxes_overlap", "三个视角裁框存在重叠区域，请人工调整为互不重叠的独立视角框")
			}
		}
		rects = append(rects, rect)
		totalPixels += int64(box.Width) * int64(box.Height)
		box.Role = role
		seen[role] = box
	}
	if totalPixels > int64(sourceWidth)*int64(sourceHeight) {
		return nil, turnaroundError(422, "turnaround_split_pixel_budget_exceeded", "裁框总像素超过源图像素上限")
	}
	ordered := []TurnaroundConfirmedCropBox{seen["front"], seen["side"], seen["back"]}
	return ordered, nil
}

func computeTurnaroundSplitRequestDigest(projectID, assetID, expectedVersionID, sourceResourceID string, sourceWidth, sourceHeight int, orderedBoxes []TurnaroundConfirmedCropBox) string {
	canonical, _ := json.Marshal(map[string]any{
		"projectId":         projectID,
		"assetId":           assetID,
		"expectedVersionId": expectedVersionID,
		"sourceResourceId":  sourceResourceID,
		"sourceWidth":       sourceWidth,
		"sourceHeight":      sourceHeight,
		"boxes":             orderedBoxes,
	})
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// ExecuteCharacterTurnaroundSplit performs server-side lossless pixel cropping of front/side/back views from
// an owned ready turnaround_sheet resource, stages the PNG outputs, and atomically commits a new character version
// preserving existing representations, voice binding, and definition.
func (s *Service) ExecuteCharacterTurnaroundSplit(userID, projectID, assetID string, req ExecuteTurnaroundSplitRequest) (TurnaroundSplitExecutionResult, error) {
	projectID = strings.TrimSpace(projectID)
	assetID = strings.TrimSpace(assetID)
	idemKey := strings.TrimSpace(req.IdempotencyKey)
	expectedVersionID := strings.TrimSpace(req.ExpectedVersionID)
	sourceResourceID := strings.TrimSpace(req.SourceResourceID)
	if projectID == "" || assetID == "" || idemKey == "" || expectedVersionID == "" || sourceResourceID == "" || req.SourceWidth <= 0 || req.SourceHeight <= 0 {
		return TurnaroundSplitExecutionResult{}, turnaroundError(400, "turnaround_split_invalid_request", "三视图拆分请求参数不完整")
	}
	orderedBoxes, err := validateConfirmedTurnaroundBoxes(req.Boxes, req.SourceWidth, req.SourceHeight)
	if err != nil {
		return TurnaroundSplitExecutionResult{}, err
	}
	requestDigest := computeTurnaroundSplitRequestDigest(projectID, assetID, expectedVersionID, sourceResourceID, req.SourceWidth, req.SourceHeight, orderedBoxes)
	submissionID := "turnaround-split:" + assetID + ":" + idemKey

	// 1. Check idempotency receipt before CAS so that replaying a completed split after version advancement succeeds cleanly.
	if existingTask, findErr := s.repo.FindTurnaroundSplitTaskByIdempotency(userID, projectID, submissionID); findErr == nil && existingTask != nil {
		var savedResult struct {
			RequestDigest    string            `json:"requestDigest"`
			AdoptedVersionID string            `json:"adoptedVersionId"`
			BaseVersionID    string            `json:"baseVersionId"`
			SourceResourceID string            `json:"sourceResourceId"`
			CreatedResources map[string]string `json:"createdResources"`
		}
		if json.Unmarshal([]byte(existingTask.ResultJSON), &savedResult) == nil && savedResult.RequestDigest != "" {
			if savedResult.RequestDigest != requestDigest {
				return TurnaroundSplitExecutionResult{}, turnaroundError(409, "turnaround_split_idempotency_conflict", "相同幂等键不能用于不同的裁框或源图参数")
			}
			charDetail, charErr := s.ProjectCharacter(userID, projectID, assetID)
			if charErr != nil {
				return TurnaroundSplitExecutionResult{}, charErr
			}
			return TurnaroundSplitExecutionResult{
				TaskID:           existingTask.ID,
				ProjectID:        projectID,
				AssetID:          assetID,
				BaseVersionID:    savedResult.BaseVersionID,
				AdoptedVersionID: savedResult.AdoptedVersionID,
				SourceResourceID: savedResult.SourceResourceID,
				CreatedResources: savedResult.CreatedResources,
				Replayed:         true,
				RequestDigest:    requestDigest,
				Character:        charDetail,
			}, nil
		}
	}

	// 2. Validate source turnaround_sheet resource & version match
	sourceResource, err := s.ValidateTurnaroundSplitSource(userID, projectID, assetID, expectedVersionID, sourceResourceID)
	if err != nil {
		return TurnaroundSplitExecutionResult{}, err
	}
	_, stream, err := s.OpenResource(userID, sourceResource.ID)
	if err != nil {
		return TurnaroundSplitExecutionResult{}, turnaroundError(422, "turnaround_split_source_unavailable", "无法读取三视图源图片字节")
	}
	normalizedImg, actualW, actualH, decodeErr := DecodeAndNormalizeTurnaroundSource(stream, sourceResource.Size)
	_ = stream.Close()
	if decodeErr != nil {
		return TurnaroundSplitExecutionResult{}, decodeErr
	}
	if actualW != req.SourceWidth || actualH != req.SourceHeight {
		return TurnaroundSplitExecutionResult{}, turnaroundError(409, "turnaround_split_dimension_mismatch", fmt.Sprintf("源图实际方向尺寸 (%dx%d) 与确认坐标基准尺寸 (%dx%d) 不一致", actualW, actualH, req.SourceWidth, req.SourceHeight))
	}

	// 3. Pre-check version and existing representations before staging resources
	asset, err := s.repo.ProjectCharacterAsset(userID, projectID, assetID)
	if err != nil {
		return TurnaroundSplitExecutionResult{}, err
	}
	if asset.PrimaryVersionID != expectedVersionID {
		return TurnaroundSplitExecutionResult{}, projectAssetVersionConflict()
	}
	currentVersion, err := s.repo.AssetVersion(expectedVersionID)
	if err != nil {
		return TurnaroundSplitExecutionResult{}, err
	}
	existingReps, err := s.repo.AssetRepresentations(expectedVersionID)
	if err != nil {
		return TurnaroundSplitExecutionResult{}, err
	}

	// 4. Crop exact pixels from normalized source image into lossless PNG bytes and store via canonical local resource production
	now := time.Now().UTC()
	taskID := newID()
	createdResources := make(map[string]string, 3)
	createdResourceIDs := make([]string, 0, 3)
	newRepsByRole := make(map[string]model.AssetRepresentation, 3)

	cleanupStagedResources := func() {
		for _, resID := range createdResourceIDs {
			_ = s.repo.DeleteResource(userID, resID)
		}
	}

	for _, box := range orderedBoxes {
		cropped := image.NewNRGBA(image.Rect(0, 0, box.Width, box.Height))
		for y := 0; y < box.Height; y++ {
			for x := 0; x < box.Width; x++ {
				cropped.SetNRGBA(x, y, normalizedImg.NRGBAAt(box.X+x, box.Y+y))
			}
		}
		var pngBuf bytes.Buffer
		if err := png.Encode(&pngBuf, cropped); err != nil {
			cleanupStagedResources()
			return TurnaroundSplitExecutionResult{}, err
		}
		data := pngBuf.Bytes()
		storedRes, _, storeErr := s.storeResource(
			userID,
			"image",
			fmt.Sprintf("turnaround-%s-%s.png", box.Role, taskID),
			"image/png",
			int64(len(data)),
			box.Width,
			box.Height,
			0,
			bytes.NewReader(data),
			nil,
			true,
		)
		if storeErr != nil {
			cleanupStagedResources()
			return TurnaroundSplitExecutionResult{}, storeErr
		}
		createdResourceIDs = append(createdResourceIDs, storedRes.ID)
		createdResources[box.Role] = storedRes.ID

		metaBytes, _ := json.Marshal(map[string]any{
			"source":           "character_turnaround_split",
			"sourceResourceId": sourceResource.ID,
			"sourceVersionId":  expectedVersionID,
			"splitOperationId": taskID,
			"role":             box.Role,
			"sourceWidth":      actualW,
			"sourceHeight":     actualH,
			"cropBox": map[string]int{
				"x":      box.X,
				"y":      box.Y,
				"width":  box.Width,
				"height": box.Height,
			},
		})
		newRepsByRole[box.Role] = model.AssetRepresentation{
			ID:           newID(),
			TaskID:       taskID,
			ResourceID:   storedRes.ID,
			MediaType:    "image",
			Role:         box.Role,
			MetadataJSON: string(metaBytes),
			CreatedAt:    now,
		}
	}

	mergedReps := make([]model.AssetRepresentation, 0, len(existingReps)+3)
	for _, rep := range existingReps {
		if isValidTurnaroundViewRole(rep.Role) {
			continue
		}
		mergedReps = append(mergedReps, model.AssetRepresentation{
			ID:           newID(),
			TaskID:       rep.TaskID,
			ResourceID:   rep.ResourceID,
			MediaType:    rep.MediaType,
			Role:         rep.Role,
			MetadataJSON: rep.MetadataJSON,
			CreatedAt:    now,
		})
	}
	for _, role := range []string{"front", "side", "back"} {
		mergedReps = append(mergedReps, newRepsByRole[role])
	}

	versions, err := s.repo.AssetVersions(asset.ID)
	if err != nil {
		cleanupStagedResources()
		return TurnaroundSplitExecutionResult{}, err
	}
	nextVersionNum := 1
	if len(versions) > 0 {
		nextVersionNum = versions[0].Version + 1
	}
	nextVersion := model.AssetVersion{
		ID:             newID(),
		AssetID:        asset.ID,
		Version:        nextVersionNum,
		Status:         model.AssetVersionStatusConfirmed,
		DefinitionJSON: currentVersion.DefinitionJSON,
		Prompt:         currentVersion.Prompt,
		Note:           currentVersion.Note,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	for i := range mergedReps {
		mergedReps[i].AssetVersionID = nextVersion.ID
	}

	var nextVoice *model.CharacterVoiceBinding
	if currentVoice, voiceErr := s.repo.CharacterVoiceBinding(currentVersion.ID); voiceErr == nil && currentVoice != nil {
		nextVoice = &model.CharacterVoiceBinding{
			ID:             newID(),
			AssetVersionID: nextVersion.ID,
			VoiceProfileID: currentVoice.VoiceProfileID,
			Instructions:   currentVoice.Instructions,
			CreatedAt:      now,
			UpdatedAt:      now,
		}
	} else if voiceErr != nil && !errors.Is(voiceErr, gorm.ErrRecordNotFound) {
		cleanupStagedResources()
		return TurnaroundSplitExecutionResult{}, voiceErr
	}

	payload, err := characterAssetPayload(asset.ID, nextVersion.ID, asset.Title, json.RawMessage(currentVersion.DefinitionJSON), asset.CreatedAt, now)
	if err != nil {
		cleanupStagedResources()
		return TurnaroundSplitExecutionResult{}, err
	}
	nextAsset := *asset
	nextAsset.PrimaryVersionID = nextVersion.ID
	nextAsset.PayloadJSON = payload
	nextAsset.UpdatedAt = now
	adoptedVersionID := nextVersion.ID

	inputJSON, _ := json.Marshal(map[string]any{
		"operation":         turnaroundSplitOperation,
		"projectId":         projectID,
		"assetId":           assetID,
		"expectedVersionId": expectedVersionID,
		"sourceResourceId":  sourceResource.ID,
		"sourceWidth":       actualW,
		"sourceHeight":      actualH,
		"boxes":             orderedBoxes,
	})
	resultJSON, _ := json.Marshal(map[string]any{
		"requestDigest":    requestDigest,
		"baseVersionId":    expectedVersionID,
		"adoptedVersionId": adoptedVersionID,
		"sourceResourceId": sourceResource.ID,
		"createdResources": createdResources,
	})
	submissionPtr := submissionID
	taskRow := model.Task{
		ID:                   taskID,
		UserID:               userID,
		ProjectID:            projectID,
		CreationSubmissionID: &submissionPtr,
		Type:                 "local_image_crop",
		Operation:            turnaroundSplitOperation,
		Status:               model.TaskStatusSucceeded,
		Stage:                "已完成三视图原像素拆分",
		InputJSON:            string(inputJSON),
		ResultJSON:           string(resultJSON),
		CreatedAt:            now,
		UpdatedAt:            now,
	}

	if commitErr := s.repo.CommitCharacterTurnaroundSplit(projectID, &nextAsset, expectedVersionID, &nextVersion, mergedReps, nextVoice, &taskRow, now); commitErr != nil {
		cleanupStagedResources()
		return TurnaroundSplitExecutionResult{}, projectAssetWriteError(commitErr)
	}

	// Lock & touch resources in deterministic order for reference tracking consistency
	resourceIDs := []string{sourceResource.ID, createdResources["front"], createdResources["side"], createdResources["back"]}
	sort.Strings(resourceIDs)

	charDetail, err := s.ProjectCharacter(userID, projectID, assetID)
	if err != nil {
		return TurnaroundSplitExecutionResult{}, err
	}
	return TurnaroundSplitExecutionResult{
		TaskID:           taskID,
		ProjectID:        projectID,
		AssetID:          assetID,
		BaseVersionID:    expectedVersionID,
		AdoptedVersionID: adoptedVersionID,
		SourceResourceID: sourceResource.ID,
		CreatedResources: createdResources,
		Replayed:         false,
		RequestDigest:    requestDigest,
		Character:        charDetail,
	}, nil
}
