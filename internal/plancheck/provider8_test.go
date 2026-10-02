package plancheck

import (
	"os"
	"testing"
)

// This fixture was captured from an offline real Google 8.5.0 provider plan,
// not a mock provider. Only the change and relevant configuration were retained.
func TestProvider8BudgetPlan(t *testing.T) {
	data, err := os.ReadFile("testdata/provider8-budget-create.json")
	if err != nil {
		t.Fatal(err)
	}
	scope := Scope{
		Kind: "billing", ControlProjectID: "test-control", AccountName: "billingAccounts/AAAAAA-BBBBBB-CCCCCC",
		Projects:       map[string]string{"test-workload": "123456789012"},
		MonthlyAmounts: map[string]float64{"test-workload": 12.5},
		Currencies:     map[string]string{"test-workload": "EUR"},
	}
	cases := []struct {
		name   string
		mutate func(*Plan)
		valid  bool
	}{
		{name: "provider_suppressed_month", valid: true},
		{name: "explicit_month", valid: true, mutate: func(p *Plan) { budgetFilter(p)["calendar_period"] = "MONTH" }},
		{name: "quarter", mutate: func(p *Plan) { budgetFilter(p)["calendar_period"] = "QUARTER" }},
		{name: "year", mutate: func(p *Plan) { budgetFilter(p)["calendar_period"] = "YEAR" }},
		{name: "custom_period", mutate: func(p *Plan) {
			budgetFilter(p)["custom_period"] = []any{map[string]any{"start_date": []any{map[string]any{"year": 2026, "month": 1, "day": 1}}}}
		}},
		{name: "unknown_period", mutate: func(p *Plan) { unknownBudgetFilter(p)["calendar_period"] = true }},
		{name: "unknown_custom_period", mutate: func(p *Plan) { unknownBudgetFilter(p)["custom_period"] = true }},
		{name: "unknown_services", mutate: func(p *Plan) { unknownBudgetFilter(p)["services"] = true }},
		{name: "unknown_entire_filter", mutate: func(p *Plan) { p.ResourceChanges[0].Change.AfterUnknown["budget_filter"] = true }},
		{name: "missing_config_proof", mutate: func(p *Plan) { p.Configuration.RootModule.ModuleCalls = nil }},
		{name: "dynamic_labels", mutate: func(p *Plan) { configuredBudgetFilter(p)["labels"] = map[string]any{"references": []any{"var.labels"}} }},
		{name: "nonempty_labels", mutate: func(p *Plan) {
			configuredBudgetFilter(p)["labels"] = map[string]any{"constant_value": map[string]any{"team": "test"}}
		}},
		{name: "update_unknown_labels", mutate: func(p *Plan) { p.ResourceChanges[0].Change.Actions = []string{"update"} }},
		{name: "import_unknown_labels", mutate: func(p *Plan) {
			p.ResourceChanges[0].Change.Actions = []string{"no-op"}
			p.ResourceChanges[0].Change.Importing = map[string]any{"id": "billingAccounts/AAAAAA-BBBBBB-CCCCCC/budgets/test"}
		}},
		{name: "unknown_currency", mutate: func(p *Plan) {
			delete(p.ResourceChanges[0].Change.After["amount"].([]any)[0].(map[string]any)["specified_amount"].([]any)[0].(map[string]any), "currency_code")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := Parse(data)
			if err != nil {
				t.Fatal(err)
			}
			if tc.mutate != nil {
				tc.mutate(&p)
			}
			err = Validate(p, scope)
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, validation error=%v", tc.valid, err)
			}
		})
	}
}

func budgetFilter(p *Plan) map[string]any {
	return p.ResourceChanges[0].Change.After["budget_filter"].([]any)[0].(map[string]any)
}

func unknownBudgetFilter(p *Plan) map[string]any {
	return p.ResourceChanges[0].Change.AfterUnknown["budget_filter"].([]any)[0].(map[string]any)
}

func configuredBudgetFilter(p *Plan) map[string]any {
	return p.Configuration.RootModule.ModuleCalls["billing_account"].Module.Resources[0].Expressions["budget_filter"].([]any)[0].(map[string]any)
}
