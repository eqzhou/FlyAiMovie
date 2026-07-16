package httpapi

import "testing"

func TestValidateAIConfigRejectsNonPublicBaseURLs(t *testing.T) {
	tests := []struct {
		name string
		base string
	}{
		{name: "loopback", base: "http://127.0.0.1:8080"},
		{name: "ipv6 loopback", base: "http://[::1]:8080"},
		{name: "private", base: "http://10.0.0.1"},
		{name: "link local", base: "http://169.254.169.254"},
		{name: "localhost", base: "http://localhost:8080"},
		{name: "userinfo", base: "https://user:pass@example.com"},
		{name: "query", base: "https://api.example.com/?token=secret"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateAIConfigInput("image", "openai", "test", tc.base, "key"); err == nil {
				t.Fatalf("expected base URL %q to be rejected", tc.base)
			}
		})
	}
}

func TestValidateAIConfigAllowsPublicBaseURL(t *testing.T) {
	if err := validateAIConfigInput("image", "openai", "test", "https://api.example.com/v1", "key"); err != nil {
		t.Fatalf("public base URL rejected: %v", err)
	}
}
