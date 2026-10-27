package app

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func installBusinessWriteDetector(t *testing.T, db *gorm.DB) func() int {
	t.Helper()
	var mu sync.Mutex
	businessWrites := 0

	detector := func(tx *gorm.DB) {
		mu.Lock()
		defer mu.Unlock()
		sql := strings.TrimSpace(strings.ToUpper(tx.Statement.SQL.String()))
		if strings.Contains(sql, "UPDATE ASSETS SET ID = ID WHERE 1 = 0") {
			return
		}
		businessWrites++
	}

	cbCreate := "test_idempotency_no_create"
	cbUpdate := "test_idempotency_no_update"
	cbDelete := "test_idempotency_no_delete"

	if err := db.Callback().Create().Before("gorm:create").Register(cbCreate, detector); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Update().Before("gorm:update").Register(cbUpdate, detector); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Delete().Before("gorm:delete").Register(cbDelete, detector); err != nil {
		t.Fatal(err)
	}

	return func() int {
		_ = db.Callback().Create().Remove(cbCreate)
		_ = db.Callback().Update().Remove(cbUpdate)
		_ = db.Callback().Delete().Remove(cbDelete)
		mu.Lock()
		defer mu.Unlock()
		return businessWrites
	}
}

func TestStoryboardAssetIdempotencySetDifferenceAndReplay(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			svc, db := appendFixture(t, dialect)
			ctx := context.Background()

			preview, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatalf("preview failed: %v", err)
			}
			baseline := preview.Baseline

			// Verify s1 baseline has multi-role rows for v-old
			if len(baseline.Shots[0].References) != 2 {
				t.Fatalf("expected s1 to have 2 baseline references, got %d", len(baseline.Shots[0].References))
			}
			// Verify s2 baseline has vb1
			if len(baseline.Shots[1].References) != 1 {
				t.Fatalf("expected s2 to have 1 baseline reference, got %d", len(baseline.Shots[1].References))
			}

			// Original request:
			// Shot A (s1): selection has v1 (new) and v-old (existing multi-role). D_A = [v1].
			// Shot B (s2): selection has vb1 (already existing). D_B = [].
			req := BindingAppendRequest{
				Baseline: baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v1", "v-old"}},
					{ShotID: "s2", AssetVersionIDs: []string{"vb1"}},
				},
			}

			var artS2Before model.ShotArtifact
			if err := db.Where("id = ?", "art-s2-ready").First(&artS2Before).Error; err != nil {
				t.Fatal(err)
			}
			var artS1ReadyBefore model.ShotArtifact
			if err := db.Where("id = ?", "art-s1-ready").First(&artS1ReadyBefore).Error; err != nil {
				t.Fatal(err)
			}
			var artS1FailedBefore model.ShotArtifact
			if err := db.Where("id = ?", "art-s1-failed").First(&artS1FailedBefore).Error; err != nil {
				t.Fatal(err)
			}

			// --- First Call: real addition on s1, none on s2 ---
			res1, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", req)
			if err != nil {
				t.Fatalf("first append failed: %v", err)
			}
			if res1.Unchanged {
				t.Fatalf("expected first append to be Unchanged=false")
			}
			if res1.AddedCount != 1 {
				t.Fatalf("expected AddedCount=1, got %d", res1.AddedCount)
			}
			if res1.ProjectRevision != baseline.ProjectRevision+1 {
				t.Fatalf("expected ProjectRevision=%d, got %d", baseline.ProjectRevision+1, res1.ProjectRevision)
			}
			if len(res1.Shots) != 2 {
				t.Fatalf("expected 2 shot results, got %d", len(res1.Shots))
			}

			// s1 checks
			s1Res := res1.Shots[0]
			if s1Res.ShotID != "s1" || s1Res.Unchanged || s1Res.AddedCount != 1 || len(s1Res.AddedVersionIDs) != 1 || s1Res.AddedVersionIDs[0] != "v1" {
				t.Fatalf("unexpected s1 append result: %+v", s1Res)
			}
			// s2 checks
			s2Res := res1.Shots[1]
			if s2Res.ShotID != "s2" || !s2Res.Unchanged || s2Res.AddedCount != 0 || len(s2Res.AddedVersionIDs) != 0 {
				t.Fatalf("unexpected s2 append result: %+v", s2Res)
			}

			// Verify Shot A (s1) artifacts:
			// art-s1-ready was "ready" and selected=true -> becomes "stale" and selected=false
			var artS1ReadyAfter model.ShotArtifact
			if err := db.Where("id = ?", "art-s1-ready").First(&artS1ReadyAfter).Error; err != nil {
				t.Fatal(err)
			}
			if artS1ReadyAfter.Status != "stale" || artS1ReadyAfter.Selected {
				t.Fatalf("expected art-s1-ready to become status=stale, selected=false; got status=%s, selected=%v", artS1ReadyAfter.Status, artS1ReadyAfter.Selected)
			}
			if artS1ReadyAfter.RevisionID != artS1ReadyBefore.RevisionID || artS1ReadyAfter.MetadataJSON != artS1ReadyBefore.MetadataJSON || artS1ReadyAfter.CreatedAt != artS1ReadyBefore.CreatedAt {
				t.Fatalf("historical artifact metadata mutated: before=%+v, after=%+v", artS1ReadyBefore, artS1ReadyAfter)
			}

			// art-s1-failed was "failed" and selected=false -> status remains "failed", selected=false
			var artS1FailedAfter model.ShotArtifact
			if err := db.Where("id = ?", "art-s1-failed").First(&artS1FailedAfter).Error; err != nil {
				t.Fatal(err)
			}
			if artS1FailedAfter.Status != "failed" || artS1FailedAfter.Selected {
				t.Fatalf("expected art-s1-failed to remain status=failed, selected=false; got status=%s, selected=%v", artS1FailedAfter.Status, artS1FailedAfter.Selected)
			}

			// Verify Shot B (s2) artifacts: 100% untouched
			var artS2After model.ShotArtifact
			if err := db.Where("id = ?", "art-s2-ready").First(&artS2After).Error; err != nil {
				t.Fatal(err)
			}
			if artS2After.Status != "ready" || !artS2After.Selected || artS2After.UpdatedAt != artS2Before.UpdatedAt {
				t.Fatalf("s2 artifact was mutated: before=%+v, after=%+v", artS2Before, artS2After)
			}

			// Verify s1 existing multi-role references preserved + 1 new reference for v1
			var s1Refs []model.ShotAssetReference
			if err := db.Where("shot_id = ?", "s1").Order("id").Find(&s1Refs).Error; err != nil {
				t.Fatal(err)
			}
			if len(s1Refs) != 3 {
				t.Fatalf("expected 3 references on s1, got %d", len(s1Refs))
			}
			var hasRZ, hasRA, hasV1 bool
			for _, r := range s1Refs {
				if r.ID == "r-z" && r.AssetVersionID == "v-old" && r.Role == "character" && r.Status == "confirmed" {
					hasRZ = true
				}
				if r.ID == "r-a" && r.AssetVersionID == "v-old" && r.Role == "reference" && r.Status == "stale" {
					hasRA = true
				}
				if r.AssetVersionID == "v1" && r.Role == "reference" && r.Status == "linked" && r.ID != "" {
					hasV1 = true
				}
			}
			if !hasRZ || !hasRA || !hasV1 {
				t.Fatalf("s1 references incomplete or corrupted: %+v", s1Refs)
			}

			// Verify s2 references 100% untouched
			var s2Refs []model.ShotAssetReference
			if err := db.Where("shot_id = ?", "s2").Order("id").Find(&s2Refs).Error; err != nil {
				t.Fatal(err)
			}
			if len(s2Refs) != 1 || s2Refs[0].ID != "r-s2" || s2Refs[0].AssetVersionID != "vb1" {
				t.Fatalf("s2 references modified: %+v", s2Refs)
			}

			// --- Second Call: Exact same request replay ---
			// Snapshot DB state before replay
			stateBeforeReplay := appendFullState(t, db)

			// Install detector to verify zero business writes run on replay
			cleanupDetector := installBusinessWriteDetector(t, db)

			res2, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", req)
			businessWrites := cleanupDetector()

			if err != nil {
				t.Fatalf("replay call failed: %v", err)
			}
			if !res2.Unchanged {
				t.Fatalf("expected replay to be Unchanged=true, got false")
			}
			if res2.AddedCount != 0 {
				t.Fatalf("expected replay AddedCount=0, got %d", res2.AddedCount)
			}
			if res2.ProjectRevision != baseline.ProjectRevision+1 {
				t.Fatalf("expected replay ProjectRevision=%d, got %d", baseline.ProjectRevision+1, res2.ProjectRevision)
			}
			if len(res2.Shots) != 2 {
				t.Fatalf("expected 2 shots in replay result, got %d", len(res2.Shots))
			}
			for i, shot := range res2.Shots {
				if !shot.Unchanged || shot.AddedCount != 0 || len(shot.AddedVersionIDs) != 0 {
					t.Fatalf("shot %d in replay not unchanged: %+v", i, shot)
				}
				if len(shot.References) == 0 || len(shot.Artifacts) == 0 {
					t.Fatalf("shot %d missing references or artifacts in replay: %+v", i, shot)
				}
			}

			if businessWrites != 0 {
				t.Fatalf("expected 0 business writes on replay, got %d", businessWrites)
			}
			stateAfterReplay := appendFullState(t, db)
			if stateBeforeReplay != stateAfterReplay {
				t.Fatalf("DB state mutated during replay!")
			}

			// --- All-existing batch (D_A = [] and D_B = [] on initial call) ---
			svcFresh, dbFresh := appendFixture(t, dialect)
			previewFresh, err := svcFresh.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatal(err)
			}
			reqAllExisting := BindingAppendRequest{
				Baseline: previewFresh.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v-old"}},
					{ShotID: "s2", AssetVersionIDs: []string{"vb1"}},
				},
			}

			stateBeforeAllExisting := appendFullState(t, dbFresh)
			cleanupDetectorFresh := installBusinessWriteDetector(t, dbFresh)

			resAllExisting, err := svcFresh.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", reqAllExisting)
			freshWrites := cleanupDetectorFresh()

			if err != nil {
				t.Fatalf("all-existing append failed: %v", err)
			}
			if !resAllExisting.Unchanged || resAllExisting.AddedCount != 0 {
				t.Fatalf("expected all-existing to be Unchanged=true, AddedCount=0; got %+v", resAllExisting)
			}
			if resAllExisting.ProjectRevision != previewFresh.Baseline.ProjectRevision {
				t.Fatalf("expected ProjectRevision to remain %d, got %d", previewFresh.Baseline.ProjectRevision, resAllExisting.ProjectRevision)
			}
			if freshWrites != 0 {
				t.Fatalf("expected 0 business writes on all-existing initial call, got %d", freshWrites)
			}
			stateAfterAllExisting := appendFullState(t, dbFresh)
			if stateBeforeAllExisting != stateAfterAllExisting {
				t.Fatalf("DB state mutated during all-existing append!")
			}
		})
	}
}

