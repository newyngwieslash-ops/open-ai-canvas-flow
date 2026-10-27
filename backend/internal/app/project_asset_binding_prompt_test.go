package app

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func storyboardPromptFixture(t *testing.T, count int) (*Service, *gorm.DB, CreateTaskRequest) {
	t.Helper()
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	db := newSQLiteTestDB(t)
	// Ignore expected lookup misses, but keep real SQL errors visible, including migration errors.
	db.Logger = logger.New(log.New(os.Stdout, "", log.LstdFlags), logger.Config{SlowThreshold: time.Second, LogLevel: logger.Error, IgnoreRecordNotFoundError: true})
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	s := &Service{repo: repository.New(db), dataDir: t.TempDir(), activeCancels: make(map[string]context.CancelFunc)}
	// Windows does not support the directory fsync in key publication; seed a test-only key.
	if err := os.WriteFile(filepath.Join(s.dataDir, ".settings-key"), []byte(strings.Repeat("k", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	p, u := seedWorkflowProject(t, db)
	if _, err := s.UpdateFeatureAvailability(&model.User{ID: "admin", Role: "admin"}, FeatureAvailability{CustomChannelsEnabled: true, TaskCenterEnabled: true}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < count; i++ {
		a := model.Asset{ID: fmt.Sprintf("asset-%03d", i), UserID: "user-1", Title: "同名道具", Category: model.AssetCategoryProp, Kind: "entity"}
		if err := db.Create(&a).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Create(&model.ProjectAssetLink{ID: newID(), ProjectID: p.ID, AssetID: a.ID}).Error; err != nil {
			t.Fatal(err)
		}
	}
	return s, db, CreateTaskRequest{ProjectID: p.ID, Type: "canvas_text", Operation: "storyboard", Prompt: "生成分镜", Input: map[string]any{
		"mode": "text", "textOptions": map[string]any{"stream": false},
		"chapterAssetContext": map[string]any{"projectId": p.ID, "unitId": u.ID, "scope": map[string]any{"kind": "all", "assetIds": []string{}}},
		"config":              map[string]any{"baseUrl": "http://127.0.0.1:1/v1", "apiKey": "test-key", "model": "test", "interfaceType": "chat-completion", "capabilityConfig": map[string]any{"text": map[string]any{"contextWindowTokens": 128000, "maxOutputTokens": 16384}}},
	}}
}

func TestStoryboardAssetPromptActualRequest(t *testing.T) {
	for _, count := range []int{61, 81} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
			s, db, req := storyboardPromptFixture(t, count)
			if err := db.Model(&model.Asset{}).Where("id = ?", "asset-000").Update("title", "@[tool:text:missing:label:icon]").Error; err != nil {
				t.Fatal(err)
			}
			var wire map[string]any
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
					t.Error(err)
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
			}))
			defer server.Close()
			req.Input["config"].(map[string]any)["baseUrl"] = server.URL
			task, err := s.CreateTask("user-1", req)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := s.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			var input map[string]any
			json.Unmarshal([]byte(saved.InputJSON), &input)
			trusted := input["chapterAssetContext"].(map[string]any)
			ids, ok := trusted["allowedAssetIds"].([]any)
			if !ok || len(ids) != count {
				t.Fatalf("trusted allow set missing: %v", trusted)
			}
			summary, ok := trusted["summary"].(string)
			if !ok || strings.Contains(summary, "url") {
				t.Fatalf("invalid safe summary: %v", trusted)
			}
			if err := db.Model(&model.Asset{}).Where("id = ?", "asset-000").Update("title", "changed after admission").Error; err != nil {
				t.Fatal(err)
			}
			hydrated, err := s.decryptTaskInputJSON(saved.InputJSON)
			if err != nil {
				t.Fatal(err)
			}
			_, err = s.processCanvasGenerationTask(context.Background(), "user-1", req.ProjectID, req.Type, task.Prompt, hydrated)
			if err != nil {
				t.Fatal(err)
			}
			messages := wire["messages"].([]any)
			content := messages[len(messages)-1].(map[string]any)["content"]
			raw, _ := json.Marshal(content)
			var text string
			if value, ok := content.(string); ok {
				text = value
			} else {
				text = string(raw)
			}
			if !strings.Contains(text, summary) {
				t.Fatalf("provider did not receive persisted summary: %s", raw)
			}
			if !strings.Contains(text, "projectAssetRefs") || !strings.Contains(text, "不写入 assetBindings.nodeId") {
				t.Error("missing chapter-only structured project asset output contract")
			}
			if calls != 1 {
				t.Fatalf("model calls=%d", calls)
			}
			if wire["max_tokens"] != float64(16384) {
				t.Fatalf("custom output limit differs from budget: %v", wire["max_tokens"])
			}
		})
	}
}

