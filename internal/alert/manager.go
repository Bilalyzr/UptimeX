package alert

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"uptimex/internal/models"
)

// Manager fans alerts out to all channels with per-(endpoint, type)
// cooldown deduplication. Delivery problems are logged and counted; they
// never block or crash the monitoring pipeline.
type Manager struct {
	channels []Channel
	cooldown time.Duration
	logger   *slog.Logger

	mu         sync.Mutex
	lastSent   map[string]time.Time
	sentCount  map[string]int64
	failCount  map[string]int64
	suppressed int64
}

// NewManager builds the manager. cooldown <= 0 disables dedup.
func NewManager(channels []Channel, cooldown time.Duration, logger *slog.Logger) *Manager {
	return &Manager{
		channels:  channels,
		cooldown:  cooldown,
		logger:    logger,
		lastSent:  map[string]time.Time{},
		sentCount: map[string]int64{},
		failCount: map[string]int64{},
	}
}

func key(a models.Alert) string {
	// MONITOR_DOWN is global (no endpoint); endpoint alerts key per endpoint.
	if a.EndpointID == 0 {
		return string(a.Type)
	}
	return fmt.Sprintf("%s:%d", a.Type, a.EndpointID)
}

// Dispatch delivers an alert unless an identical alert fired within the
// cooldown window. RECOVERED alerts also reset the matching DOWN cooldown key
// so a fresh outage right after recovery is never suppressed.
func (m *Manager) Dispatch(ctx context.Context, alert models.Alert) {
	m.DispatchWithExtra(ctx, alert, nil)
}

// DispatchWithExtra fans an alert out to the manager's global channels plus
// any tenant-specific channels, applying unified per-(endpoint, type) cooldown.
func (m *Manager) DispatchWithExtra(ctx context.Context, alert models.Alert, extra []Channel) {
	total := len(m.channels) + len(extra)
	if total == 0 {
		return
	}
	k := key(alert)

	m.mu.Lock()
	if m.cooldown > 0 {
		if last, ok := m.lastSent[k]; ok && time.Since(last) < m.cooldown {
			m.suppressed++
			m.mu.Unlock()
			m.logger.Debug("alert_suppressed_cooldown", "key", k)
			return
		}
	}
	m.lastSent[k] = time.Now()
	if alert.Type == models.AlertRecovered && alert.EndpointID != 0 {
		delete(m.lastSent, fmt.Sprintf("%s:%d", models.AlertDown, alert.EndpointID))
	}
	m.mu.Unlock()

	alert.DetectedAt = time.Now().UTC()
	all := make([]Channel, 0, total)
	all = append(all, m.channels...)
	all = append(all, extra...)

	for _, ch := range all {
		if err := ch.Send(ctx, alert); err != nil {
			m.mu.Lock()
			m.failCount[ch.Name()]++
			m.mu.Unlock()
			m.logger.Error("alert_delivery_failed", "channel", ch.Name(), "type", alert.Type, "error", err)
			continue
		}
		m.mu.Lock()
		m.sentCount[ch.Name()]++
		m.mu.Unlock()
		m.logger.Info("alert_delivered", "channel", ch.Name(), "type", alert.Type,
			"endpoint_id", alert.EndpointID, "endpoint", alert.EndpointName)
	}
}

// Stats reports delivery counters for observability.
type Stats struct {
	SentByChannel        map[string]int64 `json:"sent_by_channel"`
	FailedByChannel      map[string]int64 `json:"failed_by_channel"`
	SuppressedByCooldown int64            `json:"suppressed_by_cooldown"`
}

// Stats snapshots the counters.
func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Stats{
		SentByChannel:        mapsCopy(m.sentCount),
		FailedByChannel:      mapsCopy(m.failCount),
		SuppressedByCooldown: m.suppressed,
	}
}

func mapsCopy(in map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
