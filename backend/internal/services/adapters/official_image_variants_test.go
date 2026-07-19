package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiImageFileResultAndMissingResult(t *testing.T) {
	responses := []string{
		`{"candidates":[{"content":{"parts":[{"fileData":{"fileUri":"https://cdn.example/gemini.png"}}]}}]}`,
		`{"candidates":[]}`,
	}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(responses[index]))
		index++
	}))
	defer server.Close()
	adapter := &GeminiImageAdapter{}
	result, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "file"})
	if err != nil || result.ImageURL == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "empty"}); err == nil {
		t.Fatal("empty Gemini result accepted")
	}
}

func TestMiniMaxImageAsyncDirectAndProviderError(t *testing.T) {
	responses := []string{
		`{"task_id":"minimax-task","base_resp":{"status_code":0}}`,
		`{"data":{"image_url":"https://cdn.example/minimax.png"},"base_resp":{"status_code":0}}`,
		`{"base_resp":{"status_code":1001,"status_msg":"rejected"}}`,
	}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(responses[index]))
		index++
	}))
	defer server.Close()
	adapter := &MiniMaxImageAdapter{}
	async, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "async"})
	if err != nil || async.TaskID != "minimax-task" {
		t.Fatalf("async=%+v err=%v", async, err)
	}
	direct, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "direct"})
	if err != nil || direct.ImageURL == "" {
		t.Fatalf("direct=%+v err=%v", direct, err)
	}
	if _, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "error"}); err == nil {
		t.Fatal("MiniMax provider error accepted")
	}
}

func TestVolcengineImageSyncAndMissingResult(t *testing.T) {
	responses := []string{
		`{"data":[{"url":"https://cdn.example/volc.png"}]}`,
		`{"data":[]}`,
	}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(responses[index]))
		index++
	}))
	defer server.Close()
	adapter := &VolcengineImageAdapter{}
	result, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "sync", Size: "1024x1024", ReferenceImages: []string{"reference"}})
	if err != nil || result.ImageURL == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	if _, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "missing"}); err == nil {
		t.Fatal("missing Volcengine result accepted")
	}
}

func TestDashScopeDirectResultAndFailedPoll(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"output":{"results":[{"url":"https://cdn.example/dashscope.png"}]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"output":{"task_status":"FAILED","message":"rejected"}}`))
	}))
	defer server.Close()
	adapter := &DashScopeImageAdapter{}
	result, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "direct", Size: "1024*1024"})
	if err != nil || result.ImageURL == "" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	poll, err := adapter.Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, "task")
	if err != nil || poll.Status != "failed" || poll.Error != "dashscope reported image generation failure" {
		t.Fatalf("poll=%+v err=%v", poll, err)
	}
}

func TestOfficialImageErrorsDoNotExposeProviderBodies(t *testing.T) {
	secret := "provider echoed secret-request-content"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(secret))
	}))
	defer server.Close()

	_, err := (&GeminiImageAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "prompt"})
	if err == nil || strings.Contains(err.Error(), secret) || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("provider error was not sanitized: %v", err)
	}
}
