package httpapi

import (
	"testing"
	"time"
)

func TestSMTPPasswordResetSenderRequiresHTTPSAndCredentials(t *testing.T) {
	sender := &SMTPPasswordResetSender{Host: "smtp.example.com", Port: 587, Username: "u", Password: "p", From: "noreply@example.com", ResetURLBase: "http://app.example.com"}
	if err := sender.SendPasswordReset("user@example.com", "token", time.Now()); err == nil {
		t.Fatal("expected insecure reset URL to be rejected")
	}
	sender.ResetURLBase = "https://app.example.com"
	sender.Username = ""
	if err := sender.SendPasswordReset("user@example.com", "token", time.Now()); err == nil {
		t.Fatal("expected missing SMTP credentials to be rejected")
	}
}
