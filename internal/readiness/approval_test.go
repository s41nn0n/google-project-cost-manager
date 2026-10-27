package readiness

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/google/uuid"
)

func transitionConfigs() (*config.Config, *config.Config) {
	baseline := projectConfig()
	baseline.ReadinessMode = ""
	target := *baseline
	target.Budgets = append([]config.Budget(nil), baseline.Budgets...)
	addPending(&target)
	target.Budgets[1].EnforcementMode = "live"
	target.ReleaseID = "reviewed-new-release"
	return baseline, &target
}

func TestTransitionAllowsOnlyOneExplicitProjectAndRelease(t *testing.T) {
	b, c := transitionConfigs()
	if err := ValidateTransition(b, c, "new"); err != nil {
		t.Fatal(err)
	}
	for name, change := range map[string]func(*config.Config){
		"existing amount":  func(c *config.Config) { c.Budgets[0].MonthlyAmount++ },
		"existing account": func(c *config.Config) { c.Budgets[0].BillingAccountName += "x" },
		"existing mode":    func(c *config.Config) { c.Budgets[0].EnforcementMode = "dry_run" },
		"topic":            func(c *config.Config) { c.Reconcile.RequiredPubSubTopic += "x" },
		"protection":       func(c *config.Config) { c.ProtectedProjects = append(c.ProtectedProjects, "other") },
		"database":         func(c *config.Config) { c.EventState.DatabaseID += "x" },
		"scope":            func(c *config.Config) { c.ReadinessMode = config.ReadinessPerProject },
		"second addition":  func(c *config.Config) { c.Budgets = append(c.Budgets, c.Budgets[1]) },
		"control":          func(c *config.Config) { c.ControlProjectID = "new" },
		"disabled":         func(c *config.Config) { c.EnforcementEnabled = false },
	} {
		t.Run(name, func(t *testing.T) {
			b, c := transitionConfigs()
			change(c)
			if ValidateTransition(b, c, "new") == nil {
				t.Fatal("unsafe transition accepted")
			}
		})
	}
	if ValidateTransition(b, c, "unrelated") == nil {
		t.Fatal("unrelated project accepted")
	}
}

func TestTransitionApprovalRetainsFreshnessFailureAndOriginalTestExpiry(t *testing.T) {
	for _, mutate := range []string{"none", "target", "missing day", "failed day", "stale", "expired", "future", "empty operator", "empty reason", "bad baseline", "baseline failed", "test expired", "extend test"} {
		t.Run(mutate, func(t *testing.T) {
			b, c := transitionConfigs()
			now := time.Now().UTC()
			before := goodEvidence(now)
			before.ToggleAt = now.Add(-24 * time.Hour)
			current := Evidence{Days: map[string]bool{now.Format("2006-01-02"): true}}
			a := TransitionApproval{SchemaVersion: 1, TargetFingerprint: Fingerprint(c), BaselineFingerprint: Fingerprint(b), BaselineRelease: b.ReleaseID, BaselineObservedAt: b.InventoryObservedAt, ProjectID: "new", Operator: "operator", Reason: "reviewed existing validation", ApprovedAt: now, ExpiresAt: before.ToggleAt.Add(30 * 24 * time.Hour)}
			switch mutate {
			case "target":
				c.Budgets[1].MonthlyAmount++
			case "missing day":
				current.Days = map[string]bool{}
			case "failed day":
				current.Days[now.Format("2006-01-02")] = false
			case "stale":
				c.InventoryObservedAt = now.Add(-49 * time.Hour)
			case "expired":
				a.ExpiresAt = now
			case "future":
				a.ApprovedAt = now.Add(time.Hour)
			case "empty operator":
				a.Operator = " "
			case "empty reason":
				a.Reason = " "
			case "bad baseline":
				a.BaselineFingerprint = "../../invalid"
			case "baseline failed":
				before.Days[now.AddDate(0, 0, -2).Format("2006-01-02")] = false
			case "test expired":
				before.ToggleAt = now.Add(-31 * 24 * time.Hour)
			case "extend test":
				a.ExpiresAt = before.ToggleAt.Add(30*24*time.Hour + time.Minute)
			}
			err := checkTransitionApproval(a, c, current, before, now)
			if (err == nil) != (mutate == "none") {
				t.Fatalf("%s: %v", mutate, err)
			}
		})
	}
}

func TestFirestoreTransitionApprovalDoesNotForgeEvidenceOrOverrideStop(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("local Firestore emulator required")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") && !strings.HasPrefix(host, "localhost:") {
		t.Fatal("loopback emulator required")
	}
	b, c := transitionConfigs()
	b.ReleaseID = uuid.NewString()
	c.ReleaseID = uuid.NewString()
	b.EventState.ProjectID, c.EventState.ProjectID = "finops-security-test", "finops-security-test"
	b.EventState.DatabaseID, c.EventState.DatabaseID = "(default)", "(default)"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	s, err := New(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		if err := s.RecordDay(ctx, b, now.AddDate(0, 0, -i), true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordToggle(ctx, b, "disposable", now); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDay(ctx, c, now, true); err != nil {
		t.Fatal(err)
	}
	if Verify(ctx, c) == nil {
		t.Fatal("unapproved transition accepted")
	}
	if err := s.ApproveTransition(ctx, b, c, "new", "operator", "explicit acceptance of existing validation"); err != nil {
		t.Fatal(err)
	}
	if err := Verify(ctx, c); err != nil {
		t.Fatal(err)
	}
	e, err := s.Read(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Days) != 1 || !e.ToggleAt.IsZero() || e.ToggleProject != "" {
		t.Fatal("approval forged evidence", e)
	}
	if s.ApproveTransition(ctx, b, c, "new", "operator", "renew") == nil {
		t.Fatal("approval overwritten")
	}
	if err := s.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if VerifyEnforcement(ctx, c) == nil {
		t.Fatal("operator stop bypassed")
	}
	if err := s.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err := VerifyEnforcement(ctx, c); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDay(ctx, c, now, false); err != nil {
		t.Fatal(err)
	}
	if Verify(ctx, c) == nil {
		t.Fatal("approval bypassed failed day")
	}
	if err := s.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
}
