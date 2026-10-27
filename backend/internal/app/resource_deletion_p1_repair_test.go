package app

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestDeletionP1NestedAppearance(t *testing.T) {
	for _, key := range []string{"live2dResourceId", "avatarBundleResourceId"} {
		for _, phase := range []string{"existing", "after_prepare"} {
			t.Run(key+"/"+phase, func(t *testing.T) {
				svc, _, _ := newResourceDeletionTestService(t)
				row := model.Resource{ID: "nested-reference", UserID: "owner", Provider: "local", ObjectKey: "nested", Status: model.ResourceStatusReady}
				path := writeClosedDeletionFixtureResource(t, svc, &row)
				setting := model.SystemSetting{Key: "appearance", ValueJSON: fmt.Sprintf(`{"canvas":{"%s":%q}}`, key, row.ID)}
				if phase == "existing" {
					if err := svc.repo.SaveSystemSetting(&setting); err != nil {
						t.Fatal(err)
					}
					var referenced *repository.ResourceDeletionReferencedError
					if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); !errors.As(err, &referenced) || len(referenced.References) == 0 {
						t.Fatalf("nested appearance reference not protected: %v", err)
					}
				} else {
					if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); err != nil {
						t.Fatal(err)
					}
					job, err := svc.repo.ClaimNextResourceDeletionJob("prepare", time.Minute)
					if err != nil || job == nil {
						t.Fatalf("claim %v %v", job, err)
					}
					if _, _, err := svc.repo.PrepareResourceDeletion(job); err != nil {
						t.Fatal(err)
					}
					if err := svc.repo.SaveSystemSetting(&setting); err == nil {
						t.Fatal("nested appearance reference crossed prepared deletion fence")
					}
				}
				if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
					t.Fatalf("appearance admission changed bytes: %q %v", data, err)
				}
			})
		}
	}
}

func writeP1Playback(t *testing.T, dir, id string) string {
	t.Helper()
	stsd := stsdBoxWithFourcc("avc1")
	moov := make([]byte, 8+len(stsd))
	binary.BigEndian.PutUint32(moov[:4], uint32(len(moov)))
	copy(moov[4:8], "moov")
	copy(moov[8:], stsd)
	path := filepath.Join(dir, "playback", id+".mp4")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, moov, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDeletionP1LegacyProcessingPlaybackAlias(t *testing.T) {
	for _, phase := range []string{"before_staging", "after_staging", "unshared"} {
		t.Run(phase, func(t *testing.T) {
			svc, db, dir := newResourceDeletionTestService(t)
			a := model.Resource{ID: "processing-a", UserID: "owner", Provider: "local", ObjectKey: "a-original", Status: model.ResourceStatusReady}
			original := writeClosedDeletionFixtureResource(t, svc, &a)
			if err := db.Model(&a).Update("playback_status", model.PlaybackStatusProcessing).Error; err != nil {
				t.Fatal(err)
			}
			b := model.Resource{ID: "live-b", UserID: "owner", Provider: "local", ObjectKey: "b-original", Status: model.ResourceStatusReady, PlaybackStatus: model.PlaybackStatusReady, PlaybackObjectKey: a.ID + ".mp4"}
			if phase == "before_staging" {
				if err := db.Create(&b).Error; err != nil {
					t.Fatal(err)
				}
			}
			path := writeP1Playback(t, dir, a.ID)
			playback, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.repo.DeleteDetachedResources([]model.Resource{a}, nil); !errors.Is(err, repository.ErrResourceProducerUnsettled) {
				t.Fatalf("legacy processing must remain unresolved: %v", err)
			}
			if phase == "after_staging" {
				// Admission was blocked; the legacy processing row still reserves its derived key.
				if err := svc.repo.CreateResource(&b); !errors.Is(err, repository.ErrResourceDeleting) {
					t.Fatalf("late alias crossed processing reservation: %v", err)
				}
			}
			svc.runPlaybackTranscode(a.UserID, a.ID, "unused")
			if err := svc.repo.CompletePlaybackTranscode(a.UserID, a.ID, model.PlaybackStatusReady, ""); err != nil && !errors.Is(err, repository.ErrResourceProducerUnsettled) {
				t.Fatalf("unexpected no-token completion: %v", err)
			}
			svc.drainResourceDeletionJobs(8)
			if data, err := os.ReadFile(path); err != nil || string(data) != string(playback) {
				t.Fatalf("legacy playback bytes changed: %q %v", data, err)
			}
			if data, err := os.ReadFile(original); err != nil || string(data) != "original" {
				t.Fatalf("legacy original bytes changed: %q %v", data, err)
			}
			stored, err := svc.repo.Resource(a.ID)
			if err != nil || stored.Status != model.ResourceStatusReady || stored.PlaybackStatus != model.PlaybackStatusProcessing || stored.PlaybackObjectKey != "" {
				t.Fatalf("no-token invocation changed legacy state: %+v %v", stored, err)
			}
			for _, table := range []any{&model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}, &model.ResourceDeletionJob{}} {
				assertModelCount(t, db, table, "1 = 1", 0)
			}
			assertModelCount(t, db, &model.ResourceProducerOperation{}, "resource_id = ? AND variant = ?", 0, a.ID, "playback")
			if phase == "before_staging" {
				assertModelCount(t, db, &model.Resource{}, "id = ? AND status = ? AND playback_object_key = ?", 1, b.ID, model.ResourceStatusReady, b.PlaybackObjectKey)
			}
		})
	}
}

