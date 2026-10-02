package plancheck

import (
	"encoding/json"
	"testing"
)

func TestImportPlusUnrelatedAdditionFails(t *testing.T) {
	p := plan(t, `{"resource_changes":[{"address":"budget","change":{"actions":["no-op"],"importing":{"id":"b"}}},{"address":"other","change":{"actions":["create"]}}]}`)
	if ValidateImport(p) == nil {
		t.Fatal("mixed import/write plan accepted")
	}
}
func TestOwnerGrantInsideAllowedModuleFails(t *testing.T) {
	p := plan(t, `{"resource_changes":[{"address":"module.control_plane.google_project_iam_member.runtime_firestore","type":"google_project_iam_member","change":{"actions":["create"],"after":{"project":"control","role":"roles/owner","member":"serviceAccount:billing-guard-runtime@control.iam.gserviceaccount.com"}}}]}`)
	if Validate(p, Scope{Kind: "control", ControlProjectID: "control"}) == nil {
		t.Fatal("privileged grant accepted")
	}
}
func TestBudgetWriterRejectsIAMAndUnknownValues(t *testing.T) {
	p := plan(t, `{"resource_changes":[{"address":"module.billing_account.google_project_iam_member.foo","type":"google_project_iam_member","change":{"actions":["create"]}}]}`)
	if Validate(p, Scope{Kind: "billing", ControlProjectID: "control"}) == nil {
		t.Fatal("budget writer IAM accepted")
	}
}

func TestExactBudgetScopeAllowsOnlyReviewedValues(t *testing.T) {
	raw := `{"resource_changes":[{"mode":"managed","address":"module.billing_account[\"billingAccounts/A\"].google_billing_budget.project[\"p1\"]","type":"google_billing_budget","change":{"actions":["create"],"after":{"billing_account":"A","display_name":"billing-guard-p1","ownership_scope":"BILLING_ACCOUNT","deletion_policy":"ABANDON","budget_filter":[{"projects":["projects/1"],"calendar_period":"MONTH","credit_types_treatment":"INCLUDE_ALL_CREDITS"}],"all_updates_rule":[{"pubsub_topic":"projects/control/topics/billing-budget-alerts","schema_version":"1.0"}],"amount":[{"specified_amount":[{"currency_code":"USD","units":"100","nanos":0}]}],"threshold_rules":[{"threshold_percent":0.5,"spend_basis":"CURRENT_SPEND"},{"threshold_percent":0.8,"spend_basis":"CURRENT_SPEND"},{"threshold_percent":1,"spend_basis":"CURRENT_SPEND"}]}}}]}`
	scope := Scope{Kind: "billing", ControlProjectID: "control", AccountName: "billingAccounts/A", Projects: map[string]string{"p1": "1"}, MonthlyAmounts: map[string]float64{"p1": 100}, Currencies: map[string]string{"p1": "USD"}}
	if err := Validate(plan(t, raw), scope); err != nil {
		t.Fatal(err)
	}
	mutations := []func(map[string]any){
		func(a map[string]any) { a["billing_account"] = "other" },
		func(a map[string]any) {
			a["budget_filter"].([]any)[0].(map[string]any)["projects"] = []any{"projects/2"}
		},
		func(a map[string]any) {
			a["amount"].([]any)[0].(map[string]any)["specified_amount"].([]any)[0].(map[string]any)["units"] = "101"
		},
		func(a map[string]any) { delete(a, "amount") },
		func(a map[string]any) {
			a["threshold_rules"].([]any)[1].(map[string]any)["spend_basis"] = "FORECASTED_SPEND"
		},
	}
	for i, mutate := range mutations {
		p := plan(t, raw)
		mutate(p.ResourceChanges[0].Change.After)
		if Validate(p, scope) == nil {
			t.Fatalf("unsafe mutation %d accepted", i)
		}
	}
	p := plan(t, raw)
	p.ResourceChanges[0].Change.Actions = []string{"no-op"}
	p.ResourceChanges[0].Change.Importing = map[string]any{"id": "billingAccounts/A/budgets/x"}
	if err := Validate(p, scope); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(p)
	if _, err := Parse(data); err != nil {
		t.Fatal(err)
	}
}

func TestInvokerCannotCrossReceiverAdminBoundary(t *testing.T) {
	raw := `{"resource_changes":[{"address":"module.control_plane.google_cloud_run_v2_service_iam_member.pubsub_receiver","type":"google_cloud_run_v2_service_iam_member","change":{"actions":["create"],"after":{"project":"control","name":"billing-guard-receiver","role":"roles/run.invoker","member":"serviceAccount:billing-guard-pubsub@control.iam.gserviceaccount.com"}}}]}`
	scope := Scope{Kind: "control", ControlProjectID: "control"}
	if err := Validate(plan(t, raw), scope); err != nil {
		t.Fatal(err)
	}
	p := plan(t, raw)
	p.ResourceChanges[0].Change.After["name"] = "billing-guard-admin"
	if Validate(p, scope) == nil {
		t.Fatal("Pub/Sub administrative invocation accepted")
	}
}
