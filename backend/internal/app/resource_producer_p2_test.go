package app

import (
	"bufio"
	"errors"
	"fmt"
	"gorm.io/driver/postgres"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type producerP2PausedReader struct{ sent bool }

func (r *producerP2PausedReader) Read(p []byte) (int, error) {
	if !r.sent {
		r.sent = true
		return copy(p, "part"), nil
	}
	fmt.Println("P2-WRITING")
	_, _ = io.Copy(io.Discard, os.Stdin)
	return 0, io.EOF
}

func TestProducerP2Child(t *testing.T) {
	mode := os.Getenv("PRODUCER_P2_CHILD")
	if mode == "" {
		return
	}
	dialector := gorm.Dialector(sqlite.Open(os.Getenv("PRODUCER_P2_DSN")))
	if os.Getenv("PRODUCER_P2_DRIVER") == "postgres" {
		dialector = postgres.Open(os.Getenv("PRODUCER_P2_DSN"))
	}
	db, err := gorm.Open(dialector, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{repo: repository.New(db), dataDir: os.Getenv("PRODUCER_P2_ROOT")}
	if mode != "write" {
		producerP2ChildWindow(t, s, db, mode)
		return
	}
	if _, _, err := s.storeResource("p2-owner", "file", "part.bin", "application/octet-stream", 4, 0, 0, 0, &producerP2PausedReader{}, nil, true); err != nil {
		t.Fatal(err)
	}
}

func producerP2Barrier(name string) {
	fmt.Println("P2-" + name)
	var b [1]byte
	_, _ = os.Stdin.Read(b[:])
}

func producerP2ChildWindow(t *testing.T, s *Service, db *gorm.DB, mode string) {
	if mode == "worker" {
		job, err := s.repo.ClaimNextResourceDeletionJob("child-worker", time.Minute)
		if err != nil || job == nil {
			t.Fatalf("claim: %v", err)
		}
		object, err := s.repo.ResourceDeletionObject(job.ID)
		if err != nil {
			t.Fatal(err)
		}
		row := model.Resource{Provider: job.Provider, ObjectKey: job.ObjectKey, PlaybackObjectKey: job.ObjectKey}
		locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{repository.ResourcePhysicalIdentity(&row, object.Variant)})
		if err != nil {
			t.Fatal(err)
		}
		defer locks.Close()
		bound := s.repo.WithResourcePhysicalLocks(locks)
		if _, _, err := bound.PrepareResourceDeletion(job); err != nil {
			t.Fatal(err)
		}
		producerP2Barrier("WORKER")
		if err := s.deleteLocalResourceObject(job.ObjectKey); err != nil {
			t.Fatal(err)
		}
		if err := bound.FinishResourceDeletion(job, false); !errors.Is(err, repository.ErrResourceDeletionLeaseLost) {
			t.Fatalf("stale worker completion: %v", err)
		}
		fmt.Println("P2-REJECTED")
		return
	}
	row := model.Resource{ID: "window", UserID: "p2-owner", Provider: "local", ObjectKey: "window", Status: model.ResourceStatusPending, Kind: "file", MimeType: "application/octet-stream", Size: 4}
	op, err := s.repo.BeginLocalResourceProduction(&row, true)
	if err != nil {
		t.Fatal(err)
	}
	if mode == "reserved" {
		producerP2Barrier("RESERVED")
	}
	locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{op.Identity})
	if err != nil {
		t.Fatal(err)
	}
	defer locks.Close()
	if mode == "locked" {
		producerP2Barrier("LOCKED")
	}
	repo := s.repo.WithResourcePhysicalLocks(locks)
	if err := repo.StartResourceProduction(op); err != nil {
		if mode == "reserved" {
			fmt.Println("P2-REJECTED")
			return
		}
		t.Fatal(err)
	}
	if err := writeLocalResourceObject(filepath.Join(s.dataDir, "resources", row.ObjectKey), strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	if mode == "closed" {
		producerP2Barrier("CLOSED")
	}
	if err := repo.FinishResourceProduction(op, true, true, "local-writer-returned"); err != nil {
		t.Fatal(err)
	}
	if err := locks.Close(); err != nil {
		t.Fatal(err)
	}
	if mode == "settled" || mode == "publish-late" || mode == "failure-late" {
		producerP2Barrier("SETTLED")
	}
	if mode == "publish-transaction" {
		paused := false
		if err := db.Callback().Update().Before("gorm:update").Register("p2_publication_barrier", func(tx *gorm.DB) {
			if !paused && tx.Statement.Table == "resources" {
				paused = true
				producerP2Barrier("PUBLISH-TX")
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	row.Status = model.ResourceStatusReady
	if mode == "failure-late" {
		row.Status = model.ResourceStatusFailed
		row.Error = "injected failure"
	}
	err = s.publishResourceProduction(&row, op, "")
	if mode == "publish-late" || mode == "failure-late" {
		if err == nil {
			t.Fatal("recovered token published")
		}
		fmt.Println("P2-REJECTED")
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println("P2-PUBLISHED")
}

type producerP2Process struct {
	cmd   *exec.Cmd
	input io.WriteCloser
	lines chan string
	done  chan struct{}
	err   error
}

func startProducerP2Process(t *testing.T, root, driver, dsn, mode string, testNames ...string) *producerP2Process {
	t.Helper()
	testName := "TestProducerP2Child"
	if len(testNames) == 1 {
		testName = testNames[0]
	}
	p := &producerP2Process{cmd: exec.Command(os.Args[0], "-test.run=^"+testName+"$", "-test.v"), lines: make(chan string, 128), done: make(chan struct{})}
	p.cmd.Env = append(os.Environ(), "PRODUCER_P2_CHILD="+mode, "PRODUCER_P2_DSN="+dsn, "PRODUCER_P2_DRIVER="+driver, "PRODUCER_P2_ROOT="+root)
	var err error
	p.input, err = p.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := p.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p.cmd.Stderr = p.cmd.Stdout
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			p.lines <- scanner.Text()
		}
		p.err = p.cmd.Wait()
		close(p.done)
	}()
	t.Cleanup(func() {
		_ = p.input.Close()
		_ = p.cmd.Process.Kill()
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
			t.Error("child cleanup timeout")
		}
	})
	return p
}
func (p *producerP2Process) barrier(t *testing.T, want string) {
	t.Helper()
	for {
		select {
		case line := <-p.lines:
			if strings.Contains(line, "P2-"+want) {
				return
			}
		case <-p.done:
			for len(p.lines) > 0 {
				if strings.Contains(<-p.lines, "P2-"+want) {
					return
				}
			}
			t.Fatalf("child exited at %s: %v", want, p.err)
		case <-time.After(20 * time.Second):
			t.Fatalf("child timeout at %s", want)
		}
	}
}
func (p *producerP2Process) resume() { _, _ = p.input.Write([]byte("x")) }
func (p *producerP2Process) kill(t *testing.T) {
	t.Helper()
	if err := p.cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	<-p.done
}

func producerP2Environment(t *testing.T) (*Service, *gorm.DB, string, string) {
	t.Helper()
	root := t.TempDir()
	driver, dsn := "sqlite", filepath.Join(root, "state.sqlite")+"?_journal_mode=WAL&_busy_timeout=2000"
	cfg := &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)}
	var dialector gorm.Dialector = sqlite.Open(dsn)
	if supplied := os.Getenv("PRODUCER_P2_POSTGRES_DSN"); supplied != "" {
		driver = "postgres"
		admin, err := gorm.Open(postgres.Open(supplied), cfg)
		if err != nil {
			t.Fatal(err)
		}
		schema := fmt.Sprintf("producer_p2_%d", time.Now().UnixNano())
		if err := admin.Exec("CREATE SCHEMA " + schema).Error; err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { admin.Exec("DROP SCHEMA " + schema + " CASCADE"); sql, _ := admin.DB(); _ = sql.Close() })
		dsn = supplied + "&search_path=" + schema
		dialector = postgres.Open(dsn)
	}
	db, err := gorm.Open(dialector, cfg)
	if err != nil {
		t.Fatal(err)
	}
	sql, _ := db.DB()
	t.Cleanup(func() { _ = sql.Close() })
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(db), dataDir: root}, db, driver, dsn
}

