package adapters

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func pngDataURI(t *testing.T, width, height int) string {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			source.Set(x, y, color.RGBA{R: uint8(x % 255), G: uint8(y % 255), B: 90, A: 255})
		}
	}
	var payload bytes.Buffer
	if err := png.Encode(&payload, source); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(payload.Bytes())
}

func jpegDataURI(t *testing.T, width, height int) string {
	t.Helper()
	source := image.NewRGBA(image.Rect(0, 0, width, height))
	var payload bytes.Buffer
	if err := jpeg.Encode(&payload, source, nil); err != nil {
		t.Fatal(err)
	}
	return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(payload.Bytes())
}

// A provider that returns a non-2xx status must surface an error rather than a
// half-parsed result.
func TestProviderJSONRejectsErrorStatusAndInvalidBody(t *testing.T) {
	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer failing.Close()
	if _, err := providerJSON(context.Background(), http.MethodGet, failing.URL, "key", nil, nil); err == nil {
		t.Fatal("HTTP 500 accepted")
	}

	malformed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer malformed.Close()
	if _, err := providerJSON(context.Background(), http.MethodGet, malformed.URL, "key", nil, nil); err == nil {
		t.Fatal("malformed JSON accepted")
	}
}

// The API key must be sent as a bearer token exactly once, even when the stored key
// already carries the prefix, and extra headers must be forwarded.
func TestProviderJSONNormalizesBearerTokenAndHeaders(t *testing.T) {
	var gotAuth, gotCustom, gotContentType string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotCustom = r.Header.Get("X-DashScope-Async")
		gotContentType = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	if _, err := providerJSON(context.Background(), http.MethodPost, server.URL, "Bearer secret", map[string]string{"X-DashScope-Async": "enable"}, map[string]any{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("authorization=%q, want the token normalized to a single Bearer prefix", gotAuth)
	}
	if gotCustom != "enable" || gotContentType != "application/json" {
		t.Fatalf("custom=%q content-type=%q", gotCustom, gotContentType)
	}
}

// MiniMax signals application-level failures inside base_resp while returning HTTP 200.
func TestProviderBaseErrorDetectsApplicationFailure(t *testing.T) {
	if err := providerBaseError("minimax", map[string]any{"base_resp": map[string]any{"status_code": float64(0)}}); err != nil {
		t.Fatalf("err=%v, want success for status_code 0", err)
	}
	if err := providerBaseError("minimax", map[string]any{}); err != nil {
		t.Fatalf("err=%v, want success when base_resp is absent", err)
	}
	err := providerBaseError("minimax", map[string]any{"base_resp": map[string]any{"status_code": float64(1004)}})
	if err == nil || !strings.Contains(err.Error(), "1004") {
		t.Fatalf("err=%v, want the provider status code reported", err)
	}
}

// MiniMax video completion returns only a file_id, which has to be exchanged for a
// download URL through the files endpoint.
func TestMiniMaxVideoPollResolvesFileIDToDownloadURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/query/video_generation"):
			_, _ = w.Write([]byte(`{"status":"Success","file_id":"file-9"}`))
		case strings.HasSuffix(r.URL.Path, "/files/retrieve"):
			if r.URL.Query().Get("file_id") != "file-9" {
				t.Errorf("file_id=%q", r.URL.Query().Get("file_id"))
			}
			_, _ = w.Write([]byte(`{"file":{"download_url":"https://cdn.example/video.mp4"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	result, err := (&MiniMaxVideoAdapter{}).Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.VideoURL != "https://cdn.example/video.mp4" {
		t.Fatalf("result=%+v, want the resolved download URL", result)
	}
}

// A completed MiniMax task with neither a URL nor a file id cannot be finalized.
func TestMiniMaxVideoPollFailsWhenCompletedTaskHasNoFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"Success"}`))
	}))
	defer server.Close()

	result, err := (&MiniMaxVideoAdapter{}).Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || result.Error == "" {
		t.Fatalf("result=%+v, want a failure for a completed task with no file", result)
	}
}

// An application error during polling must be reported as a failed poll, not a
// transport error, so the worker marks the job failed.
func TestMiniMaxVideoPollReportsApplicationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":2013}}`))
	}))
	defer server.Close()

	result, err := (&MiniMaxVideoAdapter{}).Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "failed" || !strings.Contains(result.Error, "2013") {
		t.Fatalf("result=%+v, want the application error surfaced", result)
	}
}

