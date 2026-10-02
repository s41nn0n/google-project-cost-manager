package repository

import (
	"github.com/example/google-project-cost-manager/internal/config"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
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
