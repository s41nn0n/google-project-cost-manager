package discovery

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var ErrBudgetInventoryUnstable = errors.New("standard alert-budget inventory changed between consecutive reads")

// The API's explicit spendCap field is the only supported discriminator. Names,
// amounts, filters, ownership scope, and alert thresholds are not type evidence.
func standardBudgets(budgets []ExistingBudget) []ExistingBudget {
	var out []ExistingBudget
	for _, budget := range budgets {
		if !budget.SpendCap {
			out = append(out, budget)
		}
	}
	return out
}

func normalizeBudget(budget ExistingBudget) ExistingBudget {
	budget.Projects = append([]string(nil), budget.Projects...)
	sort.Strings(budget.Projects)
	budget.Thresholds = append([]Threshold(nil), budget.Thresholds...)
	sort.Slice(budget.Thresholds, func(i, j int) bool {
		if budget.Thresholds[i].Percent != budget.Thresholds[j].Percent {
			return budget.Thresholds[i].Percent < budget.Thresholds[j].Percent
		}
		return budget.Thresholds[i].SpendBasis < budget.Thresholds[j].SpendBasis
	})
	return budget
}

// Compare all fields used for adoption, not just resource names. API ordering is
// not meaningful, but missing budgets or changed configuration must block plans.
func compareBudgetSnapshots(first, second []ExistingBudget) error {
	snapshot := func(budgets []ExistingBudget) (map[string]string, error) {
		out := make(map[string]string, len(budgets))
		for _, budget := range budgets {
			if budget.Name == "" {
				return nil, fmt.Errorf("budget response contains an empty resource name")
			}
			if _, duplicate := out[budget.Name]; duplicate {
				return nil, fmt.Errorf("budget response contains duplicate resource %s", budget.Name)
			}
			data, err := json.Marshal(normalizeBudget(budget))
			if err != nil {
				return nil, fmt.Errorf("normalize budget %s: %w", budget.Name, err)
			}
			out[budget.Name] = string(data)
		}
		return out, nil
	}
	a, err := snapshot(first)
	if err != nil {
		return err
	}
	b, err := snapshot(second)
	if err != nil {
		return err
	}
	var added, removed, changed []string
	for name, value := range a {
		other, exists := b[name]
		if !exists {
			removed = append(removed, name)
		} else if other != value {
			changed = append(changed, name)
		}
	}
	for name := range b {
		if _, exists := a[name]; !exists {
			added = append(added, name)
		}
	}
	if len(added)+len(removed)+len(changed) == 0 {
		return nil
	}
	// Keep logs bounded and omit budget amounts and notification configuration.
	summary := func(names []string) string {
		sort.Strings(names)
		count := len(names)
		if count > 5 {
			names = names[:5]
		}
		return fmt.Sprintf("%d [%s]", count, strings.Join(names, ", "))
	}
	return fmt.Errorf("%w: first=%d second=%d; added=%s; removed=%s; changed=%s; no snapshot selected; investigate concurrent budget edits and Billing Budget API visibility before retrying; do not bypass coverage checks", ErrBudgetInventoryUnstable, len(a), len(b), summary(added), summary(removed), summary(changed))
}
