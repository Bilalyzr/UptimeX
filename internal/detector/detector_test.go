package detector

import (
	"testing"
	"time"

	"uptimex/internal/models"
)

func st(state models.EndpointState, fails int) models.EndpointStatus {
	return models.EndpointStatus{EndpointID: 1, State: state, ConsecutiveFailures: fails}
}

var now = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func TestSingleFailureDoesNotCreateOutage(t *testing.T) {
	next, tr := Evaluate(st(models.StateHealthy, 0), false, 3, now)
	if next.State != models.StateFailing || next.ConsecutiveFailures != 1 {
		t.Fatalf("1 failure -> %+v", next)
	}
	if tr.WentDown || tr.Recovered {
		t.Fatal("1 failure must not raise or resolve anything")
	}
}

func TestTwoFailuresStillFailing(t *testing.T) {
	next, _ := Evaluate(st(models.StateHealthy, 1), false, 3, now)
	if next.State != models.StateFailing || next.ConsecutiveFailures != 2 {
		t.Fatalf("2 failures -> %+v", next)
	}
}

func TestThirdFailureGoesDown(t *testing.T) {
	next, tr := Evaluate(st(models.StateFailing, 2), false, 3, now)
	if next.State != models.StateDown || next.ConsecutiveFailures != 3 {
		t.Fatalf("3 failures -> %+v", next)
	}
	if !tr.WentDown {
		t.Fatal("threshold crossing must set WentDown")
	}
	if tr.From != models.StateFailing || tr.To != models.StateDown {
		t.Fatalf("transition = %s -> %s", tr.From, tr.To)
	}
}

func TestSuccessResetsCounterBeforeThreshold(t *testing.T) {
	next, tr := Evaluate(st(models.StateFailing, 2), true, 3, now)
	if next.State != models.StateHealthy || next.ConsecutiveFailures != 0 {
		t.Fatalf("failure->success -> %+v", next)
	}
	if tr.WentDown || tr.Recovered {
		t.Fatal("no DOWN was involved; recovery must not fire")
	}
	if next.LastSuccessAt == nil || next.LastCheckedAt == nil {
		t.Fatal("timestamps must be stamped")
	}
}

func TestSuccessAfterFailureThenFailureAgain(t *testing.T) {
	after1, _ := Evaluate(st(models.StateHealthy, 0), false, 3, now)
	afterReset, _ := Evaluate(after1, true, 3, now)
	afterFail, _ := Evaluate(afterReset, false, 3, now)
	if afterFail.State != models.StateFailing || afterFail.ConsecutiveFailures != 1 {
		t.Fatalf("counter must restart from 1 after reset: %+v", afterFail)
	}
}

func TestDownToSuccessEmitsRecovery(t *testing.T) {
	next, tr := Evaluate(st(models.StateDown, 5), true, 3, now)
	if next.State != models.StateHealthy || next.ConsecutiveFailures != 0 {
		t.Fatalf("DOWN->success -> %+v", next)
	}
	if !tr.Recovered || tr.WentDown {
		t.Fatalf("transition = %+v", tr)
	}
	if next.LastSuccessAt == nil {
		t.Fatal("recovery must stamp last_success_at")
	}
}

func TestDownRepeatedFailureStaysDown(t *testing.T) {
	next, tr := Evaluate(st(models.StateDown, 3), false, 3, now)
	if next.State != models.StateDown || next.ConsecutiveFailures != 4 {
		t.Fatalf("DOWN->failure -> %+v", next)
	}
	if tr.WentDown {
		t.Fatal("must not raise a second outage for the same incident")
	}
}

func TestCustomThresholdOne(t *testing.T) {
	next, tr := Evaluate(st(models.StateHealthy, 0), false, 1, now)
	if next.State != models.StateDown || !tr.WentDown {
		t.Fatalf("threshold=1 -> %+v %+v", next, tr)
	}
}

func TestInputNotMutated(t *testing.T) {
	in := st(models.StateFailing, 2)
	_, _ = Evaluate(in, false, 3, now)
	if in.ConsecutiveFailures != 2 || in.State != models.StateFailing || in.LastCheckedAt != nil {
		t.Fatalf("input mutated: %+v", in)
	}
}
