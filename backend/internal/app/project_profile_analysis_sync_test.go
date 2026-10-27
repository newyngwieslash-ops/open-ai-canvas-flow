package app

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

// A metadata-only edit from an older tab must not undo a successful adoption.
func TestProfileAnalysisSyncRejectsStaleAssetHead(t *testing.T) {
	for _, entry := range []string{"upsert", "replace"} {
		for _, head := range []string{"stale", "missing"} {
			t.Run(entry+"/"+head, func(t *testing.T) {
				s, db, projectID, asset, task := profileAdoptFixture(t)
				s.dataDir = t.TempDir()
				if err := db.AutoMigrate(&model.UserDailyActivity{}); err != nil {
					t.Fatal(err)
				}
				if err := db.Create(&model.AssetRepresentation{ID: newID(), AssetVersionID: asset.PrimaryVersionID, TaskID: "original-media-task", ResourceID: "image-1", MediaType: "image", Role: "primary"}).Error; err != nil {
					t.Fatal(err)
				}
				old, err := s.UserAsset("user-1", asset.ID)
				if err != nil {
					t.Fatal(err)
				}
				request := AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "sync-head-adopt-0001"}
				adopted, err := s.AdoptProjectProfileAnalysis("user-1", projectID, asset.ID, task.ID, request)
				if err != nil || adopted.AdoptedVersionID == asset.PrimaryVersionID {
					t.Fatalf("real adoption did not advance head: %+v %v", adopted, err)
				}
				before := profileMatrixSnapshot(t, db)
				var payload map[string]json.RawMessage
				if err := json.Unmarshal(old, &payload); err != nil {
					t.Fatal(err)
				}
				payload["title"] = json.RawMessage(`"edited in old tab"`)
				if head == "missing" {
					delete(payload, "primaryVersionId")
				}
				raw, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				if entry == "replace" {
					all, loadErr := s.UserAssets("user-1")
					if loadErr != nil {
						t.Fatal(loadErr)
					}
					for i, item := range all {
						var identity struct {
							ID string `json:"id"`
						}
						if err := json.Unmarshal(item, &identity); err != nil {
							t.Fatal(err)
						}
						if identity.ID == asset.ID {
							all[i] = raw
						}
					}
					_, err = s.ReplaceUserAssets("user-1", AssetsSyncRequest{Assets: all})
				} else {
					_, err = s.UpsertUserAsset("user-1", raw)
				}
				requireVersionConflict(t, err)
				if !errors.Is(err, repository.ErrProjectAssetVersionChanged) {
					t.Fatalf("version conflict lost repository cause: %v", err)
				}
				if after := profileMatrixSnapshot(t, db); !reflect.DeepEqual(after, before) {
					t.Fatal("rejected sync changed head, version, media, receipt or project revision")
				}
				replay, err := s.AdoptProjectProfileAnalysis("user-1", projectID, asset.ID, task.ID, request)
				if err != nil || !replay.Replayed || replay.AdoptedVersionID != adopted.AdoptedVersionID || replay.Asset.PrimaryVersionID != adopted.AdoptedVersionID {
					t.Fatalf("sync damaged adoption replay/current state: %+v %v", replay, err)
				}
			})
		}
	}
}

func TestProfileAnalysisSyncAllowsCurrentHeadAndUnversionedMetadata(t *testing.T) {
	s, db, projectID, asset, task := profileAdoptFixture(t)
	s.dataDir = t.TempDir()
	if err := db.AutoMigrate(&model.UserDailyActivity{}); err != nil {
		t.Fatal(err)
	}
	adopted, err := s.AdoptProjectProfileAnalysis("user-1", projectID, asset.ID, task.ID, AdoptProfileAnalysisRequest{asset.PrimaryVersionID, []string{"description"}, "sync-head-adopt-0001"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := s.UserAsset("user-1", asset.ID)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(current, &payload); err != nil {
		t.Fatal(err)
	}
	payload["title"] = json.RawMessage(`"current title"`)
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	before := profileMatrixSnapshot(t, db)
	if _, err := s.UpsertUserAsset("user-1", raw); err != nil {
		t.Fatalf("current head metadata rejected: %v", err)
	}
	stored, err := s.repo.AssetForUser("user-1", asset.ID)
	if err != nil || stored.Title != "current title" || stored.PrimaryVersionID != adopted.AdoptedVersionID {
		t.Fatalf("current metadata not saved: %+v %v", stored, err)
	}
	after := profileMatrixSnapshot(t, db)
	delete(before, "assets")
	delete(after, "assets")
	if !reflect.DeepEqual(after, before) {
		t.Fatal("metadata edit changed version, media, receipt or project revision")
	}
	unversioned := json.RawMessage(`{"id":"sync-unversioned","kind":"text","title":"original","coverUrl":"","tags":[],"data":{"content":"keep"}}`)
	if _, err := s.UpsertUserAsset("user-1", unversioned); err != nil {
		t.Fatalf("new unversioned asset rejected: %v", err)
	}
	unversioned = json.RawMessage(`{"id":"sync-unversioned","kind":"text","title":"updated","coverUrl":"","tags":[],"data":{"content":"keep"}}`)
	if _, err := s.UpsertUserAsset("user-1", unversioned); err != nil {
		t.Fatalf("unversioned metadata rejected: %v", err)
	}
	stored, err = s.repo.AssetForUser("user-1", "sync-unversioned")
	if err != nil || stored.Title != "updated" || stored.PrimaryVersionID != "" {
		t.Fatalf("unversioned metadata not saved: %+v %v", stored, err)
	}
}