// A submit response without a task id must be rejected instead of returning an empty
// task that can never be polled.
func TestMiniMaxVideoGenerateRequiresTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	if _, err := (&MiniMaxVideoAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, VideoGenInput{Prompt: "p"}); err == nil {
		t.Fatal("submit without a task id was accepted")
	}
}

// MiniMax rejects submissions at the application level while returning HTTP 200.
func TestMiniMaxVideoGenerateSurfacesApplicationError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"base_resp":{"status_code":1004},"task_id":"ignored"}`))
	}))
	defer server.Close()

	_, err := (&MiniMaxVideoAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, VideoGenInput{Prompt: "p"})
	if err == nil || !strings.Contains(err.Error(), "1004") {
		t.Fatalf("err=%v, want the application error surfaced", err)
	}
}

// Volcengine and Aliyun submissions must reject responses that carry no task id.
func TestVideoAdaptersRejectResponsesWithoutTaskID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":{}}`))
	}))
	defer server.Close()
	cfg := AIConfig{BaseURL: server.URL, APIKey: "key", Model: "m"}

	if _, err := (&VolcengineVideoAdapter{}).Generate(context.Background(), cfg, VideoGenInput{Prompt: "p"}); err == nil {
		t.Fatal("volcengine submit without an id was accepted")
	}
	if _, err := (&AliyunVideoAdapter{}).Generate(context.Background(), cfg, VideoGenInput{Prompt: "p"}); err == nil {
		t.Fatal("aliyun submit without a task id was accepted")
	}
}

// A failed provider status must carry an operator-visible error message.
func TestVideoPollFailuresCarryErrorMessages(t *testing.T) {
	volcengine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"failed"}`))
	}))
	defer volcengine.Close()
	aliyun := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_status":"FAILED"}}`))
	}))
	defer aliyun.Close()

	volcResult, err := (&VolcengineVideoAdapter{}).Poll(context.Background(), AIConfig{BaseURL: volcengine.URL, APIKey: "key"}, "t")
	if err != nil {
		t.Fatal(err)
	}
	if volcResult.Status != "failed" || volcResult.Error == "" {
		t.Fatalf("volcengine=%+v, want a failure message", volcResult)
	}
	aliResult, err := (&AliyunVideoAdapter{}).Poll(context.Background(), AIConfig{BaseURL: aliyun.URL, APIKey: "key"}, "t")
	if err != nil {
		t.Fatal(err)
	}
	if aliResult.Status != "failed" || aliResult.Error == "" {
		t.Fatalf("aliyun=%+v, want a failure message", aliResult)
	}
}

// Aliyun returns the finished video inside output.results rather than a top-level URL.
func TestAliyunVideoPollReadsResultsArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"output":{"task_status":"SUCCEEDED","results":[{"url":"https://cdn.example/a.mp4"}]}}`))
	}))
	defer server.Close()

	result, err := (&AliyunVideoAdapter{}).Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, "t")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.VideoURL != "https://cdn.example/a.mp4" {
		t.Fatalf("result=%+v, want the URL taken from output.results", result)
	}
}

// Volcengine nests the finished video inside content.
func TestVolcengineVideoPollReadsNestedContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"succeeded","content":{"video_url":"https://cdn.example/b.mp4"}}`))
	}))
	defer server.Close()

	result, err := (&VolcengineVideoAdapter{}).Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, "t")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.VideoURL != "https://cdn.example/b.mp4" {
		t.Fatalf("result=%+v, want the nested content URL", result)
	}
}

// joinPath must not duplicate an API prefix already present in the configured base URL.
func TestJoinPathAvoidsDuplicatedAPIPrefix(t *testing.T) {
	for _, tc := range []struct{ base, path, want string }{
		{"https://host.example/api/v3", "/api/v3/contents", "https://host.example/api/v3/contents"},
		{"https://host.example/api/v1", "/api/v1/tasks/9", "https://host.example/api/v1/tasks/9"},
		{"https://host.example", "/api/v3/contents", "https://host.example/api/v3/contents"},
		{"https://host.example/", "/api/v3/contents", "https://host.example/api/v3/contents"},
	} {
		if got := joinPath(tc.base, tc.path); got != tc.want {
			t.Errorf("joinPath(%q,%q)=%q want %q", tc.base, tc.path, got, tc.want)
		}
	}
}

