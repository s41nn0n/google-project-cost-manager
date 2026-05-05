package reconcile

import (
	"context"
	"errors"
	"testing"

	"github.com/example/google-project-cost-manager/internal/config"
)

type fakeLister []Budget

func (f fakeLister) ListBudgets(context.Context, string) ([]Budget, error) { return []Budget(f), nil }

type fakeResolver struct{ err error }

func (f fakeResolver) ResolveProjectID(_ context.Context, p string) (string, error) {
	if f.err != nil {
		return "", f.err
	}
	if p == "projects/123" {
		return "p1", nil
	}
	return p, nil
}

func reconcileCfg(t *testing.T, yaml string) *config.Config {
	t.Helper()
	c, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRunNoDiff(t *testing.T) {
	c := reconcileCfg(t, `reconcile: {enabled: true, billingAccountName: billingAccounts/123, requiredPubSubTopic: projects/core/topics/t}
budgets:
- names: [prod]
  projects: [p1]
  threshold: 0.8
`)
	r, err := Run(context.Background(), c, fakeLister{{Name: "billingAccounts/123/budgets/prod", DisplayName: "prod", Projects: []string{"projects/123"}, Thresholds: []float64{0.8}, PubSubTopic: "projects/core/topics/t"}}, fakeResolver{})
	if err != nil || len(r.Diffs) != 0 || r.Summary.GCPBudgets != 1 || r.Summary.ConfiguredBudgets != 1 {
		t.Fatalf("unexpected result err=%v result=%+v", err, r)
	}
}

func TestRunDiffTypes(t *testing.T) {
	c := reconcileCfg(t, `reconcile: {enabled: true, billingAccountName: billingAccounts/123, requiredPubSubTopic: projects/core/topics/t}
budgets:
- names: [configured-missing]
  projects: [p1]
- names: [drift]
  projects: [p1, p2]
  threshold: 0.9
`)
	r, err := Run(context.Background(), c, fakeLister{
		{Name: "billingAccounts/123/budgets/drift", DisplayName: "drift", Projects: []string{"projects/p1", "projects/p3"}, Thresholds: []float64{0.8}, PubSubTopic: ""},
		{Name: "billingAccounts/123/budgets/extra", DisplayName: "extra", Projects: []string{"projects/p4"}, Thresholds: []float64{0.8}, PubSubTopic: "projects/core/topics/t"},
	}, fakeResolver{})
	if err != nil {
		t.Fatal(err)
	}
	for _, typ := range []string{"configured_budget_missing_in_gcp", "project_scope_diff", "threshold_diff", "pubsub_topic_missing_or_mismatch", "gcp_budget_not_configured"} {
		if !hasDiff(r, typ) {
			t.Fatalf("missing diff %s in %+v", typ, r.Diffs)
		}
	}
	if r.Summary.Errors != 2 || r.Summary.Warnings != 3 {
		t.Fatalf("bad summary: %+v", r.Summary)
	}
}

func TestRunResolverFailureIsDiff(t *testing.T) {
	c := reconcileCfg(t, `reconcile: {enabled: true, billingAccountName: billingAccounts/123, requiredPubSubTopic: projects/core/topics/t}
budgets:
- names: [prod]
  projects: [p1]
`)
	r, err := Run(context.Background(), c, fakeLister{{Name: "billingAccounts/123/budgets/prod", DisplayName: "prod", Projects: []string{"projects/123"}, Thresholds: []float64{0.8}, PubSubTopic: "projects/core/topics/t"}}, fakeResolver{err: errors.New("nope")})
	if err != nil || !hasDiff(r, "project_scope_diff") {
		t.Fatalf("expected resolver diff err=%v result=%+v", err, r)
	}
}

func hasDiff(r Result, typ string) bool {
	for _, d := range r.Diffs {
		if d.Type == typ {
			return true
		}
	}
	return false
}
