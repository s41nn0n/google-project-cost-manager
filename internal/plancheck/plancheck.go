package plancheck

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type Plan struct {
	ResourceChanges []ResourceChange `json:"resource_changes"`
}

type ResourceChange struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Mode    string `json:"mode"`
	Change  struct {
		Actions      []string       `json:"actions"`
		Importing    any            `json:"importing"`
		After        map[string]any `json:"after"`
		AfterUnknown map[string]any `json:"after_unknown"`
	} `json:"change"`
}

func Parse(data []byte) (Plan, error) {
	var plan Plan
	if err := json.Unmarshal(data, &plan); err != nil {
		return Plan{}, err
	}
	return plan, nil
}

func ValidateImport(plan Plan) error {
	imports := 0
	for _, resource := range plan.ResourceChanges {
		if resource.Mode == "data" {
			continue
		}
		if resource.Change.Importing == nil {
			if !onlyNoop(resource.Change.Actions) {
				return fmt.Errorf("import-only plan changes %s", resource.Address)
			}
			continue
		}
		imports++
		if !onlyNoop(resource.Change.Actions) {
			return fmt.Errorf("import %s has remote actions %v", resource.Address, resource.Change.Actions)
		}
	}
	if imports == 0 {
		return fmt.Errorf("import-only plan contains no imports")
	}
	return rejectDeleteOrReplace(plan)
}

func ValidateOnboarding(plan Plan, scopes ...Scope) error {
	if len(scopes) != 1 {
		return fmt.Errorf("onboarding requires an explicit reviewed scope; type-only validation is unsafe")
	}
	if err := Validate(plan, scopes[0]); err != nil {
		return err
	}
	if err := rejectDeleteOrReplace(plan); err != nil {
		return err
	}
	allowed := map[string]bool{
		"google_billing_budget":             true,
		"google_project_iam_member":         true,
		"google_billing_account_iam_member": true,
	}
	for _, resource := range plan.ResourceChanges {
		if onlyNoop(resource.Change.Actions) {
			continue
		}
		if !allowed[resource.Type] {
			return fmt.Errorf("onboarding changed unrelated resource %s (%s)", resource.Address, resource.Type)
		}
		if len(resource.Change.Actions) != 1 || resource.Change.Actions[0] != "create" {
			return fmt.Errorf("onboarding must only create FinOps resources: %s has %v", resource.Address, resource.Change.Actions)
		}
	}
	return nil
}

func rejectDeleteOrReplace(plan Plan) error {
	for _, resource := range plan.ResourceChanges {
		create, deleteAction := false, false
		for _, action := range resource.Change.Actions {
			create = create || action == "create"
			deleteAction = deleteAction || action == "delete"
		}
		if deleteAction {
			if create {
				return fmt.Errorf("replacement forbidden: %s", resource.Address)
			}
			return fmt.Errorf("destroy forbidden: %s", resource.Address)
		}
	}
	return nil
}

func onlyNoop(actions []string) bool { return len(actions) == 1 && actions[0] == "no-op" }

type Scope struct {
	Kind                 string             `json:"kind"`
	ControlProjectID     string             `json:"controlProjectId"`
	ControlProjectNumber string             `json:"controlProjectNumber"`
	AccountName          string             `json:"accountName"`
	Projects             map[string]string  `json:"projects"`
	ImageDigest          string             `json:"imageDigest"`
	MonthlyAmounts       map[string]float64 `json:"monthlyAmounts"`
	Currencies           map[string]string  `json:"currencies"`
}