// joinV1 must add the /v1 segment only when it is missing.
func TestJoinV1AddsVersionSegmentOnce(t *testing.T) {
	if got := joinV1("https://host.example/v1", "/models"); got != "https://host.example/v1/models" {
		t.Errorf("joinV1 with existing /v1 = %q", got)
	}
	if got := joinV1("https://host.example/", "/models"); got != "https://host.example/v1/models" {
		t.Errorf("joinV1 without /v1 = %q", got)
	}
}

func TestDefaultStringFallsBackForBlankValues(t *testing.T) {
	if got := defaultString("   ", "fallback"); got != "fallback" {
		t.Errorf("defaultString(blank)=%q", got)
	}
	if got := defaultString("value", "fallback"); got != "value" {
		t.Errorf("defaultString(value)=%q", got)
	}
}

// Vidu requires both a usable base URL and an API key before any request is attempted.
func TestViduRejectsInvalidConfiguration(t *testing.T) {
	for _, cfg := range []AIConfig{
		{BaseURL: "", APIKey: "token"},
		{BaseURL: "ftp://host.example", APIKey: "token"},
		{BaseURL: "https://host.example", APIKey: "  "},
	} {
		if err := validateViduConfig(cfg); err == nil {
			t.Fatalf("validateViduConfig(%+v) accepted an invalid config", cfg)
		}
		if _, err := (&ViduVideoAdapter{}).Generate(context.Background(), cfg, VideoGenInput{ImageURL: "https://cdn/a.png"}); err == nil {
			t.Fatalf("Generate accepted an invalid config %+v", cfg)
		}
		if _, err := (&ViduVideoAdapter{}).Poll(context.Background(), cfg, "task"); err == nil {
			t.Fatalf("Poll accepted an invalid config %+v", cfg)
		}
	}
}

// Vidu is image-to-video only, so a prompt without any image must be rejected locally.
func TestViduGenerateRequiresAnImage(t *testing.T) {
	_, err := (&ViduVideoAdapter{}).Generate(context.Background(), AIConfig{BaseURL: "https://host.example", APIKey: "token"}, VideoGenInput{Prompt: "text only"})
	if err == nil || !strings.Contains(err.Error(), "at least one image") {
		t.Fatalf("err=%v, want the missing-image error", err)
	}
}

// Vidu polling needs a task id.
func TestViduPollRequiresTaskID(t *testing.T) {
	if _, err := (&ViduVideoAdapter{}).Poll(context.Background(), AIConfig{BaseURL: "https://host.example", APIKey: "token"}, "  "); err == nil {
		t.Fatal("empty task id accepted")
	}
}

// The Vidu token header must be normalized to a single "Token " prefix, and a response
// without a task id must be rejected.
func TestViduGenerateNormalizesTokenAndRequiresTaskID(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	// Already prefixed with "Token ", so the adapter must not prefix it twice.
	prefixedKey := "Token " + "fixture-value"
	_, err := (&ViduVideoAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: prefixedKey}, VideoGenInput{ImageURL: "https://cdn/a.png"})
	if err == nil || !strings.Contains(err.Error(), "missing task id") {
		t.Fatalf("err=%v, want the missing task id error", err)
	}
	if gotAuth != prefixedKey {
		t.Fatalf("authorization=%q, want a single Token prefix", gotAuth)
	}
}

// A Vidu HTTP error must not be reported as a successful submission.
func TestViduGenerateReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	defer server.Close()

	_, err := (&ViduVideoAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "token"}, VideoGenInput{ImageURL: "https://cdn/a.png"})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("err=%v, want the HTTP 403 surfaced", err)
	}
}

