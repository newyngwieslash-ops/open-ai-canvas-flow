package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func newProducerRecoveryTestService(t *testing.T) (*Service, *gorm.DB, *model.User) {
	t.Helper()
	dbs := profileConcurrencyDatabases(t, "sqlite")
	if err := database.MigrateSchema(dbs[0]); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, ".settings-key"), []byte("01234567890123456789012345678901"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, "resources"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dataDir, playbackDirName), 0o755); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{
		ID:     "admin-prod-rec-1",
		Email:  "admin-prod-rec@example.com",
		Role:   model.UserRoleAdmin,
		Status: model.UserStatusActive,
	}
	if err := dbs[0].Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(dbs[0]), dataDir: dataDir}, dbs[0], admin
}

func seedControlledPlaybackInAttemptedState(t *testing.T, svc *Service, db *gorm.DB, resID, userID, owner string) (model.Resource, model.ResourceProducerOperation) {
	t.Helper()
	row := model.Resource{
		ID:                resID,
		UserID:            userID,
		Kind:              "video",
		Provider:          "local",
		ObjectKey:         userID + "/" + resID + ".mov",
		PlaybackObjectKey: resID + ".mp4",
		PlaybackStatus:    model.PlaybackStatusProcessing,
		Status:            model.ResourceStatusReady,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	origOp := repository.LegacyResourceProducerIdentity(&row, "original")
	origOp.ID = "op-orig-" + resID
	origOp.Protocol = repository.LocalGoProducerProtocol
	origOp.PhysicalState = "settled"
	origOp.Outcome = "success"
	origOp.Proof = "local-writer-returned"
	if err := db.Create(&origOp).Error; err != nil {
		t.Fatal(err)
	}

	playOp := repository.LegacyResourceProducerIdentity(&row, "playback")
	playOp.ID = "op-play-" + resID
	playOp.Protocol = repository.ControlledPlaybackProtocol
	playOp.PhysicalState = "writing"
	playOp.ControlOwner = owner
	playOp.ControlPhase = "attempted"
	playOp.ControlResult = ""
	if err := db.Create(&playOp).Error; err != nil {
		t.Fatal(err)
	}

	writeLegacyLocalResourceFile(t, svc, row.ObjectKey, "source-video-"+resID)
	playPath := filepath.Join(svc.dataDir, playbackDirName, row.PlaybackObjectKey)
	if err := os.WriteFile(playPath, []byte("partial-or-complete-mp4-"+resID), 0o644); err != nil {
		t.Fatal(err)
	}
	return row, playOp
}

func TestResourceProducerFullRecovery_EnvironmentAndPlatformGates(t *testing.T) {
	svc, db, admin := newProducerRecoveryTestService(t)
	row, playOp := seedControlledPlaybackInAttemptedState(t, svc, db, "vid-gate-1", "user-1", "crashed-ctrl-1")

	validLinuxEnv := ProducerRecoveryEnvironmentContract{
		EnvironmentID:        "vm-c2a-linux-01",
		Platform:             "linux",
		ImageSHA256:          "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		IsolatedRootPath:     svc.dataDir,
		AdminOwner:           "kernel-sec-ops",
		NamespaceUIDIsolated: true,
		NoNewPrivileges:      true,
		CustodyVerified:      true,
		AuthorizationTicket:  "auth-c2a-2026-11-02",
		CandidateOnly:        false,
	}

	// 1. CandidateOnly environment or missing CustodyVerified / NamespaceUIDIsolated must be rejected before touching state.
	candidateEnv := validLinuxEnv
	candidateEnv.CandidateOnly = true
	if err := ValidateProducerRecoveryEnvironment(candidateEnv); err == nil {
		t.Fatal("expected CandidateOnly environment to be rejected")
	}

	noCustodyEnv := validLinuxEnv
	noCustodyEnv.CustodyVerified = false
	if err := ValidateProducerRecoveryEnvironment(noCustodyEnv); err == nil {
		t.Fatal("expected environment without CustodyVerified to be rejected")
	}

	if err := ValidateProducerRecoveryEnvironment(validLinuxEnv); err != nil {
		t.Fatalf("ValidateProducerRecoveryEnvironment(validLinuxEnv) error = %v", err)
	}

	baseProof := ProducerDomainRecoveryProof{
		Environment:                   validLinuxEnv,
		OperationID:                   playOp.ID,
		ResourceID:                    row.ID,
		UserID:                        row.UserID,
		ExpectedControlOwner:          playOp.ControlOwner,
		OriginalGenerationID:          "gen-c2a-1",
		ProofPlatform:                 "linux",
		AllControllersExited:          true,
		BorrowedLegacyHandle:          false,
		RecreatedEmptyDomain:          false,
		SameNameJobAssumed:            false,
		AllOriginalHandlesLost:        false,
		ParentFilesStructContinuous:   true,
		LateJoinSealed:                true,
		AllWritersExited:              true,
		VerifiedEncoderSuccessReceipt: true,
		EncoderReceiptLost:            false,
	}

	// 2. Reject cross-platform extrapolation (Windows environment claiming Linux C2-A proof).
	winEnv := validLinuxEnv
	winEnv.Platform = "windows"
	crossPlatformProof := baseProof
	crossPlatformProof.Environment = winEnv
	crossPlatformProof.ProofPlatform = "linux"
	if _, err := svc.RecoverAndSettleProducerOperation(admin, crossPlatformProof); err == nil {
		t.Fatal("expected cross-platform Linux proof on Windows environment to be rejected")
	}

	// 3. Reject Windows recovery when AllOriginalHandlesLost is true or SameNameJobAssumed is true, preserving ErrResourceProducerUnsettled!
	winLostHandlesProof := baseProof
	winLostHandlesProof.Environment = winEnv
	winLostHandlesProof.ProofPlatform = "windows"
	winLostHandlesProof.AllOriginalHandlesLost = true
	winLostHandlesProof.SameNameJobAssumed = true
	if _, err := svc.RecoverAndSettleProducerOperation(admin, winLostHandlesProof); !errors.Is(err, repository.ErrResourceProducerUnsettled) {
		t.Fatalf("expected Windows lost-handles recovery to fail closed with ErrResourceProducerUnsettled, got %v", err)
	}

	// 4. Reject recreated empty domain or borrowed legacy handle on Linux.
	recreatedEmptyProof := baseProof
	recreatedEmptyProof.RecreatedEmptyDomain = true
	if _, err := svc.RecoverAndSettleProducerOperation(admin, recreatedEmptyProof); !errors.Is(err, repository.ErrResourceProducerUnsettled) {
		t.Fatalf("expected RecreatedEmptyDomain to fail closed with ErrResourceProducerUnsettled, got %v", err)
	}

	// Confirm the operation is still in "writing" / "attempted" and deletion is still blocked.
	if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); !errors.Is(err, repository.ErrResourceProducerUnsettled) {
		t.Fatalf("expected unsettled playback operation to block deletion, got %v", err)
	}
}

