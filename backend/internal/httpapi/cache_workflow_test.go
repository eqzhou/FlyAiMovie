package httpapi

import (
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/services/mediacache"
)

func TestMediaCacheStatsAndPurgeWorkflow(t *testing.T) {
	server, router := testServerRouter(t)
	relative, absolute, err := server.Store.SaveBytes("cache", "expired.bin", []byte("expired"))
	if err != nil {
		t.Fatal(err)
	}
	object, _, err := server.Cache.Put(mediacache.PutInput{Namespace: "test", Key: "expired", ContentHash: "expired-hash", Kind: "binary", LocalPath: relative, PublicURL: server.Store.PublicURL(relative), Size: 7})
	if err != nil {
		t.Fatal(err)
	}
	if err := server.Cache.Release(0, "test", "expired"); err != nil {
		t.Fatal(err)
	}
	expired := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if err := db.DB.Model(&models.MediaCacheObject{}).Where("id = ?", object.ID).Update("expires_at", expired).Error; err != nil {
		t.Fatal(err)
	}
	stats := performRequest(router, http.MethodGet, "/api/v1/organization/cache", "", nil)
	if stats.Code != http.StatusOK {
		t.Fatalf("stats status=%d body=%s", stats.Code, stats.Body.String())
	}
	purged := performRequest(router, http.MethodPost, "/api/v1/organization/cache/purge", `{}`, nil)
	if purged.Code != http.StatusOK {
		t.Fatalf("purge status=%d body=%s", purged.Code, purged.Body.String())
	}
	if _, err := os.Stat(absolute); !os.IsNotExist(err) {
		t.Fatalf("expired file still exists: %v", err)
	}
	var task models.MediaDeletionTask
	if err := db.DB.Where("organization_id = ? AND local_path = ?", 0, relative).First(&task).Error; err != nil {
		t.Fatal(err)
	}
	if task.Status != "completed" || task.Reason != "cache_expired" {
		t.Fatalf("cleanup task=%+v", task)
	}
}

func TestMediaCachePurgeValidatesLimitsAndSupportsEmptyCache(t *testing.T) {
	_, router := testServerRouter(t)
	for _, body := range []string{`{"limit":-1}`, `{"limit":1001}`, `{"limit":"bad"}`, `{"broken":`} {
		response := performRequest(router, http.MethodPost, "/api/v1/organization/cache/purge", body, nil)
		if response.Code != http.StatusBadRequest {
			t.Errorf("body=%s status=%d response=%s", body, response.Code, response.Body.String())
		}
	}
	response := performRequest(router, http.MethodPost, "/api/v1/organization/cache/purge", `{"limit":1}`, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("empty purge status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestMediaCachePurgeRequiresAdminRole(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Auth = config.AuthConfig{Enabled: true, SessionTTLHours: 24, CookieName: "fly_session"}
	router := server.Router()
	cookie, csrf, _ := createTestActorSession(t, server, "cache-editor@example.com", "cache-editor", "editor")
	stats := performAuthRequest(router, http.MethodGet, "/api/v1/organization/cache", "", cookie, "")
	if stats.Code != http.StatusOK {
		t.Fatalf("stats status=%d body=%s", stats.Code, stats.Body.String())
	}
	purge := performAuthRequest(router, http.MethodPost, "/api/v1/organization/cache/purge", `{}`, cookie, csrf)
	if purge.Code != http.StatusForbidden {
		t.Fatalf("purge status=%d body=%s", purge.Code, purge.Body.String())
	}
}
