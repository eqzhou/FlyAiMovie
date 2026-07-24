package httpapi

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
)

func TestLiveSmokeSMTPPasswordResetAndInvitation(t *testing.T) {
	host := strings.TrimSpace(os.Getenv("SMOKE_SMTP_HOST"))
	user := strings.TrimSpace(os.Getenv("SMOKE_SMTP_USERNAME"))
	pass := strings.TrimSpace(os.Getenv("SMOKE_SMTP_PASSWORD"))
	from := strings.TrimSpace(os.Getenv("SMOKE_SMTP_FROM"))
	to := strings.TrimSpace(os.Getenv("SMOKE_SMTP_TO"))
	base := strings.TrimSpace(os.Getenv("SMOKE_APP_URL_BASE"))
	portText := strings.TrimSpace(os.Getenv("SMOKE_SMTP_PORT"))
	if host == "" || user == "" || pass == "" || from == "" || to == "" || base == "" || portText == "" {
		t.Skip("live SMTP smoke skipped; set SMOKE_SMTP_HOST, SMOKE_SMTP_PORT, SMOKE_SMTP_USERNAME, SMOKE_SMTP_PASSWORD, SMOKE_SMTP_FROM, SMOKE_SMTP_TO, SMOKE_APP_URL_BASE")
	}
	if !strings.HasPrefix(strings.ToLower(base), "https://") {
		t.Fatal("SMOKE_APP_URL_BASE must be https")
	}
	port := 587
	if _, err := fmtSscanfPort(portText, &port); err != nil {
		t.Fatalf("invalid SMOKE_SMTP_PORT: %v", err)
	}
	sender := NewSMTPPasswordResetSender(config.EmailConfig{
		SMTPHost: host, SMTPPort: port, SMTPUsername: user, SMTPPassword: pass, From: from, ResetURLBase: base,
	})
	if err := sender.SendPasswordReset(to, "smoke-reset-token", time.Now().UTC().Add(30*time.Minute)); err != nil {
		t.Fatalf("password reset smoke failed: %v", err)
	}
	if err := sender.SendInvitation(to, "FlyAiMovie Smoke", "editor", "smoke-invite-token", time.Now().UTC().Add(2*time.Hour)); err != nil {
		t.Fatalf("invitation smoke failed: %v", err)
	}
}

func fmtSscanfPort(text string, port *int) (int, error) {
	n := 0
	for _, r := range text {
		if r < '0' || r > '9' {
			return 0, errInvalidPort
		}
		n = n*10 + int(r-'0')
	}
	if n <= 0 || n > 65535 {
		return 0, errInvalidPort
	}
	*port = n
	return n, nil
}

type invalidPortError struct{}

func (invalidPortError) Error() string { return "invalid port" }

var errInvalidPort = invalidPortError{}
