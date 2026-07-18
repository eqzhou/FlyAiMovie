package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProbeConnectionUsesAuthenticatedModelEndpoint(t *testing.T) {
	credential := strings.Join([]string{"provider", "fixture"}, "-")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer "+credential {
			t.Fatalf("authorization=%q", r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"model-a"}]}`))
	}))
	defer server.Close()

	result, err := ProbeConnection(context.Background(), AIConfig{
		Provider: "openai", BaseURL: server.URL, APIKey: credential, Model: "model-a",
	})
	if err != nil {
		t.Fatalf("probe connection: %v", err)
	}
	if result.Status != "ok" || result.LatencyMS < 0 || result.Provider != "openai" || result.Model != "model-a" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestProbeConnectionRejectsProviderAuthenticationFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer server.Close()

	if _, err := ProbeConnection(context.Background(), AIConfig{Provider: "openai", BaseURL: server.URL, APIKey: "bad"}); err == nil {
		t.Fatal("authentication failure accepted")
	}
}

func TestProbeConnectionSupportsMock(t *testing.T) {
	result, err := ProbeConnection(context.Background(), AIConfig{Provider: "mock", Model: "mock"})
	if err != nil || result.Status != "ok" || result.Detail == "" {
		t.Fatalf("mock result=%+v err=%v", result, err)
	}
}

func TestProbeConnectionFallsBackToNonBillableReachabilityCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Fatalf("method=%q", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	result, err := ProbeConnection(context.Background(), AIConfig{Provider: "vidu", BaseURL: server.URL, APIKey: "key", Model: "vidu-model"})
	if err != nil || result.Status != "ok" || !strings.Contains(result.Detail, "未触发生成任务") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