func TestProducerP2ProcessExitWindows(t *testing.T) {
	for _, mode := range []string{"reserved", "closed", "settled", "publish-late", "failure-late"} {
		t.Run(mode, func(t *testing.T) {
			s, db, driver, dsn := producerP2Environment(t)
			p := startProducerP2Process(t, s.dataDir, driver, dsn, mode)
			barrier := "SETTLED"
			if mode == "reserved" {
				barrier = "RESERVED"
			}
			if mode == "closed" {
				barrier = "CLOSED"
			}
			p.barrier(t, barrier)
			var row model.Resource
			if err := db.First(&row, "id = ?", "window").Error; err != nil {
				t.Fatal(err)
			}
			if mode == "closed" {
				if err := s.cleanupDetachedUserResources(row.UserID, []model.Resource{row}); err == nil {
					t.Fatal("live closed writer lock ignored")
				}
				p.kill(t)
			}
			if mode == "settled" {
				p.kill(t)
			}
			if err := s.cleanupDetachedUserResources(row.UserID, []model.Resource{row}); err != nil {
				t.Fatal(err)
			}
			var op model.ResourceProducerOperation
			if err := db.First(&op).Error; err != nil {
				t.Fatal(err)
			}
			if !op.Fenced || op.PhysicalState != "settled" || op.Outcome != "failed" || op.Proof != "local-lock-recovered" {
				t.Fatalf("recovery=%+v", op)
			}
			if mode == "reserved" || mode == "publish-late" || mode == "failure-late" {
				p.resume()
				p.barrier(t, "REJECTED")
				<-p.done
				if p.err != nil {
					t.Fatal(p.err)
				}
			}
			s.drainResourceDeletionJobs(4)
			if _, err := s.repo.Resource(row.ID); err == nil {
				t.Fatal("recovered resource survived")
			}
		})
	}
}