// Vidu returns finished videos in a creations array and uses its own status vocabulary.
func TestViduPollReadsCreationsAndNormalizesStatus(t *testing.T) {
	var payload string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	cfg := AIConfig{BaseURL: server.URL, APIKey: "token"}

	payload = `{"state":"success","creations":[{"url":"https://cdn.example/v.mp4"}]}`
	result, err := (&ViduVideoAdapter{}).Poll(context.Background(), cfg, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.VideoURL != "https://cdn.example/v.mp4" {
		t.Fatalf("result=%+v, want the creations URL", result)
	}

	for state, want := range map[string]string{"queueing": "pending", "processing": "processing", "failed": "failed", "": "processing"} {
		payload = `{"state":"` + state + `"}`
		polled, err := (&ViduVideoAdapter{}).Poll(context.Background(), cfg, "task-1")
		if err != nil {
			t.Fatal(err)
		}
		if polled.Status != want {
			t.Errorf("state %q normalized to %q, want %q", state, polled.Status, want)
		}
	}
}

// A Vidu poll HTTP error must be reported.
func TestViduPollReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "gone", http.StatusNotFound)
	}))
	defer server.Close()

	if _, err := (&ViduVideoAdapter{}).Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "token"}, "task"); err == nil {
		t.Fatal("poll accepted HTTP 404")
	}
}

// MiniMax endpoints must be derived without duplicating /v1 and must reject bases
// that are empty or not http(s).
func TestMiniMaxEndpointDerivation(t *testing.T) {
	tts, err := miniMaxTTSEndpoint("https://api.example.com/v1")
	if err != nil || tts != "https://api.example.com/v1/t2a_v2" {
		t.Fatalf("tts=%q err=%v", tts, err)
	}
	tts, err = miniMaxTTSEndpoint("https://api.example.com/?a=b")
	if err != nil || tts != "https://api.example.com/v1/t2a_v2" {
		t.Fatalf("tts=%q err=%v, want query stripped and /v1 added", tts, err)
	}
	voices, err := miniMaxVoiceEndpoint("https://api.example.com/v1")
	if err != nil || !strings.HasPrefix(voices, "https://api.example.com/v1/get_voice?") || !strings.Contains(voices, "voice_type=all") {
		t.Fatalf("voices=%q err=%v", voices, err)
	}
	for _, base := range []string{"", "   ", "ftp://api.example.com"} {
		if _, err := miniMaxTTSEndpoint(base); err == nil {
			t.Errorf("miniMaxTTSEndpoint(%q) accepted an invalid base", base)
		}
		if _, err := miniMaxVoiceEndpoint(base); err == nil {
			t.Errorf("miniMaxVoiceEndpoint(%q) accepted an invalid base", base)
		}
	}
}

