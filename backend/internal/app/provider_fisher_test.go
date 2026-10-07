package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"infinite-canvas/backend/internal/model"
	"infinite-canvas/backend/internal/protocol"
)

func fisherTestInput() canvasGenerationInput {
	return canvasGenerationInput{Mode: "video", Prompt: "自然转身", Config: providerConfig{InterfaceType: "fisher-h3-video", Model: "MiniMax-H3", VQuality: "768P", Size: "16:9", VideoSeconds: "5"}, Metadata: map[string]interface{}{}}
}

func TestFisherCapabilityEnvelope(t *testing.T) {
	v := DefaultModelCapabilityConfig("fisher-h3-video").Video
	if v.References.MaxImages != 11 || v.References.MaxAudios != 4 || v.References.MaxVideos != 3 || v.References.PromptMaxChars != 10000 || v.Duration.Min != 4 || v.Duration.Max != 60 || strings.Join(v.Resolutions, ",") != "480P,768P,1080P" {
		t.Fatalf("Fisher envelope: %#v", v)
	}
}

func TestFisherRequestStableRolesAndOptions(t *testing.T) {
	input := fisherTestInput()
	input.ReferenceImages = []providerMedia{{ID: "ref", URL: "https://example.com/ref.jpg"}, {ID: "tail", URL: "https://example.com/tail.jpg"}}
	input.ReferenceAudios = []providerMedia{{ID: "voice", URL: "https://example.com/voice.wav"}, {ID: "drive", URL: "https://example.com/drive.wav"}}
	input.ReferenceVideos = []providerMedia{{ID: "clip", URL: "https://example.com/clip.mp4", DurationMs: 5000}}
	input.Metadata = map[string]interface{}{"videoEndFrameNodeId": "tail", "providerOptions": map[string]interface{}{"fisher-h3-video": map[string]interface{}{"driveAudioId": "drive", "videoStartTimes": map[string]interface{}{"clip": 1.0}, "audio_control": map[string]interface{}{"denoise_strength": 0.0, "add_drive_as_reference": false}}}}
	request := protocolRequestFromInput(input)
	if request.Images[0].Role != "reference_image" || request.Images[1].Role != "last_frame" || request.Audios[1].Role != "drive_audio" || request.Videos[0].Metadata["start_time_seconds"] != 1.0 {
		t.Fatalf("roles/offsets lost: %#v", request)
	}
	control, _ := request.ProviderOptions["fisher-h3-video"]["audio_control"].(map[string]interface{})
	if control["mode"] != "lock_source" || control["denoise_strength"] != 0.0 || control["add_drive_as_reference"] != false {
		t.Fatalf("audio defaults/explicit values: %#v", control)
	}
	if request.Inputs[3].Role != "reference_audio" || request.Inputs[4].Role != "drive_audio" {
		t.Fatalf("Inputs lost normalized roles: %#v", request.Inputs)
	}
	adapter, ok := loadOfficialFallbackRegistry().Resolve("fisher-h3-video")
	if !ok {
		t.Fatal("Fisher plugin missing")
	}
	spec, err := adapter.BuildCreate(context.Background(), protocol.RequestContext{Request: request})
	if err != nil {
		t.Fatal(err)
	}
	wire, _ := json.Marshal(spec.Body)
	var payload map[string]any
	if err := json.Unmarshal(wire, &payload); err != nil {
		t.Fatal(err)
	}
	actualControl, _ := payload["audio_control"].(map[string]any)
	if actualControl["mode"] != "lock_source" || actualControl["denoise_strength"] != 0.0 || actualControl["add_drive_as_reference"] != false {
		t.Fatalf("manifest lost backend options: %s", wire)
	}
	content, _ := payload["content"].([]any)
	offsetFound, driverFound := false, false
	for _, raw := range content {
		item := raw.(map[string]any)
		offsetFound = offsetFound || item["start_time_seconds"] == 1.0
		driverFound = driverFound || item["role"] == "drive_audio"
	}
	if !offsetFound || !driverFound {
		t.Fatalf("manifest lost backend media roles: %s", wire)
	}
}

type fisherCreateSpy struct {
	protocol.UnavailableAdapter
	calls int
}

func (a *fisherCreateSpy) BuildCreate(context.Context, protocol.RequestContext) (protocol.RequestSpec, error) {
	a.calls++
	return protocol.RequestSpec{}, errors.New("create reached")
}