func TestStoryboardAssetPromptScopeAndForgery(t *testing.T) {
	s, _, base := storyboardPromptFixture(t, 2)
	for _, tc := range []struct {
		name   string
		change func(map[string]any)
		valid  bool
	}{
		{"selected", func(c map[string]any) {
			c["scope"] = map[string]any{"kind": "selected", "assetIds": []string{"asset-001"}}
		}, true},
		{"foreign project", func(c map[string]any) { c["projectId"] = "foreign" }, false},
		{"foreign unit", func(c map[string]any) { c["unitId"] = "foreign" }, false},
		{"external asset", func(c map[string]any) {
			c["scope"] = map[string]any{"kind": "selected", "assetIds": []string{"foreign"}}
		}, false},
		{"empty selected", func(c map[string]any) { c["scope"] = map[string]any{"kind": "selected", "assetIds": []string{}} }, false},
		{"all with ids", func(c map[string]any) { c["scope"] = map[string]any{"kind": "all", "assetIds": []string{"asset-000"}} }, false},
		{"forged summary", func(c map[string]any) { c["summary"] = "evil"; c["allowedAssetIds"] = []string{"foreign"} }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := base
			req.Input, _ = normalizeTaskInput(base.Input)
			tc.change(req.Input["chapterAssetContext"].(map[string]any))
			task, err := s.CreateTask("user-1", req)
			if !tc.valid {
				if err == nil {
					t.Fatal("untrusted context accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			saved, err := s.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			var input map[string]any
			json.Unmarshal([]byte(saved.InputJSON), &input)
			c := input["chapterAssetContext"].(map[string]any)
			if c["totalCount"] != float64(2) || c["sentCount"] != float64(1) {
				t.Fatalf("selected scope lost full catalog count: %v", c)
			}
		})
	}
}

func TestStoryboardAssetPromptBudgetsAndSharedCanvas(t *testing.T) {
	s, db, req := storyboardPromptFixture(t, 1)
	// A single row is 49 ASCII bytes plus the UTF-8 name (including array brackets).
	base := `[{"assetId":"asset-000","category":"prop","name":""}]`
	remaining := 24576 - len(base)
	name := strings.Repeat("中", remaining/3) + strings.Repeat("x", remaining%3)
	if err := db.Model(&model.Asset{}).Where("id = ?", "asset-000").Update("title", name).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateTask("user-1", req); err != nil {
		t.Fatalf("exact UTF-8 limit rejected: %v", err)
	}
	db.Model(&model.Asset{}).Where("id = ?", "asset-000").Update("title", name+"x")
	if _, err := s.CreateTask("user-1", req); err == nil {
		t.Error("24577 byte summary accepted")
	}
	db.Model(&model.Asset{}).Where("id = ?", "asset-000").Update("title", "道具")
	req.Prompt = strings.Repeat("中", 5000)
	req.Input["config"].(map[string]any)["capabilityConfig"] = map[string]any{"text": map[string]any{"contextWindowTokens": 4096, "maxOutputTokens": 256}}
	if _, err := s.CreateTask("user-1", req); err == nil {
		t.Error("full prompt above model context accepted")
	}
}

func storyboardPromptCapture(t *testing.T) (*httptest.Server, *[]map[string]any) {
	t.Helper()
	wires := []map[string]any{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var wire map[string]any
		if err := json.NewDecoder(r.Body).Decode(&wire); err != nil {
			t.Error(err)
		}
		wires = append(wires, wire)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	t.Cleanup(server.Close)
	return server, &wires
}

func storyboardPromptExecute(t *testing.T, s *Service, task *model.Task) error {
	t.Helper()
	raw, err := s.decryptTaskInputJSON(task.InputJSON)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.processCanvasGenerationTask(context.Background(), task.UserID, task.ProjectID, task.Type, task.Prompt, raw)
	return err
}

func storyboardPromptNoWrites(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, table := range []any{&model.Task{}, &model.BillingOrder{}, &model.CreditLedgerEntry{}} {
		var count int64
		if err := db.Model(table).Count(&count).Error; err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Errorf("rejected request wrote %T: %d", table, count)
		}
	}
}

func TestStoryboardAssetPromptStrictBudget(t *testing.T) {
	for _, tc := range []struct {
		name           string
		window, output int
		missingOutput  bool
	}{
		{"high output ratio", 4096, 4095, false},
		{"small window reserve", 4096, 256, false},
		{"missing output", 128000, 0, true},
		{"zero output", 128000, 0, false},
		{"negative output", 128000, -1, false},
		{"below output range", 128000, 255, false},
		{"above output range", 2000000, 1000001, false},
		{"output exceeds window", 4096, 8192, false},
		{"output equals window", 128000, 128000, false},
		{"missing window", 0, 16384, false},
		{"below window range", 4095, 256, false},
		{"above window range", 10000001, 16384, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db, req := storyboardPromptFixture(t, 1)
			server, wires := storyboardPromptCapture(t)
			config := req.Input["config"].(map[string]any)
			config["baseUrl"] = server.URL
			text := map[string]any{"contextWindowTokens": tc.window, "maxOutputTokens": tc.output}
			if tc.missingOutput {
				delete(text, "maxOutputTokens")
			}
			config["capabilityConfig"] = map[string]any{"text": text}
			if _, err := s.CreateTask("user-1", req); err == nil {
				t.Error("invalid or exhausted context accepted at admission")
			}
			storyboardPromptNoWrites(t, db)
			if len(*wires) != 0 {
				t.Error("rejected admission dispatched HTTP")
			}
			// Also exercise execution independently, as persisted capability may change.
			req.Input["prompt"] = req.Prompt
			raw, err := json.Marshal(req.Input)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.processCanvasGenerationTask(context.Background(), "user-1", req.ProjectID, req.Type, req.Prompt, string(raw)); err == nil {
				t.Error("invalid or exhausted context dispatched at execution")
			}
			if len(*wires) != 0 {
				t.Error("rejected execution dispatched HTTP")
			}
		})
	}
}

func TestStoryboardAssetPromptSharedCanvasActualRequest(t *testing.T) {
	s, _, req := storyboardPromptFixture(t, 1)
	server, wires := storyboardPromptCapture(t)
	delete(req.Input, "chapterAssetContext")
	req.Prompt = "shared canvas"
	config := req.Input["config"].(map[string]any)
	config["baseUrl"], config["systemPrompt"] = server.URL, "system instruction"
	// This would fail the chapter hard gate; shared canvas must retain its existing contract.
	config["capabilityConfig"] = map[string]any{"text": map[string]any{"contextWindowTokens": 4096, "maxOutputTokens": 256}}
	req.Input["textHistory"] = []map[string]any{{"role": "user", "content": "previous question"}, {"role": "assistant", "content": "previous answer"}}
	task, err := s.CreateTask("user-1", req)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.repo.Task(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := s.decryptTaskInputJSON(saved.InputJSON)
	if err != nil {
		t.Fatal(err)
	}
	var persisted map[string]any
	if err := json.Unmarshal([]byte(raw), &persisted); err != nil {
		t.Fatal(err)
	}
	if _, exists := persisted["chapterAssetContext"]; exists {
		t.Fatal("shared persisted input gained chapter context")
	}
	if saved.Prompt != req.Prompt {
		t.Fatal("shared prompt changed")
	}
	if err := storyboardPromptExecute(t, s, saved); err != nil {
		t.Fatal(err)
	}
	if len(*wires) != 1 {
		t.Fatalf("shared calls=%d", len(*wires))
	}
	want := map[string]any{"model": "test", "messages": []any{
		map[string]any{"role": "system", "content": "system instruction"},
		map[string]any{"role": "user", "content": "previous question"},
		map[string]any{"role": "assistant", "content": "previous answer"},
		map[string]any{"role": "user", "content": "shared canvas"},
	}, "max_tokens": float64(256)}
	if !reflect.DeepEqual((*wires)[0], want) {
		t.Fatalf("shared wire changed: got %#v want %#v", (*wires)[0], want)
	}
}

func storyboardPromptChannel(t *testing.T, db *gorm.DB, baseURL, id string, window, output int) model.ChannelModel {
	t.Helper()
	profile := DefaultModelCapabilityConfigForModel("chat-completion", "text-test")
	profile.Text.ContextWindowTokens, profile.Text.MaxOutputTokens = window, output
	channel := model.ModelChannel{ID: id, Scope: model.ChannelScopeSystem, Enabled: true, Name: id, APIFormat: "openai", BaseURL: baseURL, APIKey: "test-key"}
	cm := model.ChannelModel{ID: id + "-model", ChannelID: id, ModelKey: id + "-text", Capability: "text", Protocol: model.ChannelInterfaceChatCompletion, Enabled: true, CapabilityConfigJSON: mustEncodeModelCapabilityConfig(t, profile)}
	tier := model.ChannelModelPriceTier{ID: id + "-tier", ChannelModelID: cm.ID, SelectorKey: "{}", SelectorJSON: "{}", BillingMode: "fixed_request", PriceConfigured: true, Enabled: true}
	for _, row := range []any{&channel, &cm, &tier} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return cm
}

func storyboardPromptSetCapability(t *testing.T, db *gorm.DB, cm model.ChannelModel, window, output int) {
	t.Helper()
	profile := DefaultModelCapabilityConfigForModel("chat-completion", "text-test")
	profile.Text.ContextWindowTokens, profile.Text.MaxOutputTokens = window, output
	if err := db.Model(&model.ChannelModel{}).Where("id = ?", cm.ID).Update("capability_config_json", mustEncodeModelCapabilityConfig(t, profile)).Error; err != nil {
		t.Fatal(err)
	}
}

func TestStoryboardAssetPromptSystemCapability(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(fmt.Sprintf("exhausted=%t", exhausted), func(t *testing.T) {
			s, db, req := storyboardPromptFixture(t, 1)
			server, wires := storyboardPromptCapture(t)
			cm := storyboardPromptChannel(t, db, server.URL, "system", 128000, 16384)
			req.Input["config"] = map[string]any{"channelId": cm.ChannelID, "model": cm.ModelKey, "capabilityConfig": map[string]any{"text": map[string]any{"contextWindowTokens": 1000000, "maxOutputTokens": 999999}}}
			if exhausted {
				storyboardPromptSetCapability(t, db, cm, 4096, 4095)
				if _, err := s.CreateTask("user-1", req); err == nil {
					t.Error("system exhausted budget accepted")
				}
				storyboardPromptNoWrites(t, db)
				if len(*wires) != 0 {
					t.Error("invalid system request dispatched")
				}
				return
			}
			task, err := s.CreateTask("user-1", req)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := s.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := storyboardPromptExecute(t, s, saved); err != nil {
				t.Fatal(err)
			}
			if len(*wires) != 1 || (*wires)[0]["max_tokens"] != float64(16384) {
				t.Errorf("system limit not applied: %#v", *wires)
			}
			storyboardPromptSetCapability(t, db, cm, 128000, 8192)
			if err := storyboardPromptExecute(t, s, saved); err != nil {
				t.Fatal(err)
			}
			if len(*wires) != 2 || (*wires)[1]["max_tokens"] != float64(8192) {
				t.Errorf("updated system limit not applied: %#v", *wires)
			}
			storyboardPromptSetCapability(t, db, cm, 4096, 4095)
			if err := storyboardPromptExecute(t, s, saved); err == nil {
				t.Error("changed system budget not rechecked")
			}
			if len(*wires) != 2 {
				t.Error("exhausted system budget reached upstream")
			}
		})
	}
}

func TestStoryboardAssetPromptLogicalReroute(t *testing.T) {
	for _, exhausted := range []bool{false, true} {
		t.Run(fmt.Sprintf("exhausted=%t", exhausted), func(t *testing.T) {
			s, db, req := storyboardPromptFixture(t, 1)
			server, wires := storyboardPromptCapture(t)
			first := storyboardPromptChannel(t, db, server.URL, "first", 128000, 16384)
			second := storyboardPromptChannel(t, db, server.URL, "second", 64000, 8192)
			profile := DefaultModelCapabilityConfigForModel("chat-completion", "text-test")
			spec, err := CapabilitySpecFromModelCapabilityConfig(profile, "text")
			if err != nil {
				t.Fatal(err)
			}
			specJSON, err := json.Marshal(spec)
			if err != nil {
				t.Fatal(err)
			}
			logical := model.LogicalModel{ID: "logical", Code: "logical-text", Capability: "text", Enabled: true, ActiveRevisionID: "revision", PricePolicy: "unified", BillingMode: "fixed_request"}
			revision := model.LogicalModelRevision{ID: logical.ActiveRevisionID, LogicalModelID: logical.ID, Version: 1, CapabilitySpecJSON: string(specJSON), DefaultOptionsJSON: "{}"}
			route1 := model.LogicalModelRoute{ID: "route1", LogicalModelRevisionID: revision.ID, ChannelModelID: first.ID, Enabled: true, Weight: 1}
			route2 := model.LogicalModelRoute{ID: "route2", LogicalModelRevisionID: revision.ID, ChannelModelID: second.ID, Enabled: false, Weight: 1}
			for _, row := range []any{&logical, &revision, &route1, &route2} {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.UpdateFeatureAvailability(&model.User{ID: "admin", Role: "admin"}, FeatureAvailability{FrontendModelsEnabled: true, TaskCenterEnabled: true}); err != nil {
				t.Fatal(err)
			}
			req.LogicalModelID = logical.ID
			req.Input["config"] = map[string]any{}
			task, err := s.CreateTask("user-1", req)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := s.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.RouteID != route1.ID {
				t.Fatalf("initial route=%s", saved.RouteID)
			}
			if err := storyboardPromptExecute(t, s, saved); err != nil {
				t.Fatal(err)
			}
			if len(*wires) != 1 || (*wires)[0]["max_tokens"] != float64(16384) {
				t.Errorf("managed output limit missing: %#v", *wires)
			}
			if err := db.Model(&model.LogicalModelRoute{}).Where("id = ?", route2.ID).Update("enabled", true).Error; err != nil {
				t.Fatal(err)
			}
			if exhausted {
				storyboardPromptSetCapability(t, db, second, 4096, 4095)
			}
			if err := db.Model(&model.Task{}).Where("id = ?", saved.ID).Update("status", model.TaskStatusRunning).Error; err != nil {
				t.Fatal(err)
			}
			saved.Status = model.TaskStatusRunning
			before := saved.InputJSON
			_, err = s.switchTaskToNextRoute(saved, []model.RouteAttempt{{RouteID: route1.ID}})
			if exhausted {
				if err == nil {
					t.Error("exhausted reroute accepted before persistence")
				}
				reloaded, readErr := s.repo.Task(task.ID)
				if readErr != nil {
					t.Fatal(readErr)
				}
				if reloaded.InputJSON != before || reloaded.RouteID != route1.ID {
					t.Error("rejected reroute changed persisted selection")
				}
				if len(*wires) != 1 {
					t.Error("exhausted reroute dispatched")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			saved, err = s.repo.Task(task.ID)
			if err != nil {
				t.Fatal(err)
			}
			if saved.RouteID != route2.ID {
				t.Fatalf("reroute=%s", saved.RouteID)
			}
			if err := storyboardPromptExecute(t, s, saved); err != nil {
				t.Fatal(err)
			}
			if len(*wires) != 2 || (*wires)[1]["max_tokens"] != float64(8192) || (*wires)[1]["model"] != second.ModelKey {
				t.Errorf("reroute capability not applied: %#v", *wires)
			}
		})
	}
}
