package readiness

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/reconcile"
	"github.com/google/uuid"
)

func TestSystemQualificationSeparatesInitialValidationFromCurrentHealth(t *testing.T) {
	c := projectConfig()
	now := time.Now().UTC()
	validated := now.Add(-60 * 24 * time.Hour)
	e := goodEvidence(validated)
	q := SystemQualification{SchemaVersion: 1, SystemFingerprint: SystemFingerprint(c), BaselineFingerprint: strings.Repeat("a", 64), BaselineRelease: "original", BaselineObservedAt: validated, ValidationApproval: strings.Repeat("b", 64), ValidatedAt: validated, QualifiedAt: now, Operator: "owner", Reason: "reviewed migration"}
	if err := validateQualification(q, c, e, now); err != nil {
		t.Fatal("genuine completed initial validation was lost:", err)
	}
	e.Days[validated.AddDate(0, 0, -2).Format("2006-01-02")] = false
	if validateQualification(q, c, e, now) == nil {
		t.Fatal("qualification accepted failed initial validation")
	}
}

func TestSystemFingerprintRetainsGlobalControlsAndRelease(t *testing.T) {
	c := projectConfig()
	before := SystemFingerprint(c)
	addPending(c)
	c.ProtectedProjects = append(c.ProtectedProjects, "unrelated")
	c.Reconcile.BillingAccountNames = append(c.Reconcile.BillingAccountNames, "billingAccounts/another")
	c.Budgets[0].MonthlyAmount++
	if SystemFingerprint(c) != before {
		t.Fatal("project onboarding reset the system qualification")
	}
	scoped, _ := Scope(c, "old")
	if SystemFingerprint(scoped) != before {
		t.Fatal("event's scoped policy lost its system qualification")
	}
	for name, mutate := range map[string]func(*config.Config){
		"release":   func(c *config.Config) { c.ReleaseID += "x" },
		"threshold": func(c *config.Config) { c.Defaults.Threshold = .9 },
		"state":     func(c *config.Config) { c.EventState.DatabaseID += "x" },
		"topic":     func(c *config.Config) { c.Reconcile.RequiredPubSubTopic += "x" },
		"unknown":   func(c *config.Config) { c.UnknownAlertPolicy = "disable" },
		"limit":     func(c *config.Config) { c.MaxProjectsDisabledPerEvent++ },
	} {
		t.Run(name, func(t *testing.T) {
			c := projectConfig()
			before := SystemFingerprint(c)
			mutate(c)
			if SystemFingerprint(c) == before {
				t.Fatal("material global change reused qualification")
			}
		})
	}
}

