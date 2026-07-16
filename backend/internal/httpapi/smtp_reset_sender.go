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

type SMTPPasswordResetSender struct {
	Host         string
	Port         int
	Username     string
	Password     string
	From         string
	ResetURLBase string
}

func NewSMTPPasswordResetSender(cfg config.EmailConfig) *SMTPPasswordResetSender {
	return &SMTPPasswordResetSender{Host: cfg.SMTPHost, Port: cfg.SMTPPort, Username: cfg.SMTPUsername, Password: cfg.SMTPPassword, From: cfg.From, ResetURLBase: strings.TrimRight(cfg.ResetURLBase, "/")}
}

func (s *SMTPPasswordResetSender) SendPasswordReset(email, token string, expiresAt time.Time) error {
	if s == nil || s.Host == "" || s.Port <= 0 || s.Username == "" || s.Password == "" || s.From == "" || !strings.HasPrefix(strings.ToLower(s.ResetURLBase), "https://") {
		return fmt.Errorf("SMTP password reset sender is not configured")
	}
	link := s.ResetURLBase + "/password-reset/" + url.PathEscape(token)
	subject := "FlyAiMovie password reset"
	body := fmt.Sprintf("Hello,\n\nUse this secure link to set a new FlyAiMovie password:\n%s\n\nThis link expires at %s UTC and can only be used once.\n\nIf you did not request this, ignore this email.\n", link, expiresAt.UTC().Format(time.RFC3339))
	msg := "From: " + s.From + "\r\n" + "To: " + email + "\r\n" + "Subject: " + subject + "\r\n" + "MIME-Version: 1.0\r\n" + "Content-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
	address := net.JoinHostPort(s.Host, fmt.Sprintf("%d", s.Port))
	if s.Port == 465 {
		conn, err := tls.Dial("tcp", address, &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12})
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, s.Host)
		if err != nil {
			_ = conn.Close()
			return err
		}
		defer client.Close()
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
	return smtp.SendMail(address, auth, s.From, []string{email}, []byte(msg))
}
