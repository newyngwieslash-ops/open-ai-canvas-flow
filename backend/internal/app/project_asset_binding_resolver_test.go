package app

import (
	"encoding/json"
	"reflect"
	"testing"
	"yingce/backend/internal/model"
)

func storyboardResolverCatalog() storyboardAssetCatalog {
	return storyboardAssetCatalog{ProjectID: "p", Assets: []storyboardAssetSnapshot{
		{ProjectID: "p", Asset: model.Asset{ID: "a", Title: "桑若", Category: "character", Status: "confirmed", PrimaryVersionID: "va"}, Versions: []model.AssetVersion{{ID: "va", AssetID: "a", Status: "confirmed"}}, ImageVersionIDs: map[string]bool{"va": true}},
		{ProjectID: "p", Asset: model.Asset{ID: "b", Title: "裴让", Category: "character", Status: "draft", PrimaryVersionID: "vb"}, Versions: []model.AssetVersion{{ID: "vb", AssetID: "b", Status: "draft"}}},
	}}
}

func storyboardHasIssue(c BindingCandidate, kind string) bool {
	for _, issue := range c.Issues {
		if issue.Kind == kind {
			return true
		}
	}
	return false
}

func TestStoryboardAssetResolverIdentity(t *testing.T) {
	tests := []struct {
		name                         string
		ref                          storyboardAssetReference
		mutate                       func(*storyboardAssetCatalog)
		allowed                      map[string]bool
		id, version, issue, evidence string
		selected, missing            bool
	}{
		{name: "valid ID", ref: storyboardAssetReference{AssetID: "a", Category: "character", Name: "wrong"}, id: "a", version: "va", evidence: "structured_id", selected: true},
		{name: "invalid ID no fallback", ref: storyboardAssetReference{AssetID: "unknown", Category: "character", Name: "桑若"}, issue: "invalid_id"},
		{name: "whitespace ID no fallback", ref: storyboardAssetReference{AssetID: " ", Category: "character", Name: "桑若"}, issue: "invalid_id"},
		{name: "cross project", ref: storyboardAssetReference{AssetID: "a", Category: "character"}, mutate: func(c *storyboardAssetCatalog) { c.Assets[0].ProjectID = "other" }, issue: "invalid_id"},
		{name: "category mismatch", ref: storyboardAssetReference{AssetID: "a", Category: "prop"}, issue: "category_mismatch"},
		{name: "unsupported category", ref: storyboardAssetReference{AssetID: "a", Category: "voice"}, issue: "category_mismatch"},
		{name: "not allowed ID", ref: storyboardAssetReference{AssetID: "a", Category: "character"}, allowed: map[string]bool{}, issue: "not_allowed"},
		{name: "not allowed name", ref: storyboardAssetReference{Name: "桑若", Category: "character"}, allowed: map[string]bool{}, issue: "not_allowed"},
		{name: "trim only", ref: storyboardAssetReference{Name: "  桑若\n", Category: "character"}, id: "a", version: "va", evidence: "exact_name"},
		{name: "not substring", ref: storyboardAssetReference{Name: "桑", Category: "character"}, issue: "unmatched"},
		{name: "no inner whitespace normalization", ref: storyboardAssetReference{Name: "桑 若", Category: "character"}, issue: "unmatched"},
		{name: "full catalog ambiguity", ref: storyboardAssetReference{Name: "桑若", Category: "character"}, allowed: map[string]bool{"a": true}, mutate: func(c *storyboardAssetCatalog) { c.Assets[1].Asset.Title = " 桑若 " }, issue: "ambiguous"},
		{name: "other category not duplicate", ref: storyboardAssetReference{Name: "桑若", Category: "character"}, mutate: func(c *storyboardAssetCatalog) {
			c.Assets[1].Asset.Title = "桑若"
			c.Assets[1].Asset.Category = "prop"
		}, id: "a", version: "va", evidence: "exact_name"},
		{name: "missing version", ref: storyboardAssetReference{AssetID: "a", Category: "character"}, mutate: func(c *storyboardAssetCatalog) { c.Assets[0].Versions = nil }, id: "a", issue: "missing_version", evidence: "structured_id"},
		{name: "foreign version", ref: storyboardAssetReference{AssetID: "a", Category: "character"}, mutate: func(c *storyboardAssetCatalog) { c.Assets[0].Versions[0].AssetID = "b" }, id: "a", issue: "missing_version", evidence: "structured_id"},
		{name: "archived version", ref: storyboardAssetReference{AssetID: "a", Category: "character"}, mutate: func(c *storyboardAssetCatalog) { c.Assets[0].Versions[0].Status = "archived" }, id: "a", issue: "missing_version", evidence: "structured_id"},
		{name: "archived asset", ref: storyboardAssetReference{AssetID: "a", Category: "character"}, mutate: func(c *storyboardAssetCatalog) { c.Assets[0].Asset.Status = "archived" }, id: "a", issue: "missing_version", evidence: "structured_id"},
		{name: "missing image bindable", ref: storyboardAssetReference{AssetID: "b", Category: "character"}, id: "b", version: "vb", issue: "missing_image", evidence: "structured_id", selected: true, missing: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := storyboardResolverCatalog()
			if tt.mutate != nil {
				tt.mutate(&catalog)
			}
			before, _ := json.Marshal(catalog)
			got := resolveStoryboardAssetReference(tt.ref, catalog, tt.allowed)
			if got.AssetID != tt.id || got.Bindable != (tt.version != "") || got.DefaultSelected != tt.selected || got.MissingImage != tt.missing {
				t.Fatalf("identity/flags: %+v", got)
			}
			if tt.version == "" {
				if got.AssetVersionID != nil {
					t.Fatalf("unexpected version: %s", *got.AssetVersionID)
				}
			} else if got.AssetVersionID == nil || *got.AssetVersionID != tt.version {
				t.Fatalf("version: %+v", got)
			}
			if tt.issue != "" && !storyboardHasIssue(got, tt.issue) {
				t.Fatalf("missing issue %s: %+v", tt.issue, got)
			}
			if tt.issue == "" && len(got.Issues) != 0 {
				t.Fatalf("unexpected issues: %+v", got)
			}
			if tt.evidence != "" && (len(got.Evidence) != 1 || got.Evidence[0].Kind != tt.evidence || got.Evidence[0].Start != 0 || got.Evidence[0].End != 0) {
				t.Fatalf("evidence: %+v", got.Evidence)
			}
			after, _ := json.Marshal(catalog)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("mutated catalog")
			}
		})
	}
}

