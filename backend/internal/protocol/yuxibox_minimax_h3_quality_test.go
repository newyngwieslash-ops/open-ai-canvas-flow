package protocol

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func yuxiboxQualityRequest() GenerationRequest {
	return GenerationRequest{
		Model:       "minimax-h3-quality",
		Prompt:      "少女在雨巷中转身回望。",
		Duration:    5,
		Resolution:  "768P",
		AspectRatio: "9:16",
		Images:      []MediaReference{{URL: "https://example.com/ref-1.jpg"}},
	}
}

func TestYuxiboxMinimaxH3QualityPackage(t *testing.T) {
	packagePath := filepath.Join("..", "..", "..", "plugin-packages")
	archive, err := os.ReadFile(filepath.Join(packagePath, "yuxibox-minimax-h3-quality.yingce-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ParsePluginPackage(archive)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(packagePath, "yuxibox-minimax-h3-quality", "manifest.json"))
	if err != nil || !bytes.Equal(source, pkg.ManifestRaw) {
		t.Fatalf("package manifest differs from generated source: %v", err)
	}
	provider := pkg.Manifest.Contributes.Providers[0]
	workflows := pkg.Manifest.Contributes.Workflows
	if provider.Label != "yuxibox minimax-h3-quality" || provider.BaseURL != "https://yuxibox.cn" || !provider.RequiresPublicMediaURLs || provider.Auth.Type != "bearer" || len(workflows) != 1 || workflows[0].ProviderID != "yuxibox-minimax-h3-quality" {
		t.Fatalf("yuxibox quality identity/workflow = %#v, %#v", provider, workflows)
	}
	if workflows[0].Defaults["model"] != "minimax-h3-quality" || workflows[0].Defaults["duration"] != float64(5) || workflows[0].Defaults["aspectRatio"] != "9:16" || workflows[0].Defaults["resolution"] != "768p" {
		t.Fatalf("yuxibox quality workflow defaults = %#v", workflows[0].Defaults)
	}
	if !reflect.DeepEqual(workflows[0].Parameters, provider.Parameters) {
		t.Fatal("yuxibox quality workflow parameters differ from provider contract")
	}
	parameterMapping := make(map[string]string)
	for _, parameter := range provider.Parameters {
		parameterMapping[parameter.Name] = parameter.Mapping
	}
	// 该模型不支持参考视频：参数必须显式声明 unsupported，而不是静默丢弃。
	if parameterMapping["videos"] != "unsupported" {
		t.Errorf("yuxibox quality videos mapping = %q, want unsupported", parameterMapping["videos"])
	}
	for _, advertised := range []string{"resolution", "audios", "duration"} {
		if _, ok := parameterMapping[advertised]; !ok {
			t.Errorf("yuxibox quality missing parameter %s", advertised)
		}
	}
	for _, unsupported := range []string{"generateAudio", "watermark"} {
		if _, ok := parameterMapping[unsupported]; ok {
			t.Errorf("yuxibox quality advertises unsupported parameter %s", unsupported)
		}
	}
}

func TestYuxiboxMinimaxH3QualityCreateAssembly(t *testing.T) {
	adapter := officialPackageAdapter(t, "yuxibox-minimax-h3-quality.yingce-plugin", "yuxibox-minimax-h3-quality")

	// 基础请求：seconds 整数化、resolution 小写、ratio 缺省 9:16、空 audios 不出现键。
	request := yuxiboxQualityRequest()
	request.AspectRatio = ""
	request.Audios = nil
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != "/v1/videos" || spec.Method != "POST" {
		t.Fatalf("create transport = %#v", spec)
	}
	body := manifestTestBody(t, spec)
	if body["model"] != "minimax-h3-quality" || body["prompt"] != "少女在雨巷中转身回望。" || body["ratio"] != "9:16" || body["resolution"] != "768p" {
		t.Fatalf("base parameters = %#v", body)
	}
	if seconds, ok := body["seconds"]; !ok || seconds != float64(5) {
		t.Fatalf("seconds = %#v (want numeric 5)", body["seconds"])
	}
	if _, exists := body["audios"]; exists {
		t.Fatal("empty audios must be omitted")
	}
	if _, exists := body["videos"]; exists {
		t.Fatal("videos must never be sent for minimax-h3-quality")
	}

	// 720P 别名归一：能力档位暴露 480P/768P，720P 输入发往上游为 768p。
	aliasRequest := yuxiboxQualityRequest()
	aliasRequest.Resolution = "720P"
	aliasSpec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: aliasRequest})
	if err != nil {
		t.Fatal(err)
	}
	if aliasBody := manifestTestBody(t, aliasSpec); aliasBody["resolution"] != "768p" {
		t.Fatalf("720P alias resolution = %#v, want 768p", aliasBody["resolution"])
	}

	// 参考图与音频按序映射为 URL 数组。
	mediaRequest := yuxiboxQualityRequest()
	mediaRequest.Images = []MediaReference{{URL: "https://example.com/ref-1.jpg"}, {URL: "https://example.com/ref-2.jpg"}}
	mediaRequest.Audios = []MediaReference{{URL: "https://example.com/dialogue.wav"}}
	mediaSpec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: mediaRequest})
	if err != nil {
		t.Fatal(err)
	}
	mediaBody := manifestTestBody(t, mediaSpec)
	images, ok := mediaBody["reference_images"].([]any)
	if !ok || len(images) != 2 || images[0] != "https://example.com/ref-1.jpg" || images[1] != "https://example.com/ref-2.jpg" {
		t.Fatalf("reference_images = %#v", mediaBody["reference_images"])
	}
	audios, ok := mediaBody["audios"].([]any)
	if !ok || len(audios) != 1 || audios[0] != "https://example.com/dialogue.wav" {
		t.Fatalf("audios = %#v", mediaBody["audios"])
	}
}

