package protocol

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func metasoRequest() GenerationRequest {
	return GenerationRequest{Model: "MiniMax-H3", Prompt: "A person turns naturally.", Duration: 5, Resolution: "768P", AspectRatio: "16:9"}
}

func TestMetasoMinimaxPackage(t *testing.T) {
	packagePath := filepath.Join("..", "..", "..", "plugin-packages")
	archive, err := os.ReadFile(filepath.Join(packagePath, "metaso-minimax-video.yingce-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ParsePluginPackage(archive)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(packagePath, "metaso-minimax-video", "manifest.json"))
	if err != nil || !bytes.Equal(source, pkg.ManifestRaw) {
		t.Fatalf("package manifest differs from generated source: %v", err)
	}
	provider := pkg.Manifest.Contributes.Providers[0]
	workflows := pkg.Manifest.Contributes.Workflows
	if provider.Label != "MiniMax-H3 (Metaso)" || provider.BaseURL != "https://metaso.cn/api/minimax" || !provider.RequiresPublicMediaURLs || provider.Auth.Type != "bearer" || len(workflows) != 1 || workflows[0].ProviderID != "metaso-minimax-video" || workflows[0].Defaults["model"] != "MiniMax-H3" || workflows[0].Defaults["resolution"] != "768P" {
		t.Fatalf("Metaso identity/workflow = %#v, %#v", provider, workflows)
	}
	if !reflect.DeepEqual(workflows[0].Parameters, provider.Parameters) {
		t.Fatal("Metaso workflow parameters differ from provider contract")
	}
}

func TestMetasoMinimaxContentAssembly(t *testing.T) {
	adapter := officialPackageAdapter(t, "metaso-minimax-video.yingce-plugin", "metaso-minimax-video")
	parameters := make(map[string]Parameter)
	for _, parameter := range adapter.Metadata().Parameters {
		parameters[parameter.Name] = parameter
	}
	for _, unsupported := range []string{"generateAudio", "watermark"} {
		if _, exists := parameters[unsupported]; exists {
			t.Errorf("Metaso advertises unsupported parameter %s", unsupported)
		}
	}
	for _, required := range []string{"duration", "resolution"} {
		if !parameters[required].Required {
			t.Errorf("Metaso parameter %s must be required", required)
		}
	}
	// 文生视频：仅文本条目。
	textOnly := metasoRequest()
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: textOnly})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != "/v2/video_generation" || spec.Method != "POST" || spec.Auth.Type != "bearer" {
		t.Fatalf("create transport = %#v", spec)
	}
	body := manifestTestBody(t, spec)
	if body["model"] != "MiniMax-H3" || body["resolution"] != "768P" || body["ratio"] != "16:9" || body["duration"] != float64(5) {
		t.Fatalf("text-only parameters = %#v", body)
	}
	content := body["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["type"] != "text" {
		t.Fatalf("text-only content = %#v", content)
	}
	for _, key := range []string{"prompt", "audio_control", "generate_audio", "aigc_watermark", "seed"} {
		if _, exists := body[key]; exists {
			t.Fatalf("unexpected field %s", key)
		}
	}
	// 图生视频：首帧 role；多模态：参考视频 + 参考音频 role；无 start_time_seconds。
	request := metasoRequest()
	request.Resolution = "2K"
	request.Images = []MediaReference{{URL: "https://example.com/first.jpg", Role: "first_frame"}}
	request.Videos = []MediaReference{{URL: "https://example.com/ref.mp4", Role: "reference_video"}}
	request.Audios = []MediaReference{{URL: "https://example.com/dialogue.wav", Role: "reference_audio"}}
	spec, err = adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body = manifestTestBody(t, spec)
	if body["resolution"] != "2K" {
		t.Fatalf("2K must pass through verbatim: %#v", body)
	}
	content = body["content"].([]any)
	if len(content) != 4 {
		t.Fatalf("multimodal content = %#v", content)
	}
	if content[1].(map[string]any)["role"] != "first_frame" {
		t.Fatalf("first frame role = %#v", content[1])
	}
	video := content[2].(map[string]any)
	if video["role"] != "reference_video" {
		t.Fatalf("video role = %#v", video)
	}
	if _, exists := video["start_time_seconds"]; exists {
		t.Fatalf("Metaso must not send start_time_seconds: %#v", video)
	}
	if content[3].(map[string]any)["role"] != "reference_audio" {
		t.Fatalf("audio role = %#v", content[3])
	}
	// 参考视频未显式指定比例时缺省 16:9 已由 body["ratio"] 断言覆盖。
	polled, err := adapter.BuildPoll(context.Background(), PollContext{TaskID: "task-123"})
	if err != nil || polled.Path != "/v2/query/video_generation/task-123" || polled.Method != "GET" {
		t.Fatalf("poll = %#v, %v", polled, err)
	}
}

