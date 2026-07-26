package httpapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestMediaUploadProbeRepairAndAssetApplication(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required")
	}
	_, router := testServerRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "Media Drama", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "Episode", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	storyboard := models.Storyboard{EpisodeID: episode.ID, StoryboardNumber: 1, Title: "Shot", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&storyboard).Error; err != nil {
		t.Fatal(err)
	}

	videoPath := filepath.Join(t.TempDir(), "probe.mp4")
	command := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-f", "lavfi", "-i", "color=c=black:s=64x48:d=0.6", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-y", videoPath)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, output)
	}
	response := performMediaUpload(t, router, videoPath, map[string]string{
		"name": "Probe Video", "drama_id": idText(drama.ID), "episode_id": idText(episode.ID), "storyboard_id": idText(storyboard.ID),
	})
	if response.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", response.Code, response.Body.String())
	}
	data := decodeResponse(t, response)["data"].(map[string]any)
	assetID := uint(data["id"].(float64))
	if data["probe_status"] != "completed" || data["duration_seconds"].(float64) <= 0 || data["width"] != float64(64) || data["height"] != float64(48) {
		t.Fatalf("metadata=%#v", data)
	}
	duplicateResponse := performMediaUpload(t, router, videoPath, map[string]string{"name": "Duplicate Video"})
	if duplicateResponse.Code != http.StatusCreated {
		t.Fatalf("duplicate upload status=%d body=%s", duplicateResponse.Code, duplicateResponse.Body.String())
	}
	duplicateData := decodeResponse(t, duplicateResponse)["data"].(map[string]any)
	duplicateAssetID := uint(duplicateData["id"].(float64))
	if duplicateData["url"] != data["url"] || duplicateData["local_path"] != data["local_path"] {
		t.Fatalf("upload was not deduplicated: first=%#v duplicate=%#v", data, duplicateData)
	}
	var cacheObject models.MediaCacheObject
	if err := db.DB.Where("organization_id = ? AND content_hash = ?", 0, data["content_hash"]).First(&cacheObject).Error; err != nil {
		t.Fatal(err)
	}
	if cacheObject.ReferenceCount != 2 {
		t.Fatalf("cache reference count=%d want 2", cacheObject.ReferenceCount)
	}
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/assets/"+idText(assetID)+"/probe", `{}`, http.StatusOK)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/assets/metadata/repair", `{}`, http.StatusOK)

	imageAsset := models.Asset{DramaID: &drama.ID, EpisodeID: &episode.ID, StoryboardID: &storyboard.ID, Name: "Frame", Type: "image", Category: "frame", URL: "https://example.com/frame.png", MimeType: "image/png", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&imageAsset).Error; err != nil {
		t.Fatal(err)
	}
	for _, frameType := range []string{"first_frame", "last_frame", "composed"} {
		assertRequestStatus(t, router, http.MethodPost, "/api/v1/assets/"+idText(imageAsset.ID)+"/apply", `{"storyboard_id":`+idText(storyboard.ID)+`,"frame_type":"`+frameType+`"}`, http.StatusOK)
	}
	if err := db.DB.First(&storyboard, storyboard.ID).Error; err != nil {
		t.Fatal(err)
	}
	if storyboard.FirstFrameImage != imageAsset.URL || storyboard.LastFrameImage != imageAsset.URL || storyboard.ComposedImage != imageAsset.URL {
		t.Fatalf("storyboard frames=%+v", storyboard)
	}
	assertRequestStatus(t, router, http.MethodGet, "/api/v1/assets/"+idText(assetID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodPut, "/api/v1/assets/"+idText(assetID), `{"is_favorite":true,"duration":1}`, http.StatusOK)
	assertRequestStatus(t, router, http.MethodDelete, "/api/v1/assets/"+idText(assetID), "", http.StatusOK)
	assertRequestStatus(t, router, http.MethodDelete, "/api/v1/assets/"+idText(duplicateAssetID), "", http.StatusOK)
}

func TestHTTPConversionHelpers(t *testing.T) {
	if optionalFormID("") != nil || optionalFormID("0") != nil || optionalFormID("bad") != nil {
		t.Fatal("invalid optional form ID accepted")
	}
	if value := optionalFormID(" 42 "); value == nil || *value != 42 {
		t.Fatalf("optional ID=%v", value)
	}
	if got := uniquePositiveIDs([]any{float64(2), "3", 2, 0, float64(2)}); !reflect.DeepEqual(got, []uint{2, 3}) {
		t.Fatalf("unique IDs=%v", got)
	}
	if asString(nil) != "" || asString("ok") != "ok" || asString(1) != "" {
		t.Fatal("asString failed")
	}
	if asInt(float64(1.9)) != 1 || asInt(2) != 2 || asInt("3") != 3 || asInt(true) != 0 || asUint("4") != 4 {
		t.Fatal("integer conversion failed")
	}
	if err := validateAIConfigReference(9999, "image"); err == nil {
		t.Fatal("missing AI config accepted")
	}
}

func TestMediaUploadRejectsOversizedContentBeforeParsingMultipart(t *testing.T) {
	_, router := testServerRouter(t)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/upload/media", strings.NewReader("not multipart"))
	request.ContentLength = maxMediaUploadRequestBytes + 1
	request.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	result := httptest.NewRecorder()

	router.ServeHTTP(result, request)
	if result.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", result.Code, result.Body.String())
	}
}

func performMediaUpload(t *testing.T, router http.Handler, filePath string, fields map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	fileWriter, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileWriter.Write(content); err != nil {
		t.Fatal(err)
	}
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/upload/media", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	result := httptest.NewRecorder()
	router.ServeHTTP(result, request)
	return result
}
