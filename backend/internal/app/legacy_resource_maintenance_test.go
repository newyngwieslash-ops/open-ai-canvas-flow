package app

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func newLegacyMaintenanceTestService(t *testing.T) (*Service, *gorm.DB, *model.User) {
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
	admin := &model.User{
		ID:     "admin-maint-1",
		Email:  "admin-maint@example.com",
		Role:   model.UserRoleAdmin,
		Status: model.UserStatusActive,
	}
	if err := dbs[0].Create(admin).Error; err != nil {
		t.Fatal(err)
	}
	return &Service{repo: repository.New(dbs[0]), dataDir: dataDir}, dbs[0], admin
}

func writeLegacyLocalResourceFile(t *testing.T, svc *Service, key string, content string) string {
	t.Helper()
	fullPath := filepath.Join(svc.dataDir, "resources", filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return fullPath
}

func TestLegacyResourceEvidenceMaintenance_ManifestProvenanceAndDryRun(t *testing.T) {
	svc, db, admin := newLegacyMaintenanceTestService(t)

	resEligible := model.Resource{
		ID:              "legacy-eligible-1",
		UserID:          "user-1",
		Kind:            "image",
		Provider:        "local",
		ObjectKey:       "user-1/legacy-eligible-1.png",
		Status:          model.ResourceStatusReady,
		ThumbnailStatus: "ready",
		ThumbnailSize:   2048,
	}
	resUnknownThumb := model.Resource{
		ID:              "legacy-unknown-thumb",
		UserID:          "user-1",
		Kind:            "image",
		Provider:        "local",
		ObjectKey:       "user-1/legacy-unknown-thumb.png",
		Status:          model.ResourceStatusReady,
		ThumbnailStatus: "ready",
		ThumbnailSize:   4096,
	}
	resWeakWriter := model.Resource{
		ID:        "legacy-weak-writer",
		UserID:    "user-1",
		Kind:      "image",
		Provider:  "local",
		ObjectKey: "user-1/legacy-weak-writer.png",
		Status:    model.ResourceStatusReady,
	}
	resDrifted := model.Resource{
		ID:        "legacy-drifted",
		UserID:    "user-1",
		Kind:      "image",
		Provider:  "local",
		ObjectKey: "user-1/legacy-drifted.png",
		Status:    model.ResourceStatusReady,
	}
	resCrossUser := model.Resource{
		ID:        "legacy-cross-user",
		UserID:    "user-2",
		Kind:      "image",
		Provider:  "local",
		ObjectKey: "user-2/legacy-cross-user.png",
		Status:    model.ResourceStatusReady,
	}
	resSharedPrimary := model.Resource{
		ID:        "legacy-shared-primary",
		UserID:    "user-1",
		Kind:      "image",
		Provider:  "local",
		ObjectKey: "shared/common.png",
		Status:    model.ResourceStatusReady,
	}
	resSharedUnsettledAlias := model.Resource{
		ID:        "legacy-shared-alias",
		UserID:    "user-3",
		Kind:      "image",
		Provider:  "local",
		ObjectKey: "shared/common.png",
		Status:    model.ResourceStatusReady,
	}

	for _, r := range []model.Resource{
		resEligible, resUnknownThumb, resWeakWriter, resDrifted,
		resCrossUser, resSharedPrimary, resSharedUnsettledAlias,
	} {
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		writeLegacyLocalResourceFile(t, svc, r.ObjectKey, "bytes-"+r.ID)
	}

	// 1. Exporting snapshot without read-only authorization ticket or admin privilege must fail.
	if _, err := svc.ExportLegacyResourceSnapshot(admin, LegacyResourceSnapshotExportRequest{
		ResourceIDs: []string{resEligible.ID},
	}); err == nil {
		t.Fatal("expected ExportLegacyResourceSnapshot without readOnlyAuthTicket to fail")
	}

	exportIds := []string{
		resEligible.ID, resUnknownThumb.ID, resWeakWriter.ID,
		resDrifted.ID, resCrossUser.ID, resSharedPrimary.ID,
	}
	snapshots, err := svc.ExportLegacyResourceSnapshot(admin, LegacyResourceSnapshotExportRequest{
		ReadOnlyAuthTicket: "ro-ticket-2026-11-02",
		ResourceIDs:        exportIds,
	})
	if err != nil {
		t.Fatalf("ExportLegacyResourceSnapshot() error = %v", err)
	}
	byID := map[string]LegacyResourceSnapshotItem{}
	for _, item := range snapshots {
		byID[item.ResourceID] = item
	}

	// Mutate resDrifted after snapshot export to trigger snapshot_drift.
	if err := db.Model(&model.Resource{}).Where("id = ?", resDrifted.ID).Update("object_key", "user-1/legacy-drifted-mutated.png").Error; err != nil {
		t.Fatal(err)
	}

	// 2. Self-declared Trusted: true with forged signature or missing ticket must fail provenance check in dry-run.
	forgedManifest := LegacyResourceEvidenceManifest{
		ManifestID:          "manifest-forged",
		SourceType:          "authorized-sanitized-export",
		ReadOnlyAuthTicket:  "ro-ticket-2026-11-02",
		ExportedAt:          time.Now().UTC(),
		Trusted:             true,
		ProvenanceSignature: "forged-sha256-digest",
		Entries: []LegacyResourceEvidenceEntry{
			{
				ResourceID:      resEligible.ID,
				UserID:          resEligible.UserID,
				ExpectedStatus:  string(resEligible.Status),
				Provider:        resEligible.Provider,
				ObjectKey:       resEligible.ObjectKey,
				ThumbnailStatus: resEligible.ThumbnailStatus,
				ThumbnailSize:   resEligible.ThumbnailSize,
				SnapshotDigest:  byID[resEligible.ID].SnapshotDigest,
			},
		},
	}
	forgedPlan, err := svc.EvaluateLegacyResourceMaintenance(admin, forgedManifest)
	if err != nil {
		t.Fatalf("EvaluateLegacyResourceMaintenance(forged) error = %v", err)
	}
	if forgedPlan.EligibleCount != 0 || forgedPlan.BlockedCount != 1 {
		t.Fatalf("forged manifest plan = %+v, want 0 eligible and 1 blocked", forgedPlan)
	}
	if !slices.Contains(forgedPlan.Items[0].Reasons, "untrusted_self_declaration") && !slices.Contains(forgedPlan.Items[0].Reasons, "unverified_provenance") {
		t.Fatalf("forged manifest reasons = %v, want untrusted_self_declaration or unverified_provenance", forgedPlan.Items[0].Reasons)
	}

	// 3. Build a properly signed manifest containing all 6 candidate entries and verify classification.
	entries := []LegacyResourceEvidenceEntry{
		{
			ResourceID:      resEligible.ID,
			UserID:          resEligible.UserID,
			ExpectedStatus:  string(resEligible.Status),
			Provider:        resEligible.Provider,
			ObjectKey:       resEligible.ObjectKey,
			ThumbnailStatus: resEligible.ThumbnailStatus,
			ThumbnailSize:   resEligible.ThumbnailSize,
			SnapshotDigest:  byID[resEligible.ID].SnapshotDigest,
			Variants: []LegacyResourceVariantProof{
				{Variant: "original", Provider: "local", ObjectKey: resEligible.ObjectKey, Disposition: "verified-present", EvidenceSource: "storage-inventory-v1"},
				{Variant: "thumbnail", Provider: "local", ObjectKey: "", Disposition: "verified-absent", EvidenceSource: "thumbnail-absent-proof-v1"},
			},
			WriterAttempts: []LegacyResourceWriterProof{
				{AttemptID: "att-1", Variant: "original", Protocol: "legacy-upload-v0", WriterID: "writer-1", TerminalState: "settled", Outcome: "success", StopWriteProof: "storage-lease-expired-and-checksum-verified"},
				{AttemptID: "att-1-thumb", Variant: "thumbnail", Protocol: "legacy-thumb-v0", WriterID: "writer-thumb-1", TerminalState: "settled", Outcome: "failed", StopWriteProof: "thumbnail-worker-retired-and-absent-verified"},
			},
		},
		{
			ResourceID:      resUnknownThumb.ID,
			UserID:          resUnknownThumb.UserID,
			ExpectedStatus:  string(resUnknownThumb.Status),
			Provider:        resUnknownThumb.Provider,
			ObjectKey:       resUnknownThumb.ObjectKey,
			ThumbnailStatus: resUnknownThumb.ThumbnailStatus,
			ThumbnailSize:   resUnknownThumb.ThumbnailSize,
			SnapshotDigest:  byID[resUnknownThumb.ID].SnapshotDigest,
			Variants: []LegacyResourceVariantProof{
				{Variant: "original", Provider: "local", ObjectKey: resUnknownThumb.ObjectKey, Disposition: "verified-present", EvidenceSource: "storage-inventory-v1"},
				{Variant: "thumbnail", Provider: "local", ObjectKey: "", Disposition: "unknown", EvidenceSource: ""},
			},
			WriterAttempts: []LegacyResourceWriterProof{
				{AttemptID: "att-2", Variant: "original", Protocol: "legacy-upload-v0", WriterID: "writer-2", TerminalState: "settled", Outcome: "success", StopWriteProof: "storage-lease-expired-and-checksum-verified"},
			},
		},
		{
			ResourceID:     resWeakWriter.ID,
			UserID:         resWeakWriter.UserID,
			ExpectedStatus: string(resWeakWriter.Status),
			Provider:       resWeakWriter.Provider,
			ObjectKey:      resWeakWriter.ObjectKey,
			SnapshotDigest: byID[resWeakWriter.ID].SnapshotDigest,
			Variants: []LegacyResourceVariantProof{
				{Variant: "original", Provider: "local", ObjectKey: resWeakWriter.ObjectKey, Disposition: "verified-present", EvidenceSource: "storage-inventory-v1"},
			},
			WriterAttempts: []LegacyResourceWriterProof{
				{AttemptID: "att-3", Variant: "original", Protocol: "legacy-upload-v0", WriterID: "writer-3", TerminalState: "settled", Outcome: "success", StopWriteProof: "operator-ack"},
			},
		},
		{
			ResourceID:     resDrifted.ID,
			UserID:         resDrifted.UserID,
			ExpectedStatus: string(resDrifted.Status),
			Provider:       resDrifted.Provider,
			ObjectKey:      resDrifted.ObjectKey,
			SnapshotDigest: byID[resDrifted.ID].SnapshotDigest,
			Variants: []LegacyResourceVariantProof{
				{Variant: "original", Provider: "local", ObjectKey: resDrifted.ObjectKey, Disposition: "verified-present", EvidenceSource: "storage-inventory-v1"},
			},
			WriterAttempts: []LegacyResourceWriterProof{
				{AttemptID: "att-4", Variant: "original", Protocol: "legacy-upload-v0", WriterID: "writer-4", TerminalState: "settled", Outcome: "success", StopWriteProof: "storage-lease-expired-and-checksum-verified"},
			},
		},
		{
			ResourceID:     resCrossUser.ID,
			UserID:         "user-wrong",
			ExpectedStatus: string(resCrossUser.Status),
			Provider:       resCrossUser.Provider,
			ObjectKey:      resCrossUser.ObjectKey,
			SnapshotDigest: byID[resCrossUser.ID].SnapshotDigest,
			Variants: []LegacyResourceVariantProof{
				{Variant: "original", Provider: "local", ObjectKey: resCrossUser.ObjectKey, Disposition: "verified-present", EvidenceSource: "storage-inventory-v1"},
			},
			WriterAttempts: []LegacyResourceWriterProof{
				{AttemptID: "att-5", Variant: "original", Protocol: "legacy-upload-v0", WriterID: "writer-5", TerminalState: "settled", Outcome: "success", StopWriteProof: "storage-lease-expired-and-checksum-verified"},
			},
		},
		{
			ResourceID:     resSharedPrimary.ID,
			UserID:         resSharedPrimary.UserID,
			ExpectedStatus: string(resSharedPrimary.Status),
			Provider:       resSharedPrimary.Provider,
			ObjectKey:      resSharedPrimary.ObjectKey,
			SnapshotDigest: byID[resSharedPrimary.ID].SnapshotDigest,
			Variants: []LegacyResourceVariantProof{
				{Variant: "original", Provider: "local", ObjectKey: resSharedPrimary.ObjectKey, Disposition: "verified-present", EvidenceSource: "storage-inventory-v1"},
			},
			WriterAttempts: []LegacyResourceWriterProof{
				{AttemptID: "att-6", Variant: "original", Protocol: "legacy-upload-v0", WriterID: "writer-6", TerminalState: "settled", Outcome: "success", StopWriteProof: "storage-lease-expired-and-checksum-verified"},
			},
		},
	}
	manifest := SignLegacyResourceEvidenceManifest(LegacyResourceEvidenceManifest{
		ManifestID:         "manifest-dry-run-1",
		SourceType:         "authorized-sanitized-export",
		ReadOnlyAuthTicket: "ro-ticket-2026-11-02",
		ExportedAt:         time.Now().UTC(),
		Trusted:            true,
		Entries:            entries,
	})

	plan, err := svc.EvaluateLegacyResourceMaintenance(admin, manifest)
	if err != nil {
		t.Fatalf("EvaluateLegacyResourceMaintenance() error = %v", err)
	}
	if plan.EligibleCount != 1 || plan.BlockedCount != 2 || plan.ConflictCount != 3 {
		t.Fatalf("plan counts = eligible:%d blocked:%d conflict:%d, want 1/2/3 (items=%+v)", plan.EligibleCount, plan.BlockedCount, plan.ConflictCount, plan.Items)
	}

	itemByID := map[string]LegacyResourceItemEvaluation{}
	for _, item := range plan.Items {
		itemByID[item.ResourceID] = item
	}
	if itemByID[resEligible.ID].Status != LegacyResourceEligible {
		t.Fatalf("resEligible status = %s (%v), want eligible", itemByID[resEligible.ID].Status, itemByID[resEligible.ID].Reasons)
	}
	if itemByID[resUnknownThumb.ID].Status != LegacyResourceBlocked || !slices.Contains(itemByID[resUnknownThumb.ID].Reasons, "unknown_thumbnail_variant") {
		t.Fatalf("resUnknownThumb = %+v, want blocked with unknown_thumbnail_variant", itemByID[resUnknownThumb.ID])
	}
	if itemByID[resWeakWriter.ID].Status != LegacyResourceBlocked || !slices.Contains(itemByID[resWeakWriter.ID].Reasons, "missing_writer_stop_proof") {
		t.Fatalf("resWeakWriter = %+v, want blocked with missing_writer_stop_proof", itemByID[resWeakWriter.ID])
	}
	if itemByID[resDrifted.ID].Status != LegacyResourceConflict || !slices.Contains(itemByID[resDrifted.ID].Reasons, "snapshot_drift") {
		t.Fatalf("resDrifted = %+v, want conflict with snapshot_drift", itemByID[resDrifted.ID])
	}
	if itemByID[resCrossUser.ID].Status != LegacyResourceConflict || !slices.Contains(itemByID[resCrossUser.ID].Reasons, "cross_user_mismatch") {
		t.Fatalf("resCrossUser = %+v, want conflict with cross_user_mismatch", itemByID[resCrossUser.ID])
	}
	if itemByID[resSharedPrimary.ID].Status != LegacyResourceConflict || !slices.Contains(itemByID[resSharedPrimary.ID].Reasons, "shared_alias_conflict") {
		t.Fatalf("resSharedPrimary = %+v, want conflict with shared_alias_conflict", itemByID[resSharedPrimary.ID])
	}

	// Verify dry-run mutated zero producer operations and left thumbnail status intact.
	var opCount int64
	if err := db.Model(&model.ResourceProducerOperation{}).Count(&opCount).Error; err != nil || opCount != 0 {
		t.Fatalf("dry-run created producer operations: count=%d err=%v", opCount, err)
	}
	var checkEligible model.Resource
	if err := db.First(&checkEligible, "id = ?", resEligible.ID).Error; err != nil {
		t.Fatal(err)
	}
	if checkEligible.ThumbnailStatus != "ready" || checkEligible.ThumbnailSize != 2048 {
		t.Fatalf("dry-run mutated resource row: %+v", checkEligible)
	}
}

func TestLegacyResourceEvidenceMaintenance_ApprovalGatesAndRealEntrypoints(t *testing.T) {
	svc, db, admin := newLegacyMaintenanceTestService(t)

	// Set up an isolated HTTP object storage server to test partial physical failure & recovery through real deletion worker.
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	var failRemoteDelete atomic.Bool
	failRemoteDelete.Store(true)
	var remoteObjectExists atomic.Bool
	remoteObjectExists.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			if failRemoteDelete.Load() {
				http.Error(w, "injected physical failure", http.StatusServiceUnavailable)
				return
			}
			remoteObjectExists.Store(false)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if !remoteObjectExists.Load() {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create 4 legacy resources:
	// 1) resRegisterOnly: local legacy resource with verified-absent thumbnail -> register_evidence only (must NOT delete)
	// 2) resDeleteLocal: unreferenced local legacy resource -> delete_after_evidence (real DeleteAdminResources + drain removes file & row)
	// 3) resReferenced: local legacy resource still referenced by an active Asset -> real entrypoint blocks deletion, retains row & file
	// 4) resPartialFail: remote S3 legacy resource -> delete_after_evidence stages deletion, first drain fails physically and retains Resource record, second drain after recovery finishes cleanly
	resRegisterOnly := model.Resource{
		ID:              "legacy-reg-only",
		UserID:          "user-1",
		Kind:            "image",
		Provider:        "local",
		ObjectKey:       "user-1/legacy-reg-only.png",
		Status:          model.ResourceStatusReady,
		ThumbnailStatus: "ready",
		ThumbnailSize:   1024,
	}
	resDeleteLocal := model.Resource{
		ID:              "legacy-del-local",
		UserID:          "user-1",
		Kind:            "image",
		Provider:        "local",
		ObjectKey:       "user-1/legacy-del-local.png",
		Status:          model.ResourceStatusReady,
		ThumbnailStatus: "ready",
		ThumbnailSize:   512,
	}
	resReferenced := model.Resource{
		ID:        "legacy-referenced",
		UserID:    "user-1",
		Kind:      "image",
		Provider:  "local",
		ObjectKey: "user-1/legacy-referenced.png",
		Status:    model.ResourceStatusReady,
	}
	resPartialFail := model.Resource{
		ID:        "legacy-partial-fail",
		UserID:    "user-1",
		Kind:      "image",
		Provider:  s3Provider,
		Endpoint:  server.URL,
		Bucket:    "maint-bucket",
		ObjectKey: "user-1/legacy-partial-fail.png",
		Status:    model.ResourceStatusReady,
	}
	resBlocked := model.Resource{
		ID:              "legacy-still-blocked",
		UserID:          "user-1",
		Kind:            "image",
		Provider:        "local",
		ObjectKey:       "user-1/legacy-still-blocked.png",
		Status:          model.ResourceStatusReady,
		ThumbnailStatus: "ready",
		ThumbnailSize:   999,
	}

	for _, r := range []model.Resource{resRegisterOnly, resDeleteLocal, resReferenced, resPartialFail, resBlocked} {
		if err := db.Create(&r).Error; err != nil {
			t.Fatal(err)
		}
		if r.Provider == "local" {
			writeLegacyLocalResourceFile(t, svc, r.ObjectKey, "data-"+r.ID)
		}
	}

	// Create an active Asset referencing resReferenced so real deletion entrypoint blocks it.
	refAsset := model.Asset{
		ID:          "asset-ref-1",
		UserID:      "user-1",
		Title:       "Referenced Asset",
		PayloadJSON: `{"data":{"storageKey":"resource:legacy-referenced"}}`,
	}
	if err := db.Create(&refAsset).Error; err != nil {
		t.Fatal(err)
	}

	// Configure OSS system setting for resPartialFail.
	if err := db.Create(&model.SystemSetting{
		Key:       ossSettingKey,
		ValueJSON: `{"enabled":true,"provider":"s3","endpoint":"` + server.URL + `","bucket":"maint-bucket","region":"us-east-1","accessKeyId":"ak","accessKeySecret":"sk"}`,
	}).Error; err != nil {
		t.Fatal(err)
	}

	// Confirm all 4 legacy resources are initially blocked from deletion by ErrResourceProducerUnsettled.
	for _, r := range []model.Resource{resRegisterOnly, resDeleteLocal, resPartialFail} {
		if err := svc.repo.DeleteDetachedResources([]model.Resource{r}, nil); !errors.Is(err, repository.ErrResourceProducerUnsettled) {
			t.Fatalf("resource %s was not blocked before evidence maintenance: %v", r.ID, err)
		}
	}

	snapshots, err := svc.ExportLegacyResourceSnapshot(admin, LegacyResourceSnapshotExportRequest{
		ReadOnlyAuthTicket: "ro-ticket-maint-2",
		ResourceIDs:        []string{resRegisterOnly.ID, resDeleteLocal.ID, resReferenced.ID, resPartialFail.ID, resBlocked.ID},
	})
	if err != nil {
		t.Fatalf("ExportLegacyResourceSnapshot() error = %v", err)
	}
	snapByID := map[string]LegacyResourceSnapshotItem{}
	for _, item := range snapshots {
		snapByID[item.ResourceID] = item
	}

	makeEligibleEntry := func(r model.Resource) LegacyResourceEvidenceEntry {
		variants := []LegacyResourceVariantProof{
			{Variant: "original", Provider: r.Provider, Endpoint: r.Endpoint, Bucket: r.Bucket, ObjectKey: r.ObjectKey, Disposition: "verified-present", EvidenceSource: "storage-inventory-v1"},
		}
		writers := []LegacyResourceWriterProof{
			{AttemptID: "att-" + r.ID, Variant: "original", Protocol: "legacy-writer-v0", WriterID: "w-" + r.ID, TerminalState: "settled", Outcome: "success", StopWriteProof: "storage-lease-expired-and-checksum-verified"},
		}
		if r.ThumbnailStatus == "ready" || r.ThumbnailSize > 0 {
			variants = append(variants, LegacyResourceVariantProof{
				Variant: "thumbnail", Provider: r.Provider, Endpoint: r.Endpoint, Bucket: r.Bucket, ObjectKey: "", Disposition: "verified-absent", EvidenceSource: "thumbnail-absent-proof-v1",
			})
			writers = append(writers, LegacyResourceWriterProof{
				AttemptID: "att-thumb-" + r.ID, Variant: "thumbnail", Protocol: "legacy-thumb-v0", WriterID: "wt-" + r.ID, TerminalState: "settled", Outcome: "failed", StopWriteProof: "thumbnail-worker-retired-and-absent-verified",
			})
		}
		return LegacyResourceEvidenceEntry{
			ResourceID:      r.ID,
			UserID:          r.UserID,
			ExpectedStatus:  string(r.Status),
			Provider:        r.Provider,
			Endpoint:        r.Endpoint,
			Bucket:          r.Bucket,
			ObjectKey:       r.ObjectKey,
			ThumbnailStatus: r.ThumbnailStatus,
			ThumbnailSize:   r.ThumbnailSize,
			SnapshotDigest:  snapByID[r.ID].SnapshotDigest,
			Variants:        variants,
			WriterAttempts:  writers,
		}
	}

	manifest := SignLegacyResourceEvidenceManifest(LegacyResourceEvidenceManifest{
		ManifestID:         "manifest-apply-1",
		SourceType:         "authorized-sanitized-export",
		ReadOnlyAuthTicket: "ro-ticket-maint-2",
		ExportedAt:         time.Now().UTC(),
		Trusted:            true,
		Entries: []LegacyResourceEvidenceEntry{
			makeEligibleEntry(resRegisterOnly),
			makeEligibleEntry(resDeleteLocal),
			makeEligibleEntry(resReferenced),
			makeEligibleEntry(resPartialFail),
			{
				ResourceID:      resBlocked.ID,
				UserID:          resBlocked.UserID,
				ExpectedStatus:  string(resBlocked.Status),
				Provider:        resBlocked.Provider,
				ObjectKey:       resBlocked.ObjectKey,
				ThumbnailStatus: resBlocked.ThumbnailStatus,
				ThumbnailSize:   resBlocked.ThumbnailSize,
				SnapshotDigest:  snapByID[resBlocked.ID].SnapshotDigest,
				Variants: []LegacyResourceVariantProof{
					{Variant: "original", Provider: "local", ObjectKey: resBlocked.ObjectKey, Disposition: "verified-present", EvidenceSource: "storage-inventory-v1"},
					{Variant: "thumbnail", Provider: "local", ObjectKey: "", Disposition: "unknown", EvidenceSource: ""},
				},
				WriterAttempts: []LegacyResourceWriterProof{
					{AttemptID: "att-blocked", Variant: "original", Protocol: "legacy-writer-v0", WriterID: "w-blocked", TerminalState: "settled", Outcome: "success", StopWriteProof: "storage-lease-expired-and-checksum-verified"},
				},
			},
		},
	})

	plan, err := svc.EvaluateLegacyResourceMaintenance(admin, manifest)
	if err != nil {
		t.Fatalf("EvaluateLegacyResourceMaintenance() error = %v", err)
	}
	if plan.EligibleCount != 4 || plan.BlockedCount != 1 {
		t.Fatalf("plan counts = %+v, want 4 eligible and 1 blocked", plan)
	}

	// Gate 1: Reject when BackupConfirmed / RecoveryDrillVerified / WriteBarrierConfirmed / MaintenanceWindow are missing.
	if _, err := svc.ApplyLegacyResourceMaintenance(admin, manifest, LegacyResourceMaintenanceApproval{
		ApprovalID:            "app-missing-barrier",
		ManifestID:            manifest.ManifestID,
		Operation:             LegacyMaintenanceOpRegisterEvidence,
		ApprovedResourceIDs:   []string{resRegisterOnly.ID},
		ExpectedDiffDigest:    plan.DiffDigest,
		BackupConfirmed:       true,
		RecoveryDrillVerified: true,
		WriteBarrierConfirmed: false,
		MaintenanceWindow:     "2026-11-02T02:00Z/2026-11-02T03:00Z",
	}); err == nil {
		t.Fatal("expected ApplyLegacyResourceMaintenance without WriteBarrierConfirmed to fail")
	}

	// Gate 2: Reject when ApprovedResourceIDs includes a blocked resource.
	if _, err := svc.ApplyLegacyResourceMaintenance(admin, manifest, LegacyResourceMaintenanceApproval{
		ApprovalID:            "app-includes-blocked",
		ManifestID:            manifest.ManifestID,
		Operation:             LegacyMaintenanceOpRegisterEvidence,
		ApprovedResourceIDs:   []string{resRegisterOnly.ID, resBlocked.ID},
		ExpectedDiffDigest:    plan.DiffDigest,
		BackupConfirmed:       true,
		RecoveryDrillVerified: true,
		WriteBarrierConfirmed: true,
		MaintenanceWindow:     "2026-11-02T02:00Z/2026-11-02T03:00Z",
	}); err == nil {
		t.Fatal("expected ApplyLegacyResourceMaintenance with blocked resource ID to fail")
	}

	// Gate 3: Apply register_evidence on resRegisterOnly -> registers settled evidence, clears verified-absent thumbnail metadata, is idempotent on repeat run, and does NOT delete the resource or file.
	regApproval := LegacyResourceMaintenanceApproval{
		ApprovalID:            "app-reg-1",
		ManifestID:            manifest.ManifestID,
		Operation:             LegacyMaintenanceOpRegisterEvidence,
		ApprovedResourceIDs:   []string{resRegisterOnly.ID},
		ExpectedDiffDigest:    plan.DiffDigest,
		BackupConfirmed:       true,
		RecoveryDrillVerified: true,
		WriteBarrierConfirmed: true,
		MaintenanceWindow:     "2026-11-02T02:00Z/2026-11-02T03:00Z",
	}
	regResult, err := svc.ApplyLegacyResourceMaintenance(admin, manifest, regApproval)
	if err != nil {
		t.Fatalf("ApplyLegacyResourceMaintenance(register_evidence) error = %v", err)
	}
	if !slices.Equal(regResult.RegisteredResourceIDs, []string{resRegisterOnly.ID}) || len(regResult.DeletedResourceIDs) != 0 {
		t.Fatalf("regResult = %+v, want Registered=[%s] and Deleted=[]", regResult, resRegisterOnly.ID)
	}
	// Repeat execution must be idempotent.
	regResultRepeat, err := svc.ApplyLegacyResourceMaintenance(admin, manifest, regApproval)
	if err != nil {
		t.Fatalf("ApplyLegacyResourceMaintenance(register_evidence repeat) error = %v", err)
	}
	if !slices.Equal(regResultRepeat.RegisteredResourceIDs, []string{resRegisterOnly.ID}) {
		t.Fatalf("repeat regResult = %+v", regResultRepeat)
	}
	// Verify file and Resource record still exist after register_evidence!
	if _, err := os.Stat(filepath.Join(svc.dataDir, "resources", filepath.FromSlash(resRegisterOnly.ObjectKey))); err != nil {
		t.Fatalf("register_evidence unexpectedly deleted physical file: %v", err)
	}
	var regRow model.Resource
	if err := db.First(&regRow, "id = ?", resRegisterOnly.ID).Error; err != nil {
		t.Fatalf("register_evidence deleted resource row: %v", err)
	}
	if regRow.ThumbnailStatus != "" || regRow.ThumbnailSize != 0 {
		t.Fatalf("register_evidence did not clear verified-absent thumbnail metadata: %+v", regRow)
	}

	// Gate 4: Re-export & evaluate after resRegisterOnly was updated, and run delete_after_evidence on [resDeleteLocal, resReferenced, resPartialFail].
	snapshots2, err := svc.ExportLegacyResourceSnapshot(admin, LegacyResourceSnapshotExportRequest{
		ReadOnlyAuthTicket: "ro-ticket-maint-3",
		ResourceIDs:        []string{resDeleteLocal.ID, resReferenced.ID, resPartialFail.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range snapshots2 {
		snapByID[item.ResourceID] = item
	}
	manifest2 := SignLegacyResourceEvidenceManifest(LegacyResourceEvidenceManifest{
		ManifestID:         "manifest-apply-2",
		SourceType:         "authorized-sanitized-export",
		ReadOnlyAuthTicket: "ro-ticket-maint-3",
		ExportedAt:         time.Now().UTC(),
		Trusted:            true,
		Entries: []LegacyResourceEvidenceEntry{
			makeEligibleEntry(resDeleteLocal),
			makeEligibleEntry(resReferenced),
			makeEligibleEntry(resPartialFail),
		},
	})
	plan2, err := svc.EvaluateLegacyResourceMaintenance(admin, manifest2)
	if err != nil {
		t.Fatal(err)
	}
	delResult, err := svc.ApplyLegacyResourceMaintenance(admin, manifest2, LegacyResourceMaintenanceApproval{
		ApprovalID:            "app-del-1",
		ManifestID:            manifest2.ManifestID,
		Operation:             LegacyMaintenanceOpDeleteAfterEvidence,
		ApprovedResourceIDs:   []string{resDeleteLocal.ID, resReferenced.ID, resPartialFail.ID},
		ExpectedDiffDigest:    plan2.DiffDigest,
		BackupConfirmed:       true,
		RecoveryDrillVerified: true,
		WriteBarrierConfirmed: true,
		MaintenanceWindow:     "2026-11-02T03:00Z/2026-11-02T04:00Z",
	})
	if err != nil {
		t.Fatalf("ApplyLegacyResourceMaintenance(delete_after_evidence) error = %v", err)
	}

	// Verify 4a: resDeleteLocal was deleted both physically and in DB via the real entrypoint.
	if _, err := os.Stat(filepath.Join(svc.dataDir, "resources", filepath.FromSlash(resDeleteLocal.ObjectKey))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected resDeleteLocal physical file to be removed, stat err = %v", err)
	}
	var count int64
	if err := db.Model(&model.Resource{}).Where("id = ?", resDeleteLocal.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("expected resDeleteLocal DB row to be finalized and deleted, count=%d err=%v", count, err)
	}

	// Verify 4b: resReferenced was blocked by the real entrypoint (DeleteAdminResources reference check), keeping both DB row and physical file!
	if len(delResult.BlockedDeletes) != 1 || delResult.BlockedDeletes[0].ID != resReferenced.ID {
		t.Fatalf("expected resReferenced to be reported in BlockedDeletes, got %+v", delResult.BlockedDeletes)
	}
	if _, err := os.Stat(filepath.Join(svc.dataDir, "resources", filepath.FromSlash(resReferenced.ObjectKey))); err != nil {
		t.Fatalf("referenced resource physical file must be retained: %v", err)
	}
	if err := db.Model(&model.Resource{}).Where("id = ? AND status = ?", resReferenced.ID, model.ResourceStatusReady).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("referenced resource row must remain ready, count=%d err=%v", count, err)
	}

	// Verify 4c: resPartialFail failed physical S3 delete on first drain -> Resource row and DeletionJob MUST still be retained (not silently cleared!).
	if !remoteObjectExists.Load() {
		t.Fatal("remote object should still exist while failRemoteDelete is true")
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", resPartialFail.ID).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("resPartialFail DB row was prematurely removed on physical failure: count=%d err=%v", count, err)
	}

	// Now recover remote storage, make retry job ready, and drain -> physical object and DB row are cleanly finalized!
	failRemoteDelete.Store(false)
	if err := db.Model(&model.ResourceDeletionJob{}).Where("resource_id = ?", resPartialFail.ID).Update("next_attempt_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(4)
	if remoteObjectExists.Load() {
		t.Fatal("expected remote object to be deleted after retry")
	}
	if err := db.Model(&model.Resource{}).Where("id = ?", resPartialFail.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("expected resPartialFail DB row to be deleted after retry, count=%d err=%v", count, err)
	}

	// Verify audit trail recorded both register_evidence and delete_after_evidence events.
	var audits []model.AdminAuditEvent
	if err := db.Where("action LIKE ?", "resource.legacy_maintenance.%").Find(&audits).Error; err != nil {
		t.Fatal(err)
	}
	if len(audits) < 2 {
		t.Fatalf("expected at least 2 legacy maintenance AdminAuditEvent records, got %d", len(audits))
	}
}
