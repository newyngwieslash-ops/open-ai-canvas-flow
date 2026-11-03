package app

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/google/uuid"
	"golang.org/x/sys/windows"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func c1Event(t *testing.T, name string) windows.Handle {
	t.Helper()
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		t.Fatal(err)
	}
	h, err := windows.CreateEvent(nil, 1, 0, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = windows.CloseHandle(h) })
	return h
}
func c1Await(t *testing.T, h windows.Handle) {
	t.Helper()
	status, err := windows.WaitForSingleObject(h, 15000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("event barrier %d %v", status, err)
	}
}
func c1WriterArgs(root, mode, event string, output ...string) []string {
	return append([]string{"-test.run=^TestProducerP3C1WriterChild$", "--", "C1-WRITER", root, mode, event}, output...)
}

func c1AppendEvent(root, event string) error {
	file, err := os.OpenFile(filepath.Join(root, "c1.events"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	_, err = file.WriteString(event + "\n")
	return errors.Join(err, file.Close())
}

func TestProducerP3C1WriterChild(t *testing.T) {
	at := -1
	for i, arg := range os.Args {
		if arg == "C1-WRITER" {
			at = i
			break
		}
	}
	if at < 0 {
		return
	}
	root, mode, event := os.Args[at+1], os.Args[at+2], os.Args[at+3]
	var output []string
	if len(os.Args) > at+4 {
		output = []string{os.Args[at+4]}
	}
	if mode == "tree" {
		child := exec.Command(os.Args[0], c1WriterArgs(root, "leaf", event+"-leaf", output...)...)
		if err := child.Start(); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "leaf.pid"), []byte(strconv.Itoa(child.Process.Pid)), 0600); err != nil {
			t.Fatal(err)
		}
		// This same Job does not grant breakaway. A successful escape is a real
		// platform failure, reported to the parent rather than silently ignored.
		escape := exec.Command(os.Args[0], c1WriterArgs(root, "exit", event+"-escape")...)
		escape.SysProcAttr = &windows.SysProcAttr{CreationFlags: windows.CREATE_BREAKAWAY_FROM_JOB}
		result := "denied"
		if err := escape.Start(); err == nil {
			result = "escaped"
			_ = escape.Wait()
		}
		if err := os.WriteFile(filepath.Join(root, "escape.result"), []byte(result), 0600); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(root, mode+".written")
	if len(output) == 1 {
		file = output[0]
	}
	if err := os.WriteFile(file, []byte("writer performed IO"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := c1AppendEvent(root, "WRITE "+mode); err != nil {
		t.Fatal(err)
	}
	ready, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, mustC1UTF16(t, event))
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetEvent(ready); err != nil {
		t.Fatal(err)
	}
	_ = windows.CloseHandle(ready)
	if mode == "exit" {
		return
	}
	for {
		if err := os.WriteFile(file, []byte(fmt.Sprintf("writer %d", time.Now().UnixNano())), 0600); err != nil {
			t.Fatal(err)
		}
		if err := c1AppendEvent(root, "WRITE "+mode); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func mustC1UTF16(t *testing.T, s string) *uint16 {
	t.Helper()
	p, err := windows.UTF16PtrFromString(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func c1JobMember(process, job windows.Handle) (bool, error) {
	pid, err := windows.GetProcessId(process)
	if err != nil {
		return false, err
	}
	var ids struct {
		Assigned, Count uint32
		IDs             [32]uintptr
	}
	if err := windows.QueryInformationJobObject(job, windows.JobObjectBasicProcessIdList, uintptr(unsafe.Pointer(&ids)), uint32(unsafe.Sizeof(ids)), nil); err != nil {
		return false, err
	}
	for i := uint32(0); i < ids.Count && i < 32; i++ {
		if ids.IDs[i] == uintptr(pid) {
			return true, nil
		}
	}
	return false, nil
}

func c1Fixture(t *testing.T, s *Service, id string) *model.Resource {
	t.Helper()
	row := &model.Resource{ID: id, UserID: "c1-owner", Provider: "local", ObjectKey: id + ".bin", Status: model.ResourceStatusReady}
	if err := os.MkdirAll(filepath.Join(s.dataDir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "resources", row.ObjectKey), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, s, row)
	return row
}
func c1Controller(t *testing.T, s *Service) *c1PlaybackController {
	t.Helper()
	c, err := newC1PlaybackController(s.repo, s.dataDir, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	s.playbackControl = c
	t.Cleanup(func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		for _, domain := range c.domains {
			_, _ = domain.result(true)
			_ = domain.close()
		}
	})
	return c
}

func TestProducerP3C1AtomicJobDescendantsAndEscape(t *testing.T) {
	root := t.TempDir()
	event := "Local\\c1-" + uuid.NewString()
	ready := c1Event(t, event)
	leafReady := c1Event(t, event+"-leaf")
	domain, err := newPlaybackControlDomain()
	if err != nil {
		t.Fatal(err)
	}
	defer domain.close()
	defer domain.result(true)
	if err := domain.start(os.Args[0], c1WriterArgs(root, "tree", event), root); err != nil {
		t.Fatal(err)
	}
	c1Await(t, ready)
	c1Await(t, leafReady)
	d := domain.(*windowsPlaybackDomain)
	if member, err := c1JobMember(d.process, d.job); err != nil || !member {
		t.Fatalf("direct member: %v %v", member, err)
	}
	pidBytes, err := os.ReadFile(filepath.Join(root, "leaf.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	observer, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer windows.CloseHandle(observer)
	if member, err := c1JobMember(observer, d.job); err != nil || !member {
		t.Fatalf("descendant member: %v %v", member, err)
	}
	escape, err := os.ReadFile(filepath.Join(root, "escape.result"))
	if err != nil || string(escape) != "denied" {
		t.Fatalf("breakaway outcome %s %v", escape, err)
	}
	result, err := domain.result(true)
	if err != nil || result != "failed" {
		t.Fatalf("stop: %s %v", result, err)
	}
	status, err := windows.WaitForSingleObject(observer, 10000)
	if err != nil || status != windows.WAIT_OBJECT_0 {
		t.Fatalf("descendant remained %d %v", status, err)
	}
	t.Log("atomic root + inherited descendant in original Job; escape denied; entire domain active=0")
}

func TestProducerP3C1ControllerDeletionAndLateLaunch(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	row := c1Fixture(t, s, "running")
	c := c1Controller(t, s)
	op, err := c.claim(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	row.PlaybackObjectKey = row.ID + ".mp4"
	event := "Local\\c1-" + uuid.NewString()
	ready := c1Event(t, event)
	leaf := c1Event(t, event+"-leaf")
	done := make(chan error, 1)
	go func() {
		done <- c.runCommand(op, filepath.Join(s.dataDir, "resources", row.ObjectKey), filepath.Join(s.dataDir, "playback", row.PlaybackObjectKey), c1WriterArgs(s.dataDir, "tree", event, filepath.Join(s.dataDir, "playback", row.PlaybackObjectKey)))
	}()
	c1Await(t, ready)
	c1Await(t, leaf)
	if err := p3bStage(s, row); err != nil {
		t.Fatal(err)
	}
	if err := c1AppendEvent(s.dataDir, "BARRIER_COMMITTED"); err != nil {
		t.Fatal(err)
	}
	job, err := s.repo.ClaimNextResourceDeletionJob("c1-worker", time.Minute)
	if err != nil || job == nil {
		t.Fatalf("claim %v", err)
	}
	// CloseAndStop finishes domain IO without waiting for the runner callback.
	if err := c.CloseAndStop(*op); err != nil {
		t.Fatal(err)
	}
	if err := c1AppendEvent(s.dataDir, "DOMAIN_STOPPED_DURABLE"); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := c.launch(op, c1WriterArgs(s.dataDir, "exit", event)); err == nil {
		t.Fatal("closed operation created late process")
	}
	if err := s.processResourceDeletionJob(job); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		job, err = s.repo.ClaimNextResourceDeletionJob("c1-worker", time.Minute)
		if err != nil {
			t.Fatal(err)
		}
		if job != nil {
			if err := s.processResourceDeletionJob(job); err != nil {
				t.Fatal(err)
			}
		}
	}
	var current model.ResourceProducerOperation
	if err := db.First(&current, "id = ?", op.ID).Error; err != nil {
		t.Fatal(err)
	}
	if current.ControlPhase != "stopped" || current.PhysicalState != "settled" || !current.Fenced {
		t.Fatalf("settlement %+v", current)
	}
	if _, err := os.Stat(filepath.Join(s.dataDir, "resources", row.ObjectKey)); !os.IsNotExist(err) {
		t.Fatalf("original remains: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.dataDir, "playback", row.PlaybackObjectKey)); !os.IsNotExist(err) {
		t.Fatalf("playback remains: %v", err)
	}
	if err := c1AppendEvent(s.dataDir, "SETTLED_AND_REMOVED"); err != nil {
		t.Fatal(err)
	}
	events, err := os.ReadFile(filepath.Join(s.dataDir, "c1.events"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(events)
	barrier := strings.Index(text, "BARRIER_COMMITTED\n")
	stopped := strings.Index(text, "DOMAIN_STOPPED_DURABLE\n")
	removed := strings.Index(text, "SETTLED_AND_REMOVED\n")
	if !strings.Contains(text, "WRITE tree\n") || !strings.Contains(text, "WRITE leaf\n") || barrier < 0 || stopped <= barrier || removed <= stopped || strings.Contains(text[stopped:], "WRITE ") {
		t.Fatalf("invalid independent writer/control ordering:\n%s", text)
	}
	t.Logf("independent writer/control event order: barrier=%d stopped=%d removed=%d; no write after durable whole-domain stop; raw stream:\n%s", barrier, stopped, removed, text)
}

func TestProducerP3C1NoIntentRecoveryAndImmutableResult(t *testing.T) {
	for _, mode := range []string{"armed", "success", "failed", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row := c1Fixture(t, s, "recover")
			c := c1Controller(t, s)
			op, err := c.claim(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "success" || mode == "failed" {
				event := "Local\\c1-" + uuid.NewString()
				_ = c1Event(t, event)
				args := c1WriterArgs(s.dataDir, "exit", event)
				if mode == "failed" {
					args = []string{"-test.run=^not-a-test$", "-invalid-c1-test-flag"}
				}
				if err := c.runCommand(op, filepath.Join(s.dataDir, "resources", row.ObjectKey), filepath.Join(s.dataDir, "playback", row.ID+".mp4"), args); err != nil {
					t.Fatal(err)
				}
			} else if mode == "unknown" {
				gate, err := c.gate(op)
				if err != nil {
					t.Fatal(err)
				}
				repo := s.repo.WithResourcePhysicalLocks(gate)
				if err := repo.TransitionPlaybackControl(op, "armed", "closed", ""); err != nil {
					t.Fatal(err)
				}
				if err := repo.TransitionPlaybackControl(op, "closed", "stopped", "unknown"); err != nil {
					t.Fatal(err)
				}
				_ = gate.Close()
			}
			cold := c1Controller(t, s)
			if err := cold.recover(s); err != nil {
				t.Fatal(err)
			}
			current, err := s.repo.Resource(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := model.PlaybackStatusFailed
			if mode == "success" {
				want = model.PlaybackStatusReady
			}
			if current.PlaybackStatus != want {
				t.Fatalf("recovered %s want %s", current.PlaybackStatus, want)
			}
			persisted, err := s.repo.ControlledPlaybackOperation(op.ID)
			if err != nil {
				t.Fatal(err)
			}
			if persisted.ControlOwner != c.owner || persisted.ControlPhase != "stopped" || persisted.PhysicalState != "settled" {
				t.Fatalf("owner takeover or missing receipt %+v", persisted)
			}
			gate, err := cold.gate(op)
			if err != nil {
				t.Fatal(err)
			}
			err = s.repo.WithResourcePhysicalLocks(gate).TransitionPlaybackControl(op, "closed", "stopped", "success")
			_ = gate.Close()
			if mode != "success" && err == nil {
				t.Fatal("terminal result overwritten")
			}
			var intents int64
			if err := db.Model(&model.ResourceDeletionIntent{}).Count(&intents).Error; err != nil || intents != 0 {
				t.Fatalf("unexpected intent %d %v", intents, err)
			}
		})
	}
}

func TestProducerP3C1ControllerChild(t *testing.T) {
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
	c, err := newC1PlaybackController(s.repo, s.dataDir, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	s.playbackControl = c
	if mode == "recover" {
		err := c.recover(s)
		if err != nil {
			fmt.Println("P2-UNKNOWN")
		} else {
			fmt.Println("P2-RECOVERED")
		}
		return
	}
	if mode == "closer-contended" {
		var op model.ResourceProducerOperation
		if err := db.Where("resource_id = ? AND variant = ?", "crash", "playback").First(&op).Error; err != nil {
			t.Fatal(err)
		}
		if gate, err := c.gate(&op); err == nil {
			_ = gate.Close()
			t.Fatal("foreign process gate contention not observed")
		}
		producerP2Barrier("CONTENDED")
		err := c.CloseAndStop(op)
		current, e := s.repo.ControlledPlaybackOperation(op.ID)
		if e != nil {
			t.Fatal(e)
		}
		if current.ControlPhase != "closed" && current.ControlPhase != "stopped" {
			t.Fatalf("close failed: %s %v", current.ControlPhase, err)
		}
		fmt.Println("P2-CLOSED")
		return
	}
	op, err := c.claim("crash")
	if err != nil || op == nil {
		t.Fatalf("claim %v", err)
	}
	if mode == "armed" {
		producerP2Barrier("ARMED")
		return
	}
	if mode == "gate-launcher" {
		producerP2Barrier("ARMED")
		event := "Local\\c1-" + uuid.NewString()
		ready := c1Event(t, event)
		done := make(chan error, 1)
		go func() {
			done <- c.runCommand(op, filepath.Join(s.dataDir, "resources", "crash.bin"), filepath.Join(s.dataDir, "playback", "crash.mp4"), c1WriterArgs(s.dataDir, "leaf", event))
		}()
		for {
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("closed launch returned success")
				}
				fmt.Println("P2-REJECTED")
				return
			default:
			}
			status, e := windows.WaitForSingleObject(ready, 100)
			if e != nil {
				t.Fatal(e)
			}
			if status == windows.WAIT_OBJECT_0 {
				break
			}
		}
		producerP2Barrier("WRITING")
		if err := c.CloseAndStop(*op); err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if err := c.launch(op, c1WriterArgs(s.dataDir, "exit", event)); err == nil {
			t.Fatal("late launch admitted")
		}
		fmt.Println("P2-STOPPED")
		return
	}
	if mode == "attempted" {
		gate, err := c.gate(op)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.repo.WithResourcePhysicalLocks(gate).TransitionPlaybackControl(op, "armed", "attempted", ""); err != nil {
			t.Fatal(err)
		}
		_ = gate.Close()
		producerP2Barrier("ATTEMPTED")
		return
	}
	if mode == "stopped" {
		event := "Local\\c1-" + uuid.NewString()
		_ = c1Event(t, event)
		if err := c.runCommand(op, filepath.Join(s.dataDir, "resources", "crash.bin"), filepath.Join(s.dataDir, "playback", "crash.mp4"), c1WriterArgs(s.dataDir, "exit", event)); err != nil {
			t.Fatal(err)
		}
		producerP2Barrier("STOPPED")
		return
	}
	event := "Local\\c1-" + uuid.NewString()
	ready := c1Event(t, event)
	done := make(chan error, 1)
	go func() {
		done <- c.runCommand(op, filepath.Join(s.dataDir, "resources", "crash.bin"), filepath.Join(s.dataDir, "playback", "crash.mp4"), c1WriterArgs(s.dataDir, "leaf", event))
	}()
	c1Await(t, ready)
	if mode == "closed" {
		gate, err := c.gate(op)
		if err != nil {
			t.Fatal(err)
		}
		if err := s.repo.WithResourcePhysicalLocks(gate).TransitionPlaybackControl(op, "attempted", "closed", ""); err != nil {
			t.Fatal(err)
		}
		_ = gate.Close()
	}
	producerP2Barrier(strings.ToUpper(mode))
}

func TestProducerP3C1CrossProcessLaunchCloseGate(t *testing.T) {
	for _, first := range []string{"close", "launch"} {
		t.Run(first, func(t *testing.T) {
			s, db, driver, dsn := producerP2Environment(t)
			_ = c1Fixture(t, s, "crash")
			launcher := startProducerP2Process(t, s.dataDir, driver, dsn, "gate-launcher", "TestProducerP3C1ControllerChild")
			launcher.barrier(t, "ARMED")
			if first == "launch" {
				launcher.resume()
				launcher.barrier(t, "WRITING")
			}
			var op model.ResourceProducerOperation
			if err := db.Where("resource_id = ? AND variant = ?", "crash", "playback").First(&op).Error; err != nil {
				t.Fatal(err)
			}
			gate, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{repository.PlaybackControlGateIdentity(op.ID)})
			if err != nil {
				t.Fatal(err)
			}
			closer := startProducerP2Process(t, s.dataDir, driver, dsn, "closer-contended", "TestProducerP3C1ControllerChild")
			closer.barrier(t, "CONTENDED")
			if err := gate.Close(); err != nil {
				t.Fatal(err)
			}
			closer.resume()
			closer.barrier(t, "CLOSED")
			<-closer.done
			if closer.err != nil {
				t.Fatal(closer.err)
			}
			launcher.resume()
			if first == "close" {
				launcher.barrier(t, "REJECTED")
			} else {
				launcher.barrier(t, "STOPPED")
			}
			<-launcher.done
			if launcher.err != nil {
				t.Fatal(launcher.err)
			}
			if first == "close" {
				if _, err := os.Stat(filepath.Join(s.dataDir, "leaf.written")); !os.IsNotExist(err) {
					t.Fatalf("closed gate launched writer: %v", err)
				}
			}
		})
	}
}

func TestProducerP3C1ActualStartupChainUsesConfiguredExecutable(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	row := c1Fixture(t, s, "startup")
	row.Kind = "video"
	stsd := stsdBoxWithFourcc("mp4v")
	moov := make([]byte, 8+len(stsd))
	binary.BigEndian.PutUint32(moov[:4], uint32(len(moov)))
	copy(moov[4:8], "moov")
	copy(moov[8:], stsd)
	if err := os.WriteFile(filepath.Join(s.dataDir, "resources", row.ObjectKey), moov, 0600); err != nil {
		t.Fatal(err)
	}
	if err := db.Model(row).Update("kind", "video").Error; err != nil {
		t.Fatal(err)
	}
	c := c1Controller(t, s)
	// The configured test executable genuinely rejects ffmpeg arguments. The
	// failure must still traverse actual maybeStart -> run -> terminal -> publish.
	s.maybeStartPlaybackTranscode(row)
	deadline := time.Now().Add(15 * time.Second)
	for {
		current, err := s.repo.Resource(row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.PlaybackStatus == model.PlaybackStatusFailed {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("actual chain did not terminate: %+v", current)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var op model.ResourceProducerOperation
	if err := db.Where("resource_id = ? AND variant = ?", row.ID, "playback").First(&op).Error; err != nil {
		t.Fatal(err)
	}
	if op.ControlOwner != c.owner || op.ControlPhase != "stopped" || op.ControlResult != "failed" || op.PhysicalState != "settled" {
		t.Fatalf("bypassed controlled startup: %+v", op)
	}
	if _, err := c.Completion(op); err != nil {
		t.Fatal(err)
	}
}

func TestProducerP3C1InvalidJobNeverStartsFallback(t *testing.T) {
	root := t.TempDir()
	event := "Local\\c1-" + uuid.NewString()
	_ = c1Event(t, event)
	d := &windowsPlaybackDomain{job: windows.InvalidHandle}
	if err := d.start(os.Args[0], c1WriterArgs(root, "exit", event), root); err == nil {
		t.Fatal("invalid Job accepted process")
	}
	if _, err := d.result(true); err == nil {
		t.Fatal("invalid original handle returned terminal proof")
	}
	if _, err := os.Stat(filepath.Join(root, "exit.written")); !os.IsNotExist(err) {
		t.Fatalf("uncontained fallback wrote: %v", err)
	}
}

func TestProducerP3C1CrossProcessCrashRecovery(t *testing.T) {
	for _, mode := range []string{"armed", "attempted", "writing", "closed", "stopped"} {
		t.Run(mode, func(t *testing.T) {
			s, db, driver, dsn := producerP2Environment(t)
			row := c1Fixture(t, s, "crash")
			p := startProducerP2Process(t, s.dataDir, driver, dsn, mode, "TestProducerP3C1ControllerChild")
			p.barrier(t, strings.ToUpper(mode))
			p.kill(t)
			recovery := startProducerP2Process(t, s.dataDir, driver, dsn, "recover", "TestProducerP3C1ControllerChild")
			if mode == "armed" || mode == "stopped" {
				recovery.barrier(t, "RECOVERED")
			} else {
				recovery.barrier(t, "UNKNOWN")
			}
			<-recovery.done
			if recovery.err != nil {
				t.Fatal(recovery.err)
			}
			var op model.ResourceProducerOperation
			if err := db.Where("resource_id = ? AND variant = ?", row.ID, "playback").First(&op).Error; err != nil {
				t.Fatal(err)
			}
			current, err := s.repo.Resource(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "armed" || mode == "stopped" {
				want := model.PlaybackStatusFailed
				if mode == "stopped" {
					want = model.PlaybackStatusReady
				}
				if current.PlaybackStatus != want || op.PhysicalState != "settled" {
					t.Fatalf("recover current=%+v op=%+v", current, op)
				}
			} else {
				if op.PhysicalState == "settled" || current.PlaybackStatus != model.PlaybackStatusProcessing {
					t.Fatalf("guessed lost handle proof %+v %+v", current, op)
				}
				if _, err := os.Stat(filepath.Join(s.dataDir, "resources", row.ObjectKey)); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestProducerP3C1ControlCommitFailureRetainsHandle(t *testing.T) {
	for _, phase := range []string{"closed", "stopped"} {
		t.Run(phase, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row := c1Fixture(t, s, "commit")
			c := c1Controller(t, s)
			op, err := c.claim(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			event := "Local\\c1-" + uuid.NewString()
			ready := c1Event(t, event)
			if err := c.launch(op, c1WriterArgs(s.dataDir, "leaf", event)); err != nil {
				t.Fatal(err)
			}
			c1Await(t, ready)
			injected := errors.New("control commit unavailable")
			name := "c1-control-failure"
			if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table == "resource_producer_operations" {
					if values, ok := tx.Statement.Dest.(map[string]any); ok && values["control_phase"] == phase {
						tx.AddError(injected)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			if err := c.CloseAndStop(*op); !errors.Is(err, injected) {
				t.Fatalf("write failure %v", err)
			}
			if c.domain(op.ID) == nil {
				t.Fatal("lost original handle after commit failure")
			}
			if _, err := c.Completion(*op); err == nil {
				t.Fatal("uncommitted terminal returned receipt")
			}
			if err := db.Callback().Update().Remove(name); err != nil {
				t.Fatal(err)
			}
			if err := c.CloseAndStop(*op); err != nil {
				t.Fatal(err)
			}
			if _, err := c.Completion(*op); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProducerP3C1ControlOwnershipMonotonicRollback(t *testing.T) {
	s, db, _, _ := producerP2Environment(t)
	row := c1Fixture(t, s, "states")
	c := c1Controller(t, s)
	op, err := c.claim(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	gate, err := c.gate(op)
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Close()
	repo := s.repo.WithResourcePhysicalLocks(gate)
	for _, mutation := range []string{"owner", "user", "identity", "protocol", "resource"} {
		other := *op
		switch mutation {
		case "owner":
			other.ControlOwner = "another-controller"
		case "user":
			other.UserID = "other-user"
		case "identity":
			other.Identity = strings.Repeat("0", 64)
		case "protocol":
			other.Protocol = "producer-accounting-v1"
		case "resource":
			other.ResourceID = "another-resource"
		}
		if err := repo.TransitionPlaybackControl(&other, "armed", "attempted", ""); err == nil {
			t.Fatalf("accepted replaced %s", mutation)
		}
	}
	for _, change := range [][3]string{{"armed", "stopped", "success"}, {"armed", "attempted", "success"}, {"armed", "invalid", ""}, {"closed", "armed", ""}} {
		if err := repo.TransitionPlaybackControl(op, change[0], change[1], change[2]); err == nil {
			t.Fatalf("invalid transition admitted %v", change)
		}
	}
	injected := errors.New("terminal persistence unavailable")
	if err := db.Callback().Update().Before("gorm:update").Register("c1-recover-rollback", func(tx *gorm.DB) {
		if tx.Statement.Table == "resource_producer_operations" {
			if values, ok := tx.Statement.Dest.(map[string]any); ok && values["control_phase"] == "stopped" {
				tx.AddError(injected)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RecoverUnstartedPlaybackControl(op); !errors.Is(err, injected) {
		t.Fatalf("rollback %v", err)
	}
	if err := db.Callback().Update().Remove("c1-recover-rollback"); err != nil {
		t.Fatal(err)
	}
	current, err := s.repo.ControlledPlaybackOperation(op.ID)
	if err != nil || current.ControlPhase != "armed" || current.ControlResult != "" {
		t.Fatalf("partial recovery persisted %+v %v", current, err)
	}
	if err := repo.RecoverUnstartedPlaybackControl(op); err != nil {
		t.Fatal(err)
	}
	if err := repo.TransitionPlaybackControl(op, "stopped", "attempted", ""); err == nil {
		t.Fatal("reopened closed launch")
	}
}

func TestProducerP3C1RecoveryRetriesOriginalDomainAfterCommitFailure(t *testing.T) {
	for _, phase := range []string{"closed", "stopped"} {
		t.Run(phase, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row := c1Fixture(t, s, "reentry")
			c := c1Controller(t, s)
			op, err := c.claim(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			injected := errors.New("normal terminal commit temporarily unavailable")
			name := "c1-normal-completion-failure"
			if err := db.Callback().Update().Before("gorm:update").Register(name, func(tx *gorm.DB) {
				if tx.Statement.Table == "resource_producer_operations" {
					if values, ok := tx.Statement.Dest.(map[string]any); ok && values["control_phase"] == phase {
						tx.AddError(injected)
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			event := "Local\\c1-" + uuid.NewString()
			_ = c1Event(t, event)
			if err := c.runCommand(op, filepath.Join(s.dataDir, "resources", row.ObjectKey), filepath.Join(s.dataDir, "playback", row.ID+".mp4"), c1WriterArgs(s.dataDir, "exit", event)); !errors.Is(err, injected) {
				t.Fatalf("injected failure: %v", err)
			}
			if c.domain(op.ID) == nil {
				t.Fatal("original completion handle discarded")
			}
			cold := c1Controller(t, s)
			if err := cold.recover(s); err == nil {
				t.Fatal("new controller guessed original terminal")
			}
			if err := db.Callback().Update().Remove(name); err != nil {
				t.Fatal(err)
			}
			s.playbackControl = c
			if err := c.recover(s); err != nil {
				t.Fatalf("same-owner recovery did not retry retained original domain: %v", err)
			}
			current, err := s.repo.Resource(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.PlaybackStatus != model.PlaybackStatusReady {
				t.Fatalf("lost successful encoder receipt: %+v", current)
			}
			persisted, err := s.repo.ControlledPlaybackOperation(op.ID)
			if err != nil || persisted.ControlPhase != "stopped" || persisted.ControlResult != "success" || persisted.PhysicalState != "settled" {
				t.Fatalf("incomplete recovered receipt %+v %v", persisted, err)
			}
			if c.domain(op.ID) != nil {
				t.Fatal("original handle not released after durable terminal")
			}
		})
	}
}

type c1DeadlineDomain struct {
	playbackControlDomain
	deadline bool
}

func (d *c1DeadlineDomain) result(stop bool) (string, error) {
	if d.deadline {
		return "", os.ErrDeadlineExceeded
	}
	return d.playbackControlDomain.result(stop)
}

func TestProducerP3C1OSFaultsRetainUnsettledEvidence(t *testing.T) {
	for _, fault := range []string{"stop-access-denied", "query-access-denied", "deadline-boundary"} {
		t.Run(fault, func(t *testing.T) {
			s, db, _, _ := producerP2Environment(t)
			row := c1Fixture(t, s, "os-fault")
			c := c1Controller(t, s)
			op, err := c.claim(row.ID)
			if err != nil {
				t.Fatal(err)
			}
			event := "Local\\c1-" + uuid.NewString()
			ready := c1Event(t, event)
			if err := c.launch(op, c1WriterArgs(s.dataDir, "leaf", event)); err != nil {
				t.Fatal(err)
			}
			c1Await(t, ready)
			d := c.domain(op.ID).(*windowsPlaybackDomain)
			var restore func()
			if fault == "deadline-boundary" {
				wrapper := &c1DeadlineDomain{playbackControlDomain: d, deadline: true}
				c.mu.Lock()
				c.domains[op.ID] = wrapper
				c.mu.Unlock()
				restore = func() { wrapper.deadline = false }
			} else {
				rights := uint32(0x0004) // JOB_OBJECT_QUERY
				if fault == "query-access-denied" {
					rights = 0x0008 // JOB_OBJECT_TERMINATE
				}
				original := d.job
				var restricted windows.Handle
				if err := windows.DuplicateHandle(windows.CurrentProcess(), original, windows.CurrentProcess(), &restricted, rights, false, 0); err != nil {
					t.Fatal(err)
				}
				d.mu.Lock()
				d.job = restricted
				d.mu.Unlock()
				// Same original domain is retained here only to restore this
				// deliberate access-right fault, never passed to a cold recovery.
				restore = func() { d.mu.Lock(); d.job = original; d.mu.Unlock(); _ = windows.CloseHandle(restricted) }
			}
			restored := false
			defer func() {
				if !restored {
					restore()
				}
			}()
			err = c.CloseAndStop(*op)
			if fault == "deadline-boundary" {
				if !errors.Is(err, os.ErrDeadlineExceeded) {
					t.Fatalf("deadline boundary %v", err)
				}
			} else if !errors.Is(err, windows.ERROR_ACCESS_DENIED) {
				t.Fatalf("real OS denial %v", err)
			}
			current, err := s.repo.ControlledPlaybackOperation(op.ID)
			if err != nil || current.ControlPhase != "closed" || current.PhysicalState == "settled" {
				t.Fatalf("fault fabricated settlement %+v %v", current, err)
			}
			if _, err := c.Completion(*op); err == nil {
				t.Fatal("fault returned terminal receipt")
			}
			if _, err := os.Stat(filepath.Join(s.dataDir, "resources", row.ObjectKey)); err != nil {
				t.Fatal(err)
			}
			var intents int64
			if err := db.Model(&model.ResourceDeletionIntent{}).Count(&intents).Error; err != nil || intents != 0 {
				t.Fatalf("unexpected deletion %d %v", intents, err)
			}
			restore()
			restored = true
			if err := c.recover(s); err != nil {
				t.Fatalf("same original domain retry %v", err)
			}
			if current, err := s.repo.Resource(row.ID); err != nil || current.PlaybackStatus != model.PlaybackStatusFailed {
				t.Fatalf("failed stop publication %+v %v", current, err)
			}
		})
	}
}

func TestProducerP3C1HardlinkReplacementRejectedBeforeCreate(t *testing.T) {
	s, _, _, _ := producerP2Environment(t)
	row := c1Fixture(t, s, "hardlink")
	c := c1Controller(t, s)
	op, err := c.claim(row.ID)
	if err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(outside, []byte("retain outside"), 0600); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(s.dataDir, "resources", row.ObjectKey)
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(outside, src); err != nil {
		t.Fatal(err)
	}
	event := "Local\\c1-" + uuid.NewString()
	_ = c1Event(t, event)
	if err := c.runCommand(op, src, filepath.Join(s.dataDir, "playback", row.ID+".mp4"), c1WriterArgs(s.dataDir, "exit", event)); !errors.Is(err, os.ErrInvalid) {
		t.Fatalf("hardlink alias accepted %v", err)
	}
	current, err := s.repo.ControlledPlaybackOperation(op.ID)
	if err != nil || current.ControlPhase != "armed" {
		t.Fatalf("OS create attempted after link replacement %+v %v", current, err)
	}
	if len(c.Operations()) != 0 || c.domain(op.ID) != nil {
		t.Fatal("unvalidated path exposed domain capability")
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "retain outside" {
		t.Fatalf("outside object changed %s %v", data, err)
	}
}
