package app

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestProducerP3BCanonicalTombstone(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	object := model.ResourceDeletionObject{ID: "tombstone", IntentID: "intent", Variant: "original", Provider: "local", ObjectKey: "video.mp4", Status: "pending"}
	if err := db.Create(&object).Error; err != nil {
		t.Fatal(err)
	}
	row := model.Resource{ID: "alias", UserID: "owner", Provider: "local", ObjectKey: "./video.mp4", Status: model.ResourceStatusReady}
	if err := s.repo.CreateResource(&row); err == nil {
		t.Fatal("canonical tombstone admitted alias")
	}
}

type p3bControl struct {
	tokens          []model.ResourceProducerOperation
	stop            func(model.ResourceProducerOperation) error
	calls           int
	complete        func(model.ResourceProducerOperation) (repository.ControlledPlaybackReceipt, error)
	completionCalls int
}

func (c *p3bControl) Completion(op model.ResourceProducerOperation) (repository.ControlledPlaybackReceipt, error) {
	c.completionCalls++
	if c.complete == nil {
		return repository.ControlledPlaybackReceipt{}, errors.New("completion proof unavailable")
	}
	return c.complete(op)
}

func (c *p3bControl) Operations() []model.ResourceProducerOperation { return c.tokens }
func (c *p3bControl) CloseAndStop(op model.ResourceProducerOperation) error {
	c.calls++
	return c.stop(op)
}

func p3bFixture(t *testing.T, s *Service, id string) (*model.Resource, *model.ResourceProducerOperation) {
	t.Helper()
	row := &model.Resource{ID: id, UserID: "p3b-owner", Provider: "local", ObjectKey: id + ".bin", Status: model.ResourceStatusReady}
	if err := os.MkdirAll(filepath.Join(s.dataDir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "resources", row.ObjectKey), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, s, row)
	op, err := s.repo.ClaimControlledPlaybackProduction(id)
	if err != nil || op == nil {
		t.Fatalf("claim: %v", err)
	}
	writeP1Playback(t, s.dataDir, id)
	return row, op
}

func p3bStage(s *Service, rows ...*model.Resource) error {
	ids := []string{}
	values := []model.Resource{}
	for _, row := range rows {
		ids = append(ids, row.ID)
		values = append(values, *row)
	}
	return s.withResourceDeletionLocks(nil, ids, func(r *repository.Repository) error { return r.DeleteDetachedResources(values, nil) })
}

func TestProducerP3BControlledBarrierAndStopBeforeLock(t *testing.T) {
	for _, state := range []string{"reserved", "writing"} {
		t.Run(state, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row, op := p3bFixture(t, s, "controlled")
			if state == "writing" {
				if err := s.repo.StartResourceProduction(op); err != nil {
					t.Fatal(err)
				}
			}
			original := filepath.Join(s.dataDir, "resources", row.ObjectKey)
			playback := filepath.Join(s.dataDir, "playback", row.ID+".mp4")
			locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{repository.ResourcePhysicalIdentity(row, "original"), op.Identity})
			if err != nil {
				t.Fatal(err)
			}
			defer locks.Close()
			control := &p3bControl{tokens: []model.ResourceProducerOperation{*op}}
			s.playbackControl = control
			control.stop = func(operation model.ResourceProducerOperation) error {
				current, e := s.repo.Resource(row.ID)
				if e != nil {
					return e
				}
				if current.Status != model.ResourceStatusDeleting {
					return errors.New("stop before barrier")
				}
				// A new lifecycle transaction must succeed: no DB transaction crosses stop.
				if e := s.repo.ResourceLifecycleTransaction(func(*repository.Repository) error { return nil }); e != nil {
					return e
				}
				if e := s.repo.StartResourceProduction(op); e == nil {
					return errors.New("late launch admitted")
				}
				for _, path := range []string{original, playback} {
					if _, e := os.Stat(path); e != nil {
						return e
					}
				}
				return locks.Close()
			}
			if err := p3bStage(s, row); err != nil {
				t.Fatal(err)
			}
			if err := s.repo.FinishPlaybackProduction(op, true, model.PlaybackStatusReady, ""); err == nil {
				t.Fatal("fenced callback published")
			}
			alias := model.Resource{ID: "late-alias", UserID: row.UserID, Provider: "local", ObjectKey: "../playback/" + row.ID + ".mp4", Status: model.ResourceStatusReady}
			if err := s.repo.CreateResource(&alias); err == nil {
				t.Fatal("late alias admitted")
			}
			for i := 0; i < 2; i++ {
				job, e := s.repo.ClaimNextResourceDeletionJob("worker", time.Minute)
				if e != nil || job == nil {
					t.Fatalf("claim: %v", e)
				}
				if e = s.processResourceDeletionJob(job); e != nil {
					t.Fatal(e)
				}
			}
			if control.calls != 1 {
				t.Fatalf("stop calls %d", control.calls)
			}
			var persisted model.ResourceProducerOperation
			if err := db.First(&persisted, "id = ?", op.ID).Error; err != nil {
				t.Fatal(err)
			}
			if !persisted.Fenced || persisted.PhysicalState != "settled" || persisted.Proof != "controlled-domain-stopped" {
				t.Fatalf("invalid settlement %+v", persisted)
			}
			for _, path := range []string{original, playback} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("not deleted: %v", err)
				}
			}
		})
	}
}