func TestStoryboardAssetResolverDTO(t *testing.T) {
	encoded, err := json.Marshal(BindingCandidate{Category: "prop", Evidence: []BindingEvidence{}, Issues: []BindingIssue{}})
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err = json.Unmarshal(encoded, &got); err != nil {
		t.Fatal(err)
	}
	if value, ok := got["assetVersionId"]; !ok || value != nil {
		t.Fatalf("nullable version: %s", encoded)
	}
	for _, key := range []string{"assetId", "category", "name", "evidence", "issues", "bindable", "missingImage", "defaultSelected"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("missing %s: %s", key, encoded)
		}
	}
}

func TestStoryboardAssetResolverEvidence(t *testing.T) {
	tests := []struct {
		name, description string
		revision          model.ShotRevision
		mutate            func(*storyboardAssetCatalog)
		kinds             []string
		selected          bool
		issue             string
	}{
		{name: "Chinese mentions not inferred props", description: "桑若视线落在领口……裴让停止喝粥……", kinds: []string{"mention", "mention"}},
		{name: "speaker is not visual", revision: model.ShotRevision{Dialogue: "桑若：裴让来了"}, kinds: []string{"speaker", "mention"}},
		{name: "Unicode offsets", description: "🌙桑若抬头", kinds: []string{"mention"}},
		{name: "negative prompt ignored", revision: model.ShotRevision{NegativePrompt: "桑若 裴让"}},
		{name: "no previous scene inheritance"},
		{name: "negation", description: "桑若没有出场", kinds: []string{"mention"}, issue: "context_warning"},
		{name: "memory", description: "回忆中的桑若", kinds: []string{"mention"}, issue: "context_warning"},
		{name: "offscreen", revision: model.ShotRevision{Dialogue: "画外音：桑若来了"}, kinds: []string{"mention"}, issue: "context_warning"},
		{name: "overlap names", description: "桑若抬头", mutate: func(c *storyboardAssetCatalog) { c.Assets[1].Asset.Title = "桑" }, kinds: []string{"mention", "mention"}, issue: "ambiguous"},
		{name: "duplicate names", description: "桑若抬头", mutate: func(c *storyboardAssetCatalog) { c.Assets[1].Asset.Title = "桑若" }, kinds: []string{"mention", "mention"}, issue: "ambiguous"},
		{name: "explicit character label", description: "角色：桑若", kinds: []string{"visual_label"}, selected: true},
		{name: "label embedded in dialogue not visual", revision: model.ShotRevision{Dialogue: "角色：桑若"}, kinds: []string{"mention"}},
		{name: "label with narrative not exact", description: "角色：桑若没有出场", kinds: []string{"mention"}, issue: "context_warning"},
		{name: "plot", revision: model.ShotRevision{PlotDescription: "桑若"}, kinds: []string{"mention"}},
		{name: "action", revision: model.ShotRevision{Action: "桑若"}, kinds: []string{"mention"}},
		{name: "image", revision: model.ShotRevision{ImagePrompt: "桑若"}, kinds: []string{"mention"}},
		{name: "video", revision: model.ShotRevision{VideoPrompt: "桑若"}, kinds: []string{"mention"}},
		{name: "free continuity not structured", revision: model.ShotRevision{ContinuityNotes: "延续桑若"}, kinds: []string{"mention"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := storyboardResolverCatalog()
			if tt.mutate != nil {
				tt.mutate(&catalog)
			}
			got := extractStoryboardAssetEvidence(tt.description, tt.revision, catalog, nil)
			if len(got) != len(tt.kinds) {
				t.Fatalf("candidates got %d want %d: %+v", len(got), len(tt.kinds), got)
			}
			fields := map[string]string{"description": tt.description, "plotDescription": tt.revision.PlotDescription, "action": tt.revision.Action, "dialogue": tt.revision.Dialogue, "imagePrompt": tt.revision.ImagePrompt, "videoPrompt": tt.revision.VideoPrompt, "continuityNotes": tt.revision.ContinuityNotes}
			for i, c := range got {
				want := catalog.Assets[i]
				if c.AssetID != want.Asset.ID || c.AssetVersionID == nil || *c.AssetVersionID != want.Asset.PrimaryVersionID || c.Bindable != (tt.issue != "ambiguous") {
					t.Fatalf("candidate %d identity/version/bindable: %+v", i, c)
				}
				if len(c.Evidence) != 1 || c.Evidence[0].Kind != tt.kinds[i] || c.DefaultSelected != tt.selected {
					t.Fatalf("candidate %d: %+v", i, c)
				}
				e := c.Evidence[0]
				runes := []rune(fields[e.Field])
				if e.Start < 0 || e.End > len(runes) || e.End <= e.Start || string(runes[e.Start:e.End]) != e.Text {
					t.Fatalf("invalid rune evidence: %+v", e)
				}
				if tt.name == "Unicode offsets" && (e.Start != 1 || e.End != 3) {
					t.Fatalf("not code-point offsets: %+v", e)
				}
				if tt.issue != "" && !storyboardHasIssue(c, tt.issue) {
					t.Fatalf("missing %s: %+v", tt.issue, c)
				}
			}
		})
	}
}

