package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func newRemoteSettlementTestService(t *testing.T) (*Service, *gorm.DB, *model.User) {
	t.Helper()
	dbs := profileConcurrencyDatabases(t, "sqlite")
	if err := database.MigrateSchema(dbs[0]); err != nil {
		t.Fatal(err)
	}
	dataDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataDir, ".settings-key"), []byte("01234567890123456789012345678901"), 0o600); err != nil {
		t.Fatal(err)
	}
	admin := &model.User{
		ID:     "admin-remote-1",
		Email:  "admin-remote@example.com",
		Role:   model.UserRoleAdmin,
		Status: model.UserStatusActive,
	}
	if err := dbs[0].Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(dbs[0]), dataDir: dataDir}, dbs[0], admin
}

func TestRemoteStoragePhysicalSettlement_ContractAndAttemptProofs(t *testing.T) {
	svc, db, admin := newRemoteSettlementTestService(t)

	validContract := RemoteStorageEndpointContract{
		ContractID:            "contract-s3-prod-1",
		Provider:              s3Provider,
		ProviderProduct:       "aws-s3",
		ProviderVersion:       "2006-03-01",
		Endpoint:              "https://s3.us-east-1.amazonaws.com",
		Bucket:                "yingce-dedicated-test",
		DedicatedPrefix:       "isolated-p4/user-1/",
		VersioningStatus:      "enabled",
		AllowedOperations:     []string{"PutObject", "CreateMultipartUpload", "UploadPart", "CompleteMultipartUpload", "AbortMultipartUpload", "ListMultipartUploads", "ListObjectVersions", "DeleteObject"},
		MaxRequests:           500,
		MaxBytes:              100 * 1024 * 1024,
		MaxDurationSeconds:    600,
		CredentialRef:         "vault://storage/test-s3-role-ref",
		CleanupOwner:          "storage-ops-team",
		AuthorizationTicket:   "auth-p4-2026-11-02",
		IsLocalCounterexample: false,
	}

	// 1. Contract validation gates: local counterexample, missing ticket, or plaintext secret in CredentialRef must be rejected.
	counterexampleContract := validContract
	counterexampleContract.IsLocalCounterexample = true
	if err := ValidateRemoteEndpointContract(counterexampleContract); err == nil {
		t.Fatal("expected local counterexample contract to be rejected")
	}

	leakedSecretContract := validContract
	leakedSecretContract.CredentialRef = "AKIAIOSFODNN7EXAMPLE:wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	if err := ValidateRemoteEndpointContract(leakedSecretContract); err == nil {
		t.Fatal("expected contract with raw secret in CredentialRef to be rejected")
	}

	if err := ValidateRemoteEndpointContract(validContract); err != nil {
		t.Fatalf("ValidateRemoteEndpointContract(valid) error = %v", err)
	}

	// Create a remote resource with an unsettled ("unknown") producer operation.
	row := model.Resource{
		ID:        "remote-res-1",
		UserID:    "user-1",
		Kind:      "video",
		Provider:  s3Provider,
		Endpoint:  validContract.Endpoint,
		Bucket:    validContract.Bucket,
		ObjectKey: "isolated-p4/user-1/video-1.mp4",
		Status:    model.ResourceStatusReady,
	}
	if err := db.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	op := repository.LegacyResourceProducerIdentity(&row, "original")
	op.ID = "op-remote-1"
	op.PhysicalState = "unknown"
	op.Outcome = "success"
	op.Proof = "no-terminal-proof"
	if err := db.Create(&op).Error; err != nil {
		t.Fatal(err)
	}

	// Confirm deletion is initially blocked by ErrResourceProducerUnsettled.
	if err := svc.repo.DeleteDetachedResources([]model.Resource{row}, nil); !errors.Is(err, repository.ErrResourceProducerUnsettled) {
		t.Fatalf("expected unsettled remote resource to block deletion, got %v", err)
	}

	// 2. Reject settlement when UnknownInitPossible is true but PostBarrierListComplete is false or TruncatedPagesRemaining > 0.
	incompleteScanProof := RemoteProducerSettlementProof{
		Contract:                validContract,
		ResourceID:              row.ID,
		UserID:                  row.UserID,
		Variant:                 "original",
		Provider:                row.Provider,
		Endpoint:                row.Endpoint,
		Bucket:                  row.Bucket,
		ObjectKey:               row.ObjectKey,
		WriterStopBarrier:       true,
		UnknownInitPossible:     true,
		PostBarrierListComplete: true,
		TruncatedPagesRemaining: 1,
		DiscoveredOrphanUploads: []string{"upload-orphan-1"},
		AbortedOrphanUploads:    []string{"upload-orphan-1"},
		Attempts: []RemoteWriteAttemptEvidence{
			{
				AttemptID:                "att-init-lost",
				Kind:                     "multipart_init",
				UploadID:                 "upload-orphan-1",
				ResponseLost:             true,
				TerminalState:            "settled",
				Outcome:                  "aborted",
				AbortConfirmed:           true,
				VerifiedAbsentAfterAbort: true,
			},
			{
				AttemptID:         "att-retry-complete",
				Kind:              "complete_multipart",
				UploadID:          "upload-final-2",
				PartNumbers:       []int{1, 2},
				TerminalState:     "settled",
				Outcome:           "success",
				ProducedVersionID: "v-final-2",
			},
		},
	}
	if err := svc.SettleRemoteResourceProduction(admin, incompleteScanProof); err == nil {
		t.Fatal("expected SettleRemoteResourceProduction with truncated multipart listing to fail")
	}

	// 3. Reject settlement when an orphan upload in DiscoveredOrphanUploads was not aborted.
	unabortedOrphanProof := incompleteScanProof
	unabortedOrphanProof.TruncatedPagesRemaining = 0
	unabortedOrphanProof.DiscoveredOrphanUploads = []string{"upload-orphan-1", "upload-orphan-2"}
	unabortedOrphanProof.AbortedOrphanUploads = []string{"upload-orphan-1"}
	if err := svc.SettleRemoteResourceProduction(admin, unabortedOrphanProof); err == nil {
		t.Fatal("expected SettleRemoteResourceProduction with unaborted orphan upload to fail")
	}

	// 4. Succeed when post-barrier paginated scan is complete, all orphan uploads are aborted & verified absent, and all attempts settled.
	validProof := incompleteScanProof
	validProof.TruncatedPagesRemaining = 0
	if err := svc.SettleRemoteResourceProduction(admin, validProof); err != nil {
		t.Fatalf("SettleRemoteResourceProduction(valid) error = %v", err)
	}

	var updatedOp model.ResourceProducerOperation
	if err := db.First(&updatedOp, "id = ?", op.ID).Error; err != nil {
		t.Fatal(err)
	}
	if updatedOp.PhysicalState != "settled" {
		t.Fatalf("expected producer operation to be settled, got %+v", updatedOp)
	}
}

