package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"yingce/backend/internal/auth"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"yingce/backend/internal/service"
)

// Opt-in acceptance of the normal upload/transcode/publication/HTTP chain using
// real tools and synthetic media. An explicit Windows acceptance build can also
// exercise C1. Neither mode proves browser, crash or remote producer recovery.
func TestProfileRealMediaUploadPlayback(t *testing.T) {
	if os.Getenv("PROFILE_MEDIA_C1") == "1" && (!realMediaC1Available || os.Getenv("PROFILE_REAL_MEDIA") != "1") {
		t.Fatal("explicit C1 media acceptance requires Windows, -tags profile_media_acceptance and PROFILE_REAL_MEDIA=1; refusing skip/fallback")
	}
	if os.Getenv("PROFILE_REAL_MEDIA") != "1" {
		t.Skip("set PROFILE_REAL_MEDIA=1 in an isolated environment with ffmpeg/ffprobe and libx264/libx265")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatal("opted-in media acceptance requires ffmpeg:", err)
	}
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Fatal("opted-in media acceptance requires ffprobe:", err)
	}
	for _, tool := range []string{ffmpeg, ffprobe} {
		version := realMediaCommand(t, tool, "-version")
		binaryBytes, err := os.ReadFile(tool)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("platform=%s/%s tool=%s version=%s sha256=%x", runtime.GOOS, runtime.GOARCH, filepath.Base(tool), strings.SplitN(string(version), "\n", 2)[0], sha256.Sum256(binaryBytes))
	}
	encoders := realMediaCommand(t, ffmpeg, "-hide_banner", "-encoders")
	for _, required := range []string{"libx264", "libx265", "mpeg4", "aac"} {
		found := false
		for _, line := range strings.Split(string(encoders), "\n") {
			fields := strings.Fields(line)
			if len(fields) > 1 && fields[1] == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("opted-in media acceptance lacks required encoder %s", required)
		}
	}
	t.Setenv("REDIS_URL", "")
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, encoder, codec string
		audio                bool
	}{
		{"h265-silent", "libx265", "hevc", false},
		{"h265-audio", "libx265", "hevc", true},
		{"mpeg4-silent", "mpeg4", "mpeg4", false},
		{"mpeg4-audio", "mpeg4", "mpeg4", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.mp4")
			duration, durationMs := "0.4", int64(400)
			width, height := 66, 50
			browserDir := os.Getenv("PROFILE_MEDIA_BROWSER_DIR")
			if browserDir != "" && tc.name == "mpeg4-audio" {
				duration, durationMs = "6", 6000
				width, height = 640, 360
			}
			args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-y", "-f", "lavfi", "-i", fmt.Sprintf("testsrc2=size=%dx%d:rate=10", width, height)}
			if tc.audio {
				args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100")
			}
			args = append(args, "-t", duration, "-map", "0:v:0")
			if tc.audio {
				args = append(args, "-map", "1:a:0")
			}
			args = append(args, "-c:v", tc.encoder, "-threads", "1", "-pix_fmt", "yuv420p")
			if tc.encoder == "libx265" {
				args = append(args, "-preset", "ultrafast", "-x265-params", "pools=1:frame-threads=1:log-level=error", "-tag:v", "hvc1")
			}
			if tc.audio {
				args = append(args, "-c:a", "aac", "-b:a", "32k")
			} else {
				args = append(args, "-an")
			}
			args = append(args, "-movflags", "+faststart", input)
			realMediaCommand(t, ffmpeg, args...)
			assertRealMediaStreams(t, ffprobe, input, tc.codec, tc.audio)
			inputBytes, err := os.ReadFile(input)
			if err != nil {
				t.Fatal(err)
			}

			dataDir := filepath.Join(dir, "data")
			if err := os.Mkdir(dataDir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dataDir, ".settings-key"), []byte("01234567890123456789012345678901"), 0600); err != nil {
				t.Fatal(err)
			}
			db, err := gorm.Open(sqlite.Open(filepath.Join(dataDir, "state.sqlite")+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			sqlDB.SetMaxOpenConns(1)
			t.Cleanup(func() { _ = sqlDB.Close() })
			if err := database.MigrateSchema(db); err != nil {
				t.Fatal(err)
			}
			for _, userID := range []string{"media-owner", "media-other"} {
				for _, record := range []any{
					&model.User{ID: userID, Username: userID, Email: userID + "@example.invalid", Role: model.UserRoleUser, Status: model.UserStatusActive},
					&model.AuthSession{ID: userID + "-session", UserID: userID, TokenHash: auth.HashToken("synthetic-media-token"), ExpiresAt: time.Now().Add(time.Hour)},
				} {
					if err := db.Create(record).Error; err != nil {
						t.Fatal(err)
					}
				}
			}
			router := gin.New()
			svc, controlled := newRealMediaAcceptanceService(t, repository.New(db), dataDir, ffmpeg)
			previousRuntime := runtimeService
			ConfigureRuntime(svc)
			t.Cleanup(func() { ConfigureRuntime(previousRuntime); _ = svc.Close() })
			RegisterUserDataRoutes(router.Group("/api"), svc)
			var body bytes.Buffer
			form := multipart.NewWriter(&body)
			part, err := form.CreateFormFile("file", tc.name+".mp4")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write(inputBytes); err != nil {
				t.Fatal(err)
			}
			for key, value := range map[string]string{"kind": "video", "width": fmt.Sprint(width), "height": fmt.Sprint(height), "durationMs": fmt.Sprint(durationMs)} {
				if err := form.WriteField(key, value); err != nil {
					t.Fatal(err)
				}
			}
			if err := form.Close(); err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/api/resources", bytes.NewReader(body.Bytes()))
			request.Header.Set("Content-Type", form.FormDataContentType())
			request.AddCookie(realMediaCookie("media-owner"))
			upload := httptest.NewRecorder()
			router.ServeHTTP(upload, request)
			var envelope struct {
				Code int `json:"code"`
				Data struct {
					Resource model.Resource `json:"resource"`
				} `json:"data"`
			}
			if err := json.Unmarshal(upload.Body.Bytes(), &envelope); err != nil || upload.Code != http.StatusOK || envelope.Code != 0 || envelope.Data.Resource.ID == "" {
				t.Fatalf("actual multipart upload: status=%d body=%s decode=%v", upload.Code, upload.Body.String(), err)
			}
			id := envelope.Data.Resource.ID
			deadline := time.Now().Add(45 * time.Second)
			for {
				response := realMediaGet(router, "/api/resources/"+id, "", "media-owner")
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || response.Code != http.StatusOK || envelope.Code != 0 {
					t.Fatalf("actual metadata: status=%d body=%s decode=%v", response.Code, response.Body.String(), err)
				}
				state := envelope.Data.Resource.PlaybackStatus
				if state == model.PlaybackStatusReady {
					break
				}
				if state == model.PlaybackStatusFailed || state == model.PlaybackStatusNone || time.Now().After(deadline) {
					t.Fatalf("actual upload did not reach playback ready: %+v", envelope.Data.Resource)
				}
				time.Sleep(20 * time.Millisecond)
			}
			var row model.Resource
			if err := db.First(&row, "id = ?", id).Error; err != nil {
				t.Fatal(err)
			}
			if row.UserID != "media-owner" || row.Kind != "video" || row.Status != model.ResourceStatusReady || row.Provider != "local" || row.MimeType != "video/mp4" || row.Size != int64(len(inputBytes)) || row.PlaybackStatus != model.PlaybackStatusReady || row.PlaybackObjectKey != id+".mp4" || row.Error != "" || row.PlaybackError != "" || row.Width != width || row.Height != height || row.DurationMs != durationMs {
				t.Fatalf("resource publication mismatch: %+v", row)
			}
			var operations []model.ResourceProducerOperation
			if err := db.Where("resource_id = ?", id).Find(&operations).Error; err != nil {
				t.Fatal(err)
			}
			if len(operations) != 2 {
				t.Fatalf("expected original and playback obligations, got %+v", operations)
			}
			variants := map[string]bool{}
			for _, op := range operations {
				if op.UserID != row.UserID || op.Provider != "local" || op.Identity == "" || op.PhysicalState != "settled" || op.Outcome != "success" || op.Fenced {
					t.Fatalf("unsettled or unexpectedly controlled operation: %+v", op)
				}
				switch op.Variant {
				case "original":
					if op.Protocol != repository.LocalGoProducerProtocol || op.Proof != "local-writer-returned" || op.ControlOwner != "" || op.ControlPhase != "" || op.ControlResult != "" {
						t.Fatalf("original proof: %+v", op)
					}
				case "playback":
					if controlled {
						if op.Protocol != repository.ControlledPlaybackProtocol || op.Proof != "controlled-domain-completed" || op.ControlOwner == "" || op.ControlPhase != "stopped" || op.ControlResult != "success" {
							t.Fatalf("Windows C1 did not durably stop/publish exact operation: %+v", op)
						}
					} else if op.Protocol != "producer-accounting-v1" || op.Proof != "ffmpeg-wait-returned" || op.ControlOwner != "" || op.ControlPhase != "" || op.ControlResult != "" {
						t.Fatalf("playback proof: %+v", op)
					}
				default:
					t.Fatalf("unexpected operation: %+v", op)
				}
				if variants[op.Variant] {
					t.Fatalf("duplicate obligation: %+v", op)
				}
				variants[op.Variant] = true
			}
			original, err := os.ReadFile(filepath.Join(dataDir, "resources", filepath.FromSlash(row.ObjectKey)))
			if err != nil || !bytes.Equal(original, inputBytes) {
				t.Fatalf("uploaded original bytes changed: %v", err)
			}
			output := filepath.Join(dataDir, "playback", row.PlaybackObjectKey)
			assertRealMediaStreams(t, ffprobe, output, "h264", tc.audio)
			outputBytes, err := os.ReadFile(output)
			if err != nil {
				t.Fatal(err)
			}
			assertRealMediaMP4(t, outputBytes)
			fileURL := "/api/resources/" + id + "/file?variant=playback"
			whole := realMediaGet(router, fileURL, "", "media-owner")
			if whole.Code != http.StatusOK || !bytes.Equal(whole.Body.Bytes(), outputBytes) || whole.Header().Get("Content-Type") != "video/mp4" || whole.Header().Get("Accept-Ranges") != "bytes" || whole.Header().Get("Content-Length") != fmt.Sprint(len(outputBytes)) {
				t.Fatalf("whole playback route: status=%d headers=%v bytes=%d", whole.Code, whole.Header(), whole.Body.Len())
			}
			var reconstructed []byte
			chunk := (len(outputBytes) + 2) / 3
			for start := 0; start < len(outputBytes); start += chunk {
				end := min(start+chunk, len(outputBytes)) - 1
				response := realMediaGet(router, fileURL, fmt.Sprintf("bytes=%d-%d", start, end), "media-owner")
				want := fmt.Sprintf("bytes %d-%d/%d", start, end, len(outputBytes))
				if response.Code != http.StatusPartialContent || response.Header().Get("Content-Range") != want || response.Header().Get("Content-Length") != fmt.Sprint(end-start+1) || !bytes.Equal(response.Body.Bytes(), outputBytes[start:end+1]) {
					t.Fatalf("range route: status=%d headers=%v bytes=%d want=%s", response.Code, response.Header(), response.Body.Len(), want)
				}
				reconstructed = append(reconstructed, response.Body.Bytes()...)
			}
			if !bytes.Equal(reconstructed, outputBytes) {
				t.Fatal("real HTTP ranges did not reconstruct playback file")
			}
			suffix := realMediaGet(router, fileURL, "bytes=-11", "media-owner")
			if suffix.Code != 206 || suffix.Header().Get("Content-Range") != fmt.Sprintf("bytes %d-%d/%d", len(outputBytes)-11, len(outputBytes)-1, len(outputBytes)) || !bytes.Equal(suffix.Body.Bytes(), outputBytes[len(outputBytes)-11:]) {
				t.Fatalf("suffix route: status=%d headers=%v", suffix.Code, suffix.Header())
			}
			unsatisfiable := realMediaGet(router, fileURL, fmt.Sprintf("bytes=%d-", len(outputBytes)+1), "media-owner")
			if unsatisfiable.Code != 416 || unsatisfiable.Header().Get("Content-Range") != fmt.Sprintf("bytes */%d", len(outputBytes)) {
				t.Fatalf("unsatisfiable route: status=%d headers=%v", unsatisfiable.Code, unsatisfiable.Header())
			}
			if malformed := realMediaGet(router, fileURL, "bytes=invalid", "media-owner"); malformed.Code != 416 {
				t.Fatalf("malformed range returned %d", malformed.Code)
			}
			for user, want := range map[string]int{"": 401, "media-other": 404} {
				if denied := realMediaGet(router, fileURL, "", user); denied.Code != want {
					t.Fatalf("playback auth %q returned %d want %d", user, denied.Code, want)
				}
			}
			returned := filepath.Join(dir, "http-playback.mp4")
			if err := os.WriteFile(returned, whole.Body.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
			realMediaCommand(t, ffmpeg, "-hide_banner", "-loglevel", "error", "-xerror", "-nostdin", "-threads", "1", "-i", returned, "-map", "0:v:0", "-map", "0:a?", "-f", "null", "-")
			t.Logf("real multipart -> original/playback settled -> h264/AAC/faststart -> whole/range/reconstruction/auth -> complete decode PASS; WindowsC1=%v case=%s input=%d output=%d sha256=%x", controlled, tc.name, len(inputBytes), len(outputBytes), sha256.Sum256(outputBytes))
			if browserDir != "" && tc.name == "mpeg4-audio" {
				realMediaBrowserFixture(t, router, fileURL, browserDir, &row, outputBytes, controlled)
			}
		})
	}
}

