package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestProducerP1LegacyRowsHaveNoStopEvidence(t *testing.T) {
	s, db := newMediaRecoveryTestService(t)
	for _, status := range []model.ResourceStatus{model.ResourceStatusReady, model.ResourceStatusFailed, model.ResourceStatusPending} {
		row := model.Resource{ID: string(status), UserID: "owner", Provider: "local", ObjectKey: string(status), Status: status}
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
		if err := s.repo.DeleteResource(row.UserID, row.ID); err == nil {
			t.Errorf("legacy %s accepted without writer evidence", status)
		}
	}
}

func TestProducerP1AcceptedRequestCommitsAfterFailure(t *testing.T) {
	s, db := newMediaRecoveryTestService(t)
	commit, committed := make(chan struct{}), make(chan struct{})
	lateFile := filepath.Join(t.TempDir(), "accepted-object")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close() // Request accepted, response lost, server can still commit.
		<-commit
		if err := os.WriteFile(lateFile, data, 0600); err != nil {
			t.Error(err)
		}
		close(committed)
	}))
	defer server.Close()
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(commit) }) }
	defer release()
	_, _, err := s.storeResourceWithWriter("owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true,
		func(_ *model.Resource, _ string, body io.Reader) (resourceWriteResult, error) {
			req, _ := http.NewRequest(http.MethodPut, server.URL, body)
			response, err := server.Client().Do(req)
			if response != nil {
				response.Body.Close()
			}
			return resourceWriteResult{}, err
		})
	if err == nil {
		t.Fatal("lost response must fail business write")
	}
	var row model.Resource
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if row.Status != model.ResourceStatusFailed {
		t.Fatalf("status=%s", row.Status)
	}
	var first model.ResourceProducerOperation
	if err := db.First(&first).Error; err != nil {
		t.Fatal(err)
	}
	if first.PhysicalState != "unknown" {
		t.Fatalf("state=%s", first.PhysicalState)
	}
	if err := s.repo.DeleteResource(row.UserID, row.ID); err == nil {
		t.Fatal("unknown accepted for deletion")
	}
	s.drainResourceDeletionJobs(4)
	release()
	<-committed
	if _, err := os.Stat(lateFile); err != nil {
		t.Fatal(err)
	}
	// A later real synchronous local write succeeds, without settling the first request.
	if _, err := s.retryStoredResource(row.UserID, &row, row.Kind, row.MimeType, row.Size, strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DeleteResource(row.UserID, row.ID); err == nil {
		t.Fatal("later success laundered unknown")
	}
	alias := row
	alias.ID, alias.Status = "alias", model.ResourceStatusPending
	op, err := s.repo.BeginResourceProduction(&alias, "original", true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.writeResourceProduction(&alias, op, "data.bin", strings.NewReader("data"), s.storeResourceObject); err != nil {
		t.Fatal(err)
	}
	alias.Status = model.ResourceStatusReady
	if err := s.repo.SaveResource(&alias); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DeleteResource(alias.UserID, alias.ID); err == nil {
		t.Fatal("alias laundered unknown")
	}
}

func TestProducerP1AtomicAdmissionAndSettlementFailure(t *testing.T) {
	for _, phase := range []string{"insert", "update"} {
		t.Run(phase, func(t *testing.T) {
			s, db := newMediaRecoveryTestService(t)
			predicate := ""
			if phase == "update" {
				predicate = " WHEN NEW.physical_state = 'settled'"
			}
			if err := db.Exec("CREATE TRIGGER fail_producer BEFORE " + strings.ToUpper(phase) + " ON resource_producer_operations" + predicate + " BEGIN SELECT RAISE(ABORT, 'injected producer persistence failure'); END").Error; err != nil {
				t.Fatal(err)
			}
			_, _, err := s.storeResource("owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true)
			if err == nil {
				t.Fatal("persistence failure hidden")
			}
			var rows []model.Resource
			if err := db.Find(&rows).Error; err != nil {
				t.Fatal(err)
			}
			if phase == "insert" {
				if len(rows) != 0 {
					t.Fatal("CreateResource survived Begin failure")
				}
			} else {
				if len(rows) != 1 || rows[0].Status != model.ResourceStatusFailed {
					t.Fatalf("rows=%v", rows)
				}
				var op model.ResourceProducerOperation
				if err := db.First(&op).Error; err != nil {
					t.Fatal(err)
				}
				if op.PhysicalState != "writing" {
					t.Fatalf("lost obligation: %s", op.PhysicalState)
				}
				if err := s.repo.DeleteResource(rows[0].UserID, rows[0].ID); err == nil {
					t.Fatal("uncommitted settlement allowed delete")
				}
			}
		})
	}
}

func TestProducerP1DeletionRechecksInterruptedOperation(t *testing.T) {
	s, db := newMediaRecoveryTestService(t)
	row, _, err := s.storeResource("owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DeleteResource(row.UserID, row.ID); err != nil {
		t.Fatal(err)
	}
	job, err := s.repo.ClaimNextResourceDeletionJob("test", time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim: %v", err)
	}
	if _, _, err := s.repo.PrepareResourceDeletion(job); err != nil {
		t.Fatal(err)
	}
	var op model.ResourceProducerOperation
	if err := db.First(&op).Error; err != nil {
		t.Fatal(err)
	}
	// Simulate recovery of a previously unrecorded interrupted attempt.
	op.ID, op.PhysicalState = "interrupted", "unknown"
	if err := db.Create(&op).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.repo.PrepareResourceDeletion(job); err == nil {
		t.Fatal("prepare bypassed unresolved operation")
	}
	if err := s.repo.FinishResourceDeletion(job, false); err == nil {
		t.Fatal("finish bypassed unresolved operation")
	}
	if _, err := s.repo.Resource(row.ID); err != nil {
		t.Fatal("resource removed", err)
	}
	var object model.ResourceDeletionObject
	if err := db.First(&object, "id = ?", job.ID).Error; err != nil {
		t.Fatal(err)
	}
	if object.Status != "pending" {
		t.Fatalf("ledger=%s", object.Status)
	}
}

func TestProducerP1SingleStartAndLiveIdentity(t *testing.T) {
	s, db := newMediaRecoveryTestService(t)
	row := model.Resource{ID: "one", UserID: "owner", Provider: "local", ObjectKey: "one", Status: model.ResourceStatusPending}
	op, err := s.repo.BeginResourceProduction(&row, "original", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&row).Update("object_key", "changed").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.StartResourceProduction(op); err == nil {
		t.Fatal("stale identity started")
	}
	if err := db.Model(&row).Update("object_key", "one").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.StartResourceProduction(op); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.StartResourceProduction(op); err == nil {
		t.Fatal("duplicate SDK invocation admitted")
	}
	if err := s.repo.FinishResourceProduction(op, true, false, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.StartResourceProduction(op); err == nil {
		t.Fatal("settled token replay admitted")
	}
	if err := db.Delete(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err == nil {
		t.Fatal("missing resource settled again")
	}
}

func TestProducerP1NewPlaybackAdmissionAndCanonicalAliases(t *testing.T) {
	s, db := newMediaRecoveryTestService(t)
	row, _, err := s.storeResource("owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	op, err := s.repo.ClaimPlaybackProduction(row.ID)
	if err != nil || op == nil {
		t.Fatalf("claim: %v", err)
	}
	var operations []model.ResourceProducerOperation
	if err := db.Where("resource_id = ? AND variant = ?", row.ID, "playback").Find(&operations).Error; err != nil {
		t.Fatal(err)
	}
	if len(operations) != 1 || operations[0].PhysicalState != "reserved" {
		t.Fatalf("fresh playback incorrectly treated as legacy: %v", operations)
	}
	if err := s.repo.StartResourceProduction(op); err != nil {
		t.Fatal(err)
	}
	// Test owns this synchronous local playback file, so its close is evidence.
	if err := os.MkdirAll(filepath.Join(s.dataDir, "playback"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "playback", row.ID+".mp4"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.CompletePlaybackTranscode(row.UserID, row.ID, model.PlaybackStatusReady, "", op); err != nil {
		t.Fatal(err)
	}
	alias := model.Resource{ID: "cross-namespace", UserID: "owner", Provider: "local", ObjectKey: "../playback/" + row.ID + ".mp4", Status: model.ResourceStatusReady}
	if err := db.Create(&alias).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DeleteResource(row.UserID, row.ID); err == nil {
		t.Fatal("cross-namespace legacy alias evaded evidence guard")
	}
	if err := db.Delete(&alias).Error; err != nil {
		t.Fatal(err)
	}
	// Same basename in a different real directory is not an alias.
	alias.ID, alias.ObjectKey = "other-namespace", row.ID+".mp4"
	if err := db.Create(&alias).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DeleteResource(row.UserID, row.ID); err != nil {
		t.Fatal(err)
	}
	s.drainResourceDeletionJobs(4)
	if _, err := s.repo.Resource(row.ID); err == nil {
		t.Fatal("known original+playback did not finalize")
	}
	// New path traversal admissions are rejected, while legacy aliases are guarded.
	alias.ID, alias.ObjectKey, alias.Status = "invalid", "../playback/escape", model.ResourceStatusPending
	if _, err := s.repo.BeginResourceProduction(&alias, "original", true); err == nil {
		t.Fatal("escaped key admitted")
	}
}

func TestProducerP1SettledCanonicalAliasRetainsBytes(t *testing.T) {
	s, _ := newMediaRecoveryTestService(t)
	for index, key := range []string{"shared.bin", "./shared.bin"} {
		row := model.Resource{ID: []string{"first", "alias"}[index], UserID: "owner", Provider: "local", ObjectKey: key, Status: model.ResourceStatusPending}
		op, err := s.repo.BeginResourceProduction(&row, "original", true)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := s.writeResourceProduction(&row, op, "shared.bin", strings.NewReader("data"), s.storeResourceObject); err != nil {
			t.Fatal(err)
		}
		row.Status = model.ResourceStatusReady
		if err := s.repo.SaveResource(&row); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.repo.DeleteResource("owner", "first"); err != nil {
		t.Fatal(err)
	}
	s.drainResourceDeletionJobs(4)
	if _, err := s.repo.Resource("alias"); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(s.dataDir, "resources", "shared.bin")); err != nil || string(data) != "data" {
		t.Fatalf("live canonical alias lost bytes: %q %v", data, err)
	}
}

// Only these selected tests call this after their own synchronous file/directory
// creation returned. It does not infer settlement for arbitrary ready fixtures.
func createClosedLocalFixtureResource(t *testing.T, s *Service, row *model.Resource) {
	t.Helper()
	if row.Provider != "local" {
		t.Fatal("fixture must own a local object")
	}
	if _, err := os.Stat(filepath.Join(s.dataDir, "resources", row.ObjectKey)); err != nil {
		t.Fatal(err)
	}
	row.Status = model.ResourceStatusPending
	op, err := s.repo.BeginResourceProduction(row, "original", true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repo.StartResourceProduction(op); err != nil {
		t.Fatal(err)
	}
	if err := s.repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	row.Status = model.ResourceStatusReady
	if err := s.repo.SaveResource(row); err != nil {
		t.Fatal(err)
	}
}

func TestProducerP1SQLiteIndependentConnectionsFenceAdmission(t *testing.T) {
	t.Setenv("DELETION_TEST_POSTGRES_DSN", "")
	db1, db2 := deletionConcurrencyDBs(t)
	repo1, repo2 := repository.New(db1), repository.New(db2)
	row := model.Resource{ID: "two-connections", UserID: "owner", Provider: "local", ObjectKey: "file", Status: model.ResourceStatusPending}
	op, err := repo1.BeginResourceProduction(&row, "original", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"reserved", "writing"} {
		if state == "writing" {
			if err := db1.Model(&row).Update("status", model.ResourceStatusPending).Error; err != nil {
				t.Fatal(err)
			}
			if err := repo1.StartResourceProduction(op); err != nil {
				t.Fatal(err)
			}
		}
		// Business failure cannot authorize deletion from another DB connection.
		if err := db1.Model(&row).Update("status", model.ResourceStatusFailed).Error; err != nil {
			t.Fatal(err)
		}
		if err := repo2.DeleteResource(row.UserID, row.ID); err == nil {
			t.Fatalf("%s admitted deletion", state)
		}
	}
	// This fixture has not invoked any physical writer, and now abandons it
	// synchronously; record that test-owned terminal outcome explicitly.
	if err := repo1.FinishResourceProduction(op, true, false, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	if err := repo2.DeleteResource(row.UserID, row.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo1.BeginResourceProduction(&row, "original", false); err == nil {
		t.Fatal("delete-first retry admitted")
	}
	s := &Service{repo: repo1}
	called := false
	if _, err := s.writeResourceProduction(&row, op, "file", strings.NewReader("data"), func(*model.Resource, string, io.Reader) (resourceWriteResult, error) {
		called = true
		return resourceWriteResult{}, nil
	}); err == nil || called {
		t.Fatalf("delete-first start touched writer: called=%v err=%v", called, err)
	}
}

func TestProducerP1CallbackSuccessDoesNotProveStopped(t *testing.T) {
	s, _ := newMediaRecoveryTestService(t)
	row, _, err := s.storeResourceWithWriter("owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true,
		func(resource *model.Resource, _ string, body io.Reader) (resourceWriteResult, error) {
			return resourceWriteResult{ETag: "etag"}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != model.ResourceStatusReady {
		t.Fatalf("business status = %s", row.Status)
	}
	if err := s.repo.DeleteResource(row.UserID, row.ID); err == nil {
		t.Fatal("arbitrary callback nil error treated as settled local writer")
	}
}

func TestProducerP1KnownLocalWriterCanDelete(t *testing.T) {
	s, _ := newMediaRecoveryTestService(t)
	row, _, err := s.storeResource("owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repo.DeleteResource(row.UserID, row.ID); err != nil {
		t.Fatal(err)
	}
	s.drainResourceDeletionJobs(4)
	if _, err := s.repo.Resource(row.ID); err == nil {
		t.Fatal("known closed local writer did not finalize")
	}
	if _, err := os.Stat(filepath.Join(s.dataDir, "resources", row.ObjectKey)); !os.IsNotExist(err) {
		t.Fatalf("physical object survived: %v", err)
	}
}
