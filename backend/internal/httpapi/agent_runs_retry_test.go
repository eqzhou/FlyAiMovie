package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/db"
	"github.com/eqzhou/flyaimovie/internal/models"
	"github.com/eqzhou/flyaimovie/internal/response"
	"github.com/eqzhou/flyaimovie/internal/services/agents"
)

func TestRetryAgentRunIsAtomicUnderConcurrency(t *testing.T) {
	_, router := testServerRouter(t)
	releaseProvider := make(chan struct{})
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-releaseProvider
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"actions":[],"summary":"done"}`}}}})
	}))
	defer provider.Close()
	now := response.Now()
	textConfig := models.AIServiceConfig{ServiceType: "text", Provider: "openai_local", Name: "slow local", BaseURL: provider.URL, Model: "slow", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&textConfig).Error; err != nil {
		t.Fatal(err)
	}
	drama := models.Drama{Title: "Concurrent Retry", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "Episode", Content: "retry", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	source := models.AgentRun{AgentType: "script_rewriter", DramaID: drama.ID, EpisodeID: episode.ID, Status: "failed", Input: "retry", StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&source).Error; err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			response := performRequest(router, http.MethodPost, "/api/v1/agent-runs/"+idText(source.ID)+"/retry", `{}`, nil)
			statuses <- response.Code
		}()
	}
	close(start)
	wait.Wait()
	close(releaseProvider)
	close(statuses)
	accepted, conflicts := 0, 0
	for status := range statuses {
		switch status {
		case http.StatusAccepted:
			accepted++
		case http.StatusConflict:
			conflicts++
		default:
			t.Fatalf("unexpected retry status=%d", status)
		}
	}
	if accepted != 1 || conflicts != 1 {
		t.Fatalf("accepted=%d conflicts=%d", accepted, conflicts)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		var active int64
		if err := db.DB.Model(&models.AgentRun{}).Where("id <> ? AND status = ?", source.ID, "running").Count(&active).Error; err != nil {
			t.Fatal(err)
		}
		if active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("background agent retry did not finish")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRetryAgentRunCreatesLinkedRunAndCompletes(t *testing.T) {
	_, router := testServerRouter(t)
	now := response.Now()
	textConfig := models.AIServiceConfig{ServiceType: "text", Provider: "mock", Name: "mock text", BaseURL: "http://localhost", Model: "mock", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&textConfig).Error; err != nil {
		t.Fatal(err)
	}
	drama := models.Drama{Title: "Retry Agent", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "Episode", Content: "retry source", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	source := models.AgentRun{AgentType: "script_rewriter", DramaID: drama.ID, EpisodeID: episode.ID, Status: "failed", Input: "重新整理剧本", LastError: "temporary failure", StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&source).Error; err != nil {
		t.Fatal(err)
	}

	retried := performRequest(router, http.MethodPost, "/api/v1/agent-runs/"+idText(source.ID)+"/retry", `{}`, nil)
	if retried.Code != http.StatusAccepted {
		t.Fatalf("retry status=%d body=%s", retried.Code, retried.Body.String())
	}
	payload := decodeResponse(t, retried)
	data, ok := payload["data"].(map[string]any)
	if !ok || data["id"] == nil {
		t.Fatalf("retry response=%#v", payload)
	}
	retryID := uint(data["id"].(float64))
	var retry models.AgentRun
	deadline := time.Now().Add(3 * time.Second)
	for {
		if err := db.DB.First(&retry, retryID).Error; err != nil {
			t.Fatal(err)
		}
		if retry.Status != "running" || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if retry.RetryOfID == nil || *retry.RetryOfID != source.ID {
		t.Fatalf("retry link=%v want=%d", retry.RetryOfID, source.ID)
	}
	if retry.Status != "completed" {
		t.Fatalf("retry status=%s error=%s", retry.Status, retry.LastError)
	}
	var events []models.AgentRunEvent
	if err := db.DB.Where("agent_run_id = ?", retry.ID).Order("sequence").Find(&events).Error; err != nil {
		t.Fatal(err)
	}
	wantEvents := []string{"started", "prompt_resolved", "tool_call", "tool_result", "completed"}
	if len(events) != len(wantEvents) {
		t.Fatalf("retry events=%+v", events)
	}
	for index, want := range wantEvents {
		if events[index].EventType != want || events[index].Sequence != index+1 {
			t.Fatalf("event[%d]=%+v want=%s sequence=%d", index, events[index], want, index+1)
		}
	}
	var unchanged models.AgentRun
	if err := db.DB.First(&unchanged, source.ID).Error; err != nil || unchanged.Status != "failed" {
		t.Fatalf("source status=%s err=%v", unchanged.Status, err)
	}
}

func TestFinishAgentRunTreatsFailedResultAsFailure(t *testing.T) {
	server, _ := testServerRouter(t)
	now := response.Now()
	run := models.AgentRun{AgentType: "extractor", DramaID: 1, EpisodeID: 1, Status: "running", Input: "extract", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	result := &agents.ChatResult{Type: "failed", Text: "部分工具执行失败", ToolCalls: []map[string]any{{"toolName": "save"}}, ToolResults: []map[string]any{{"result": "failed"}}}
	if err := server.finishAgentRun(&run, result, nil); err != nil {
		t.Fatal(err)
	}
	var stored models.AgentRun
	if err := db.DB.First(&stored, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Status != "failed" || stored.LastError != result.Text {
		t.Fatalf("stored status=%s error=%q", stored.Status, stored.LastError)
	}
}

func TestAgentRunObserverReportsPersistenceFailure(t *testing.T) {
	server, _ := testServerRouter(t)
	run := models.AgentRun{ID: 99, AgentType: "extractor"}
	observer, eventError := server.agentRunObserver(&run)
	observer(agents.RunEvent{EventType: "tool_call", Payload: map[string]any{"invalid": make(chan int)}})
	if err := eventError(); err == nil {
		t.Fatal("observer persistence error was discarded")
	}
}

func TestRetryAgentRunRejectsInvalidSource(t *testing.T) {
	_, router := testServerRouter(t)
	now := response.Now()
	completed := models.AgentRun{AgentType: "extractor", DramaID: 1, EpisodeID: 1, Status: "completed", Input: "done", StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&completed).Error; err != nil {
		t.Fatal(err)
	}

	assertRequestStatus(t, router, http.MethodPost, "/api/v1/agent-runs/"+idText(completed.ID)+"/retry", `{}`, http.StatusConflict)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/agent-runs/999999/retry", `{}`, http.StatusNotFound)
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/agent-runs/not-an-id/retry", `{}`, http.StatusBadRequest)

	missingEpisode := models.AgentRun{AgentType: "extractor", DramaID: 99, EpisodeID: 99, Status: "failed", Input: "missing", StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&missingEpisode).Error; err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/agent-runs/"+idText(missingEpisode.ID)+"/retry", `{}`, http.StatusNotFound)

	otherOrganization := models.AgentRun{OrganizationID: 77, AgentType: "extractor", DramaID: 1, EpisodeID: 1, Status: "failed", Input: "other", StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&otherOrganization).Error; err != nil {
		t.Fatal(err)
	}
	assertRequestStatus(t, router, http.MethodPost, "/api/v1/agent-runs/"+idText(otherOrganization.ID)+"/retry", `{}`, http.StatusNotFound)
}
