package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

func TestChatWithMaxTokensForwardsLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["max_tokens"] != float64(321) {
			t.Fatalf("max_tokens=%v, want 321", body["max_tokens"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	defer server.Close()

	result, err := ChatWithMaxTokens(context.Background(), &ServiceConfig{BaseURL: server.URL, APIKey: "test", Model: "test-model"}, "system", "user", 0.2, 321)
	if err != nil || result != "ok" {
		t.Fatalf("result=%q err=%v", result, err)
	}
}

func TestPreferredConfigMustMatchServiceType(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/ai.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	row := models.AIServiceConfig{
		ServiceType: "text", Provider: "mock", Name: "text-only", BaseURL: "http://localhost",
		APIKey: "mock", Model: "mock", IsActive: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := GetActiveConfig("image", &row.ID); err == nil {
		t.Fatal("text config was accepted for image generation")
	}
}

func TestTaskConfigCanLoadInactiveOriginalConfig(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/task.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	row := models.AIServiceConfig{
		ServiceType: "video", Provider: "vidu", Name: "disabled-after-submit", BaseURL: "https://api.example.com",
		APIKey: "secret", Model: "video", IsActive: false, CreatedAt: now, UpdatedAt: now,
	}
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	config, err := GetTaskConfig("video", &row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if config.ID != row.ID || config.Provider != "vidu" {
		t.Fatalf("unexpected task config: %+v", config)
	}
}

func TestOrganizationConfigCannotCrossTenant(t *testing.T) {
	database, err := db.Open(t.TempDir() + "/org.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	row := models.AIServiceConfig{OrganizationID: 2, ServiceType: "image", Provider: "mock", Name: "tenant-b", IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&row).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := GetOrganizationConfig(1, "image", &row.ID); err == nil {
		t.Fatal("tenant A selected tenant B config")
	}
}
