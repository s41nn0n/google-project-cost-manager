// Package readiness stores rollout evidence outside mutable GitHub variables.
package readiness

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"cloud.google.com/go/firestore"
	"github.com/example/google-project-cost-manager/internal/config"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Evidence struct {
	Days          map[string]bool `json:"days" firestore:"days"`
	ToggleProject string          `json:"toggleProject" firestore:"toggleProject"`
	ToggleAt      time.Time       `json:"toggleAt" firestore:"toggleAt"`
}

func Fingerprint(c *config.Config) string {
	copy := *c
	copy.EnforcementEnabled = false
	copy.InventoryObservedAt = time.Time{}
	copy.Defaults.DryRun = config.Bool(true)
	copy.Budgets = append([]config.Budget(nil), c.Budgets...)
	for i := range copy.Budgets {
		copy.Budgets[i].EnforcementMode = "dry_run"
		copy.Budgets[i].DryRun = nil
	}
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func CheckFresh(c *config.Config, now time.Time) error {
	if c.ReleaseID == "" || c.InventoryObservedAt.IsZero() {
		return errors.New("release identity and inventory observation are required")
	}
	age := now.Sub(c.InventoryObservedAt)
	if age < -5*time.Minute || age > 48*time.Hour {
		return errors.New("stale or future inventory observation")
	}
	return nil
}

func Check(e Evidence, c *config.Config, now time.Time) error {
	if err := CheckFresh(c, now); err != nil {
		return err
	}
	if e.ToggleProject == "" || e.ToggleProject == c.ControlProjectID || e.ToggleAt.IsZero() || e.ToggleAt.After(now.Add(5*time.Minute)) || now.Sub(e.ToggleAt) > 30*24*time.Hour {
		return errors.New("verified recent disposable-project toggle evidence is missing")
	}
	end := now.UTC()
	today := end.Format("2006-01-02")
	if passed, ok := e.Days[today]; ok && !passed {
		return errors.New("today's reconciliation failed")
	}
	if !e.Days[today] {
		end = end.AddDate(0, 0, -1)
	}
	for i := 0; i < 7; i++ {
		if !e.Days[end.AddDate(0, 0, -i).Format("2006-01-02")] {
			return errors.New("seven consecutive successful daily reconciliations are required")
		}
	}
	return nil
}

type Store struct{ client *firestore.Client }

func New(ctx context.Context, c *config.Config) (*Store, error) {
	if c.EventState.Backend != "firestore" {
		return nil, errors.New("readiness requires Firestore")
	}
	client, err := firestore.NewClientWithDatabase(ctx, c.EventState.ProjectID, c.EventState.DatabaseID)
	if err != nil {
		return nil, err
	}
	return &Store{client: client}, nil
}
func (s *Store) Close() error { return s.client.Close() }
func (s *Store) ref(c *config.Config) *firestore.DocumentRef {
	return s.client.Collection("rollout-evidence").Doc(Fingerprint(c))
}
func (s *Store) Read(ctx context.Context, c *config.Config) (Evidence, error) {
	snap, err := s.ref(c).Get(ctx)
	if status.Code(err) == codes.NotFound {
		return Evidence{Days: map[string]bool{}}, nil
	}
	if err != nil {
		return Evidence{}, err
	}
	var e Evidence
	err = snap.DataTo(&e)
	return e, err
}
func (s *Store) update(ctx context.Context, c *config.Config, f func(*Evidence)) error {
	ref := s.ref(c)
	return s.client.RunTransaction(ctx, func(ctx context.Context, tx *firestore.Transaction) error {
		e := Evidence{Days: map[string]bool{}}
		snap, err := tx.Get(ref)
		if err == nil {
			if err := snap.DataTo(&e); err != nil {
				return err
			}
		} else if status.Code(err) != codes.NotFound {
			return err
		}
		if e.Days == nil {
			e.Days = map[string]bool{}
		}
		f(&e)
		return tx.Set(ref, e)
	})
}
func (s *Store) RecordDay(ctx context.Context, c *config.Config, now time.Time, passed bool) error {
	return s.update(ctx, c, func(e *Evidence) {
		day := now.UTC().Format("2006-01-02")
		prior, exists := e.Days[day]
		e.Days[day] = passed && (!exists || prior)
	})
}

// RecordToggle is only used by the explicit operator CLI after an observed cycle.
func (s *Store) RecordToggle(ctx context.Context, c *config.Config, project string, now time.Time) error {
	if project == "" || project == c.ControlProjectID {
		return fmt.Errorf("invalid disposable project")
	}
	return s.update(ctx, c, func(e *Evidence) { e.ToggleProject = project; e.ToggleAt = now.UTC() })
}
func Verify(ctx context.Context, c *config.Config) error {
	if err := CheckFresh(c, time.Now().UTC()); err != nil {
		return err
	}
	s, err := New(ctx, c)
	if err != nil {
		return err
	}
	defer s.Close()
	e, err := s.Read(ctx, c)
	if err != nil {
		return err
	}
	return Check(e, c, time.Now().UTC())
}

// The operator stop is independent of policy publication, so an in-flight CI run
// cannot undo an emergency stop by appending another policy secret version.
func (s *Store) SetEnabled(ctx context.Context, enabled bool) error {
	_, err := s.client.Collection("operator-controls").Doc("enforcement").Set(ctx, map[string]any{"enabled": enabled, "updatedAt": time.Now().UTC()})
	return err
}
func VerifyEnforcement(ctx context.Context, c *config.Config) error {
	if err := Verify(ctx, c); err != nil {
		return err
	}
	s, err := New(ctx, c)
	if err != nil {
		return err
	}
	defer s.Close()
	snap, err := s.client.Collection("operator-controls").Doc("enforcement").Get(ctx)
	if err != nil {
		return fmt.Errorf("operator enforcement switch unavailable or disabled: %w", err)
	}
	enabled, err := snap.DataAt("enabled")
	if err != nil || enabled != true {
		return errors.New("operator enforcement switch is disabled")
	}
	return nil
}
