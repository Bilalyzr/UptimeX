package alert

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"

	"uptimex/internal/models"
)

// EmailConfig configures the optional SMTP channel.
type EmailConfig struct {
	Name     string // optional channel label (defaults to "email")
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       string
	Timeout  time.Duration
	Attempts int
	Backoff  time.Duration
}

// EmailChannel sends plain-text notifications over SMTP with strict socket
// deadlines, context cancellation, port-465 implicit TLS / STARTTLS support,
// and bounded retries (PRD §25, §28).
type EmailChannel struct {
	cfg EmailConfig
}

// NewEmailChannel builds the channel.
func NewEmailChannel(cfg EmailConfig) *EmailChannel {
	if cfg.Name == "" {
		cfg.Name = "email"
	}
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Attempts < 1 {
		cfg.Attempts = 1
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = time.Second
	}
	return &EmailChannel{cfg: cfg}
}

func (e *EmailChannel) Name() string { return e.cfg.Name }

// Send delivers one alert email with bounded retries.
func (e *EmailChannel) Send(ctx context.Context, a models.Alert) error {
	subject := fmt.Sprintf("[uptimex] %s %s", a.Type, a.EndpointName)
	if a.Type == models.AlertMonitorDown {
		subject = "[uptimex] MONITOR DOWN"
	}

	detected := a.DetectedAt
	if detected.IsZero() {
		detected = time.Now().UTC()
	}

	var body strings.Builder
	body.WriteString(a.Message + "\r\n\r\n")
	if a.URL != "" {
		fmt.Fprintf(&body, "URL: %s\r\n", a.URL)
	}
	if a.DashboardURL != "" {
		fmt.Fprintf(&body, "Dashboard: %s\r\n", a.DashboardURL)
	}
	if a.FailureCount > 0 {
		fmt.Fprintf(&body, "Consecutive failures: %d\r\n", a.FailureCount)
	}
	if a.LastStatus != nil {
		fmt.Fprintf(&body, "Last status code: %d\r\n", *a.LastStatus)
	}
	if a.LastError != "" {
		fmt.Fprintf(&body, "Last error: %s\r\n", a.LastError)
	}
	fmt.Fprintf(&body, "Detected at: %s\r\n", detected.UTC().Format(time.RFC3339))

	return SendRawEmail(ctx, e.cfg, e.cfg.To, subject, body.String())
}

// SendRawEmail sends a plain-text email over SMTP obeying ctx and cfg.Timeout,
// retrying up to cfg.Attempts times. Reused by password-reset delivery.
func SendRawEmail(ctx context.Context, cfg EmailConfig, to, subject, textBody string) error {
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if cfg.Attempts < 1 {
		cfg.Attempts = 1
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = time.Second
	}

	var msg strings.Builder
	msg.WriteString("From: " + cfg.From + "\r\n")
	msg.WriteString("To: " + to + "\r\n")
	msg.WriteString("Subject: " + subject + "\r\n")
	msg.WriteString("Date: " + time.Now().UTC().Format(time.RFC1123Z) + "\r\n")
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(textBody)
	payload := []byte(msg.String())

	var lastErr error
	for attempt := 1; attempt <= cfg.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := sendSMTPOnce(ctx, cfg, to, payload); err == nil {
			return nil
		} else {
			lastErr = err
		}
		if attempt < cfg.Attempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * cfg.Backoff):
			}
		}
	}
	return fmt.Errorf("email delivery failed after %d attempts: %w", cfg.Attempts, lastErr)
}

func sendSMTPOnce(ctx context.Context, cfg EmailConfig, to string, payload []byte) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	attemptCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	deadline, _ := attemptCtx.Deadline()
	dialer := &net.Dialer{Timeout: cfg.Timeout}

	var (
		conn net.Conn
		err  error
	)
	if cfg.Port == 465 {
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config:    &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12},
		}
		conn, err = tlsDialer.DialContext(attemptCtx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(attemptCtx, "tcp", addr)
	}
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(deadline)

	// Cancel blocked socket I/O immediately if ctx is canceled early.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-attemptCtx.Done():
			_ = conn.SetDeadline(time.Now())
		case <-done:
		}
	}()

	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if cfg.Port != 465 {
		if ok, _ := client.Extension("STARTTLS"); ok {
			tlsCfg := &tls.Config{ServerName: cfg.Host, MinVersion: tls.VersionTLS12}
			if err := client.StartTLS(tlsCfg); err != nil {
				return err
			}
		}
	}
	if cfg.Username != "" {
		if ok, _ := client.Extension("AUTH"); ok {
			if err := client.Auth(smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)); err != nil {
				return err
			}
		}
	}
	if err := client.Mail(cfg.From); err != nil {
		return err
	}
	for _, rcpt := range strings.Split(to, ",") {
		rcpt = strings.TrimSpace(rcpt)
		if rcpt == "" {
			continue
		}
		if err := client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(payload); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
