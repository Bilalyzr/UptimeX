package alert

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
	"time"

	"uptimex/internal/models"
)

// EmailConfig configures the optional SMTP channel.
type EmailConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	To       string
	Timeout  time.Duration
}

// EmailChannel sends plain-text notifications over SMTP. It is optional;
// configure SMTP_HOST to enable.
type EmailChannel struct {
	cfg EmailConfig
}

// NewEmailChannel builds the channel.
func NewEmailChannel(cfg EmailConfig) *EmailChannel {
	if cfg.Port == 0 {
		cfg.Port = 587
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &EmailChannel{cfg: cfg}
}

func (e *EmailChannel) Name() string { return "email" }

// Send delivers one email. Delivery failure is returned to the manager.
func (e *EmailChannel) Send(ctx context.Context, a models.Alert) error {
	addr := fmt.Sprintf("%s:%d", e.cfg.Host, e.cfg.Port)
	subject := fmt.Sprintf("[uptimex] %s %s", a.Type, a.EndpointName)
	if a.Type == models.AlertMonitorDown {
		subject = "[uptimex] MONITOR DOWN"
	}

	var b strings.Builder
	b.WriteString("From: " + e.cfg.From + "\r\n")
	b.WriteString("To: " + e.cfg.To + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("Date: " + a.DetectedAt.Format(time.RFC1123Z) + "\r\n")
	b.WriteString("\r\n")
	b.WriteString(a.Message + "\r\n\r\n")
	if a.URL != "" {
		fmt.Fprintf(&b, "URL: %s\r\n", a.URL)
	}
	if a.FailureCount > 0 {
		fmt.Fprintf(&b, "Consecutive failures: %d\r\n", a.FailureCount)
	}
	if a.LastStatus != nil {
		fmt.Fprintf(&b, "Last status code: %d\r\n", *a.LastStatus)
	}
	if a.LastError != "" {
		fmt.Fprintf(&b, "Last error: %s\r\n", a.LastError)
	}
	fmt.Fprintf(&b, "Detected at: %s\r\n", a.DetectedAt.UTC().Format(time.RFC3339))

	var auth smtp.Auth
	if e.cfg.Username != "" {
		auth = smtp.PlainAuth("", e.cfg.Username, e.cfg.Password, e.cfg.Host)
	}
	return smtp.SendMail(addr, auth, e.cfg.From, []string{e.cfg.To}, []byte(b.String()))
}