func TestStoryboardAssetResolverStructuredEvidence(t *testing.T) {
	for _, tt := range []struct {
		name, kind, source string
		ref                storyboardAssetReference
		selected           bool
		issue              string
	}{
		{name: "structured ID", kind: "structured_id", ref: storyboardAssetReference{AssetID: "a", Category: "character"}, selected: true},
		{name: "structured visual label", kind: "visual_label", ref: storyboardAssetReference{Name: "桑若", Category: "character"}, selected: true},
		{name: "traceable continuity", kind: "structured_continuity", source: "previous-shot", ref: storyboardAssetReference{AssetID: "a", Category: "character"}, selected: true},
		{name: "untraceable continuity", kind: "structured_continuity", ref: storyboardAssetReference{AssetID: "a", Category: "character"}, issue: "context_warning"},
		{name: "bad structured ID no fallback", kind: "visual_label", ref: storyboardAssetReference{AssetID: "bad", Name: "桑若", Category: "character"}, issue: "invalid_id"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStoryboardAssetEvidence("", model.ShotRevision{}, storyboardResolverCatalog(), []storyboardStructuredEvidence{{Reference: tt.ref, Kind: tt.kind, Field: "assetBindings[0]", SourceShotID: tt.source}})
			if len(got) != 1 {
				t.Fatalf("got %+v", got)
			}
			c := got[0]
			if tt.issue == "invalid_id" {
				if c.AssetID != "" || c.AssetVersionID != nil || c.Bindable {
					t.Fatalf("invalid identity must not bind: %+v", c)
				}
			} else if c.AssetID != "a" || c.AssetVersionID == nil || *c.AssetVersionID != "va" || !c.Bindable {
				t.Fatalf("structured identity/version/bindable: %+v", c)
			}
			if c.DefaultSelected != tt.selected {
				t.Fatalf("selected: %+v", c)
			}
			if tt.issue != "" && !storyboardHasIssue(c, tt.issue) {
				t.Fatalf("missing %s: %+v", tt.issue, c)
			}
			if len(c.Evidence) != 1 || c.Evidence[0].Field != "assetBindings[0]" || c.Evidence[0].Start != 0 || c.Evidence[0].End != 0 || c.Evidence[0].Text != "" {
				t.Fatalf("fabricated evidence: %+v", c)
			}
		})
	}
}

