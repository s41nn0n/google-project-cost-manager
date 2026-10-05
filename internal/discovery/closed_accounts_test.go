package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScanClosedAccountsRemainInventoryOnlyWithoutDefaults(t *testing.T) {
	policy := basePolicy()
	external := exactBudget("unrelated-budget", "2")
	external.Name = "billingAccounts/B/budgets/external"
	fake := fakeClient{
		accounts: []BillingAccount{{Name: "billingAccounts/C"}, {Name: "billingAccounts/A", Open: true}, {Name: "billingAccounts/B"}},
		projects: []Project{
			{ProjectID: "old-project", ProjectNumber: "2", LifecycleState: "ACTIVE"},
			{ProjectID: "control", ProjectNumber: "3", LifecycleState: "ACTIVE"},
			{ProjectID: "app", ProjectNumber: "1", LifecycleState: "ACTIVE"},
			{ProjectID: "inactive", ProjectNumber: "4", LifecycleState: "DELETE_REQUESTED"},
		},
		links: map[string]BillingLink{
			"app":         {BillingAccountName: "billingAccounts/A", BillingEnabled: true},
			"control":     {BillingAccountName: "billingAccounts/A", BillingEnabled: true},
			"old-project": {BillingAccountName: "billingAccounts/B", BillingEnabled: false},
		},
		budgets: map[string][]ExistingBudget{"billingAccounts/B": {external}},
	}
	result, err := Scan(context.Background(), "organizations/123", policy, fake)
	if err != nil {
		t.Fatal(err)
	}
	wantCoverage := Coverage{TotalProjects: 4, Complete: true, Classifications: map[string]int{ClassManaged: 1, ClassProtected: 1, ClassUnbilled: 1, ClassInactive: 1}, BudgetDiscovery: BudgetDiscoveryCoverage{Scope: BudgetScopeStandardAlertsOnly, PreviewSpendCaps: PreviewSpendCapVisibilityNotVerified}}
	if !reflect.DeepEqual(result.Coverage, wantCoverage) || len(result.Diagnostics) != 0 || len(result.Imports) != 0 {
		t.Fatalf("unexpected coverage, diagnostics, or imports: %+v", result)
	}
	wantAccounts := []BillingAccount{{Name: "billingAccounts/A", Open: true}, {Name: "billingAccounts/B"}, {Name: "billingAccounts/C"}}
	if !reflect.DeepEqual(result.Inventory.BillingAccounts, wantAccounts) {
		t.Fatalf("closed accounts missing from inventory: %+v", result.Inventory.BillingAccounts)
	}
	if len(result.Inventory.Budgets) != 1 || result.Inventory.Budgets[0].Name != external.Name || result.Inventory.Budgets[0].Classification != "externally_owned" {
		t.Fatalf("unrelated closed-account budget was not preserved: %+v", result.Inventory.Budgets)
	}
	oldProject := result.Inventory.Projects[3]
	if oldProject.ProjectID != "old-project" || oldProject.Classification != ClassUnbilled || oldProject.BillingAccountName != "billingAccounts/B" || oldProject.BillingEnabled || oldProject.MonthlyAmount != 0 || oldProject.CurrencyCode != "" || oldProject.BudgetAccountName != "" || oldProject.ImportCandidate != "" {
		t.Fatalf("closed account invented billing or budget policy: %+v", oldProject)
	}
	data, err := terraformInputs(result, policy)
	if err != nil {
		t.Fatal(err)
	}
	var inputs struct {
		Accounts map[string]struct {
			Projects map[string]json.RawMessage `json:"projects"`
		} `json:"billing_accounts"`
	}
	if err := json.Unmarshal(data, &inputs); err != nil {
		t.Fatal(err)
	}
	if len(inputs.Accounts) != 1 || len(inputs.Accounts["billingAccounts/A"].Projects) != 2 || inputs.Accounts["billingAccounts/A"].Projects["app"] == nil || inputs.Accounts["billingAccounts/A"].Projects["control"] == nil {
		t.Fatalf("inventory-only accounts entered Terraform inputs: %s", data)
	}

	// Different API ordering must not change any of the generated documents.
	first, second := t.TempDir(), t.TempDir()
	if err := WriteOutput(first, result, policy); err != nil {
		t.Fatal(err)
	}
	fake.accounts = []BillingAccount{{Name: "billingAccounts/B"}, {Name: "billingAccounts/C"}, {Name: "billingAccounts/A", Open: true}}
	for i, j := 0, len(fake.projects)-1; i < j; i, j = i+1, j-1 {
		fake.projects[i], fake.projects[j] = fake.projects[j], fake.projects[i]
	}
	again, err := Scan(context.Background(), "organizations/123", policy, fake)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteOutput(second, again, policy); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inventory.yaml", "coverage.json", "imports.json", "imports.tf", "diagnostics.json", "terraform.auto.tfvars.json"} {
		a, err := osRead(filepath.Join(first, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := osRead(filepath.Join(second, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("%s changed with API ordering", name)
		}
	}
}

func TestScanReopenedAccountRequiresDefaultEvenWithoutProjects(t *testing.T) {
	policy := basePolicy()
	delete(policy.BillingAccounts, "billingAccounts/A")
	for _, open := range []bool{false, true} {
		fake := fakeClient{accounts: []BillingAccount{{Name: "billingAccounts/A", Open: open}}}
		result, err := Scan(context.Background(), "organizations/123", policy, fake)
		if err != nil {
			t.Fatal(err)
		}
		if result.Coverage.Complete == open {
			t.Fatalf("open=%t incorrectly accepted or rejected: %+v", open, result)
		}
		if open && (len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "missing_default_amount" || result.Diagnostics[0].BillingAccount != "billingAccounts/A") {
			t.Fatalf("reopened account did not require reviewed default: %+v", result)
		}
		if !open && len(result.Diagnostics) != 0 {
			t.Fatalf("unused closed account produced diagnostics: %+v", result)
		}
	}
}

func TestScanActiveBillingLinkOnClosedAccountStillRequiresDefault(t *testing.T) {
	policy := basePolicy()
	delete(policy.BillingAccounts, "billingAccounts/A")
	fake := fakeClient{
		accounts: []BillingAccount{{Name: "billingAccounts/A"}},
		projects: []Project{{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"}},
		links:    map[string]BillingLink{"p1": {BillingAccountName: "billingAccounts/A", BillingEnabled: true}},
	}
	result, err := Scan(context.Background(), "organizations/123", policy, fake)
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage.Complete || result.Inventory.Projects[0].Classification != ClassBlocked || len(result.Imports) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "missing_default_amount" || result.Diagnostics[0].ProjectID != "p1" {
		t.Fatalf("active billed project escaped default requirement: %+v", result)
	}
}

func TestScanClosedAccountVisibilityFailuresRemainBlocking(t *testing.T) {
	for _, budgetFailure := range []bool{true, false} {
		name := "project billing visibility"
		if budgetFailure {
			name = "budget visibility"
		}
		t.Run(name, func(t *testing.T) {
			fake := fakeClient{
				accounts: []BillingAccount{{Name: "billingAccounts/B"}},
				projects: []Project{{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"}},
				links:    map[string]BillingLink{"p1": {BillingAccountName: "billingAccounts/B"}},
			}
			wantCode := "project_billing_inaccessible"
			if budgetFailure {
				fake.budgetErr = map[string]error{"billingAccounts/B": errors.New("permission denied")}
				wantCode = "billing_account_inaccessible"
			} else {
				fake.linkErr = map[string]error{"p1": errors.New("permission denied")}
			}
			result, err := Scan(context.Background(), "organizations/123", basePolicy(), fake)
			if err != nil {
				t.Fatal(err)
			}
			if result.Coverage.Complete || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != wantCode {
				t.Fatalf("closed account bypassed visibility requirement: %+v", result)
			}
			if !budgetFailure && result.Inventory.Projects[0].Classification != ClassBlocked {
				t.Fatalf("project visibility failure was not blocked: %+v", result)
			}
		})
	}
}

func TestScanClosedAccountCanonicalBudgetCannotLoseReviewedPolicy(t *testing.T) {
	for _, variant := range []string{"exact", "changed amount", "changed topic", "multi-project", "changed project scope"} {
		t.Run(variant, func(t *testing.T) {
			budget := exactBudget("billing-guard-p1", "1")
			budget.Name = "billingAccounts/B/budgets/guard"
			switch variant {
			case "changed amount":
				budget.Amount.Units = 200
			case "changed topic":
				budget.PubSubTopic = "projects/other/topics/alerts"
			case "multi-project":
				budget.Projects = []string{"projects/1", "projects/2"}
			case "changed project scope":
				budget.Projects = []string{"projects/2"}
			}
			fake := fakeClient{
				accounts: []BillingAccount{{Name: "billingAccounts/B"}},
				projects: []Project{{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"}},
				budgets:  map[string][]ExistingBudget{"billingAccounts/B": {budget}},
			}
			result, err := Scan(context.Background(), "organizations/123", basePolicy(), fake)
			if err != nil {
				t.Fatal(err)
			}
			if result.Coverage.Complete || result.Inventory.Projects[0].Classification != ClassBlocked || len(result.Imports) != 0 || len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "missing_default_amount" || result.Diagnostics[0].ProjectID != "p1" || result.Diagnostics[0].BillingAccount != "billingAccounts/B" {
				t.Fatalf("canonical budget silently lost reviewed policy: %+v", result)
			}
			if result.Inventory.Budgets[0].Classification != "externally_owned" || result.Inventory.Projects[0].MonthlyAmount != 0 || result.Inventory.Projects[0].ImportCandidate != "" {
				t.Fatalf("canonical name alone inferred budget ownership or amount: %+v", result)
			}
		})
	}
}

func TestScanClosedAccountRetainsExactGuardWithReviewedDefault(t *testing.T) {
	policy := basePolicy()
	policy.BillingAccounts["billingAccounts/B"] = AccountPolicy{DefaultMonthlyAmount: 100, CurrencyCode: "USD"}
	budget := exactBudget("billing-guard-p1", "1")
	budget.Name = "billingAccounts/B/budgets/guard"
	fake := fakeClient{
		accounts: []BillingAccount{{Name: "billingAccounts/B"}},
		projects: []Project{{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"}},
		budgets:  map[string][]ExistingBudget{"billingAccounts/B": {budget}},
	}
	result, err := Scan(context.Background(), "organizations/123", policy, fake)
	if err != nil {
		t.Fatal(err)
	}
	project := result.Inventory.Projects[0]
	if !result.Coverage.Complete || len(result.Diagnostics) != 0 || project.Classification != ClassUnbilled || project.BillingEnabled || project.BillingAccountName != "" || project.MonthlyAmount != 100 || project.BudgetAccountName != "billingAccounts/B" || project.BudgetClassification != "retained_unbilled" || project.ImportCandidate != budget.Name || len(result.Imports) != 1 || result.Imports[0].BillingAccountName != "billingAccounts/B" || result.Imports[0].RemoteID != budget.Name {
		t.Fatalf("closed-account retained budget was not preserved safely: %+v", result)
	}
}
