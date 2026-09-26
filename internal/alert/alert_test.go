package alert

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"uptimex/internal/logging"
	"uptimex/internal/models"
)

func TestWebhookDeliveryAndSignature(t *testing.T) {
	var (
		mu       sync.Mutex
		received []models.Alert
		sig      string
		event    string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var a models.Alert
		_ = json.Unmarshal(body, &a)
		mu.Lock()
		received = append(received, a)
		sig = r.Header.Get("X-UptimeX-Signature")
		event = r.Header.Get("X-UptimeX-Event")
		mu.Unlock()
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{URL: srv.URL, Secret: "s3cret", Attempts: 1, Timeout: 2 * time.Second})
	err := ch.Send(context.Background(), models.Alert{Type: models.AlertDown, EndpointName: "Payments", Message: "down"})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 || received[0].EndpointName != "Payments" {
		t.Fatalf("received = %+v", received)
	}
	if sig == "" || len(sig) < 10 {
		t.Fatalf("signature missing: %q", sig)
	}
	if event != "DOWN" {
		t.Fatalf("event header = %q", event)
	}
}

func TestWebhookRetriesThenFails(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{URL: srv.URL, Attempts: 3, Backoff: time.Millisecond})
	err := ch.Send(context.Background(), models.Alert{Type: models.AlertRecovered})
	if err == nil {
		t.Fatal("must fail after retries")
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3 (bounded retries)", calls)
	}
}

func TestWebhookSucceedsOnRetry(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(500)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{URL: srv.URL, Attempts: 3, Backoff: time.Millisecond})
	if err := ch.Send(context.Background(), models.Alert{Type: models.AlertHighLatency}); err != nil {
		t.Fatalf("should succeed on second attempt: %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d", calls)
	}
}

type recordingChannel struct {
	mu   sync.Mutex
	got  []models.Alert
	fail bool
}

func (r *recordingChannel) Name() string { return "recording" }
func (r *recordingChannel) Send(ctx context.Context, a models.Alert) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("channel down")
	}
	r.got = append(r.got, a)
	return nil
}

func TestManagerCooldownDeduplicates(t *testing.T) {
	rec := &recordingChannel{}
	m := NewManager([]Channel{rec}, 50*time.Millisecond, logging.NewForTest())
	ctx := context.Background()

	m.Dispatch(ctx, models.Alert{Type: models.AlertDown, EndpointID: 1})
	m.Dispatch(ctx, models.Alert{Type: models.AlertDown, EndpointID: 1}) // within cooldown -> suppressed
	m.Dispatch(ctx, models.Alert{Type: models.AlertDown, EndpointID: 2}) // different endpoint -> sent

	if len(rec.got) != 2 {
		t.Fatalf("delivered %d alerts, want 2 (dedup per endpoint)", len(rec.got))
	}

	time.Sleep(60 * time.Millisecond)
	m.Dispatch(ctx, models.Alert{Type: models.AlertDown, EndpointID: 1}) // cooldown expired -> sent
	if len(rec.got) != 3 {
		t.Fatalf("after cooldown delivery = %d, want 3", len(rec.got))
	}

	st := m.Stats()
	if st.SuppressedByCooldown != 1 {
		t.Fatalf("suppressed = %d, want 1", st.SuppressedByCooldown)
	}
}

func TestManagerContinuesAfterChannelFailure(t *testing.T) {
	broken := &recordingChannel{fail: true}
	ok := &recordingChannel{}
	m := NewManager([]Channel{broken, ok}, 0, logging.NewForTest())

	m.Dispatch(context.Background(), models.Alert{Type: models.AlertRecovered, EndpointID: 3})
	if len(ok.got) != 1 {
		t.Fatal("healthy channel must still receive alerts")
	}
	if st := m.Stats(); st.FailedByChannel["recording"] != 1 {
		t.Fatalf("failure not counted: %+v", st)
	}
}