func TestStoryboardAssetResolverLabelIdentity(t *testing.T) {
	for _, tt := range []struct {
		name, text, otherName string
		otherCategory         model.AssetCategory
		ids                   []string
		kinds                 []string
		ambiguous, selected   bool
	}{
		{name: "character label excludes same-name prop", text: "角色：桑若", otherName: "桑若", otherCategory: "prop", ids: []string{"a"}, kinds: []string{"visual_label"}, selected: true},
		{name: "prop label excludes same-name character", text: "道具:桑若", otherName: "桑若", otherCategory: "prop", ids: []string{"b"}, kinds: []string{"visual_label"}, selected: true},
		{name: "environment label excludes same-name character", text: "场景：桑若", otherName: "桑若", otherCategory: "environment", ids: []string{"b"}, kinds: []string{"visual_label"}, selected: true},
		{name: "unqualified cross-category name remains ambiguous", text: "桑若抬头", otherName: "桑若", otherCategory: "prop", ids: []string{"a", "b"}, kinds: []string{"mention", "mention"}, ambiguous: true},
		{name: "label retains same-category duplicates", text: "角色：桑若", otherName: "桑若", otherCategory: "character", ids: []string{"a", "b"}, kinds: []string{"visual_label", "visual_label"}, ambiguous: true},
		{name: "label retains same-category overlaps", text: "角色：桑若", otherName: "桑", otherCategory: "character", ids: []string{"a", "b"}, kinds: []string{"visual_label", "mention"}, ambiguous: true},
		{name: "label excludes cross-category overlaps", text: "角色：桑若", otherName: "桑", otherCategory: "prop", ids: []string{"a"}, kinds: []string{"visual_label"}, selected: true},
		{name: "dialogue does not gain category scope", text: "", otherName: "桑若", otherCategory: "prop", ids: []string{"a", "b"}, kinds: []string{"mention", "mention"}, ambiguous: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			catalog := storyboardResolverCatalog()
			catalog.Assets[1].Asset.Title = tt.otherName
			catalog.Assets[1].Asset.Category = tt.otherCategory
			revision := model.ShotRevision{}
			if tt.name == "dialogue does not gain category scope" {
				revision.Dialogue = "角色：桑若"
			}
			got := extractStoryboardAssetEvidence(tt.text, revision, catalog, nil)
			if len(got) != len(tt.ids) {
				t.Fatalf("candidates got %d want %d: %+v", len(got), len(tt.ids), got)
			}
			for i, c := range got {
				if c.AssetID != tt.ids[i] || c.AssetVersionID == nil || *c.AssetVersionID != "v"+tt.ids[i] || c.Bindable == tt.ambiguous || c.DefaultSelected != tt.selected {
					t.Errorf("candidate %d identity/version/flags: %+v", i, c)
				}
				if storyboardHasIssue(c, "ambiguous") != tt.ambiguous || len(c.Evidence) != 1 || c.Evidence[0].Kind != tt.kinds[i] {
					t.Errorf("candidate %d ambiguity/evidence: %+v", i, c)
				}
				wantCategory := BindingCategory("character")
				if tt.ids[i] == "b" {
					wantCategory = BindingCategory(tt.otherCategory)
				}
				if c.Category != wantCategory {
					t.Errorf("candidate %d category: %+v", i, c)
				}
			}
		})
	}
}

