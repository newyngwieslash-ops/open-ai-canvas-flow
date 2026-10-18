package protocol

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func yuxiboxRequest() GenerationRequest {
	return GenerationRequest{Model: "seedance2.5-yx-count", Prompt: "少女在雨巷中转身回望。", Duration: 30, AspectRatio: "16:9"}
}

func TestYuxiboxSeedancePackage(t *testing.T) {
	packagePath := filepath.Join("..", "..", "..", "plugin-packages")
	archive, err := os.ReadFile(filepath.Join(packagePath, "yuxibox-seedance-video.yingce-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ParsePluginPackage(archive)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(packagePath, "yuxibox-seedance-video", "manifest.json"))
	if err != nil || !bytes.Equal(source, pkg.ManifestRaw) {
		t.Fatalf("package manifest differs from generated source: %v", err)
	}
	provider := pkg.Manifest.Contributes.Providers[0]
	workflows := pkg.Manifest.Contributes.Workflows
	if provider.Label != "yuxibox Seedance" || provider.BaseURL != "https://yuxibox.cn" || !provider.RequiresPublicMediaURLs || provider.Auth.Type != "bearer" || len(workflows) != 1 || workflows[0].ProviderID != "yuxibox-seedance-video" {
		t.Fatalf("yuxibox identity/workflow = %#v, %#v", provider, workflows)
	}
	if workflows[0].Defaults["model"] != "seedance2.5-yx-count" || workflows[0].Defaults["duration"] != float64(30) || workflows[0].Defaults["aspectRatio"] != "16:9" {
		t.Fatalf("yuxibox workflow defaults = %#v", workflows[0].Defaults)
	}
	if !reflect.DeepEqual(workflows[0].Parameters, provider.Parameters) {
		t.Fatal("yuxibox workflow parameters differ from provider contract")
	}
	parameterNames := make(map[string]bool)
	for _, parameter := range provider.Parameters {
		parameterNames[parameter.Name] = true
	}
	for _, unsupported := range []string{"resolution", "generateAudio", "watermark"} {
		if parameterNames[unsupported] {
			t.Errorf("yuxibox advertises unsupported parameter %s", unsupported)
		}
	}
	if !parameterNames["duration"] {
		t.Error("yuxibox duration parameter missing")
	}
}

func TestYuxiboxSeedanceCreateAssembly(t *testing.T) {
	adapter := officialPackageAdapter(t, "yuxibox-seedance-video.yingce-plugin", "yuxibox-seedance-video")
	// 文生视频：seconds 字符串化、ratio 缺省 16:9、空素材组不出现字段键。
	textOnly := yuxiboxRequest()
	textOnly.AspectRatio = ""
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: textOnly})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != "/v1/videos" || spec.Method != "POST" {
		t.Fatalf("create transport = %#v", spec)
	}
	body := manifestTestBody(t, spec)
	if body["model"] != "seedance2.5-yx-count" || body["prompt"] != "少女在雨巷中转身回望。" || body["seconds"] != "30" || body["ratio"] != "16:9" {
		t.Fatalf("text-only parameters = %#v", body)
	}
	for _, key := range []string{"reference_images", "videos", "audios", "size", "resolution", "input_reference"} {
		if _, exists := body[key]; exists {
			t.Fatalf("unexpected field %s", key)
		}
	}
	// 多模态：三组素材按序映射 URL。
	request := yuxiboxRequest()
	request.AspectRatio = "adaptive"
	request.Images = []MediaReference{{URL: "https://example.com/ref-1.jpg"}, {URL: "https://example.com/ref-2.jpg"}}
	request.Videos = []MediaReference{{URL: "https://example.com/ref.mp4"}}
	request.Audios = []MediaReference{{URL: "https://example.com/dialogue.wav"}}
	spec, err = adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	body = manifestTestBody(t, spec)
	if body["ratio"] != "adaptive" || body["seconds"] != "30" {
		t.Fatalf("multimodal parameters = %#v", body)
	}
	images := body["reference_images"].([]any)
	if len(images) != 2 || images[0] != "https://example.com/ref-1.jpg" || images[1] != "https://example.com/ref-2.jpg" {
		t.Fatalf("reference_images = %#v", images)
	}
	if body["videos"].([]any)[0] != "https://example.com/ref.mp4" || body["audios"].([]any)[0] != "https://example.com/dialogue.wav" {
		t.Fatalf("videos/audios = %#v, %#v", body["videos"], body["audios"])
	}
}

