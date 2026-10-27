package app

import (
	"bytes"
	"context"
	"errors"
	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/eventstate"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
