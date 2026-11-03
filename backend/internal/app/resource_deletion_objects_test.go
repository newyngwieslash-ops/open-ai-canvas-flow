package app

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestStoryboardAssetResourceRaceRemotePhysicalRetry(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
			var fail, exists atomic.Bool
			fail.Store(true)
			exists.Store(true)
			var deletes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/test-bucket/untracked" {
					if r.Method != http.MethodGet {
						t.Errorf("untracked remote object reached physical deletion: %s", r.Method)
						w.WriteHeader(http.StatusForbidden)
						return
					}
					_, _ = io.WriteString(w, "untracked-bytes")
					return
				}
				if r.Method == http.MethodDelete {
					deletes.Add(1)
					if fail.Load() {
						http.Error(w, "injected physical failure", http.StatusServiceUnavailable)
						return
					}
					exists.Store(false)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if !exists.Load() {
					http.NotFound(w, r)
					return
				}
				_, _ = io.WriteString(w, "object-bytes")
			}))
			defer server.Close()
			dbs := profileConcurrencyDatabases(t, dialect)
			if err := database.MigrateSchema(dbs[0]); err != nil {
				t.Fatal(err)
			}
			svc := &Service{repo: repository.New(dbs[0]), dataDir: t.TempDir()}
			if err := os.WriteFile(filepath.Join(svc.dataDir, ".settings-key"), []byte("01234567890123456789012345678901"), 0600); err != nil {
				t.Fatal(err)
			}
			setting, _ := json.Marshal(ossSettingValue{Enabled: true, Provider: s3Provider, Endpoint: server.URL, Bucket: "test-bucket", Region: "us-east-1", AccessKeyID: "test-id", AccessKeySecret: "test-secret"})
			if err := dbs[0].Create(&model.SystemSetting{Key: ossSettingKey, ValueJSON: string(setting)}).Error; err != nil {
				t.Fatal(err)
			}
			row := model.Resource{ID: "remote", UserID: "owner", Provider: s3Provider, Endpoint: server.URL, Bucket: "test-bucket", ObjectKey: "untracked", Status: model.ResourceStatusReady}
			if err := dbs[0].Create(&row).Error; err != nil {
				t.Fatal(err)
			}
			// A ready remote row is not proof that all remote writers have stopped.
			// Keep admission fail-closed; exercise the exact HTTP deletion helper
			// separately against this test-owned server, without forging settlement.
			if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); !errors.Is(err, repository.ErrResourceProducerUnsettled) {
				t.Fatalf("untracked remote admitted deletion: %v", err)
			}
			svc.drainResourceDeletionJobs(1)
			var count int64
			if err := dbs[1].Model(&model.Resource{}).Where("id = ?", row.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if deletes.Load() != 0 || count != 1 || !exists.Load() {
				t.Fatalf("untracked remote not retained: deletes=%d records=%d object=%v", deletes.Load(), count, exists.Load())
			}
			for _, table := range []any{&model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}, &model.ResourceDeletionJob{}} {
				if err := dbs[1].Model(table).Count(&count).Error; err != nil || count != 0 {
					t.Fatalf("blocked remote created %T: %d %v", table, count, err)
				}
			}
			// This separate object is owned only by the HTTP test server, with no
			// upload sessions or database row. It proves helper retry, not settlement.
			ownedObject := row
			ownedObject.ID, ownedObject.ObjectKey = "http-only", "object"
			if err := svc.deleteStoredResourceObject(row.UserID, &ownedObject); err == nil {
				t.Fatal("HTTP deletion helper hid injected physical failure")
			}
			failedAttempts := deletes.Load()
			if failedAttempts == 0 || !exists.Load() {
				t.Fatalf("failed HTTP attempt changed object: deletes=%d exists=%v", deletes.Load(), exists.Load())
			}
			resp, err := http.Get(server.URL + "/test-bucket/object")
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 200 || string(body) != "object-bytes" {
				t.Fatalf("object inaccessible after failure: %v %d", err, resp.StatusCode)
			}
			fail.Store(false)
			if err := svc.deleteStoredResourceObject(row.UserID, &ownedObject); err != nil {
				t.Fatal(err)
			}
			if err := dbs[1].Model(&model.Resource{}).Where("id = ?", row.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if count != 1 || exists.Load() || deletes.Load() != failedAttempts+1 {
				t.Fatalf("HTTP retry must delete only bytes, retaining untracked row: count=%d exists=%v deletes=%d", count, exists.Load(), deletes.Load())
			}
			resp, err = http.Get(server.URL + "/test-bucket/object")
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != 404 {
				t.Fatalf("deleted object still accessible: %d", resp.StatusCode)
			}
			resp, err = http.Get(server.URL + "/test-bucket/untracked")
			if err != nil {
				t.Fatal(err)
			}
			body, err = io.ReadAll(resp.Body)
			resp.Body.Close()
			if err != nil || resp.StatusCode != 200 || string(body) != "untracked-bytes" {
				t.Fatalf("blocked remote lost its distinct bytes: %q %v %d", body, err, resp.StatusCode)
			}
		})
	}
}

