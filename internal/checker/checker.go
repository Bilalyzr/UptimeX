// Package checker executes health probes against monitored endpoints and
// classifies every outcome into a structured result. A probe failure of any
// kind (timeout, DNS, TLS, refused connection) is a data point, never a
// crash: Check always returns a result.
package checker

import (
	"context"

	"uptimex/internal/models"
)

// Checker probes a single endpoint. Implementations must be safe for
// concurrent use by many workers.
type Checker interface {
	Check(ctx context.Context, ep models.Endpoint) models.CheckResult
}
