package app

import (
	"os"
	"path/filepath"
	"testing"
	"yingce/backend/internal/model"
)

func TestPlaybackCompletedReservationSurvivesPreclaimSave(t *testing.T) {
	for _, terminal := range []string{model.PlaybackStatusReady, model.PlaybackStatusFailed} {
		t.Run(terminal, func(t *testing.T) {
			svc, db, dir := newResourceDeletionTestService(t)
			stale := model.Resource{ID: "completed-producer", UserID: "owner", Provider: "local", ObjectKey: "completed-original", Status: model.ResourceStatusReady}
			if err := os.MkdirAll(filepath.Join(dir, "resources"), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "resources", stale.ObjectKey), []byte("data"), 0600); err != nil {
				t.Fatal(err)
			}
			createClosedLocalFixtureResource(t, svc, &stale)
			claimed, err := svc.repo.ClaimPlaybackProduction(stale.ID)
			if err != nil || claimed == nil {
				t.Fatalf("claim %v %v", claimed, err)
			}
			path := writeP1Playback(t, dir, stale.ID)
			if err := svc.repo.StartResourceProduction(claimed); err != nil {
				t.Fatal(err)
			}
			if err := svc.repo.FinishPlaybackProduction(claimed, true, terminal, "producer result"); err != nil {
				t.Fatal(err)
			}
			// The preclaim snapshot is stale even though the producer is no longer processing.
			stale.ETag = "unrelated-save"
			_ = svc.repo.SaveResource(&stale)
			current, err := svc.repo.Resource(stale.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.PlaybackObjectKey != stale.ID+".mp4" || current.PlaybackStatus != terminal || current.PlaybackError != "producer result" {
				t.Errorf("generic save erased completed producer fields: %+v", current)
			}
			if err := svc.repo.DeleteDetachedResources([]model.Resource{*current}, nil); err != nil {
				t.Fatal(err)
			}
			var count int64
			if err := db.Model(&model.ResourceDeletionObject{}).Where("variant = ? AND object_key = ?", "playback", stale.ID+".mp4").Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 {
				t.Errorf("physical playback missing from durable ledger: %d", count)
			}
			svc.drainResourceDeletionJobs(8)
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("actual deletion leaked completed playback: %v", err)
			}
			if _, err := svc.repo.Resource(stale.ID); err == nil {
				t.Error("completed deletion retained resource")
			}
		})
	}
}