// Validate rejects unexpected addresses, projects, identities and operations.
func Validate(plan Plan, s Scope) error {
	if s.ControlProjectID == "" {
		return fmt.Errorf("control project scope is required")
	}
	if err := rejectDeleteOrReplace(plan); err != nil {
		return err
	}
	importing := false
	for _, r := range plan.ResourceChanges {
		if r.Change.Importing != nil {
			importing = true
		}
	}
	if importing {
		if err := ValidateImport(plan); err != nil {
			return err
		}
	}
	for _, r := range plan.ResourceChanges {
		if r.Mode == "data" || (onlyNoop(r.Change.Actions) && r.Change.Importing == nil) {
			continue
		}
		if len(r.Change.Actions) != 1 || (r.Change.Actions[0] != "create" && r.Change.Actions[0] != "update" && !(r.Change.Importing != nil && r.Change.Actions[0] == "no-op")) {
			return fmt.Errorf("unsupported action on %s", r.Address)
		}
		if s.Kind == "billing" {
			if r.Type != "google_billing_budget" {
				return fmt.Errorf("budget writer may not change %s", r.Type)
			}
			matched := false
			for id, number := range s.Projects {
				address := fmt.Sprintf("module.billing_account[%q].google_billing_budget.project[%q]", s.AccountName, id)
				if r.Address != address {
					continue
				}
				matched = true
				if r.Change.After["billing_account"] != strings.TrimPrefix(s.AccountName, "billingAccounts/") || r.Change.After["display_name"] != "billing-guard-"+id || r.Change.After["ownership_scope"] != "BILLING_ACCOUNT" || r.Change.After["deletion_policy"] != "ABANDON" {
					return fmt.Errorf("wrong budget account")
				}
				filter, ok := singleton(r.Change.After["budget_filter"])
				if !ok {
					return fmt.Errorf("budget scope is unknown")
				}
				projects, ok := filter["projects"].([]any)
				if !ok || len(projects) != 1 || projects[0] != "projects/"+number || filter["calendar_period"] != "MONTH" || filter["credit_types_treatment"] != "INCLUDE_ALL_CREDITS" {
					return fmt.Errorf("unexpected budget filter")
				}
				for _, key := range []string{"services", "labels", "subaccounts", "credit_types", "resource_ancestors", "custom_period"} {
					switch v := filter[key].(type) {
					case []any:
						if len(v) > 0 {
							return fmt.Errorf("unexpected budget selector %s", key)
						}
					case map[string]any:
						if len(v) > 0 {
							return fmt.Errorf("unexpected budget selector %s", key)
						}
					}
				}
				updates, ok := singleton(r.Change.After["all_updates_rule"])
				if !ok || updates["pubsub_topic"] != "projects/"+s.ControlProjectID+"/topics/billing-budget-alerts" || updates["schema_version"] != "1.0" {
					return fmt.Errorf("unexpected budget topic")
				}
				amount, ok := singleton(r.Change.After["amount"])
				if !ok {
					return fmt.Errorf("unknown amount")
				}
				money, ok := singleton(amount["specified_amount"])
				if !ok {
					return fmt.Errorf("unknown specified amount")
				}
				units, err := strconv.ParseFloat(fmt.Sprint(money["units"]), 64)
				if err != nil {
					return fmt.Errorf("unknown money units")
				}
				nanos, err := strconv.ParseFloat(fmt.Sprint(money["nanos"]), 64)
				if err != nil {
					return fmt.Errorf("unknown money nanos")
				}
				if math.IsNaN(units) || math.IsInf(units, 0) || math.IsNaN(nanos) || math.IsInf(nanos, 0) || s.MonthlyAmounts[id] <= 0 || math.Abs(units+nanos/1e9-s.MonthlyAmounts[id]) > 0.000001 || money["currency_code"] != s.Currencies[id] {
					return fmt.Errorf("unreviewed budget amount/currency")
				}
				rules, ok := r.Change.After["threshold_rules"].([]any)
				if !ok || len(rules) != 3 {
					return fmt.Errorf("unexpected thresholds")
				}
				thresholds := map[float64]bool{}
				for _, v := range rules {
					rule, ok := v.(map[string]any)
					if !ok || rule["spend_basis"] != "CURRENT_SPEND" {
						return fmt.Errorf("forecast or unknown threshold")
					}
					n, ok := rule["threshold_percent"].(float64)
					if !ok {
						return fmt.Errorf("unknown threshold")
					}
					thresholds[n] = true
				}
				if !thresholds[0.5] || !thresholds[0.8] || !thresholds[1] {
					return fmt.Errorf("unexpected threshold values")
				}
			}
			if !matched {
				return fmt.Errorf("unexpected budget address %s", r.Address)
			}
		} else if s.Kind == "control" {
			if err := validateControl(r, s); err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unknown scope kind")
		}
	}
	return nil
}

func singleton(v any) (map[string]any, bool) {
	items, ok := v.([]any)
	if !ok || len(items) != 1 {
		return nil, false
	}
	item, ok := items[0].(map[string]any)
	return item, ok
}