// The voice list must be flattened across catalogues, and an empty catalogue must be
// reported as an error rather than an empty success.
func TestListMiniMaxVoicesFlattensCatalogues(t *testing.T) {
	var payload string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	cfg := AIConfig{BaseURL: server.URL, APIKey: "key"}

	payload = `{"system_voice":[{"voice_id":"sys-1","voice_name":"系统音"},{"name":"no id"}],"voice_cloning":[{"voice_id":"clone-1"}]}`
	voices, err := ListMiniMaxVoices(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(voices) != 2 {
		t.Fatalf("voices=%+v, want entries without an id skipped", voices)
	}
	if voices[0].ID != "sys-1" || voices[0].Name != "系统音" || voices[0].Capabilities != "system_voice" {
		t.Fatalf("voices[0]=%+v", voices[0])
	}
	if voices[1].ID != "clone-1" || voices[1].Name != "clone-1" {
		t.Fatalf("voices[1]=%+v, want the id used as the fallback name", voices[1])
	}

	payload = `{"system_voice":[]}`
	if _, err := ListMiniMaxVoices(context.Background(), cfg); err == nil {
		t.Fatal("empty voice catalogue accepted")
	}

	payload = `{"base_resp":{"status_code":1004}}`
	if _, err := ListMiniMaxVoices(context.Background(), cfg); err == nil {
		t.Fatal("application error accepted")
	}
}

// A bad base URL must fail before any request is made.
func TestListMiniMaxVoicesValidatesBaseURL(t *testing.T) {
	if _, err := ListMiniMaxVoices(context.Background(), AIConfig{BaseURL: "", APIKey: "key"}); err == nil {
		t.Fatal("empty base URL accepted")
	}
}

// The TTS adapter must validate its inputs locally before spending a provider call.
func TestMiniMaxTTSValidatesInputs(t *testing.T) {
	adapter := &MiniMaxTTSAdapter{}
	if _, err := adapter.Generate(context.Background(), AIConfig{BaseURL: ""}, TTSInput{Text: "hi"}); err == nil {
		t.Fatal("empty base URL accepted")
	}
	if _, err := adapter.Generate(context.Background(), AIConfig{BaseURL: "https://api.example.com", APIKey: " "}, TTSInput{Text: "hi"}); err == nil {
		t.Fatal("missing API key accepted")
	}
	if _, err := adapter.Generate(context.Background(), AIConfig{BaseURL: "https://api.example.com", APIKey: "key"}, TTSInput{Text: "   "}); err == nil {
		t.Fatal("empty text accepted")
	}
}

// A successful TTS response is hex-encoded audio; the adapter must decode it and apply
// the documented model and voice defaults.
func TestMiniMaxTTSDecodesHexAudioAndAppliesDefaults(t *testing.T) {
	audio := []byte("fake mp3 payload")
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"data":{"audio":"` + hex.EncodeToString(audio) + `"},"base_resp":{"status_code":0}}`))
	}))
	defer server.Close()

	result, err := (&MiniMaxTTSAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, TTSInput{Text: "你好"})
	if err != nil {
		t.Fatal(err)
	}
	if string(result.AudioBytes) != string(audio) || result.Format != "mp3" {
		t.Fatalf("result=%+v, want the decoded audio", result)
	}
	if body["model"] != "speech-02-hd" {
		t.Fatalf("model=%v, want the documented default", body["model"])
	}
	voice, _ := body["voice_setting"].(map[string]any)
	if voice["voice_id"] != "male-qn-qingse" {
		t.Fatalf("voice_setting=%v, want the default voice", voice)
	}
}

// Provider-side failures must not be mistaken for audio.
func TestMiniMaxTTSRejectsFailureResponses(t *testing.T) {
	var payload string
	var status int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != 0 {
			w.WriteHeader(status)
		}
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	cfg := AIConfig{BaseURL: server.URL, APIKey: "key"}

	for _, tc := range []struct {
		name    string
		status  int
		payload string
	}{
		{"http error", http.StatusBadGateway, `{}`},
		{"application error", 0, `{"base_resp":{"status_code":1004}}`},
		{"no audio", 0, `{"data":{"audio":""},"base_resp":{"status_code":0}}`},
		{"invalid hex", 0, `{"data":{"audio":"zzzz"},"base_resp":{"status_code":0}}`},
		{"malformed json", 0, `not json`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, payload = tc.status, tc.payload
			if _, err := (&MiniMaxTTSAdapter{}).Generate(context.Background(), cfg, TTSInput{Text: "你好"}); err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
		})
	}
}

// The compat image poller walks several candidate paths; when every one fails it must
// report "processing" with the last error rather than claiming completion.
func TestOpenAICompatImagePollReportsProcessingWhenAllPathsFail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusInternalServerError)
	}))
	defer server.Close()

	result, err := (&OpenAICompatImageAdapter{ProviderName: "chatfire"}).Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "processing" || result.Error == "" {
		t.Fatalf("result=%+v, want processing with the last error retained", result)
	}
}

// Provider status vocabularies must be normalized to the internal set.
func TestOpenAICompatImagePollNormalizesStatuses(t *testing.T) {
	var payload string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	adapter := &OpenAICompatImageAdapter{ProviderName: "chatfire"}
	cfg := AIConfig{BaseURL: server.URL, APIKey: "key"}

	for _, tc := range []struct{ payloadValue, want string }{
		{`{"status":"queued"}`, "pending"},
		{`{"status":"error"}`, "failed"},
		{`{"status":"running"}`, "processing"},
		{`{}`, "processing"},
	} {
		payload = tc.payloadValue
		result, err := adapter.Poll(context.Background(), cfg, "task-1")
		if err != nil {
			t.Fatal(err)
		}
		if result.Status != tc.want {
			t.Errorf("payload %s normalized to %q, want %q", tc.payloadValue, result.Status, tc.want)
		}
	}
}

// A completed poll may deliver the image through a nested data.images array.
func TestOpenAICompatImagePollReadsNestedImageArray(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"succeeded","data":{"images":[{"url":"https://cdn.example/i.png"}]}}`))
	}))
	defer server.Close()

	result, err := (&OpenAICompatImageAdapter{ProviderName: "chatfire"}).Poll(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "completed" || result.ImageURL != "https://cdn.example/i.png" {
		t.Fatalf("result=%+v, want the nested image URL", result)
	}
}

