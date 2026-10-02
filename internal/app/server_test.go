package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/eventstate"
	"github.com/example/google-project-cost-manager/internal/reconcile"
)

type failingBilling struct{}

func (f failingBilling) GetProjectBillingInfo(context.Context, string) (*billing.ProjectBillingInfo, error) {
	return &billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}, nil
}
func (f failingBilling) DisableBilling(context.Context, string) error { return errors.New("boom") }
func (f failingBilling) SetBillingAccount(context.Context, string, string) error {
	return nil
}

type fakeBudgetLister struct{}

func (fakeBudgetLister) ListBudgets(context.Context, string) ([]reconcile.Budget, error) {
	return nil, nil
}

type fakeProjectResolver struct{}

func (fakeProjectResolver) ResolveProjectID(context.Context, string) (string, error) { return "", nil }

func TestBillingAlertDisableFailureReturns500(t *testing.T) {
	cfg := organizationConfig(t, "live")
	s, err := NewServer(Options{ReadinessCheck: testReady, Billing: failingBilling{}, EventStore: &eventstate.MemoryStore{}, LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }})
	if err != nil {
		t.Fatal(err)
	}
	push := organizationPush(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(push))
	res := httptest.NewRecorder()
	s.Router().ServeHTTP(res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
	if !bytes.Contains(res.Body.Bytes(), []byte("disable_failed: boom")) {
		t.Fatalf("evaluation JSON missing failure: %s", res.Body.String())
	}
}

func TestReconcileConfigLoadFailure(t *testing.T) {
	s, _ := NewServer(Options{LoadConfig: func(context.Context) (*config.Config, error) { return nil, errors.New("load failed") }})
	res := httptest.NewRecorder()
	s.Router().ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/reconcile", nil))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
}

func TestReconcileDisabledReturns400(t *testing.T) {
	cfg, _ := config.Parse([]byte(`budgets: []`))
	s, _ := NewServer(Options{LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }})
	res := httptest.NewRecorder()
	s.Router().ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/reconcile", nil))
	if res.Code != http.StatusBadRequest {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
}

func TestReconcileSuccess(t *testing.T) {
	cfg, _ := config.Parse([]byte(`reconcile: {enabled: true, billingAccountName: billingAccounts/123, requiredPubSubTopic: projects/core/topics/t}
budgets: []`))
	s, _ := NewServer(Options{
		LoadConfig:      func(context.Context) (*config.Config, error) { return cfg, nil },
		ReconcileLister: fakeBudgetLister{}, ProjectResolver: fakeProjectResolver{},
		ReconcileRunner: func(context.Context, *config.Config, reconcile.BudgetLister, reconcile.ProjectResolver) (reconcile.Result, error) {
			return reconcile.Result{SourceOfTruth: "gcp_budgets", Mode: "diff_only"}, nil
		},
	})
	res := httptest.NewRecorder()
	s.Router().ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/reconcile", nil))
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte("gcp_budgets")) {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
}

func TestReconcileRunnerError(t *testing.T) {
	cfg, _ := config.Parse([]byte(`reconcile: {enabled: true, billingAccountName: billingAccounts/123, requiredPubSubTopic: projects/core/topics/t}
budgets: []`))
	s, _ := NewServer(Options{
		LoadConfig:      func(context.Context) (*config.Config, error) { return cfg, nil },
		ReconcileLister: fakeBudgetLister{}, ProjectResolver: fakeProjectResolver{},
		ReconcileRunner: func(context.Context, *config.Config, reconcile.BudgetLister, reconcile.ProjectResolver) (reconcile.Result, error) {
			return reconcile.Result{}, errors.New("reconcile failed")
		},
	})
	res := httptest.NewRecorder()
	s.Router().ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/reconcile", nil))
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
}

func TestSelfTestSetupValidationUsesReconcile(t *testing.T) {
	cfg, _ := config.Parse([]byte(`reconcile: {enabled: true, billingAccountName: billingAccounts/123, requiredPubSubTopic: projects/core/topics/t}
budgets: []`))
	s, _ := NewServer(Options{
		LoadConfig:      func(context.Context) (*config.Config, error) { return cfg, nil },
		ReconcileLister: fakeBudgetLister{}, ProjectResolver: fakeProjectResolver{},
		ReconcileRunner: func(context.Context, *config.Config, reconcile.BudgetLister, reconcile.ProjectResolver) (reconcile.Result, error) {
			return reconcile.Result{Summary: reconcile.Summary{Warnings: 1}}, nil
		},
	})
	body := bytes.NewBufferString(`{"mode":"setup_validation"}`)
	res := httptest.NewRecorder()
	s.Router().ServeHTTP(res, httptest.NewRequest(http.MethodPost, "/self-test", body))
	if res.Code != http.StatusOK || !bytes.Contains(res.Body.Bytes(), []byte("reconciliation")) {
		t.Fatalf("got status %d body %s", res.Code, res.Body.String())
	}
}
