package eventstate

import (
	"context"
	"testing"
	"time"
)

func TestNewerAboveThresholdAfterFailedHigherCost(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	store := &MemoryStore{}
	first := Event{MessageID: "failed-20-dollar-spend", PublishTime: now, CostAmount: 20}
	disposition, err := store.Claim(ctx, "20-dollar-budget-80-percent-threshold", &first)
	if err != nil || disposition != Accepted {
		t.Fatal(disposition, err)
	}
	if err = store.Complete(ctx, "20-dollar-budget-80-percent-threshold", first, false); err != nil {
		t.Fatal(err)
	}
	next := Event{MessageID: "newer-18-dollar-spend", PublishTime: now.Add(time.Minute), CostAmount: 18}
	disposition, err = store.Claim(ctx, "20-dollar-budget-80-percent-threshold", &next)
	if err != nil {
		t.Fatal(err)
	}
	if disposition != Accepted {
		t.Fatalf("newer $18 event, still above $16 trigger after failed $20 attempt: got %s; receiver will acknowledge without retrying disable", disposition)
	}
}

func TestFailedAndExpiredClaimsPreserveLeaseAndPublicationOrdering(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "expired", true: "failed"}[failed], func(t *testing.T) {
			ctx := context.Background()
			now := time.Now().UTC()
			store := &MemoryStore{now: func() time.Time { return now }}
			first := Event{MessageID: "first", PublishTime: now, CostAmount: 20}
			if state, err := store.Claim(ctx, "key", &first); err != nil || state != Accepted {
				t.Fatal(state, err)
			}
			if failed {
				if err := store.Complete(ctx, "key", first, false); err != nil {
					t.Fatal(err)
				}
			} else {
				busy := Event{MessageID: "new", PublishTime: now.Add(time.Minute), CostAmount: 18}
				if state, err := store.Claim(ctx, "key", &busy); err != nil || state != Busy {
					t.Fatal(state, err)
				}
				now = now.Add(LeaseDuration + time.Second)
			}
			older := Event{MessageID: "older", PublishTime: first.PublishTime.Add(-time.Second), CostAmount: 30}
			if state, err := store.Claim(ctx, "key", &older); err != nil || state != OutOfOrder {
				t.Fatal(state, err)
			}
			next := Event{MessageID: "next", PublishTime: now.Add(time.Minute), CostAmount: 18}
			if state, err := store.Claim(ctx, "key", &next); err != nil || state != Accepted {
				t.Fatal(state, err)
			}
			if err := store.Complete(ctx, "key", next, true); err != nil {
				t.Fatal(err)
			}
			if state, err := store.Claim(ctx, "key", &next); err != nil || state != Duplicate {
				t.Fatal(state, err)
			}
			correction := Event{MessageID: "after-success", PublishTime: next.PublishTime.Add(time.Minute), CostAmount: 17}
			if state, err := store.Claim(ctx, "key", &correction); err != nil || state != OutOfOrder {
				t.Fatal(state, err)
			}
		})
	}
}