func TestProducerP3BStopFailureLeaseAndRestart(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	row, op := p3bFixture(t, s, "retry")
	control := &p3bControl{tokens: []model.ResourceProducerOperation{*op}, stop: func(model.ResourceProducerOperation) error { return errors.New("terminal evidence missing") }}
	s.playbackControl = control
	if err := p3bStage(s, row); err != nil {
		t.Fatal(err)
	}
	job, err := s.repo.ClaimNextResourceDeletionJob("old", time.Minute)
	if err != nil || job == nil {
		t.Fatal(err)
	}
	if err = s.processResourceDeletionJob(job); err == nil {
		t.Fatal("missing proof removed files")
	}
	for _, path := range []string{filepath.Join(s.dataDir, "resources", row.ObjectKey), filepath.Join(s.dataDir, "playback", row.ID+".mp4")} {
		if _, err = os.Stat(path); err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Model(&model.ResourceDeletionJob{}).Where("id = ?", job.ID).Update("next_attempt_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatal(err)
	}
	newer, err := s.repo.ClaimNextResourceDeletionJob("new", time.Minute)
	if err != nil || newer == nil {
		t.Fatal(err)
	}
	calls := control.calls
	if err = s.processResourceDeletionJob(job); err == nil {
		t.Fatal("stale worker accepted")
	}
	if control.calls != calls {
		t.Fatal("stale worker stopped producer")
	}
	// A fresh Service has no in-memory capability: barrier persists and removal fails.
	cold := &Service{repo: repository.New(db), dataDir: s.dataDir}
	if err = cold.processResourceDeletionJob(newer); err == nil {
		t.Fatal("cold worker guessed stop evidence")
	}
	current, err := s.repo.Resource(row.ID)
	if err != nil || current.Status != model.ResourceStatusDeleting {
		t.Fatalf("barrier lost %v", err)
	}
}

func TestProducerP3BRejectUnownedDomainAndNoControl(t *testing.T) {
	for _, kind := range []string{"nil", "wrong-token", "foreign-alias", "unselected-alias", "foreign-operation", "legacy", "reference"} {
		t.Run(kind, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row, op := p3bFixture(t, s, "guard")
			control := &p3bControl{tokens: []model.ResourceProducerOperation{*op}, stop: func(model.ResourceProducerOperation) error { return nil }}
			if kind != "nil" {
				s.playbackControl = control
			}
			switch kind {
			case "wrong-token":
				control.tokens[0].ID = "wrong"
			case "foreign-alias", "unselected-alias":
				owner := row.UserID
				if kind == "foreign-alias" {
					owner = "other"
				}
				alias := model.Resource{ID: "alias", UserID: owner, Provider: "local", ObjectKey: "../playback/./guard.mp4", Status: model.ResourceStatusReady}
				if err := db.Create(&alias).Error; err != nil {
					t.Fatal(err)
				}
			case "foreign-operation":
				other := *op
				other.ID = "foreign"
				other.UserID = "other"
				other.ResourceID = "foreign-row"
				other.PhysicalState = "settled"
				if err := db.Create(&other).Error; err != nil {
					t.Fatal(err)
				}
			case "legacy":
				if err := db.Model(op).Update("protocol", "producer-accounting-v1").Error; err != nil {
					t.Fatal(err)
				}
			case "reference":
				setting := model.SystemSetting{Key: "p3b-ref", ValueJSON: `{"resourceId":"guard"}`}
				if err := db.Create(&setting).Error; err != nil {
					t.Fatal(err)
				}
			}
			if err := p3bStage(s, row); err == nil {
				t.Fatal("unsafe domain admitted")
			}
			if control.calls != 0 {
				t.Fatal("stopped before authorization")
			}
			current, err := s.repo.Resource(row.ID)
			if err != nil || current.Status != model.ResourceStatusReady {
				t.Fatalf("failed admission changed row %v", err)
			}
		})
	}
}