// readProviderResponse must refuse payloads beyond the hard cap so a hostile provider
// cannot exhaust memory.
func TestReadProviderResponseEnforcesSizeCap(t *testing.T) {
	oversized := bytes.Repeat([]byte("a"), maxProviderResponseBytes+1)
	if _, err := readProviderResponse(bytes.NewReader(oversized)); err == nil {
		t.Fatal("oversized provider response accepted")
	}
	data, err := readProviderResponse(bytes.NewReader(bytes.Repeat([]byte("a"), 16)))
	if err != nil || len(data) != 16 {
		t.Fatalf("data=%d err=%v", len(data), err)
	}
}

// OpenAI video references are data URIs; unsupported or malformed inputs must be
// rejected before upload.
func TestDecodeImageDataURIRejectsUnsupportedInput(t *testing.T) {
	for _, value := range []string{
		"https://cdn.example/a.png",
		"data:image/gif;base64,AAAA",
		"data:image/png,notbase64header",
		"data:image/png;base64,!!!!",
		"data:image/png;base64,",
	} {
		if _, _, err := decodeImageDataURI(value); err == nil {
			t.Errorf("decodeImageDataURI(%q) accepted invalid input", value)
		}
	}
	mimeType, data, err := decodeImageDataURI("data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("payload")))
	if err != nil || mimeType != "image/png" || string(data) != "payload" {
		t.Fatalf("mime=%q data=%q err=%v", mimeType, data, err)
	}
}

func TestParseVideoSizeRejectsMalformedValues(t *testing.T) {
	for _, value := range []string{"", "1280", "axb", "0x720", "1280x0", "-10x20"} {
		if _, _, ok := parseVideoSize(value); ok {
			t.Errorf("parseVideoSize(%q) accepted a malformed size", value)
		}
	}
	width, height, ok := parseVideoSize("1280x720")
	if !ok || width != 1280 || height != 720 {
		t.Fatalf("parseVideoSize=%d,%d,%v", width, height, ok)
	}
}

// A reference image that already matches the target size must pass through untouched;
// a mismatched one must be resized while keeping its encoding.
func TestPrepareOpenAIReferenceResizesOnlyWhenNeeded(t *testing.T) {
	exact := pngDataURI(t, 64, 32)
	_, exactData, err := decodeImageDataURI(exact)
	if err != nil {
		t.Fatal(err)
	}
	mimeType, data, err := prepareOpenAIReference(exact, "64x32")
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/png" || !bytes.Equal(data, exactData) {
		t.Fatal("a correctly sized PNG reference was re-encoded")
	}

	mimeType, resized, err := prepareOpenAIReference(pngDataURI(t, 20, 10), "40x20")
	if err != nil {
		t.Fatal(err)
	}
	decoded, _, err := image.Decode(bytes.NewReader(resized))
	if err != nil {
		t.Fatal(err)
	}
	if mimeType != "image/png" || decoded.Bounds().Dx() != 40 || decoded.Bounds().Dy() != 20 {
		t.Fatalf("mime=%q bounds=%v, want a 40x20 PNG", mimeType, decoded.Bounds())
	}

	jpegMime, jpegData, err := prepareOpenAIReference(jpegDataURI(t, 20, 10), "40x20")
	if err != nil {
		t.Fatal(err)
	}
	if jpegMime != "image/jpeg" {
		t.Fatalf("mime=%q, want the JPEG encoding preserved", jpegMime)
	}
	if _, err := jpeg.Decode(bytes.NewReader(jpegData)); err != nil {
		t.Fatalf("resized JPEG is not decodable: %v", err)
	}
}