func TestYuxiboxMinimaxH3QualityValidation(t *testing.T) {
	adapter := officialPackageAdapter(t, "yuxibox-minimax-h3-quality.yingce-plugin", "yuxibox-minimax-h3-quality")
	for _, tc := range []struct {
		name   string
		mutate func(*GenerationRequest)
	}{
		{"model renamed", func(r *GenerationRequest) { r.Model = "minimax-h3" }},
		{"prompt empty", func(r *GenerationRequest) { r.Prompt = "  " }},
		{"prompt too long", func(r *GenerationRequest) { r.Prompt = string(make([]byte, 20001)) }},
		{"duration zero", func(r *GenerationRequest) { r.Duration = 0 }},
		{"duration too high", func(r *GenerationRequest) { r.Duration = 16 }},
		{"ratio not in tier", func(r *GenerationRequest) { r.AspectRatio = "4:3" }},
		{"resolution not supported", func(r *GenerationRequest) { r.Resolution = "1080P" }},
		{"no images", func(r *GenerationRequest) { r.Images = nil }},
		{"ten images", func(r *GenerationRequest) {
			r.Images = nil
			for i := 0; i < 10; i++ {
				r.Images = append(r.Images, MediaReference{URL: "https://example.com/x.jpg"})
			}
		}},
		{"four audios", func(r *GenerationRequest) {
			for i := 0; i < 4; i++ {
				r.Audios = append(r.Audios, MediaReference{URL: "https://example.com/x.wav"})
			}
		}},
		{"reference video present", func(r *GenerationRequest) {
			r.Videos = []MediaReference{{URL: "https://example.com/x.mp4"}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := yuxiboxQualityRequest()
			tc.mutate(&request)
			if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
				t.Fatal("invalid yuxibox minimax-h3-quality request accepted")
			}
		})
	}
}

func TestYuxiboxMinimaxH3QualityPollAndResponse(t *testing.T) {
	adapter := officialPackageAdapter(t, "yuxibox-minimax-h3-quality.yingce-plugin", "yuxibox-minimax-h3-quality")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"t1","object":"video","model":"minimax-h3-quality","status":"queued"}`))
	if err != nil || created.TaskID != "t1" || created.Status != StatusPending {
		t.Fatalf("created = %#v, %v", created, err)
	}
	// 追踪编号不得成为任务编号。
	tracked, err := adapter.ParseCreate(context.Background(), []byte(`{"request_id":"trace-123"}`))
	if err != nil || tracked.TaskID != "" {
		t.Fatalf("tracking ID must not become task ID: %#v, %v", tracked, err)
	}
	polled, err := adapter.BuildPoll(context.Background(), PollContext{TaskID: "task-123"})
	if err != nil || polled.Path != "/v1/videos/task-123" || polled.Method != "GET" {
		t.Fatalf("poll = %#v, %v", polled, err)
	}
	// 内容兜底端点：video_url 缺失时宿主经 ResultAdapter 下载 MP4 二进制。
	resultCapability, ok := adapter.(ResultCapability)
	if !ok || !resultCapability.ResultAvailable() {
		t.Fatal("yuxibox minimax-h3-quality result operation is unavailable")
	}
	resultAdapter, ok := adapter.(ResultAdapter)
	if !ok {
		t.Fatal("yuxibox minimax-h3-quality does not implement ResultAdapter")
	}
	download, err := resultAdapter.BuildResult(context.Background(), PollContext{TaskID: "task-123"})
	if err != nil || download.Path != "/v1/videos/task-123/content" || download.Method != "GET" || download.Headers["Accept"] != "video/mp4" || download.Auth.Type != "bearer" {
		t.Fatalf("result = %#v, %v", download, err)
	}
	// 状态四态归一与 URL 字段 coalesce（video_url 优先于 download_url）。
	for _, tc := range []struct {
		payload string
		status  Status
		message string
	}{
		{`{"id":"t","status":"queued"}`, StatusPending, ""},
		{`{"id":"t","status":"in_progress","progress":50}`, StatusProcessing, ""},
		{`{"id":"t","status":"completed","video_url":"https://example.com/a.mp4"}`, StatusSucceeded, ""},
		{`{"id":"t","status":"completed","download_url":"https://example.com/only.mp4"}`, StatusSucceeded, ""},
		{`{"id":"t","status":"failed","error":{"message":"content rejected"}}`, StatusFailed, "content rejected"},
	} {
		result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-123"}, []byte(tc.payload))
		if err != nil || result.Status != tc.status || result.Message != tc.message {
			t.Fatalf("poll %s = %#v, %v", tc.payload, result, err)
		}
		if tc.status == StatusSucceeded && (result.Result == nil || len(result.Result.Videos) != 1) {
			t.Fatalf("successful result = %#v", result)
		}
	}
	// video_url 与 download_url 同时存在时优先 video_url。
	result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-123"}, []byte(`{"status":"completed","video_url":"https://example.com/first.mp4","download_url":"https://example.com/second.mp4"}`))
	if err != nil || result.Result == nil || len(result.Result.Videos) != 1 || result.Result.Videos[0].URL != "https://example.com/first.mp4" {
		t.Fatalf("video_url precedence = %#v, %v", result.Result, err)
	}
}
