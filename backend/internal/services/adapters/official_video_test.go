package adapters

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiniMaxVideoSubmitPollAndFileRetrieve(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/v1/video_generation":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["model"] != "video-01" || body["first_frame_image"] != "https://cdn/first.png" {
				t.Errorf("unexpected submit body: %#v", body)
			}
			writeJSON(w, `{"task_id":"task-mm","base_resp":{"status_code":0}}`)
		case "/v1/query/video_generation":
			if r.URL.Query().Get("task_id") != "task-mm" {
				t.Errorf("task_id=%q", r.URL.Query().Get("task_id"))
			}
			writeJSON(w, `{"status":"Success","file_id":88,"base_resp":{"status_code":0}}`)
		case "/v1/files/retrieve":
			if r.URL.Query().Get("file_id") != "88" {
				t.Errorf("file_id=%q", r.URL.Query().Get("file_id"))
			}
			writeJSON(w, `{"file":{"download_url":"https://cdn/video.mp4"},"base_resp":{"status_code":0}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := &MiniMaxVideoAdapter{}
	cfg := AIConfig{BaseURL: server.URL, APIKey: "secret", Model: "video-01"}
	submitted, err := adapter.Generate(context.Background(), cfg, VideoGenInput{Prompt: "scene", FirstFrameURL: "https://cdn/first.png"})
	if err != nil || !submitted.IsAsync || submitted.TaskID != "task-mm" {
		t.Fatalf("submitted=%+v err=%v", submitted, err)
	}
	polled, err := adapter.Poll(context.Background(), cfg, submitted.TaskID)
	if err != nil || polled.Status != "completed" || polled.VideoURL != "https://cdn/video.mp4" {
		t.Fatalf("polled=%+v err=%v", polled, err)
	}
}

func TestVolcengineVideoUsesContentContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v3/contents/generations/tasks":
			var body struct {
				Model   string           `json:"model"`
				Content []map[string]any `json:"content"`
				Ratio   string           `json:"ratio"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Model != "seedance-model" || body.Ratio != "16:9" || len(body.Content) != 3 {
				t.Errorf("unexpected submit body: %#v", body)
			}
			if body.Content[1]["role"] != "first_frame" || body.Content[2]["role"] != "last_frame" {
				t.Errorf("missing frame roles: %#v", body.Content)
			}
			writeJSON(w, `{"id":"task-ark"}`)
		case "GET /api/v3/contents/generations/tasks/task-ark":
			writeJSON(w, `{"id":"task-ark","status":"succeeded","content":{"video_url":"https://cdn/ark.mp4"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := &VolcengineVideoAdapter{}
	cfg := AIConfig{BaseURL: server.URL, APIKey: "ark-key", Model: "seedance-model"}
	submitted, err := adapter.Generate(context.Background(), cfg, VideoGenInput{Prompt: "scene", FirstFrameURL: "https://cdn/first.png", LastFrameURL: "https://cdn/last.png", AspectRatio: "16:9"})
	if err != nil || submitted.TaskID != "task-ark" {
		t.Fatalf("submitted=%+v err=%v", submitted, err)
	}
	polled, err := adapter.Poll(context.Background(), cfg, submitted.TaskID)
	if err != nil || polled.Status != "completed" || polled.VideoURL != "https://cdn/ark.mp4" {
		t.Fatalf("polled=%+v err=%v", polled, err)
	}
}

func TestAliyunVideoUsesAsyncTaskContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v1/services/aigc/video-generation/video-synthesis":
			if r.Header.Get("X-DashScope-Async") != "enable" || r.Header.Get("Authorization") != "Bearer dash-key" {
				t.Errorf("unexpected headers: %#v", r.Header)
			}
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			input, _ := body["input"].(map[string]any)
			if input["prompt"] != "scene" || input["img_url"] != "https://cdn/first.png" {
				t.Errorf("unexpected input: %#v", input)
			}
			writeJSON(w, `{"output":{"task_id":"task-ali","task_status":"PENDING"},"request_id":"req"}`)
		case "GET /api/v1/tasks/task-ali":
			writeJSON(w, `{"output":{"task_id":"task-ali","task_status":"SUCCEEDED","video_url":"https://cdn/ali.mp4"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := &AliyunVideoAdapter{}
	cfg := AIConfig{BaseURL: server.URL, APIKey: "dash-key", Model: "wan-video"}
	submitted, err := adapter.Generate(context.Background(), cfg, VideoGenInput{Prompt: "scene", ImageURL: "https://cdn/first.png"})
	if err != nil || submitted.TaskID != "task-ali" {
		t.Fatalf("submitted=%+v err=%v", submitted, err)
	}
	polled, err := adapter.Poll(context.Background(), cfg, submitted.TaskID)
	if err != nil || polled.Status != "completed" || polled.VideoURL != "https://cdn/ali.mp4" {
		t.Fatalf("polled=%+v err=%v", polled, err)
	}
}

func TestViduVideoUsesTokenAndCreationsContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token test-placeholder-key" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /ent/v2/img2video":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			images, _ := body["images"].([]any)
			if body["model"] != "vidu2.0" || len(images) != 1 || images[0] != "https://cdn/first.png" {
				t.Errorf("unexpected Vidu body: %#v", body)
			}
			writeJSON(w, `{"task_id":"task-vidu"}`)
		case "GET /ent/v2/tasks/task-vidu/creations":
			writeJSON(w, `{"state":"success","creations":[{"url":"https://cdn/vidu.mp4"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	adapter := &ViduVideoAdapter{}
	var cfg AIConfig
	if err := json.Unmarshal([]byte(`{"base_url":"`+server.URL+`","api_key":"test-placeholder-key","model":"vidu2.0"}`), &cfg); err != nil {
		t.Fatal(err)
	}
	submitted, err := adapter.Generate(context.Background(), cfg, VideoGenInput{Prompt: "scene", FirstFrameURL: "https://cdn/first.png"})
	if err != nil || submitted.TaskID != "task-vidu" {
		t.Fatalf("submitted=%+v err=%v", submitted, err)
	}
	polled, err := adapter.Poll(context.Background(), cfg, submitted.TaskID)
	if err != nil || polled.Status != "completed" || polled.VideoURL != "https://cdn/vidu.mp4" {
		t.Fatalf("polled=%+v err=%v", polled, err)
	}
}

func TestViduRejectsMissingImageAndDoesNotFallbackOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()
	adapter := &ViduVideoAdapter{}
	cfg := AIConfig{BaseURL: server.URL, APIKey: "vidu-key"}
	if _, err := adapter.Generate(context.Background(), cfg, VideoGenInput{Prompt: "scene"}); err == nil || !strings.Contains(err.Error(), "requires at least one image") {
		t.Fatalf("missing image err=%v", err)
	}
	if _, err := adapter.Generate(context.Background(), cfg, VideoGenInput{Prompt: "scene", ImageURL: "https://cdn/image.png"}); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("HTTP error=%v", err)
	}
}

func TestProviderHTTPErrorIsBounded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("x", 32)))
	}))
	defer server.Close()
	_, err := (&AliyunVideoAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "x"}, VideoGenInput{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 400") {
		t.Fatalf("err=%v", err)
	}
}

func TestProviderRegistryRejectsUnsupportedPairs(t *testing.T) {
	if IsSupportedProvider("video", "gemini") || IsSupportedProvider("audio", "openai") {
		t.Fatal("unsupported service/provider pair was accepted")
	}
	if !IsSupportedProvider("image", "gemini") || !IsSupportedProvider("video", "vidu") || !IsSupportedProvider("audio", "minimax") {
		t.Fatal("supported service/provider pair was rejected")
	}
	if _, err := GetVideoAdapter("unknown").Generate(context.Background(), AIConfig{}, VideoGenInput{}); err == nil || !strings.Contains(err.Error(), "unsupported video provider") {
		t.Fatalf("unknown video provider err=%v", err)
	}
}

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(body))
}
