package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/services/skillregistry"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRunObservedUsesExactSkillSnapshotOverride(t *testing.T) {
	var systemMessage string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		var body struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if len(body.Messages) > 0 {
			systemMessage = body.Messages[0].Content
		}
		writeChatResponse(w, `{"actions":[{"tool":"save_script","args":{"script":"## S01 | 内景 · 工作室 | 日"}}],"summary":"saved"}`)
	}))
	defer server.Close()

	fixture := newRunnerFixture(t, server.URL, nil)
	registry := skillregistry.New(db.DB)
	current, err := registry.CreateVersion(21, 3, "script_rewriter", skillregistry.VersionInput{MainMarkdown: "current published skill marker"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(21, 3, "script_rewriter", current.ID); err != nil {
		t.Fatal(err)
	}

	snapshot := "exact source run skill marker"
	digest := sha256.Sum256([]byte(snapshot))
	override := SkillSnapshotOverride{
		SourceRunID: 91, SkillSource: "database", SkillID: 7, SkillVersionID: 8,
		SkillVersion: 2, SkillHash: hex.EncodeToString(digest[:]), SkillSnapshot: snapshot,
	}
	var events []RunEvent
	result, err := fixture.runner.RunObservedWithOptions(
		context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite",
		func(event RunEvent) { events = append(events, event) }, RunOptions{SkillSnapshot: &override},
	)
	if err != nil || result == nil || result.Type != "done" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if !strings.Contains(systemMessage, snapshot) || strings.Contains(systemMessage, current.MainMarkdown) {
		t.Fatalf("system did not use exact override: %q", systemMessage)
	}
	if len(events) == 0 || events[0].EventType != "prompt_resolved" {
		t.Fatalf("events=%+v", events)
	}
	payload := events[0].Payload
	if payload["skill_snapshot_mode"] != "source_run" || payload["skill_snapshot_source_run_id"] != uint(91) || payload["skill_hash"] != override.SkillHash {
		t.Fatalf("prompt event=%+v", payload)
	}
}

func TestResolveSystemPromptPrefersPublishedOrganizationSkillAndFallsBack(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:runner-skill-registry?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.Organization{}, &models.Skill{}, &models.SkillVersion{}, &models.SkillPublication{}, &models.PromptTemplate{}, &models.Drama{}, &models.Character{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	if err := database.Create(&models.Organization{ID: 8, Name: "Skill Org", Slug: "skill-org", Status: "active", CreatedAt: "now", UpdatedAt: "now"}).Error; err != nil {
		t.Fatal(err)
	}
	db.DB = database
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "extractor"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extractor", "SKILL.md"), []byte("builtin marker"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := NewRunner(dir)

	registry := skillregistry.New(database)
	version, err := registry.CreateVersion(8, 3, "extractor", skillregistry.VersionInput{MainMarkdown: "database marker", References: map[string]string{"references/rules.md": "reference marker"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(8, 3, "extractor", version.ID); err != nil {
		t.Fatal(err)
	}

	resolution, err := runner.resolveSystemPrompt(8, "extractor", 0, models.Episode{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resolution.System, "database marker") || !strings.Contains(resolution.System, "reference marker") || strings.Contains(resolution.System, "builtin marker") {
		t.Fatalf("system=%q", resolution.System)
	}
	if resolution.SkillSource != "database" || resolution.SkillID == 0 || resolution.SkillVersion != 1 || resolution.SkillHash != version.ContentSHA256 {
		t.Fatalf("resolution=%+v", resolution)
	}

	digest := sha256.Sum256([]byte(resolution.SkillSnapshot))
	if resolution.SkillHash != hex.EncodeToString(digest[:]) {
		t.Fatalf("skill hash %q does not verify snapshot", resolution.SkillHash)
	}

	fallback, err := runner.resolveSystemPrompt(9, "extractor", 0, models.Episode{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if fallback.SkillSource != "builtin" || !strings.Contains(fallback.System, "builtin marker") {
		t.Fatalf("fallback=%+v", fallback)
	}
}

func TestResolveSystemPromptUsesPublishedLocalWorkspaceSkill(t *testing.T) {
	database, err := gorm.Open(sqlite.Open("file:runner-local-skill-registry?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.AutoMigrate(&models.Organization{}, &models.Skill{}, &models.SkillVersion{}, &models.SkillPublication{}, &models.PromptTemplate{}, &models.Drama{}, &models.Character{}, &models.Scene{}); err != nil {
		t.Fatal(err)
	}
	db.DB = database
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "extractor"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extractor", "SKILL.md"), []byte("local builtin marker"), 0o600); err != nil {
		t.Fatal(err)
	}

	registry := skillregistry.New(database)
	version, err := registry.CreateVersion(0, 0, "extractor", skillregistry.VersionInput{MainMarkdown: "local database marker"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(0, 0, "extractor", version.ID); err != nil {
		t.Fatal(err)
	}

	resolution, err := NewRunner(dir).resolveSystemPrompt(0, "extractor", 0, models.Episode{}, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if resolution.SkillSource != "database" || !strings.Contains(resolution.System, "local database marker") || strings.Contains(resolution.System, "local builtin marker") {
		t.Fatalf("resolution=%+v", resolution)
	}
}

func TestRunObservedFailsForInvalidPublishedSkillInsteadOfUsingBuiltin(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		writeChatResponse(w, `{"actions":[],"summary":"unexpected"}`)
	}))
	defer server.Close()
	fixture := newRunnerFixture(t, server.URL, nil)
	registry := skillregistry.New(db.DB)
	version, err := registry.CreateVersion(21, 3, "script_rewriter", skillregistry.VersionInput{MainMarkdown: "database skill"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Publish(21, 3, "script_rewriter", version.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Model(&models.SkillVersion{}).Where("id = ?", version.ID).Update("references_json", "{").Error; err != nil {
		t.Fatal(err)
	}

	_, err = fixture.runner.RunObserved(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite", nil)
	if err == nil || !strings.Contains(err.Error(), "render published skill") {
		t.Fatalf("err=%v", err)
	}
	if requests != 0 {
		t.Fatalf("model was called %d times", requests)
	}
}
