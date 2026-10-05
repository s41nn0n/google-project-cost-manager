package reconcile

import (
	"context"
	"errors"
	"github.com/example/google-project-cost-manager/internal/config"
	"testing"
)

type canonicalResolver struct {
	account string
	err     error
}

func (r canonicalResolver) ResolveProjectID(context.Context, string) (string, error) {
	return "p1", nil
}
func (r canonicalResolver) BillingAccount(context.Context, string) (string, error) {
	return r.account, r.err
}

func TestCanonicalDriftAndMissingPermissionsBlockReadiness(t *testing.T) {
	c := &config.Config{Defaults: config.Defaults{Threshold: 0.8}, Reconcile: config.ReconcileConfig{Enabled: true, BillingAccountNames: []string{"billingAccounts/A"}, RequiredPubSubTopic: "projects/control/topics/billing-budget-alerts"}, Budgets: []config.Budget{{BudgetResourceName: "billingAccounts/A/budgets/B", BillingAccountName: "billingAccounts/A", ProjectID: "p1", Projects: []string{"p1"}, MonthlyAmount: 100, CurrencyCode: "USD", Threshold: 0.8}}}
	b := Budget{Name: "billingAccounts/A/budgets/B", Projects: []string{"projects/1"}, Thresholds: []float64{0.5, 0.8, 1}, SpendBases: []string{"CURRENT_SPEND", "CURRENT_SPEND", "CURRENT_SPEND"}, CalendarPeriod: "MONTH", MonthlyAmount: 100, CurrencyCode: "USD", CreditTreatment: "INCLUDE_ALL_CREDITS", PubSubTopic: c.Reconcile.RequiredPubSubTopic}
	good := canonicalResolver{account: "billingAccounts/A"}
	r, err := Run(context.Background(), c, fakeLister{b}, good)
	if err != nil || r.Summary.Errors != 0 {
		t.Fatalf("valid canonical: %v %+v", err, r)
	}
	for name, mutate := range map[string]func(*Budget){"spend_cap": func(b *Budget) { b.SpendCap = true }, "filter": func(b *Budget) { b.RestrictedFilter = true }, "amount": func(b *Budget) { b.MonthlyAmount = 101 }, "currency": func(b *Budget) { b.CurrencyCode = "EUR" }, "period": func(b *Budget) { b.CalendarPeriod = "YEAR" }, "forecast": func(b *Budget) { b.SpendBases = []string{"CURRENT_SPEND", "FORECASTED_SPEND", "CURRENT_SPEND"} }, "resource": func(b *Budget) { b.Name = "billingAccounts/A/budgets/OTHER" }} {
		t.Run(name, func(t *testing.T) {
			copy := b
			mutate(&copy)
			r, err := Run(context.Background(), c, fakeLister{copy}, good)
			if err != nil || r.Summary.Errors == 0 {
				t.Fatalf("drift accepted: %v %+v", err, r)
			}
		})
	}
	r, err = Run(context.Background(), c, fakeLister{b}, canonicalResolver{err: errors.New("unlink permission missing")})
	if err != nil || !hasDiff(r, "billing_link_diff") {
		t.Fatalf("permission failure accepted: %v %+v", err, r)
	}
	r, err = Run(context.Background(), c, fakeLister{b}, canonicalResolver{})
	if err != nil || r.Summary.Errors != 0 {
		t.Fatalf("already unbilled should reconcile: %v %+v", err, r)
	}
}
