package readiness

import (
	"errors"
	"testing"
	"time"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/reconcile"
)

func projectConfig() *config.Config {
	return &config.Config{
		SchemaVersion: 2, ReadinessMode: config.ReadinessPerProject,
		ControlProjectID: "control", ReleaseID: "release@sha256:one", InventoryObservedAt: time.Now().UTC(),
		EnforcementEnabled: true, Defaults: config.Defaults{Threshold: .8, DryRun: config.Bool(false), Action: "disable_billing"},
		ProtectedProjects: []string{"control"}, Projects: map[string]config.ProjectPolicy{},
		Reconcile:  config.ReconcileConfig{Enabled: true, BillingAccountNames: []string{"billingAccounts/A"}, RequiredPubSubTopic: "projects/control/topics/t"},
		EventState: config.EventStateConfig{Backend: "firestore", ProjectID: "control", DatabaseID: "events"},
		Budgets:    []config.Budget{{BudgetResourceName: "billingAccounts/A/budgets/old", BillingAccountName: "billingAccounts/A", ProjectID: "old", ProjectNumber: "1", Projects: []string{"old"}, EnforcementMode: "live", MonthlyAmount: 20, CurrencyCode: "USD", Threshold: .8}},
	}
}

func addPending(c *config.Config) {
	c.Budgets = append(c.Budgets, config.Budget{BudgetResourceName: "billingAccounts/A/budgets/new", BillingAccountName: "billingAccounts/A", ProjectID: "new", ProjectNumber: "2", Projects: []string{"new"}, EnforcementMode: "dry_run", MonthlyAmount: 5, CurrencyCode: "USD", Threshold: .8})
}

func goodEvidence(now time.Time) Evidence {
	e := Evidence{Days: map[string]bool{}, ToggleProject: "disposable", ToggleAt: now}
	for i := 0; i < 7; i++ {
		e.Days[now.AddDate(0, 0, -i).Format("2006-01-02")] = true
	}
	return e
}

func TestProjectFingerprintIgnoresUnrelatedOnboarding(t *testing.T) {
	c := projectConfig()
	old, _ := ProjectFingerprint(c, "old")
	addPending(c)
	c.Reconcile.BillingAccountNames = append(c.Reconcile.BillingAccountNames, "billingAccounts/other")
	c.ProtectedProjects = append(c.ProtectedProjects, "unrelated")
	c.Projects["new"] = config.ProjectPolicy{Action: "ignore"}
	c.InventoryObservedAt = c.InventoryObservedAt.Add(time.Minute)
	now, _ := ProjectFingerprint(c, "old")
	if now != old {
		t.Fatal("unrelated onboarding invalidated existing evidence")
	}
	c.Budgets[1].MonthlyAmount = 99
	now, _ = ProjectFingerprint(c, "old")
	if now != old {
		t.Fatal("another project's terms invalidated existing evidence")
	}
	c.Budgets[0].EnforcementMode = "dry_run"
	c.Defaults.DryRun = config.Bool(true)
	c.EnforcementEnabled = false
	now, _ = ProjectFingerprint(c, "old")
	if now != old {
		t.Fatal("dry-run/live promotion changed evidence identity")
	}
}

func TestProjectFingerprintRejectsMaterialTargetChanges(t *testing.T) {
	for name, change := range map[string]func(*config.Config){
		"amount":     func(c *config.Config) { c.Budgets[0].MonthlyAmount++ },
		"currency":   func(c *config.Config) { c.Budgets[0].CurrencyCode = "EUR" },
		"budget":     func(c *config.Config) { c.Budgets[0].BudgetResourceName += "-changed" },
		"number":     func(c *config.Config) { c.Budgets[0].ProjectNumber = "99" },
		"account":    func(c *config.Config) { c.Budgets[0].BillingAccountName = "billingAccounts/B" },
		"release":    func(c *config.Config) { c.ReleaseID = "another-release" },
		"topic":      func(c *config.Config) { c.Reconcile.RequiredPubSubTopic = "projects/control/topics/other" },
		"state":      func(c *config.Config) { c.EventState.DatabaseID = "other" },
		"protection": func(c *config.Config) { c.ProtectedProjects = append(c.ProtectedProjects, "old") },
		"threshold":  func(c *config.Config) { c.Budgets[0].Threshold = .9 },
	} {
		t.Run(name, func(t *testing.T) {
			c := projectConfig()
			before, _ := ProjectFingerprint(c, "old")
			change(c)
			after, _ := ProjectFingerprint(c, "old")
			if before == after {
				t.Fatal("material change reused evidence")
			}
		})
	}
	c := projectConfig()
	if _, err := ProjectFingerprint(c, "control"); err == nil {
		t.Fatal("control project accepted")
	}
	if _, err := ProjectFingerprint(c, "unknown"); err == nil {
		t.Fatal("unknown project accepted")
	}
}

func TestPendingProjectCannotBlockOrBypassLiveGate(t *testing.T) {
	c := projectConfig()
	addPending(c)
	now := time.Now().UTC()
	reads := map[string]int{}
	read := func(project string) (Evidence, error) {
		reads[project]++
		if project == "old" {
			return goodEvidence(now), nil
		}
		return Evidence{}, nil
	}
	if err := checkProjects(c, now, read); err != nil {
		t.Fatal(err)
	}
	if reads["new"] != 0 {
		t.Fatal("pending project blocked live publication")
	}
	c.Budgets[1].EnforcementMode = "live"
	if checkProjects(c, now, read) == nil {
		t.Fatal("new live project bypassed seven-day/toggle gate")
	}
	c.Budgets[1].EnforcementMode = "dry_run"
	if checkProjects(c, now, func(string) (Evidence, error) { return Evidence{}, errors.New("state unavailable") }) == nil {
		t.Fatal("unavailable evidence accepted")
	}
	c.InventoryObservedAt = now.Add(-49 * time.Hour)
	if checkProjects(c, now, read) == nil {
		t.Fatal("stale inventory accepted")
	}
}

func TestScopedReconciliationFailuresAndStickyDays(t *testing.T) {
	c := projectConfig()
	addPending(c)
	now := time.Now().UTC()
	r := reconcile.Result{Summary: reconcile.Summary{ConfiguredBudgets: 2, Errors: 1}, Diffs: []reconcile.Diff{{BudgetName: c.Budgets[1].BudgetResourceName, Severity: reconcile.SeverityError}}}
	got := ProjectResults(c, r, nil, now)
	if !got["old"] || got["new"] {
		t.Fatalf("failure was not isolated: %v", got)
	}
	e := Evidence{Days: map[string]bool{}}
	recordDay(&e, now, false)
	recordDay(&e, now, true)
	if e.Days[now.Format("2006-01-02")] {
		t.Fatal("same-day failure erased")
	}
	r.Diffs[0].BudgetName = "unknown"
	for _, ok := range ProjectResults(c, r, nil, now) {
		if ok {
			t.Fatal("unattributed failure accepted")
		}
	}
	r.Diffs = nil
	for _, ok := range ProjectResults(c, r, nil, now) {
		if ok {
			t.Fatal("incomplete error report accepted")
		}
	}
	r.Summary.Errors = 0
	r.Summary.ConfiguredBudgets = 1
	for _, ok := range ProjectResults(c, r, nil, now) {
		if ok {
			t.Fatal("incomplete coverage accepted")
		}
	}
	r.Summary.ConfiguredBudgets = 2
	for _, ok := range ProjectResults(c, r, errors.New("list denied"), now) {
		if ok {
			t.Fatal("failed scan accepted")
		}
	}
}
