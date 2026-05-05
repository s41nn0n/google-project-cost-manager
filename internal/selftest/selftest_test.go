package selftest

import (
	"context"
	"errors"
	"testing"

	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/reconcile"
)

type fakeBilling struct {
	enabled bool
	account string
	getErr  error
}

func (f *fakeBilling) GetProjectBillingInfo(context.Context, string) (*billing.ProjectBillingInfo, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return &billing.ProjectBillingInfo{BillingEnabled: f.enabled, BillingAccountName: f.account}, nil
}
func (f *fakeBilling) DisableBilling(context.Context, string) error {
	f.enabled = false
	f.account = ""
	return nil
}
func (f *fakeBilling) SetBillingAccount(_ context.Context, _ string, account string) error {
	f.enabled = true
	f.account = account
	return nil
}

func TestDryRunNoBillingNeeded(t *testing.T) {
	c, _ := config.Parse([]byte(`budgets: []`))
	r := Run(context.Background(), "dry_run", c, nil, &MemoryStore{})
	if len(r.Steps) < 2 || !r.Steps[1].OK {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestSetupValidationNoErrorsSucceeds(t *testing.T) {
	c, _ := config.Parse([]byte(`reconcile: {enabled: true, billingAccountName: billingAccounts/123, requiredPubSubTopic: projects/core/topics/t}`))
	r := RunWithReconciler(context.Background(), "setup_validation", c, nil, &MemoryStore{}, func(context.Context, *config.Config) (reconcile.Result, error) {
		return reconcile.Result{}, nil
	})
	if !hasStep(r, "reconciliation", true) {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestSetupValidationErrorDiffFails(t *testing.T) {
	c, _ := config.Parse([]byte(`reconcile: {enabled: true, billingAccountName: billingAccounts/123, requiredPubSubTopic: projects/core/topics/t}`))
	r := RunWithReconciler(context.Background(), "setup_validation", c, nil, &MemoryStore{}, func(context.Context, *config.Config) (reconcile.Result, error) {
		return reconcile.Result{Summary: reconcile.Summary{Errors: 1}}, nil
	})
	if !hasStep(r, "reconciliation", false) {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestSetupValidationWarningsOnlySucceeds(t *testing.T) {
	c, _ := config.Parse([]byte(`reconcile: {enabled: true, billingAccountName: billingAccounts/123, requiredPubSubTopic: projects/core/topics/t}`))
	r := RunWithReconciler(context.Background(), "setup_validation", c, nil, &MemoryStore{}, func(context.Context, *config.Config) (reconcile.Result, error) {
		return reconcile.Result{Summary: reconcile.Summary{Warnings: 1}}, nil
	})
	if !hasStep(r, "reconciliation", true) {
		t.Fatalf("unexpected result: %+v", r)
	}
}

func TestBillingToggleRestores(t *testing.T) {
	c, _ := config.Parse([]byte(`selfTest:
  testProjectId: test-project
  billingAccountName: billingAccounts/123
  allowedProjectIdPattern: ^test-
`))
	fb := &fakeBilling{enabled: true, account: "billingAccounts/123"}
	r := Run(context.Background(), "billing_toggle", c, fb, &MemoryStore{})
	if r.FinalState != "billing_enabled" || !fb.enabled || fb.account != "billingAccounts/123" {
		t.Fatalf("unexpected result: %+v fake=%+v", r, fb)
	}
}

func TestBillingToggleRestoresAfterConfirmDisabledReadFailure(t *testing.T) {
	c, _ := config.Parse([]byte(`selfTest:
  testProjectId: test-project
  billingAccountName: billingAccounts/123
`))
	fb := &failingSecondReadBilling{fakeBilling: fakeBilling{enabled: true, account: "billingAccounts/123"}}
	r := Run(context.Background(), "billing_toggle", c, fb, &MemoryStore{})
	if r.FinalState != "billing_enabled" || !fb.enabled || fb.account != "billingAccounts/123" {
		t.Fatalf("billing was not restored: result=%+v fake=%+v", r, fb.fakeBilling)
	}
	if !hasStep(r, "confirm_disabled", false) || !hasStep(r, "restore_billing", true) {
		t.Fatalf("expected confirm_disabled failure and restore success: %+v", r.Steps)
	}
}

type failingSecondReadBilling struct {
	fakeBilling
	reads int
}

func (f *failingSecondReadBilling) GetProjectBillingInfo(ctx context.Context, project string) (*billing.ProjectBillingInfo, error) {
	f.reads++
	if f.reads == 2 {
		return nil, errors.New("read failed")
	}
	return f.fakeBilling.GetProjectBillingInfo(ctx, project)
}

func hasStep(r Result, name string, ok bool) bool {
	for _, s := range r.Steps {
		if s.Name == name && s.OK == ok {
			return true
		}
	}
	return false
}