func TestProducerP2PublicationTransactionPreventsRecoveryInterleave(t *testing.T) {
	s, db, driver, dsn := producerP2Environment(t)
	p := startProducerP2Process(t, s.dataDir, driver, dsn, "publish-transaction")
	p.barrier(t, "PUBLISH-TX")
	var row model.Resource
	if err := db.First(&row, "id = ?", "window").Error; err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- s.cleanupDetachedUserResources(row.UserID, []model.Resource{row}) }()
	select {
	case err := <-finished:
		t.Fatalf("recovery interleaved while publish tx paused: %v", err)
	case <-time.After(150 * time.Millisecond):
	}
	p.resume()
	p.barrier(t, "PUBLISHED")
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("recovery did not proceed after publication commit")
	}
	var op model.ResourceProducerOperation
	if err := db.First(&op).Error; err != nil {
		t.Fatal(err)
	}
	if op.Proof != "local-writer-returned" || op.Outcome != "success" {
		t.Fatalf("completed publication was falsely recovered: %+v", op)
	}
}

func TestProducerP2DeletionRejectsLostLockCapability(t *testing.T) {
	s, _, _, _ := producerP2Environment(t)
	row, _, err := s.storeResource("p2-owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.cleanupDetachedUserResources(row.UserID, []model.Resource{*row}); err != nil {
		t.Fatal(err)
	}
	job, err := s.repo.ClaimNextResourceDeletionJob("lost-lock", time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim %v", err)
	}
	locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{repository.ResourcePhysicalIdentity(row, "original")})
	if err != nil {
		t.Fatal(err)
	}
	bound := s.repo.WithResourcePhysicalLocks(locks)
	if _, _, err := bound.PrepareResourceDeletion(job); err != nil {
		t.Fatal(err)
	}
	if err := locks.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := bound.PrepareResourceDeletion(job); err == nil {
		t.Error("Prepare accepted lost physical lock")
	}
	if err := bound.FinishResourceDeletion(job, false); err == nil {
		t.Error("Finish accepted lost physical lock")
	}
}

