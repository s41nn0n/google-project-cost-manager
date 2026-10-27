package discovery

import (
	"encoding/json"
	"testing"
)

func TestProjectReadinessDefaultsNewProjectsToDryRun(t *testing.T) {
	p := basePolicy()
	p.ApplyDefaults()
	p.ReadinessMode = "per_project"
	p.EnforcementMode = "live"
	p.ProjectEnforcementModes = map[string]string{"existing": "live", "retained": "live"}
	r := Result{Inventory: Inventory{Organization: "organizations/123", Projects: []ProjectInventory{
		{Project: Project{ProjectID: "existing", ProjectNumber: "1"}, BillingLink: BillingLink{BillingAccountName: "billingAccounts/A"}, Classification: ClassManaged},
		{Project: Project{ProjectID: "new", ProjectNumber: "2"}, BillingLink: BillingLink{BillingAccountName: "billingAccounts/A"}, Classification: ClassManaged},
		{Project: Project{ProjectID: "retained", ProjectNumber: "3"}, BudgetAccountName: "billingAccounts/A", Classification: ClassUnbilled},
	}}}
	data, err := terraformInputs(r, p)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Mode     string `json:"readiness_mode"`
		Accounts map[string]struct {
			Projects map[string]struct {
				Mode string `json:"enforcement_mode"`
			} `json:"projects"`
		} `json:"billing_accounts"`
	}
	if err = json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	projects := got.Accounts["billingAccounts/A"].Projects
	if got.Mode != "per_project" || projects["existing"].Mode != "live" || projects["new"].Mode != "dry_run" || projects["retained"].Mode != "dry_run" {
		t.Fatalf("unsafe modes: %s", data)
	}
	p.ReadinessMode = ""
	if p.Validate() == nil {
		t.Fatal("per-project overrides accepted in legacy mode")
	}
	p.ReadinessMode = "per_project"
	p.ProjectEnforcementModes["control"] = "live"
	if p.Validate() == nil {
		t.Fatal("live control project accepted")
	}
}