func TestStoryboardAssetResolverContextHeaders(t *testing.T) {
	for _, tt := range []struct {
		name, description string
		revision          model.ShotRevision
		kind              string
		warning, selected bool
	}{
		{name: "negative header before label", description: "以下角色不出场：\n角色：桑若", kind: "visual_label", warning: true},
		{name: "offscreen header before label", description: "画外音：\n角色：桑若", kind: "visual_label", warning: true},
		{name: "negative header and split label", description: "以下角色不出场：\n角色：\n桑若", kind: "mention", warning: true},
		{name: "offscreen header and split label", description: "画外音：\n角色：\n桑若", kind: "mention", warning: true},
		{name: "header scope unclear after blank line", description: "以下角色不出场：\n\n角色：桑若", kind: "visual_label", warning: true},
		{name: "header scope unclear after intervening line", description: "画外音:\r\n声音说明\r\n角色：桑若", kind: "visual_label", warning: true},
		{name: "positive header does not warn", description: "以下角色出场：\n角色：桑若", kind: "visual_label", selected: true},
		{name: "later header does not warn earlier label", description: "角色：桑若\n以下角色不出场：", kind: "visual_label", selected: true},
		{name: "header scope stays in field", description: "画外音：", revision: model.ShotRevision{ImagePrompt: "角色：桑若"}, kind: "visual_label", selected: true},
		{name: "prompt header before label", revision: model.ShotRevision{ImagePrompt: "以下角色不出场：\n角色：桑若"}, kind: "visual_label", warning: true},
		{name: "dialogue header before speaker", revision: model.ShotRevision{Dialogue: "画外音：\n桑若：走吧"}, kind: "speaker", warning: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got := extractStoryboardAssetEvidence(tt.description, tt.revision, storyboardResolverCatalog(), nil)
			if len(got) != 1 {
				t.Fatalf("candidates: %+v", got)
			}
			c := got[0]
			if c.AssetID != "a" || c.AssetVersionID == nil || *c.AssetVersionID != "va" || !c.Bindable || c.DefaultSelected != tt.selected {
				t.Errorf("identity/version/flags: %+v", c)
			}
			if storyboardHasIssue(c, "context_warning") != tt.warning || len(c.Evidence) != 1 || c.Evidence[0].Kind != tt.kind {
				t.Errorf("warning/evidence: %+v", c)
			}
			if len(c.Evidence) == 1 {
				e := c.Evidence[0]
				field := map[string]string{"description": tt.description, "imagePrompt": tt.revision.ImagePrompt, "dialogue": tt.revision.Dialogue}[e.Field]
				runes := []rune(field)
				if e.Start < 0 || e.End > len(runes) || e.End <= e.Start || string(runes[e.Start:e.End]) != "桑若" || e.Text != "桑若" {
					t.Errorf("invalid rune evidence: %+v", e)
				}
			}
		})
	}
}

