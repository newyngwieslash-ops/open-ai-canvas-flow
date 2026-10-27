package repository

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
)

func storyboardUTC(state storyboardWriterState) storyboardWriterState {
	value := reflect.ValueOf(&state).Elem()
	for i := 0; i < value.NumField(); i++ {
		rows := value.Field(i)
		copy := reflect.MakeSlice(rows.Type(), rows.Len(), rows.Len())
		reflect.Copy(copy, rows)
		rows.Set(copy)
		for j := 0; j < rows.Len(); j++ {
			row := rows.Index(j)
			for k := 0; k < row.NumField(); k++ {
				if stamp, ok := row.Field(k).Interface().(time.Time); ok {
					row.Field(k).Set(reflect.ValueOf(stamp.UTC()))
				}
			}
		}
	}
	return state
}

func TestStoryboardAssetConcurrencyLateDomainPreservesHead(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			a, b := storyboardDatabases(t, dialect)
			if err := database.MigrateSchema(a.db); err != nil {
				t.Fatal(err)
			}
			asset := model.Asset{ID: "a", UserID: "owner", Kind: "entity", Category: model.AssetCategoryCharacter, Status: "draft", PrimaryVersionID: "v1", PayloadJSON: "{}"}
			for _, row := range []any{&asset, &model.AssetVersion{ID: "v1", AssetID: "a", Version: 1, DefinitionJSON: "{}"}, &model.ProjectAssetLink{ID: "l", ProjectID: "p", AssetID: "a"}} {
				if err := a.db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			stale := asset
			asset.PrimaryVersionID = "v2"
			asset.Status = model.AssetVersionStatusConfirmed
			if err := a.SaveCharacterVersion("p", &asset, &model.AssetVersion{ID: "v2", AssetID: "a", Version: 2, DefinitionJSON: "{}"}, nil, nil, "v1"); err != nil {
				t.Fatal(err)
			}
			want := storyboardWriterSnapshot(t, b)
			stale.Category = model.AssetCategoryEnvironment
			stale.UpdatedAt = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
			started := time.Now().Add(-time.Microsecond)
			if err := b.UpdateAssetDomain(&stale); err != nil {
				t.Fatal(err)
			}
			got := storyboardWriterSnapshot(t, a)
			want.Assets[0].Category = model.AssetCategoryEnvironment
			want.Assets[0].UpdatedAt = stale.UpdatedAt
			want.Projects[0].Revision++
			if at := got.Projects[0].UpdatedAt; at.Before(started) || at.After(time.Now().Add(time.Microsecond)) {
				t.Fatal("project timestamp outside category write")
			}
			want.Projects[0].UpdatedAt = got.Projects[0].UpdatedAt
			if !reflect.DeepEqual(storyboardUTC(want), storyboardUTC(got)) {
				t.Fatalf("late category overwrote committed head/status or other rows: asset=%+v", got.Assets[0])
			}
		})
	}
}

