package app

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func appendFixture(t *testing.T, dialect string) (*Service, *gorm.DB) {
	t.Helper()
	dbs := profileConcurrencyDatabases(t, dialect)
	db := dbs[0]
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	rows := []any{
		&model.User{ID: "u", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.User{ID: "admin", Username: "admin", Role: model.UserRoleAdmin, Status: model.UserStatusActive},
		&model.User{ID: "foreign-u", Username: "foreign", Role: model.UserRoleUser, Status: model.UserStatusActive},
		&model.Project{ID: "p", UserID: "u", Name: "append-proj", Revision: 17},
		&model.Project{ID: "other-p", UserID: "u", Name: "other-proj", Revision: 3},
		&model.Project{ID: "foreign-p", UserID: "foreign-u", Name: "PRIVATE_FOREIGN_PROJECT", Revision: 1},
		&model.ProjectUnit{ID: "unit", ProjectID: "p", Title: "unit-1"},
		&model.ProjectUnit{ID: "other-unit", ProjectID: "p", Title: "unit-2"},
		&model.ProjectUnit{ID: "other-p-unit", ProjectID: "other-p", Title: "other-p-unit"},
		&model.Shot{ID: "s1", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "rev1", Description: "角色：阿青", Title: "shot-1", Status: "draft", Position: 1},
		&model.Shot{ID: "s2", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "rev2", Description: "道具：青钢剑", Title: "shot-2", Status: "draft", Position: 2},
		&model.Shot{ID: "cross-unit-shot", ProjectID: "p", UnitID: "other-unit", CurrentRevisionID: "rev-cu", Description: "异单元镜头", Title: "shot-cu", Status: "draft"},
		&model.Shot{ID: "cross-proj-shot", ProjectID: "other-p", UnitID: "other-p-unit", CurrentRevisionID: "rev-cp", Description: "异项目镜头", Title: "shot-cp", Status: "draft"},
		&model.ShotRevision{ID: "rev1", ShotID: "s1", Version: 1, Dialogue: "阿青：你好", ActionBeatsJSON: `[{"action":"look"}]`},
		&model.ShotRevision{ID: "rev2", ShotID: "s2", Version: 1, Dialogue: "拔剑", ActionBeatsJSON: `[{"action":"draw"}]`},
		&model.ShotRevision{ID: "rev-cu", ShotID: "cross-unit-shot", Version: 1, ActionBeatsJSON: `[]`},
		&model.ShotRevision{ID: "rev-cp", ShotID: "cross-proj-shot", Version: 1, ActionBeatsJSON: `[]`},
		&model.Asset{ID: "a", UserID: "u", Category: model.AssetCategoryCharacter, Title: "阿青", Status: "draft", PrimaryVersionID: "v1", PayloadJSON: `{}`},
		&model.Asset{ID: "b", UserID: "u", Category: model.AssetCategoryProp, Title: "青钢剑", Status: "draft", PrimaryVersionID: "vb1", PayloadJSON: `{}`},
		&model.Asset{ID: "c", UserID: "u", Category: model.AssetCategoryEnvironment, Title: "竹林", Status: "draft", PrimaryVersionID: "vc1", PayloadJSON: `{}`},
		&model.Asset{ID: "unlinked-a", UserID: "u", Category: model.AssetCategoryCharacter, Title: "未链接资产", Status: "draft", PrimaryVersionID: "v-unlinked", PayloadJSON: `{}`},
		&model.Asset{ID: "foreign-a", UserID: "foreign-u", Category: model.AssetCategoryCharacter, Title: "PRIVATE_FOREIGN_TITLE", Status: "draft", PrimaryVersionID: "v-foreign", PayloadJSON: `{}`},
		&model.Asset{ID: "material-a", UserID: "u", Category: model.AssetCategoryMaterial, Title: "素材分类", Status: "draft", PrimaryVersionID: "v-material", PayloadJSON: `{}`},
		&model.Asset{ID: "archived-a", UserID: "u", Category: model.AssetCategoryProp, Title: "归档资产", Status: "archived", PrimaryVersionID: "v-archived-asset", PayloadJSON: `{}`},
		&model.ProjectAssetLink{ID: "link-a", ProjectID: "p", AssetID: "a"},
		&model.ProjectAssetLink{ID: "link-b", ProjectID: "p", AssetID: "b"},
		&model.ProjectAssetLink{ID: "link-c", ProjectID: "p", AssetID: "c"},
		&model.ProjectAssetLink{ID: "link-material", ProjectID: "p", AssetID: "material-a"},
		&model.ProjectAssetLink{ID: "link-archived", ProjectID: "p", AssetID: "archived-a"},
		&model.Resource{ID: "res1", UserID: "u", Kind: "image", Status: model.ResourceStatusReady, Width: 100, Height: 100, MimeType: "image/png", ObjectKey: "u/res1.png"},
		&model.Resource{ID: "res-foreign", UserID: "foreign-u", Kind: "image", Status: model.ResourceStatusReady, Width: 100, Height: 100, MimeType: "image/png", ObjectKey: "PRIVATE_OBJECT_KEY"},
		&model.AssetVersion{ID: "v1", AssetID: "a", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res1"}}`},
		&model.AssetVersion{ID: "v-old", AssetID: "a", Version: 2, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "v-archived-ver", AssetID: "a", Version: 3, Status: "archived", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "v-deleted-ver", AssetID: "a", Version: 4, Status: "deleted", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb1", AssetID: "b", Version: 1, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb2", AssetID: "b", Version: 2, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb3", AssetID: "b", Version: 3, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb4", AssetID: "b", Version: 4, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb5", AssetID: "b", Version: 5, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vb6", AssetID: "b", Version: 6, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "vc1", AssetID: "c", Version: 1, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "v-unlinked", AssetID: "unlinked-a", Version: 1, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "v-foreign", AssetID: "foreign-a", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res-foreign"}}`},
		&model.AssetVersion{ID: "v-material", AssetID: "material-a", Version: 1, Status: "draft", DefinitionJSON: `{}`},
		&model.AssetVersion{ID: "v-archived-asset", AssetID: "archived-a", Version: 1, Status: "draft", DefinitionJSON: `{}`},
		&model.ShotAssetReference{ID: "r-z", ShotID: "s1", AssetVersionID: "v-old", Role: "character", Status: "confirmed"},
		&model.ShotAssetReference{ID: "r-a", ShotID: "s1", AssetVersionID: "v-old", Role: "reference", Status: "stale"},
		&model.ShotAssetReference{ID: "r-s2", ShotID: "s2", AssetVersionID: "vb1", Role: "prop", Status: "confirmed"},
		&model.ShotArtifact{ID: "art-s1-ready", ProjectID: "p", UnitID: "unit", ShotID: "s1", RevisionID: "rev1", Type: "image", Version: 1, Status: "ready", Selected: true, MetadataJSON: `{"k":1}`},
		&model.ShotArtifact{ID: "art-s1-failed", ProjectID: "p", UnitID: "unit", ShotID: "s1", RevisionID: "rev1", Type: "image", Version: 2, Status: "failed", Selected: false, MetadataJSON: `{"k":2}`},
		&model.ShotArtifact{ID: "art-s2-ready", ProjectID: "p", UnitID: "unit", ShotID: "s2", RevisionID: "rev2", Type: "image", Version: 1, Status: "ready", Selected: true, MetadataJSON: `{"k":3}`},
		&model.WorkflowInstance{ID: "wf-unit", ProjectID: "p", UnitID: "unit", Status: "completed", Revision: 2},
		&model.WorkflowStepInstance{ID: "wf-step-sb", WorkflowInstanceID: "wf-unit", StepKey: "storyboard", Position: 1, Status: "completed"},
		&model.WorkflowStepInstance{ID: "wf-step-vid", WorkflowInstanceID: "wf-unit", StepKey: "video", Position: 2, Status: "completed"},
	}
	for _, row := range rows {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	return &Service{repo: repository.New(db)}, db
}

func appendFullState(t *testing.T, db *gorm.DB) string {
	t.Helper()
	state := map[string]any{}
	tables := []string{
		"projects",
		"project_units",
		"shots",
		"shot_revisions",
		"shot_asset_references",
		"shot_artifacts",
		"workflow_instances",
		"workflow_step_instances",
		"project_asset_links",
		"assets",
		"asset_versions",
		"asset_representations",
		"resources",
	}
	for _, table := range tables {
		var rows []map[string]any
		if err := db.Table(table).Order("id").Find(&rows).Error; err != nil {
			t.Fatal(err)
		}
		state[table] = rows
	}
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func assertAppendAppError(t *testing.T, err error, wantStatus, wantCode int, wantReason ErrorReason) *AppError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error (status=%d code=%d reason=%s), got nil", wantStatus, wantCode, wantReason)
	}
	var appErr *AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("expected *AppError, got %T: %v", err, err)
	}
	if appErr.Status != wantStatus || appErr.Code != wantCode || appErr.Reason != wantReason {
		t.Fatalf("AppError mismatch: got status=%d code=%d reason=%s message=%q, want status=%d code=%d reason=%s",
			appErr.Status, appErr.Code, appErr.Reason, appErr.Message, wantStatus, wantCode, wantReason)
	}
	for _, forbidden := range []string{"PRIVATE_FOREIGN_TITLE", "PRIVATE_FOREIGN_PROJECT", "PRIVATE_OBJECT_KEY"} {
		if strings.Contains(appErr.Message, forbidden) {
			t.Fatalf("error message leaked sensitive data %q: %s", forbidden, appErr.Message)
		}
	}
	return appErr
}

func TestAppendProjectUnitShotAssetBindingsHappyPathAndSelectiveInvalidation(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			svc, db := appendFixture(t, dialect)
			ctx := context.Background()

			preview, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatal(err)
			}

			// s1: selects existing "v-old" + new "v1" + duplicate "v1" (D = ["v1"])
			// s2: selects existing "vb1" only (D = [])
			res, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
				Baseline: preview.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v-old", "v1", "v1"}},
					{ShotID: "s2", AssetVersionIDs: []string{"vb1"}},
				},
			})
			if err != nil {
				t.Fatalf("AppendProjectUnitShotAssetBindings failed: %v", err)
			}

			if res.ProjectRevision != 18 || res.Unchanged || res.AddedCount != 1 || len(res.Shots) != 2 {
				t.Fatalf("unexpected append result: %+v", res)
			}
			if res.Shots[0].ShotID != "s1" || res.Shots[0].Unchanged || len(res.Shots[0].AddedVersionIDs) != 1 || res.Shots[0].AddedVersionIDs[0] != "v1" {
				t.Fatalf("unexpected s1 result: %+v", res.Shots[0])
			}
			if res.Shots[1].ShotID != "s2" || !res.Shots[1].Unchanged || len(res.Shots[1].AddedVersionIDs) != 0 {
				t.Fatalf("unexpected s2 result: %+v", res.Shots[1])
			}

			// Verify existing multi-role/status references on s1 are untouched and new reference has role="reference", status="linked".
			var s1Refs []model.ShotAssetReference
			if err := db.Where("shot_id = ?", "s1").Order("id").Find(&s1Refs).Error; err != nil {
				t.Fatal(err)
			}
			if len(s1Refs) != 3 {
				t.Fatalf("expected 3 references on s1, got %d: %+v", len(s1Refs), s1Refs)
			}
			foundRZ, foundRA, foundNew := false, false, false
			newRefID := ""
			for _, ref := range s1Refs {
				switch ref.ID {
				case "r-z":
					foundRZ = ref.AssetVersionID == "v-old" && ref.Role == "character" && ref.Status == "confirmed"
				case "r-a":
					foundRA = ref.AssetVersionID == "v-old" && ref.Role == "reference" && ref.Status == "stale"
				default:
					foundNew = ref.AssetVersionID == "v1" && ref.Role == "reference" && ref.Status == "linked"
					newRefID = ref.ID
				}
			}
			if !foundRZ || !foundRA || !foundNew || newRefID == "" {
				t.Fatalf("s1 references not preserved/added properly: %+v", s1Refs)
			}

			// Verify C1: BindingAppendResult.Shots carries per-shot addedCount, refreshed references, and refreshed artifacts from the same transaction.
			type wireShotResult struct {
				ShotID          string             `json:"shotId"`
				AddedCount      *int               `json:"addedCount"`
				AddedVersionIDs []string           `json:"addedVersionIds"`
				Unchanged       bool               `json:"unchanged"`
				References      []BindingReference `json:"references"`
				Artifacts       []BindingArtifact  `json:"artifacts"`
			}
			var wire struct {
				Shots []wireShotResult `json:"shots"`
			}
			rawRes, err := json.Marshal(res)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(rawRes, &wire); err != nil {
				t.Fatal(err)
			}
			if len(wire.Shots) != 2 {
				t.Fatalf("expected 2 wire shots, got %+v", wire.Shots)
			}
			if wire.Shots[0].AddedCount == nil || *wire.Shots[0].AddedCount != 1 {
				t.Fatalf("expected s1 addedCount=1 in JSON payload, got %v (raw=%s)", wire.Shots[0].AddedCount, string(rawRes))
			}
			if len(wire.Shots[0].References) != 3 {
				t.Fatalf("expected s1 refreshed references len=3, got %+v", wire.Shots[0].References)
			}
			for i, ref := range s1Refs {
				got := wire.Shots[0].References[i]
				if got.ReferenceID != ref.ID || got.AssetVersionID != ref.AssetVersionID || got.Role != ref.Role || got.Status != ref.Status {
					t.Fatalf("s1 refreshed reference[%d] mismatch: got %+v, want id=%s ver=%s role=%s status=%s", i, got, ref.ID, ref.AssetVersionID, ref.Role, ref.Status)
				}
			}
			if len(wire.Shots[0].Artifacts) != 2 {
				t.Fatalf("expected s1 refreshed artifacts len=2, got %+v", wire.Shots[0].Artifacts)
			}
			if wire.Shots[0].Artifacts[0].ArtifactID != "art-s1-failed" || wire.Shots[0].Artifacts[0].Status != "failed" || wire.Shots[0].Artifacts[0].Selected || wire.Shots[0].Artifacts[0].ContentFingerprint == "" {
				t.Fatalf("unexpected s1 artifact[0]: %+v", wire.Shots[0].Artifacts[0])
			}
			if wire.Shots[0].Artifacts[1].ArtifactID != "art-s1-ready" || wire.Shots[0].Artifacts[1].Status != "stale" || wire.Shots[0].Artifacts[1].Selected || wire.Shots[0].Artifacts[1].ContentFingerprint == "" {
				t.Fatalf("unexpected s1 artifact[1] (must reflect post-invalidation stale/unselected): %+v", wire.Shots[0].Artifacts[1])
			}
			if wire.Shots[1].AddedCount == nil || *wire.Shots[1].AddedCount != 0 {
				t.Fatalf("expected s2 addedCount=0 in JSON payload, got %v", wire.Shots[1].AddedCount)
			}
			if len(wire.Shots[1].References) != 1 || wire.Shots[1].References[0].ReferenceID != "r-s2" || wire.Shots[1].References[0].AssetVersionID != "vb1" {
				t.Fatalf("unexpected s2 refreshed references: %+v", wire.Shots[1].References)
			}
			if len(wire.Shots[1].Artifacts) != 1 || wire.Shots[1].Artifacts[0].ArtifactID != "art-s2-ready" || wire.Shots[1].Artifacts[0].Status != "ready" || !wire.Shots[1].Artifacts[0].Selected {
				t.Fatalf("unexpected s2 refreshed artifacts: %+v", wire.Shots[1].Artifacts)
			}

			// Verify selective artifact invalidation: s1 ready artifact becomes stale/unselected; s1 failed artifact stays failed; s2 ready artifact stays ready/selected!
			var artS1Ready, artS1Failed, artS2Ready model.ShotArtifact
			if err := db.First(&artS1Ready, "id = ?", "art-s1-ready").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&artS1Failed, "id = ?", "art-s1-failed").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&artS2Ready, "id = ?", "art-s2-ready").Error; err != nil {
				t.Fatal(err)
			}
			if artS1Ready.Status != "stale" || artS1Ready.Selected {
				t.Fatalf("expected art-s1-ready to become stale/unselected, got status=%s selected=%v", artS1Ready.Status, artS1Ready.Selected)
			}
			if artS1Failed.Status != "failed" || artS1Failed.Selected {
				t.Fatalf("expected art-s1-failed to remain failed/unselected, got status=%s selected=%v", artS1Failed.Status, artS1Failed.Selected)
			}
			if artS2Ready.Status != "ready" || !artS2Ready.Selected {
				t.Fatalf("expected art-s2-ready (D=empty) to remain ready/selected, got status=%s selected=%v", artS2Ready.Status, artS2Ready.Selected)
			}

			// Verify unit workflow invalidation and project revision increment.
			var wf model.WorkflowInstance
			if err := db.First(&wf, "id = ?", "wf-unit").Error; err != nil {
				t.Fatal(err)
			}
			if wf.Status != model.WorkflowStatusActive || wf.Revision != 3 {
				t.Fatalf("expected workflow status active and revision 3, got status=%s revision=%d", wf.Status, wf.Revision)
			}
			var sbStep, vidStep model.WorkflowStepInstance
			if err := db.First(&sbStep, "id = ?", "wf-step-sb").Error; err != nil {
				t.Fatal(err)
			}
			if err := db.First(&vidStep, "id = ?", "wf-step-vid").Error; err != nil {
				t.Fatal(err)
			}
			if sbStep.Status != model.WorkflowStepStatusRunning || vidStep.Status != model.WorkflowStepStatusPending {
				t.Fatalf("expected storyboard=running and video=pending, got sb=%s vid=%s", sbStep.Status, vidStep.Status)
			}
			var proj model.Project
			if err := db.First(&proj, "id = ?", "p").Error; err != nil {
				t.Fatal(err)
			}
			if proj.Revision != 18 {
				t.Fatalf("expected project revision 18, got %d", proj.Revision)
			}

			// Verify all-unchanged batch performs zero writes and keeps revision 18.
			preview2, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatal(err)
			}
			beforeUnchanged := appendFullState(t, db)
			res2, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
				Baseline: preview2.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v-old", "v1"}},
					{ShotID: "s2", AssetVersionIDs: []string{}},
				},
			})
			if err != nil {
				t.Fatalf("unchanged append failed: %v", err)
			}
			if !res2.Unchanged || res2.AddedCount != 0 || res2.ProjectRevision != 18 {
				t.Fatalf("expected unchanged result with revision 18, got %+v", res2)
			}
			var wire2 struct {
				Shots []wireShotResult `json:"shots"`
			}
			rawRes2, err := json.Marshal(res2)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(rawRes2, &wire2); err != nil {
				t.Fatal(err)
			}
			if len(wire2.Shots) != 2 || wire2.Shots[0].AddedCount == nil || *wire2.Shots[0].AddedCount != 0 || len(wire2.Shots[0].References) != 3 || len(wire2.Shots[0].Artifacts) != 2 {
				t.Fatalf("unexpected unchanged s1 wire result: %+v", wire2.Shots)
			}
			if wire2.Shots[1].AddedCount == nil || *wire2.Shots[1].AddedCount != 0 || len(wire2.Shots[1].References) != 1 || len(wire2.Shots[1].Artifacts) != 1 {
				t.Fatalf("unexpected unchanged s2 wire result: %+v", wire2.Shots)
			}
			if afterUnchanged := appendFullState(t, db); afterUnchanged != beforeUnchanged {
				t.Fatalf("unchanged append mutated database state")
			}
		})
	}
}