// WebP cannot be re-encoded locally, and undecodable or badly sized inputs must fail.
func TestPrepareOpenAIReferenceRejectsUnsupportedCases(t *testing.T) {
	webp := "data:image/webp;base64," + base64.StdEncoding.EncodeToString([]byte("webp"))
	if _, _, err := prepareOpenAIReference(webp, "64x32"); err == nil {
		t.Fatal("WebP reference accepted")
	}
	if _, _, err := prepareOpenAIReference(pngDataURI(t, 8, 8), "bad-size"); err == nil {
		t.Fatal("invalid size accepted")
	}
	notAnImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("not a png"))
	if _, _, err := prepareOpenAIReference(notAnImage, "64x32"); err == nil {
		t.Fatal("undecodable reference accepted")
	}
}

// The probe must reject providers it cannot test and base URLs that are unusable.
func TestConnectionProbeRequestValidatesProviderAndURL(t *testing.T) {
	if _, err := connectionProbeRequest(AIConfig{Provider: "openai", BaseURL: "ftp://host"}); err == nil {
		t.Fatal("non-http base URL accepted")
	}
	if _, err := connectionProbeRequest(AIConfig{Provider: "unknown", BaseURL: "https://host.example"}); err == nil {
		t.Fatal("unsupported provider accepted for probing")
	}
	plan, err := connectionProbeRequest(AIConfig{Provider: "gemini", BaseURL: "https://host.example", APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if plan.headers["X-Goog-Api-Key"] != "key" || plan.headers["Authorization"] != "" {
		t.Fatalf("headers=%v, want Gemini to use its own key header", plan.headers)
	}
	if !plan.verifyModel || !strings.HasSuffix(plan.endpoint, "/v1beta/models") {
		t.Fatalf("plan=%+v", plan)
	}
	viduPlan, err := connectionProbeRequest(AIConfig{Provider: "vidu", BaseURL: "https://host.example", APIKey: "key"})
	if err != nil {
		t.Fatal(err)
	}
	if viduPlan.verifyModel || viduPlan.method != http.MethodHead {
		t.Fatalf("plan=%+v, want a non-billable HEAD probe", viduPlan)
	}
}

// When the provider enumerates models, a configured model that is absent must be
// reported so the operator can fix the config.
func TestProbeConnectionDetectsMissingConfiguredModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"other-model"}]}`))
	}))
	defer server.Close()

	_, err := ProbeConnection(context.Background(), AIConfig{Provider: "openai", BaseURL: server.URL, APIKey: "key", Model: "wanted-model"})
	if err == nil || !strings.Contains(err.Error(), "wanted-model") {
		t.Fatalf("err=%v, want the missing model reported", err)
	}
}

// Gemini lists models as objects with a name field prefixed by "models/".
func TestProbeResponseContainsModelHandlesBothCatalogueShapes(t *testing.T) {
	found, known := probeResponseContainsModel([]byte(`{"models":[{"name":"models/gemini-pro"}]}`), "gemini-pro")
	if !known || !found {
		t.Fatalf("found=%v known=%v, want the prefixed name matched", found, known)
	}
	found, known = probeResponseContainsModel([]byte(`{"data":[{"id":"a"}]}`), "b")
	if !known || found {
		t.Fatalf("found=%v known=%v, want a known catalogue without the model", found, known)
	}
	if _, known := probeResponseContainsModel([]byte(`not json`), "a"); known {
		t.Fatal("malformed catalogue reported as known")
	}
	if _, known := probeResponseContainsModel([]byte(`{"object":"list"}`), "a"); known {
		t.Fatal("catalogue without a list reported as known")
	}
}

// A model-verifying probe must treat a non-2xx status as a failure, while a
// reachability-only probe still rejects 5xx.
func TestProbeConnectionStatusHandlingDependsOnVerification(t *testing.T) {
	serverError := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer serverError.Close()

	if _, err := ProbeConnection(context.Background(), AIConfig{Provider: "vidu", BaseURL: serverError.URL, APIKey: "key"}); err == nil {
		t.Fatal("HTTP 500 accepted for a reachability probe")
	}
	if _, err := ProbeConnection(context.Background(), AIConfig{Provider: "openai", BaseURL: serverError.URL, APIKey: "key"}); err == nil {
		t.Fatal("HTTP 500 accepted for a model probe")
	}

	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFound.Close()
	if _, err := ProbeConnection(context.Background(), AIConfig{Provider: "openai", BaseURL: notFound.URL, APIKey: "key"}); err == nil {
		t.Fatal("HTTP 404 accepted for a model-verifying probe")
	}
}

// A transport failure must be translated into operator guidance.
func TestProbeConnectionHumanizesTransportFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := server.URL
	server.Close()

	_, err := ProbeConnection(context.Background(), AIConfig{Provider: "openai", BaseURL: url, APIKey: "key"})
	if err == nil || !strings.Contains(err.Error(), "连接失败") {
		t.Fatalf("err=%v, want a humanized connection failure", err)
	}
}

// Every humanize branch must produce operator guidance rather than a raw error.
func TestHumanizeProviderProbeErrorCoversKnownFailureModes(t *testing.T) {
	if got := humanizeProviderProbeError(nil); got == "" {
		t.Fatal("nil error produced an empty message")
	}
	cases := map[string]string{
		"provider address is not allowed": "私网",
		"dial tcp: i/o timeout":           "超时",
		"no such host":                    "DNS",
		"connection refused":              "拒绝连接",
		"something entirely unexpected":   "连接失败：",
	}
	for input, want := range cases {
		got := humanizeProviderProbeError(errors.New(input))
		if !strings.Contains(got, want) {
			t.Errorf("humanize(%q)=%q, want it to mention %q", input, got, want)
		}
	}
}

// SecureProviderHTTPClient must hand out a client with the same guarded transport.
func TestSecureProviderHTTPClientKeepsGuardedTransport(t *testing.T) {
	t.Setenv("AI_PROVIDER_PRIVATE_HOSTS", "")
	t.Setenv("AI_PROVIDER_CA_FILE", "")
	client := SecureProviderHTTPClient(5 * time.Second)
	if client == nil || client.Timeout != 5*time.Second {
		t.Fatalf("client=%+v, want the requested timeout", client)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://169.254.169.254/latest/meta-data", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Do(req); err == nil || !strings.Contains(err.Error(), "provider address is not allowed") {
		t.Fatalf("err=%v, want the metadata address blocked at dial time", err)
	}
}

// A hostname that cannot be resolved must be refused rather than dialed blindly.
func TestLookupProviderIPsRejectsUnresolvableHost(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if _, err := lookupProviderIPs(ctx, "invalid-host-that-does-not-exist.invalid", false); err == nil {
		t.Fatal("unresolvable host accepted")
	}
}

// A loopback literal is allowed for in-process contract servers, but other private
// literals require explicit allow-listing.
func TestProviderLiteralIPAllowance(t *testing.T) {
	t.Setenv("AI_PROVIDER_PRIVATE_HOSTS", "")
	t.Setenv("AI_PROVIDER_CA_FILE", "")
	if !providerLiteralIPAllowed("127.0.0.1", net.ParseIP("127.0.0.1")) {
		t.Fatal("loopback literal was rejected")
	}
	if providerLiteralIPAllowed("10.0.0.5", net.ParseIP("10.0.0.5")) {
		t.Fatal("private literal was allowed without an allowlist")
	}
}

// providerTLSConfig must enforce a modern floor and only install custom roots when a
// readable, parseable CA file is configured.
func TestProviderTLSConfigUsesCustomRootsOnlyWhenConfigured(t *testing.T) {
	t.Setenv("AI_PROVIDER_CA_FILE", "")
	config := providerTLSConfig()
	if config.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion=%d, want TLS 1.2", config.MinVersion)
	}
	if config.RootCAs != nil {
		t.Fatal("custom roots installed without a CA file")
	}
	t.Setenv("AI_PROVIDER_CA_FILE", t.TempDir()+"/missing.pem")
	if providerTLSConfig().RootCAs != nil {
		t.Fatal("custom roots installed from an unreadable CA file")
	}
	if ProviderCustomCAConfigured() {
		t.Fatal("unreadable CA file reported as configured")
	}
	garbage := t.TempDir() + "/garbage.pem"
	if err := os.WriteFile(garbage, []byte("not a certificate"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_PROVIDER_CA_FILE", garbage)
	if ProviderCustomCAConfigured() {
		t.Fatal("unparseable CA file reported as configured")
	}
}
