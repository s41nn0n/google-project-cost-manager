package repository

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/google-project-cost-manager/internal/config"
)

func TestGeneratedPolicyGlobalStopWins(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq required")
	}
	dir := t.TempDir()
	input := `{"billing_account_name":"billingAccounts/A","protected_projects":{},"budgets":{"p1":{"budget_resource_name":"billingAccounts/A/budgets/B","billing_account_name":"billingAccounts/A","project_id":"p1","project_number":"1","enforcement_mode":"live","threshold":0.8,"monthly_amount":100,"currency_code":"USD"}}}`
	if err := os.WriteFile(filepath.Join(dir, "A.json"), []byte(input), 0600); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("sh", "../../examples/private-deployment/scripts/build-policy", dir, "123", "control", "events", "projects/control/topics/billing-budget-alerts", "dry_run")
	command.Env = append(os.Environ(), "RELEASE_ID=test-digest", "INVENTORY_OBSERVED_AT=2026-10-02T00:00:00Z", "ENFORCEMENT_ENABLED=false")
	data, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("generator: %v %s", err, data)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.EnforcementEnabled || cfg.Budgets[0].EnforcementMode != "dry_run" || cfg.Budgets[0].MonthlyAmount != 100 {
		t.Fatalf("unsafe generated policy %+v", cfg)
	}
}

func TestGeneratedPolicyRequiresCanonicalBudgetIdentity(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Fatal("jq is required to verify the generated policy contract")
	}
	const accountName = "billingAccounts/AAAAAA-BBBBBB-CCCCCC"
	const budgetID = "00000000-0000-0000-0000-000000000001"
	for _, tc := range []struct {
		name               string
		budgetResourceName string
		valid              bool
	}{
		{"canonical_provider_id", accountName + "/budgets/" + budgetID, true},
		{"short_provider_name", budgetID, false},
		{"wrong_billing_account", "billingAccounts/DDDDDD-EEEEEE-FFFFFF/budgets/" + budgetID, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			input := fmt.Sprintf(`{"billing_account_name":%q,"protected_projects":{},"budgets":{"test-workload":{"budget_resource_name":%q,"billing_account_name":%q,"project_id":"test-workload","project_number":"123456789012","enforcement_mode":"dry_run","threshold":0.8,"monthly_amount":12.5,"currency_code":"EUR"}}}`, accountName, tc.budgetResourceName, accountName)
			if err := os.WriteFile(filepath.Join(dir, "account.json"), []byte(input), 0600); err != nil {
				t.Fatal(err)
			}
			command := exec.Command("sh", "../../examples/private-deployment/scripts/build-policy", dir, "123", "test-control", "events", "projects/test-control/topics/billing-budget-alerts", "dry_run")
			command.Env = append(os.Environ(), "RELEASE_ID=test-digest", "INVENTORY_OBSERVED_AT=2026-10-02T00:00:00Z", "ENFORCEMENT_ENABLED=false")
			data, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("generator: %v %s", err, data)
			}
			cfg, err := config.Parse(data)
			if !tc.valid {
				if err == nil || !strings.Contains(err.Error(), "canonical budget must be unique and belong to its configured billing account") {
					t.Fatalf("incomplete or wrong-account identity must fail validation, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(cfg.Budgets) != 1 {
				t.Fatalf("expected exactly one canonical budget, got %d", len(cfg.Budgets))
			}
			budget := cfg.Budgets[0]
			if budget.BudgetResourceName != tc.budgetResourceName || budget.BillingAccountName != accountName || budget.ProjectID != "test-workload" || budget.ProjectNumber != "123456789012" {
				t.Fatalf("canonical identity changed in generated policy: %+v", budget)
			}
			if cfg.EnforcementEnabled || budget.EnforcementMode != "dry_run" || budget.MonthlyAmount != 12.5 || budget.CurrencyCode != "EUR" || budget.Threshold != 0.8 || len(budget.Names) != 0 {
				t.Fatalf("canonical output fix must preserve dry-run financial settings without name aliases: %+v", budget)
			}
		})
	}
}