func TestStoryboardAssetConcurrencyLateHeadlessLink(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, initial := range []bool{false, true} {
				t.Run(map[bool]string{false: "link_only", true: "initial_version"}[initial], func(t *testing.T) {
					a, b := storyboardDatabases(t, dialect)
					if err := database.MigrateSchema(a.db); err != nil {
						t.Fatal(err)
					}
					asset := model.Asset{ID: "a", UserID: "owner", Category: model.AssetCategoryCharacter, Status: "draft", PayloadJSON: "{}"}
					if err := a.db.Create(&asset).Error; err != nil {
						t.Fatal(err)
					}
					stale := asset
					asset.Category = model.AssetCategoryEnvironment
					if err := a.UpdateAssetDomain(&asset); err != nil {
						t.Fatal(err)
					}
					asset.Status = model.AssetVersionStatusConfirmed
					if err := a.UpsertAsset(&asset); err != nil {
						t.Fatal(err)
					}
					var version *model.AssetVersion
					if initial {
						version = &model.AssetVersion{ID: "v1", AssetID: "a", Version: 1, Status: model.AssetVersionStatusConfirmed, DefinitionJSON: "{}"}
						stale.PrimaryVersionID = "v1"
					}
					want := storyboardWriterSnapshot(t, a)
					started := time.Now().Add(-time.Microsecond)
					if _, err := b.LinkProjectAsset(&stale, version, &model.ProjectAssetLink{ID: "l", ProjectID: "p", AssetID: "a"}); err != nil {
						t.Fatal(err)
					}
					got := storyboardWriterSnapshot(t, a)
					want.Projects[0].Revision++
					if at := got.Projects[0].UpdatedAt; at.Before(started) || at.After(time.Now().Add(time.Microsecond)) {
						t.Fatal("project timestamp outside link write")
					}
					want.Projects[0].UpdatedAt = got.Projects[0].UpdatedAt
					if len(got.Links) != 1 || got.Links[0].ID != "l" || got.Links[0].ProjectID != "p" || got.Links[0].AssetID != "a" {
						t.Fatal("link not created")
					}
					want.Links = got.Links
					if initial {
						if len(got.Versions) != 1 || got.Versions[0].ID != "v1" || got.Versions[0].AssetID != "a" {
							t.Fatal("initial version missing")
						}
						want.Versions = got.Versions
						want.Assets[0].PrimaryVersionID = "v1"
					}
					if !reflect.DeepEqual(want, got) {
						t.Fatalf("late headless link overwrote current identity/status: %+v", got.Assets[0])
					}
				})
			}
		})
	}
}

func TestStoryboardAssetConcurrencyLateStepPreservesOutput(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			a, b := storyboardDatabases(t, dialect)
			if err := database.MigrateSchema(a.db); err != nil {
				t.Fatal(err)
			}
			instance := model.WorkflowInstance{ID: "wf", ProjectID: "p", UnitID: "u", Revision: 1}
			step := model.WorkflowStepInstance{ID: "step", WorkflowInstanceID: "wf", Status: model.WorkflowStepStatusRunning, OutputJSON: "{}"}
			for _, row := range []any{&model.ProjectUnit{ID: "u", ProjectID: "p"}, &model.Shot{ID: "s", ProjectID: "p", UnitID: "u", CurrentRevisionID: "r"}, &model.ShotRevision{ID: "r", ShotID: "s"}, &instance, &step} {
				if err := a.db.Create(row).Error; err != nil {
					t.Fatal(err)
				}
			}
			stale := step
			instance.Revision = 2
			step.Status = model.WorkflowStepStatusCompleted
			step.OutputJSON = `{"result":"committed"}`
			if err := a.RegisterWorkflowTaskOutput(&step, nil, &instance, "p", &model.WorkflowStepTask{ID: "tl", WorkflowStepID: "step", TaskID: "task"}, nil, nil, &model.ShotArtifact{ID: "out", ProjectID: "p", UnitID: "u", ShotID: "s", RevisionID: "r", TaskID: "task", Type: "image", Status: "ready", Selected: true}); err != nil {
				t.Fatal(err)
			}
			before := storyboardWriterSnapshot(t, b)
			if len(before.Steps) != 1 || before.Steps[0].Status != model.WorkflowStepStatusCompleted || before.Steps[0].OutputJSON != `{"result":"committed"}` || len(before.Artifacts) != 1 || before.Artifacts[0].ID != "out" || len(before.Tasks) != 1 || before.Tasks[0].TaskID != "task" {
				t.Fatal("output writer did not commit full fixture")
			}
			stale.Status = model.WorkflowStepStatusFailed
			stale.Error = "late failure"
			if err := b.UpdateWorkflowProgress(&stale, nil, &instance, "p"); !errors.Is(err, gorm.ErrInvalidData) {
				t.Errorf("late step did not reject stale revision: %v", err)
			}
			if after := storyboardWriterSnapshot(t, b); !reflect.DeepEqual(before, after) {
				t.Error("late step changed full workflow/artifact/task state")
			}
		})
	}
}
