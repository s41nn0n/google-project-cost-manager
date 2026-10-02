package policy

import (
	"github.com/example/google-project-cost-manager/internal/pubsub"
	"testing"
	"time"
)

func TestGooglePacificCalendarPeriod(t *testing.T) {
	now := time.Date(2026, 2, 10, 12, 0, 0, 0, time.UTC)
	if stalePeriod(pubsub.BudgetAlert{CostIntervalStart: "2026-02-01T08:00:00Z"}, now) {
		t.Fatal("Google's Pacific midnight was rejected")
	}
	boundary := time.Date(2026, 2, 1, 3, 0, 0, 0, time.UTC)
	if !stalePeriod(pubsub.BudgetAlert{CostIntervalStart: "2026-02-01T08:00:00Z"}, boundary) {
		t.Fatal("future Pacific month accepted")
	}
	if !stalePeriod(pubsub.BudgetAlert{CostIntervalStart: "2026-01-01T08:00:00Z"}, now) {
		t.Fatal("previous period accepted")
	}
}
