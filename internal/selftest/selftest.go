package selftest

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sync"
	"time"

	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/reconcile"
)

type Store interface {
	LastRun(ctx context.Context, key string) (time.Time, error)
	SetLastRun(ctx context.Context, key string, t time.Time) error
}

type MemoryStore struct {
	mu   sync.Mutex
	Last time.Time
}

func (m *MemoryStore) LastRun(context.Context, string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.Last, nil
}
func (m *MemoryStore) SetLastRun(_ context.Context, _ string, t time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Last = t
	return nil
}

type Step struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}
type Result struct {
	Mode       string `json:"mode"`
	FinalState string `json:"finalState,omitempty"`
	Steps      []Step `json:"steps"`
}

type ReconcileRunner func(context.Context, *config.Config) (reconcile.Result, error)

func Run(ctx context.Context, mode string, c *config.Config, bc billing.Client, st Store) Result {
	return RunWithReconciler(ctx, mode, c, bc, st, nil)
}

func RunWithReconciler(ctx context.Context, mode string, c *config.Config, bc billing.Client, st Store, rr ReconcileRunner) Result {
	if mode == "" {
		mode = "dry_run"
	}
	r := Result{Mode: mode}
	add := func(n string, ok bool, detail string, err error) {
		s := Step{Name: n, OK: ok, Detail: detail}
		if err != nil {
			s.Error = err.Error()
		}
		r.Steps = append(r.Steps, s)
	}
	add("config_valid", true, "configuration loaded", nil)
	switch mode {
	case "dry_run":
		add("dry_run", true, "no billing changes attempted", nil)
		return r
	case "billing_read_check":
		p := c.SelfTest.TestProjectID
		if p == "" {
			add("guardrails", false, "", errors.New("selfTest.testProjectId is required"))
			return r
		}
		if bc == nil {
			add("billing_client", false, "", errors.New("billing client is required"))
			return r
		}
		info, err := bc.GetProjectBillingInfo(ctx, p)
		add("billing_read", err == nil, fmt.Sprintf("enabled=%v account=%s", infoVal(info), acctVal(info)), err)
		return r
	case "setup_validation":
		if rr == nil {
			add("reconcile_client", false, "", errors.New("reconcile runner is required"))
			return r
		}
		res, err := rr(ctx, c)
		if err != nil {
			add("reconciliation", false, "", err)
			return r
		}
		if res.Summary.Errors > 0 {
			add("reconciliation", false, fmt.Sprintf("errors=%d warnings=%d", res.Summary.Errors, res.Summary.Warnings), errors.New("setup validation found error severity reconciliation diffs"))
			return r
		}
		add("reconciliation", true, fmt.Sprintf("errors=%d warnings=%d", res.Summary.Errors, res.Summary.Warnings), nil)
		return r
	case "billing_toggle":
		if bc == nil {
			add("billing_client", false, "", errors.New("billing client is required"))
			return r
		}
		if err := guardrails(ctx, c, st); err != nil {
			add("guardrails", false, "", err)
			return r
		}
		add("guardrails", true, "passed", nil)
		p, acct := c.SelfTest.TestProjectID, c.SelfTest.BillingAccountName
		info, err := bc.GetProjectBillingInfo(ctx, p)
		add("billing_read_initial", err == nil, fmt.Sprintf("enabled=%v account=%s", infoVal(info), acctVal(info)), err)
		if err != nil {
			return r
		}
		if info == nil || !info.BillingEnabled || info.BillingAccountName != acct {
			add("expected_billing_link", false, "", errors.New("disposable project is not linked to the expected account"))
			return r
		}
		if err := bc.DisableBilling(ctx, p); err != nil {
			add("disable_billing", false, "", err)
			return r
		}
		add("disable_billing", true, "requested", nil)

		restore := func() bool {
			if err := bc.SetBillingAccount(ctx, p, acct); err != nil {
				add("restore_billing", false, "", err)
				return false
			}
			add("restore_billing", true, acct, nil)
			info, err = bc.GetProjectBillingInfo(ctx, p)
			ok := err == nil && info != nil && info.BillingEnabled && info.BillingAccountName == acct
			add("confirm_enabled", ok, fmt.Sprintf("enabled=%v account=%s", infoVal(info), acctVal(info)), combine(err, ok, "billing not enabled"))
			if ok {
				r.FinalState = "billing_enabled"
			}
			return ok
		}

		info, err = bc.GetProjectBillingInfo(ctx, p)
		ok := err == nil && info != nil && !info.BillingEnabled
		add("confirm_disabled", ok, fmt.Sprintf("enabled=%v", infoVal(info)), combine(err, ok, "billing still enabled"))
		if !ok {
			restore()
			return r
		}
		if restore() && st != nil {
			_ = st.SetLastRun(ctx, "billing_toggle", time.Now())
		}
		return r
	default:
		add("mode", false, "", fmt.Errorf("unsupported mode %q", mode))
		return r
	}
}

func guardrails(ctx context.Context, c *config.Config, st Store) error {
	p := c.SelfTest.TestProjectID
	if p == "" {
		return errors.New("selfTest.testProjectId is required")
	}
	if c.SelfTest.BillingAccountName == "" {
		return errors.New("selfTest.billingAccountName is required for billing_toggle")
	}
	for _, pp := range c.ProtectedProjects {
		if pp == p {
			return errors.New("test project is protected")
		}
	}
	if pat := c.SelfTest.AllowedProjectIDPattern; pat != "" {
		ok, _ := regexp.MatchString(pat, p)
		if !ok {
			return errors.New("test project not allowed by allowedProjectIdPattern")
		}
	}
	if c.SelfTest.MinimumIntervalHours > 0 && st != nil {
		last, _ := st.LastRun(ctx, "billing_toggle")
		if !last.IsZero() && time.Since(last) < time.Duration(c.SelfTest.MinimumIntervalHours)*time.Hour {
			return errors.New("minimumIntervalHours has not elapsed")
		}
	}
	return nil
}
func infoVal(i *billing.ProjectBillingInfo) bool {
	return i != nil && i.BillingEnabled
}
func acctVal(i *billing.ProjectBillingInfo) string {
	if i == nil {
		return ""
	}
	return i.BillingAccountName
}
func combine(err error, ok bool, msg string) error {
	if err != nil {
		return err
	}
	if !ok {
		return errors.New(msg)
	}
	return nil
}
