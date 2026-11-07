package app

import (
	"bytes"
	"context"
	"errors"
	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/eventstate"
	"github.com/example/google-project-cost-manager/internal/reconcile"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthAndSetupValidationExposeBlockedEnforcement(t *testing.T) {
	for _, ready := range []bool{false, true} {
		cfg := organizationConfig(t, "live")
		cfg.ReleaseID, cfg.InventoryObservedAt = "release", time.Now().UTC()
		calls := 0
		s, _ := NewServer(Options{
			LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil },
			ReadinessCheck: func(context.Context, *config.Config) error {
				calls++
				if !ready {
					return errors.New("current project preflight failed")
				}
				return nil
			},
			ReconcileLister: fakeBudgetLister{}, ProjectResolver: fakeProjectResolver{},
			RecordReconciliation: func(context.Context, *config.Config, time.Time, time.Time, reconcile.Result, error) error { return nil },
			ReconcileRunner: func(context.Context, *config.Config, reconcile.BudgetLister, reconcile.ProjectResolver) (reconcile.Result, error) {
				return reconcile.Result{Summary: reconcile.Summary{ConfiguredBudgets: 1}}, nil
			},
		})
		for _, request := range []*http.Request{
			httptest.NewRequest(http.MethodGet, "/readyz", nil),
			httptest.NewRequest(http.MethodPost, "/self-test", bytes.NewBufferString(`{"mode":"setup_validation"}`)),
		} {
			response := httptest.NewRecorder()
			s.Router().ServeHTTP(response, request)
			expected := http.StatusServiceUnavailable
			if ready {
				expected = http.StatusOK
			}
			if response.Code != expected {
				t.Fatalf("ready=%v path=%s status=%d body=%s", ready, request.URL.Path, response.Code, response.Body.String())
			}
		}
		if calls != 2 {
			t.Fatal("health checks omitted live enforcement readiness")
		}
		cfg.InventoryObservedAt = time.Now().Add(-49 * time.Hour)
		response := httptest.NewRecorder()
		s.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/self-test", bytes.NewBufferString(`{"mode":"setup_validation"}`)))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatal("stale inventory reported healthy")
		}
	}
}

func TestMissingReadinessCannotDisable(t *testing.T) {
	cfg := organizationConfig(t, "live")
	client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}}
	s, _ := NewServer(Options{Billing: client, EventStore: &eventstate.MemoryStore{}, LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }, ReadinessCheck: func(context.Context, *config.Config) error { return errors.New("seven verified days missing") }})
	response := httptest.NewRecorder()
	s.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(organizationPush(t, nil))))
	if response.Code != 500 || client.disableCalls != 0 {
		t.Fatalf("missing readiness bypassed: %d calls=%d", response.Code, client.disableCalls)
	}
}

func TestProjectReadinessChecksOnlyTargetAtBothFences(t *testing.T) {
	cfg := organizationConfig(t, "live")
	cfg.ReadinessMode = config.ReadinessPerProject
	pending := cfg.Budgets[0]
	pending.ProjectID, pending.ProjectNumber, pending.Projects = "pending", "222", []string{"pending"}
	pending.BudgetResourceName, pending.EnforcementMode = "billingAccounts/AAA/budgets/pending", "dry_run"
	cfg.Budgets = append(cfg.Budgets, pending)
	client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}}
	calls := 0
	s, err := NewServer(Options{Billing: client, EventStore: &eventstate.MemoryStore{}, LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }, ReadinessCheck: func(_ context.Context, scoped *config.Config) error {
		calls++
		if len(scoped.Budgets) != 1 || scoped.Budgets[0].ProjectID != "p1" {
			return errors.New("unrelated pending evidence was checked")
		}
		return nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	s.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(organizationPush(t, nil))))
	if response.Code != 200 || client.disableCalls != 1 || calls != 2 {
		t.Fatalf("status=%d calls=%d checks=%d body=%s", response.Code, client.disableCalls, calls, response.Body.String())
	}
}

func TestProjectReadinessStillChecksStopImmediatelyBeforeUnlink(t *testing.T) {
	cfg := organizationConfig(t, "live")
	cfg.ReadinessMode = config.ReadinessPerProject
	client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}}
	calls := 0
	s, _ := NewServer(Options{Billing: client, EventStore: &eventstate.MemoryStore{}, LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }, ReadinessCheck: func(context.Context, *config.Config) error {
		calls++
		if calls == 2 {
			return errors.New("operator stopped enforcement")
		}
		return nil
	}})
	response := httptest.NewRecorder()
	s.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(organizationPush(t, nil))))
	if response.Code != 500 || client.disableCalls != 0 || calls != 2 {
		t.Fatalf("stop bypassed: status=%d calls=%d checks=%d", response.Code, client.disableCalls, calls)
	}
}
