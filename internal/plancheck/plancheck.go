package plancheck

import (
	"encoding/json"
	"fmt"
)

type Plan struct {
	ResourceChanges []ResourceChange `json:"resource_changes"`
}

type ResourceChange struct {
	Address string `json:"address"`
	Type    string `json:"type"`
	Change  struct {
		Actions   []string `json:"actions"`
		Importing any      `json:"importing"`
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
	for _, resource := range plan.ResourceChanges {
		if resource.Change.Importing == nil {
			continue
		}
		if !onlyNoop(resource.Change.Actions) {
			return fmt.Errorf("import %s has remote actions %v", resource.Address, resource.Change.Actions)
		}
	}
	return rejectDeleteOrReplace(plan)
}

func ValidateOnboarding(plan Plan) error {
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
