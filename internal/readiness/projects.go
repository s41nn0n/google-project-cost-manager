package readiness

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/reconcile"
)

// Scope retains every global safety setting and the selected project's terms,
// but excludes unrelated projects/accounts. Release changes still reset evidence.
func Scope(c *config.Config, project string) (*config.Config, error) {
	copy := *c
	copy.Budgets = nil
	for _, b := range c.Budgets {
		if b.ProjectID == project {
			copy.Budgets = append(copy.Budgets, b)
		}
	}
	if len(copy.Budgets) != 1 || project == c.ControlProjectID {
		return nil, errors.New("readiness requires exactly one non-control canonical project")
	}
	copy.Projects = map[string]config.ProjectPolicy{}
	if p, ok := c.Projects[project]; ok {
		copy.Projects[project] = p
	}
	copy.ProtectedProjects = []string{c.ControlProjectID}
	for _, p := range c.ProtectedProjects {
		if p == project {
			copy.ProtectedProjects = append(copy.ProtectedProjects, project)
			break
		}
	}
	sort.Strings(copy.ProtectedProjects)
	copy.Reconcile.BillingAccountName = ""
	copy.Reconcile.BillingAccountNames = []string{copy.Budgets[0].BillingAccountName}
	return &copy, nil
}

func ProjectFingerprint(c *config.Config, project string) (string, error) {
	scoped, err := Scope(c, project)
	if err != nil {
		return "", err
	}
	scoped.ReadinessMode = ""
	return "project-v1-" + Fingerprint(scoped), nil
}

// ProjectResults never turns unattributed or incomplete results into success.
// An error confined to a pending project does not poison unrelated live evidence.
func ProjectResults(c *config.Config, result reconcile.Result, runErr error, now time.Time) map[string]bool {
	passed := map[string]bool{}
	known := map[string]string{}
	complete := runErr == nil && CheckFresh(c, now) == nil && result.Summary.ConfiguredBudgets == len(c.Budgets)
	for _, b := range c.Budgets {
		passed[b.ProjectID] = complete
		known[b.BudgetResourceName] = b.ProjectID
	}
	errorsSeen := 0
	for _, diff := range result.Diffs {
		if diff.Severity != reconcile.SeverityError {
			continue
		}
		errorsSeen++
		project, ok := known[diff.BudgetName]
		if !ok || diff.BudgetName == "" {
			complete = false
		} else {
			passed[project] = false
		}
	}
	if !complete || errorsSeen != result.Summary.Errors {
		for project := range passed {
			passed[project] = false
		}
	}
	return passed
}

func (s *Store) ReadProject(ctx context.Context, c *config.Config, project string) (Evidence, error) {
	key, err := ProjectFingerprint(c, project)
	if err != nil {
		return Evidence{}, err
	}
	return s.readKey(ctx, key)
}

func (s *Store) RecordReconciliation(ctx context.Context, c *config.Config, now time.Time, result reconcile.Result, runErr error) error {
	if c.ReadinessMode == "" {
		passed := runErr == nil && result.Summary.Errors == 0 && CheckFresh(c, now) == nil
		if err := s.RecordDay(ctx, c, now, passed); err != nil {
			return err
		}
	}
	// Also collect real scoped observations in legacy mode for an explicit,
	// same-release migration. No historical days are copied or manufactured.
	for project, passed := range ProjectResults(c, result, runErr, now) {
		key, err := ProjectFingerprint(c, project)
		if err != nil {
			return err
		}
		if err := s.updateKey(ctx, key, func(e *Evidence) { recordDay(e, now, passed) }); err != nil {
			return err
		}
	}
	return nil
}

func checkProjects(c *config.Config, now time.Time, read func(string) (Evidence, error)) error {
	if err := CheckFresh(c, now); err != nil {
		return err
	}
	liveOnly := c.EnforcementEnabled && c.Defaults.DryRun != nil && !*c.Defaults.DryRun
	for _, b := range c.Budgets {
		// Pending dry-run projects may be published alongside verified live ones.
		// Promoting one to live requires its own full evidence first.
		if liveOnly && b.EnforcementMode != "live" {
			continue
		}
		e, err := read(b.ProjectID)
		if err == nil {
			err = Check(e, c, now)
		}
		if err != nil {
			return fmt.Errorf("project %s: %w", b.ProjectID, err)
		}
	}
	return nil
}
