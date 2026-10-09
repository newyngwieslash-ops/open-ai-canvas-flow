package app

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"yingce/backend/internal/protocol"
)

const fisherH3Protocol = "fisher-h3-video"

func applyFisherH3Options(input canvasGenerationInput, request *protocol.GenerationRequest) {
	options := make(map[string]any)
	for key, value := range request.ProviderOptions[fisherH3Protocol] {
		options[key] = value
	}
	driveID, _ := options["driveAudioId"].(string)
	for index := range request.Images {
		image := &request.Images[index]
		image.Role = "reference_image"
		if id := metadataString(input.Metadata, "videoStartFrameNodeId"); id != "" && image.ID == id {
			image.Role = "first_frame"
		}
		if id := metadataString(input.Metadata, "videoEndFrameNodeId"); id != "" && image.ID == id {
			image.Role = "last_frame"
		}
	}
	for index := range request.Audios {
		if driveID != "" && request.Audios[index].ID == driveID {
			request.Audios[index].Role = "drive_audio"
		}
	}
	if starts, ok := options["videoStartTimes"].(map[string]any); ok {
		for index := range request.Videos {
			if start, exists := starts[request.Videos[index].ID]; exists {
				request.Videos[index].Metadata["start_time_seconds"] = start
			}
		}
	}
	control := map[string]any{"mode": "native", "denoise_strength": 0.35, "add_drive_as_reference": false}
	if driveID != "" {
		control = map[string]any{"mode": "lock_source", "denoise_strength": 0.0, "add_drive_as_reference": true}
	}
	if configured, ok := options["audio_control"].(map[string]any); ok {
		for key, value := range configured {
			control[key] = value
		}
	}
	options["audio_control"] = control
	request.ProviderOptions[fisherH3Protocol] = options
	request.Resolution = strings.ToUpper(request.Resolution)
	if request.AspectRatio == "" && len(request.Inputs) > 0 {
		request.AspectRatio = "16:9"
	}
}

// validateFisherH3Input runs at the host boundary, before a billable create.
func validateFisherH3Input(input canvasGenerationInput, request protocol.GenerationRequest) error {
	if len(input.ReferenceImages) != len(request.Images) || len(input.ReferenceVideos) != len(request.Videos) || len(input.ReferenceAudios) != len(request.Audios) {
		return fmt.Errorf("Fisher-H3：参考素材缺少可提交的 URL")
	}
	if raw, exists := input.Metadata["providerOptions"]; exists {
		namespaces, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("Fisher-H3：providerOptions 必须为对象")
		}
		if options, exists := namespaces[fisherH3Protocol]; exists {
			if _, ok := options.(map[string]any); !ok {
				return fmt.Errorf("Fisher-H3：提供商参数必须为对象")
			}
		}
	}
	for _, key := range []string{"videoStartFrameNodeId", "videoEndFrameNodeId"} {
		if raw, exists := input.Metadata[key]; exists {
			if _, ok := raw.(string); !ok {
				return fmt.Errorf("Fisher-H3：首尾帧 ID 必须为字符串")
			}
		}
		id := metadataString(input.Metadata, key)
		if id == "" {
			continue
		}
		found := false
		for _, image := range request.Images {
			if image.ID == id {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("Fisher-H3：所选首尾帧未包含在请求中")
		}
	}
	if id := metadataString(input.Metadata, "videoStartFrameNodeId"); id != "" && id == metadataString(input.Metadata, "videoEndFrameNodeId") {
		return fmt.Errorf("Fisher-H3：首尾帧必须选择不同素材")
	}
	options := request.ProviderOptions[fisherH3Protocol]
	if raw, exists := options["driveAudioId"]; exists {
		id, ok := raw.(string)
		if !ok {
			return fmt.Errorf("Fisher-H3：驱动音频 ID 无效")
		}
		if id != "" {
			found := false
			for _, audio := range request.Audios {
				if audio.ID == id {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("Fisher-H3：所选驱动音频未包含在请求中")
			}
		}
	}
	if raw, exists := options["videoStartTimes"]; exists {
		starts, ok := raw.(map[string]any)
		if !ok {
			return fmt.Errorf("Fisher-H3：视频起始偏移必须按素材 ID 配置")
		}
		for id := range starts {
			found := false
			for _, video := range request.Videos {
				if video.ID == id && id != "" {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("Fisher-H3：视频偏移引用的素材未包含在请求中")
			}
		}
	}
	if configured, ok := input.Metadata["providerOptions"].(map[string]any); ok {
		if namespace, ok := configured[fisherH3Protocol].(map[string]any); ok {
			if raw, exists := namespace["audio_control"]; exists {
				if _, ok := raw.(map[string]any); !ok {
					return fmt.Errorf("Fisher-H3：audio_control 必须为对象")
				}
			}
		}
	}
	if duration, err := strconv.Atoi(strings.TrimSpace(input.Config.VideoSeconds)); err != nil || duration != request.Duration {
		return fmt.Errorf("Fisher-H3：时长必须为整数秒")
	}
	return validateFisherH3Request(request)
}

func fisherNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, !math.IsNaN(v) && !math.IsInf(v, 0)
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil && !math.IsNaN(n) && !math.IsInf(n, 0)
	}
	return 0, false
}

