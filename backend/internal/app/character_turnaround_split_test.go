package app

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

func createTestPNGBytes(t *testing.T, width, height int, pixelFn func(x, y int) color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, pixelFn(x, y))
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// buildJPEGWithEXIFOrientation wraps a standard JPEG with an APP1 EXIF segment specifying the given orientation (1..8).
func buildJPEGWithEXIFOrientation(t *testing.T, width, height int, orientation uint16, fill color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}
	var raw bytes.Buffer
	if err := jpeg.Encode(&raw, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	jpegBytes := raw.Bytes()
	if len(jpegBytes) < 2 || jpegBytes[0] != 0xFF || jpegBytes[1] != 0xD8 {
		t.Fatalf("invalid jpeg header")
	}

	tiff := make([]byte, 8+2+12+4)
	copy(tiff[0:2], []byte("MM"))
	binary.BigEndian.PutUint16(tiff[2:4], 0x002A)
	binary.BigEndian.PutUint32(tiff[4:8], 8)
	binary.BigEndian.PutUint16(tiff[8:10], 1)
	binary.BigEndian.PutUint16(tiff[10:12], 0x0112)
	binary.BigEndian.PutUint16(tiff[12:14], 3)
	binary.BigEndian.PutUint32(tiff[14:18], 1)
	binary.BigEndian.PutUint16(tiff[18:20], orientation)

	exifPayload := append([]byte("Exif\x00\x00"), tiff...)
	segLen := uint16(len(exifPayload) + 2)
	var out bytes.Buffer
	out.Write([]byte{0xFF, 0xD8, 0xFF, 0xE1})
	_ = binary.Write(&out, binary.BigEndian, segLen)
	out.Write(exifPayload)
	out.Write(jpegBytes[2:])
	return out.Bytes()
}

func storeLocalTestImageResource(t *testing.T, svc *Service, db *gorm.DB, userID, resourceID, mimeType string, width, height int, data []byte) *model.Resource {
	t.Helper()
	_ = db
	ext := ".png"
	if mimeType == "image/jpeg" {
		ext = ".jpg"
	}
	res, _, err := svc.storeResource(userID, "image", resourceID+ext, mimeType, int64(len(data)), width, height, 0, bytes.NewReader(data), nil, true)
	if err != nil {
		t.Fatalf("storeResource: %v", err)
	}
	return res
}

