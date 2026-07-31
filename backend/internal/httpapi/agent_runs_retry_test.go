package httpapi

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if strings.Contains(events[1].PayloadJSON, `"skill_snapshot_mode":"source_run"`) {
		t.Fatalf("legacy source unexpectedly used a snapshot override: %s", events[1].PayloadJSON)
	}
	var unchanged models.AgentRun
	if err := db.DB.First(&unchanged, source.ID).Error; err != nil || unchanged.Status != "failed" {
		t.Fatalf("source status=%s err=%v", unchanged.Status, err)
	}
}

func TestRetryAgentRunReusesExactSourceSkillSnapshotAndBackfillsHash(t *testing.T) {
	_, router := testServerRouter(t)
	systemMessages := make(chan string, 4)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
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
			select {
			case systemMessages <- body.Messages[0].Content:
			default:
			}
		}
		writeAgentRunChatResponse(w, `{"actions":[{"tool":"save_script","args":{"script":"## S01 | 内景 · 工作室 | 日"}}],"summary":"done"}`)
	}))
	defer provider.Close()

	now := response.Now()
	if err := db.DB.Create(&models.AIServiceConfig{ServiceType: "text", Provider: "openai_local", Name: "snapshot provider", BaseURL: provider.URL, Model: "test", IsActive: true, IsDefault: true, CreatedAt: now, UpdatedAt: now}).Error; err != nil {
		t.Fatal(err)
	}
	drama := models.Drama{Title: "Snapshot Retry", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "Episode", Content: "retry", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	skillID, versionID := uint(7), uint(9)
	snapshot := "# Exact source Skill\n\nNever resolve a newer version."
	source := models.AgentRun{
		AgentType: "script_rewriter", DramaID: drama.ID, EpisodeID: episode.ID, Status: "failed", Input: "retry",
		SkillID: &skillID, SkillVersionID: &versionID, SkillVersion: 3, SkillSource: "database", SkillSnapshot: snapshot,
		StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.DB.Create(&source).Error; err != nil {
		t.Fatal(err)
	}

	retried := performRequest(router, http.MethodPost, "/api/v1/agent-runs/"+idText(source.ID)+"/retry", `{}`, nil)
	if retried.Code != http.StatusAccepted {
		t.Fatalf("retry status=%d body=%s", retried.Code, retried.Body.String())
	}
	if strings.Contains(retried.Body.String(), snapshot) || strings.Contains(retried.Body.String(), "skill_snapshot") {
		t.Fatalf("retry response exposed the large snapshot: %s", retried.Body.String())
	}
	retryID := uint(decodeResponse(t, retried)["data"].(map[string]any)["id"].(float64))
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
	digest := sha256.Sum256([]byte(snapshot))
	wantHash := hex.EncodeToString(digest[:])
	if retry.Status != "completed" || retry.SkillSnapshot != snapshot || retry.SkillContentSHA256 != wantHash || retry.SkillID == nil || *retry.SkillID != skillID || retry.SkillVersionID == nil || *retry.SkillVersionID != versionID || retry.SkillVersion != 3 || retry.SkillSource != "database" {
		t.Fatalf("retry did not preserve exact skill metadata: %+v", retry)
	}
	select {
	case system := <-systemMessages:
		if !strings.Contains(system, snapshot) {
			t.Fatalf("model system omitted source snapshot: %q", system)
		}
	case <-time.After(time.Second):
		t.Fatal("model did not receive a system prompt")
	}
	var event models.AgentRunEvent
	if err := db.DB.Where("agent_run_id = ? AND event_type = ?", retry.ID, "prompt_resolved").First(&event).Error; err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(event.PayloadJSON, `"skill_snapshot_mode":"source_run"`) || !strings.Contains(event.PayloadJSON, `"skill_snapshot_source_run_id":`+idText(source.ID)) {
		t.Fatalf("prompt event lost retry provenance: %s", event.PayloadJSON)
	}
}

func TestRetryAgentRunRejectsCorruptSourceSkillSnapshot(t *testing.T) {
	_, router := testServerRouter(t)
	now := response.Now()
	drama := models.Drama{Title: "Corrupt Snapshot", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&drama).Error; err != nil {
		t.Fatal(err)
	}
	episode := models.Episode{DramaID: drama.ID, EpisodeNumber: 1, Title: "Episode", Status: "draft", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&episode).Error; err != nil {
		t.Fatal(err)
	}
	source := models.AgentRun{AgentType: "extractor", DramaID: drama.ID, EpisodeID: episode.ID, Status: "failed", Input: "retry", SkillSnapshot: "tampered", SkillContentSHA256: strings.Repeat("0", 64), StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&source).Error; err != nil {
		t.Fatal(err)
	}

	response := performRequest(router, http.MethodPost, "/api/v1/agent-runs/"+idText(source.ID)+"/retry", `{}`, nil)
	if response.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var retries int64
	if err := db.DB.Model(&models.AgentRun{}).Where("retry_of_id = ?", source.ID).Count(&retries).Error; err != nil {
		t.Fatal(err)
	}
	if retries != 0 {
		t.Fatalf("corrupt source created %d retries", retries)
	}
}

func writeAgentRunChatResponse(w http.ResponseWriter, content string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": content}}}})
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

func TestPromptResolvedEventPersistsExactSkillSnapshot(t *testing.T) {
	_, _ = testServerRouter(t)
	now := response.Now()
	organization := models.Organization{Name: "Snapshot Org", Slug: "snapshot-org", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&organization).Error; err != nil {
		t.Fatal(err)
	}
	run := models.AgentRun{OrganizationID: organization.ID, AgentType: "extractor", DramaID: 1, EpisodeID: 1, Status: "running", Input: "extract", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	snapshot := "# Organization extractor\n\nReference rules"
	if err := appendAgentRunEvent(run.OrganizationID, run.ID, "prompt_resolved", "", map[string]any{
		"skill_source": "database", "skill_id": uint(7), "skill_version_id": uint(9),
		"skill_version": 3, "skill_hash": "abc123", "skill_snapshot": snapshot,
	}); err != nil {
		t.Fatal(err)
	}
	var stored models.AgentRun
	if err := db.DB.First(&stored, run.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.SkillSnapshot != snapshot || stored.SkillContentSHA256 != "abc123" || stored.SkillVersion != 3 {
		t.Fatalf("stored skill snapshot mismatch: %+v", stored)
	}
}

func TestAgentRunResponsesOmitSkillSnapshotButDetailKeepsOutput(t *testing.T) {
	_, router := testServerRouter(t)
	now := response.Now()
	run := models.AgentRun{
		AgentType: "extractor", DramaID: 1, EpisodeID: 1, Status: "completed", Input: "extract",
		OutputJSON: `{"summary":"large output"}`, SkillSnapshot: "# exact skill snapshot",
		StartedAt: now, CompletedAt: &now, CreatedAt: now, UpdatedAt: now,
	}
	if err := db.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.DB.Create(&models.AgentRunEvent{
		AgentRunID: run.ID, Sequence: 1, EventType: "prompt_resolved",
		PayloadJSON: `{"skill_source":"database","skill_hash":"abc123","skill_snapshot":"# exact skill snapshot"}`,
		CreatedAt:   now,
	}).Error; err != nil {
		t.Fatal(err)
	}

	list := performRequest(router, http.MethodGet, "/api/v1/agent-runs", "", nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	listRows := decodeResponse(t, list)["data"].([]any)
	if len(listRows) != 1 {
		t.Fatalf("list rows=%d body=%s", len(listRows), list.Body.String())
	}
	listed := listRows[0].(map[string]any)
	if _, exists := listed["skill_snapshot"]; exists {
		t.Fatalf("list exposed skill_snapshot: %s", list.Body.String())
	}
	if listed["output_json"] != run.OutputJSON {
		t.Fatalf("list changed the existing output_json contract: %s", list.Body.String())
	}
	if listed["input"] != run.Input || listed["status"] != run.Status {
		t.Fatalf("list omitted summary fields: %#v", listed)
	}

	detail := performRequest(router, http.MethodGet, "/api/v1/agent-runs/"+idText(run.ID), "", nil)
	if detail.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
	detailRun := decodeResponse(t, detail)["data"].(map[string]any)["run"].(map[string]any)
	if _, exists := detailRun["skill_snapshot"]; exists {
		t.Fatalf("detail exposed skill snapshot: %#v", detailRun)
	}
	if detailRun["output_json"] != run.OutputJSON {
		t.Fatalf("detail lost output: %#v", detailRun)
	}
	detailEvents := decodeResponse(t, detail)["data"].(map[string]any)["events"].([]any)
	payload := detailEvents[0].(map[string]any)["payload_json"].(string)
	if strings.Contains(payload, "skill_snapshot") || !strings.Contains(payload, "skill_hash") {
		t.Fatalf("detail event payload was not safely redacted: %s", payload)
	}
}

func TestAppendAgentRunEventRejectsMissingParentOrOrganization(t *testing.T) {
	server, _ := testServerRouter(t)
	now := response.Now()
	organization := models.Organization{Name: "Event Org", Slug: "event-org", Status: "active", CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&organization).Error; err != nil {
		t.Fatal(err)
	}
	run := models.AgentRun{OrganizationID: organization.ID, AgentType: "extractor", Status: "running", StartedAt: now, CreatedAt: now, UpdatedAt: now}
	if err := db.DB.Create(&run).Error; err != nil {
		t.Fatal(err)
	}
	if err := purgeOrganization(db.DB, server.Store, organization.ID, nil, nil); err != nil {
		t.Fatal(err)
	}

	if err := appendAgentRunEvent(organization.ID, run.ID, "tool_call", "save", map[string]any{"ok": true}); err == nil {
		t.Fatal("expected append to reject a deleted organization")
	}
	if err := appendAgentRunEvent(organization.ID, run.ID+999, "tool_call", "save", map[string]any{"ok": true}); err == nil {
		t.Fatal("expected append to reject a missing parent run")
	}
	var events int64
	if err := db.DB.Model(&models.AgentRunEvent{}).Where("organization_id = ?", organization.ID).Count(&events).Error; err != nil {
		t.Fatal(err)
	}
	if events != 0 {
		t.Fatalf("orphan events=%d", events)
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
