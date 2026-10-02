package discovery

import (
	"context"
	"encoding/json"
	"testing"
)

func TestUnbilledRetainsExactBudgetWithoutInventingBillingLink(t *testing.T) {
	f := fakeClient{projects: []Project{{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"}}, accounts: []BillingAccount{{Name: "billingAccounts/A"}}, budgets: map[string][]ExistingBudget{"billingAccounts/A": {exactBudget("billing-guard-p1", "1")}}}
	r, err := Scan(context.Background(), "organizations/123", basePolicy(), f)
	if err != nil {
		t.Fatal(err)
	}
	p := r.Inventory.Projects[0]
	if !r.Coverage.Complete || p.Classification != ClassUnbilled || p.BillingEnabled || p.BillingAccountName != "" || p.BudgetAccountName != "billingAccounts/A" || p.ImportCandidate == "" {
		t.Fatalf("incorrect retention: %+v", p)
	}
	data, err := terraformInputs(r, basePolicy())
	if err != nil {
		t.Fatal(err)
	}
	var inputs struct {
		Accounts map[string]struct {
			Projects map[string]struct {
				Classification     string `json:"classification"`
				BudgetResourceName string `json:"budget_resource_name"`
			}
		} `json:"billing_accounts"`
	}
	if err := json.Unmarshal(data, &inputs); err != nil {
		t.Fatal(err)
	}
	retained := inputs.Accounts["billingAccounts/A"].Projects["p1"]
	if retained.Classification != ClassUnbilled || retained.BudgetResourceName != p.ImportCandidate {
		t.Fatalf("missing retention inputs: %s", data)
	}
	f.budgets["billingAccounts/A"][0].DisplayName = "unrelated"
	r, err = Scan(context.Background(), "organizations/123", basePolicy(), f)
	if err != nil {
		t.Fatal(err)
	}
	if r.Inventory.Projects[0].ImportCandidate != "" {
		t.Fatal("unrelated budget retained")
	}
}
