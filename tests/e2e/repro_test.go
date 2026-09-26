package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestReproBackdateScheduling(t *testing.T) {
	h := newHarness(t, 0)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	ep := h.addEndpoint(t, "repro", srv.URL, 2000, 3)

	pollUntil(t, "first check", 3*time.Second, func() bool {
		return h.engine.Stats().ChecksAttempted >= 1
	})
	time.Sleep(100 * time.Millisecond) // let everything settle

	// Directly backdate, exactly like makeDue but visible.
	st, err := h.repo.GetStatus(context.Background(), ep.ID)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("before backdate: last_checked=%v state=%s consecutive=%d", st.LastCheckedAt, st.State, st.ConsecutiveFailures)
	old := time.Now().UTC().Add(-time.Minute)
	st.LastCheckedAt = &old
	if err := h.repo.UpsertStatus(context.Background(), st); err != nil {
		t.Fatal(err)
	}

	cur, _ := h.repo.GetStatus(context.Background(), ep.ID)
	t.Logf("after backdate: last_checked=%v", cur.LastCheckedAt)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.engine.Stats().ChecksAttempted >= 2 {
			t.Logf("second check fired; stats=%+v", h.engine.Stats())
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	stats := h.engine.Stats()
	t.Fatalf("second check never fired; stats=%+v sched=%+v", stats, stats.Scheduler)
}