func TestResourceProducerFullRecovery_ConcurrentIsolationAndLostReceipt(t *testing.T) {
	svc, db, admin := newProducerRecoveryTestService(t)

	// Create 3 concurrent controlled playback operations:
	// 1) resLostReceipt: writers exited & late join sealed, decodable file exists on disk, BUT encoder receipt was lost -> MUST settle as failed and NEVER publish PlaybackStatusReady, then allow clean deletion!
	// 2) resSuccessReceipt: writers exited & late join sealed, verified encoder receipt exists -> settles as success and publishes PlaybackStatusReady!
	// 3) resConcurrentUnsettled: another concurrent operation that has NOT been recovered -> MUST remain untouched in "writing"/"attempted" and still blocked from deletion!
	resLostReceipt, opLostReceipt := seedControlledPlaybackInAttemptedState(t, svc, db, "vid-lost-receipt", "user-1", "crashed-ctrl-a")
	resSuccessReceipt, opSuccessReceipt := seedControlledPlaybackInAttemptedState(t, svc, db, "vid-ok-receipt", "user-1", "crashed-ctrl-b")
	resConcurrentUnsettled, opConcurrentUnsettled := seedControlledPlaybackInAttemptedState(t, svc, db, "vid-still-writing", "user-2", "crashed-ctrl-c")

	linuxEnv := ProducerRecoveryEnvironmentContract{
		EnvironmentID:        "vm-c2a-linux-02",
		Platform:             "linux",
		ImageSHA256:          "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		IsolatedRootPath:     svc.dataDir,
		AdminOwner:           "kernel-sec-ops",
		NamespaceUIDIsolated: true,
		NoNewPrivileges:      true,
		CustodyVerified:      true,
		AuthorizationTicket:  "auth-c2a-concurrent-2026-11-02",
		CandidateOnly:        false,
	}

	// 1. Recover resLostReceipt (EncoderReceiptLost: true, DecodableOutputFileExists: true).
	lostResult, err := svc.RecoverAndSettleProducerOperation(admin, ProducerDomainRecoveryProof{
		Environment:                   linuxEnv,
		OperationID:                   opLostReceipt.ID,
		ResourceID:                    resLostReceipt.ID,
		UserID:                        resLostReceipt.UserID,
		ExpectedControlOwner:          opLostReceipt.ControlOwner,
		OriginalGenerationID:          "gen-lost-1",
		ProofPlatform:                 "linux",
		AllControllersExited:          true,
		ParentFilesStructContinuous:   true,
		LateJoinSealed:                true,
		AllWritersExited:              true,
		VerifiedEncoderSuccessReceipt: false,
		EncoderReceiptLost:            true,
		DecodableOutputFileExists:     true,
	})
	if err != nil {
		t.Fatalf("RecoverAndSettleProducerOperation(lostReceipt) error = %v", err)
	}
	if lostResult.PublishedReady || lostResult.PlaybackStatus != model.PlaybackStatusFailed || lostResult.PhysicalState != "settled" {
		t.Fatalf("lost receipt recovery must NOT publish ready; got %+v", lostResult)
	}
	var checkLostRow model.Resource
	if err := db.First(&checkLostRow, "id = ?", resLostReceipt.ID).Error; err != nil {
		t.Fatal(err)
	}
	if checkLostRow.PlaybackStatus != model.PlaybackStatusFailed {
		t.Fatalf("expected resLostReceipt PlaybackStatus=failed, got %s", checkLostRow.PlaybackStatus)
	}

	// 2. Recover resSuccessReceipt (VerifiedEncoderSuccessReceipt: true, EncoderReceiptLost: false).
	okResult, err := svc.RecoverAndSettleProducerOperation(admin, ProducerDomainRecoveryProof{
		Environment:                   linuxEnv,
		OperationID:                   opSuccessReceipt.ID,
		ResourceID:                    resSuccessReceipt.ID,
		UserID:                        resSuccessReceipt.UserID,
		ExpectedControlOwner:          opSuccessReceipt.ControlOwner,
		OriginalGenerationID:          "gen-ok-2",
		ProofPlatform:                 "linux",
		AllControllersExited:          true,
		ParentFilesStructContinuous:   true,
		LateJoinSealed:                true,
		AllWritersExited:              true,
		VerifiedEncoderSuccessReceipt: true,
		EncoderReceiptLost:            false,
		DecodableOutputFileExists:     true,
	})
	if err != nil {
		t.Fatalf("RecoverAndSettleProducerOperation(successReceipt) error = %v", err)
	}
	if !okResult.PublishedReady || okResult.PlaybackStatus != model.PlaybackStatusReady || okResult.PhysicalState != "settled" {
		t.Fatalf("expected verified receipt recovery to publish ready; got %+v", okResult)
	}

	// 3. Verify concurrent isolation: opConcurrentUnsettled must still be in "writing" / "attempted" and blocked from deletion!
	var checkConcurrentOp model.ResourceProducerOperation
	if err := db.First(&checkConcurrentOp, "id = ?", opConcurrentUnsettled.ID).Error; err != nil {
		t.Fatal(err)
	}
	if checkConcurrentOp.PhysicalState != "writing" || checkConcurrentOp.ControlPhase != "attempted" {
		t.Fatalf("concurrent operation was mutated by other recoveries: %+v", checkConcurrentOp)
	}
	if err := svc.repo.DeleteDetachedResources([]model.Resource{resConcurrentUnsettled}, nil); !errors.Is(err, repository.ErrResourceProducerUnsettled) {
		t.Fatalf("expected unrecovered concurrent resource to remain blocked from deletion, got %v", err)
	}

	// 4. Verify resLostReceipt (now physically settled) can be cleanly deleted through the real deletion entrypoint + worker!
	delRes, err := svc.DeleteAdminResources(admin, AdminResourceDeleteRequest{
		ResourceIDs: []string{resLostReceipt.ID},
	})
	if err != nil {
		t.Fatalf("DeleteAdminResources(resLostReceipt) error = %v", err)
	}
	if len(delRes.Deleted) != 1 || delRes.Deleted[0] != resLostReceipt.ID {
		t.Fatalf("expected resLostReceipt to be admitted for deletion, got %+v", delRes)
	}
	svc.drainResourceDeletionJobs(4)
	var countLost int64
	if err := db.Model(&model.Resource{}).Where("id = ?", resLostReceipt.ID).Count(&countLost).Error; err != nil || countLost != 0 {
		t.Fatalf("expected resLostReceipt DB row to be finalized and deleted after recovery, count=%d err=%v", countLost, err)
	}
}