func TestFisherInvalidInputStopsBeforeCreate(t *testing.T) {
	cases := map[string]func(*canvasGenerationInput){
		"model":             func(i *canvasGenerationInput) { i.Config.Model = "other" },
		"text":              func(i *canvasGenerationInput) { i.Prompt = " " },
		"text limit":        func(i *canvasGenerationInput) { i.Prompt = strings.Repeat("中", 10001) },
		"resolution":        func(i *canvasGenerationInput) { i.Config.VQuality = "2K" },
		"ordinary duration": func(i *canvasGenerationInput) { i.Config.VideoSeconds = "16" },
		"fractional":        func(i *canvasGenerationInput) { i.Config.VideoSeconds = "5.5" },
		"stale frame":       func(i *canvasGenerationInput) { i.Metadata["videoEndFrameNodeId"] = "gone" },
		"same frames": func(i *canvasGenerationInput) {
			i.ReferenceImages = []providerMedia{{ID: "frame", URL: "https://example.com/f.jpg"}}
			i.Metadata["videoStartFrameNodeId"] = "frame"
			i.Metadata["videoEndFrameNodeId"] = "frame"
		},
		"stale drive": func(i *canvasGenerationInput) {
			i.Metadata["providerOptions"] = map[string]interface{}{"fisher-h3-video": map[string]interface{}{"driveAudioId": "gone"}}
		},
		"adaptive reference": func(i *canvasGenerationInput) {
			i.Config.Size = "adaptive"
			i.ReferenceImages = []providerMedia{{ID: "ref", URL: "https://example.com/a.jpg"}}
		},
		"reference count": func(i *canvasGenerationInput) {
			for n := 0; n < 10; n++ {
				i.ReferenceImages = append(i.ReferenceImages, providerMedia{ID: fmt.Sprint(n), URL: "https://example.com/a.jpg"})
			}
		},
		"offset remaining": func(i *canvasGenerationInput) {
			i.ReferenceVideos = []providerMedia{{ID: "clip", URL: "https://example.com/v.mp4", DurationMs: 5000}}
			i.Metadata["providerOptions"] = map[string]interface{}{"fisher-h3-video": map[string]interface{}{"videoStartTimes": map[string]interface{}{"clip": 4.0}}}
		},
		"control needs driver": func(i *canvasGenerationInput) {
			i.Metadata["providerOptions"] = map[string]interface{}{"fisher-h3-video": map[string]interface{}{"audio_control": map[string]interface{}{"mode": "reference_only"}}}
		},
		"negative driver": func(i *canvasGenerationInput) {
			i.ReferenceAudios = []providerMedia{{ID: "drive", URL: "https://example.com/d.wav", DurationMs: -1}}
			i.Metadata["providerOptions"] = map[string]any{"fisher-h3-video": map[string]any{"driveAudioId": "drive"}}
		},
		"negative video": func(i *canvasGenerationInput) {
			i.ReferenceVideos = []providerMedia{{ID: "clip", URL: "https://example.com/v.mp4", DurationMs: -1}}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			input := fisherTestInput()
			mutate(&input)
			adapter := &fisherCreateSpy{}
			_, err := runProtocolAdapterTaskWithPolicy(context.Background(), input, adapter, videoPollPolicy{})
			if err == nil || adapter.calls != 0 {
				t.Fatalf("validation did not prevent paid create: calls=%d err=%v", adapter.calls, err)
			}
		})
	}
}

func TestFisherHeaderTaskContinuesPolling(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	for _, scenario := range []string{"http error", "body read error", "invalid json"} {
		t.Run(scenario, func(t *testing.T) {
			posts, polls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost {
					posts++
					w.Header().Set("X-Task-Id", "fisher-existing")
					switch scenario {
					case "http error":
						w.WriteHeader(503)
						fmt.Fprint(w, `{"error":{"message":"uncertain"}}`)
					case "body read error":
						w.Header().Set("Content-Length", "100")
						fmt.Fprint(w, `{`)
					default:
						fmt.Fprint(w, `broken`)
					}
					return
				}
				polls++
				if r.URL.Path != "/v2/query/video_generation/fisher-existing" {
					t.Errorf("wrong task path %s", r.URL.Path)
				}
				fmt.Fprint(w, `{"task":{"status":"failed","error":{"code":"failed","message":"terminal sentinel"}}}`)
			}))
			defer server.Close()
			input := fisherTestInput()
			input.Config.BaseURL = server.URL
			adapter, ok := loadOfficialFallbackRegistry().Resolve("fisher-h3-video")
			if !ok {
				t.Fatal("Fisher plugin missing")
			}
			_, err := runProtocolAdapterTaskWithPolicy(context.Background(), input, adapter, videoPollPolicy{})
			if posts != 1 || polls != 1 || err == nil || !strings.Contains(err.Error(), "terminal sentinel") {
				t.Fatalf("header task not recovered: posts=%d polls=%d err=%v", posts, polls, err)
			}
		})
	}
}

