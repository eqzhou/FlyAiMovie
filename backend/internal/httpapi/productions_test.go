package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestProductionHTTPCreateListCancelAndRetry(t *testing.T) {
	_, router := testServerRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "自动制作", TotalEpisodes: 1, Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "第一集", Content: "故事", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}

	bad := performRequest(router, http.MethodPost, "/api/v1/productions", `{"drama_id":0}`, nil)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad status=%d body=%s", bad.Code, bad.Body.String())
	}

	created := performRequest(router, http.MethodPost, "/api/v1/productions", `{"drama_id":`+itoa(drama.ID)+`,"episode_id":`+itoa(episode.ID)+`}`, nil)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}
	var envelope struct {
		Data models.ProductionRun `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.ID == 0 || envelope.Data.Stage != "script" {
		t.Fatalf("run=%+v", envelope.Data)
	}
	loaded := performRequest(router, http.MethodGet, "/api/v1/productions/"+itoa(envelope.Data.ID), "", nil)
	if loaded.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", loaded.Code, loaded.Body.String())
	}
	if invalid := performRequest(router, http.MethodGet, "/api/v1/productions/not-a-number", "", nil); invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid get=%d", invalid.Code)
	}
	if missing := performRequest(router, http.MethodGet, "/api/v1/productions/999999", "", nil); missing.Code != http.StatusNotFound {
		t.Fatalf("missing get=%d", missing.Code)
	}

	duplicate := performRequest(router, http.MethodPost, "/api/v1/productions", `{"drama_id":`+itoa(drama.ID)+`,"episode_id":`+itoa(episode.ID)+`}`, nil)
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", duplicate.Code, duplicate.Body.String())
	}

	listed := performRequest(router, http.MethodGet, "/api/v1/productions?episode_id="+itoa(episode.ID), "", nil)
	if listed.Code != http.StatusOK || !json.Valid(listed.Body.Bytes()) {
		t.Fatalf("list status=%d body=%s", listed.Code, listed.Body.String())
	}
	if invalidList := performRequest(router, http.MethodGet, "/api/v1/productions?episode_id=bad", "", nil); invalidList.Code != http.StatusBadRequest {
		t.Fatalf("invalid list=%d", invalidList.Code)
	}

	canceled := performRequest(router, http.MethodPost, "/api/v1/productions/"+itoa(envelope.Data.ID)+"/cancel", "", nil)
	if canceled.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", canceled.Code, canceled.Body.String())
	}
	retried := performRequest(router, http.MethodPost, "/api/v1/productions/"+itoa(envelope.Data.ID)+"/retry", "", nil)
	if retried.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", retried.Code, retried.Body.String())
	}
	if conflict := performRequest(router, http.MethodPost, "/api/v1/productions/"+itoa(envelope.Data.ID)+"/retry", "", nil); conflict.Code != http.StatusConflict {
		t.Fatalf("retry conflict=%d body=%s", conflict.Code, conflict.Body.String())
	}
}
