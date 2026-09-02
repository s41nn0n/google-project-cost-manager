package config

import "testing"

func TestOrganizationPolicyRequiresCanonicalLiveSafety(t *testing.T) {
	base := `schemaVersion: 2
controlProjectId: control
unknownAlertPolicy: ignore
maxProjectsDisabledPerEvent: 1
protectedProjects: [control]
eventState: {backend: firestore, projectId: control, databaseId: events}
budgets:
- budgetResourceName: billingAccounts/A/budgets/B
  billingAccountName: billingAccounts/A
  projectId: p1
  projectNumber: "1"
  enforcementMode: live
  projects: [p1]
`
	if _, err := Parse([]byte(base)); err != nil {
		t.Fatal(err)
	}
	cases := []string{
		`schemaVersion: 2
controlProjectId: control
unknownAlertPolicy: disable_billing
maxProjectsDisabledPerEvent: 1
protectedProjects: [control]`,
		`schemaVersion: 2
controlProjectId: control
unknownAlertPolicy: ignore
maxProjectsDisabledPerEvent: 2
protectedProjects: [control]`,
		`schemaVersion: 2
controlProjectId: control
unknownAlertPolicy: ignore
maxProjectsDisabledPerEvent: 1
protectedProjects: [control]
budgets: [{enforcementMode: live, projects: [p1, p2]}]`,
	}
	for _, input := range cases {
		if _, err := Parse([]byte(input)); err == nil {
			t.Fatalf("expected rejection for:\n%s", input)
		}
	}
}
