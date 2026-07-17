package agents

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
)

type runnerFixture struct {
	runner  *Runner
	drama   models.Drama
	episode models.Episode
}

func newRunnerFixture(t *testing.T, modelURL string, agentConfig *models.AgentConfig) runnerFixture {
	t.Helper()
	database, err := db.Open(t.TempDir() + "/runner.db")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(database); err != nil {
		t.Fatal(err)
	}
	now := response.Now()
	drama := models.Drama{OrganizationID: 21, Title: "Runner Drama", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{OrganizationID: 21, DramaID: drama.ID, EpisodeNumber: 1, Title: "Runner Episode", Content: "original", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	service := models.AIServiceConfig{
		OrganizationID: 21, ServiceType: "text", Provider: "openai_local", Name: "local-test",
		BaseURL: modelURL, Model: "service-model", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now,
	}
	if err := database.Create(&service).Error; err != nil {
		t.Fatal(err)
	}
	if agentConfig != nil {
		config := *agentConfig
		config.OrganizationID = 21
		config.AgentType = "script_rewriter"
		config.Name = "Script Runner"
		config.IsActive = true
		config.CreatedAt = now
		config.UpdatedAt = now
		if err := database.Create(&config).Error; err != nil {
			t.Fatal(err)
		}
	}
	return runnerFixture{runner: NewRunner(t.TempDir()), drama: drama, episode: episode}
}

func TestRunnerRunExecutesReadThenWriteModelPass(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		requestCount++
		if request.URL.Path != "/v1/chat/completions" {
			t.Errorf("path=%q", request.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["model"] != "agent-model" || body["max_tokens"] != float64(123) {
			t.Errorf("model request=%#v", body)
		}
		messages, _ := body["messages"].([]any)
		if len(messages) != 2 {
			t.Errorf("messages=%#v", messages)
		}
		if requestCount == 1 {
			if body["temperature"] != float64(0.7) {
				t.Errorf("first temperature=%v", body["temperature"])
			}
			writeChatResponse(w, "```json\n{\"actions\":[{\"tool\":\"read_episode_script\",\"args\":{}}],\"summary\":\"reading\"}\n```")
			return
		}
		if body["temperature"] != float64(0.3) {
			t.Errorf("second temperature=%v", body["temperature"])
		}
		writeChatResponse(w, `{"actions":[{"tool":"read_episode_script","args":{}},{"tool":"save_script","args":{"script":"## S02 | 内景 · 车站 | 夜"}}],"summary":"saved"}`)
	}))
	defer server.Close()
	temperature, maxTokens, maxIterations := 0.7, 123, 3
	fixture := newRunnerFixture(t, server.URL, &models.AgentConfig{
		Model: "agent-model", SystemPrompt: "Custom system", Temperature: &temperature,
		MaxTokens: &maxTokens, MaxIterations: &maxIterations,
	})

	result, err := fixture.runner.Run(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "done" || result.Text != "saved" || len(result.ToolCalls) != 2 || requestCount != 2 {
		t.Fatalf("result=%+v requestCount=%d", result, requestCount)
	}
	if err := db.DB.First(&fixture.episode, fixture.episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fixture.episode.ScriptContent, "S02") {
		t.Fatalf("script=%q", fixture.episode.ScriptContent)
	}
}

func TestRunnerRunInvalidJSONUsesDeterministicWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeChatResponse(w, "## S03 | 外景 · 街道 | 日\n角色：出发。")
	}))
	defer server.Close()
	maxIterations := 1
	fixture := newRunnerFixture(t, server.URL, &models.AgentConfig{MaxIterations: &maxIterations})
	result, err := fixture.runner.Run(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "done" || len(result.ToolCalls) != 2 {
		t.Fatalf("result=%+v", result)
	}
	if err := db.DB.First(&fixture.episode, fixture.episode.ID).Error; err != nil || !strings.Contains(fixture.episode.ScriptContent, "S03") {
		t.Fatalf("episode=%+v err=%v", fixture.episode, err)
	}
}

func TestRunnerRunReportsToolFailureAndValidatesOwnership(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeChatResponse(w, `{"actions":[{"tool":"unknown","args":{}}],"summary":"bad"}`)
	}))
	defer server.Close()
	fixture := newRunnerFixture(t, server.URL, nil)
	result, err := fixture.runner.Run(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite")
	if err != nil || result.Type != "failed" || !hasToolFailure(result.ToolResults) {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := fixture.runner.Run(context.Background(), 21, "unknown", fixture.drama.ID, fixture.episode.ID, "x"); err == nil {
		t.Fatal("unsupported agent accepted")
	}
	if _, err := fixture.runner.Run(context.Background(), 21, "script_rewriter", fixture.drama.ID+999, fixture.episode.ID, "x"); err == nil {
		t.Fatal("cross-drama episode accepted")
	}
}

func writeChatResponse(writer http.ResponseWriter, content string) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}},
	})
}