func TestAppendProjectUnitShotAssetBindingsBatchAtomicityAndRejections(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			svc, db := appendFixture(t, dialect)
			ctx := context.Background()

			preview, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatal(err)
			}
			before := appendFullState(t, db)

			checkNoMutation := func(t *testing.T) {
				t.Helper()
				if after := appendFullState(t, db); after != before {
					t.Fatalf("rejected append mutated database state")
				}
			}

			// 1. Auth & 404 checks
			t.Run("auth_and_not_found", func(t *testing.T) {
				_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "", "p", "unit", BindingAppendRequest{
					Baseline:   preview.Baseline,
					Selections: []BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}, {ShotID: "s2", AssetVersionIDs: []string{"vc1"}}},
				})
				assertAppendAppError(t, err, 401, CodeUnauthorized, ReasonUnauthorized)
				checkNoMutation(t)

				for _, tc := range []struct {
					user, proj, unit string
				}{
					{"u", "missing-p", "unit"},
					{"u", "foreign-p", "unit"},
					{"u", "p", "missing-unit"},
					{"u", "p", "other-p-unit"},
				} {
					b := preview.Baseline
					b.ProjectID = tc.proj
					b.UnitID = tc.unit
					_, err := svc.AppendProjectUnitShotAssetBindings(ctx, tc.user, tc.proj, tc.unit, BindingAppendRequest{
						Baseline:   b,
						Selections: []BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}, {ShotID: "s2", AssetVersionIDs: []string{"vc1"}}},
					})
					assertAppendAppError(t, err, 404, CodeNotFound, ReasonNotFound)
					if !errors.Is(err, gorm.ErrRecordNotFound) {
						t.Fatalf("expected gorm.ErrRecordNotFound in unwrap chain for %+v, got %v", tc, err)
					}
					checkNoMutation(t)
				}
			})

			// 2. Structural 400 checks (multi-shot batch where s1 is valid, s2 or structure is invalid)
			t.Run("invalid_request_400", func(t *testing.T) {
				manyShots := make([]string, 101)
				manySelections := make([]BindingSelection, 101)
				for i := range manyShots {
					id := fmt.Sprintf("shot-%d", i)
					manyShots[i] = id
					manySelections[i] = BindingSelection{ShotID: id}
				}
				bMany := preview.Baseline
				bMany.ShotIDs = manyShots

				bMismatchProj := preview.Baseline
				bMismatchProj.ProjectID = "other-p"

				bMismatchUnit := preview.Baseline
				bMismatchUnit.UnitID = "other-unit"

				bEmptyShots := preview.Baseline
				bEmptyShots.ShotIDs = nil

				cloneBaseline := func() BindingBaseline {
					raw, err := json.Marshal(preview.Baseline)
					if err != nil {
						t.Fatal(err)
					}
					var copyBaseline BindingBaseline
					if err := json.Unmarshal(raw, &copyBaseline); err != nil {
						t.Fatal(err)
					}
					return copyBaseline
				}

				bNegRev := cloneBaseline()
				bNegRev.ProjectRevision = -1

				bNilShots := cloneBaseline()
				bNilShots.Shots = nil

				bNilCatalog := cloneBaseline()
				bNilCatalog.Catalog = nil

				bUnsortedShotIDs := cloneBaseline()
				bUnsortedShotIDs.ShotIDs = []string{"s2", "s1"}
				bUnsortedShotIDs.Shots = []BindingShotBaseline{bUnsortedShotIDs.Shots[1], bUnsortedShotIDs.Shots[0]}

				bMismatchedShotOrder := cloneBaseline()
				bMismatchedShotOrder.ShotIDs = []string{"s1", "s2"}
				bMismatchedShotOrder.Shots = []BindingShotBaseline{bMismatchedShotOrder.Shots[1], bMismatchedShotOrder.Shots[0]}

				bEmptyShotFP := cloneBaseline()
				bEmptyShotFP.Shots[0].ShotFingerprint = ""

				bEmptyRevFP := cloneBaseline()
				bEmptyRevFP.Shots[0].RevisionFingerprint = ""

				bNilRefs := cloneBaseline()
				bNilRefs.Shots[0].References = nil

				bNilArts := cloneBaseline()
				bNilArts.Shots[0].Artifacts = nil

				bEmptyRefID := cloneBaseline()
				bEmptyRefID.Shots[0].References[0].ReferenceID = ""

				bEmptyRefVerID := cloneBaseline()
				bEmptyRefVerID.Shots[0].References[0].AssetVersionID = ""

				bEmptyArtID := cloneBaseline()
				bEmptyArtID.Shots[0].Artifacts[0].ArtifactID = ""

				bEmptyArtFP := cloneBaseline()
				bEmptyArtFP.Shots[0].Artifacts[0].ContentFingerprint = ""

				bUnsortedCatalog := cloneBaseline()
				bUnsortedCatalog.Catalog[0], bUnsortedCatalog.Catalog[1] = bUnsortedCatalog.Catalog[1], bUnsortedCatalog.Catalog[0]

				bEmptyAssetFP := cloneBaseline()
				bEmptyAssetFP.Catalog[0].AssetFingerprint = ""

				bEmptyLinkFP := cloneBaseline()
				bEmptyLinkFP.Catalog[0].LinkFingerprint = ""

				bNilVersions := cloneBaseline()
				bNilVersions.Catalog[0].Versions = nil

				bUnsortedVersions := cloneBaseline()
				bUnsortedVersions.Catalog[0].Versions[0], bUnsortedVersions.Catalog[0].Versions[1] = bUnsortedVersions.Catalog[0].Versions[1], bUnsortedVersions.Catalog[0].Versions[0]

				bEmptyVerFP := cloneBaseline()
				bEmptyVerFP.Catalog[0].Versions[0].VersionFingerprint = ""

				bEmptyMediaFP := cloneBaseline()
				bEmptyMediaFP.Catalog[0].Versions[0].MediaFingerprint = ""

				validSel := []BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}, {ShotID: "s2"}}

				cases := []struct {
					name string
					req  BindingAppendRequest
				}{
					{"empty_shot_ids", BindingAppendRequest{Baseline: bEmptyShots, Selections: []BindingSelection{{ShotID: "s1"}}}},
					{"too_many_shots", BindingAppendRequest{Baseline: bMany, Selections: manySelections}},
					{"baseline_project_mismatch", BindingAppendRequest{Baseline: bMismatchProj, Selections: validSel}},
					{"baseline_unit_mismatch", BindingAppendRequest{Baseline: bMismatchUnit, Selections: validSel}},
					{"missing_selection_for_s2", BindingAppendRequest{Baseline: preview.Baseline, Selections: []BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}}}},
					{"extra_selection_shot", BindingAppendRequest{Baseline: preview.Baseline, Selections: []BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}, {ShotID: "s2"}, {ShotID: "s3"}}}},
					{"conflicting_duplicate_shot_selection", BindingAppendRequest{Baseline: preview.Baseline, Selections: []BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}, {ShotID: "s1", AssetVersionIDs: []string{"vc1"}}, {ShotID: "s2"}}}},
					{"blank_asset_version_id", BindingAppendRequest{Baseline: preview.Baseline, Selections: []BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v1"}}, {ShotID: "s2", AssetVersionIDs: []string{"   "}}}}},
					{"negative_project_revision", BindingAppendRequest{Baseline: bNegRev, Selections: validSel}},
					{"nil_baseline_shots", BindingAppendRequest{Baseline: bNilShots, Selections: validSel}},
					{"nil_baseline_catalog", BindingAppendRequest{Baseline: bNilCatalog, Selections: validSel}},
					{"unsorted_baseline_shot_ids", BindingAppendRequest{Baseline: bUnsortedShotIDs, Selections: validSel}},
					{"mismatched_shot_order", BindingAppendRequest{Baseline: bMismatchedShotOrder, Selections: validSel}},
					{"empty_shot_fingerprint", BindingAppendRequest{Baseline: bEmptyShotFP, Selections: validSel}},
					{"empty_revision_fingerprint", BindingAppendRequest{Baseline: bEmptyRevFP, Selections: validSel}},
					{"nil_shot_references", BindingAppendRequest{Baseline: bNilRefs, Selections: validSel}},
					{"nil_shot_artifacts", BindingAppendRequest{Baseline: bNilArts, Selections: validSel}},
					{"empty_reference_id", BindingAppendRequest{Baseline: bEmptyRefID, Selections: validSel}},
					{"empty_reference_asset_version_id", BindingAppendRequest{Baseline: bEmptyRefVerID, Selections: validSel}},
					{"empty_artifact_id", BindingAppendRequest{Baseline: bEmptyArtID, Selections: validSel}},
					{"empty_artifact_content_fingerprint", BindingAppendRequest{Baseline: bEmptyArtFP, Selections: validSel}},
					{"unsorted_catalog", BindingAppendRequest{Baseline: bUnsortedCatalog, Selections: validSel}},
					{"empty_catalog_asset_fingerprint", BindingAppendRequest{Baseline: bEmptyAssetFP, Selections: validSel}},
					{"empty_catalog_link_fingerprint", BindingAppendRequest{Baseline: bEmptyLinkFP, Selections: validSel}},
					{"nil_catalog_versions", BindingAppendRequest{Baseline: bNilVersions, Selections: validSel}},
					{"unsorted_catalog_versions", BindingAppendRequest{Baseline: bUnsortedVersions, Selections: validSel}},
					{"empty_version_fingerprint", BindingAppendRequest{Baseline: bEmptyVerFP, Selections: validSel}},
					{"empty_media_fingerprint", BindingAppendRequest{Baseline: bEmptyMediaFP, Selections: validSel}},
				}
				for _, tc := range cases {
					t.Run(tc.name, func(t *testing.T) {
						_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", tc.req)
						assertAppendAppError(t, err, 400, CodeInvalidArgument, ReasonStoryboardAssetBindingInvalidRequest)
						checkNoMutation(t)
					})
				}

				t.Run("archived_project_rejected_400", func(t *testing.T) {
					var origProj model.Project
					if err := db.First(&origProj, "id = ?", "p").Error; err != nil {
						t.Fatal(err)
					}
					if err := db.Model(&model.Project{}).Where("id = ?", "p").UpdateColumn("status", model.ProjectStatusArchived).Error; err != nil {
						t.Fatal(err)
					}
					_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
						Baseline:   preview.Baseline,
						Selections: validSel,
					})
					if errRestore := db.Model(&model.Project{}).Where("id = ?", "p").UpdateColumn("status", origProj.Status).Error; errRestore != nil {
						t.Fatal(errRestore)
					}
					assertAppendAppError(t, err, 400, CodeInvalidArgument, ReasonStoryboardAssetBindingInvalidRequest)
					checkNoMutation(t)
				})
			})

			// 3. Invalid reference 422 checks (s1 is valid and would append "v1", s2 has invalid reference -> whole batch rolls back)
			t.Run("invalid_reference_422", func(t *testing.T) {
				badVersionIDs := []struct {
					name      string
					versionID string
				}{
					{"nonexistent_version", "v-does-not-exist"},
					{"deleted_version", "v-deleted-ver"},
					{"archived_version", "v-archived-ver"},
					{"unlinked_asset_version", "v-unlinked"},
					{"foreign_user_version", "v-foreign"},
					{"material_category_version", "v-material"},
					{"archived_asset_version", "v-archived-asset"},
				}
				for _, tc := range badVersionIDs {
					t.Run(tc.name, func(t *testing.T) {
						_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
							Baseline: preview.Baseline,
							Selections: []BindingSelection{
								{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
								{ShotID: "s2", AssetVersionIDs: []string{tc.versionID}},
							},
						})
						assertAppendAppError(t, err, 422, CodeStoryboardAssetBindingInvalidReference, ReasonStoryboardAssetBindingInvalidReference)
						checkNoMutation(t)
					})
				}

				// Admin user must still respect project ownership, project_asset_links, and foreign user ownership!
				t.Run("admin_cannot_bind_unlinked_or_foreign", func(t *testing.T) {
					_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "admin", "p", "unit", BindingAppendRequest{
						Baseline: preview.Baseline,
						Selections: []BindingSelection{
							{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
							{ShotID: "s2", AssetVersionIDs: []string{"v-foreign"}},
						},
					})
					assertAppendAppError(t, err, 404, CodeNotFound, ReasonNotFound)
					checkNoMutation(t)

					// Elevate "u" to admin temporarily and verify admin still cannot bind unlinked or foreign versions on own project
					if err := db.Model(&model.User{}).Where("id = ?", "u").Update("role", model.UserRoleAdmin).Error; err != nil {
						t.Fatal(err)
					}
					for _, badVer := range []string{"v-unlinked", "v-foreign"} {
						_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
							Baseline: preview.Baseline,
							Selections: []BindingSelection{
								{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
								{ShotID: "s2", AssetVersionIDs: []string{badVer}},
							},
						})
						assertAppendAppError(t, err, 422, CodeStoryboardAssetBindingInvalidReference, ReasonStoryboardAssetBindingInvalidReference)
					}
					if err := db.Model(&model.User{}).Where("id = ?", "u").Update("role", model.UserRoleUser).Error; err != nil {
						t.Fatal(err)
					}
					checkNoMutation(t)
				})

				// Cross-unit or cross-project shot in batch -> 422 invalid_reference
				for _, crossShotID := range []string{"cross-unit-shot", "cross-proj-shot"} {
					bCross := preview.Baseline
					bCross.ShotIDs = []string{crossShotID, "s1"}
					bCross.Shots = []BindingShotBaseline{
						{
							ShotID:              crossShotID,
							ShotFingerprint:     "fp-cross-shot",
							RevisionFingerprint: "fp-cross-rev",
							References:          []BindingReference{},
							Artifacts:           []BindingArtifact{},
						},
						preview.Baseline.Shots[0],
					}
					_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
						Baseline: bCross,
						Selections: []BindingSelection{
							{ShotID: crossShotID, AssetVersionIDs: []string{"vc1"}},
							{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
						},
					})
					assertAppendAppError(t, err, 422, CodeStoryboardAssetBindingInvalidReference, ReasonStoryboardAssetBindingInvalidReference)
					checkNoMutation(t)
				}
			})

			// 4. Capacity exceeded 422 checks (s1 is valid, s2 exceeds 6 distinct assetVersionIds)
			t.Run("capacity_exceeded_422", func(t *testing.T) {
				// s2 already has "vb1" (1 existing). Adding 6 new versions ("v1", "vb2", "vb3", "vb4", "vb5", "vb6") -> 7 distinct versions > 6.
				_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
					Baseline: preview.Baseline,
					Selections: []BindingSelection{
						{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
						{ShotID: "s2", AssetVersionIDs: []string{"v1", "vb2", "vb3", "vb4", "vb5", "vb6"}},
					},
				})
				assertAppendAppError(t, err, 422, CodeStoryboardAssetBindingCapacityExceeded, ReasonStoryboardAssetBindingCapacityExceeded)
				checkNoMutation(t)
			})

			// 5. Baseline conflict 409 checks (s1 is valid, baseline is stale/tampered)
			t.Run("baseline_conflict_409", func(t *testing.T) {
				// Missing an existing reference in baseline
				bMissingRef := preview.Baseline
				bMissingRef.Shots = append([]BindingShotBaseline{}, preview.Baseline.Shots...)
				bMissingRef.Shots[0].References = bMissingRef.Shots[0].References[:1]
				_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
					Baseline: bMissingRef,
					Selections: []BindingSelection{
						{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
						{ShotID: "s2", AssetVersionIDs: []string{"vc1"}},
					},
				})
				assertAppendAppError(t, err, 409, CodeConflict, ReasonStoryboardAssetBindingConflict)
				checkNoMutation(t)

				// Stale projectRevision in baseline
				bStaleRev := preview.Baseline
				bStaleRev.ProjectRevision = 16
				_, err = svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
					Baseline: bStaleRev,
					Selections: []BindingSelection{
						{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
						{ShotID: "s2", AssetVersionIDs: []string{"vc1"}},
					},
				})
				assertAppendAppError(t, err, 409, CodeConflict, ReasonStoryboardAssetBindingConflict)
				checkNoMutation(t)
			})

			// 6. Mid-transaction DB failure rollback (inject callback error before ShotAssetReference create on s2)
			t.Run("mid_transaction_db_error_rolls_back_all_shots", func(t *testing.T) {
				cbName := "test_append_fail_second_insert"
				insertCount := 0
				if err := db.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
					if tx.Statement != nil && tx.Statement.Table == "shot_asset_references" {
						insertCount++
						if insertCount >= 2 {
							tx.AddError(errors.New("simulated mid-transaction insert failure"))
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Callback().Create().Remove(cbName) }()

				_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
					Baseline: preview.Baseline,
					Selections: []BindingSelection{
						{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
						{ShotID: "s2", AssetVersionIDs: []string{"vc1"}},
					},
				})
				if err == nil {
					t.Fatal("expected error from mid-transaction failure, got nil")
				}
				checkNoMutation(t)
			})

			// 7. SQLite busy/locked raw error maps to 503 storyboard_asset_binding_busy (Retryable: true)
			t.Run("sqlite_busy_locked_error_maps_to_503", func(t *testing.T) {
				cbName := "test_append_sqlite_locked_503"
				if err := db.Callback().Query().Before("gorm:query").Register(cbName, func(tx *gorm.DB) {
					if tx.Statement != nil && tx.Statement.Table == "shots" {
						tx.AddError(errors.New("database is locked"))
					}
				}); err != nil {
					t.Fatal(err)
				}
				defer func() { _ = db.Callback().Query().Remove(cbName) }()

				_, err := svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
					Baseline: preview.Baseline,
					Selections: []BindingSelection{
						{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
						{ShotID: "s2", AssetVersionIDs: []string{"vc1"}},
					},
				})
				_ = db.Callback().Query().Remove(cbName)
				appErr := assertAppendAppError(t, err, 503, CodeUnavailable, ReasonStoryboardAssetBindingBusy)
				if !appErr.Retryable {
					t.Fatalf("expected Retryable=true on 503 busy error, got %+v", appErr)
				}
				checkNoMutation(t)
			})
		})
	}
}

