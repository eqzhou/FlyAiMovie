package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestRateLimitRejectsExcessRequestsAndIgnoresForwardedFor(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Server.RateLimitPerMinute = 2
	router := server.Router()

	for i := 0; i < 2; i++ {
		response := performRequestFrom(router, http.MethodGet, "/api/v1/health", "203.0.113.10:1200", "198.51.100.1")
		if response.Code != http.StatusOK {
			t.Fatalf("request %d status=%d body=%s", i+1, response.Code, response.Body.String())
		}
	}

	limited := performRequestFrom(router, http.MethodGet, "/api/v1/health", "203.0.113.10:9999", "198.51.100.2")
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("limited status=%d want 429; body=%s", limited.Code, limited.Body.String())
	}
	if limited.Header().Get("Retry-After") == "" {
		t.Fatal("limited response is missing Retry-After")
	}
}

func TestRateLimitIsolatesRemoteAddressesAndCoversWebhook(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Server.RateLimitPerMinute = 1
	router := server.Router()

	first := performRequestFrom(router, http.MethodGet, "/api/v1/health", "203.0.113.11:1200", "")
	if first.Code != http.StatusOK {
		t.Fatalf("first source status=%d", first.Code)
	}
	secondSource := performRequestFrom(router, http.MethodGet, "/api/v1/health", "203.0.113.12:1200", "")
	if secondSource.Code != http.StatusOK {
		t.Fatalf("second source status=%d", secondSource.Code)
	}

	webhook := performRequestFrom(router, http.MethodPost, "/api/v1/webhooks/generic", "203.0.113.11:1300", "")
	if webhook.Code != http.StatusTooManyRequests {
		t.Fatalf("webhook status=%d want 429; body=%s", webhook.Code, webhook.Body.String())
	}
}

func TestRateLimitCoversCORSPreflight(t *testing.T) {
	server, _ := testServerRouter(t)
	server.Cfg.Server.RateLimitPerMinute = 1
	router := server.Router()

	first := performRequestFrom(router, http.MethodOptions, "/api/v1/dramas", "203.0.113.13:1200", "")
	if first.Code != http.StatusNoContent {
		t.Fatalf("first preflight status=%d want 204", first.Code)
	}
	limited := performRequestFrom(router, http.MethodOptions, "/api/v1/dramas", "203.0.113.13:1201", "")
	if limited.Code != http.StatusTooManyRequests {
		t.Fatalf("second preflight status=%d want 429", limited.Code)
	}
}

func TestRateLimiterResetsExpiredWindow(t *testing.T) {
	now := time.Date(2026, time.July, 16, 10, 0, 0, 0, time.UTC)
	limiter := newIPRateLimiter(1, time.Minute, func() time.Time { return now })

	if allowed, _ := limiter.Allow("203.0.113.20"); !allowed {
		t.Fatal("first request should be allowed")
	}
	if allowed, _ := limiter.Allow("203.0.113.20"); allowed {
		t.Fatal("second request should be limited")
	}

	now = now.Add(time.Minute)
	if allowed, _ := limiter.Allow("203.0.113.20"); !allowed {
		t.Fatal("request after window reset should be allowed")
	}
}

func TestRateLimiterBoundsTrackedSources(t *testing.T) {
	limiter := newIPRateLimiter(1, time.Minute, time.Now)
	for i := 0; i < maxTrackedRateLimitSources*2; i++ {
		limiter.Allow("source-" + strconv.Itoa(i))
	}
	if got := len(limiter.entries); got > maxTrackedRateLimitSources+1 {
		t.Fatalf("tracked sources=%d exceeds bounded capacity", got)
	}
}

func TestRateLimiterAllowsConcurrentRequestsWithoutExceedingLimit(t *testing.T) {
	const limit = 50
	limiter := newIPRateLimiter(limit, time.Minute, time.Now)
	var allowed int
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < limit*2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, _ := limiter.Allow("203.0.113.21")
			if ok {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != limit {
		t.Fatalf("allowed=%d want %d", allowed, limit)
	}
}

func TestRemoteIPHandlesIPv4IPv6AndMalformedAddresses(t *testing.T) {
	tests := map[string]string{
		"203.0.113.30:8080":     "203.0.113.30",
		"203.0.113.31":          "203.0.113.31",
		"[2001:db8::1]:8080":    "2001:db8::1",
		"2001:db8::2":           "2001:db8::2",
		"malformed remote addr": "unknown",
		"":                      "unknown",
	}
	for input, want := range tests {
		if got := remoteIP(input); got != want {
			t.Errorf("remoteIP(%q)=%q want %q", input, got, want)
		}
	}
}

func performRequestFrom(handler http.Handler, method, path, remoteAddr, forwardedFor string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = remoteAddr
	if forwardedFor != "" {
		req.Header.Set("X-Forwarded-For", forwardedFor)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, req)
	return response
}