func realMediaBrowserFixture(t *testing.T, router http.Handler, fileURL, proofDir string, row *model.Resource, expected []byte, controlled bool) {
	t.Helper()
	if !filepath.IsAbs(proofDir) {
		t.Fatal("browser proof directory must be an explicit absolute path")
	}
	if err := os.MkdirAll(proofDir, 0700); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(proofDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("invalid browser proof directory: %v", err)
	}
	readyPath, releasePath := filepath.Join(proofDir, "ready.json"), filepath.Join(proofDir, "release")
	for _, path := range []string{readyPath, releasePath} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("browser proof requires fresh sentinel paths: %s (%v)", filepath.Base(path), err)
		}
	}
	server := httptest.NewServer(router) // loopback only, the same real Gin routes
	defer server.Close()
	request, err := http.NewRequest(http.MethodGet, server.URL+fileURL+"&access=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(realMediaCookie("media-owner"))
	client := server.Client()
	client.Timeout = 10 * time.Second
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Code int `json:"code"`
		Data struct {
			Access service.ResourceAccess `json:"access"`
		} `json:"data"`
	}
	err = json.NewDecoder(response.Body).Decode(&envelope)
	_ = response.Body.Close()
	access := envelope.Data.Access
	if err != nil || response.StatusCode != 200 || envelope.Code != 0 || access.ResourceID != row.ID || string(access.ActualVariant) != "playback" || access.URL == "" {
		t.Fatalf("actual authenticated browser access: status=%d decode=%v access=%+v", response.StatusCode, err, access)
	}
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := url.Parse(access.URL)
	if err != nil {
		t.Fatal(err)
	}
	public := base.ResolveReference(reference)
	if public.Scheme != base.Scheme || public.Host != base.Host || !strings.HasPrefix(public.Path, "/api/public/resources/") || public.Query().Get("variant") != "playback" || public.Query().Get("signature") == "" {
		t.Fatal("expected a real same-loopback signed playback route")
	}
	publicResponse, err := client.Get(public.String())
	if err != nil {
		t.Fatal(err)
	}
	var publicBody bytes.Buffer
	_, copyErr := publicBody.ReadFrom(publicResponse.Body)
	closeErr := publicResponse.Body.Close()
	if copyErr != nil || closeErr != nil || publicResponse.StatusCode != 200 || !bytes.Equal(publicBody.Bytes(), expected) {
		t.Fatalf("actual signed public playback mismatch: status=%d copy=%v close=%v", publicResponse.StatusCode, copyErr, closeErr)
	}
	ready, err := json.MarshalIndent(struct {
		BaseURL                 string  `json:"baseURL"`
		PlaybackURL             string  `json:"playbackURL"`
		ResourceID              string  `json:"resourceID"`
		ExpectedDurationSeconds float64 `json:"expectedDurationSeconds"`
		ExpectedBytes           int     `json:"expectedBytes"`
		ExpectedWidth           int     `json:"expectedWidth"`
		ExpectedHeight          int     `json:"expectedHeight"`
		WindowsC1               bool    `json:"windowsC1"`
	}{server.URL, public.String(), row.ID, float64(row.DurationMs) / 1000, len(expected), row.Width, row.Height, controlled}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(proofDir, ".ready-*.json")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(ready); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(file.Name(), readyPath); err != nil {
		t.Fatal(err)
	}
	t.Logf("browser fixture ready at %s; release the fresh 'release' sentinel within 4 minutes; browser verification is recorded separately", readyPath)
	timer := time.NewTimer(4 * time.Minute)
	defer timer.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-timer.C:
			t.Fatal("browser fixture timed out awaiting release; no browser pass claimed")
		case <-ticker.C:
			info, err := os.Lstat(releasePath)
			if err == nil {
				if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
					t.Fatal("browser release sentinel must be a regular file")
				}
				return
			}
			if !os.IsNotExist(err) {
				t.Fatal(err)
			}
		}
	}
}

