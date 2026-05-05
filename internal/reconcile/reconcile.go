package reconcile

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/example/google-project-cost-manager/internal/config"
)

const (
	SeverityError   = "error"
	SeverityWarning = "warning"
)

type Budget struct {
	Name        string
	DisplayName string
	Projects    []string
	Thresholds  []float64
	PubSubTopic string
}

type BudgetLister interface {
	ListBudgets(ctx context.Context, billingAccountName string) ([]Budget, error)
}

type ProjectResolver interface {
	ResolveProjectID(ctx context.Context, project string) (string, error)
}

type Summary struct {
	GCPBudgets        int `json:"gcpBudgets"`
	ConfiguredBudgets int `json:"configuredBudgets"`
	Errors            int `json:"errors"`
	Warnings          int `json:"warnings"`
}

type Diff struct {
	Type                   string    `json:"type"`
	Severity               string    `json:"severity"`
	BudgetDisplayName      string    `json:"budgetDisplayName,omitempty"`
	BudgetName             string    `json:"budgetName,omitempty"`
	ConfiguredName         string    `json:"configuredName,omitempty"`
	Message                string    `json:"message,omitempty"`
	ProjectsInGCPNotConfig []string  `json:"projectsInGcpNotConfig,omitempty"`
	ProjectsInConfigNotGCP []string  `json:"projectsInConfigNotGcp,omitempty"`
	GCPThresholds          []float64 `json:"gcpThresholds,omitempty"`
	ConfiguredThreshold    float64   `json:"configuredThreshold,omitempty"`
	ExpectedTopic          string    `json:"expectedTopic,omitempty"`
	ActualTopic            string    `json:"actualTopic,omitempty"`
}

type Result struct {
	SourceOfTruth string  `json:"sourceOfTruth"`
	Mode          string  `json:"mode"`
	Summary       Summary `json:"summary"`
	Diffs         []Diff  `json:"diffs"`
}

func Run(ctx context.Context, cfg *config.Config, lister BudgetLister, resolver ProjectResolver) (Result, error) {
	if cfg == nil {
		return Result{}, fmt.Errorf("config is required")
	}
	if !cfg.Reconcile.Enabled {
		return Result{}, fmt.Errorf("reconcile.enabled must be true")
	}
	if lister == nil {
		return Result{}, fmt.Errorf("budget lister is required")
	}
	budgets, err := lister.ListBudgets(ctx, cfg.Reconcile.BillingAccountName)
	if err != nil {
		return Result{}, err
	}
	r := Result{SourceOfTruth: cfg.Reconcile.SourceOfTruth, Mode: cfg.Reconcile.Mode}
	r.Summary.GCPBudgets = len(budgets)
	r.Summary.ConfiguredBudgets = len(cfg.Budgets)

	matchedGCP := map[int]bool{}
	for _, cb := range cfg.Budgets {
		idx := findBudget(budgets, cb.Names)
		if idx < 0 {
			name := firstName(cb.Names)
			r.add(Diff{Type: "configured_budget_missing_in_gcp", Severity: SeverityError, ConfiguredName: name, Message: "Config has policy for a budget that does not exist in GCP"})
			continue
		}
		matchedGCP[idx] = true
		gb := budgets[idx]
		if diff, ok := projectDiff(ctx, cb.Projects, gb.Projects, gb, resolver); ok {
			r.add(diff)
		}
		configuredThreshold := cb.Threshold
		if configuredThreshold == 0 {
			configuredThreshold = cfg.Defaults.Threshold
		}
		if !thresholdContains(gb.Thresholds, configuredThreshold) {
			r.add(Diff{Type: "threshold_diff", Severity: SeverityWarning, BudgetDisplayName: gb.DisplayName, BudgetName: gb.Name, GCPThresholds: gb.Thresholds, ConfiguredThreshold: configuredThreshold})
		}
		if cfg.Reconcile.RequiredPubSubTopic != "" && gb.PubSubTopic != cfg.Reconcile.RequiredPubSubTopic {
			r.add(Diff{Type: "pubsub_topic_missing_or_mismatch", Severity: SeverityError, BudgetDisplayName: gb.DisplayName, BudgetName: gb.Name, ExpectedTopic: cfg.Reconcile.RequiredPubSubTopic, ActualTopic: gb.PubSubTopic, Message: "Budget Pub/Sub notification topic does not match required topic"})
		}
	}
	for i, gb := range budgets {
		if !matchedGCP[i] {
			r.add(Diff{Type: "gcp_budget_not_configured", Severity: SeverityWarning, BudgetDisplayName: gb.DisplayName, BudgetName: gb.Name, Message: "Budget exists in GCP but has no enforcement policy in config"})
		}
	}
	return r, nil
}

func (r *Result) add(d Diff) {
	r.Diffs = append(r.Diffs, d)
	if d.Severity == SeverityError {
		r.Summary.Errors++
	} else if d.Severity == SeverityWarning {
		r.Summary.Warnings++
	}
}

func findBudget(budgets []Budget, names []string) int {
	for i, b := range budgets {
		for _, n := range names {
			if n == "" {
				continue
			}
			if n == b.Name || n == b.DisplayName || strings.HasSuffix(b.Name, "/"+n) {
				return i
			}
		}
	}
	return -1
}

func firstName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return names[0]
}

func projectDiff(ctx context.Context, configured, gcp []string, b Budget, resolver ProjectResolver) (Diff, bool) {
	cm := set(configured)
	gm := map[string]bool{}
	for _, p := range gcp {
		id := strings.TrimPrefix(p, "projects/")
		if resolver != nil {
			resolved, err := resolver.ResolveProjectID(ctx, p)
			if err != nil {
				return Diff{Type: "project_scope_diff", Severity: SeverityWarning, BudgetDisplayName: b.DisplayName, BudgetName: b.Name, Message: "resolve GCP budget project " + p + ": " + err.Error()}, true
			}
			if resolved != "" {
				id = strings.TrimPrefix(resolved, "projects/")
			}
		}
		gm[id] = true
	}
	var inGCPNotConfig, inConfigNotGCP []string
	for p := range gm {
		if !cm[p] {
			inGCPNotConfig = append(inGCPNotConfig, p)
		}
	}
	for p := range cm {
		if !gm[p] {
			inConfigNotGCP = append(inConfigNotGCP, p)
		}
	}
	sort.Strings(inGCPNotConfig)
	sort.Strings(inConfigNotGCP)
	if len(inGCPNotConfig) == 0 && len(inConfigNotGCP) == 0 {
		return Diff{}, false
	}
	return Diff{Type: "project_scope_diff", Severity: SeverityWarning, BudgetDisplayName: b.DisplayName, BudgetName: b.Name, ProjectsInGCPNotConfig: inGCPNotConfig, ProjectsInConfigNotGCP: inConfigNotGCP}, true
}

func set(vals []string) map[string]bool {
	m := map[string]bool{}
	for _, v := range vals {
		v = strings.TrimPrefix(v, "projects/")
		if v != "" {
			m[v] = true
		}
	}
	return m
}

func thresholdContains(vals []float64, want float64) bool {
	const epsilon = 0.000001
	for _, v := range vals {
		if v-want < epsilon && want-v < epsilon {
			return true
		}
	}
	return false
}
