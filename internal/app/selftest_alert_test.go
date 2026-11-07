package app

import (
	"bytes"
	"context"
	"errors"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/reconcile"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSelfTestReconcileFailureEmitsSafetyAlert(t *testing.T) {
	for _, failure := range []string{"diff", "api", "config"} {
		t.Run(failure, func(t *testing.T) { testSelfTestFailureAlert(t, failure) })
	}
}

func testSelfTestFailureAlert(t *testing.T, failure string) {
	cfg := organizationConfig(t, "live")
	cfg.ReleaseID = "review"
	cfg.InventoryObservedAt = time.Now().UTC()
	recorded := false
	server, err := NewServer(Options{RecordReconciliation: func(_ context.Context, _ *config.Config, started, completed time.Time, result reconcile.Result, runErr error) error {
		recorded = true
		if completed.Before(started) {
			t.Fatal("observation interval is reversed")
		}
		if runErr == nil && result.Summary.Errors == 0 {
			t.Fatal("failed check recorded as success")
		}
		return nil
	}, LoadConfig: func(context.Context) (*config.Config, error) {
		if failure == "config" {
			return nil, errors.New("policy unavailable")
		}
		return cfg, nil
	}, ReadinessCheck: func(context.Context, *config.Config) error { return nil }, ReconcileLister: fakeBudgetLister{}, ProjectResolver: fakeProjectResolver{}, ReconcileRunner: func(context.Context, *config.Config, reconcile.BudgetLister, reconcile.ProjectResolver) (reconcile.Result, error) {
		if failure == "api" {
			return reconcile.Result{}, errors.New("budget API unavailable")
		}
		return reconcile.Result{Summary: reconcile.Summary{ConfiguredBudgets: 1, Errors: 1}}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	original := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = original; reader.Close(); writer.Close() }()
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/self-test", bytes.NewBufferString(`{"mode":"setup_validation"}`)))
	writer.Close()
	os.Stdout = original
	logs, _ := io.ReadAll(reader)
	expected := http.StatusServiceUnavailable
	if failure == "config" {
		expected = http.StatusInternalServerError
	}
	if response.Code != expected {
		t.Fatalf("expected failed self-test: %d", response.Code)
	}
	if !strings.Contains(string(logs), `"alert_kind":"reconciliation_error"`) {
		t.Fatalf("self-test returned HTTP 503 (%s), but emitted no monitored safety alert: %q", response.Body.String(), logs)
	}
	if failure != "config" && !recorded {
		t.Fatal("failed setup validation did not update current evidence")
	}
}