func realMediaCommand(t *testing.T, tool string, args ...string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, tool, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("real media command %s %v: %v\n%s", filepath.Base(tool), args, err, output)
	}
	return output
}

func realMediaCookie(user string) *http.Cookie {
	return &http.Cookie{Name: service.SessionCookieName, Value: user + "-session.synthetic-media-token"}
}

func realMediaGet(router http.Handler, url, byteRange, user string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodGet, url, nil)
	if user != "" {
		request.AddCookie(realMediaCookie(user))
	}
	if byteRange != "" {
		request.Header.Set("Range", byteRange)
	}
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func assertRealMediaStreams(t *testing.T, ffprobe, path, codec string, audio bool) {
	t.Helper()
	output := realMediaCommand(t, ffprobe, "-v", "error", "-show_entries", "stream=codec_type,codec_name,pix_fmt,width,height", "-of", "json", path)
	var result struct {
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			PixFmt    string `json:"pix_fmt"`
			Width     int    `json:"width"`
			Height    int    `json:"height"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatal(err)
	}
	videoCount, audioCount := 0, 0
	for _, stream := range result.Streams {
		switch stream.CodecType {
		case "video":
			videoCount++
			if stream.CodecName != codec || stream.PixFmt != "yuv420p" || stream.Width <= 0 || stream.Height <= 0 || stream.Width%2 != 0 || stream.Height%2 != 0 {
				t.Fatalf("unexpected real video stream: %+v", stream)
			}
		case "audio":
			audioCount++
			if stream.CodecName != "aac" {
				t.Fatalf("unexpected real audio stream: %+v", stream)
			}
		default:
			t.Fatalf("unexpected media stream: %+v", stream)
		}
	}
	wantAudio := 0
	if audio {
		wantAudio = 1
	}
	if videoCount != 1 || audioCount != wantAudio {
		t.Fatalf("stream counts video=%d audio=%d wantAudio=%d: %s", videoCount, audioCount, wantAudio, output)
	}
}

func assertRealMediaMP4(t *testing.T, data []byte) {
	t.Helper()
	moov, mdat := -1, -1
	for offset := 0; offset < len(data); {
		if len(data)-offset < 8 {
			t.Fatal("truncated MP4 box header")
		}
		size := uint64(binary.BigEndian.Uint32(data[offset : offset+4]))
		kind := string(data[offset+4 : offset+8])
		header := uint64(8)
		if size == 1 {
			if len(data)-offset < 16 {
				t.Fatal("truncated extended MP4 box")
			}
			size = binary.BigEndian.Uint64(data[offset+8 : offset+16])
			header = 16
		} else if size == 0 {
			size = uint64(len(data) - offset)
		}
		if size < header || size > uint64(len(data)-offset) {
			t.Fatalf("invalid MP4 box %s size=%d", kind, size)
		}
		switch kind {
		case "moov":
			moov = offset
		case "mdat":
			mdat = offset
		case "moof":
			t.Fatal("fragmented MP4 is not ordinary faststart playback")
		}
		offset += int(size)
	}
	if moov < 0 || mdat < 0 || moov >= mdat {
		t.Fatalf("playback is not faststart MP4: moov=%d mdat=%d", moov, mdat)
	}
}