func TestStoryboardAssetIdempotencyConcurrentDuplicateRequests(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dbs := profileConcurrencyDatabases(t, dialect)
			db1 := dbs[0]
			db2 := dbs[1]
			if err := database.MigrateSchema(db1); err != nil {
				t.Fatal(err)
			}
			rows := []any{
				&model.User{ID: "u", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.Project{ID: "p", UserID: "u", Name: "append-proj", Revision: 17},
				&model.ProjectUnit{ID: "unit", ProjectID: "p", Title: "unit-1"},
				&model.Shot{ID: "s1", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "rev1", Description: "角色：阿青", Title: "shot-1", Status: "draft", Position: 1},
				&model.Shot{ID: "s2", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "rev2", Description: "道具：青钢剑", Title: "shot-2", Status: "draft", Position: 2},
				&model.ShotRevision{ID: "rev1", ShotID: "s1", Version: 1, Dialogue: "阿青：你好", ActionBeatsJSON: `[{"action":"look"}]`},
				&model.ShotRevision{ID: "rev2", ShotID: "s2", Version: 1, Dialogue: "拔剑", ActionBeatsJSON: `[{"action":"draw"}]`},
				&model.Asset{ID: "a", UserID: "u", Category: model.AssetCategoryCharacter, Title: "阿青", Status: "draft", PrimaryVersionID: "v1", PayloadJSON: `{}`},
				&model.Asset{ID: "b", UserID: "u", Category: model.AssetCategoryProp, Title: "青钢剑", Status: "draft", PrimaryVersionID: "vb1", PayloadJSON: `{}`},
				&model.ProjectAssetLink{ID: "link-a", ProjectID: "p", AssetID: "a"},
				&model.ProjectAssetLink{ID: "link-b", ProjectID: "p", AssetID: "b"},
				&model.Resource{ID: "res1", UserID: "u", Kind: "image", Status: model.ResourceStatusReady, Width: 100, Height: 100, MimeType: "image/png", ObjectKey: "u/res1.png"},
				&model.AssetVersion{ID: "v1", AssetID: "a", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res1"}}`},
				&model.AssetVersion{ID: "vb1", AssetID: "b", Version: 1, Status: "draft", DefinitionJSON: `{}`},
				&model.ShotAssetReference{ID: "r-s2", ShotID: "s2", AssetVersionID: "vb1", Role: "prop", Status: "confirmed"},
				&model.ShotArtifact{ID: "art-s1-ready", ProjectID: "p", UnitID: "unit", ShotID: "s1", RevisionID: "rev1", Type: "image", Version: 1, Status: "ready", Selected: true, MetadataJSON: `{"k":1}`},
				&model.ShotArtifact{ID: "art-s2-ready", ProjectID: "p", UnitID: "unit", ShotID: "s2", RevisionID: "rev2", Type: "image", Version: 1, Status: "ready", Selected: true, MetadataJSON: `{"k":2}`},
			}
			for _, row := range rows {
				if err := db1.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			svc1 := &Service{repo: repository.New(db1)}
			svc2 := &Service{repo: repository.New(db2)}

			ctx := context.Background()
			preview, err := svc1.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatalf("preview failed: %v", err)
			}

			req := BindingAppendRequest{
				Baseline: preview.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
					{ShotID: "s2", AssetVersionIDs: []string{}},
				},
			}

			type callResult struct {
				res BindingAppendResult
				err error
			}
			results := make(chan callResult, 2)
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(2)

			go func() {
				defer wg.Done()
				<-start
				res, err := svc1.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", req)
				results <- callResult{res, err}
			}()
			go func() {
				defer wg.Done()
				<-start
				res, err := svc2.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", req)
				results <- callResult{res, err}
			}()

			time.Sleep(20 * time.Millisecond)
			close(start)
			wg.Wait()
			close(results)

			var successWrites, successIdempotent int
			for cr := range results {
				if cr.err != nil {
					t.Fatalf("concurrent request returned error: %v", cr.err)
				}
				if !cr.res.Unchanged && cr.res.AddedCount == 1 && cr.res.ProjectRevision == preview.Baseline.ProjectRevision+1 {
					successWrites++
				} else if cr.res.Unchanged && cr.res.AddedCount == 0 && cr.res.ProjectRevision == preview.Baseline.ProjectRevision+1 {
					successIdempotent++
				} else {
					t.Fatalf("unexpected concurrent result: %+v", cr.res)
				}
			}

			if successWrites != 1 || successIdempotent != 1 {
				t.Fatalf("expected exactly 1 write and 1 idempotent replay, got writes=%d idempotent=%d", successWrites, successIdempotent)
			}

			// Verify DB has only 1 revision bump and no duplicate references
			var proj model.Project
			if err := db1.Where("id = ?", "p").First(&proj).Error; err != nil {
				t.Fatal(err)
			}
			if proj.Revision != preview.Baseline.ProjectRevision+1 {
				t.Fatalf("expected project revision %d, got %d", preview.Baseline.ProjectRevision+1, proj.Revision)
			}
			var refs []model.ShotAssetReference
			if err := db1.Where("shot_id = ? AND asset_version_id = ?", "s1", "v1").Find(&refs).Error; err != nil {
				t.Fatal(err)
			}
			if len(refs) != 1 {
				t.Fatalf("expected exactly 1 reference for v1 on s1, got %d", len(refs))
			}
		})
	}
}

