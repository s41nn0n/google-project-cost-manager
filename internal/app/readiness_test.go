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
