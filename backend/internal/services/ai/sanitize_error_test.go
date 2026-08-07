package ai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// Provider errors can carry request URLs, model names and key fragments in
// their message. sanitizeChatProviderError reduces them to a status code so
// nothing vendor-specific reaches an API response or a job's error column.

func TestSanitizeChatProviderErrorKeepsOnlyStatusCode(t *testing.T) {
	secret := "https://api.vendor.example/v1/chat?key=sk-live-SECRET"

	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "APIError",
			err:  &openai.APIError{HTTPStatusCode: 429, Message: secret},
			want: "text provider request failed with HTTP 429",
		},
		{
			name: "RequestError",
			err:  &openai.RequestError{HTTPStatusCode: 502, Err: errors.New(secret)},
			want: "text provider request failed with HTTP 502",
		},
		{
			name: "wrapped APIError",
			err:  fmt.Errorf("calling provider: %w", &openai.APIError{HTTPStatusCode: 401, Message: secret}),
			want: "text provider request failed with HTTP 401",
		},
		{
			name: "plain error",
			err:  errors.New(secret),
			want: "text provider request failed",
		},
		{
			// Without a status code there is nothing safe to report, so it must
			// fall back rather than surface the message.
			name: "APIError without status",
			err:  &openai.APIError{Message: secret},
			want: "text provider request failed",
		},
		{
			name: "nil error",
			err:  nil,
			want: "text provider request failed",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeChatProviderError(context.Background(), tc.err)
			if got == nil {
				t.Fatal("sanitizeChatProviderError returned nil")
			}
			if got.Error() != tc.want {
				t.Fatalf("message = %q, want %q", got.Error(), tc.want)
			}
			if strings.Contains(got.Error(), "sk-live") || strings.Contains(got.Error(), "vendor.example") {
				t.Fatalf("sanitized error leaked provider detail: %q", got.Error())
			}
		})
	}
}

func TestSanitizeChatProviderErrorPrefersContextCause(t *testing.T) {
	// A cancelled or timed-out request is the caller's own condition, not a
	// provider failure, so the context error must win and stay recognisable to
	// errors.Is upstream.
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	got := sanitizeChatProviderError(canceled, &openai.APIError{HTTPStatusCode: 500, Message: "boom"})
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", got)
	}

	expired, cancelExpired := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancelExpired()
	got = sanitizeChatProviderError(expired, errors.New("boom"))
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", got)
	}
}
