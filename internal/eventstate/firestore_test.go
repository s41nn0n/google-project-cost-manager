package eventstate

import (
	"cloud.google.com/go/firestore"
	"context"
	"errors"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFirestoreLeaseRecoveryAndFencing(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("local Firestore emulator required")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") && !strings.HasPrefix(host, "localhost:") {
		t.Fatal("integration test refuses non-loopback emulator")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := NewFirestoreStore(ctx, "finops-security-test", "(default)", "test-events-"+uuid.NewString())
	if err != nil {
		t.Fatal(err)
	}
	defer s.client.Close()
	first := Event{MessageID: "one", PublishTime: time.Now().UTC(), CostAmount: 80}
	if d, err := s.Claim(ctx, "key", &first); err != nil || d != Accepted {
		t.Fatalf("claim %s %v", d, err)
	}
	retry := Event{MessageID: first.MessageID, PublishTime: first.PublishTime, CostAmount: 80}
	if d, err := s.Claim(ctx, "key", &retry); err != nil || d != Busy {
		t.Fatalf("unfinished %s %v", d, err)
	}
	_, err = s.client.Collection(s.collection).Doc("key").Update(ctx, []firestore.Update{{Path: "leaseUntil", Value: time.Now().Add(-time.Minute)}})
	if err != nil {
		t.Fatal(err)
	}
	if d, err := s.Claim(ctx, "key", &retry); err != nil || d != Accepted {
		t.Fatalf("recovery %s %v", d, err)
	}
	if err := s.Complete(ctx, "key", first, true); !errors.Is(err, ErrLostClaim) {
		t.Fatalf("old worker not fenced: %v", err)
	}
	if err := s.Complete(ctx, "key", retry, true); err != nil {
		t.Fatal(err)
	}
	if d, err := s.Claim(ctx, "key", &first); err != nil || d != Duplicate {
		t.Fatalf("completed %s %v", d, err)
	}
}
