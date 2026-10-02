package eventstate

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCrashLeaseRecoveryAndFencing(t *testing.T) {
	now := time.Now().UTC()
	store := &MemoryStore{now: func() time.Time { return now }}
	ctx := context.Background()
	first := Event{MessageID: "one", PublishTime: now, CostAmount: 80}
	if d, e := store.Claim(ctx, "key", &first); e != nil || d != Accepted {
		t.Fatalf("%s %v", d, e)
	}
	retry := Event{MessageID: first.MessageID, PublishTime: first.PublishTime, CostAmount: 80}
	if d, _ := store.Claim(ctx, "key", &retry); d != Busy {
		t.Fatal("unfinished work must not be acknowledged", d)
	}
	now = now.Add(LeaseDuration + time.Second)
	if d, e := store.Claim(ctx, "key", &retry); e != nil || d != Accepted {
		t.Fatalf("expired claim: %s %v", d, e)
	}
	if err := store.Complete(ctx, "key", first, true); !errors.Is(err, ErrLostClaim) {
		t.Fatal("old worker was not fenced", err)
	}
	if err := store.Complete(ctx, "key", retry, true); err != nil {
		t.Fatal(err)
	}
	if d, _ := store.Claim(ctx, "key", &first); d != Duplicate {
		t.Fatal(d)
	}
}