func TestDeletionP1PlaybackClaimReservesIdentity(t *testing.T) {
	svc, _, dir := newResourceDeletionTestService(t)
	a := model.Resource{ID: "claim-a", UserID: "owner", Provider: "local", ObjectKey: "a", Status: model.ResourceStatusReady}
	writeClosedDeletionFixtureResource(t, svc, &a)
	op, err := svc.repo.ClaimPlaybackProduction(a.ID)
	if err != nil || op == nil {
		t.Fatalf("claim %v %v", op, err)
	}
	stored, err := svc.repo.Resource(a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.PlaybackObjectKey != a.ID+".mp4" {
		t.Errorf("claim failed to reserve key: %q", stored.PlaybackObjectKey)
	}
	b := model.Resource{ID: "claim-b", UserID: "owner", Provider: "local", ObjectKey: "b", Status: model.ResourceStatusReady, PlaybackObjectKey: a.ID + ".mp4"}
	if err := svc.repo.CreateResource(&b); !errors.Is(err, repository.ErrResourceDeleting) {
		t.Fatalf("live producer identity admitted an alias: %v", err)
	}
	svc.runPlaybackTranscode(a.UserID, a.ID, "unused")
	stored, err = svc.repo.Resource(a.ID)
	if err != nil || stored.PlaybackStatus != model.PlaybackStatusProcessing {
		t.Fatalf("no-token invocation published playback: %+v %v", stored, err)
	}
	if err := svc.repo.StartResourceProduction(op); err != nil {
		t.Fatal(err)
	}
	writeP1Playback(t, dir, a.ID)
	if err := svc.repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CompletePlaybackTranscode(a.UserID, a.ID, model.PlaybackStatusReady, "", op); err != nil {
		t.Fatal(err)
	}
	stored, err = svc.repo.Resource(a.ID)
	if err != nil || stored.PlaybackStatus != model.PlaybackStatusReady {
		t.Fatalf("ordinary playback failed: %+v %v", stored, err)
	}
}

func TestDeletionP1AppearanceExactSchema(t *testing.T) {
	ids, err := repository.AppearanceResourceIDs(`{"canvas":{"live2dResourceId":"model","avatarBundleResourceId":"bundle"},"metadata":{"live2dResourceId":"not-real"}}`)
	if err != nil || len(ids) != 2 || ids[0] != "bundle" || ids[1] != "model" {
		t.Fatalf("exact extraction = %v %v", ids, err)
	}
}

func TestDeletionP1TerminalResultsRemainReferences(t *testing.T) {
	for _, entry := range []string{"service", "repository"} {
		t.Run(entry, func(t *testing.T) {
			svc, db, _ := newResourceDeletionTestService(t)
			row := model.Resource{ID: "terminal-resource", UserID: "owner", Provider: "local", ObjectKey: "terminal", Status: model.ResourceStatusReady}
			asset := model.Asset{ID: "terminal-asset", UserID: "owner", PayloadJSON: `{"resourceId":"terminal-resource"}`}
			task := model.Task{ID: "terminal-task", UserID: "owner", Status: model.TaskStatusSucceeded, InputJSON: `{}`, ResultJSON: `{"resourceId":"terminal-resource"}`, Prompt: "retained terminal result"}
			for _, v := range []any{&row, &asset, &task} {
				if err := db.Create(v).Error; err != nil {
					t.Fatal(err)
				}
			}
			var err error
			if entry == "service" {
				err = svc.PurgeUserAsset(asset.UserID, asset.ID)
			} else {
				err = svc.repo.DeleteAsset(asset.UserID, asset.ID)
			}
			if err == nil {
				t.Fatal("terminal task business reference ignored")
			}
		})
	}
}

func TestDeletionP1PlaybackClaimRejectsPreexistingAlias(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	a := model.Resource{ID: "preclaim-a", UserID: "owner", Provider: "local", ObjectKey: "a", Status: model.ResourceStatusReady}
	b := model.Resource{ID: "preclaim-b", UserID: "owner", Provider: "local", ObjectKey: "b", PlaybackObjectKey: a.ID + ".mp4", Status: model.ResourceStatusReady}
	for _, row := range []any{&a, &b} {
		if err := db.Create(row).Error; err != nil {
			t.Fatal(err)
		}
	}
	path := writeP1Playback(t, dir, a.ID)
	if claimed, _ := svc.repo.ClaimPlaybackTranscode(a.ID); claimed {
		t.Fatal("producer allowed to overwrite existing alias")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestDeletionP1PlaybackLedgerRetry(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	a := model.Resource{ID: "retry-playback", UserID: "owner", Provider: "local", ObjectKey: "original", Status: model.ResourceStatusReady}
	writeClosedDeletionFixtureResource(t, svc, &a)
	op, err := svc.repo.ClaimPlaybackProduction(a.ID)
	if err != nil || op == nil {
		t.Fatalf("claim %v %v", op, err)
	}
	if err := svc.repo.StartResourceProduction(op); err != nil {
		t.Fatal(err)
	}
	path := writeP1Playback(t, dir, a.ID)
	if err := svc.repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CompletePlaybackTranscode(a.UserID, a.ID, model.PlaybackStatusReady, "", op); err != nil {
		t.Fatal(err)
	}
	// Inject a physical deletion fault only after the synchronous writer closed.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DeleteDetachedResources([]model.Resource{a}, nil); err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	if _, err := svc.repo.Resource(a.ID); err != nil {
		t.Fatalf("partial playback failure lost resource: %v", err)
	}
	var object model.ResourceDeletionObject
	if err := db.Where("variant = ?", "playback").First(&object).Error; err != nil {
		t.Fatal(err)
	}
	if object.Status != "pending" {
		t.Fatalf("failed object status=%s", object.Status)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ResourceDeletionJob{}).Where("1=1").Update("next_attempt_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	if _, err := svc.repo.Resource(a.ID); err == nil {
		t.Fatal("retry did not finalize")
	}
	alias := a
	alias.ID = "retry-alias"
	alias.PlaybackObjectKey = a.ID + ".mp4"
	alias.Status = model.ResourceStatusReady
	if err := svc.repo.CreateResource(&alias); err == nil {
		t.Fatal("retry lost playback tombstone")
	}
}

func TestDeletionP1PlaybackReservationCannotBeClearedByStaleSave(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	a := model.Resource{ID: "frozen-reservation", UserID: "owner", Provider: "local", ObjectKey: "frozen-original", Status: model.ResourceStatusReady}
	if err := db.Create(&a).Error; err != nil {
		t.Fatal(err)
	}
	if claimed, err := svc.repo.ClaimPlaybackTranscode(a.ID); err != nil || !claimed {
		t.Fatalf("claim %v %v", claimed, err)
	}
	if err := svc.repo.SaveResource(&a); err == nil {
		t.Fatal("stale resource save cleared producer identity and processing fence")
	}
}