func TestFisherRawPayloadRejectedBeforeSystemCreate(t *testing.T) {
	// This uses the existing host runner to prove conditional bounds, independent
	// of a manifest's own optional validations.
	input := fisherTestInput()
	input.ReferenceAudios = []providerMedia{{ID: "drive", URL: "https://example.com/d.wav"}}
	input.Metadata["providerOptions"] = map[string]any{"fisher-h3-video": map[string]any{"driveAudioId": "drive", "audio_control": map[string]any{"mode": "reference_only", "add_drive_as_reference": false}}}
	adapter := &fisherCreateSpy{}
	_, err := runProtocolAdapterTaskWithPolicy(context.Background(), input, adapter, videoPollPolicy{})
	if err == nil || adapter.calls != 0 {
		t.Fatalf("invalid reference_only reached create: %v", err)
	}
}

func TestFisherMissingPluginNeverUsesLegacyCreate(t *testing.T) {
	input := fisherTestInput()
	_, err := runVideoTaskWithPolicy(withProtocolRegistry(context.Background(), emptyProtocolRegistry), input, videoPollPolicy{})
	if err == nil || !strings.Contains(err.Error(), "Fisher-H3 视频插件未安装") {
		t.Fatalf("missing plugin fell through: %v", err)
	}
}

func TestFisherNoTaskIDNeverRecreatesOrTreatsTraceAsTask(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	for _, body := range []string{`{"request_id":"trace-only"}`, `broken`} {
		t.Run(body, func(t *testing.T) {
			posts, polls := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost {
					posts++
				} else {
					polls++
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, body)
			}))
			defer server.Close()
			input := fisherTestInput()
			input.Config.BaseURL = server.URL
			adapter, ok := loadOfficialFallbackRegistry().Resolve("fisher-h3-video")
			if !ok {
				t.Fatal("Fisher plugin missing")
			}
			_, err := runProtocolAdapterTaskWithPolicy(context.Background(), input, adapter, videoPollPolicy{})
			if posts != 1 || polls != 0 || !isRouteDispatchUncertain(err) || !billingFailureUncertain(err) {
				t.Fatalf("unsafe uncertain result: posts=%d polls=%d err=%v", posts, polls, err)
			}
		})
	}
}

func TestFisherTaskIDPersistenceAndFailureClosed(t *testing.T) {
	svc, db := newTimelineTaskTestService(t)
	task := &model.Task{ID: "fisher-persist", UserID: "user", Status: model.TaskStatusRunning}
	if err := db.Create(task).Error; err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), providerAnalyticsKey{}, providerAnalyticsContext{Service: svc, TaskID: task.ID})
	submission := &fisherSubmission{}
	if err := persistFisherTaskID(ctx, submission, "fisher-existing"); err != nil {
		t.Fatal(err)
	}
	stored, err := svc.repo.Task(task.ID)
	if err != nil || stored.ProviderRequestID != "fisher-existing" || stored.PollStage != "accepted" {
		t.Fatalf("task not persisted: %#v %v", stored, err)
	}
	if err := db.Migrator().DropTable(&model.Task{}); err != nil {
		t.Fatal(err)
	}
	submission = &fisherSubmission{}
	err = persistFisherTaskID(ctx, submission, "fisher-existing")
	if err == nil || submission.PersistError == nil || !isRouteDispatchUncertain(err) {
		t.Fatalf("persistence failure hidden: %v", err)
	}
}

func TestFisherLegalMixedCapacityAndControlDefaults(t *testing.T) {
	input := fisherTestInput()
	input.Config.VideoSeconds = "60"
	for n := 0; n < 11; n++ {
		input.ReferenceImages = append(input.ReferenceImages, providerMedia{ID: fmt.Sprint(n), URL: "https://example.com/i.jpg"})
	}
	input.Metadata["videoStartFrameNodeId"] = "9"
	input.Metadata["videoEndFrameNodeId"] = "10"
	for n := 0; n < 4; n++ {
		input.ReferenceAudios = append(input.ReferenceAudios, providerMedia{ID: fmt.Sprintf("audio-%d", n), URL: "https://example.com/a.wav"})
	}
	input.Metadata["providerOptions"] = map[string]any{"fisher-h3-video": map[string]any{"driveAudioId": "audio-3"}}
	input.ReferenceVideos = []providerMedia{{ID: "unknown-duration", URL: "https://example.com/v.mp4"}}
	request := protocolRequestFromInput(input)
	if err := validateFisherH3Input(input, request); err != nil {
		t.Fatalf("legal mixed request rejected: %v", err)
	}
	control := request.ProviderOptions[fisherH3Protocol]["audio_control"].(map[string]any)
	if control["mode"] != "lock_source" || control["denoise_strength"] != 0.0 || control["add_drive_as_reference"] != true {
		t.Fatalf("driver defaults: %#v", control)
	}
	plain := protocolRequestFromInput(fisherTestInput())
	control = plain.ProviderOptions[fisherH3Protocol]["audio_control"].(map[string]any)
	if control["mode"] != "native" || control["denoise_strength"] != 0.35 || control["add_drive_as_reference"] != false {
		t.Fatalf("native defaults: %#v", control)
	}
}