func TestCharacterTurnaroundSplit_PlanAdmissionAndStrictDraftSchema(t *testing.T) {
	svc, db, projectID := referenceTestService(t)
	svc.dataDir = t.TempDir()
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.TaskLog{}, &model.Result{}, &model.TaskTextDelta{}, &model.ResourceProducerOperation{}, &model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}, &model.ResourceDeletionJob{}, &model.AdminAuditEvent{}, &model.UploadReservation{}, &model.CanvasSnapshot{}, &model.CanvasSnapshotResource{}, &model.UserDailyActivity{}, &model.CloudAgentResourceLease{}, &model.Tool{}, &model.WorkbenchTool{}, &model.WorkbenchPreset{}, &model.WorkflowTemplateVersion{}, &model.ArkPrivateAssetBinding{}, &model.CanvasProject{}, &model.CreationRun{}, &model.CreationSubmission{}, &model.StyleProfile{}, &model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.Shot{}, &model.ShotRevision{}, &model.ShotArtifact{}, &model.Announcement{}, &model.AnnouncementImageDraft{}); err != nil {
		t.Fatal(err)
	}
	userID := "user-1"

	sheetBytes := createTestPNGBytes(t, 300, 100, func(x, y int) color.NRGBA {
		if x < 100 {
			return color.NRGBA{R: 220, G: 40, B: 40, A: 255}
		}
		if x < 200 {
			return color.NRGBA{R: 40, G: 220, B: 40, A: 255}
		}
		return color.NRGBA{R: 40, G: 40, B: 220, A: 255}
	})
	sheetRes := storeLocalTestImageResource(t, svc, db, userID, "res-turnaround-sheet-1", "image/png", 300, 100, sheetBytes)
	portraitRes := storeLocalTestImageResource(t, svc, db, userID, "res-portrait-only-1", "image/png", 100, 100, sheetBytes)

	charDetail, err := svc.CreateProjectCharacter(userID, projectID, CreateProjectCharacterRequest{
		Name:            "林默",
		ImageResourceID: sheetRes.ID,
		ImageRole:       "turnaround_sheet",
		Definition:      map[string]any{"appearance": "黑衣剑客"},
	})
	if err != nil {
		t.Fatalf("CreateProjectCharacter: %v", err)
	}
	assetID := charDetail.Asset.ID
	baseVersionID := charDetail.Character.VersionID

	portraitChar, err := svc.CreateProjectCharacter(userID, projectID, CreateProjectCharacterRequest{
		Name:            "普通人像角色",
		ImageResourceID: portraitRes.ID,
		ImageRole:       "portrait",
	})
	if err != nil {
		t.Fatalf("CreateProjectCharacter portrait: %v", err)
	}

	// 1. Reject ordinary portrait character without turnaround_sheet
	_, err = svc.ValidateTurnaroundSplitSource(userID, projectID, portraitChar.Asset.ID, portraitChar.Character.VersionID, portraitRes.ID)
	if err == nil {
		t.Fatalf("expected ordinary portrait without turnaround_sheet to be rejected")
	}

	// 2. Reject forged/unowned resource or version mismatch
	_, err = svc.ValidateTurnaroundSplitSource(userID, projectID, assetID, "wrong-version-id", sheetRes.ID)
	if err == nil {
		t.Fatalf("expected version mismatch to be rejected")
	}
	_, err = svc.ValidateTurnaroundSplitSource(userID, projectID, assetID, baseVersionID, portraitRes.ID)
	if err == nil {
		t.Fatalf("expected unbound resource to be rejected as turnaround source")
	}

	// 3. Valid turnaround_sheet source passes validation
	validatedSource, err := svc.ValidateTurnaroundSplitSource(userID, projectID, assetID, baseVersionID, sheetRes.ID)
	if err != nil || validatedSource.ID != sheetRes.ID {
		t.Fatalf("expected valid turnaround_sheet source, got err=%v", err)
	}

	// 4. Strict draft schema validation: reject unknown fields, confidence fabrication, invalid role, or out-of-bounds coordinates
	if _, err := ParseTurnaroundSplitPlanDraft(`{"views":[{"role":"front","box":{"x":0,"y":0,"width":100,"height":100},"confidence":0.99}]}`, 300, 100); err == nil {
		t.Fatalf("expected draft with fabricated confidence field to be rejected")
	}
	if _, err := ParseTurnaroundSplitPlanDraft(`{"views":[{"role":"unknown_angle","box":{"x":0,"y":0,"width":100,"height":100}}]}`, 300, 100); err == nil {
		t.Fatalf("expected draft with invalid role to be rejected")
	}

	// 5. Partial / ambiguous draft marks RequiresManualReview = true and does not fabricate missing views
	partialDraft, err := ParseTurnaroundSplitPlanDraft(`{"views":[{"role":"front","box":{"x":0,"y":0,"width":100,"height":100},"note":"正面清晰"},{"role":"side","box":{"x":100,"y":0,"width":100,"height":100},"note":"侧面清晰"}],"manualReason":"缺少背面视角，请人工确认或重选整图"}`, 300, 100)
	if err != nil {
		t.Fatalf("unexpected error on valid partial draft: %v", err)
	}
	if !partialDraft.RequiresManualReview || len(partialDraft.Views) != 2 || partialDraft.ManualReason == "" {
		t.Fatalf("expected partial draft to require manual review with 2 views, got %+v", partialDraft)
	}

	// 6. Complete 3-view draft still requires explicit user confirmation before any save
	fullDraft, err := ParseTurnaroundSplitPlanDraft(`{"views":[{"role":"front","box":{"x":0,"y":0,"width":100,"height":100}},{"role":"side","box":{"x":100,"y":0,"width":100,"height":100}},{"role":"back","box":{"x":200,"y":0,"width":100,"height":100}}]}`, 300, 100)
	if err != nil {
		t.Fatalf("unexpected error on full draft: %v", err)
	}
	if len(fullDraft.Views) != 3 {
		t.Fatalf("expected 3 views in full draft, got %d", len(fullDraft.Views))
	}
}

