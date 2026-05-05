package policy

import (
	"testing"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/pubsub"
)

func TestEvaluateMultiProjectBudgetWithSafety(t *testing.T) {
	c, _ := config.Parse([]byte(`defaults:
  dryRun: false
budgets:
- names: [prod-budget]
  projects: [p1, p2]
  threshold: 0.5
maxProjectsDisabledPerEvent: 1
`))
	ev := Evaluate(c, pubsub.BudgetAlert{BudgetDisplayName: "prod-budget", CostAmount: 9, BudgetAmount: 10})
	if len(ev.Decisions) != 2 {
		t.Fatalf("got %d decisions", len(ev.Decisions))
	}
	if !ev.Decisions[0].Disable || ev.Decisions[1].Disable {
		t.Fatalf("unexpected decisions: %+v", ev.Decisions)
	}
}

func TestUnknownDefaultsDryRun(t *testing.T) {
	c, _ := config.Parse([]byte(`budgets: []`))
	ev := Evaluate(c, pubsub.BudgetAlert{BudgetDisplayName: "missing", CostAmount: 1, BudgetAmount: 1})
	if !ev.Unknown || len(ev.Decisions) != 1 || !ev.Decisions[0].DryRun || ev.Decisions[0].Disable {
		t.Fatalf("unexpected unknown eval: %+v", ev)
	}
}

func TestUnknownPolicyCanTargetExplicitFallbackProjects(t *testing.T) {
	c, err := config.Parse([]byte(`defaults:
  dryRun: false
unknownAlertPolicy: disable_billing
unknownAlertProjects: [fallback-test]
budgets: []
`))
	if err != nil {
		t.Fatal(err)
	}
	ev := Evaluate(c, pubsub.BudgetAlert{BudgetDisplayName: "missing", CostAmount: 1, BudgetAmount: 1})
	if !ev.Unknown || len(ev.Decisions) != 1 || ev.Decisions[0].ProjectID != "fallback-test" || !ev.Decisions[0].Disable {
		t.Fatalf("unexpected unknown fallback eval: %+v", ev)
	}
}

func TestActionDryRunAndIgnoreDoNotDisable(t *testing.T) {
	c, _ := config.Parse([]byte(`defaults:
  dryRun: false
budgets:
- names: [prod-budget]
  projects: [p1, p2]
  action: dry_run
projects:
  p2:
    action: ignore
`))
	ev := Evaluate(c, pubsub.BudgetAlert{BudgetDisplayName: "prod-budget", CostAmount: 1, BudgetAmount: 1})
	if len(ev.Decisions) != 2 {
		t.Fatalf("got %d decisions", len(ev.Decisions))
	}
	if !ev.Decisions[0].DryRun || ev.Decisions[0].Disable {
		t.Fatalf("dry_run action should force dry-run without disable: %+v", ev.Decisions[0])
	}
	if ev.Decisions[1].Disable {
		t.Fatalf("ignore action should not disable: %+v", ev.Decisions[1])
	}
}
