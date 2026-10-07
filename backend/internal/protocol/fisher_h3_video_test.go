package protocol

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func fisherRequest() GenerationRequest {
	return GenerationRequest{Model: "MiniMax-H3", Prompt: "A person turns naturally.", Duration: 5, Resolution: "1080P", AspectRatio: "16:9"}
}

func TestFisherH3ParameterContract(t *testing.T) {
	adapter := officialPackageAdapter(t, "fisher-h3-video.yingce-plugin", "fisher-h3-video")
	parameters := make(map[string]Parameter)
	for _, parameter := range adapter.Metadata().Parameters {
		parameters[parameter.Name] = parameter
	}
	for _, unsupported := range []string{"generateAudio", "watermark"} {
		if _, exists := parameters[unsupported]; exists {
			t.Errorf("Fisher advertises unsupported parameter %s", unsupported)
		}
	}
	for _, required := range []string{"duration", "resolution"} {
		if !parameters[required].Required {
			t.Errorf("Fisher parameter %s must be required", required)
		}
	}
}

func TestFisherH3Plugin(t *testing.T) {
	packagePath := filepath.Join("..", "..", "..", "plugin-packages")
	archive, err := os.ReadFile(filepath.Join(packagePath, "fisher-h3-video.yingce-plugin"))
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := ParsePluginPackage(archive)
	if err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(packagePath, "fisher-h3-video", "manifest.json"))
	if err != nil || !bytes.Equal(source, pkg.ManifestRaw) {
		t.Fatalf("package manifest differs from generated source: %v", err)
	}
	provider := pkg.Manifest.Contributes.Providers[0]
	workflows := pkg.Manifest.Contributes.Workflows
	if provider.Label != "Fisher-H3" || provider.BaseURL != "https://api.work-fisher.com" || !provider.RequiresPublicMediaURLs || len(workflows) != 1 || workflows[0].ProviderID != "fisher-h3-video" || workflows[0].Defaults["model"] != "MiniMax-H3" {
		t.Fatalf("Fisher identity/workflow = %#v, %#v", provider, workflows)
	}
	if !reflect.DeepEqual(workflows[0].Parameters, provider.Parameters) {
		t.Fatal("Fisher workflow parameters differ from provider contract")
	}
	adapter := officialPackageAdapter(t, "fisher-h3-video.yingce-plugin", "fisher-h3-video")
	request := fisherRequest()
	request.Images = []MediaReference{{URL: "https://example.com/end.jpg", Role: "last_frame"}}
	for i := 0; i < 9; i++ {
		request.Images = append(request.Images, MediaReference{URL: fmt.Sprintf("https://example.com/%d.jpg", i), Role: "reference_image"})
	}
	request.Videos = []MediaReference{{URL: "https://example.com/ref.mp4", Role: "reference_video", Metadata: map[string]any{"start_time_seconds": 2}}}
	request.Audios = []MediaReference{{URL: "https://example.com/dialogue.wav", Role: "drive_audio"}}
	for i := 0; i < 3; i++ {
		request.Audios = append(request.Audios, MediaReference{URL: fmt.Sprintf("https://example.com/%d.wav", i), Role: "reference_audio"})
	}
	request.Duration = 60
	request.ProviderOptions = map[string]map[string]any{"fisher-h3-video": {"audio_control": map[string]any{"mode": "remix_source", "denoise_strength": 0, "add_drive_as_reference": false}}}
	spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	if spec.Path != "/v2/video_generation" || spec.Method != "POST" || spec.Auth.Type != "bearer" {
		t.Fatalf("create transport = %#v", spec)
	}
	body := manifestTestBody(t, spec)
	if body["model"] != "MiniMax-H3" || body["resolution"] != "1080P" || body["ratio"] != "16:9" || body["duration"] != float64(60) {
		t.Fatalf("output parameters = %#v", body)
	}
	content := body["content"].([]any)
	if len(content) != 16 || content[1].(map[string]any)["role"] != "last_frame" || content[11].(map[string]any)["start_time_seconds"] != float64(2) {
		t.Fatalf("mixed content = %#v", content)
	}
	control := body["audio_control"].(map[string]any)
	if control["mode"] != "remix_source" || control["denoise_strength"] != float64(0) || control["add_drive_as_reference"] != false {
		t.Fatalf("explicit audio values = %#v", control)
	}
	for _, key := range []string{"prompt", "seed", "generate_audio", "aigc_watermark", "target", "conditions"} {
		if _, exists := body[key]; exists {
			t.Fatalf("unexpected field %s", key)
		}
	}
	polled, err := adapter.BuildPoll(context.Background(), PollContext{TaskID: "task-123"})
	if err != nil || polled.Path != "/v2/query/video_generation/task-123" || polled.Method != "GET" {
		t.Fatalf("poll = %#v, %v", polled, err)
	}
}

