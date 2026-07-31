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
	"github.com/eqzhou/flyaimovie/internal/testsupport"
)

type runnerFixture struct {
	runner  *Runner
	drama   models.Drama
	episode models.Episode
}

func newRunnerFixture(t *testing.T, modelURL string, agentConfig *models.AgentConfig) runnerFixture {
	t.Helper()
	database := testsupport.OpenDatabase(t)
	now := response.Now()
	organization := models.Organization{ID: 21, Name: "Runner Org", Slug: "runner-org", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := database.Create(&organization).Error; err != nil {
		t.Fatal(err)
	}
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

func TestRunnerReportsSecondPassWriteFailure(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		if requestCount == 1 {
			writeChatResponse(w, `{"actions":[{"tool":"read_episode_script","args":{}}],"summary":"reading"}`)
			return
		}
		writeChatResponse(w, `{"actions":[{"tool":"save_script","args":{}}],"summary":"saved"}`)
	}))
	defer server.Close()
	maxIterations := 2
	fixture := newRunnerFixture(t, server.URL, &models.AgentConfig{Model: "agent-model", MaxIterations: &maxIterations})

	result, err := fixture.runner.Run(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite")
	if err != nil {
		t.Fatal(err)
	}
	if result.Type != "failed" || !hasToolFailure(result.ToolResults) {
		t.Fatalf("write failure reported as success: %+v", result)
	}
}

func TestRunnerDoesNotFallbackAfterRealProviderFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()
	fixture := newRunnerFixture(t, server.URL, nil)
	result, err := fixture.runner.Run(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite")
	if err == nil || result != nil {
		t.Fatalf("real provider failure used offline fallback: result=%+v err=%v", result, err)
	}
	if err := db.DB.First(&fixture.episode, fixture.episode.ID).Error; err != nil {
		t.Fatal(err)
	}
	if fixture.episode.ScriptContent != "" {
		t.Fatalf("provider failure mutated episode: %q", fixture.episode.ScriptContent)
	}
}

func TestRunnerUsesOrganizationPromptTemplateAndVersion(t *testing.T) {
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
		writeChatResponse(w, `{"actions":[{"tool":"save_script","args":{"script":"## S01 | 内景 · 车站 | 夜"}}],"summary":"saved"}`)
	}))
	defer server.Close()
	fixture := newRunnerFixture(t, server.URL, nil)
	now := response.Now()
	template := models.PromptTemplate{OrganizationID: 21, Key: "script_rewriter", Name: "Custom", Category: "agent_system", Content: "为 {{drama_title}} / {{episode_title}} 执行 {{user_instruction}}", VariablesJSON: `["drama_title","episode_title","user_instruction"]`, Version: 7, IsActive: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&template).Error; err != nil {
		t.Fatal(err)
	}
	var runEvents []RunEvent
	if _, err := fixture.runner.RunObserved(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "重写对白", func(event RunEvent) {
		runEvents = append(runEvents, event)
	}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Runner Drama", "Runner Episode", "重写对白", "提示词模板版本: 7", "save_script"} {
		if !strings.Contains(systemMessage, expected) {
			t.Fatalf("system prompt missing %q: %s", expected, systemMessage)
		}
	}
	if len(runEvents) == 0 || runEvents[0].EventType != "prompt_resolved" {
		t.Fatalf("run events=%+v", runEvents)
	}
	promptEvent := runEvents[0]
	if promptEvent.Payload["template_id"] != template.ID || promptEvent.Payload["key"] != template.Key || promptEvent.Payload["version"] != template.Version {
		t.Fatalf("prompt event=%+v", promptEvent)
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
	var events []RunEvent
	result, err := fixture.runner.RunObserved(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite", func(event RunEvent) { events = append(events, event) })
	if err != nil || result.Type != "failed" || !strings.Contains(result.Text, "校验失败") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for _, event := range events {
		if event.EventType == "tool_call" || event.EventType == "tool_result" {
			t.Fatalf("invalid plan executed tool event=%+v", event)
		}
	}
	if _, err := fixture.runner.Run(context.Background(), 21, "unknown", fixture.drama.ID, fixture.episode.ID, "x"); err == nil {
		t.Fatal("unsupported agent accepted")
	}
	if _, err := fixture.runner.Run(context.Background(), 21, "script_rewriter", fixture.drama.ID+999, fixture.episode.ID, "x"); err == nil {
		t.Fatal("cross-drama episode accepted")
	}
}

func TestRunnerRejectsExcessiveActionsAndOversizedArguments(t *testing.T) {
	t.Run("too many actions", func(t *testing.T) {
		actions := make([]map[string]any, 33)
		for index := range actions {
			actions[index] = map[string]any{"tool": "read_episode_script", "args": map[string]any{}}
		}
		encoded, _ := json.Marshal(map[string]any{"actions": actions, "summary": "too many"})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeChatResponse(w, string(encoded)) }))
		defer server.Close()
		fixture := newRunnerFixture(t, server.URL, nil)
		var events []RunEvent
		result, err := fixture.runner.RunObserved(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite", func(event RunEvent) { events = append(events, event) })
		if err != nil || result.Type != "failed" || !strings.Contains(result.Text, "动作数量") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		for _, event := range events {
			if event.EventType == "tool_call" {
				t.Fatalf("excessive plan executed tools: %+v", events)
			}
		}
	})

	t.Run("oversized arguments", func(t *testing.T) {
		largeScript := strings.Repeat("x", 70*1024)
		encoded, _ := json.Marshal(map[string]any{"actions": []map[string]any{{"tool": "save_script", "args": map[string]any{"script": largeScript}}}, "summary": "large"})
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeChatResponse(w, string(encoded)) }))
		defer server.Close()
		fixture := newRunnerFixture(t, server.URL, nil)
		result, err := fixture.runner.Run(context.Background(), 21, "script_rewriter", fixture.drama.ID, fixture.episode.ID, "rewrite")
		if err != nil || result.Type != "failed" || !strings.Contains(result.Text, "参数过大") {
			t.Fatalf("result=%+v err=%v", result, err)
		}
		var episode models.Episode
		if err := db.DB.First(&episode, fixture.episode.ID).Error; err != nil {
			t.Fatal(err)
		}
		if episode.ScriptContent == largeScript {
			t.Fatal("oversized script was persisted")
		}
	})
}

func writeChatResponse(writer http.ResponseWriter, content string) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{
		"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": content}}},
	})
}
