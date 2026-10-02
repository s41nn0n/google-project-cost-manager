package eventstate

import (
	"context"
	"testing"
	"time"
)

func TestMemoryStoreDuplicateAndOutOfOrder(t *testing.T) {
	store := &MemoryStore{}
	now := time.Now().UTC()
	first := Event{MessageID: "one", PublishTime: now, CostAmount: 80}
	if got, err := store.Claim(context.Background(), "key", &first); err != nil || got != Accepted {
		t.Fatalf("first claim = %q, %v", got, err)
	}
	if err := store.Complete(context.Background(), "key", first, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Claim(context.Background(), "key", &first); got != Duplicate {
		t.Fatalf("duplicate claim = %q", got)
	}
	older := Event{MessageID: "older", PublishTime: now.Add(-time.Minute), CostAmount: 70}
	if got, _ := store.Claim(context.Background(), "key", &older); got != OutOfOrder {
		t.Fatalf("older claim = %q", got)
	}
}

func TestFailedClaimCanRetry(t *testing.T) {
	store := &MemoryStore{}
	event := Event{MessageID: "one", PublishTime: time.Now().UTC(), CostAmount: 80}
	_, _ = store.Claim(context.Background(), "key", &event)
	_ = store.Complete(context.Background(), "key", event, false)
	if got, err := store.Claim(context.Background(), "key", &event); err != nil || got != Accepted {
		t.Fatalf("retry claim = %q, %v", got, err)
	}
}