func TestProducerP2RecoveryRollbackAndAliasBoundary(t *testing.T) {
	for _, scenario := range []string{"settlement-error", "stage-error", "reference", "foreign-alias", "selected-alias", "legacy-unknown"} {
		t.Run(scenario, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row := model.Resource{ID: "recover", UserID: "p2-owner", Provider: "local", ObjectKey: "recover", Status: model.ResourceStatusPending}
			op, err := s.repo.BeginLocalResourceProduction(&row, true)
			if err != nil {
				t.Fatal(err)
			}
			rows := []model.Resource{row}
			switch scenario {
			case "settlement-error":
				db.Callback().Update().Before("gorm:update").Register("p2_inject", func(tx *gorm.DB) {
					if tx.Statement.Table == "resource_producer_operations" {
						tx.AddError(errors.New("injected settlement failure"))
					}
				})
			case "stage-error":
				db.Callback().Create().Before("gorm:create").Register("p2_inject", func(tx *gorm.DB) {
					if tx.Statement.Table == "resource_deletion_intents" {
						tx.AddError(errors.New("injected stage failure"))
					}
				})
			case "reference":
				if err := db.Create(&model.CanvasProject{ID: "ref", UserID: row.UserID, PayloadJSON: `{"resourceId":"recover"}`}).Error; err != nil {
					t.Fatal(err)
				}
			case "foreign-alias", "selected-alias":
				alias := row
				alias.ID = "alias"
				alias.ObjectKey = "./recover"
				if _, err := s.repo.BeginLocalResourceProduction(&alias, true); err != nil {
					t.Fatal(err)
				}
				if scenario == "selected-alias" {
					rows = append(rows, alias)
				}
			case "legacy-unknown":
				old := *op
				old.ID = "old"
				old.Protocol = "producer-accounting-v1"
				old.PhysicalState = "unknown"
				if err := db.Create(&old).Error; err != nil {
					t.Fatal(err)
				}
			}
			ids := []string{}
			for _, row := range rows {
				ids = append(ids, row.ID)
			}
			err = s.withResourceDeletionLocks(nil, ids, func(repo *repository.Repository) error { return repo.DeleteDetachedResources(rows, nil) })
			if scenario == "selected-alias" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("unsafe recovery committed")
			}
			var stored model.ResourceProducerOperation
			if err := db.First(&stored, "id = ?", op.ID).Error; err != nil {
				t.Fatal(err)
			}
			if stored.Fenced || stored.PhysicalState != "reserved" {
				t.Fatalf("partial recovery leaked: %+v", stored)
			}
			current, err := s.repo.Resource(row.ID)
			if err != nil || current.Status != model.ResourceStatusPending {
				t.Fatalf("partial state: %+v %v", current, err)
			}
			locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{op.Identity})
			if err != nil {
				t.Fatal("lock leaked", err)
			}
			if err := locks.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProducerP2PhysicalExclusionBatchAndPathChecks(t *testing.T) {
	s, db, driver, dsn := producerP2Environment(t)
	p := startProducerP2Process(t, s.dataDir, driver, dsn, "locked")
	p.barrier(t, "LOCKED")
	var row model.Resource
	if err := db.First(&row, "id = ?", "window").Error; err != nil {
		t.Fatal(err)
	}
	identity := repository.ResourcePhysicalIdentity(&row, "original")
	if locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{identity}); err == nil {
		locks.Close()
		t.Fatal("second process entered held physical identity")
	}
	other := row
	other.ID, other.ObjectKey = "other", "other"
	op, err := s.repo.BeginLocalResourceProduction(&other, true)
	if err != nil {
		t.Fatal(err)
	}
	locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{op.Identity})
	if err != nil {
		t.Fatal("different identity blocked", err)
	}
	busy := make(chan error, 1)
	go func() {
		second, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{op.Identity})
		if second != nil {
			second.Close()
		}
		busy <- err
	}()
	if err := <-busy; err == nil {
		t.Fatal("same-process independent Flock bypassed exclusion")
	}
	if err := locks.Close(); err != nil {
		t.Fatal(err)
	}
	err = s.withResourceDeletionLocks(nil, []string{other.ID, row.ID}, func(repo *repository.Repository) error {
		return repo.DeleteDetachedResources([]model.Resource{other, row}, nil)
	})
	if err == nil {
		t.Fatal("busy batch committed")
	}
	var stored model.ResourceProducerOperation
	if err := db.First(&stored, "id = ?", op.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Fenced {
		t.Fatal("busy batch partly fenced")
	}
	p.kill(t)
	for _, order := range [][]string{{op.Identity, identity}, {identity, op.Identity}} {
		locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, order)
		if err != nil {
			t.Fatal(err)
		}
		if err := locks.Close(); err != nil {
			t.Fatal(err)
		}
	}
	alias := row
	alias.ObjectKey = "./window"
	if repository.ResourcePhysicalIdentity(&alias, "original") != identity {
		t.Fatal("canonical alias has different lock")
	}
	if runtime.GOOS == "windows" {
		alias.ObjectKey = "WINDOW"
		if repository.ResourcePhysicalIdentity(&alias, "original") != identity {
			t.Fatal("Windows case alias has different lock")
		}
	}
	lockPath := filepath.Join(s.dataDir, ".resource-producer-locks", identity+".lock")
	if err := os.Link(lockPath, lockPath+".linked"); err != nil {
		t.Fatal(err)
	}
	if lock, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{identity}); err == nil {
		lock.Close()
		t.Fatal("hardlinked lock accepted")
	}
	if err := os.Remove(lockPath + ".linked"); err != nil {
		t.Fatal(err)
	}
	locks, err = repository.AcquireResourcePhysicalLocks(s.dataDir, []string{identity})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(lockPath, lockPath+".replaced"); err == nil {
		if err := os.WriteFile(lockPath, nil, 0600); err != nil {
			t.Fatal(err)
		}
		if err := locks.Validate(identity); err == nil {
			t.Fatal("replaced lock identity accepted")
		}
	} else {
		t.Log("OS prevents replacing the open lock file", err)
	}
	if err := locks.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestProducerP2EmptyLockSetAndPublicationToken(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	locks, err := repository.AcquireResourcePhysicalLocks(filepath.Join(s.dataDir, "absent", "root"), nil)
	if err != nil {
		t.Fatal("empty remote-only set depends on local root", err)
	}
	defer locks.Close()
	row := model.Resource{ID: "late-local", UserID: "p2-owner", Provider: "local", ObjectKey: "late-local", Status: model.ResourceStatusPending}
	op, err := s.repo.BeginLocalResourceProduction(&row, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.repo.WithResourcePhysicalLocks(locks).DeleteResource(row.UserID, row.ID); !errors.Is(err, repository.ErrResourceCleanupSetChanged) {
		t.Fatalf("empty snapshot failed to detect new local set: %v", err)
	}
	row.Status = model.ResourceStatusReady
	if err := s.repo.SaveResource(&row); err == nil {
		t.Fatal("no-token Save published new protocol")
	}
	if err := s.repo.SaveResourceWithinStorageLimit(&row, 1<<30, 1<<30, ""); err == nil {
		t.Fatal("no-token quota Save published")
	}
	var saved model.ResourceProducerOperation
	if err := db.First(&saved, "id = ?", op.ID).Error; err != nil {
		t.Fatal(err)
	}
	if saved.Fenced || saved.PhysicalState != "reserved" {
		t.Fatal("token refusal changed obligation")
	}
}

func TestProducerP2WorkerLeaseReclaimAndExit(t *testing.T) {
	for _, exit := range []bool{false, true} {
		t.Run(fmt.Sprintf("exit-%t", exit), func(t *testing.T) {
			s, db, driver, dsn := producerP2Environment(t)
			row, _, err := s.storeResource("p2-owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.repo.DeleteResource(row.UserID, row.ID); err != nil {
				t.Fatal(err)
			}
			p := startProducerP2Process(t, s.dataDir, driver, dsn, "worker")
			p.barrier(t, "WORKER")
			if err := db.Model(&model.ResourceDeletionJob{}).Where("1 = 1").Update("lease_expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
				t.Fatal(err)
			}
			job, err := s.repo.ClaimNextResourceDeletionJob("new-worker", time.Minute)
			if err != nil || job == nil {
				t.Fatalf("reclaim: %v", err)
			}
			if err := s.processResourceDeletionJob(job); err == nil {
				t.Fatal("new worker bypassed old physical lock")
			}
			if _, err := os.Stat(filepath.Join(s.dataDir, "resources", row.ObjectKey)); err != nil {
				t.Fatal("busy worker removed bytes", err)
			}
			if exit {
				p.kill(t)
			} else {
				p.resume()
				p.barrier(t, "REJECTED")
				<-p.done
				if p.err != nil {
					t.Fatal(p.err)
				}
			}
			if err := db.Model(&model.ResourceDeletionJob{}).Where("1 = 1").Update("next_attempt_at", time.Now().Add(-time.Minute)).Error; err != nil {
				t.Fatal(err)
			}
			s.drainResourceDeletionJobs(4)
			if _, err := s.repo.Resource(row.ID); err == nil {
				t.Fatal("reclaimed worker did not finish")
			}
		})
	}
}

func TestProducerP2ExpandedCandidateSetRetriesWholeTransaction(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	first := model.Resource{ID: "first", UserID: "p2-owner", Provider: "local", ObjectKey: "first", Status: model.ResourceStatusPending}
	firstOp, err := s.repo.BeginLocalResourceProduction(&first, true)
	if err != nil {
		t.Fatal(err)
	}
	second := first
	second.ID, second.ObjectKey = "second", "second"
	if _, err := s.repo.BeginLocalResourceProduction(&second, true); err != nil {
		t.Fatal(err)
	}
	asset := model.Asset{ID: "changing", UserID: first.UserID, PayloadJSON: `{"resourceId":"first"}`}
	if err := db.Create(&asset).Error; err != nil {
		t.Fatal(err)
	}
	attempts := 0
	err = s.withResourceDeletionLocks([]string{asset.ID}, nil, func(repo *repository.Repository) error {
		attempts++
		if attempts == 1 {
			if err := db.Model(&asset).Update("payload_json", `{"resourceIds":["first","second"]}`).Error; err != nil {
				return err
			}
		}
		var op model.ResourceProducerOperation
		if err := db.First(&op, "id = ?", firstOp.ID).Error; err != nil {
			return err
		}
		if op.Fenced {
			return errors.New("previous failed admission partly fenced")
		}
		return repo.DeleteAssetsAndResources(first.UserID, []string{asset.ID}, nil, nil, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("candidate retries=%d", attempts)
	}
	for _, id := range []string{first.ID, second.ID} {
		row, err := s.repo.Resource(id)
		if err != nil || row.Status != model.ResourceStatusDeleting {
			t.Fatalf("set not atomically staged: %+v %v", row, err)
		}
	}
}

func TestProducerP2UnsupportedObligationsStayUnresolved(t *testing.T) {
	for _, kind := range []string{"legacy-local", "remote", "playback", "callback"} {
		t.Run(kind, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			var row *model.Resource
			if kind == "playback" || kind == "callback" {
				var err error
				if kind == "callback" {
					row, _, err = s.storeResourceWithWriter("p2-owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true, func(*model.Resource, string, io.Reader) (resourceWriteResult, error) {
						return resourceWriteResult{}, nil
					})
				} else {
					row, _, err = s.storeResource("p2-owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true)
				}
				if err != nil {
					t.Fatal(err)
				}
				if kind == "playback" {
					if _, err := s.repo.ClaimPlaybackProduction(row.ID); err != nil {
						t.Fatal(err)
					}
				}
			} else {
				row = &model.Resource{ID: kind, UserID: "p2-owner", Provider: "local", ObjectKey: kind, Status: model.ResourceStatusPending}
				if kind == "remote" {
					row.Provider = "s3"
					s.dataDir = filepath.Join(s.dataDir, "unavailable", "root")
				}
				if _, err := s.repo.BeginResourceProduction(row, "original", true); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.withResourceDeletionLocks(nil, []string{row.ID}, func(repo *repository.Repository) error { return repo.DeleteResource(row.UserID, row.ID) }); err == nil {
				t.Fatal("unsupported producer recovered")
			}
			var operations []model.ResourceProducerOperation
			if err := db.Where("resource_id = ?", row.ID).Find(&operations).Error; err != nil {
				t.Fatal(err)
			}
			for _, op := range operations {
				if op.Proof == "local-lock-recovered" || op.Fenced {
					t.Fatal("unsupported obligation mutated")
				}
				if kind == "callback" && op.Protocol != "producer-accounting-v1" {
					t.Fatal("callback borrowed new protocol")
				}
			}
		})
	}
}

func TestProducerP2InvalidLockDirectoryRetainsPending(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	if err := os.WriteFile(filepath.Join(s.dataDir, ".resource-producer-locks"), []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.storeResource("p2-owner", "file", "data.bin", "application/octet-stream", 4, 0, 0, 0, strings.NewReader("data"), nil, true); err == nil {
		t.Fatal("invalid lock directory accepted")
	}
	var op model.ResourceProducerOperation
	if err := db.First(&op).Error; err != nil {
		t.Fatal(err)
	}
	if op.PhysicalState != "reserved" || op.Fenced {
		t.Fatalf("lock failure fabricated settlement: %+v", op)
	}
}

func TestProducerP2SymlinkAndPermissionChecks(t *testing.T) {
	t.Run("symlink-root", func(t *testing.T) {
		root := t.TempDir()
		target := filepath.Join(root, "real")
		if err := os.Mkdir(target, 0700); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(root, "alias")
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("environment cannot create test symlink: %v", err)
		}
		id := repository.ResourcePhysicalIdentity(&model.Resource{Provider: "local", ObjectKey: "x"}, "original")
		if lock, err := repository.AcquireResourcePhysicalLocks(link, []string{id}); err == nil {
			lock.Close()
			t.Fatal("symlink root accepted")
		}
	})
	t.Run("readonly-lock-file", func(t *testing.T) {
		root := t.TempDir()
		id := repository.ResourcePhysicalIdentity(&model.Resource{Provider: "local", ObjectKey: "x"}, "original")
		lock, err := repository.AcquireResourcePhysicalLocks(root, []string{id})
		if err != nil {
			t.Fatal(err)
		}
		if err := lock.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, ".resource-producer-locks", id+".lock")
		if err := os.Chmod(path, 0400); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(path, 0600)
		probe, err := os.OpenFile(path, os.O_RDWR, 0600)
		if err == nil {
			probe.Close()
			t.Skip("privileged environment bypasses readonly-file permission; rejection not verified")
		}
		if lock, err := repository.AcquireResourcePhysicalLocks(root, []string{id}); err == nil {
			lock.Close()
			t.Fatal("unwritable lock file accepted")
		}
	})
}

func TestProducerP2RetryTokensDoNotDependOnClock(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	row := model.Resource{ID: "retry-clock", UserID: "p2-owner", Provider: "local", ObjectKey: "clock", Status: model.ResourceStatusFailed, Kind: "file", MimeType: "application/octet-stream", Size: 4}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if ok, err := s.repo.ClaimFailedResourceUpload(row.UserID, row.ID); err != nil || !ok {
		t.Fatalf("claim %v", err)
	}
	row.Status = model.ResourceStatusPending
	_, op, err := s.writeStoredResourceProduction(&row, nil, "clock", strings.NewReader("data"))
	if err != nil {
		t.Fatal(err)
	}
	var operations []model.ResourceProducerOperation
	if err := db.Where("resource_id = ?", row.ID).Find(&operations).Error; err != nil {
		t.Fatal(err)
	}
	for _, item := range operations {
		t.Logf("protocol=%s proof=%s created=%s", item.Protocol, item.Proof, item.CreatedAt.Format(time.RFC3339Nano))
	}
	// Database timestamp precision must not be an authority order. The legacy
	// marker and the admitted attempt can legally share exactly the same tick.
	tick := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := db.Model(&model.ResourceProducerOperation{}).Where("resource_id = ?", row.ID).Update("created_at", tick).Error; err != nil {
		t.Fatal(err)
	}
	row.Status = model.ResourceStatusFailed
	if err := s.publishResourceProduction(&row, op, ""); err != nil {
		t.Fatalf("same-tick legacy marker rejected current failure publication: %v", err)
	}
	if ok, err := s.repo.ClaimFailedResourceUpload(row.UserID, row.ID); err != nil || !ok {
		t.Fatalf("retry claim %v", err)
	}
	row.Status = model.ResourceStatusPending
	_, next, err := s.writeStoredResourceProduction(&row, nil, "clock", strings.NewReader("next"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.ResourceProducerOperation{}).Where("resource_id = ?", row.ID).Update("created_at", tick).Error; err != nil {
		t.Fatal(err)
	}
	row.Status = model.ResourceStatusReady
	if err := s.publishResourceProduction(&row, op, ""); err == nil {
		t.Fatal("old same-tick token published over next attempt")
	}
	if err := s.publishResourceProduction(&row, next, ""); err != nil {
		t.Fatal(err)
	}
	if err := s.withResourceDeletionLocks(nil, []string{row.ID}, func(repo *repository.Repository) error { return repo.DeleteResource(row.UserID, row.ID) }); err == nil {
		t.Fatal("legacy unknown was washed by retries")
	}
}

func TestProducerP2KilledWriterCanBeRecovered(t *testing.T) {
	root := t.TempDir()
	dsn := filepath.Join(root, "state.sqlite") + "?_journal_mode=WAL&_busy_timeout=1000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	t.Cleanup(func() { _ = sqlDB.Close() })
	s := &Service{repo: repository.New(db), dataDir: root}
	cmd := exec.Command(os.Args[0], "-test.run=^TestProducerP2Child$", "-test.v")
	cmd.Env = append(os.Environ(), "PRODUCER_P2_CHILD=write", "PRODUCER_P2_DSN="+dsn, "PRODUCER_P2_ROOT="+root)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = stdin.Close(); _ = cmd.Process.Kill(); _ = cmd.Wait() })
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "P2-WRITING") {
				ready <- nil
				return
			}
		}
		ready <- fmt.Errorf("child exited before writer barrier: %v", scanner.Err())
	}()
	select {
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("child barrier timeout")
	}
	var row model.Resource
	if err := db.First(&row).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.cleanupDetachedUserResources(row.UserID, []model.Resource{row}); err == nil {
		t.Fatal("live writer deletion accepted")
	}
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	if err := s.cleanupDetachedUserResources(row.UserID, []model.Resource{row}); err != nil {
		t.Fatalf("dead new-protocol writer was not recovered: %v", err)
	}
	s.drainResourceDeletionJobs(4)
	if _, err := s.repo.Resource(row.ID); err == nil {
		t.Fatal("recovered row survived")
	}
	if _, err := os.Stat(filepath.Join(root, "resources", row.ObjectKey)); !os.IsNotExist(err) {
		t.Fatalf("partial object survived: %v", err)
	}
}
