package app

import (
	"bytes"
	"context"
	"errors"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/readiness"
	"github.com/example/google-project-cost-manager/internal/reconcile"
	"github.com/google/uuid"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestOlderSlowReconciliationCannotEraseNewerFailure(t *testing.T) {
	for _, route := range []string{"/reconcile", "/self-test"} {
		t.Run(route, func(t *testing.T) { testReconciliationOrder(t, route) })
	}
}

func testReconciliationOrder(t *testing.T, route string) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("local emulator required")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") && !strings.HasPrefix(host, "localhost:") {
		t.Fatal("loopback emulator required")
	}
	cfg := organizationConfig(t, "live")
	cfg.ReadinessMode = config.ReadinessPerProject
	cfg.ReleaseID = uuid.NewString()
	cfg.InventoryObservedAt = time.Now().UTC()
	cfg.Reconcile.Enabled = true
	cfg.EventState.ProjectID = "finops-local-review"
	cfg.EventState.DatabaseID = "(default)"
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	var calls atomic.Int32
	server, err := NewServer(Options{LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }, ReadinessCheck: func(context.Context, *config.Config) error { return nil }, ReconcileLister: fakeBudgetLister{}, ProjectResolver: fakeProjectResolver{}, ReconcileRunner: func(context.Context, *config.Config, reconcile.BudgetLister, reconcile.ProjectResolver) (reconcile.Result, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return reconcile.Result{Summary: reconcile.Summary{ConfiguredBudgets: 1}}, nil
		}
		return reconcile.Result{Summary: reconcile.Summary{ConfiguredBudgets: 1, Errors: 1}, Diffs: []reconcile.Diff{{Severity: reconcile.SeverityError, BudgetName: cfg.Budgets[0].BudgetResourceName}}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer close(done)
		server.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, route, bytes.NewBufferString(`{"mode":"setup_validation"}`)))
	}()
	<-started
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/reconcile", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatal(response.Code, response.Body.String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store, err := readiness.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	before, err := store.ReadProject(ctx, cfg, "p1")
	if err != nil || before.LastPassed {
		t.Fatal("failed preflight not recorded", err)
	}
	close(release)
	<-done
	after, err := store.ReadProject(ctx, cfg, "p1")
	if err != nil {
		t.Fatal(err)
	}
	if after.LastPassed {
		t.Fatal(errors.New("older reconciliation finished late and changed LastPassed from false to true; daily failure remains but qualified readiness now accepts the stale success"))
	}
	if after.Days[time.Now().UTC().Format("2006-01-02")] {
		t.Fatal("daily failure was erased")
	}
}

func TestReconciliationAPIErrorPersistsFailedHealth(t *testing.T) {
	host := os.Getenv("FIRESTORE_EMULATOR_HOST")
	if host == "" {
		t.Skip("local emulator required")
	}
	if !strings.HasPrefix(host, "127.0.0.1:") && !strings.HasPrefix(host, "localhost:") {
		t.Fatal("loopback emulator required")
	}
	cfg := organizationConfig(t, "live")
	cfg.ReadinessMode, cfg.ReleaseID = config.ReadinessPerProject, uuid.NewString()
	cfg.InventoryObservedAt, cfg.Reconcile.Enabled = time.Now().UTC(), true
	cfg.EventState.ProjectID, cfg.EventState.DatabaseID = "finops-local-review", "(default)"
	server, err := NewServer(Options{LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }, ReconcileLister: fakeBudgetLister{}, ProjectResolver: fakeProjectResolver{}, ReconcileRunner: func(context.Context, *config.Config, reconcile.BudgetLister, reconcile.ProjectResolver) (reconcile.Result, error) {
		return reconcile.Result{Summary: reconcile.Summary{ConfiguredBudgets: 1}}, errors.New("partial API failure")
	}})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/reconcile", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatal(response.Code)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	store, err := readiness.New(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	e, err := store.ReadProject(ctx, cfg, "p1")
	if err != nil || e.LastPassed || e.LastReconciledAt.IsZero() {
		t.Fatal("API failure was not persisted", e, err)
	}
}
