// Package detector implements the consecutive-failure state machine
// (PRD §15):
//
//	HEALTHY --failure--> FAILING(count=1..threshold-1) --failure@threshold--> DOWN
//	any state --success--> HEALTHY (counter reset; DOWN also emits recovery)
//
// A single failure never means an outage; only threshold consecutive
// failures do. Evaluate is a pure function, so every transition is
// unit-testable without mocks.
package detector

import (
	"time"

	"uptimex/internal/models"
)

// Transition describes the outcome of one evaluation.
type Transition struct {
	From                models.EndpointState
	To                  models.EndpointState
	ConsecutiveFailures int
	// WentDown is true when the endpoint crossed the threshold now.
	WentDown bool
	// Recovered is true when a previously-DOWN endpoint succeeded now.
	Recovered bool
}

// Evaluate applies one check outcome to the current status and returns the
// next status plus what changed. The input status is not mutated.
func Evaluate(st models.EndpointStatus, success bool, threshold int, now time.Time) (models.EndpointStatus, Transition) {
	tr := Transition{From: st.State, To: st.State, ConsecutiveFailures: st.ConsecutiveFailures}
	next := st
	next.UpdatedAt = now
	next.LastCheckedAt = &now

	if success {
		next.LastSuccessAt = &now
		next.ConsecutiveFailures = 0
		if st.State == models.StateDown {
			tr.Recovered = true
		}
		next.State = models.StateHealthy
		tr.To = models.StateHealthy
		tr.ConsecutiveFailures = 0
		return next, tr
	}

	next.LastFailureAt = &now
	next.ConsecutiveFailures = st.ConsecutiveFailures + 1
	tr.ConsecutiveFailures = next.ConsecutiveFailures

	if st.State == models.StateDown {
		// Already down: keep counting, no new incident.
		next.State = models.StateDown
		tr.To = models.StateDown
		return next, tr
	}

	if next.ConsecutiveFailures >= threshold {
		next.State = models.StateDown
		tr.To = models.StateDown
		tr.WentDown = true
		return next, tr
	}
	next.State = models.StateFailing
	tr.To = models.StateFailing
	return next, tr
}

// DefaultThreshold is used when an endpoint carries no explicit threshold.
const DefaultThreshold = 3
