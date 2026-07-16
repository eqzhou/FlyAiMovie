package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGeminiImageContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/v1beta/models/gemini-test:generateContent") {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("key") != "gem-key" {
			t.Errorf("missing query key")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["contents"]; !ok {
			t.Errorf("missing contents: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"aW1hZ2U="}}]}}]}`))
	}))
	defer srv.Close()
	r, err := (&GeminiImageAdapter{}).Generate(context.Background(), AIConfig{BaseURL: srv.URL, APIKey: "gem-key", Model: "gemini-test"}, ImageGenInput{Prompt: "a cat"})
	if err != nil {
		t.Fatal(err)
	}
	if r.Base64 != "aW1hZ2U=" || r.MimeType != "image/png" || r.IsAsync {
		t.Fatalf("unexpected result %#v", r)
	}
}

func TestMiniMaxImageContract(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/image_generation" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer mm-key" {
			t.Errorf("missing bearer")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["model"] != "image-test" || body["aspect_ratio"] != "16:9" {
			t.Errorf("unexpected body %#v", body)
		}
		references, _ := body["subject_reference"].([]any)
		if len(references) != 1 {
			t.Errorf("subject_reference must be an array: %#v", body["subject_reference"])
		}
		_, _ = w.Write([]byte(`{"data":{"image_urls":["https://cdn.example/image.png"]}}`))
	}))
	defer srv.Close()
	r, err := (&MiniMaxImageAdapter{}).Generate(context.Background(), AIConfig{BaseURL: srv.URL + "/v1", APIKey: "mm-key", Model: "image-test"}, ImageGenInput{Prompt: "a cat", Size: "16x9", ReferenceImages: []string{"https://cdn.example/ref.png"}})
	if err != nil {
		t.Fatal(err)
	}
	if r.ImageURL == "" || r.IsAsync {
		t.Fatalf("unexpected result %#v", r)
	}
}

func TestVolcengineImageContractAndPoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v3/images/generations" {
			if r.Header.Get("Authorization") != "Bearer ark-key" {
				t.Errorf("missing bearer")
			}
			_, _ = w.Write([]byte(`{"id":"task-1"}`))
			return
		}
		if r.URL.Path != "/api/v3/images/generations/task-1" {
			t.Errorf("unexpected poll path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"status":"succeeded","data":[{"url":"https://cdn.example/seedream.png"}]}`))
	}))
	defer srv.Close()
	a := &VolcengineImageAdapter{}
	r, err := a.Generate(context.Background(), AIConfig{BaseURL: srv.URL, APIKey: "ark-key", Model: "seedream-test"}, ImageGenInput{Prompt: "a cat"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsAsync || r.TaskID != "task-1" {
		t.Fatalf("unexpected generate %#v", r)
	}
	p, err := a.Poll(context.Background(), AIConfig{BaseURL: srv.URL, APIKey: "ark-key"}, r.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "completed" || p.ImageURL == "" {
		t.Fatalf("unexpected poll %#v", p)
	}
}

func TestDashScopeImageContractAndPoll(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/services/aigc/text2image/image-synthesis" {
			if r.Header.Get("Authorization") != "Bearer ali-key" || r.Header.Get("X-DashScope-Async") != "enable" {
				t.Errorf("missing dashscope auth headers")
			}
			_, _ = w.Write([]byte(`{"output":{"task_status":"PENDING","task_id":"ds-1"}}`))
			return
		}
		if r.URL.Path != "/api/v1/tasks/ds-1" {
			t.Errorf("unexpected poll path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"output":{"task_status":"SUCCEEDED","results":[{"url":"https://cdn.example/wan.png"}]}}`))
	}))
	defer srv.Close()
	a := &DashScopeImageAdapter{}
	r, err := a.Generate(context.Background(), AIConfig{BaseURL: srv.URL, APIKey: "ali-key", Model: "wanx-test"}, ImageGenInput{Prompt: "a cat"})
	if err != nil {
		t.Fatal(err)
	}
	if !r.IsAsync || r.TaskID != "ds-1" {
		t.Fatalf("unexpected generate %#v", r)
	}
	p, err := a.Poll(context.Background(), AIConfig{BaseURL: srv.URL, APIKey: "ali-key"}, r.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Status != "completed" || p.ImageURL == "" {
		t.Fatalf("unexpected poll %#v", p)
	}
}