func validateFisherH3Request(request protocol.GenerationRequest) error {
	fail := func(message string) error { return fmt.Errorf("Fisher-H3：%s", message) }
	if request.Model != "MiniMax-H3" {
		return fail("model 必须为 MiniMax-H3")
	}
	if strings.TrimSpace(request.Prompt) == "" || utf8.RuneCountInString(request.Prompt) > 10000 {
		return fail("提示词不能为空且最多 10000 字符")
	}
	switch strings.ToUpper(request.Resolution) {
	case "480P", "768P", "1080P":
	default:
		return fail("分辨率仅支持 480P、768P、1080P")
	}
	counts := map[string]int{}
	for _, image := range request.Images {
		switch image.Role {
		case "first_frame", "last_frame", "reference_image":
			counts[image.Role]++
		default:
			return fail("图片角色无效")
		}
	}
	for _, audio := range request.Audios {
		switch audio.Role {
		case "reference_audio", "drive_audio":
			counts[audio.Role]++
		default:
			return fail("音频角色无效")
		}
		if audio.Role == "drive_audio" {
			if ms, ok := fisherNumber(audio.Metadata["durationMs"]); ok && (ms < 0 || ms > 60000) {
				return fail("驱动音频时长无效或超过 60 秒")
			}
		}
	}
	if counts["reference_image"] > 9 || counts["first_frame"] > 1 || counts["last_frame"] > 1 || counts["reference_audio"] > 3 || counts["drive_audio"] > 1 || len(request.Videos) > 3 {
		return fail("参考素材或首尾帧数量超限")
	}
	maxSeconds := 15
	if counts["drive_audio"] == 1 {
		maxSeconds = 60
	}
	if request.Duration < 4 || request.Duration > maxSeconds {
		return fail(fmt.Sprintf("时长必须为 4–%d 整数秒", maxSeconds))
	}
	frames := counts["first_frame"]+counts["last_frame"] > 0
	switch request.AspectRatio {
	case "1:1", "2:3", "3:2", "3:4", "4:3", "9:16", "16:9", "21:9":
	case "adaptive", "auto":
		if !frames {
			return fail("adaptive/auto 仅支持首尾帧模式")
		}
	case "":
		if len(request.Images)+len(request.Videos)+len(request.Audios) == 0 {
			return fail("文生视频必须指定比例")
		}
	default:
		return fail("比例无效")
	}
	for _, video := range request.Videos {
		if video.Role != "reference_video" {
			return fail("视频角色无效")
		}
		start := 0.0
		if raw, exists := video.Metadata["start_time_seconds"]; exists {
			var ok bool
			start, ok = fisherNumber(raw)
			if !ok || start < 0 {
				return fail("视频起始偏移必须为非负有限数")
			}
		}
		if ms, ok := fisherNumber(video.Metadata["durationMs"]); ok && ms != 0 {
			seconds := ms / 1000
			if seconds < 2 || seconds > 15 || seconds-start < 2 || seconds-start > 15 {
				return fail("参考视频和偏移后的剩余视频须为 2–15 秒")
			}
		}
	}
	control, ok := request.ProviderOptions[fisherH3Protocol]["audio_control"].(map[string]any)
	if !ok {
		return fail("audio_control 无效")
	}
	mode, ok := control["mode"].(string)
	if !ok {
		return fail("音频模式无效")
	}
	switch mode {
	case "native":
	case "lock_source", "remix_source", "reference_only":
		if counts["drive_audio"] != 1 {
			return fail("非 native 模式必须选择驱动音频")
		}
	default:
		return fail("音频模式无效")
	}
	denoise, ok := fisherNumber(control["denoise_strength"])
	if !ok || denoise < 0 || denoise > 1 {
		return fail("去噪强度必须为 0–1")
	}
	add, ok := control["add_drive_as_reference"].(bool)
	if !ok || mode == "reference_only" && !add {
		return fail("驱动作为参考参数无效")
	}
	return nil
}

