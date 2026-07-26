package httpapi

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"net/url"
	"strings"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
)

// A stalled SMTP peer must not pin a request goroutine indefinitely. Dialing
// and the whole conversation are bounded so password reset and invitation
// endpoints fail fast instead of hanging.
const (
	smtpDialTimeout    = 10 * time.Second
	smtpSessionTimeout = 30 * time.Second
)

type SMTPPasswordResetSender struct {
	Host         string
	Port         int
	Username     string
	Password     string
	From         string
	ResetURLBase string

	// Zero means use the package defaults; tests override these to assert the
	// stall protection without waiting for the production timeouts.
	DialTimeout    time.Duration
	SessionTimeout time.Duration
}

func (s *SMTPPasswordResetSender) dialTimeout() time.Duration {
	if s.DialTimeout > 0 {
		return s.DialTimeout
	}
	return smtpDialTimeout
}

func (s *SMTPPasswordResetSender) sessionTimeout() time.Duration {
	if s.SessionTimeout > 0 {
		return s.SessionTimeout
	}
	return smtpSessionTimeout
}

func NewSMTPPasswordResetSender(cfg config.EmailConfig) *SMTPPasswordResetSender {
	return &SMTPPasswordResetSender{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.From, ResetURLBase: strings.TrimRight(cfg.ResetURLBase, "/")}
}

func (s *SMTPPasswordResetSender) configured() error {
	if s == nil || s.Host == "" || s.Port <= 0 || s.Username == "" || s.Password == "" || s.From == "" || !strings.HasPrefix(strings.ToLower(s.ResetURLBase), "https://") {
		return fmt.Errorf("SMTP mailer is not configured")
	}
	return nil
}

func (s *SMTPPasswordResetSender) SendPasswordReset(email, token string, expiresAt time.Time) error {
	if err := s.configured(); err != nil {
		return err
	}
	link := s.ResetURLBase + "/password-reset/" + url.PathEscape(token)
	subject := "FlyAiMovie password reset"
	body := fmt.Sprintf("Hello,\n\nUse this secure link to set a new FlyAiMovie password:\n%s\n\nThis link expires at %s UTC and can only be used once.\n\nIf you did not request this, ignore this email.\n", link, expiresAt.UTC().Format(time.RFC3339))
	return s.send(email, subject, body)
}

func (s *SMTPPasswordResetSender) SendInvitation(email, organizationName, role, token string, expiresAt time.Time) error {
	if err := s.configured(); err != nil {
		return err
	}
	org := strings.TrimSpace(organizationName)
	if org == "" {
		org = "a FlyAiMovie workspace"
	}
	link := s.ResetURLBase + "/invite/" + url.PathEscape(token)
	subject := "FlyAiMovie invitation"
	body := fmt.Sprintf("Hello,\n\nYou have been invited to join %s as %s.\n\nAccept the invitation with this secure link:\n%s\n\nThis link expires at %s UTC and can only be used once.\n\nIf you did not expect this invitation, ignore this email.\n", org, strings.TrimSpace(role), link, expiresAt.UTC().Format(time.RFC3339))
	return s.send(email, subject, body)
}

func (s *SMTPPasswordResetSender) send(email, subject, body string) error {
	msg := "From: " + s.From + "\r\n" + "To: " + email + "\r\n" + "Subject: " + subject + "\r\n" + "MIME-Version: 1.0\r\n" + "Content-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
	address := net.JoinHostPort(s.Host, fmt.Sprintf("%d", s.Port))
	conn, err := s.dial(address)
	if err != nil {
		return err
	}
	// Bound the whole conversation, not just the dial: a peer that accepts the
	// connection and then stalls mid-command would otherwise block forever.
	if err := conn.SetDeadline(time.Now().Add(s.sessionTimeout())); err != nil {
		_ = conn.Close()
		return err
	}
	client, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer client.Close()
	if s.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return err
			}
		}
	}
	if err := client.Auth(auth); err != nil {
		return err
	}
	if err := client.Mail(s.From); err != nil {
		return err
	}
	if err := client.Rcpt(email); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := writer.Write([]byte(msg)); err != nil {
		_ = writer.Close()
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// dial applies the connect timeout for both implicit TLS (465) and plain
// submission ports, which then upgrade through STARTTLS when advertised.
func (s *SMTPPasswordResetSender) dial(address string) (net.Conn, error) {
	dialer := &net.Dialer{Timeout: s.dialTimeout()}
	if s.Port == 465 {
		return tls.DialWithDialer(dialer, "tcp", address, &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12})
	}
	return dialer.Dial("tcp", address)
}
