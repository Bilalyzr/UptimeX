package alert

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"uptimex/internal/models"
)

// WebhookConfig configures the webhook channel.
type WebhookConfig struct {
	URL      string
	Secret   string // optional; enables HMAC-SHA256 signing
	Timeout  time.Duration
	Attempts int // bounded retries
	Backoff  time.Duration
}

// WebhookChannel POSTs alerts as JSON. When a secret is configured the body
// is signed so receivers can verify authenticity.
type WebhookChannel struct {
	cfg    WebhookConfig
	client *http.Client
}

// NewWebhookChannel builds the channel with its own bounded client.
func NewWebhookChannel(cfg WebhookConfig) *WebhookChannel {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 5 * time.Second
	}
	if cfg.Attempts < 1 {
		cfg.Attempts = 1
	}
	if cfg.Backoff <= 0 {
		cfg.Backoff = time.Second
	}
	return &WebhookChannel{
		cfg:    cfg,
		client: &http.Client{Timeout: cfg.Timeout},
	}
}

func (w *WebhookChannel) Name() string { return "webhook" }

// Send delivers the alert with bounded retries. A 2xx response is success;
// anything else is an error (recorded by the manager, never fatal).
func (w *WebhookChannel) Send(ctx context.Context, alert models.Alert) error {
	body, err := json.Marshal(alert)
	if err != nil {
		return fmt.Errorf("marshal alert: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= w.cfg.Attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.cfg.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "uptimex-webhook/1.0")
		req.Header.Set("X-UptimeX-Event", string(alert.Type))
		if w.cfg.Secret != "" {
			mac := hmac.New(sha256.New, []byte(w.cfg.Secret))
			mac.Write(body)
			req.Header.Set("X-UptimeX-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}

		resp, err := w.client.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4*1024))
			_ = resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			lastErr = fmt.Errorf("webhook returned %s", resp.Status)
		} else {
			lastErr = err
		}

		if attempt < w.cfg.Attempts {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * w.cfg.Backoff):
			}
		}
	}
	return fmt.Errorf("webhook delivery failed after %d attempts: %w", w.cfg.Attempts, lastErr)
}