func TestFisherH3DefaultsAndValidation(t *testing.T) {
	adapter := officialPackageAdapter(t, "fisher-h3-video.yingce-plugin", "fisher-h3-video")
	for _, drive := range []bool{false, true} {
		request := fisherRequest()
		request.Resolution = "480p"
		if drive {
			request.Audios = []MediaReference{{URL: "https://example.com/drive.wav", Role: "drive_audio"}}
		}
		spec, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request})
		if err != nil {
			t.Fatal(err)
		}
		body := manifestTestBody(t, spec)
		control := body["audio_control"].(map[string]any)
		mode, denoise := "native", 0.35
		if drive {
			mode, denoise = "lock_source", 0
		}
		if body["resolution"] != "480P" || control["mode"] != mode || control["denoise_strength"] != denoise || control["add_drive_as_reference"] != drive {
			t.Fatalf("drive=%v defaults = %#v", drive, body)
		}
	}
	for _, tc := range []struct {
		name   string
		mutate func(*GenerationRequest)
	}{
		{"model", func(r *GenerationRequest) { r.Model = "minimax-h3" }},
		{"prompt", func(r *GenerationRequest) { r.Prompt = "  " }},
		{"resolution", func(r *GenerationRequest) { r.Resolution = "2K" }},
		{"ordinary duration", func(r *GenerationRequest) { r.Duration = 16 }},
		{"text ratio", func(r *GenerationRequest) { r.AspectRatio = "" }},
		{"reference adaptive", func(r *GenerationRequest) {
			r.Images = []MediaReference{{Role: "reference_image"}}
			r.AspectRatio = "adaptive"
		}},
		{"two tails", func(r *GenerationRequest) { r.Images = []MediaReference{{Role: "last_frame"}, {Role: "last_frame"}} }},
		{"ten reference images", func(r *GenerationRequest) {
			for i := 0; i < 10; i++ {
				r.Images = append(r.Images, MediaReference{Role: "reference_image"})
			}
		}},
		{"four references", func(r *GenerationRequest) {
			for i := 0; i < 4; i++ {
				r.Audios = append(r.Audios, MediaReference{Role: "reference_audio"})
			}
		}},
		{"two drives", func(r *GenerationRequest) { r.Audios = []MediaReference{{Role: "drive_audio"}, {Role: "drive_audio"}} }},
		{"invalid role", func(r *GenerationRequest) { r.Images = []MediaReference{{Role: "unknown"}} }},
		{"lock without drive", func(r *GenerationRequest) {
			r.ProviderOptions = map[string]map[string]any{"fisher-h3-video": {"audio_control": map[string]any{"mode": "lock_source"}}}
		}},
		{"reference false", func(r *GenerationRequest) {
			r.Audios = []MediaReference{{Role: "drive_audio"}}
			r.ProviderOptions = map[string]map[string]any{"fisher-h3-video": {"audio_control": map[string]any{"mode": "reference_only", "add_drive_as_reference": false}}}
		}},
		{"denoise", func(r *GenerationRequest) {
			r.ProviderOptions = map[string]map[string]any{"fisher-h3-video": {"audio_control": map[string]any{"denoise_strength": 2}}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			request := fisherRequest()
			tc.mutate(&request)
			if _, err := adapter.BuildCreate(context.Background(), RequestContext{Request: request}); err == nil {
				t.Fatal("invalid Fisher request accepted")
			}
		})
	}
}

func TestFisherH3Response(t *testing.T) {
	adapter := officialPackageAdapter(t, "fisher-h3-video.yingce-plugin", "fisher-h3-video")
	created, err := adapter.ParseCreate(context.Background(), []byte(`{"task_id":"task-123"}`))
	if err != nil || created.TaskID != "task-123" || created.Status != StatusPending {
		t.Fatalf("created = %#v, %v", created, err)
	}
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
