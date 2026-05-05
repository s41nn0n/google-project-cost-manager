package policy

import (
	"strings"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/pubsub"
)

type Decision struct {
	ProjectID     string  `json:"projectId"`
	MatchedBudget string  `json:"matchedBudget,omitempty"`
	Ratio         float64 `json:"ratio"`
	Threshold     float64 `json:"threshold"`
	DryRun        bool    `json:"dryRun"`
	Action        string  `json:"action"`
	Disable       bool    `json:"disable"`
	Reason        string  `json:"reason"`
}

type Evaluation struct {
	Decisions []Decision `json:"decisions"`
	Unknown   bool       `json:"unknown"`
}

func Evaluate(c *config.Config, a pubsub.BudgetAlert) Evaluation {
	ratio := a.Ratio()
	b, matched := matchBudget(c, a)
	if !matched {
		return unknown(c, ratio)
	}
	threshold := c.Defaults.Threshold
	if b.Threshold > 0 {
		threshold = b.Threshold
	}
	dry := *c.Defaults.DryRun
	if b.DryRun != nil {
		dry = *b.DryRun
	}
	action := c.Defaults.Action
	if b.Action != "" {
		action = b.Action
	}
	out := Evaluation{}
	disabledCount := 0
	protected := set(c.ProtectedProjects)
	for _, p := range b.Projects {
		d := Decision{ProjectID: p, MatchedBudget: firstNonEmpty(b.Names), Ratio: ratio, Threshold: threshold, DryRun: dry, Action: action}
		if pp, ok := c.Projects[p]; ok {
			if pp.DryRun != nil {
				d.DryRun = *pp.DryRun
			}
			if pp.Action != "" {
				d.Action = pp.Action
			}
		}
		applyAction(&d)
		if protected[p] {
			d.Reason = "protected_project"
		} else if ratio < threshold {
			d.Reason = "below_threshold"
		} else if d.Action != "disable_billing" {
			d.Reason = "action_not_disable_billing"
		} else if c.MaxProjectsDisabledPerEvent > 0 && !d.DryRun && disabledCount >= c.MaxProjectsDisabledPerEvent {
			d.Reason = "max_projects_disabled_per_event_reached"
		} else {
			d.Disable = true
			d.Reason = "threshold_exceeded"
			if !d.DryRun {
				disabledCount++
			}
		}
		out.Decisions = append(out.Decisions, d)
	}
	return out
}

func matchBudget(c *config.Config, a pubsub.BudgetAlert) (config.Budget, bool) {
	names := a.Names()
	for _, b := range c.Budgets {
		for _, bn := range b.Names {
			for _, an := range names {
				if an != "" && (an == bn || strings.HasSuffix(an, "/"+bn)) {
					return b, true
				}
			}
		}
	}
	return config.Budget{}, false
}

func unknown(c *config.Config, ratio float64) Evaluation {
	projects := c.UnknownAlertProjects
	if len(projects) == 0 {
		d := Decision{Ratio: ratio, Threshold: c.Defaults.Threshold, Action: c.UnknownAlertPolicy, Disable: false}
		applyAction(&d)
		switch c.UnknownAlertPolicy {
		case "ignore":
			d.Reason = "unknown_alert_ignored"
		case "disable_billing":
			d.Reason = "unknown_alert_no_project"
		default:
			d.Reason = "unknown_alert_dry_run"
		}
		return Evaluation{Unknown: true, Decisions: []Decision{d}}
	}

	out := Evaluation{Unknown: true}
	protected := set(c.ProtectedProjects)
	disabledCount := 0
	for _, p := range projects {
		d := Decision{ProjectID: p, Ratio: ratio, Threshold: c.Defaults.Threshold, Action: c.UnknownAlertPolicy, DryRun: *c.Defaults.DryRun}
		applyAction(&d)
		if protected[p] {
			d.Reason = "protected_project"
		} else if ratio < c.Defaults.Threshold {
			d.Reason = "below_threshold"
		} else if d.Action != "disable_billing" {
			d.Reason = "unknown_alert_" + d.Action
		} else if c.MaxProjectsDisabledPerEvent > 0 && !d.DryRun && disabledCount >= c.MaxProjectsDisabledPerEvent {
			d.Reason = "max_projects_disabled_per_event_reached"
		} else {
			d.Disable = true
			d.Reason = "unknown_alert_threshold_exceeded"
			if !d.DryRun {
				disabledCount++
			}
		}
		out.Decisions = append(out.Decisions, d)
	}
	return out
}

func applyAction(d *Decision) {
	switch d.Action {
	case "dry_run":
		d.DryRun = true
		d.Disable = false
	case "ignore":
		d.Disable = false
	}
}

func set(xs []string) map[string]bool {
	m := map[string]bool{}
	for _, x := range xs {
		m[x] = true
	}
	return m
}
func firstNonEmpty(xs []string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}
