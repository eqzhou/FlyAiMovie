package adapters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestRegistryReturnsKnownAndRejectsUnknownProviders(t *testing.T) {
	for _, provider := range []string{"mock", "openai", "chatfire", "gemini", "minimax", "volcengine", "ali"} {
		adapter := GetImageAdapter(strings.ToUpper(provider))
		if adapter.Name() == "" || !IsSupportedProvider("image", provider) {
			t.Fatalf("image provider=%q adapter=%T", provider, adapter)
		}
	}
	for _, provider := range []string{"mock", "openai", "minimax", "volcengine", "vidu", "ali"} {
		adapter := GetVideoAdapter(strings.ToUpper(provider))
		if adapter.Name() == "" || !IsSupportedProvider("video", provider) {
			t.Fatalf("video provider=%q adapter=%T", provider, adapter)
		}
	}
	for _, provider := range []string{"mock", "minimax"} {
		adapter := GetTTSAdapter(strings.ToUpper(provider))
		if adapter.Name() == "" || !IsSupportedProvider("audio", provider) {
			t.Fatalf("audio provider=%q adapter=%T", provider, adapter)
		}
	}
	if !IsSupportedProvider("text", "openai_local") || IsSupportedProvider("unknown", "mock") {
		t.Fatal("text or unknown provider support is incorrect")
	}
	image := GetImageAdapter("missing")
	if image.Name() != "missing" {
		t.Fatalf("image name=%q", image.Name())
	}
	if _, err := image.Generate(context.Background(), AIConfig{}, ImageGenInput{}); err == nil {
		t.Fatal("unsupported image generate succeeded")
	}
	if _, err := image.Poll(context.Background(), AIConfig{}, "task"); err == nil {
		t.Fatal("unsupported image poll succeeded")
	}
	video := GetVideoAdapter("missing")
	if video.Name() != "missing" {
		t.Fatalf("video name=%q", video.Name())
	}
	if _, err := video.Generate(context.Background(), AIConfig{}, VideoGenInput{}); err == nil {
		t.Fatal("unsupported video generate succeeded")
	}
	if _, err := video.Poll(context.Background(), AIConfig{}, "task"); err == nil {
		t.Fatal("unsupported video poll succeeded")
	}
	audio := GetTTSAdapter("missing")
	if audio.Name() != "missing" {
		t.Fatalf("audio name=%q", audio.Name())
	}
	if _, err := audio.Generate(context.Background(), AIConfig{}, TTSInput{}); err == nil {
		t.Fatal("unsupported audio generate succeeded")
	}
}

func TestAdapterHelperVariants(t *testing.T) {
	if poll, _ := (&GeminiImageAdapter{}).Poll(context.Background(), AIConfig{}, "task"); poll.Status != "failed" {
		t.Fatalf("gemini poll=%+v", poll)
	}
	if poll, _ := (&MiniMaxImageAdapter{}).Poll(context.Background(), AIConfig{}, "task"); poll.Status != "failed" {
		t.Fatalf("minimax poll=%+v", poll)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte("image"))
	for _, tc := range []struct {
		payload map[string]any
		wantURL bool
		wantB64 bool
		wantErr bool
	}{
		{map[string]any{"output": map[string]any{"results": []any{map[string]any{"url": "https://cdn/image.png"}}}}, true, false, false},
		{map[string]any{"image_url": "https://cdn/direct.png"}, true, false, false},
		{map[string]any{"output": map[string]any{"base64": encoded}}, false, true, false},
		{map[string]any{"output": map[string]any{"base64": "invalid"}}, false, false, true},
	} {
		result, err := dashscopeResult(tc.payload)
		if (err != nil) != tc.wantErr {
			t.Fatalf("payload=%#v result=%+v err=%v", tc.payload, result, err)
		}
		if err == nil && ((result.ImageURL != "") != tc.wantURL || (result.Base64 != "") != tc.wantB64) {
			t.Fatalf("payload=%#v result=%+v", tc.payload, result)
		}
	}
	for input, want := range map[string]string{
		"Success": "completed", "done": "completed", "FAILED": "failed", "cancelled": "failed", "queued": "pending", "running": "processing", "mystery": "processing", "": "processing",
	} {
		if got := normalizeVideoStatus(input); got != want {
			t.Errorf("normalizeVideoStatus(%q)=%q want %q", input, got, want)
		}
	}
	for mime, want := range map[string]string{"image/png": ".png", "image/jpeg": ".jpg", "image/webp": ".webp", "text/plain": ".png"} {
		if got := imageExtension(mime); got != want {
			t.Errorf("imageExtension(%q)=%q", mime, got)
		}
	}
	for _, tc := range []struct {
		size, aspect, want string
	}{
		{"1920x1080", "", "1920x1080"},
		{"", "9:16", "720x1280"},
		{"", "", "1280x720"},
	} {
		if got := openAIVideoSize(tc.size, tc.aspect); got != tc.want {
			t.Errorf("openAIVideoSize(%q,%q)=%q", tc.size, tc.aspect, got)
		}
	}
}

