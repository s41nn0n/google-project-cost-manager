package discovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type fakeClient struct {
	projects  []Project
	accounts  []BillingAccount
	links     map[string]BillingLink
	budgets   map[string][]ExistingBudget
	linkErr   map[string]error
	budgetErr map[string]error
}

func (f fakeClient) ListProjects(context.Context, string) ([]Project, error) { return f.projects, nil }
func (f fakeClient) ListBillingAccounts(context.Context, string) ([]BillingAccount, error) {
	return f.accounts, nil
}
func (f fakeClient) GetProjectBilling(_ context.Context, id string) (BillingLink, error) {
	return f.links[id], f.linkErr[id]
}
func (f fakeClient) ListBudgets(_ context.Context, account string) ([]ExistingBudget, error) {
	return f.budgets[account], f.budgetErr[account]
}

func basePolicy() ReviewedPolicy {
	return ReviewedPolicy{
		ControlProjectID:    "control",
		RequiredPubSubTopic: "projects/control/topics/billing-budget-alerts",
		BillingAccounts:     map[string]AccountPolicy{"billingAccounts/A": {DefaultMonthlyAmount: 100, CurrencyCode: "USD"}},
	}
}

func exactBudget(name, project string) ExistingBudget {
	return ExistingBudget{
		Name: "billingAccounts/A/budgets/one", DisplayName: name, OwnershipScope: "BILLING_ACCOUNT", FilterCompatible: true, NotificationCompatible: true,
		Projects: []string{"projects/" + project}, CalendarPeriod: "MONTH",
		Amount: Money{CurrencyCode: "USD", Units: 100}, CreditTypesTreatment: "INCLUDE_ALL_CREDITS",
		PubSubTopic: "projects/control/topics/billing-budget-alerts",
		Thresholds:  []Threshold{{0.5, "CURRENT_SPEND"}, {0.8, "CURRENT_SPEND"}, {1, "CURRENT_SPEND"}},
	}
}

func TestScanClassifiesAndFindsExactImport(t *testing.T) {
	fake := fakeClient{
		projects: []Project{{ProjectID: "z-project", ProjectNumber: "2", LifecycleState: "DELETE_REQUESTED"}, {ProjectID: "a-project", ProjectNumber: "1", LifecycleState: "ACTIVE"}, {ProjectID: "control", ProjectNumber: "3", LifecycleState: "ACTIVE"}},
		accounts: []BillingAccount{{Name: "billingAccounts/A", Open: true}},
		links:    map[string]BillingLink{"a-project": {BillingEnabled: true, BillingAccountName: "billingAccounts/A"}, "control": {BillingEnabled: true, BillingAccountName: "billingAccounts/A"}},
		budgets:  map[string][]ExistingBudget{"billingAccounts/A": {exactBudget("billing-guard-a-project", "1")}},
		linkErr:  map[string]error{}, budgetErr: map[string]error{},
	}
	result, err := Scan(context.Background(), "organizations/123", basePolicy(), fake)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Coverage.Complete || len(result.Imports) != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	got := []string{result.Inventory.Projects[0].ProjectID + ":" + result.Inventory.Projects[0].Classification, result.Inventory.Projects[1].ProjectID + ":" + result.Inventory.Projects[1].Classification, result.Inventory.Projects[2].ProjectID + ":" + result.Inventory.Projects[2].Classification}
	want := []string{"a-project:managed", "control:protected", "z-project:inactive"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("classifications = %#v, want %#v", got, want)
	}
}

func TestScanBlocksMissingVisibilityAndDefaults(t *testing.T) {
	policy := basePolicy()
	delete(policy.BillingAccounts, "billingAccounts/A")
	// Validation allows the scanner to report a discovered account missing its reviewed default.
	fake := fakeClient{
		projects: []Project{{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"}},
		accounts: []BillingAccount{{Name: "billingAccounts/A"}},
		links:    map[string]BillingLink{"p1": {BillingEnabled: true, BillingAccountName: "billingAccounts/A"}},
		budgets:  map[string][]ExistingBudget{}, linkErr: map[string]error{}, budgetErr: map[string]error{},
	}
	result, err := Scan(context.Background(), "organizations/123", policy, fake)
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage.Complete || result.Inventory.Projects[0].Classification != ClassBlocked {
		t.Fatalf("expected blocked result: %+v", result)
	}

	fake.linkErr["p1"] = errors.New("permission denied")
	result, err = Scan(context.Background(), "organizations/123", basePolicy(), fake)
	if err != nil {
		t.Fatal(err)
	}
	if result.Inventory.Projects[0].Classification != ClassBlocked {
		t.Fatalf("permission failure not blocked: %+v", result)
	}
}

func TestExactBudgetMatchRejectsForecastAndNonCanonicalName(t *testing.T) {
	entry := ProjectInventory{Project: Project{ProjectID: "p1", ProjectNumber: "1"}, MonthlyAmount: 100, CurrencyCode: "USD", CanonicalDisplayName: "billing-guard-p1"}
	policy := basePolicy()
	budget := exactBudget("someone-elses-budget", "1")
	if got := compatibleBudgets([]ExistingBudget{budget}, entry, policy); len(got) != 0 {
		t.Fatalf("non-canonical budget matched: %+v", got)
	}
	budget.DisplayName = entry.CanonicalDisplayName
	budget.Thresholds[1].SpendBasis = "FORECASTED_SPEND"
	if got := compatibleBudgets([]ExistingBudget{budget}, entry, policy); len(got) != 0 {
		t.Fatalf("forecast budget matched: %+v", got)
	}
}