func validateControl(r ResourceChange, s Scope) error {
	allowed := map[string][]string{
		"google_project_service": {"required"}, "google_service_account": {"runtime", "pubsub_invoker", "scheduler_invoker"},
		"google_firestore_database": {"events"}, "google_project_iam_member": {"runtime_firestore"},
		"google_secret_manager_secret": {"policy"}, "google_secret_manager_secret_version": {"bootstrap_policy"},
		"google_secret_manager_secret_iam_member": {"runtime_policy"}, "google_pubsub_topic": {"billing_alerts", "dead_letter"},
		"google_pubsub_subscription": {"receiver", "dead_letter"}, "google_cloud_run_v2_service": {"receiver", "admin"},
		"google_cloud_run_v2_service_iam_member": {"pubsub_receiver", "scheduler_admin", "admin"},
		"google_service_account_iam_member":      {"pubsub_token_creator", "scheduler_token_creator"},
		"google_cloud_scheduler_job":             {"reconcile", "self_test"}, "google_logging_metric": {"failures"},
		"google_monitoring_alert_policy":        {"failures", "dead_letter"},
		"google_pubsub_topic_iam_member":        {"billing_budget_publisher", "dead_letter_forwarder"},
		"google_pubsub_subscription_iam_member": {"dead_letter_subscriber"},
	}
	found := false
	for _, name := range allowed[r.Type] {
		base := "module.control_plane." + r.Type + "." + name
		if r.Address == base || strings.HasPrefix(r.Address, base+"[") {
			found = true
		}
	}
	if !found {
		return fmt.Errorf("unexpected control resource %s", r.Address)
	}
	after := r.Change.After
	resourceName := strings.Split(strings.TrimPrefix(r.Address, "module.control_plane."+r.Type+"."), "[")[0]
	pubsubAgent := "serviceAccount:service-" + s.ControlProjectNumber + "@gcp-sa-pubsub.iam.gserviceaccount.com"
	schedulerAgent := "serviceAccount:service-" + s.ControlProjectNumber + "@gcp-sa-cloudscheduler.iam.gserviceaccount.com"
	switch r.Type {
	case "google_project_service":
		services := map[string]bool{"billingbudgets.googleapis.com": true, "cloudbilling.googleapis.com": true, "cloudresourcemanager.googleapis.com": true, "cloudscheduler.googleapis.com": true, "firestore.googleapis.com": true, "logging.googleapis.com": true, "monitoring.googleapis.com": true, "pubsub.googleapis.com": true, "run.googleapis.com": true, "secretmanager.googleapis.com": true}
		service, _ := after["service"].(string)
		if !services[service] || after["disable_on_destroy"] != false {
			return fmt.Errorf("unexpected managed API or destructive API behavior")
		}
	case "google_service_account":
		ids := map[string]string{"runtime": "billing-guard-runtime", "pubsub_invoker": "billing-guard-pubsub", "scheduler_invoker": "billing-guard-scheduler"}
		if after["account_id"] != ids[resourceName] {
			return fmt.Errorf("unexpected service account")
		}
	case "google_secret_manager_secret":
		if after["secret_id"] != "billing-guard-policy" {
			return fmt.Errorf("unexpected policy secret")
		}
	case "google_secret_manager_secret_iam_member":
		if after["secret_id"] != "projects/"+s.ControlProjectID+"/secrets/billing-guard-policy" {
			return fmt.Errorf("unexpected secret IAM target")
		}
	case "google_pubsub_topic", "google_pubsub_topic_iam_member":
		names := map[string]string{"billing_alerts": "billing-budget-alerts", "dead_letter": "billing-budget-alerts-dead-letter", "billing_budget_publisher": "billing-budget-alerts", "dead_letter_forwarder": "billing-budget-alerts-dead-letter"}
		field := "name"
		if r.Type == "google_pubsub_topic_iam_member" {
			field = "topic"
		}
		if after[field] != names[resourceName] {
			return fmt.Errorf("unexpected topic target")
		}
		if r.Type == "google_pubsub_topic_iam_member" {
			expected := pubsubAgent
			if resourceName == "billing_budget_publisher" {
				expected = "serviceAccount:billing-budget-alerts@system.gserviceaccount.com"
			}
			if after["member"] != expected {
				return fmt.Errorf("wrong topic publisher")
			}
		}
	case "google_pubsub_subscription_iam_member":
		if after["subscription"] != "billing-guard-receiver" || after["member"] != pubsubAgent {
			return fmt.Errorf("unexpected subscription IAM target")
		}
	case "google_service_account_iam_member":
		expected := pubsubAgent
		if resourceName == "scheduler_token_creator" {
			expected = schedulerAgent
		}
		if after["member"] != expected {
			return fmt.Errorf("wrong token creator")
		}
	case "google_cloud_run_v2_service", "google_cloud_run_v2_service_iam_member":
		name, member := "billing-guard-admin", "serviceAccount:billing-guard-scheduler@"+s.ControlProjectID+".iam.gserviceaccount.com"
		if resourceName == "receiver" || resourceName == "pubsub_receiver" {
			name, member = "billing-guard-receiver", "serviceAccount:billing-guard-pubsub@"+s.ControlProjectID+".iam.gserviceaccount.com"
		}
		if after["name"] != name {
			return fmt.Errorf("unexpected Cloud Run target")
		}
		if r.Type == "google_cloud_run_v2_service_iam_member" && after["member"] != member {
			return fmt.Errorf("unexpected Cloud Run invoker")
		}
	}
	if r.Type != "google_secret_manager_secret_version" && r.Type != "google_service_account_iam_member" {
		if after["project"] != s.ControlProjectID {
			return fmt.Errorf("control resource outside control project: %s", r.Address)
		}
	}
	if strings.Contains(r.Type, "_iam_") {
		role, _ := after["role"].(string)
		member, _ := after["member"].(string)
		expectedRole := map[string]string{"google_project_iam_member": "roles/datastore.user", "google_secret_manager_secret_iam_member": "roles/secretmanager.secretAccessor", "google_cloud_run_v2_service_iam_member": "roles/run.invoker", "google_service_account_iam_member": "roles/iam.serviceAccountTokenCreator", "google_pubsub_topic_iam_member": "roles/pubsub.publisher", "google_pubsub_subscription_iam_member": "roles/pubsub.subscriber"}[r.Type]
		if role != expectedRole || member == "" || r.Change.AfterUnknown["member"] == true {
			return fmt.Errorf("unknown or unexpected IAM role/member: %s", r.Address)
		}
		suffix := "@" + s.ControlProjectID + ".iam.gserviceaccount.com"
		switch r.Type {
		case "google_project_iam_member", "google_secret_manager_secret_iam_member":
			if member != "serviceAccount:billing-guard-runtime"+suffix {
				return fmt.Errorf("wrong runtime IAM member")
			}
		case "google_cloud_run_v2_service_iam_member":
			if member != "serviceAccount:billing-guard-pubsub"+suffix && member != "serviceAccount:billing-guard-scheduler"+suffix {
				return fmt.Errorf("unexpected service invoker; operator grants require separate onboarding")
			}
		default:
			pubsub := "serviceAccount:service-" + s.ControlProjectNumber + "@gcp-sa-pubsub.iam.gserviceaccount.com"
			scheduler := "serviceAccount:service-" + s.ControlProjectNumber + "@gcp-sa-cloudscheduler.iam.gserviceaccount.com"
			if s.ControlProjectNumber == "" || (member != "serviceAccount:billing-budget-alerts@system.gserviceaccount.com" && member != pubsub && member != scheduler) {
				return fmt.Errorf("unexpected Google service agent")
			}
			if r.Type == "google_service_account_iam_member" {
				name := "billing-guard-pubsub"
				if member == scheduler {
					name = "billing-guard-scheduler"
				}
				if after["service_account_id"] != "projects/"+s.ControlProjectID+"/serviceAccounts/"+name+suffix {
					return fmt.Errorf("unexpected token-creator target")
				}
			}
		}
	}
	if r.Type == "google_secret_manager_secret_version" && after["secret"] != "projects/"+s.ControlProjectID+"/secrets/billing-guard-policy" {
		return fmt.Errorf("secret version outside canonical policy secret")
	}
	if r.Type == "google_cloud_run_v2_service" {
		template, ok := singleton(after["template"])
		if !ok {
			return fmt.Errorf("unknown Cloud Run template")
		}
		if template["service_account"] != "billing-guard-runtime@"+s.ControlProjectID+".iam.gserviceaccount.com" {
			return fmt.Errorf("unexpected Cloud Run runtime identity")
		}
		container, ok := singleton(template["containers"])
		if !ok || s.ImageDigest == "" || container["image"] != s.ImageDigest {
			return fmt.Errorf("unreviewed Cloud Run image")
		}
		env, ok := container["env"].([]any)
		if !ok {
			return fmt.Errorf("unknown runtime environment")
		}
		expected := map[string]string{"ROUTE_MODE": resourceName, "CONFIG_BACKEND": "secretmanager", "CONFIG_SECRET_NAME": "projects/" + s.ControlProjectID + "/secrets/billing-guard-policy", "CONFIG_SECRET_VERSION": "latest"}
		for _, v := range env {
			entry, ok := v.(map[string]any)
			if !ok {
				return fmt.Errorf("unknown runtime environment")
			}
			name, _ := entry["name"].(string)
			value, _ := entry["value"].(string)
			want, ok := expected[name]
			if !ok || value != want {
				return fmt.Errorf("unexpected runtime configuration")
			}
			delete(expected, name)
		}
		if len(expected) != 0 || after["deletion_protection"] != true {
			return fmt.Errorf("missing runtime configuration or deletion protection")
		}
	}
	return nil
}