func TestProducerP3BAtomicPublicationRollback(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	row, _, err := s.storeResource("owner", "file", "atomic.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	op, err := s.repo.ClaimPlaybackProduction(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.repo.StartResourceProduction(op); err != nil {
		t.Fatal(err)
	}
	failure := errors.New("injected publication failure")
	if err = db.Callback().Update().Before("gorm:update").Register("p3b_publish_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "resources" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.repo.FinishPlaybackProduction(op, true, model.PlaybackStatusReady, ""); !errors.Is(err, failure) {
		t.Fatalf("expected failure %v", err)
	}
	var persisted model.ResourceProducerOperation
	if err = db.First(&persisted, "id = ?", op.ID).Error; err != nil {
		t.Fatal(err)
	}
	if persisted.PhysicalState != "writing" {
		t.Fatal("partial physical settlement committed")
	}
	if err = db.Callback().Update().Remove("p3b_publish_failure"); err != nil {
		t.Fatal(err)
	}
	if err = s.repo.FinishPlaybackProduction(op, true, model.PlaybackStatusReady, ""); err != nil {
		t.Fatal(err)
	}
	current, err := s.repo.Resource(row.ID)
	if err != nil || current.PlaybackStatus != model.PlaybackStatusReady {
		t.Fatalf("publish %v", err)
	}
}

func TestProducerP3BAssetHistoryAndReferenceAdmission(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	first, op1 := p3bFixture(t, s, "current")
	second, op2 := p3bFixture(t, s, "history")
	asset := model.Asset{ID: "history-asset", UserID: first.UserID, PayloadJSON: `{"resourceId":"current"}`}
	version := model.AssetVersion{ID: "history-version", AssetID: asset.ID, DefinitionJSON: `{"resourceId":"history"}`}
	for _, v := range []any{&asset, &version} {
		if err := db.Create(v).Error; err != nil {
			t.Fatal(err)
		}
	}
	control := &p3bControl{tokens: []model.ResourceProducerOperation{*op1, *op2}, stop: func(model.ResourceProducerOperation) error { return nil }}
	s.playbackControl = control
	if err := s.withResourceDeletionLocks([]string{asset.ID}, []string{first.ID}, func(r *repository.Repository) error {
		return r.DeleteAssetsAndResources(first.UserID, []string{asset.ID}, []string{first.ID}, nil, false)
	}); err != nil {
		t.Fatal(err)
	}
	for _, row := range []*model.Resource{first, second} {
		current, err := s.repo.Resource(row.ID)
		if err != nil || current.Status != model.ResourceStatusDeleting {
			t.Fatalf("history missing %v", err)
		}
		if err := s.repo.UpsertCanvasProject(&model.CanvasProject{ID: "late-" + row.ID, UserID: row.UserID, PayloadJSON: `{"resourceId":"` + row.ID + `"}`}); err == nil {
			t.Fatal("new reference admitted")
		}
	}
	for i := 0; i < 4; i++ {
		job, err := s.repo.ClaimNextResourceDeletionJob("history-worker", time.Minute)
		if err != nil || job == nil {
			t.Fatal(err)
		}
		if err = s.processResourceDeletionJob(job); err != nil {
			t.Fatal(err)
		}
	}
	if control.calls != 2 {
		t.Fatalf("not whole intent stopped: %d", control.calls)
	}
}

func TestProducerP3BPublicationTokenBinding(t *testing.T) {
	for _, kind := range []string{"identity", "user", "protocol", "resource-identity"} {
		t.Run(kind, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row, _, err := s.storeResource("owner", "file", "binding.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true)
			if err != nil {
				t.Fatal(err)
			}
			op, err := s.repo.ClaimPlaybackProduction(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.repo.StartResourceProduction(op); err != nil {
				t.Fatal(err)
			}
			if err = s.repo.FinishResourceProduction(op, true, true, "ffmpeg-wait-returned"); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "identity":
				op.Identity = "wrong"
			case "user":
				op.UserID = "other"
			case "protocol":
				op.Protocol = "wrong"
			case "resource-identity":
				if err = db.Model(row).Update("provider", "s3").Error; err != nil {
					t.Fatal(err)
				}
			}
			if err = s.repo.CompletePlaybackTranscode(row.UserID, row.ID, model.PlaybackStatusReady, "", op); err == nil {
				t.Fatal("mismatched token published")
			}
		})
	}
}