func TestDeletionLifecycleUnknownThumbnailRetained(t *testing.T) {
	for _, tc := range []struct {
		status  string
		size    int64
		blocked bool
	}{{"", 0, false}, {"pending", 0, false}, {"failed", 0, false}, {"ready", 0, true}, {"failed", 8, true}} {
		t.Run(tc.status+string(rune(tc.size+'0')), func(t *testing.T) {
			svc, db, dir := newResourceDeletionTestService(t)
			row := model.Resource{ID: "thumb", UserID: "user-1", Provider: "local", ObjectKey: "thumb.png", Status: model.ResourceStatusReady, ThumbnailStatus: tc.status, ThumbnailSize: tc.size}
			path := filepath.Join(dir, "resources", row.ObjectKey)
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("original"), 0600); err != nil {
				t.Fatal(err)
			}
			createClosedLocalFixtureResource(t, svc, &row)
			if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); err != nil {
				t.Fatal(err)
			}
			svc.drainResourceDeletionJobs(8)
			var count int64
			if err := db.Model(&model.Resource{}).Where("id = ?", row.ID).Count(&count).Error; err != nil {
				t.Fatal(err)
			}
			if (count == 1) != tc.blocked {
				t.Fatalf("status %q size %d retained=%d want blocked=%v", tc.status, tc.size, count, tc.blocked)
			}
			if tc.blocked {
				if data, err := os.ReadFile(path); err != nil || string(data) != "original" {
					t.Fatalf("unknown thumbnail lost original bytes: %q %v", data, err)
				}
				var jobs int64
				if err := db.Model(&model.ResourceDeletionJob{}).Count(&jobs).Error; err != nil || jobs != 1 {
					t.Fatalf("unknown thumbnail lost retry intent: %d %v", jobs, err)
				}
			} else if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Fatalf("no necessary thumbnail retained original: %v", err)
			}
		})
	}
}