func TestAppendProjectUnitShotAssetBindingsOwnerScopeIsolationAndHeadlessLocking(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dbs := profileConcurrencyDatabases(t, dialect)
			db := dbs[0]
			if err := database.MigrateSchema(db); err != nil {
				t.Fatal(err)
			}
			svc := &Service{repo: repository.New(db)}
			rows := []any{
				&model.User{ID: "u", Username: "owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.User{ID: "co-owner", Username: "co-owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.User{ID: "res-owner", Username: "res-owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.User{ID: "headless-owner", Username: "headless-owner", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.User{ID: "foreign-u", Username: "foreign-u", Role: model.UserRoleUser, Status: model.UserStatusActive},
				&model.Project{ID: "p", UserID: "u", Name: "proj", Revision: 5},
				&model.Project{ID: "co-p", UserID: "co-owner", Name: "co-proj", Revision: 2},
				&model.Project{ID: "foreign-p", UserID: "foreign-u", Name: "PRIVATE_FOREIGN_PROJECT", Revision: 1},
				&model.ProjectUnit{ID: "unit", ProjectID: "p", Title: "u1"},
				&model.ProjectUnit{ID: "foreign-unit", ProjectID: "foreign-p", Title: "fu1"},
				&model.Shot{ID: "s1", ProjectID: "p", UnitID: "unit", CurrentRevisionID: "rev1", Description: "角色：阿青", Title: "s1", Status: "draft", Position: 1},
				&model.Shot{ID: "foreign-shot", ProjectID: "foreign-p", UnitID: "foreign-unit", CurrentRevisionID: "rev-f", Description: "foreign", Title: "fs", Status: "draft", Position: 1},
				&model.ShotRevision{ID: "rev1", ShotID: "s1", Version: 1, ActionBeatsJSON: `[]`},
				&model.ShotRevision{ID: "rev-f", ShotID: "foreign-shot", Version: 1, ActionBeatsJSON: `[]`},
				&model.Asset{ID: "a", UserID: "u", Category: model.AssetCategoryCharacter, Title: "阿青", Status: "draft", PrimaryVersionID: "v1", PayloadJSON: `{}`},
				&model.Asset{ID: "headless-a", UserID: "headless-owner", Category: model.AssetCategoryProp, Title: "无版本资产", Status: "draft", PayloadJSON: `{}`},
				&model.Asset{ID: "foreign-a", UserID: "foreign-u", Category: model.AssetCategoryCharacter, Title: "PRIVATE_FOREIGN_TITLE", Status: "draft", PrimaryVersionID: "v-foreign", PayloadJSON: `{}`},
				&model.ProjectAssetLink{ID: "link-a", ProjectID: "p", AssetID: "a"},
				&model.ProjectAssetLink{ID: "link-co-a", ProjectID: "co-p", AssetID: "a"},
				&model.ProjectAssetLink{ID: "link-headless", ProjectID: "p", AssetID: "headless-a"},
				&model.Resource{ID: "res1", UserID: "u", Kind: "image", Status: model.ResourceStatusReady, Width: 100, Height: 100, MimeType: "image/png", ObjectKey: "u/res1.png"},
				&model.Resource{ID: "res-rep", UserID: "res-owner", Kind: "image", Status: model.ResourceStatusReady, Width: 100, Height: 100, MimeType: "image/png", ObjectKey: "res-owner/rep.png"},
				&model.Resource{ID: "res-foreign", UserID: "foreign-u", Kind: "image", Status: model.ResourceStatusReady, Width: 100, Height: 100, MimeType: "image/png", ObjectKey: "PRIVATE_OBJECT_KEY"},
				&model.AssetVersion{ID: "v1", AssetID: "a", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res1"}}`},
				&model.AssetVersion{ID: "v-foreign", AssetID: "foreign-a", Version: 1, Status: "draft", DefinitionJSON: `{"referenceImage":{"resourceId":"res-foreign"}}`},
				&model.AssetRepresentation{ID: "rep1", AssetVersionID: "v1", ResourceID: "res-rep", Role: "primary", MediaType: "image", MetadataJSON: `{}`},
				&model.ShotAssetReference{ID: "ref-foreign", ShotID: "foreign-shot", AssetVersionID: "v-foreign", Role: "character", Status: "confirmed"},
			}
			for _, row := range rows {
				if err := db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}

			repo := repository.New(db)
			owners, err := repo.DiscoverStoryboardBindingAppendOwners("p", []string{"foreign-shot", "s1"}, []string{"v1", "v-foreign"})
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains(owners, "foreign-u") {
				t.Fatalf("DiscoverStoryboardBindingAppendOwners leaked foreign owner 'foreign-u' from untrusted shotId/versionId: %v", owners)
			}
			for _, wantOwner := range []string{"u", "co-owner", "res-owner", "headless-owner"} {
				if !slices.Contains(owners, wantOwner) {
					t.Fatalf("DiscoverStoryboardBindingAppendOwners missed required owner %q in %v", wantOwner, owners)
				}
			}

			if dialect == "postgres" {
				// Verify LockStoryboardBindingAppendScope locks headless-a and never locks foreign rows
				lockedAssets := []string{}
				lockedVersions := []string{}
				lockedShots := []string{}
				cbName := "verify_append_scope_for_update"
				if err := db.Callback().Query().After("gorm:query").Register(cbName, func(tx *gorm.DB) {
					if _, ok := tx.Statement.Clauses["FOR"]; !ok {
						return
					}
					switch dest := tx.Statement.Dest.(type) {
					case *[]model.Asset:
						for _, row := range *dest {
							lockedAssets = append(lockedAssets, row.ID)
						}
					case *[]model.AssetVersion:
						for _, row := range *dest {
							lockedVersions = append(lockedVersions, row.ID)
						}
					case *[]model.Shot:
						for _, row := range *dest {
							lockedShots = append(lockedShots, row.ID)
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
				err = repo.WithStoryboardAssetOwners(context.Background(), owners, func(tx *repository.Repository) error {
					return tx.LockStoryboardBindingAppendScope("p", "unit", []string{"foreign-shot", "s1"}, []string{"v1", "v-foreign"})
				})
				_ = db.Callback().Query().Remove(cbName)
				if err != nil {
					t.Fatalf("LockStoryboardBindingAppendScope failed: %v", err)
				}
				if !slices.Contains(lockedAssets, "headless-a") {
					t.Fatalf("expected headless linked asset 'headless-a' to be locked, got lockedAssets=%v", lockedAssets)
				}
				if slices.Contains(lockedAssets, "foreign-a") || slices.Contains(lockedVersions, "v-foreign") || slices.Contains(lockedShots, "foreign-shot") {
					t.Fatalf("LockStoryboardBindingAppendScope locked foreign rows: assets=%v versions=%v shots=%v", lockedAssets, lockedVersions, lockedShots)
				}

				// Verify on PostgreSQL that when foreign-u holds advisory lock + FOR UPDATE on foreign rows in dbs[1],
				// AppendProjectUnitShotAssetBindings on dbs[0] does NOT block and immediately returns 422 invalid_reference!
				preview, err := svc.PreviewProjectUnitShotAssetBindings(context.Background(), "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1"}})
				if err != nil {
					t.Fatal(err)
				}

				foreignLocked := make(chan struct{})
				releaseForeign := make(chan struct{})
				foreignDone := make(chan error, 1)
				go func() {
					foreignDone <- dbs[1].Transaction(func(ftx *gorm.DB) error {
						sum := sha256.Sum256([]byte("storyboard-assets:foreign-u"))
						key := int64(binary.BigEndian.Uint64(sum[:8]))
						if err := ftx.Exec("SELECT pg_advisory_xact_lock(?)", key).Error; err != nil {
							return err
						}
						var fShot model.Shot
						if err := ftx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fShot, "id = ?", "foreign-shot").Error; err != nil {
							return err
						}
						var fVer model.AssetVersion
						if err := ftx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&fVer, "id = ?", "v-foreign").Error; err != nil {
							return err
						}
						close(foreignLocked)
						<-releaseForeign
						return nil
					})
				}()
				<-foreignLocked
				defer func() {
					close(releaseForeign)
					if err := <-foreignDone; err != nil {
						t.Errorf("foreign lock tx error: %v", err)
					}
				}()

				shortCtx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
				defer cancel()
				_, err = svc.AppendProjectUnitShotAssetBindings(shortCtx, "u", "p", "unit", BindingAppendRequest{
					Baseline:   preview.Baseline,
					Selections: []BindingSelection{{ShotID: "s1", AssetVersionIDs: []string{"v-foreign"}}},
				})
				assertAppendAppError(t, err, 422, CodeStoryboardAssetBindingInvalidReference, ReasonStoryboardAssetBindingInvalidReference)

				bForeignShot := preview.Baseline
				bForeignShot.ShotIDs = []string{"foreign-shot", "s1"}
				bForeignShot.Shots = []BindingShotBaseline{
					{
						ShotID:              "foreign-shot",
						ShotFingerprint:     "fp-foreign-shot",
						RevisionFingerprint: "fp-foreign-rev",
						References:          []BindingReference{},
						Artifacts:           []BindingArtifact{},
					},
					preview.Baseline.Shots[0],
				}
				shortCtx2, cancel2 := context.WithTimeout(context.Background(), 600*time.Millisecond)
				defer cancel2()
				_, err = svc.AppendProjectUnitShotAssetBindings(shortCtx2, "u", "p", "unit", BindingAppendRequest{
					Baseline: bForeignShot,
					Selections: []BindingSelection{
						{ShotID: "foreign-shot", AssetVersionIDs: []string{"v1"}},
						{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
					},
				})
				assertAppendAppError(t, err, 422, CodeStoryboardAssetBindingInvalidReference, ReasonStoryboardAssetBindingInvalidReference)
			}
		})
	}
}

func TestAppendProjectUnitShotAssetBindingsConcurrentDriftReturns409Before422(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			svc, db := appendFixture(t, dialect)
			ctx := context.Background()

			preview, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatal(err)
			}

			// Case A1: Selected version "v1" transitions to "deleting" after preview -> stale baseline must return 409 conflict, fresh baseline returns 422 invalid_reference.
			if err := db.Model(&model.AssetVersion{}).Where("id = ?", "v1").UpdateColumn("status", "deleting").Error; err != nil {
				t.Fatal(err)
			}
			_, err = svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
				Baseline: preview.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
					{ShotID: "s2", AssetVersionIDs: []string{}},
				},
			})
			assertAppendAppError(t, err, 409, CodeConflict, ReasonStoryboardAssetBindingConflict)

			freshPreviewDeleting, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
				Baseline: freshPreviewDeleting.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
					{ShotID: "s2", AssetVersionIDs: []string{}},
				},
			})
			assertAppendAppError(t, err, 422, CodeStoryboardAssetBindingInvalidReference, ReasonStoryboardAssetBindingInvalidReference)

			if err := db.Model(&model.AssetVersion{}).Where("id = ?", "v1").UpdateColumn("status", "draft").Error; err != nil {
				t.Fatal(err)
			}

			// Case A2: Asset "a" is unlinked from project "p" after preview -> stale baseline must return 409 conflict, fresh baseline returns 422 invalid_reference.
			if err := db.Delete(&model.ProjectAssetLink{}, "id = ?", "link-a").Error; err != nil {
				t.Fatal(err)
			}
			_, err = svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
				Baseline: preview.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
					{ShotID: "s2", AssetVersionIDs: []string{}},
				},
			})
			assertAppendAppError(t, err, 409, CodeConflict, ReasonStoryboardAssetBindingConflict)

			freshPreviewUnlinked, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
				Baseline: freshPreviewUnlinked.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{"v1"}},
					{ShotID: "s2", AssetVersionIDs: []string{}},
				},
			})
			assertAppendAppError(t, err, 422, CodeStoryboardAssetBindingInvalidReference, ReasonStoryboardAssetBindingInvalidReference)

			if err := db.Create(&model.ProjectAssetLink{ID: "link-a", ProjectID: "p", AssetID: "a"}).Error; err != nil {
				t.Fatal(err)
			}

			// Case B: Shot s2 has 5 references at preview time; a 6th reference is concurrently added before append.
			// Selecting 1 more version ("vc1") with the stale 5-ref baseline must return 409 conflict (not 422 capacity_exceeded);
			// with a fresh 6-ref baseline it must return 422 capacity_exceeded.
			for i, vid := range []string{"vb2", "vb3", "vb4", "vb5"} {
				if err := db.Create(&model.ShotAssetReference{
					ID:             fmt.Sprintf("r-s2-extra-%d", i),
					ShotID:         "s2",
					AssetVersionID: vid,
					Role:           "prop",
					Status:         "confirmed",
				}).Error; err != nil {
					t.Fatal(err)
				}
			}
			preview5, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatal(err)
			}
			if err := db.Create(&model.ShotAssetReference{
				ID:             "r-s2-extra-6th",
				ShotID:         "s2",
				AssetVersionID: "vb6",
				Role:           "prop",
				Status:         "confirmed",
			}).Error; err != nil {
				t.Fatal(err)
			}

			_, err = svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
				Baseline: preview5.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{}},
					{ShotID: "s2", AssetVersionIDs: []string{"vc1"}},
				},
			})
			assertAppendAppError(t, err, 409, CodeConflict, ReasonStoryboardAssetBindingConflict)

			preview6, err := svc.PreviewProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingPreviewRequest{ShotIDs: []string{"s1", "s2"}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = svc.AppendProjectUnitShotAssetBindings(ctx, "u", "p", "unit", BindingAppendRequest{
				Baseline: preview6.Baseline,
				Selections: []BindingSelection{
					{ShotID: "s1", AssetVersionIDs: []string{}},
					{ShotID: "s2", AssetVersionIDs: []string{"vc1"}},
				},
			})
			assertAppendAppError(t, err, 422, CodeStoryboardAssetBindingCapacityExceeded, ReasonStoryboardAssetBindingCapacityExceeded)
		})
	}
}
