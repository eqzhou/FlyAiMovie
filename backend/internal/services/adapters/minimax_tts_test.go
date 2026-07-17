package adapters

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMiniMaxTTSGenerateContract(t *testing.T) {
	wantAudio := []byte("ID3-mini-max")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/t2a_v2" {
			t.Fatalf("path = %s, want /v1/t2a_v2", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content type = %q", got)
		}
		var body struct {
			Model        string `json:"model"`
			Text         string `json:"text"`
			Stream       bool   `json:"stream"`
			VoiceSetting struct {
				VoiceID string `json:"voice_id"`
			} `json:"voice_setting"`
			AudioSetting struct {
				Format string `json:"format"`
			} `json:"audio_setting"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if body.Model != "speech-02-turbo" || body.Text != "你好" || body.Stream || body.VoiceSetting.VoiceID != "female-test" || body.AudioSetting.Format != "mp3" {
			t.Fatalf("unexpected request body: %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"audio":"` + hex.EncodeToString(wantAudio) + `"},"base_resp":{"status_code":0,"status_msg":""}}`))
	}))
	defer server.Close()

	result, err := (&MiniMaxTTSAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "test-key"}, TTSInput{Text: " 你好 ", VoiceID: "female-test", Model: "speech-02-turbo"})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if string(result.AudioBytes) != string(wantAudio) || result.AudioHex != hex.EncodeToString(wantAudio) || result.Format != "mp3" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestMiniMaxTTSGenerateErrors(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AIConfig
		input   TTSInput
		handler http.HandlerFunc
		want    string
	}{
		{name: "missing base url", cfg: AIConfig{APIKey: "key"}, input: TTSInput{Text: "hello"}, want: "base URL is required"},
		{name: "missing api key", cfg: AIConfig{BaseURL: "http://example.test"}, input: TTSInput{Text: "hello"}, want: "API key is required"},
		{name: "missing text", cfg: AIConfig{BaseURL: "http://example.test", APIKey: "key"}, want: "text is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := (&MiniMaxTTSAdapter{}).Generate(context.Background(), tc.cfg, tc.input)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}

	for _, tc := range []struct {
		name string
		code int
		body string
		want string
	}{
		{name: "http json error", code: http.StatusUnauthorized, body: `{"message":"invalid api key"}`, want: "HTTP 401: invalid api key"},
		{name: "business error", code: http.StatusOK, body: `{"data":{},"base_resp":{"status_code":1004,"status_msg":"invalid voice"}}`, want: "provider status 1004: invalid voice"},
		{name: "empty audio", code: http.StatusOK, body: `{"data":{"audio":""},"base_resp":{"status_code":0}}`, want: "response contains no audio"},
		{name: "invalid audio", code: http.StatusOK, body: `{"data":{"audio":"not-hex"},"base_resp":{"status_code":0}}`, want: "audio is not valid hex"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.code)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			_, err := (&MiniMaxTTSAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, TTSInput{Text: "hello"})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestMiniMaxTTSEndpoint(t *testing.T) {
	for _, tc := range []struct {
		base string
		want string
	}{
		{base: "https://api.minimax.chat", want: "https://api.minimax.chat/v1/t2a_v2"},
		{base: "https://api.minimax.chat/", want: "https://api.minimax.chat/v1/t2a_v2"},
		{base: "https://api.minimax.chat/v1", want: "https://api.minimax.chat/v1/t2a_v2"},
		{base: "https://api.minimax.chat/v1/", want: "https://api.minimax.chat/v1/t2a_v2"},
	} {
		t.Run(tc.base, func(t *testing.T) {
			got, err := miniMaxTTSEndpoint(tc.base)
			if err != nil || got != tc.want {
				t.Fatalf("endpoint = %q, err = %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestListMiniMaxVoicesContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/get_voice" || r.URL.Query().Get("voice_type") != "all" {
			t.Fatalf("url=%s", r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer key" {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{"system_voice":[{"voice_id":"voice-1","voice_name":"Narrator","language":"zh"}],"base_resp":{"status_code":0}}`))
	}))
	defer server.Close()
	voices, err := ListMiniMaxVoices(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"})
	if err != nil || len(voices) != 1 || voices[0].ID != "voice-1" || voices[0].Name != "Narrator" {
		t.Fatalf("voices=%+v err=%v", voices, err)
	}
}
