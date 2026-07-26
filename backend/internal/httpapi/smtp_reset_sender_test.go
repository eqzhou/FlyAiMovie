package httpapi

import (
	"bufio"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eqzhou/flyaimovie/internal/config"
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
	tlsSender := &SMTPPasswordResetSender{Host: "localhost", Port: 465, Username: "u", Password: "p", From: "noreply@example.com", ResetURLBase: "https://app.example.com"}
	if err := tlsSender.SendPasswordReset("user@example.com", "token", time.Now()); err == nil {
		t.Fatal("expected unavailable implicit TLS SMTP server to fail")
	}
}

func TestSMTPPasswordResetSenderDeliversThroughLocalServer(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	messages := make(chan string, 1)
	errors := make(chan error, 1)
	go serveTestSMTP(listener, messages, errors)
	port := listener.Addr().(*net.TCPAddr).Port
	sender := NewSMTPPasswordResetSender(config.EmailConfig{
		SMTPHost: "localhost", SMTPPort: port, SMTPUsername: "user", SMTPPassword: "password",
		From: "noreply@example.com", ResetURLBase: "https://app.example.com/",
	})
	if sender.ResetURLBase != "https://app.example.com" {
		t.Fatalf("reset URL=%q", sender.ResetURLBase)
	}
	if err := sender.SendPasswordReset("person@example.com", "token with spaces", time.Unix(1_700_000_000, 0)); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-errors:
		t.Fatal(err)
	case message := <-messages:
		if !strings.Contains(message, "To: person@example.com") || !strings.Contains(message, "https://app.example.com/password-reset/token%20with%20spaces") {
			t.Fatalf("message=%q", message)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("SMTP server did not receive message")
	}
	if err := (NoopPasswordResetSender{}).SendPasswordReset("x", "y", time.Now()); err == nil {
		t.Fatal("noop sender unexpectedly succeeded")
	}
}

// A peer that accepts the connection and then goes silent must not pin the
// caller: without a session deadline the SMTP client would block forever
// waiting for the greeting.
func TestSMTPSenderFailsFastWhenServerStalls(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		// Never send the 220 greeting; hold the connection open.
		accepted <- connection
	}()

	sender := &SMTPPasswordResetSender{
		Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port,
		Username: "user", Password: "password", From: "noreply@example.com",
		ResetURLBase:   "https://app.example.com",
		SessionTimeout: 250 * time.Millisecond,
	}

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- sender.SendPasswordReset("person@example.com", "token", time.Unix(1_700_000_000, 0)) }()

	select {
	case sendErr := <-done:
		if sendErr == nil {
			t.Fatal("expected a stalled SMTP server to fail the send")
		}
		if elapsed := time.Since(start); elapsed > 5*time.Second {
			t.Fatalf("send took %s, expected it to abort near the session timeout", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("send blocked on a stalled SMTP server")
	}
	select {
	case connection := <-accepted:
		_ = connection.Close()
	default:
	}
}

// An unroutable address must be bounded by the dial timeout rather than the
// operating system's much longer default.
func TestSMTPSenderAppliesDialTimeout(t *testing.T) {
	sender := &SMTPPasswordResetSender{
		// TEST-NET-1 is reserved and discards traffic, so the dial hangs.
		Host: "192.0.2.1", Port: 587,
		Username: "user", Password: "password", From: "noreply@example.com",
		ResetURLBase: "https://app.example.com",
		DialTimeout:  250 * time.Millisecond,
	}
	done := make(chan error, 1)
	go func() { done <- sender.SendPasswordReset("person@example.com", "token", time.Unix(1_700_000_000, 0)) }()
	select {
	case sendErr := <-done:
		if sendErr == nil {
			t.Fatal("expected an unreachable SMTP host to fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("send blocked past the configured dial timeout")
	}
}

func serveTestSMTP(listener net.Listener, messages chan<- string, errors chan<- error) {
	connection, err := listener.Accept()
	if err != nil {
		errors <- err
		return
	}
	defer connection.Close()
	reader := bufio.NewReader(connection)
	writer := bufio.NewWriter(connection)
	write := func(line string) bool {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			errors <- err
			return false
		}
		if err := writer.Flush(); err != nil {
			errors <- err
			return false
		}
		return true
	}
	if !write("220 localhost test SMTP") {
		return
	}
	var data strings.Builder
	inData := false
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			errors <- err
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if inData {
			if line == "." {
				inData = false
				messages <- data.String()
				if !write("250 queued") {
					return
				}
				continue
			}
			data.WriteString(line)
			data.WriteByte('\n')
			continue
		}
		command := strings.ToUpper(strings.Fields(line)[0])
		switch command {
		case "EHLO", "HELO":
			if _, err := writer.WriteString("250-localhost\r\n250 AUTH PLAIN\r\n"); err != nil {
				errors <- err
				return
			}
			_ = writer.Flush()
		case "AUTH":
			if !write("235 authenticated") {
				return
			}
		case "MAIL", "RCPT":
			if !write("250 ok") {
				return
			}
		case "DATA":
			inData = true
			if !write("354 end with dot") {
				return
			}
		case "QUIT":
			_ = write("221 bye")
			return
		default:
			errors <- &net.AddrError{Err: "unexpected SMTP command " + strconv.Quote(line), Addr: listener.Addr().String()}
			return
		}
	}
}
