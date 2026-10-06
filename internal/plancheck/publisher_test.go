package plancheck

import "testing"

// This literal is Google's documented identity, deliberately independent of
// the production constant so a repeated typo cannot make the test pass.
// https://docs.cloud.google.com/organization-policy/restrict-domains
func billingPublisherPlan(t *testing.T) Plan {
	t.Helper()
	return plan(t, `{"resource_changes":[{"mode":"managed","address":"module.control_plane.google_pubsub_topic_iam_member.billing_budget_publisher","type":"google_pubsub_topic_iam_member","change":{"actions":["create"],"after":{"project":"test-control","topic":"billing-budget-alerts","role":"roles/pubsub.publisher","member":"serviceAccount:billing-budget-alert@system.gserviceaccount.com"},"after_unknown":{}}}]}`)
}

func publisherScope() Scope {
	return Scope{Kind: "control", ControlProjectID: "test-control", ControlProjectNumber: "123456789012"}
}

func TestBillingBudgetPublisherContract(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*ResourceChange)
		valid  bool
	}{
		{name: "documented_publisher_create", valid: true},
		{name: "documented_publisher_update", valid: true, mutate: func(r *ResourceChange) { r.Change.Actions = []string{"update"} }},
		{name: "plural_identity_typo", mutate: func(r *ResourceChange) {
			r.Change.After["member"] = "serviceAccount:billing-budget-alerts@system.gserviceaccount.com"
		}},
		{name: "unrelated_member", mutate: func(r *ResourceChange) {
			r.Change.After["member"] = "serviceAccount:unrelated@test-control.iam.gserviceaccount.com"
		}},
		{name: "pubsub_agent_is_not_budget_publisher", mutate: func(r *ResourceChange) {
			r.Change.After["member"] = "serviceAccount:service-123456789012@gcp-sa-pubsub.iam.gserviceaccount.com"
		}},
		{name: "all_users", mutate: func(r *ResourceChange) { r.Change.After["member"] = "allUsers" }},
		{name: "all_authenticated_users", mutate: func(r *ResourceChange) { r.Change.After["member"] = "allAuthenticatedUsers" }},
		{name: "unknown_member", mutate: func(r *ResourceChange) { r.Change.AfterUnknown["member"] = true }},
		{name: "missing_member", mutate: func(r *ResourceChange) { delete(r.Change.After, "member") }},
		{name: "wrong_project", mutate: func(r *ResourceChange) { r.Change.After["project"] = "workload-project" }},
		{name: "wrong_topic", mutate: func(r *ResourceChange) { r.Change.After["topic"] = "billing-budget-alerts-dead-letter" }},
		{name: "topic_name_stays_plural", mutate: func(r *ResourceChange) { r.Change.After["topic"] = "billing-budget-alert" }},
		{name: "broader_role", mutate: func(r *ResourceChange) { r.Change.After["role"] = "roles/pubsub.admin" }},
		{name: "unrelated_address", mutate: func(r *ResourceChange) { r.Address = "module.control_plane.google_pubsub_topic_iam_member.unrelated" }},
		{name: "destroy", mutate: func(r *ResourceChange) { r.Change.Actions = []string{"delete"} }},
		{name: "replacement", mutate: func(r *ResourceChange) { r.Change.Actions = []string{"delete", "create"} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := billingPublisherPlan(t)
			if tc.mutate != nil {
				tc.mutate(&p.ResourceChanges[0])
			}
			err := Validate(p, publisherScope())
			if (err == nil) != tc.valid {
				t.Fatalf("valid=%v, validation error=%v", tc.valid, err)
			}
		})
	}
}

func TestBillingPublisherCanResumePartialControlDeployment(t *testing.T) {
	p := billingPublisherPlan(t)
	for _, existing := range []struct{ resourceType, name string }{
		{"google_service_account", "runtime"},
		{"google_cloud_run_v2_service", "receiver"},
		{"google_cloud_run_v2_service", "admin"},
	} {
		r := ResourceChange{Mode: "managed", Type: existing.resourceType, Address: "module.control_plane." + existing.resourceType + "." + existing.name}
		r.Change.Actions = []string{"no-op"}
		p.ResourceChanges = append(p.ResourceChanges, r)
	}
	if err := Validate(p, publisherScope()); err != nil {
		t.Fatalf("completing only the missing publisher binding must pass: %v", err)
	}
	p.ResourceChanges[1].Change.Actions = []string{"delete"}
	if Validate(p, publisherScope()) == nil {
		t.Fatal("partial-deployment recovery must not allow destruction of existing resources")
	}
}

func TestBudgetPublisherCannotInvokeServicesOrMintTokens(t *testing.T) {
	cases := []struct {
		resourceType, name, role, targetField, target string
	}{
		{"google_cloud_run_v2_service_iam_member", "pubsub_receiver", "roles/run.invoker", "name", "billing-guard-receiver"},
		{"google_cloud_run_v2_service_iam_member", "scheduler_admin", "roles/run.invoker", "name", "billing-guard-admin"},
		{"google_service_account_iam_member", "pubsub_token_creator", "roles/iam.serviceAccountTokenCreator", "service_account_id", "projects/test-control/serviceAccounts/billing-guard-pubsub@test-control.iam.gserviceaccount.com"},
		{"google_service_account_iam_member", "scheduler_token_creator", "roles/iam.serviceAccountTokenCreator", "service_account_id", "projects/test-control/serviceAccounts/billing-guard-scheduler@test-control.iam.gserviceaccount.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := billingPublisherPlan(t)
			r := &p.ResourceChanges[0]
			r.Type = tc.resourceType
			r.Address = "module.control_plane." + tc.resourceType + "." + tc.name
			r.Change.After["role"] = tc.role
			r.Change.After[tc.targetField] = tc.target
			if Validate(p, publisherScope()) == nil {
				t.Fatal("the budget publisher must only publish to the budget notification topic")
			}
		})
	}
}
