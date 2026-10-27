package config

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestReadinessModeValidationAndLegacyEncoding(t *testing.T) {
	c := &Config{}
	c.ApplyDefaults()
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "readinessMode") {
		t.Fatal("legacy fingerprint wire format changed")
	}
	c.ReadinessMode = "unknown"
	if c.Validate() == nil {
		t.Fatal("unknown readiness mode accepted")
	}
	c.ReadinessMode = ReadinessPerProject
	if c.Validate() == nil {
		t.Fatal("noncanonical/nonpersistent project mode accepted")
	}
}

func TestProjectReadinessRejectsAmbiguousProjectScopes(t *testing.T) {
	fixture := []byte(`schemaVersion: 2
readinessMode: per_project
controlProjectId: control
protectedProjects: [control]
eventState: {backend: firestore, projectId: control, databaseId: events}
reconcile: {enabled: true, billingAccountName: billingAccounts/A, requiredPubSubTopic: projects/control/topics/t}
budgets:
- budgetResourceName: billingAccounts/A/budgets/one
  billingAccountName: billingAccounts/A
  projectId: workload
  projectNumber: "1"
  projects: [workload]
  enforcementMode: dry_run
`)
	c, err := Parse(fixture)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"control", "projects/control", "../control", " workload"} {
		copy := *c
		copy.Budgets = append([]Budget(nil), c.Budgets...)
		copy.Budgets[0].ProjectID = id
		copy.Budgets[0].Projects = []string{id}
		if copy.Validate() == nil {
			t.Fatalf("accepted ambiguous scope %q", id)
		}
	}
	duplicate := c.Budgets[0]
	duplicate.BudgetResourceName = "billingAccounts/A/budgets/two"
	c.Budgets = append(c.Budgets, duplicate)
	if c.Validate() == nil {
		t.Fatal("duplicate project identity accepted")
	}
}