func TestMockAdaptersProduceLocalMedia(t *testing.T) {
	imageAdapter := &MockImageAdapter{}
	image, err := imageAdapter.Generate(context.Background(), AIConfig{}, ImageGenInput{})
	if err != nil || !IsFileURL(image.ImageURL) {
		t.Fatalf("image=%+v err=%v", image, err)
	}
	defer os.Remove(FileURLPath(image.ImageURL))
	if _, err := os.Stat(FileURLPath(image.ImageURL)); err != nil {
		t.Fatal(err)
	}
	if polled, _ := imageAdapter.Poll(context.Background(), AIConfig{}, "task"); polled.Status != "failed" {
		t.Fatalf("image poll=%+v", polled)
	}
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg is required for Mock video and TTS")
	}
	videoAdapter := &MockVideoAdapter{}
	video, err := videoAdapter.Generate(context.Background(), AIConfig{}, VideoGenInput{Duration: 1})
	if err != nil || !IsFileURL(video.VideoURL) {
		t.Fatalf("video=%+v err=%v", video, err)
	}
	defer os.Remove(FileURLPath(video.VideoURL))
	if polled, _ := videoAdapter.Poll(context.Background(), AIConfig{}, "task"); polled.Status != "failed" {
		t.Fatalf("video poll=%+v", polled)
	}
	audio, err := (&MockTTSAdapter{}).Generate(context.Background(), AIConfig{}, TTSInput{Text: "test"})
	if err != nil || len(audio.AudioBytes) == 0 {
		t.Fatalf("audio=%+v err=%v", audio, err)
	}
}

func TestGenericVideoAdapterSubmitAndPollContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Errorf("authorization=%q", r.Header.Get("Authorization"))
		}
		switch r.Method + " " + r.URL.Path {
		case "POST /api/v3/contents/generations/tasks":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["first_frame_image"] != "first" || body["last_frame_image"] != "last" || body["duration"] != float64(3) {
				t.Errorf("body=%#v", body)
			}
			_, _ = w.Write([]byte(`{"data":{"task_id":"generic-task"}}`))
		case "GET /api/v3/contents/generations/tasks/generic-task":
			_, _ = w.Write([]byte(`{"data":{"status":"SUCCEEDED","content":{"video_url":"https://cdn.example/video.mp4"}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter := &GenericVideoAdapter{ProviderName: "generic"}
	result, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key", Model: "model"}, VideoGenInput{Prompt: "prompt", FirstFrameURL: "first", LastFrameURL: "last", Duration: 3, AspectRatio: "16:9"})
	if err != nil || result.TaskID != "generic-task" || adapter.Name() != "generic" {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	poll, err := adapter.Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, result.TaskID)
	if err != nil || poll.Status != "completed" || poll.VideoURL == "" {
		t.Fatalf("poll=%+v err=%v", poll, err)
	}
}

func TestOpenAICompatibleImageSyncAndPollContracts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/images/generations":
			_, _ = w.Write([]byte(`{"data":[{"b64_json":"aW1hZ2U="}]}`))
		case "GET /v1/images/tasks/task-1":
			_, _ = w.Write([]byte(`{"status":"succeeded","data":{"image_url":"https://cdn.example/image.png"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	adapter := &OpenAICompatImageAdapter{ProviderName: "chatfire"}
	generated, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "prompt", ReferenceImages: []string{"reference"}})
	if err != nil || generated.Base64 != "aW1hZ2U=" || adapter.Name() != "chatfire" {
		t.Fatalf("generated=%+v err=%v", generated, err)
	}
	polled, err := adapter.Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, "task-1")
	if err != nil || polled.Status != "completed" || polled.ImageURL == "" {
		t.Fatalf("polled=%+v err=%v", polled, err)
	}
}

func TestOpenAIImageAdapterURLBase64AndErrors(t *testing.T) {
	responses := []string{
		`{"data":[{"url":"https://cdn.example/image.png"}]}`,
		`{"data":[{"b64_json":"aW1hZ2U="}]}`,
		`{"data":[]}`,
	}
	index := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(responses[index]))
		index++
	}))
	defer server.Close()
	adapter := &OpenAIImageAdapter{}
	first, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "one"})
	if err != nil || first.ImageURL == "" || adapter.Name() != "openai" {
		t.Fatalf("first=%+v err=%v", first, err)
	}
	second, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "two"})
	if err != nil || second.Base64 == "" {
		t.Fatalf("second=%+v err=%v", second, err)
	}
	if _, err := adapter.Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "three"}); err == nil {
		t.Fatal("empty response accepted")
	}
	if poll, _ := adapter.Poll(context.Background(), AIConfig{}, "task"); poll.Status != "failed" {
		t.Fatalf("poll=%+v", poll)
	}
}
