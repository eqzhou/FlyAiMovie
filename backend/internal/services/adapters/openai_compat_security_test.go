package adapters

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAICompatImageDoesNotExposeProviderErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "provider-internal-secret", http.StatusBadGateway)
	}))
	defer server.Close()
	_, err := (&OpenAICompatImageAdapter{ProviderName: "chatfire"}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "test"})
	if err == nil {
		t.Fatal("expected provider error")
	}
	if strings.Contains(err.Error(), "provider-internal-secret") {
		t.Fatalf("provider response leaked into error: %v", err)
	}
}

func TestOpenAICompatImageRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("x", maxProviderResponseBytes+1)))
	}))
	defer server.Close()
	_, err := (&OpenAICompatImageAdapter{}).Generate(context.Background(), AIConfig{BaseURL: server.URL, APIKey: "key"}, ImageGenInput{Prompt: "test"})
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("oversized response err=%v", err)
	}
}