func TestStoryboardAssetResolverCapacity(t *testing.T) {
	catalog := storyboardResolverCatalog()
	candidates := []BindingCandidate{resolveStoryboardAssetReference(storyboardAssetReference{AssetID: "a", Category: "character"}, catalog, nil), resolveStoryboardAssetReference(storyboardAssetReference{AssetID: "b", Category: "character"}, catalog, nil)}
	for _, tt := range []struct {
		name                       string
		refs                       []model.ShotAssetReference
		total, existing, remaining int
		over                       bool
	}{
		{name: "five plus two", refs: []model.ShotAssetReference{{ID: "1", AssetVersionID: "v1"}, {ID: "2", AssetVersionID: "v2"}, {ID: "3", AssetVersionID: "v3"}, {ID: "4", AssetVersionID: "v4"}, {ID: "5", AssetVersionID: "v5"}}, total: 7, existing: 5, remaining: 1, over: true},
		{name: "same version two roles", refs: []model.ShotAssetReference{{ID: "r1", AssetVersionID: "va", Role: "reference", Status: "linked"}, {ID: "r2", AssetVersionID: "va", Role: "character", Status: "manual"}}, total: 2, existing: 1, remaining: 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			before, _ := json.Marshal(candidates)
			got := applyStoryboardAssetCapacity(tt.refs, candidates)
			if got.ExistingVersionCount != tt.existing || got.RemainingCapacity != tt.remaining || got.TotalVersionCount != tt.total {
				t.Fatalf("counts: %+v", got)
			}
			if !reflect.DeepEqual(got.ExistingReferences, tt.refs) {
				t.Fatalf("lost reference tuples: %+v", got.ExistingReferences)
			}
			if len(got.Candidates) != 2 {
				t.Fatalf("truncated: %+v", got)
			}
			for _, c := range got.Candidates {
				if c.DefaultSelected == tt.over || storyboardHasIssue(c, "capacity_exceeded") != tt.over {
					t.Fatalf("capacity selection: %+v", c)
				}
			}
			after, _ := json.Marshal(candidates)
			if !reflect.DeepEqual(before, after) {
				t.Fatal("mutated candidate input")
			}
		})
	}
}

func TestStoryboardAssetResolverExistingOverflow(t *testing.T) {
	refs := []model.ShotAssetReference{{ID: "1", AssetVersionID: "1"}, {ID: "2", AssetVersionID: "2"}, {ID: "3", AssetVersionID: "3"}, {ID: "4", AssetVersionID: "4"}, {ID: "5", AssetVersionID: "5"}, {ID: "6", AssetVersionID: "6"}, {ID: "7", AssetVersionID: "7"}}
	got := applyStoryboardAssetCapacity(refs, nil)
	if got.TotalVersionCount != 7 || got.RemainingCapacity != 0 || !reflect.DeepEqual(got.ExistingReferences, refs) {
		t.Fatalf("changed old references: %+v", got)
	}
	if len(got.Issues) != 1 || got.Issues[0].Kind != "capacity_exceeded" {
		t.Fatalf("old overflow must be visible even without candidates: %+v", got)
	}
}

func TestStoryboardAssetResolverVisualCategories(t *testing.T) {
	catalog := storyboardResolverCatalog()
	catalog.Assets[0].Asset.Category = "environment"
	catalog.Assets[0].Asset.Title = "厨房"
	catalog.Assets[1].Asset.Category = "prop"
	catalog.Assets[1].Asset.Title = "碗勺"
	for _, tt := range []struct {
		text, kind string
		selected   bool
	}{{"场景：厨房\n道具：碗勺", "visual_label", true}, {"厨房里有人喝粥，碗勺静置", "mention", false}} {
		got := extractStoryboardAssetEvidence(tt.text, model.ShotRevision{}, catalog, nil)
		if len(got) != 2 {
			t.Fatalf("got %+v", got)
		}
		for _, c := range got {
			if c.DefaultSelected != tt.selected || c.Evidence[0].Kind != tt.kind {
				t.Fatalf("got %+v", c)
			}
		}
	}
	// 即使目录包含道具和场景，也不能从动作或上一镜猜测引用。
	if got := extractStoryboardAssetEvidence("视线落在领口，停止喝粥", model.ShotRevision{}, catalog, nil); len(got) != 0 {
		t.Fatalf("inferred objects: %+v", got)
	}
}
