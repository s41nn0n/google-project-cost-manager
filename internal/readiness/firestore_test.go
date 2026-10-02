package readiness

import (
	"context"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/google/uuid"
	"os"
	"strings"
	"testing"
	"time"
)

func TestFirestoreEvidenceCountsDaysNotRuns(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("local Firestore emulator required")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") && !strings.HasPrefix(host, "localhost:") {
		t.Fatal("integration test refuses non-loopback emulator")
	}
	now := time.Now().UTC()
	c := &config.Config{ControlProjectID: "control", ReleaseID: uuid.NewString(), InventoryObservedAt: now, EventState: config.EventStateConfig{Backend: "firestore", ProjectID: "finops-security-test", DatabaseID: "(default)"}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := New(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 7; i++ {
		if err := s.RecordDay(ctx, c, now, true); err != nil {
			t.Fatal(err)
		}
	}
	e, err := s.Read(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Days) != 1 {
		t.Fatalf("repeated runs created %d days", len(e.Days))
	}
	if err := s.RecordDay(ctx, c, now, false); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDay(ctx, c, now, true); err != nil {
		t.Fatal(err)
	}
	e, err = s.Read(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if e.Days[now.Format("2006-01-02")] {
		t.Fatal("same-day success erased failure")
	}
	for i := 0; i < 7; i++ {
		if err := s.RecordDay(ctx, c, now.AddDate(0, 0, -i-1), true); err != nil {
			t.Fatal(err)
		}
	}
	// Use a new release to isolate independent switch behavior from sticky failure.
	c.ReleaseID = uuid.NewString()
	for i := 0; i < 7; i++ {
		if err := s.RecordDay(ctx, c, now.AddDate(0, 0, -i), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordToggle(ctx, c, "disposable", now); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if VerifyEnforcement(ctx, c) == nil {
		t.Fatal("disabled operator stop accepted")
	}
	if err := s.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := VerifyEnforcement(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	c.InventoryObservedAt = now.Add(time.Minute)
	if VerifyEnforcement(ctx, c) == nil {
		t.Fatal("policy refresh undid persistent operator stop")
	}
}