func TestDeletionLifecyclePartialObjectsRetryPreservesRecords(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	rows := []model.Resource{{ID: "part-one", UserID: "user-1", Provider: "local", ObjectKey: "one", Status: model.ResourceStatusReady}, {ID: "part-two", UserID: "user-1", Provider: "local", ObjectKey: "two", Status: model.ResourceStatusReady}}
	if err := os.MkdirAll(filepath.Join(dir, "resources", "two"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "resources", "one"), []byte("one"), 0600); err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		createClosedLocalFixtureResource(t, svc, &rows[i])
	}
	if err := svc.repo.DeleteDetachedResources(rows, nil); err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	var count, complete int64
	db.Model(&model.Resource{}).Count(&count)
	db.Model(&model.ResourceDeletionObject{}).Where("status = ?", "completed").Count(&complete)
	if count != 2 || complete != 1 {
		t.Fatalf("partial result records=%d complete=%d", count, complete)
	}
	if _, err := os.Stat(filepath.Join(dir, "resources", "one")); !os.IsNotExist(err) {
		t.Fatalf("successful object not deleted: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "resources", "two")); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ResourceDeletionJob{}).Where("1 = 1").Update("next_attempt_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	db.Model(&model.Resource{}).Count(&count)
	db.Model(&model.ResourceDeletionObject{}).Where("status = ?", "completed").Count(&complete)
	if count != 0 || complete != 2 {
		t.Fatalf("retry records=%d complete=%d", count, complete)
	}
}

func TestDeletionLifecyclePlaybackVariantIsNecessary(t *testing.T) {
	svc, db, dir := newResourceDeletionTestService(t)
	row := model.Resource{ID: "variant", UserID: "user-1", Provider: "local", ObjectKey: "original", Status: model.ResourceStatusReady}
	original := filepath.Join(dir, "resources", row.ObjectKey)
	if err := os.MkdirAll(filepath.Dir(original), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(original, []byte("bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, svc, &row)
	op, err := svc.repo.ClaimPlaybackProduction(row.ID)
	if err != nil || op == nil {
		t.Fatalf("playback claim: %v", err)
	}
	if err := svc.repo.StartResourceProduction(op); err != nil {
		t.Fatal(err)
	}
	writeP1Playback(t, dir, row.ID)
	if err := svc.repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.CompletePlaybackTranscode(row.UserID, row.ID, model.PlaybackStatusReady, "", op); err != nil {
		t.Fatal(err)
	}
	if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(8)
	for _, path := range []string{filepath.Join(dir, "resources", "original"), filepath.Join(dir, "playback", "variant.mp4")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("variant still exists: %s: %v", path, err)
		}
	}
	var count int64
	db.Model(&model.ResourceDeletionObject{}).Where("status = ?", "completed").Count(&count)
	if count != 2 {
		t.Fatalf("completed variants=%d", count)
	}
}

func TestDeletionLifecycleAliasKeepsObjectAndDeletingAliasesConverge(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(map[bool]string{false: "retained", true: "all-deleting"}[both], func(t *testing.T) {
			svc, db, dir := newResourceDeletionTestService(t)
			rows := []model.Resource{{ID: "alias-a", UserID: "user-1", Provider: "local", ObjectKey: "shared", Status: model.ResourceStatusReady}, {ID: "alias-b", UserID: "user-2", Provider: "local", ObjectKey: "./shared", Status: model.ResourceStatusReady}}
			path := filepath.Join(dir, "resources", "shared")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("shared"), 0600); err != nil {
				t.Fatal(err)
			}
			for i := range rows {
				createClosedLocalFixtureResource(t, svc, &rows[i])
			}
			for i, provider := range []string{"", " LOCAL "} {
				if err := db.Model(&rows[i]).Update("provider", provider).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := svc.repo.DeleteDetachedResources(rows[:1], nil); err != nil {
				t.Fatal(err)
			}
			if both {
				if err := svc.repo.DeleteDetachedResources(rows[1:], nil); err != nil {
					t.Fatal(err)
				}
			}
			svc.drainResourceDeletionJobs(8)
			_, err := os.Stat(path)
			if both && !os.IsNotExist(err) {
				t.Fatalf("deleting aliases stuck: %v", err)
			}
			if !both && err != nil {
				t.Fatalf("nondeleting alias lost physical object: %v", err)
			}
			var count int64
			db.Model(&model.Resource{}).Count(&count)
			want := int64(1)
			if both {
				want = 0
			}
			if count != want {
				t.Fatalf("aliases remaining=%d want%d", count, want)
			}
		})
	}
}

func TestDeletionLifecycleWorkerRechecksLateReferenceAndIdentity(t *testing.T) {
	for _, change := range []string{"reference", "identity"} {
		t.Run(change, func(t *testing.T) {
			svc, db, dir := newResourceDeletionTestService(t)
			row := model.Resource{ID: "late-recheck", UserID: "user-1", Provider: "local", ObjectKey: "recheck", Status: model.ResourceStatusReady}
			path := filepath.Join(dir, "resources", "recheck")
			if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			createClosedLocalFixtureResource(t, svc, &row)
			if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); err != nil {
				t.Fatal(err)
			}
			if change == "reference" {
				if err := db.Create(&model.CanvasProject{ID: "late-canvas", UserID: row.UserID, Title: "late source", PayloadJSON: `{"resourceId":"late-recheck"}`}).Error; err != nil {
					t.Fatal(err)
				}
			} else {
				if err := db.Model(&row).Update("object_key", "different").Error; err != nil {
					t.Fatal(err)
				}
			}
			svc.drainResourceDeletionJobs(8)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("late change ignored: %v", err)
			}
			var jobs int64
			db.Model(&model.ResourceDeletionJob{}).Count(&jobs)
			if jobs != 1 {
				t.Fatalf("retry job lost: %d", jobs)
			}
		})
	}
}