// ValidateFisherH3VideoPayload checks raw relay requests with the same limits as
// queued generation. Protocol identity is supplied by the channel, never URL guessing.
func ValidateFisherH3VideoPayload(body []byte) error {
	var payload struct {
		Model        string          `json:"model"`
		Resolution   string          `json:"resolution"`
		Ratio        string          `json:"ratio"`
		Duration     int             `json:"duration"`
		AudioControl json.RawMessage `json:"audio_control"`
		Content      []struct {
			Type     string `json:"type"`
			Text     string `json:"text"`
			Role     string `json:"role"`
			ImageURL struct {
				URL string `json:"url"`
			} `json:"image_url"`
			VideoURL struct {
				URL string `json:"url"`
			} `json:"video_url"`
			AudioURL struct {
				URL string `json:"url"`
			} `json:"audio_url"`
			Start json.RawMessage `json:"start_time_seconds"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fmt.Errorf("Fisher-H3：请求字段类型无效")
	}
	request := protocol.GenerationRequest{Model: payload.Model, Resolution: payload.Resolution, AspectRatio: payload.Ratio, Duration: payload.Duration, ProviderOptions: map[string]map[string]any{fisherH3Protocol: {}}}
	drive := false
	var prompt strings.Builder
	for _, item := range payload.Content {
		media := protocol.MediaReference{Role: item.Role, Metadata: map[string]any{}}
		switch item.Type {
		case "text":
			prompt.WriteString(item.Text)
			continue
		case "image_url":
			media.URL = item.ImageURL.URL
			if media.Role == "" {
				media.Role = "first_frame"
			}
			request.Images = append(request.Images, media)
		case "video_url":
			media.URL = item.VideoURL.URL
			if len(item.Start) > 0 {
				var start any
				if json.Unmarshal(item.Start, &start) != nil {
					return fmt.Errorf("Fisher-H3：视频偏移无效")
				}
				media.Metadata["start_time_seconds"] = start
			}
			request.Videos = append(request.Videos, media)
		case "audio_url":
			media.URL = item.AudioURL.URL
			drive = drive || media.Role == "drive_audio"
			request.Audios = append(request.Audios, media)
		default:
			return fmt.Errorf("Fisher-H3：content 类型无效")
		}
		if strings.TrimSpace(media.URL) == "" {
			return fmt.Errorf("Fisher-H3：参考素材 URL 不能为空")
		}
	}
	request.Prompt = prompt.String()
	control := map[string]any{"mode": "native", "denoise_strength": 0.35, "add_drive_as_reference": false}
	if drive {
		control = map[string]any{"mode": "lock_source", "denoise_strength": 0.0, "add_drive_as_reference": true}
	}
	if len(payload.AudioControl) > 0 {
		var configured map[string]any
		if json.Unmarshal(payload.AudioControl, &configured) != nil || configured == nil {
			return fmt.Errorf("Fisher-H3：audio_control 必须为对象")
		}
		for key, value := range configured {
			control[key] = value
		}
	}
	request.ProviderOptions[fisherH3Protocol]["audio_control"] = control
	if request.AspectRatio == "" && len(request.Images)+len(request.Videos)+len(request.Audios) > 0 {
		request.AspectRatio = "16:9"
	}
	return validateFisherH3Request(request)
}

type fisherSubmissionKey struct{}
type fisherSubmission struct {
	TaskID       string
	PersistError error
}

func validFisherTaskID(value string) string {
	if len(value) == 0 || len(value) > 256 {
		return ""
	}
	first := value[0]
	if !(first >= 'a' && first <= 'z' || first >= 'A' && first <= 'Z' || first >= '0' && first <= '9') {
		return ""
	}
	for _, c := range value {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:", c)) {
			return ""
		}
	}
	return value
}

func persistFisherTaskID(ctx context.Context, submission *fisherSubmission, id string) error {
	if id = validFisherTaskID(id); id == "" {
		return nil
	}
	submission.TaskID = id
	metadata, _ := ctx.Value(providerAnalyticsKey{}).(providerAnalyticsContext)
	if metadata.Service != nil && metadata.TaskID != "" {
		next := time.Now().Add(defaultVideoPollInterval)
		if err := metadata.Service.repo.UpdateTaskProviderState(metadata.TaskID, id, "accepted", &next); err != nil {
			submission.PersistError = routeDispatchUncertainError{fmt.Sprintf("Fisher-H3 任务编号保存失败，请核对已有任务 %s：%v", id, err)}
			return submission.PersistError
		}
		stored, err := metadata.Service.repo.Task(metadata.TaskID)
		if err != nil || stored.ProviderRequestID != id {
			submission.PersistError = routeDispatchUncertainError{"Fisher-H3 任务编号未能持久保存，请核对已有任务与费用"}
			return submission.PersistError
		}
	}
	return nil
}

func FisherH3TaskID(value string, apiKey string) string {
	if apiKey != "" && strings.Contains(value, apiKey) {
		return ""
	}
	return validFisherTaskID(value)
}