func TestStoryboardAssetIdempotencyPostCommitDriftRejectsWith409(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			ctx := context.Background()

			type mutationCase struct {
				name   string
				mutate func(t *testing.T, db *gorm.DB, req *BindingAppendRequest)
			}

			cases := []mutationCase{
				{
					name: "script_shot_description_changed",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.Shot{}).Where("id = ?", "s1").Update("description", "改动脚本描述").Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "script_shot_revision_dialogue_changed",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.ShotRevision{}).Where("id = ?", "rev1").Update("dialogue", "阿青：新的台词").Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "adding_another_reference",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Create(&model.ShotAssetReference{
							ID:             "r-extra",
							ShotID:         "s1",
							AssetVersionID: "vb2",
							Role:           "prop",
							Status:         "linked",
						}).Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "deleting_existing_reference",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Where("id = ?", "r-z").Delete(&model.ShotAssetReference{}).Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "deleting_newly_added_reference",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Where("shot_id = ? AND asset_version_id = ?", "s1", "v1").Delete(&model.ShotAssetReference{}).Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "changing_role_on_existing_reference",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.ShotAssetReference{}).Where("id = ?", "r-z").Update("role", "environment").Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "changing_status_on_newly_added_reference",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.ShotAssetReference{}).Where("shot_id = ? AND asset_version_id = ?", "s1", "v1").Update("status", "stale").Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "unlinking_asset",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Where("id = ?", "link-a").Delete(&model.ProjectAssetLink{}).Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "switching_asset_primary_version",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.Asset{}).Where("id = ?", "a").Update("primary_version_id", "v-old").Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "changing_version_status",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.AssetVersion{}).Where("id = ?", "v1").Update("status", "archived").Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "changing_version_definition",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.AssetVersion{}).Where("id = ?", "v1").Update("definition_json", `{"referenceImage":{"resourceId":"res-foreign"}}`).Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "media_resource_mutated",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.Resource{}).Where("id = ?", "res1").Update("width", 999).Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "reselecting_invalidated_artifact",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.ShotArtifact{}).Where("id = ?", "art-s1-ready").Updates(map[string]any{"selected": true, "status": "ready"}).Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "creating_new_shot_artifact",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Create(&model.ShotArtifact{
							ID:         "art-new",
							ProjectID:  "p",
							UnitID:     "unit",
							ShotID:     "s1",
							RevisionID: "rev1",
							Type:       "image",
							Status:     "ready",
							Selected:   true,
						}).Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "extra_project_revision_increment",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						if err := db.Model(&model.Project{}).Where("id = ?", "p").Update("revision", 99).Error; err != nil {
							t.Fatal(err)
						}
					},
				},
				{
					name: "changing_selections_on_retry_different_version",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						req.Selections = []BindingSelection{
							{ShotID: "s1", AssetVersionIDs: []string{"vb2"}},
							{ShotID: "s2", AssetVersionIDs: []string{}},
						}
					},
				},
				{
					name: "changing_selections_on_retry_empty",
					mutate: func(t *testing.T, db *gorm.DB, req *BindingAppendRequest) {
						req.Selections = []BindingSelection{
							{ShotID: "s1", AssetVersionIDs: []string{}},
							{ShotID: "s2", AssetVersionIDs: []string{}},
						}
					},
				},
			}

			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					svc, db := appendFixture(t, dialect)
					preview, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
					if err != nil {
						t.Fatal(err)
					}
					req := BindingAppendRequest{
						Baseline: preview.Baseline,
						Selections: []BindingSelection{
							{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
							{ShotID: "s2", AssetVersionIDs: []string{}},
						},
					}

					// First append succeeds
					res1, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", req)
					if err != nil || res1.Unchanged || res1.AddedCount != 1 {
						t.Fatalf("first append failed: res=%+v, err=%v", res1, err)
					}

					// Apply post-commit drift mutation
					tc.mutate(t, db, &req)

					// Snapshot state to verify zero mutations occurred on rejected retry
					stateBeforeRetry := appendFullState(t, db)

					// Replay original request
					_, err = svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", req)
					if err == nil {
						t.Fatalf("expected 409 conflict error after drift, got nil")
					}
					var appErr *AppError
					if !errors.As(err, &appErr) || appErr.Status != CodeConflict || appErr.Reason != ReasonStoryboardAssetBindingConflict {
						t.Fatalf("expected 409 storyboard_asset_binding_conflict, got %+v", err)
					}

					stateAfterRetry := appendFullState(t, db)
					if stateBeforeRetry != stateAfterRetry {
						t.Fatalf("DB state was modified during rejected replay!")
					}
				})
			}
		})
	}
}

