package plancheck

import "testing"

func plan(t *testing.T, input string) Plan {
	t.Helper()
	parsed, err := Parse([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func TestImportMustHaveZeroRemoteChanges(t *testing.T) {
	good := plan(t, `{"resource_changes":[{"address":"google_billing_budget.project[\"p1\"]","type":"google_billing_budget","change":{"actions":["no-op"],"importing":{"id":"billingAccounts/A/budgets/B"}}}]}`)
	if err := ValidateImport(good); err != nil {
		t.Fatal(err)
	}
	bad := plan(t, `{"resource_changes":[{"address":"google_billing_budget.project[\"p1\"]","type":"google_billing_budget","change":{"actions":["update"],"importing":{"id":"billingAccounts/A/budgets/B"}}}]}`)
	if err := ValidateImport(bad); err == nil {
		t.Fatal("expected changed import to fail")
	}
}

func TestOnboardingOnlyCreatesExpectedFinOpsResources(t *testing.T) {
	good := plan(t, `{"resource_changes":[
{"address":"google_billing_budget.project[\"new\"]","type":"google_billing_budget","change":{"actions":["create"]}},
{"address":"google_project_iam_member.runtime_billing_unlink[\"new\"]","type":"google_project_iam_member","change":{"actions":["create"]}}
]}`)
	if err := ValidateOnboarding(good); err == nil {
		t.Fatal("an onboarding plan without a reviewed scope must fail")
	}
	bad := plan(t, `{"resource_changes":[{"address":"google_compute_instance.app","type":"google_compute_instance","change":{"actions":["delete","create"]}}]}`)
	if err := ValidateOnboarding(bad); err == nil {
		t.Fatal("expected unrelated replacement to fail")
	}
}