func TestProducerP3BRejectTokenlessPublication(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	row := model.Resource{ID: "video", UserID: "owner", Provider: "local", ObjectKey: "video", Status: model.ResourceStatusReady, PlaybackObjectKey: "video.mp4", PlaybackStatus: model.PlaybackStatusProcessing}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.repo.CompletePlaybackTranscode(row.UserID, row.ID, model.PlaybackStatusReady, ""); err == nil {
		t.Fatal("tokenless callback published playback")
	}
}

func TestProducerP3BChild(t *testing.T) {
	mode := os.Getenv("PRODUCER_P2_CHILD")
	if mode == "" {
		return
	}
	var dialector gorm.Dialector = sqlite.Open(os.Getenv("PRODUCER_P2_DSN"))
	if os.Getenv("PRODUCER_P2_DRIVER") == "postgres" {
		dialector = postgres.Open(os.Getenv("PRODUCER_P2_DSN"))
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	var op model.ResourceProducerOperation
	if err = db.Where("protocol = ?", repository.ControlledPlaybackProtocol).First(&op).Error; err != nil {
		t.Fatal(err)
	}
	repo := repository.New(db).WithControlledPlayback([]model.ResourceProducerOperation{op})
	if strings.Contains(mode, "crash") || mode == "recover" {
		s := &Service{repo: repository.New(db), dataDir: os.Getenv("PRODUCER_P2_ROOT")}
		s.playbackControl = &p3bControl{tokens: []model.ResourceProducerOperation{op}, stop: func(model.ResourceProducerOperation) error {
			if mode == "stop-crash" {
				producerP2Barrier("P3B-CRASH")
			}
			return nil
		}}
		if mode == "barrier-crash" {
			if err = repo.DeleteResource(op.UserID, op.ResourceID); err != nil {
				t.Fatal(err)
			}
			producerP2Barrier("P3B-CRASH")
			return
		}
		for {
			job, e := s.repo.ClaimNextResourceDeletionJob("child-worker", time.Minute)
			if e != nil {
				t.Fatal(e)
			}
			if job == nil {
				break
			}
			if mode == "settled-crash" {
				pending, identities, e := repo.ControlledPlaybackForDeletion(job)
				if e != nil {
					t.Fatal(e)
				}
				locks, e := repository.AcquireResourcePhysicalLocks(s.dataDir, identities)
				if e != nil {
					t.Fatal(e)
				}
				if e = repo.WithResourcePhysicalLocks(locks).SettleControlledPlaybackDeletion(job, pending); e != nil {
					t.Fatal(e)
				}
				if e = locks.Close(); e != nil {
					t.Fatal(e)
				}
				producerP2Barrier("P3B-CRASH")
				return
			}
			if e = s.processResourceDeletionJob(job); e != nil {
				t.Fatal(e)
			}
		}
		fmt.Println("P2-P3B-DONE")
		return
	}
	paused := false
	if err = db.Callback().Update().Before("gorm:update").Register("p3b_hold_transaction", func(tx *gorm.DB) {
		table := "resource_producer_operations"
		if mode == "stage" {
			table = "resources"
		}
		if !paused && tx.Statement.Table == table {
			paused = true
			producerP2Barrier("P3B-TX")
		}
	}); err != nil {
		t.Fatal(err)
	}
	if mode == "start" {
		err = repo.StartResourceProduction(&op)
	} else {
		err = repo.DeleteResource(op.UserID, op.ResourceID)
	}
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("P2-P3B-DONE")
}

func TestProducerP3BProcessTransactionContention(t *testing.T) {
	for _, mode := range []string{"start", "stage"} {
		t.Run(mode, func(t *testing.T) {
			s, db, driver, dsn := producerP2Environment(t)
			row, op := p3bFixture(t, s, "race")
			s.playbackControl = &p3bControl{tokens: []model.ResourceProducerOperation{*op}, stop: func(model.ResourceProducerOperation) error { return nil }}
			sql, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			sql.SetMaxOpenConns(1)
			capture := &p3bContentionLogger{Interface: db.Logger}
			db.Logger = capture
			setting := "PRAGMA busy_timeout = 5"
			if driver == "postgres" {
				setting = "SET lock_timeout = '50ms'"
			}
			if err = db.Exec(setting).Error; err != nil {
				t.Fatal(err)
			}
			if driver == "postgres" {
				// Owner transactions set their own five-second timeout. Shorten it
				// in this isolated connection before the context deadline can win.
				if err = db.Callback().Raw().After("gorm:raw").Register("p3b_lock_timeout", func(tx *gorm.DB) {
					if tx.Statement.SQL.String() == "SET LOCAL lock_timeout = '5s'" {
						_, e := tx.Statement.ConnPool.ExecContext(tx.Statement.Context, "SET LOCAL lock_timeout = '50ms'")
						if e != nil {
							tx.AddError(e)
						}
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			child := startProducerP2Process(t, s.dataDir, driver, dsn, mode, "TestProducerP3BChild")
			child.barrier(t, "P3B-TX")
			if mode == "start" {
				err = p3bStage(s, row)
			} else {
				err = s.repo.StartResourceProduction(op)
			}
			if err == nil || capture.driverError == "" {
				t.Fatalf("expected actual driver contention, got %v", err)
			}
			t.Logf("%s %s driver contention: %s; entry error: %v", driver, mode, capture.driverError, err)
			child.resume()
			child.barrier(t, "P3B-DONE")
			<-child.done
			if child.err != nil {
				t.Fatal(child.err)
			}
			if mode == "start" {
				if err = p3bStage(s, row); err != nil {
					t.Fatal(err)
				}
			} else {
				if err = s.repo.StartResourceProduction(op); err == nil {
					t.Fatal("barrier loser started")
				}
			}
			if err = s.repo.StartResourceProduction(op); err == nil {
				t.Fatal("late start escaped durable fence")
			}
		})
	}
}

type p3bContentionLogger struct {
	logger.Interface
	driverError string
}

func (l *p3bContentionLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	if err != nil && (strings.Contains(strings.ToLower(err.Error()), "locked") || strings.Contains(err.Error(), "55P03")) {
		l.driverError = err.Error()
	}
	l.Interface.Trace(ctx, begin, fc, err)
}

func TestProducerP3BMixedP2Recovery(t *testing.T) {
	for _, mixed := range []bool{false, true} {
		t.Run(fmt.Sprint(mixed), func(t *testing.T) {
			s, _, _, _ := producerP2Environment(t)
			row := &model.Resource{ID: "p2-mixed", UserID: "p3b-owner", Provider: "local", ObjectKey: "p2-mixed", Status: model.ResourceStatusPending}
			if _, err := s.repo.BeginLocalResourceProduction(row, true); err != nil {
				t.Fatal(err)
			}
			control := &p3bControl{stop: func(model.ResourceProducerOperation) error { return nil }}
			s.playbackControl = control
			rows := []*model.Resource{row}
			if mixed {
				other, op := p3bFixture(t, s, "p3-mixed")
				rows = append(rows, other)
				control.tokens = []model.ResourceProducerOperation{*op}
			}
			if err := p3bStage(s, rows...); err != nil {
				t.Fatalf("controller disabled P2 recovery: %v", err)
			}
			current, err := s.repo.Resource(row.ID)
			if err != nil || current.Status != model.ResourceStatusDeleting {
				t.Fatalf("P2 recovery incomplete %v", err)
			}
		})
	}
}

func TestProducerP3BControlledCompletion(t *testing.T) {
	for _, kind := range []string{"success", "encoder-failed", "proof-missing", "wrong-token", "foreign-call", "fenced", "rollback"} {
		t.Run(kind, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row, op := p3bFixture(t, s, "result")
			if err := s.repo.StartResourceProduction(op); err != nil {
				t.Fatal(err)
			}
			control := &p3bControl{tokens: []model.ResourceProducerOperation{*op}, stop: func(model.ResourceProducerOperation) error { return nil }}
			writerLocks, e := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{repository.ResourcePhysicalIdentity(row, "original"), op.Identity})
			if e != nil {
				t.Fatal(e)
			}
			defer writerLocks.Close()
			control.complete = func(input model.ResourceProducerOperation) (repository.ControlledPlaybackReceipt, error) {
				if err := writerLocks.Close(); err != nil {
					return repository.ControlledPlaybackReceipt{}, err
				}
				result := repository.ControlledPlaybackReceipt{Operation: input, Success: kind != "encoder-failed"}
				if kind == "proof-missing" {
					return result, errors.New("lost whole-domain receipt")
				}
				if kind == "wrong-token" {
					result.Operation.Identity = "wrong"
				}
				return result, nil
			}
			s.playbackControl = control
			if kind == "fenced" {
				if err := p3bStage(s, row); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "rollback" {
				if err := db.Callback().Update().Before("gorm:update").Register("p3b_controlled_publish_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "resources" {
						tx.AddError(errors.New("publication failed"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			requested := *op
			if kind == "foreign-call" {
				requested.UserID = "foreign"
			}
			err := s.completeControlledPlayback(&requested)
			if (kind == "foreign-call" || kind == "fenced") && control.completionCalls != 0 {
				t.Fatal("invalid completion caused control side effect")
			}
			var persisted model.ResourceProducerOperation
			if e := db.First(&persisted, "id = ?", op.ID).Error; e != nil {
				t.Fatal(e)
			}
			current, e := s.repo.Resource(row.ID)
			if e != nil {
				t.Fatal(e)
			}
			switch kind {
			case "success", "encoder-failed":
				expected := model.PlaybackStatusReady
				if kind == "encoder-failed" {
					expected = model.PlaybackStatusFailed
				}
				if err != nil || current.PlaybackStatus != expected || persisted.PhysicalState != "settled" {
					t.Fatalf("completion %s %+v %v", current.PlaybackStatus, persisted, err)
				}
			default:
				if err == nil || current.PlaybackStatus == model.PlaybackStatusReady || persisted.PhysicalState != "writing" {
					t.Fatalf("invalid proof published/settled %+v %v", persisted, err)
				}
			}
		})
	}
}

func p3bRetryNow(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Model(&model.ResourceDeletionJob{}).Where("1 = 1").Updates(map[string]any{"next_attempt_at": time.Now().Add(-time.Hour), "lease_expires_at": time.Now().Add(-time.Hour)}).Error; err != nil {
		t.Fatal(err)
	}
}

func p3bAssertFiles(t *testing.T, s *Service, rows ...*model.Resource) {
	t.Helper()
	for _, row := range rows {
		for _, path := range []string{filepath.Join(s.dataDir, "resources", row.ObjectKey), filepath.Join(s.dataDir, "playback", row.ID+".mp4")} {
			if _, err := os.Stat(path); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestProducerP3BWorkerFailureBoundaries(t *testing.T) {
	for _, kind := range []string{"settle-rollback", "finish-rollback", "remove-failure", "second-stop", "lease-during-stop"} {
		t.Run(kind, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row, op := p3bFixture(t, s, "fault")
			rows := []*model.Resource{row}
			tokens := []model.ResourceProducerOperation{*op}
			if kind == "second-stop" {
				other, otherOp := p3bFixture(t, s, "second")
				rows = append(rows, other)
				tokens = append(tokens, *otherOp)
			}
			control := &p3bControl{tokens: tokens, stop: func(model.ResourceProducerOperation) error { return nil }}
			s.playbackControl = control
			if err := p3bStage(s, rows...); err != nil {
				t.Fatal(err)
			}
			job, err := s.repo.ClaimNextResourceDeletionJob("fault-worker", time.Minute)
			if err != nil || job == nil {
				t.Fatal(err)
			}
			switch kind {
			case "settle-rollback":
				if err = db.Callback().Update().Before("gorm:update").Register("p3b_settle_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "resource_producer_operations" {
						tx.AddError(errors.New("settle unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "finish-rollback":
				if err = db.Callback().Delete().Before("gorm:delete").Register("p3b_finish_failure", func(tx *gorm.DB) {
					if tx.Statement.Table == "resource_deletion_jobs" {
						tx.AddError(errors.New("finish unavailable"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			case "second-stop":
				control.stop = func(model.ResourceProducerOperation) error {
					if control.calls == 2 {
						return errors.New("second domain terminal missing")
					}
					return nil
				}
			case "lease-during-stop":
				control.stop = func(model.ResourceProducerOperation) error {
					return db.Model(&model.ResourceDeletionJob{}).Where("id = ?", job.ID).Updates(map[string]any{"lease_owner": "replacement", "attempts": gorm.Expr("attempts + 1"), "lease_expires_at": time.Now().Add(time.Minute)}).Error
				}
			case "remove-failure":
				object, e := s.repo.ResourceDeletionObject(job.ID)
				if e != nil {
					t.Fatal(e)
				}
				directory := "resources"
				if object.Variant == "playback" {
					directory = "playback"
				}
				path := filepath.Join(s.dataDir, directory, object.ObjectKey)
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err = os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
				if err = os.WriteFile(filepath.Join(path, "block"), []byte("test"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err = s.processResourceDeletionJob(job); err == nil {
				t.Fatal("fault falsely completed")
			}
			current, err := s.repo.Resource(row.ID)
			if err != nil || current.Status != model.ResourceStatusDeleting {
				t.Fatalf("barrier lost %v", err)
			}
			var persisted model.ResourceProducerOperation
			if err = db.First(&persisted, "id = ?", op.ID).Error; err != nil {
				t.Fatal(err)
			}
			if kind == "settle-rollback" || kind == "second-stop" || kind == "lease-during-stop" {
				p3bAssertFiles(t, s, rows...)
				if persisted.PhysicalState == "settled" {
					t.Fatal("partial/unowned settlement committed")
				}
			} else if persisted.PhysicalState != "settled" {
				t.Fatal("committed settlement lost")
			}
			if kind == "settle-rollback" {
				db.Callback().Update().Remove("p3b_settle_failure")
			}
			if kind == "finish-rollback" {
				db.Callback().Delete().Remove("p3b_finish_failure")
			}
			if kind == "remove-failure" {
				object, _ := s.repo.ResourceDeletionObject(job.ID)
				directory := "resources"
				if object.Variant == "playback" {
					directory = "playback"
				}
				path := filepath.Join(s.dataDir, directory, object.ObjectKey)
				if err = os.Remove(filepath.Join(path, "block")); err != nil {
					t.Fatal(err)
				}
				if err = os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			control.stop = func(model.ResourceProducerOperation) error { return nil }
			p3bRetryNow(t, db)
			for i := 0; i < len(rows)*2; i++ {
				next, e := s.repo.ClaimNextResourceDeletionJob("retry-worker", time.Minute)
				if e != nil || next == nil {
					t.Fatalf("retry claim %v", e)
				}
				if e = s.processResourceDeletionJob(next); e != nil {
					t.Fatal(e)
				}
			}
			if _, err = s.repo.Resource(row.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("retry did not finish %v", err)
			}
		})
	}
}

func TestProducerP3BProcessCrashRecovery(t *testing.T) {
	for _, mode := range []string{"barrier-crash", "stop-crash", "settled-crash"} {
		t.Run(mode, func(t *testing.T) {
			s, db, driver, dsn := producerP2Environment(t)
			row, op := p3bFixture(t, s, "crash")
			s.playbackControl = &p3bControl{tokens: []model.ResourceProducerOperation{*op}, stop: func(model.ResourceProducerOperation) error { return nil }}
			if mode != "barrier-crash" {
				if err := p3bStage(s, row); err != nil {
					t.Fatal(err)
				}
			}
			child := startProducerP2Process(t, s.dataDir, driver, dsn, mode, "TestProducerP3BChild")
			child.barrier(t, "P3B-CRASH")
			child.kill(t)
			p3bAssertFiles(t, s, row)
			current, err := s.repo.Resource(row.ID)
			if err != nil || current.Status != model.ResourceStatusDeleting {
				t.Fatalf("crash lost committed barrier %v", err)
			}
			var operation model.ResourceProducerOperation
			if err = db.First(&operation, "id = ?", op.ID).Error; err != nil {
				t.Fatal(err)
			}
			if (mode == "settled-crash") != (operation.PhysicalState == "settled") {
				t.Fatalf("unexpected durable boundary %+v", operation)
			}
			p3bRetryNow(t, db)
			recovery := startProducerP2Process(t, s.dataDir, driver, dsn, "recover", "TestProducerP3BChild")
			recovery.barrier(t, "P3B-DONE")
			<-recovery.done
			if recovery.err != nil {
				t.Fatal(recovery.err)
			}
			if _, err = s.repo.Resource(row.ID); !errors.Is(err, gorm.ErrRecordNotFound) {
				t.Fatalf("new process did not finish %v", err)
			}
			t.Logf("%s %s: killed child %d, recovered with child %d", driver, mode, child.cmd.Process.Pid, recovery.cmd.Process.Pid)
		})
	}
}