func TestMetasoMinimaxDefaultsAndValidation(t *testing.T) {
	adapter := officialPackageAdapter(t, "metaso-minimax-video.yingce-plugin", "metaso-minimax-video")
	// 参考素材存在时比例缺省 16:9、480P 小写归一为大写。
	request := metasoRequest()
	request.Resolution = "480p"
	request.AspectRatio = ""
	request.Images = []MediaReference{{URL: "https://example.com/first.jpg", Role: "first_frame"}}
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body := manifestTestBody(t, spec)
	if body["resolution"] != "480P" || body["ratio"] != "16:9" {
		t.Fatalf("reference defaults = %#v", body)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*GenerationRequest)
	}{
		{"model", func(r *GenerationRequest) { r.Model = "minimax-h3" }},
		{"prompt", func(r *GenerationRequest) { r.Prompt = "  " }},
		{"prompt too long", func(r *GenerationRequest) { r.Prompt = string(make([]byte, 10001)) }},
		{"resolution", func(r *GenerationRequest) { r.Resolution = "2160p" }},
		{"adaptive", func(r *GenerationRequest) { r.Resolution = "adaptive" }},
		{"duration low", func(r *GenerationRequest) { r.Duration = 3 }},
		{"duration high", func(r *GenerationRequest) { r.Duration = 16 }},
		{"text ratio", func(r *GenerationRequest) { r.AspectRatio = "" }},
		{"bad ratio", func(r *GenerationRequest) { r.AspectRatio = "2:3" }},
		{"two first frames", func(r *GenerationRequest) { r.Images = []MediaReference{{Role: "first_frame"}, {Role: "first_frame"}} }},
		{"last frame", func(r *GenerationRequest) { r.Images = []MediaReference{{Role: "last_frame"}} }},
		{"ten reference images", func(r *GenerationRequest) {
			for i := 0; i < 10; i++ {
				r.Images = append(r.Images, MediaReference{Role: "reference_image"})
			}
		}},
		{"four videos", func(r *GenerationRequest) {
			for i := 0; i < 4; i++ {
				r.Videos = append(r.Videos, MediaReference{Role: "reference_video"})
			}
		}},
		{"four audios", func(r *GenerationRequest) {
			for i := 0; i < 4; i++ {
				r.Audios = append(r.Audios, MediaReference{Role: "reference_audio"})
			}
		}},
		{"invalid image role", func(r *GenerationRequest) { r.Images = []MediaReference{{Role: "unknown"}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := metasoRequest()
			tc.mutate(&request)
			if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
				t.Fatal("invalid Metaso request accepted")
			}
		})
	}
}

func TestMetasoMinimaxResponse(t *testing.T) {
	adapter := officialPackageAdapter(t, "metaso-minimax-video.yingce-plugin", "metaso-minimax-video")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"task_id":"task-123"}`))
	if err != nil || created.TaskID != "task-123" || created.Status != StatusPending {
		t.Fatalf("created = %#v, %v", created, err)
	}
	// 追踪 request_id 不得成为任务编号。
	tracked, err := adapter.ParseCreate(context.Background(), []byte(`{"request_id":"trace-123"}`))
	if err != nil || tracked.TaskID != "" {
		t.Fatalf("tracking ID must not become task ID: %#v, %v", tracked, err)
	}
	for _, tc := range []struct {
		payload string
		status  Status
		message string
	}{
		{`{"task":{"status":"queued"}}`, StatusPending, ""},
		{`{"task":{"status":"running"}}`, StatusProcessing, ""},
		{`{"task":{"status":"failed","error":{"message":"rejected"}}}`, StatusFailed, "rejected"},
		{`{"error":{"code":"1001","message":"quota exceeded"}}`, StatusFailed, "quota exceeded"},
		{`{"base_resp":{"status_code":1004,"status_msg":"invalid api key"}}`, StatusFailed, "invalid api key"},
		{`{"task":{"status":"cancelled"}}`, StatusCancelled, ""},
		{`{"task":{"status":"succeeded","content":{"url":"https://example.com/result.mp4"}}}`, StatusSucceeded, ""},
	} {
		result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-123"}, []byte(tc.payload))
		if err != nil || result.Status != tc.status || result.TaskID != "task-123" || result.Message != tc.message {
			t.Fatalf("poll = %#v, %v", result, err)
		}
		if tc.status == StatusSucceeded && (result.Result == nil || len(result.Result.Videos) != 1 || result.Result.Videos[0].URL != "https://example.com/result.mp4") {
			t.Fatalf("successful result = %#v", result)
		}
	}
}
