package readiness

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/example/google-project-cost-manager/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TransitionApproval accepts an already validated deployment for one explicitly
// reviewed project addition. It references evidence; it never manufactures days
// or records a new billing-toggle test. There is no HTTP approval endpoint.
type TransitionApproval struct {
	SchemaVersion       int       `firestore:"schemaVersion"`
	TargetFingerprint   string    `firestore:"targetFingerprint"`
	BaselineFingerprint string    `firestore:"baselineFingerprint"`
	BaselineRelease     string    `firestore:"baselineRelease"`
	BaselineObservedAt  time.Time `firestore:"baselineObservedAt"`
	ProjectID           string    `firestore:"projectId"`
	Operator            string    `firestore:"operator"`
	Reason              string    `firestore:"reason"`
	ApprovedAt          time.Time `firestore:"approvedAt"`
	ExpiresAt           time.Time `firestore:"expiresAt"`
}

func ValidateTransition(baseline, target *config.Config, project string) error {
	if baseline.ReadinessMode != "" || target.ReadinessMode != "" ||
		baseline.SchemaVersion != 2 || target.SchemaVersion != 2 ||
		!baseline.EnforcementEnabled || !target.EnforcementEnabled ||
		baseline.Defaults.DryRun == nil || *baseline.Defaults.DryRun ||
		target.Defaults.DryRun == nil || *target.Defaults.DryRun ||
		project == "" || project == target.ControlProjectID ||
		len(target.Budgets) != len(baseline.Budgets)+1 {
		return errors.New("approval requires two live legacy policies and exactly one non-control project addition")
	}
	old := map[string]config.Budget{}
	for _, b := range baseline.Budgets {
		if _, exists := old[b.ProjectID]; exists || b.ProjectID == project {
			return errors.New("baseline has duplicate projects or already contains the new project")
		}
		old[b.ProjectID] = b
	}
	seen := map[string]bool{}
	for _, b := range target.Budgets {
		if seen[b.ProjectID] {
			return errors.New("target contains duplicate projects")
		}
		seen[b.ProjectID] = true
		if b.ProjectID == project {
			if b.EnforcementMode != "live" || b.BudgetResourceName == "" || len(b.Projects) != 1 || b.Projects[0] != project {
				return errors.New("new project must have one live canonical budget")
			}
		} else if before, ok := old[b.ProjectID]; !ok || !reflect.DeepEqual(before, b) {
			return errors.New("existing project terms must remain unchanged")
		}
	}
	if !seen[project] {
		return errors.New("new project is missing")
	}
	// Only the release and the single new canonical budget may change. Policy
	// freshness and enforcement toggles are already normalized by Fingerprint.
	copy := *baseline
	copy.ReleaseID, copy.Budgets = target.ReleaseID, target.Budgets
	if Fingerprint(&copy) != Fingerprint(target) {
		return errors.New("transition changes global safety settings or protection rules")
	}
	return nil
}

func checkObservedDay(e Evidence, now time.Time) error {
	today := now.UTC().Format("2006-01-02")
	if passed, exists := e.Days[today]; exists {
		if !passed {
			return errors.New("today's reconciliation failed")
		}
		return nil
	}
	if !e.Days[now.UTC().AddDate(0, 0, -1).Format("2006-01-02")] {
		return errors.New("a current successful target reconciliation is required")
	}
	return nil
}

func checkTransitionApproval(a TransitionApproval, c *config.Config, current, baseline Evidence, now time.Time) error {
	if err := CheckFresh(c, now); err != nil {
		return err
	}
	if c.ReadinessMode != "" || a.SchemaVersion != 1 || a.TargetFingerprint != Fingerprint(c) ||
		!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(a.BaselineFingerprint) ||
		a.BaselineFingerprint == a.TargetFingerprint || a.BaselineRelease == "" ||
		a.ProjectID == "" || a.ProjectID == c.ControlProjectID ||
		strings.TrimSpace(a.Operator) == "" || strings.TrimSpace(a.Reason) == "" ||
		a.ApprovedAt.IsZero() || a.ApprovedAt.After(now.Add(5*time.Minute)) ||
		!a.ExpiresAt.After(now) || a.ExpiresAt.After(a.ApprovedAt.Add(30*24*time.Hour)) ||
		a.ExpiresAt.After(baseline.ToggleAt.Add(30*24*time.Hour)) {
		return errors.New("missing, expired, or mismatched operator transition approval")
	}
	// Validate the original seven real dates at the moment of operator approval.
	// The original test still expires at its original timestamp, never renewed.
	source := *c
	source.ReleaseID, source.InventoryObservedAt = a.BaselineRelease, a.BaselineObservedAt
	if err := Check(baseline, &source, a.ApprovedAt); err != nil {
		return fmt.Errorf("approved baseline: %w", err)
	}
	if now.Sub(baseline.ToggleAt) > 30*24*time.Hour {
		return errors.New("original disposable-project test has expired")
	}
	// An approval accepts missing history, never a failure observed after it.
	for day, passed := range current.Days {
		if !passed && day >= a.ApprovedAt.UTC().Format("2006-01-02") {
			return errors.New("target reconciliation failed after operator approval")
		}
	}
	return checkObservedDay(current, now)
}

// ApproveTransition is an explicit operator operation. Create is append-only;
// rerunning this CLI cannot extend an existing approval or erase a failed day.
func (s *Store) ApproveTransition(ctx context.Context, baseline, target *config.Config, project, operator, reason string) error {
	if err := ValidateTransition(baseline, target, project); err != nil {
		return err
	}
	now := time.Now().UTC()
	before, err := s.Read(ctx, baseline)
	if err != nil {
		return err
	}
	if err := Check(before, baseline, now); err != nil {
		return fmt.Errorf("baseline must pass the original gates without an exception: %w", err)
	}
	current, err := s.Read(ctx, target)
	if err != nil {
		return err
	}
	// The initial approval requires today's actual reconciliation, not yesterday.
	if !current.Days[now.Format("2006-01-02")] {
		return errors.New("today's successful target reconciliation is required")
	}
	a := TransitionApproval{
		SchemaVersion: 1, TargetFingerprint: Fingerprint(target), BaselineFingerprint: Fingerprint(baseline),
		BaselineRelease: baseline.ReleaseID, BaselineObservedAt: baseline.InventoryObservedAt,
		ProjectID: project, Operator: operator, Reason: reason, ApprovedAt: now,
		ExpiresAt: before.ToggleAt.Add(30 * 24 * time.Hour),
	}
	if err := checkTransitionApproval(a, target, current, before, now); err != nil {
		return err
	}
	_, err = s.client.Collection("operator-readiness-approvals").Doc(a.TargetFingerprint).Create(ctx, a)
	return err
}

func (s *Store) verifyTransitionApproval(ctx context.Context, c *config.Config, current Evidence, now time.Time, original error) error {
	snap, err := s.client.Collection("operator-readiness-approvals").Doc(Fingerprint(c)).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return original
	}
	if err != nil {
		return err
	}
	var a TransitionApproval
	if err := snap.DataTo(&a); err != nil {
		return err
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(a.BaselineFingerprint) {
		return errors.New("invalid approved baseline key")
	}
	baseline, err := s.readKey(ctx, a.BaselineFingerprint)
	if err != nil {
		return err
	}
	return checkTransitionApproval(a, c, current, baseline, now)
}
