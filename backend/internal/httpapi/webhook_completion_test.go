package httpapi

import (
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestGenericImageWebhookFinalizesResourceAssetAndJob(t *testing.T) {
	server, router := testServerRouter(t)
	now := response.Now()
	character := models.Character{DramaID: 1, Name: "webhook character", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&character).Error; err != nil {
		t.Fatal(err)
	}
	record := models.ImageGeneration{CharacterID: &character.ID, TaskID: "webhook-image-1", Status: "processing", Provider: "volcengine", ImageType: "character", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&record).Error; err != nil {
		t.Fatal(err)
	}
	job, err := server.Jobs.CreateForTargetOrganization(0, "image.generate", "image_generation", record.ID, record.Provider, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Model(&record).Update("job_id", job.ID).Error; err != nil {
		t.Fatal(err)
	}

	mockDir := filepath.Join(os.TempDir(), "flyaimovie-mock")
	if err := os.MkdirAll(mockDir, 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(mockDir, "webhook-*.png")
	if err != nil {
		t.Fatal(err)
	}
	path := file.Name()
	defer os.Remove(path)
	if err := png.Encode(file, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	body := `{"type":"image","task_id":"webhook-image-1","status":"completed","url":"file://` + path + `"}`
	completed := performRequest(router, http.MethodPost, "/api/v1/webhooks/generic", body, signedWebhookHeaders(body, "webhook-complete-1", time.Now()))
	if completed.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", completed.Code, completed.Body.String())
	}
	if err := db.DB.First(&record, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != "completed" || !strings.HasPrefix(record.ImageURL, "/static/images/") {
		t.Fatalf("record=%+v", record)
	}
	if err := db.DB.First(&character, character.ID).Error; err != nil {
		t.Fatal(err)
	}
	if character.ImageURL != record.ImageURL {
		t.Fatalf("character=%+v", character)
	}
	var asset models.Asset
	if err := db.DB.Where("image_gen_id = ?", record.ID).First(&asset).Error; err != nil {
		t.Fatal(err)
	}
	storedJob, err := server.Jobs.Get(job.ID)
	if err != nil || storedJob.Status != "succeeded" {
		t.Fatalf("job=%+v err=%v", storedJob, err)
	}
}

func TestGenericWebhookIgnoresAmbiguousProviderTask(t *testing.T) {
	_, router := testServerRouter(t)
	now := response.Now()
	records := []models.ImageGeneration{
		{OrganizationID: 1, TaskID: "duplicate-task", Status: "processing", Provider: "volcengine", CreatedAt: now, UpdatedAt: now},
		{OrganizationID: 2, TaskID: "duplicate-task", Status: "processing", Provider: "volcengine", CreatedAt: now, UpdatedAt: now},
	}
	if err := db.DB.Create(&records).Error; err != nil {
		t.Fatal(err)
	}
	body := `{"type":"image","task_id":"duplicate-task","status":"failed","error":"must not apply"}`
	result := performRequest(router, http.MethodPost, "/api/v1/webhooks/generic", body, signedWebhookHeaders(body, "ambiguous-task-1", time.Now()))
	if result.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", result.Code, result.Body.String())
	}
	var changed int64
	if err := db.DB.Model(&models.ImageGeneration{}).Where("task_id = ? AND status != ?", "duplicate-task", "processing").Count(&changed).Error; err != nil {
		t.Fatal(err)
	}
	if changed != 0 {
		t.Fatalf("ambiguous task changed %d records", changed)
	}
}

func TestViduWebhookMapsNestedStatusAndRejectsReplay(t *testing.T) {
	_, router := testServerRouter(t)
	now := response.Now()
	record := models.VideoGeneration{TaskID: "vidu-task-1", Status: "processing", Provider: "vidu", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&record).Error; err != nil {
		t.Fatal(err)
	}

	processingBody := `{"data":{"task_id":"vidu-task-1"},"status":"running"}`
	headers := signedWebhookHeaders(processingBody, "vidu-processing-1", time.Now())
	processing := performRequest(router, http.MethodPost, "/api/v1/webhooks/vidu", processingBody, headers)
	if processing.Code != http.StatusOK {
		t.Fatalf("processing status=%d body=%s", processing.Code, processing.Body.String())
	}
	replay := performRequest(router, http.MethodPost, "/api/v1/webhooks/vidu", processingBody, headers)
	if replay.Code != http.StatusOK || !strings.Contains(replay.Body.String(), `"duplicate":true`) {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	failedBody := `{"id":"vidu-task-1","state":"failed","err_code":"E_PROVIDER","err_msg":"generation failed"}`
	failed := performRequest(router, http.MethodPost, "/api/v1/webhooks/vidu", failedBody, signedWebhookHeaders(failedBody, "vidu-failed-1", time.Now()))
	if failed.Code != http.StatusOK {
		t.Fatalf("failed status=%d body=%s", failed.Code, failed.Body.String())
	}
	if err := db.DB.First(&record, record.ID).Error; err != nil {
		t.Fatal(err)
	}
	if record.Status != "failed" || !strings.Contains(record.ErrorMsg, "E_PROVIDER") {
		t.Fatalf("record=%+v", record)
	}

	missingTask := `{"status":"completed"}`
	bad := performRequest(router, http.MethodPost, "/api/v1/webhooks/vidu", missingTask, signedWebhookHeaders(missingTask, "vidu-missing-1", time.Now()))
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("missing task status=%d body=%s", bad.Code, bad.Body.String())
	}
}
