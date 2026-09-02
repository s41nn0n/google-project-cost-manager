package policy

import (
	"strings"
	"time"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/pubsub"
)

type Decision struct {
	ProjectID              string  `json:"projectId"`
	ProjectNumber          string  `json:"projectNumber,omitempty"`
	MatchedBudget          string  `json:"matchedBudget,omitempty"`
	ExpectedBillingAccount string  `json:"expectedBillingAccount,omitempty"`
	Ratio                  float64 `json:"ratio"`
	Threshold              float64 `json:"threshold"`
	DryRun                 bool    `json:"dryRun"`
	Action                 string  `json:"action"`
	Disable                bool    `json:"disable"`
	Reason                 string  `json:"reason"`
}

type Evaluation struct {
	Decisions []Decision `json:"decisions"`
	Unknown   bool       `json:"unknown"`
}

func Evaluate(c *config.Config, alert pubsub.BudgetAlert) Evaluation {
	ratio := alert.Ratio()
	budget, matched := matchBudget(c, alert)
	if !matched {
		return unknown(c, ratio)
	}
	threshold := c.Defaults.Threshold
	if budget.Threshold > 0 {
		threshold = budget.Threshold
	}
	dry := *c.Defaults.DryRun
	if budget.DryRun != nil {
		dry = *budget.DryRun
	}
	if budget.EnforcementMode != "" {
		dry = budget.EnforcementMode != "live"
	}
	action := c.Defaults.Action
	if budget.Action != "" {
		action = budget.Action
	}
	projects := budget.Projects
	if len(projects) == 0 && budget.ProjectID != "" {
		projects = []string{budget.ProjectID}
	}
	out := Evaluation{}
	disabledCount := 0
	protected := set(c.ProtectedProjects)
	for _, projectID := range projects {
		matchedName := firstNonEmpty(budget.Names)
		if budget.BudgetResourceName != "" {
			matchedName = budget.BudgetResourceName
		}
		decision := Decision{ProjectID: projectID, ProjectNumber: budget.ProjectNumber, MatchedBudget: matchedName, ExpectedBillingAccount: budget.BillingAccountName, Ratio: ratio, Threshold: threshold, DryRun: dry, Action: action}
		if projectPolicy, ok := c.Projects[projectID]; ok {
			if projectPolicy.DryRun != nil {
				decision.DryRun = *projectPolicy.DryRun
			}
			if projectPolicy.Action != "" {
				decision.Action = projectPolicy.Action
			}
		}
		applyAction(&decision)
		switch {
		case protected[projectID]:
			decision.Reason = "protected_project"
		case c.SchemaVersion >= 2 && len(projects) != 1:
			decision.Reason = "multi_project_event_rejected"
		case c.SchemaVersion >= 2 && alert.ForecastThresholdExceeded > 0 && alert.AlertThresholdExceeded == 0:
			decision.Reason = "forecast_only"
		case c.SchemaVersion >= 2 && alert.AlertThresholdExceeded == 0:
			decision.Reason = "malformed_actual_spend_event"
		case c.SchemaVersion >= 2 && stalePeriod(alert, time.Now().UTC()):
			decision.Reason = "stale_period"
		case ratio < threshold:
			decision.Reason = "below_threshold"
		case decision.Action != "disable_billing":
			decision.Reason = "action_not_disable_billing"
		case c.MaxProjectsDisabledPerEvent > 0 && !decision.DryRun && disabledCount >= c.MaxProjectsDisabledPerEvent:
			decision.Reason = "max_projects_disabled_per_event_reached"
		default:
			decision.Disable = true
			decision.Reason = "threshold_exceeded"
			if !decision.DryRun {
				disabledCount++
			}
		}
		out.Decisions = append(out.Decisions, decision)
	}
	return out
}

func matchBudget(c *config.Config, alert pubsub.BudgetAlert) (config.Budget, bool) {
	canonical := alert.CanonicalBudgetName()
	account := alert.BillingAccountName()
	for _, budget := range c.Budgets {
		if budget.BudgetResourceName != "" && canonical == budget.BudgetResourceName && account == budget.BillingAccountName {
			return budget, true
		}
	}
	if c.SchemaVersion >= 2 {
		return config.Budget{}, false
	}
	for _, budget := range c.Budgets {
		for _, configuredName := range budget.Names {
			for _, alertName := range alert.Names() {
				if alertName != "" && (alertName == configuredName || strings.HasSuffix(alertName, "/"+configuredName)) {
					return budget, true
				}
			}
		}
	}
	return config.Budget{}, false
}

func stalePeriod(alert pubsub.BudgetAlert, now time.Time) bool {
	start, err := alert.PeriodStart()
	if err != nil {
		return true
	}
	return start.UTC().Year() != now.Year() || start.UTC().Month() != now.Month() || start.UTC().Day() != 1
}

func unknown(c *config.Config, ratio float64) Evaluation {
	projects := c.UnknownAlertProjects
	if len(projects) == 0 {
		decision := Decision{Ratio: ratio, Threshold: c.Defaults.Threshold, Action: c.UnknownAlertPolicy}
		applyAction(&decision)
		switch c.UnknownAlertPolicy {
		case "ignore":
			decision.Reason = "unknown_alert_ignored"
		case "disable_billing":
			decision.Reason = "unknown_alert_no_project"
		default:
			decision.Reason = "unknown_alert_dry_run"
		}
		return Evaluation{Unknown: true, Decisions: []Decision{decision}}
	}
	out := Evaluation{Unknown: true}
	protected := set(c.ProtectedProjects)
	disabledCount := 0
	for _, project := range projects {
		decision := Decision{ProjectID: project, Ratio: ratio, Threshold: c.Defaults.Threshold, Action: c.UnknownAlertPolicy, DryRun: *c.Defaults.DryRun}
		applyAction(&decision)
		switch {
		case protected[project]:
			decision.Reason = "protected_project"
		case ratio < c.Defaults.Threshold:
			decision.Reason = "below_threshold"
		case decision.Action != "disable_billing":
			decision.Reason = "unknown_alert_" + decision.Action
		case c.MaxProjectsDisabledPerEvent > 0 && !decision.DryRun && disabledCount >= c.MaxProjectsDisabledPerEvent:
			decision.Reason = "max_projects_disabled_per_event_reached"
		default:
			decision.Disable = true
			decision.Reason = "unknown_alert_threshold_exceeded"
			if !decision.DryRun {
				disabledCount++
			}
		}
		out.Decisions = append(out.Decisions, decision)
	}
	return out
}

func applyAction(decision *Decision) {
	switch decision.Action {
	case "dry_run":
		decision.DryRun, decision.Disable = true, false
	case "ignore":
		decision.Disable = false
	}
}

func set(values []string) map[string]bool {
	out := map[string]bool{}
	for _, value := range values {
		out[value] = true
	}
	return out
}

func firstNonEmpty(values []string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