func TestYuxiboxSeedanceValidation(t *testing.T) {
	adapter := officialPackageAdapter(t, "yuxibox-seedance-video.yingce-plugin", "yuxibox-seedance-video")
	for _, tc := range []struct {
		name   string
		mutate func(*GenerationRequest)
	}{
		{"model renamed", func(r *GenerationRequest) { r.Model = "seedance2.5" }},
		{"prompt empty", func(r *GenerationRequest) { r.Prompt = "  " }},
		{"prompt too long", func(r *GenerationRequest) { r.Prompt = string(make([]byte, 16001)) }},
		{"duration low", func(r *GenerationRequest) { r.Duration = 3 }},
		{"duration high", func(r *GenerationRequest) { r.Duration = 31 }},
		{"ratio not in tier", func(r *GenerationRequest) { r.AspectRatio = "2:3" }},
		{"thirty one images", func(r *GenerationRequest) {
			for i := 0; i < 31; i++ {
				r.Images = append(r.Images, MediaReference{URL: "https://example.com/x.jpg"})
			}
		}},
		{"eleven videos", func(r *GenerationRequest) {
			for i := 0; i < 11; i++ {
				r.Videos = append(r.Videos, MediaReference{URL: "https://example.com/x.mp4"})
			}
		}},
		{"eleven audios", func(r *GenerationRequest) {
			for i := 0; i < 11; i++ {
				r.Audios = append(r.Audios, MediaReference{URL: "https://example.com/x.wav"})
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := yuxiboxRequest()
			tc.mutate(&request)
			if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
				t.Fatal("invalid yuxibox request accepted")
			}
		})
	}
}

func TestYuxiboxSeedancePollAndResponse(t *testing.T) {
	adapter := officialPackageAdapter(t, "yuxibox-seedance-video.yingce-plugin", "yuxibox-seedance-video")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"id":"t1","status":"queued"}`))
	if err != nil || created.TaskID != "t1" || created.Status != StatusPending {
		t.Fatalf("created = %#v, %v", created, err)
	}
	// 追踪 request_id 不得成为任务编号（metaso 测试同款断言）。
	// 缺任务 ID 的“结果不确定”由声明式宿主 provider_protocol.go 抛出
	//（extractProviderTaskID 兜底后仍为空 → 报“声明式协议创建请求没有返回任务 ID”），
	// 插件层 ParseCreate 只保证不把无关编号当任务 ID。
	tracked, err := adapter.ParseCreate(context.Background(), []byte(`{"request_id":"trace-123"}`))
	if err != nil || tracked.TaskID != "" {
		t.Fatalf("tracking ID must not become task ID: %#v, %v", tracked, err)
	}
	polled, err := adapter.BuildPoll(context.Background(), PollContext{TaskID: "task-123"})
	if err != nil || polled.Path != "/v1/videos/task-123" || polled.Method != "GET" {
		t.Fatalf("poll = %#v, %v", polled, err)
	}
	// 内容兜底端点：URL 全缺失时宿主经 ResultAdapter 下载 MP4 二进制（Bearer 由宿主注入）。
	resultCapability, ok := adapter.(ResultCapability)
	if !ok || !resultCapability.ResultAvailable() {
		t.Fatal("yuxibox result operation is unavailable")
	}
	resultAdapter, ok := adapter.(ResultAdapter)
	if !ok {
		t.Fatal("yuxibox does not implement ResultAdapter")
	}
	download, err := resultAdapter.BuildResult(context.Background(), PollContext{TaskID: "task-123"})
	if err != nil || download.Path != "/v1/videos/task-123/content" || download.Method != "GET" || download.Headers["Accept"] != "video/mp4" || download.Auth.Type != "bearer" {
		t.Fatalf("result = %#v, %v", download, err)
	}
	// 状态归一与多字段 URL coalesce。
	for _, tc := range []struct {
		payload string
		status  Status
		message string
	}{
		{`{"status":"queued"}`, StatusPending, ""},
		{`{"status":"in_progress","progress":50}`, StatusProcessing, ""},
		{`{"status":"failed","fail_reason":"rejected"}`, StatusFailed, "rejected"},
		{`{"error":{"code":"401","message":"invalid api key"}}`, StatusFailed, "invalid api key"},
		{`{"status":"cancelled"}`, StatusCancelled, ""},
		{`{"status":"completed","url":"https://example.com/result.mp4"}`, StatusSucceeded, ""},
		{`{"status":"completed","video_url":"https://example.com/alt.mp4"}`, StatusSucceeded, ""},
		{`{"status":"completed","download_url":"https://example.com/only-download.mp4"}`, StatusSucceeded, ""},
		{`{"status":"completed","output":{"url":"https://example.com/output.mp4"}}`, StatusSucceeded, ""},
	} {
		result, err := adapter.ParsePoll(context.Background(), PollContext{TaskID: "task-123"}, []byte(tc.payload))
		if err != nil || result.Status != tc.status || result.TaskID != "task-123" || result.Message != tc.message {
			t.Fatalf("poll %s = %#v, %v", tc.payload, result, err)
		}
		if tc.status == StatusSucceeded && (result.Result == nil || len(result.Result.Videos) != 1) {
			t.Fatalf("successful result = %#v", result)
		}
	}
}
