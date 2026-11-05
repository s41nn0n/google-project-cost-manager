package readiness

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/example/google-project-cost-manager/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SystemQualification references completed initial validation. It authorizes
// one reviewed release and global safety configuration, not arbitrary policies.
// Project terms and permissions are rechecked by real runtime reconciliation.
type SystemQualification struct {
	SchemaVersion       int       `firestore:"schemaVersion"`
	SystemFingerprint   string    `firestore:"systemFingerprint"`
	BaselineFingerprint string    `firestore:"baselineFingerprint"`
	BaselineRelease     string    `firestore:"baselineRelease"`
	BaselineObservedAt  time.Time `firestore:"baselineObservedAt"`
	ValidationApproval  string    `firestore:"validationApproval"`
	ValidatedAt         time.Time `firestore:"validatedAt"`
	QualifiedAt         time.Time `firestore:"qualifiedAt"`
	Operator            string    `firestore:"operator"`
	Reason              string    `firestore:"reason"`
}

func SystemFingerprint(c *config.Config) string {
	copy := *c
	copy.ReadinessMode = config.ReadinessPerProject
	copy.Budgets, copy.Projects = nil, nil
	copy.ProtectedProjects = []string{c.ControlProjectID}
	copy.Reconcile.BillingAccountName, copy.Reconcile.BillingAccountNames = "", nil
	return "system-v1-" + Fingerprint(&copy)
}

func validateQualification(q SystemQualification, c *config.Config, baseline Evidence, now time.Time) error {
	if c.ReadinessMode != config.ReadinessPerProject || q.SchemaVersion != 1 ||
		q.SystemFingerprint != SystemFingerprint(c) ||
		!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(q.BaselineFingerprint) ||
		!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(q.ValidationApproval) ||
		strings.TrimSpace(q.Operator) == "" || strings.TrimSpace(q.Reason) == "" ||
		q.QualifiedAt.IsZero() || q.QualifiedAt.After(now.Add(5*time.Minute)) ||
		q.ValidatedAt.IsZero() || q.ValidatedAt.After(q.QualifiedAt) {
		return errors.New("invalid or mismatched system qualification")
	}
	// The original seven dates and observed toggle must have passed at the
	// recorded validation time. This does not create a new toggle or renew it.
	source := *c
	source.ReleaseID, source.InventoryObservedAt = q.BaselineRelease, q.BaselineObservedAt
	if err := Check(baseline, &source, q.ValidatedAt); err != nil {
		return fmt.Errorf("initial validation: %w", err)
	}
	return nil
}

func checkCurrentObservation(e Evidence, now time.Time) error {
	if e.LastReconciledAt.IsZero() || !e.LastPassed ||
		e.LastReconciledAt.After(now.Add(5*time.Minute)) || now.Sub(e.LastReconciledAt) > 36*time.Hour {
		return errors.New("a successful project reconciliation within 36 hours is required")
	}
	return nil
}

// QualifySystem is an explicit operator migration. The historical approval is
// used only as proof of initial validation at its original time. Its failed
// target is never treated as currently healthy. No rollout evidence is edited.
func (s *Store) QualifySystem(ctx context.Context, baseline, target *config.Config, approvalKey, operator, reason string) error {
	if target.ReadinessMode != config.ReadinessPerProject || target.SchemaVersion != 2 ||
		!regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(approvalKey) ||
		strings.TrimSpace(operator) == "" || strings.TrimSpace(reason) == "" {
		return errors.New("canonical per-project target, historical approval, operator and reason are required")
	}
	if err := CheckFresh(target, time.Now().UTC()); err != nil {
		return err
	}
	source := *baseline
	source.ReleaseID = target.ReleaseID
	if SystemFingerprint(&source) != SystemFingerprint(target) {
		return errors.New("global safety settings differ from the initially validated system")
	}
	snap, err := s.client.Collection("operator-readiness-approvals").Doc(approvalKey).Get(ctx)
	if err != nil {
		return err
	}
	var approval TransitionApproval
	if err := snap.DataTo(&approval); err != nil {
		return err
	}
	if approval.SchemaVersion != 1 || approval.TargetFingerprint != approvalKey ||
		approval.BaselineFingerprint != Fingerprint(baseline) || approval.BaselineRelease != baseline.ReleaseID ||
		!approval.BaselineObservedAt.Equal(baseline.InventoryObservedAt.Truncate(time.Microsecond)) || approval.ApprovedAt.IsZero() {
		return errors.New("historical approval does not reference the supplied baseline")
	}
	before, err := s.Read(ctx, baseline)
	if err != nil {
		return err
	}
	q := SystemQualification{
		SchemaVersion: 1, SystemFingerprint: SystemFingerprint(target),
		BaselineFingerprint: Fingerprint(baseline), BaselineRelease: baseline.ReleaseID,
		BaselineObservedAt: baseline.InventoryObservedAt, ValidationApproval: approvalKey,
		ValidatedAt: approval.ApprovedAt, QualifiedAt: time.Now().UTC(), Operator: operator, Reason: reason,
	}
	if err := validateQualification(q, target, before, time.Now().UTC()); err != nil {
		return err
	}
	// Qualification is only issued after this release's actual target preflight.
	for _, budget := range target.Budgets {
		e, err := s.ReadProject(ctx, target, budget.ProjectID)
		if err != nil {
			return err
		}
		if err := checkCurrentObservation(e, time.Now().UTC()); err != nil {
			return fmt.Errorf("project %s: %w", budget.ProjectID, err)
		}
	}
	_, err = s.client.Collection("system-qualifications").Doc(q.SystemFingerprint).Create(ctx, q)
	return err
}

func (s *Store) verifyQualifiedSystem(ctx context.Context, c *config.Config, now time.Time) (bool, error) {
	snap, err := s.client.Collection("system-qualifications").Doc(SystemFingerprint(c)).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var q SystemQualification
	if err := snap.DataTo(&q); err != nil {
		return true, err
	}
	if !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(q.BaselineFingerprint) {
		return true, errors.New("invalid qualification baseline")
	}
	baseline, err := s.readKey(ctx, q.BaselineFingerprint)
	if err != nil {
		return true, err
	}
	if err := validateQualification(q, c, baseline, now); err != nil {
		return true, err
	}
	liveOnly := c.EnforcementEnabled && c.Defaults.DryRun != nil && !*c.Defaults.DryRun
	for _, budget := range c.Budgets {
		if liveOnly && budget.EnforcementMode != "live" {
			continue
		}
		e, err := s.ReadProject(ctx, c, budget.ProjectID)
		if err == nil {
			err = checkCurrentObservation(e, now)
		}
		if err != nil {
			return true, fmt.Errorf("project %s: %w", budget.ProjectID, err)
		}
	}
	return true, nil
}
