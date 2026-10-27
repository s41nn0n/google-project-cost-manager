package readiness

import (
	"context"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/reconcile"
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

func TestFirestoreProjectOnboardingDoesNotResetExistingEvidence(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("local Firestore emulator required")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") && !strings.HasPrefix(host, "localhost:") {
		t.Fatal("integration test refuses non-loopback emulator")
	}
	c := projectConfig()
	c.ReadinessMode = "" // Collect genuine scoped observations alongside legacy evidence.
	c.ReleaseID = uuid.NewString()
	c.EventState.ProjectID = "finops-security-test"
	c.EventState.DatabaseID = "(default)"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := New(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		day := now.AddDate(0, 0, -i)
		c.InventoryObservedAt = day
		if err = s.RecordReconciliation(ctx, c, day, reconcile.Result{Summary: reconcile.Summary{ConfiguredBudgets: 1}}, nil); err != nil {
			t.Fatal(err)
		}
	}
	c.InventoryObservedAt = now
	if err = s.RecordToggle(ctx, c, "disposable", now); err != nil {
		t.Fatal(err)
	}
	if err = Verify(ctx, c); err != nil {
		t.Fatal(err)
	}
	c.ReadinessMode = config.ReadinessPerProject
	if err = Verify(ctx, c); err != nil {
		t.Fatal("same-release migration lost actual scoped observations:", err)
	}
	addPending(c)
	before, err := s.ReadProject(ctx, c, "old")
	if err != nil {
		t.Fatal(err)
	}
	newEvidence, err := s.ReadProject(ctx, c, "new")
	if err != nil {
		t.Fatal(err)
	}
	if len(newEvidence.Days) != 0 || !newEvidence.ToggleAt.IsZero() {
		t.Fatal("new project inherited evidence")
	}
	result := reconcile.Result{Summary: reconcile.Summary{ConfiguredBudgets: 2, Errors: 1}, Diffs: []reconcile.Diff{{Severity: reconcile.SeverityError, BudgetName: c.Budgets[1].BudgetResourceName}}}
	if err = s.RecordReconciliation(ctx, c, now, result, nil); err != nil {
		t.Fatal(err)
	}
	if err = Verify(ctx, c); err != nil {
		t.Fatal("pending project's failure blocked existing live project:", err)
	}
	after, err := s.ReadProject(ctx, c, "old")
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Days) != len(after.Days) || !after.Days[now.Format("2006-01-02")] {
		t.Fatal("existing evidence changed")
	}
	c.Budgets[1].EnforcementMode = "live"
	if Verify(ctx, c) == nil {
		t.Fatal("pending project promoted without evidence")
	}
	c.Budgets[1].EnforcementMode = "dry_run"
	if err = s.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	scoped, err := Scope(c, "old")
	if err != nil {
		t.Fatal(err)
	}
	if VerifyEnforcement(ctx, scoped) == nil {
		t.Fatal("operator stop bypassed")
	}
	if err = s.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err = VerifyEnforcement(ctx, scoped); err != nil {
		t.Fatal(err)
	}
	if err = s.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
}