func TestStoryboardAssetIdempotencyLegacyMultiShotDisconnectAndDrift(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			svc, db := appendFixture(t, dialect)
			ctx := context.Background()

			// Add a 3rd legacy shot s3 with narrative/dialogue mentions only (defaultSelected=false) and a succeeded selected artifact
			extraRows := []any{
				&model.Shot{ID: "s3", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "rev3", Description: "窗外夜色", Title: "shot-3", Status: "draft", Position: 3},
				&model.ShotRevision{ID: "rev3", ShotID: "s3", Version: 1, PlotDescription: "阿青在窗边沉思，目光掠过竹林", Dialogue: "阿青：雨停了", ActionBeatsJSON: `[{"action":"think"}]`},
				&model.ShotArtifact{ID: "art-s1-succeeded", ProjectID: "p", UnitID: "unit", ShotID: "s1", RevisionID: "rev1", Type: "image", Version: 3, Status: "succeeded", Selected: false, MetadataJSON: `{"k":11}`},
				&model.ShotArtifact{ID: "art-s1-approved", ProjectID: "p", UnitID: "unit", ShotID: "s1", RevisionID: "rev1", Type: "image", Version: 4, Status: "approved", Selected: false, MetadataJSON: `{"k":12}`},
				&model.ShotArtifact{ID: "art-s3-succeeded", ProjectID: "p", UnitID: "unit", ShotID: "s3", RevisionID: "rev3", Type: "image", Version: 1, Status: "succeeded", Selected: true, MetadataJSON: `{"k":31}`},
			}
			for _, row := range extraRows {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}

			preview, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2", "s3"}})
			if err != nil {
				t.Fatalf("3-shot preview failed: %v", err)
			}
			if len(preview.Shots) != 3 {
				t.Fatalf("expected 3 shots in preview, got %d", len(preview.Shots))
			}
			for _, c := range preview.Shots[2].Candidates {
				if c.DefaultSelected {
					t.Fatalf("expected s3 narrative/dialogue candidate %s to have defaultSelected=false: %+v", c.Name, c)
				}
			}

			// Multi-shot request:
			// s1: existing multi-role v-old + new v1 (D = [v1])
			// s2: existing vb1 (D = [])
			// s3: narrative mention unselected [] (D = [])
			req := BindingAppendRequest{
				Baseline: preview.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v-old", "v1"}},
					{ShotID: "s2", AssetVersionIDs: []string{"vb1"}},
					{ShotID: "s3", AssetVersionIDs: []string{}},
				},
			}

			res1, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", req)
			if err != nil {
				t.Fatalf("initial 3-shot append failed: %v", err)
			}
			if res1.Unchanged || res1.AddedCount != 1 || res1.ProjectRevision != preview.Baseline.ProjectRevision+1 || len(res1.Shots) != 3 {
				t.Fatalf("unexpected 3-shot append result: %+v", res1)
			}
			if res1.Shots[0].Unchanged || res1.Shots[0].AddedCount != 1 || !res1.Shots[1].Unchanged || res1.Shots[1].AddedCount != 0 || !res1.Shots[2].Unchanged || res1.Shots[2].AddedCount != 0 {
				t.Fatalf("unexpected per-shot results: %+v", res1.Shots)
			}

			// Verify selective invalidation across s1, s2, s3
			var arts []model.ShotArtifact
			if err := db.Where("shot_id IN ?", []string{"s1", "s2", "s3"}).Order("id").Find(&arts).Error; err != nil {
				t.Fatal(err)
			}
			artMap := map[string]model.ShotArtifact{}
			for _, a := range arts {
				artMap[a.ID] = a
			}
			for _, id := range []string{"art-s1-ready", "art-s1-succeeded", "art-s1-approved"} {
				if artMap[id].Status != "stale" || artMap[id].Selected {
					t.Fatalf("expected s1 artifact %s to be stale/selected=false, got %+v", id, artMap[id])
				}
			}
			if artMap["art-s1-failed"].Status != "failed" || artMap["art-s1-failed"].Selected {
				t.Fatalf("expected s1 failed artifact to remain failed/selected=false, got %+v", artMap["art-s1-failed"])
			}
			if artMap["art-s2-ready"].Status != "ready" || !artMap["art-s2-ready"].Selected {
				t.Fatalf("expected s2 untouched artifact to remain ready/selected=true, got %+v", artMap["art-s2-ready"])
			}
			if artMap["art-s3-succeeded"].Status != "succeeded" || !artMap["art-s3-succeeded"].Selected {
				t.Fatalf("expected s3 untouched artifact to remain succeeded/selected=true, got %+v", artMap["art-s3-succeeded"])
			}

			// Disconnect idempotent replay: exact same request returns 200/unchanged=true with zero DB writes
			stateBeforeReplay := appendFullState(t, db)
			cleanupDetector := installBusinessWriteDetector(t, db)
			resReplay, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", req)
			writes := cleanupDetector()
			if err != nil {
				t.Fatalf("3-shot disconnect replay failed: %v", err)
			}
			if !resReplay.Unchanged || resReplay.AddedCount != 0 || resReplay.ProjectRevision != preview.Baseline.ProjectRevision+1 || len(resReplay.Shots) != 3 {
				t.Fatalf("unexpected 3-shot replay result: %+v", resReplay)
			}
			for i, sr := range resReplay.Shots {
				if !sr.Unchanged || sr.AddedCount != 0 || len(sr.AddedVersionIDs) != 0 {
					t.Fatalf("expected shot[%d] in 3-shot replay to be unchanged, got %+v", i, sr)
				}
			}
			if writes != 0 || appendFullState(t, db) != stateBeforeReplay {
				t.Fatalf("expected 0 business writes and identical state on 3-shot replay, got writes=%d", writes)
			}

			// Post-commit drift on s3 (untouched shot artifact deselected or script edited) -> must reject replay with 409 conflict
			if err := db.Model(&model.ShotArtifact{}).Where("id = ?", "art-s3-succeeded").Update("selected", false).Error; err != nil {
				t.Fatal(err)
			}
			stateBeforeDriftReplay := appendFullState(t, db)
			_, err = svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", req)
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != CodeConflict || appErr.Reason != ReasonStoryboardAssetBindingConflict {
				t.Fatalf("expected 409 storyboard_asset_binding_conflict when s3 artifact drifted, got %v", err)
			}
			if appendFullState(t, db) != stateBeforeDriftReplay {
				t.Fatalf("rejected 3-shot drift replay mutated DB state!")
			}
		})
	}
}