func TestRemoteStoragePhysicalSettlement_ExhaustionConfigDriftAndRecovery(t *testing.T) {
	svc, db, admin := newRemoteSettlementTestService(t)
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")

	var denyPermission atomic.Bool
	denyPermission.Store(true)
	var deleteCalls atomic.Int32
	var wrongBucketCalls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleteCalls.Add(1)
			if denyPermission.Load() {
				http.Error(w, "AccessDenied", http.StatusForbidden)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	wrongServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			wrongBucketCalls.Add(1)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer wrongServer.Close()

	contract := RemoteStorageEndpointContract{
		ContractID:            "contract-s3-exhaust-1",
		Provider:              s3Provider,
		ProviderProduct:       "aws-s3",
		ProviderVersion:       "2006-03-01",
		Endpoint:              server.URL,
		Bucket:                "maint-versioned-bucket",
		DedicatedPrefix:       "isolated-p4/user-1/",
		VersioningStatus:      "enabled",
		AllowedOperations:     []string{"PutObject", "DeleteObject", "ListObjectVersions", "ListMultipartUploads", "AbortMultipartUpload"},
		MaxRequests:           500,
		MaxBytes:              100 * 1024 * 1024,
		MaxDurationSeconds:    600,
		CredentialRef:         "vault://storage/test-s3-exhaust-ref",
		CleanupOwner:          "storage-ops-team",
		AuthorizationTicket:   "auth-p4-exhaust-2026-11-02",
		IsLocalCounterexample: false,
	}

	// Create 2 remote resources:
	// 1) resSingle: unaliased remote resource on server.URL / maint-versioned-bucket
	// 2) resSharedA & resSharedB: two remote resources sharing the same physical object
	resSingle := model.Resource{
		ID:        "remote-exhaust-single",
		UserID:    "user-1",
		Kind:      "image",
		Provider:  s3Provider,
		Endpoint:  server.URL,
		Bucket:    "maint-versioned-bucket",
		ObjectKey: "isolated-p4/user-1/single.png",
		Status:    model.ResourceStatusReady,
	}
	resSharedA := model.Resource{
		ID:        "remote-shared-a",
		UserID:    "user-1",
		Kind:      "image",
		Provider:  s3Provider,
		Endpoint:  server.URL,
		Bucket:    "maint-versioned-bucket",
		ObjectKey: "isolated-p4/user-1/shared.png",
		Status:    model.ResourceStatusReady,
	}
	resSharedB := model.Resource{
		ID:        "remote-shared-b",
		UserID:    "user-1",
		Kind:      "image",
		Provider:  s3Provider,
		Endpoint:  server.URL,
		Bucket:    "maint-versioned-bucket",
		ObjectKey: "isolated-p4/user-1/shared.png",
		Status:    model.ResourceStatusReady,
	}

	for _, r := range []model.Resource{resSingle, resSharedA, resSharedB} {
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		proof := RemoteProducerSettlementProof{
			Contract:                contract,
			ResourceID:              r.ID,
			UserID:                  r.UserID,
			Variant:                 "original",
			Provider:                r.Provider,
			Endpoint:                r.Endpoint,
			Bucket:                  r.Bucket,
			ObjectKey:               r.ObjectKey,
			WriterStopBarrier:       true,
			UnknownInitPossible:     false,
			PostBarrierListComplete: true,
			Attempts: []RemoteWriteAttemptEvidence{
				{
					AttemptID:         "att-" + r.ID,
					Kind:              "put_object",
					TerminalState:     "settled",
					Outcome:           "success",
					ProducedVersionID: "v1-" + r.ID,
				},
			},
		}
		if err := svc.SettleRemoteResourceProduction(admin, proof); err != nil {
			t.Fatalf("SettleRemoteResourceProduction(%s) error = %v", r.ID, err)
		}
	}

	// Configure system OSS setting initially pointing to the WRONG endpoint/bucket (simulating config drift).
	if err := db.Create(&model.SystemSetting{
		Key:       ossSettingKey,
		ValueJSON: `{"enabled":true,"provider":"s3","endpoint":"` + wrongServer.URL + `","bucket":" drifted-other-bucket","region":"us-east-1","accessKeyId":"ak","accessKeySecret":"sk"}`,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// 1. DeleteMarker-only evidence (payload version still Deleted: false) must be rejected before touching records.
	deleteMarkerOnlyEvidence := RemotePhysicalExhaustionEvidence{
		Contract:               contract,
		ResourceID:             resSingle.ID,
		UserID:                 resSingle.UserID,
		PostDeleteListComplete: true,
		Versions: []RemoteObjectVersionItem{
			{VersionID: "v1-payload", IsDeleteMarker: false, Deleted: false},
			{VersionID: "v2-marker", IsDeleteMarker: true, Deleted: true},
		},
	}
	if _, err := svc.ExecuteRemoteContractDeletion(admin, deleteMarkerOnlyEvidence); err == nil {
		t.Fatal("expected ExecuteRemoteContractDeletion to reject delete-marker-only evidence when payload version remains")
	}

	// 2. With complete exhaustion evidence but drifted system OSS setting (pointing to wrongServer/drifted-other-bucket),
	// ExecuteRemoteContractDeletion must block and NEVER send a delete request to wrongServer.
	exhaustedEvidence := RemotePhysicalExhaustionEvidence{
		Contract:                contract,
		ResourceID:              resSingle.ID,
		UserID:                  resSingle.UserID,
		ActiveMultipartUploads:  []string{"mp-late-1"},
		AbortedMultipartUploads: []string{"mp-late-1"},
		PostDeleteListComplete:  true,
		Versions: []RemoteObjectVersionItem{
			{VersionID: "v1-payload", IsDeleteMarker: false, Deleted: true},
			{VersionID: "v2-marker", IsDeleteMarker: true, Deleted: true},
		},
	}
	if _, err := svc.ExecuteRemoteContractDeletion(admin, exhaustedEvidence); err == nil {
		t.Fatal("expected ExecuteRemoteContractDeletion to reject when active OSS config drifted from resource historical identity")
	}
	if wrongBucketCalls.Load() != 0 {
		t.Fatalf("drifted config caused %d delete calls to wrong server", wrongBucketCalls.Load())
	}

	// Restore matching OSS config for server.URL / maint-versioned-bucket.
	if err := db.Model(&model.SystemSetting{}).Where("key = ?", ossSettingKey).
		Update("value_json", `{"enabled":true,"provider":"s3","endpoint":"`+server.URL+`","bucket":"maint-versioned-bucket","region":"us-east-1","accessKeyId":"ak","accessKeySecret":"sk"}`).Error; err != nil {
		t.Fatal(err)
	}

	// 3. While denyPermission is true (403 Forbidden), ExecuteRemoteContractDeletion stages the deletion intent,
	// attempts physical deletion, fails closed on 403, and retains the Resource record and ResourceDeletionJob!
	res403, err := svc.ExecuteRemoteContractDeletion(admin, exhaustedEvidence)
	if err == nil {
		t.Fatalf("expected 403 permission failure to return error, got result %+v", res403)
	}
	var singleCount int64
	if err := db.Model(&model.Resource{}).Where("id = ?", resSingle.ID).Count(&singleCount).Error; err != nil || singleCount != 1 {
		t.Fatalf("expected resSingle record to be retained on 403 permission failure, count=%d err=%v", singleCount, err)
	}

	// 4. Restore permission (denyPermission = false) and re-enter ExecuteRemoteContractDeletion -> completes physical deletion & removes Resource row!
	denyPermission.Store(false)
	if err := db.Model(&model.ResourceDeletionJob{}).Where("resource_id = ?", resSingle.ID).Update("next_attempt_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	resRecovered, err := svc.ExecuteRemoteContractDeletion(admin, exhaustedEvidence)
	if err != nil {
		t.Fatalf("ExecuteRemoteContractDeletion(recovered) error = %v", err)
	}
	if !resRecovered.Finalized || resRecovered.RetainedPhysicalObject {
		t.Fatalf("expected resSingle to be finalized and physically deleted, got %+v", resRecovered)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", resSingle.ID).Count(&singleCount).Error; err != nil || singleCount != 0 {
		t.Fatalf("expected resSingle DB row to be removed after recovery, count=%d err=%v", singleCount, err)
	}

	// 5. Shared physical alias: deleting resSharedA while resSharedB is still live must retain the physical object (zero additional HTTP DELETE calls) while finalizing resSharedA's DB record!
	beforeSharedDeletes := deleteCalls.Load()
	sharedEvidence := RemotePhysicalExhaustionEvidence{
		Contract:               contract,
		ResourceID:             resSharedA.ID,
		UserID:                 resSharedA.UserID,
		PostDeleteListComplete: true,
		Versions: []RemoteObjectVersionItem{
			{VersionID: "v1-" + resSharedA.ID, IsDeleteMarker: false, Deleted: true},
		},
	}
	sharedRes, err := svc.ExecuteRemoteContractDeletion(admin, sharedEvidence)
	if err != nil {
		t.Fatalf("ExecuteRemoteContractDeletion(resSharedA) error = %v", err)
	}
	if !sharedRes.Finalized || !sharedRes.RetainedPhysicalObject {
		t.Fatalf("expected resSharedA deletion to finalize DB record while retaining shared physical object, got %+v", sharedRes)
	}
	if deleteCalls.Load() != beforeSharedDeletes {
		t.Fatalf("expected 0 physical DELETE calls for shared alias object, got %d", deleteCalls.Load()-beforeSharedDeletes)
	}
	var countA, countB int64
	if err := db.Model(&model.Resource{}).Where("id = ?", resSharedA.ID).Count(&countA).Error; err != nil || countA != 0 {
		t.Fatalf("expected resSharedA row to be removed, count=%d err=%v", countA, err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", resSharedB.ID).Count(&countB).Error; err != nil || countB != 1 {
		t.Fatalf("expected live alias resSharedB row to remain intact, count=%d err=%v", countB, err)
	}
}