func TestCurrentObservationRecoversWithoutErasingOrReorderingFailures(t *testing.T) {
	now := time.Now().UTC()
	e := Evidence{Days: map[string]bool{}}
	recordDay(&e, now, false)
	recordObservation(&e, now, false)
	if checkCurrentObservation(e, now) == nil {
		t.Fatal("failed project accepted")
	}
	recordDay(&e, now.Add(time.Minute), true)
	recordObservation(&e, now.Add(time.Minute), true)
	if err := checkCurrentObservation(e, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if e.Days[now.Format("2006-01-02")] {
		t.Fatal("recovery erased historical failure")
	}
	recordObservation(&e, now.Add(2*time.Minute), false)
	recordObservation(&e, now.Add(time.Minute), true)
	recordObservation(&e, now.Add(2*time.Minute), true)
	if checkCurrentObservation(e, now.Add(2*time.Minute)) == nil {
		t.Fatal("older or simultaneous success erased a newer failure")
	}
	for _, observation := range []Evidence{
		{}, {LastPassed: true, LastReconciledAt: now.Add(-37 * time.Hour)}, {LastPassed: true, LastReconciledAt: now.Add(time.Hour)},
	} {
		if checkCurrentObservation(observation, now) == nil {
			t.Fatal("missing, stale or future observation accepted")
		}
	}
}

func TestFirestoreQualifiedOnboardingAndRecovery(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("local Firestore emulator required")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") && !strings.HasPrefix(host, "localhost:") {
		t.Fatal("loopback emulator required")
	}
	b, target := transitionConfigs()
	b.ReleaseID, target.ReleaseID = uuid.NewString(), uuid.NewString()
	b.EventState.ProjectID, target.EventState.ProjectID = "finops-security-test", "finops-security-test"
	b.EventState.DatabaseID, target.EventState.DatabaseID = "(default)", "(default)"
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	s, err := New(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	now := time.Now().UTC()
	for i := 0; i < 7; i++ {
		if err = s.RecordDay(ctx, b, now.AddDate(0, 0, -i), true); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.RecordToggle(ctx, b, "disposable", now); err != nil {
		t.Fatal(err)
	}
	if err = s.RecordDay(ctx, target, now, true); err != nil {
		t.Fatal(err)
	}
	if err = s.ApproveTransition(ctx, b, target, "new", "owner", "previous onboarding"); err != nil {
		t.Fatal(err)
	}
	approvalKey := Fingerprint(target)
	if err = s.RecordDay(ctx, target, now, false); err != nil {
		t.Fatal(err)
	}
	target.ReadinessMode = config.ReadinessPerProject
	result := reconcile.Result{Summary: reconcile.Summary{ConfiguredBudgets: 2}}
	if err = s.RecordReconciliation(ctx, target, now, result, nil); err != nil {
		t.Fatal(err)
	}
	if err = s.QualifySystem(ctx, b, target, approvalKey, "owner", "reviewed recovery"); err != nil {
		t.Fatal(err)
	}
	if err = s.QualifySystem(ctx, b, target, approvalKey, "owner", "overwrite"); err == nil {
		t.Fatal("qualification was mutable")
	}
	if err = s.SetEnabled(ctx, true); err != nil {
		t.Fatal(err)
	}
	if err = VerifyEnforcement(ctx, target); err != nil {
		t.Fatal(err)
	}
	// Onboarding a third pending project cannot change either existing scope.
	third := target.Budgets[1]
	third.ProjectID, third.ProjectNumber, third.Projects = "third", "3", []string{"third"}
	third.BudgetResourceName, third.EnforcementMode = "billingAccounts/A/budgets/third", "dry_run"
	target.Budgets = append(target.Budgets, third)
	if err = VerifyEnforcement(ctx, target); err != nil {
		t.Fatal("pending project blocked existing enforcement:", err)
	}
	target.Budgets[2].EnforcementMode = "live"
	if VerifyEnforcement(ctx, target) == nil {
		t.Fatal("new live project bypassed its actual preflight")
	}
	result.Summary.ConfiguredBudgets = 3
	if err = s.RecordReconciliation(ctx, target, now.Add(time.Minute), result, nil); err != nil {
		t.Fatal(err)
	}
	if err = VerifyEnforcement(ctx, target); err != nil {
		t.Fatal(err)
	}
	// A genuine new observation recovers current health without erasing a day.
	result.Summary.Errors = 1
	result.Diffs = []reconcile.Diff{{BudgetName: third.BudgetResourceName, Severity: reconcile.SeverityError}}
	if err = s.RecordReconciliation(ctx, target, now.Add(2*time.Minute), result, nil); err != nil {
		t.Fatal(err)
	}
	if VerifyEnforcement(ctx, target) == nil {
		t.Fatal("failed live project accepted")
	}
	scoped, _ := Scope(target, "old")
	if err = VerifyEnforcement(ctx, scoped); err != nil {
		t.Fatal("unrelated failure poisoned existing event:", err)
	}
	result.Summary.Errors, result.Diffs = 0, nil
	if err = s.RecordReconciliation(ctx, target, now.Add(3*time.Minute), result, nil); err != nil {
		t.Fatal(err)
	}
	if err = VerifyEnforcement(ctx, target); err != nil {
		t.Fatal(err)
	}
	e, err := s.ReadProject(ctx, target, "third")
	if err != nil || e.Days[now.Format("2006-01-02")] {
		t.Fatal("historical failure was lost", err)
	}
	if err = s.SetEnabled(ctx, false); err != nil {
		t.Fatal(err)
	}
	if VerifyEnforcement(ctx, target) == nil {
		t.Fatal("qualification bypassed operator stop")
	}
	target.ReleaseID += "changed"
	if Verify(ctx, target) == nil {
		t.Fatal("unreviewed release reused qualification")
	}
}