func TestCharacterTurnaroundSplit_PixelCropEXIFIdempotencyAndRepresentationMerge(t *testing.T) {
	svc, db, projectID := referenceTestService(t)
	svc.dataDir = t.TempDir()
	if err := db.AutoMigrate(&model.SystemSetting{}, &model.TaskLog{}, &model.Result{}, &model.TaskTextDelta{}, &model.ResourceProducerOperation{}, &model.ResourceDeletionIntent{}, &model.ResourceDeletionObject{}, &model.ResourceDeletionJob{}, &model.AdminAuditEvent{}, &model.UploadReservation{}, &model.CanvasSnapshot{}, &model.CanvasSnapshotResource{}, &model.UserDailyActivity{}, &model.CloudAgentResourceLease{}, &model.Tool{}, &model.WorkbenchTool{}, &model.WorkbenchPreset{}, &model.WorkflowTemplateVersion{}, &model.ArkPrivateAssetBinding{}, &model.CanvasProject{}, &model.CreationRun{}, &model.CreationSubmission{}, &model.StyleProfile{}, &model.ProjectAssetCandidate{}, &model.WorkflowInstance{}, &model.WorkflowStepInstance{}, &model.Shot{}, &model.ShotRevision{}, &model.ShotArtifact{}, &model.Announcement{}, &model.AnnouncementImageDraft{}); err != nil {
		t.Fatal(err)
	}
	userID := "user-1"

	sheetBytes := createTestPNGBytes(t, 300, 100, func(x, y int) color.NRGBA {
		switch {
		case x < 100:
			return color.NRGBA{R: 210, G: 30, B: 40, A: 255}
		case x < 200:
			return color.NRGBA{R: 30, G: 210, B: 50, A: 255}
		default:
			return color.NRGBA{R: 40, G: 50, B: 210, A: 255}
		}
	})
	sheetRes := storeLocalTestImageResource(t, svc, db, userID, "res-sheet-crop-1", "image/png", 300, 100, sheetBytes)
	exprRes := storeLocalTestImageResource(t, svc, db, userID, "res-expr-sheet-1", "image/png", 100, 100, sheetBytes)

	now := time.Now().UTC()
	voiceProfile := &model.VoiceProfile{
		ID:                   "voice-prof-split-1",
		UserID:               userID,
		Name:                 "林默原声",
		Provider:             "custom",
		VoiceKey:             "linmo-v1",
		Language:             "zh-CN",
		Timbre:               "冷峻青年",
		CompatibleModelsJSON: "[]",
		Status:               "active",
		CreatedAt:            now,
		UpdatedAt:            now,
	}
	if err := svc.repo.CreateVoiceProfile(voiceProfile); err != nil {
		t.Fatalf("CreateVoiceProfile: %v", err)
	}

	charDetail, err := svc.CreateProjectCharacter(userID, projectID, CreateProjectCharacterRequest{
		Name:            "林默",
		ImageResourceID: sheetRes.ID,
		Definition: map[string]any{
			"role":       "男主角",
			"appearance": "高马尾黑衣剑客",
		},
	})
	if err != nil {
		t.Fatalf("CreateProjectCharacter: %v", err)
	}
	assetID := charDetail.Asset.ID
	if _, err := svc.BindProjectCharacterVoice(userID, projectID, assetID, BindCharacterVoiceRequest{
		VoiceProfileID: voiceProfile.ID,
		Instructions:   "沉稳低沉",
	}); err != nil {
		t.Fatalf("BindProjectCharacterVoice: %v", err)
	}
	charDetail, err = svc.ReplaceProjectCharacterRepresentations(userID, projectID, assetID, ReplaceCharacterRepresentationsRequest{
		Representations: []CharacterRepresentationInput{
			{Role: "turnaround_sheet", ResourceID: sheetRes.ID, Metadata: map[string]any{"source": "initial_sheet"}},
			{Role: "primary", ResourceID: sheetRes.ID, Metadata: map[string]any{"source": "initial_primary"}},
			{Role: "expression_sheet", ResourceID: exprRes.ID, Metadata: map[string]any{"source": "initial_expr"}},
		},
	})
	if err != nil {
		t.Fatalf("ReplaceProjectCharacterRepresentations: %v", err)
	}
	baseVersionID := charDetail.Character.VersionID

	validBoxes := []TurnaroundConfirmedCropBox{
		{Role: "front", X: 0, Y: 0, Width: 100, Height: 100, Confirmed: true},
		{Role: "side", X: 100, Y: 0, Width: 100, Height: 100, Confirmed: true},
		{Role: "back", X: 200, Y: 0, Width: 100, Height: 100, Confirmed: true},
	}

	// 1. Reject unconfirmed box or overlapping boxes
	unconfirmedBoxes := []TurnaroundConfirmedCropBox{
		{Role: "front", X: 0, Y: 0, Width: 100, Height: 100, Confirmed: true},
		{Role: "side", X: 100, Y: 0, Width: 100, Height: 100, Confirmed: false},
		{Role: "back", X: 200, Y: 0, Width: 100, Height: 100, Confirmed: true},
	}
	if _, err := svc.ExecuteCharacterTurnaroundSplit(userID, projectID, assetID, ExecuteTurnaroundSplitRequest{
		IdempotencyKey:    "idem-unconfirmed",
		ExpectedVersionID: baseVersionID,
		SourceResourceID:  sheetRes.ID,
		SourceWidth:       300,
		SourceHeight:      100,
		Boxes:             unconfirmedBoxes,
	}); err == nil {
		t.Fatalf("expected unconfirmed box to be rejected")
	}

	overlappingBoxes := []TurnaroundConfirmedCropBox{
		{Role: "front", X: 0, Y: 0, Width: 120, Height: 100, Confirmed: true},
		{Role: "side", X: 100, Y: 0, Width: 100, Height: 100, Confirmed: true},
		{Role: "back", X: 200, Y: 0, Width: 100, Height: 100, Confirmed: true},
	}
	if _, err := svc.ExecuteCharacterTurnaroundSplit(userID, projectID, assetID, ExecuteTurnaroundSplitRequest{
		IdempotencyKey:    "idem-overlap",
		ExpectedVersionID: baseVersionID,
		SourceResourceID:  sheetRes.ID,
		SourceWidth:       300,
		SourceHeight:      100,
		Boxes:             overlappingBoxes,
	}); err == nil {
		t.Fatalf("expected overlapping boxes to be rejected")
	}

	// 2. Execute valid turnaround split
	splitRes, err := svc.ExecuteCharacterTurnaroundSplit(userID, projectID, assetID, ExecuteTurnaroundSplitRequest{
		IdempotencyKey:    "idem-split-success-1",
		ExpectedVersionID: baseVersionID,
		SourceResourceID:  sheetRes.ID,
		SourceWidth:       300,
		SourceHeight:      100,
		Boxes:             validBoxes,
	})
	if err != nil {
		t.Fatalf("ExecuteCharacterTurnaroundSplit failed: %v", err)
	}
	if splitRes.AdoptedVersionID == "" || splitRes.AdoptedVersionID == baseVersionID {
		t.Fatalf("expected new AdoptedVersionID, got %q", splitRes.AdoptedVersionID)
	}
	if len(splitRes.CreatedResources) != 3 {
		t.Fatalf("expected 3 created resources, got %d", len(splitRes.CreatedResources))
	}

	// Verify pixel exactness of cropped PNGs on disk
	expectedColors := map[string]color.NRGBA{
		"front": {R: 210, G: 30, B: 40, A: 255},
		"side":  {R: 30, G: 210, B: 50, A: 255},
		"back":  {R: 40, G: 50, B: 210, A: 255},
	}
	for role, expectedColor := range expectedColors {
		resID := splitRes.CreatedResources[role]
		if resID == "" {
			t.Fatalf("missing created resource for role %s", role)
		}
		_, stream, openErr := svc.OpenResource(userID, resID)
		if openErr != nil {
			t.Fatalf("OpenResource(%s): %v", resID, openErr)
		}
		decoded, decodeErr := png.Decode(stream)
		_ = stream.Close()
		if decodeErr != nil {
			t.Fatalf("png.Decode(%s): %v", resID, decodeErr)
		}
		if decoded.Bounds().Dx() != 100 || decoded.Bounds().Dy() != 100 {
			t.Fatalf("expected 100x100 cropped image for %s, got %v", role, decoded.Bounds())
		}
		r, g, b, a := decoded.At(50, 50).RGBA()
		if uint8(r>>8) != expectedColor.R || uint8(g>>8) != expectedColor.G || uint8(b>>8) != expectedColor.B || uint8(a>>8) != expectedColor.A {
			t.Fatalf("pixel mismatch for %s: got (%d,%d,%d,%d), want %+v", role, r>>8, g>>8, b>>8, a>>8, expectedColor)
		}
	}

	// Verify existing representations (turnaround_sheet, primary, expression_sheet), voice, and definition are preserved!
	updatedChar, err := svc.ProjectCharacter(userID, projectID, assetID)
	if err != nil {
		t.Fatalf("ProjectCharacter: %v", err)
	}
	if updatedChar.Character.VersionID != splitRes.AdoptedVersionID {
		t.Fatalf("expected character version %s, got %s", splitRes.AdoptedVersionID, updatedChar.Character.VersionID)
	}
	if updatedChar.Character.VoiceStatus != "ready" || updatedChar.Character.Voice == nil || updatedChar.Character.Voice.Profile.ID != voiceProfile.ID {
		t.Fatalf("expected voice binding to be preserved, got %+v", updatedChar.Character.Voice)
	}
	if updatedChar.Character.Definition["appearance"] != "高马尾黑衣剑客" {
		t.Fatalf("expected definition to be preserved, got %+v", updatedChar.Character.Definition)
	}
	repByRole := map[string]string{}
	for _, rep := range updatedChar.Character.Representations {
		repByRole[rep.Role] = rep.ResourceID
	}
	if repByRole["turnaround_sheet"] != sheetRes.ID || repByRole["primary"] != sheetRes.ID || repByRole["expression_sheet"] != exprRes.ID {
		t.Fatalf("expected turnaround_sheet, primary, and expression_sheet to be preserved, got %+v", repByRole)
	}
	if repByRole["front"] != splitRes.CreatedResources["front"] || repByRole["side"] != splitRes.CreatedResources["side"] || repByRole["back"] != splitRes.CreatedResources["back"] {
		t.Fatalf("expected front/side/back representations to match created resources, got %+v", repByRole)
	}

	// 3. Idempotent replay with original ExpectedVersionID returns the exact same result without error or duplicate version
	replayRes, err := svc.ExecuteCharacterTurnaroundSplit(userID, projectID, assetID, ExecuteTurnaroundSplitRequest{
		IdempotencyKey:    "idem-split-success-1",
		ExpectedVersionID: baseVersionID,
		SourceResourceID:  sheetRes.ID,
		SourceWidth:       300,
		SourceHeight:      100,
		Boxes:             validBoxes,
	})
	if err != nil {
		t.Fatalf("expected idempotent replay to succeed, got %v", err)
	}
	if !replayRes.Replayed || replayRes.AdoptedVersionID != splitRes.AdoptedVersionID {
		t.Fatalf("expected replayed receipt matching original version, got %+v", replayRes)
	}

	// 4. Same idempotencyKey with different parameters fails with 409 conflict
	alteredBoxes := []TurnaroundConfirmedCropBox{
		{Role: "front", X: 0, Y: 0, Width: 90, Height: 100, Confirmed: true},
		{Role: "side", X: 100, Y: 0, Width: 100, Height: 100, Confirmed: true},
		{Role: "back", X: 200, Y: 0, Width: 100, Height: 100, Confirmed: true},
	}
	if _, err := svc.ExecuteCharacterTurnaroundSplit(userID, projectID, assetID, ExecuteTurnaroundSplitRequest{
		IdempotencyKey:    "idem-split-success-1",
		ExpectedVersionID: baseVersionID,
		SourceResourceID:  sheetRes.ID,
		SourceWidth:       300,
		SourceHeight:      100,
		Boxes:             alteredBoxes,
	}); err == nil {
		t.Fatalf("expected same idempotencyKey with different boxes to fail with conflict")
	}

	// 5. Stale ExpectedVersionID with a new idempotencyKey fails with 409 CAS conflict
	if _, err := svc.ExecuteCharacterTurnaroundSplit(userID, projectID, assetID, ExecuteTurnaroundSplitRequest{
		IdempotencyKey:    "idem-stale-version",
		ExpectedVersionID: baseVersionID,
		SourceResourceID:  sheetRes.ID,
		SourceWidth:       300,
		SourceHeight:      100,
		Boxes:             validBoxes,
	}); err == nil {
		t.Fatalf("expected stale ExpectedVersionID to fail with version conflict")
	}

	// 6. EXIF orientation normalization test (JPEG raw 100x300 with EXIF Orientation=6 (90° CW) -> display 300x100)
	jpegRaw := buildJPEGWithEXIFOrientation(t, 100, 300, 6, color.NRGBA{R: 120, G: 80, B: 200, A: 255})
	decodedImg, normW, normH, err := DecodeAndNormalizeTurnaroundSource(bytes.NewReader(jpegRaw), int64(len(jpegRaw)))
	if err != nil {
		t.Fatalf("DecodeAndNormalizeTurnaroundSource JPEG EXIF 6 failed: %v", err)
	}
	if normW != 300 || normH != 100 || decodedImg.Bounds().Dx() != 300 || decodedImg.Bounds().Dy() != 100 {
		t.Fatalf("expected EXIF 6 100x300 JPEG to normalize to 300x100 display dimensions, got %dx%d", normW, normH)
	}

	// 7. Source turnaround_sheet and created front/side/back resources are protected from deletion
	adminUser := &model.User{ID: userID, Role: "admin"}
	delSheet, err := svc.DeleteAdminResources(adminUser, AdminResourceDeleteRequest{ResourceIDs: []string{sheetRes.ID}})
	if err != nil || len(delSheet.Blocked) != 1 {
		t.Fatalf("expected source turnaround_sheet resource to be blocked due to references, got %+v err=%v", delSheet, err)
	}
	delFront, err := svc.DeleteAdminResources(adminUser, AdminResourceDeleteRequest{ResourceIDs: []string{splitRes.CreatedResources["front"]}})
	if err != nil || len(delFront.Blocked) != 1 {
		t.Fatalf("expected cropped front resource to be blocked due to references, got %+v err=%v", delFront, err)
	}
}
