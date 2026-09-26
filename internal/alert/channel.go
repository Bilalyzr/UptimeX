// Package alert delivers incident notifications. Channels implement a small
// interface; the manager applies per-endpoint deduplication and cooldowns so
// a flapping endpoint cannot spam receivers (PRD §25: alert fatigue).
package alert

import (
	"context"

	"uptimex/internal/models"
)

// Channel is one delivery destination (webhook, email, ...).
type Channel interface {
	Name() string
	Send(ctx context.Context, alert models.Alert) error
}
