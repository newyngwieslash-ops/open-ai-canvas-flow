package app

import (
	"os"
	"path/filepath"
	"testing"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type c1Repository interface {
	ClaimWindowsControlledPlayback(string, string) (*model.ResourceProducerOperation, error)
	ControlledPlaybackOperation(string) (*model.ResourceProducerOperation, error)
	TransitionPlaybackControl(*model.ResourceProducerOperation, string, string, string) error
	RecoverUnstartedPlaybackControl(*model.ResourceProducerOperation) error
}

// A missing atomic claim would leave reserved rows with no crash recovery path.
func TestProducerP3C1AtomicClaim(t *testing.T) {
	s, _, _, _ := producerP2Environment(t)
	row := &model.Resource{ID: "c1", UserID: "owner", Provider: "local", ObjectKey: "c1.bin", Status: model.ResourceStatusReady}
	if err := os.MkdirAll(filepath.Join(s.dataDir, "resources"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.dataDir, "resources", row.ObjectKey), []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	createClosedLocalFixtureResource(t, s, row)
	r, ok := any(s.repo).(c1Repository)
	if !ok {
		t.Fatal("repository has no atomic controlled claim and monotonic gate")
	}
	op, err := r.ClaimWindowsControlledPlayback(row.ID, "controller-one")
	if err != nil || op == nil {
		t.Fatalf("claim: %v", err)
	}
	if op.ControlOwner != "controller-one" || op.ControlPhase != "armed" || op.PhysicalState != "writing" || op.ControlResult != "" {
		t.Fatalf("nonrecoverable claim: %+v", op)
	}
	if second, err := r.ClaimWindowsControlledPlayback(row.ID, "controller-two"); err != nil || second != nil {
		t.Fatalf("duplicate: %+v %v", second, err)
	}
	if err := r.TransitionPlaybackControl(op, "armed", "attempted", ""); err == nil {
		t.Fatal("transition without launch lock admitted")
	}
	locks, err := repository.AcquireResourcePhysicalLocks(s.dataDir, []string{repository.ResourcePhysicalIdentity(row, "original")})
	if err != nil {
		t.Fatal(err)
	}
	defer locks.Close()
}
