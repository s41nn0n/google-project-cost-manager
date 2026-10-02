package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/eventstate"
	"github.com/example/google-project-cost-manager/internal/pubsub"
)

func TestGlobalStopsCannotBeOverridden(t *testing.T) {
	for _, stop := range []string{"dry_run", "disabled"} {
		t.Run(stop, func(t *testing.T) {
			cfg := organizationConfig(t, "live")
			cfg.Budgets[0].DryRun = config.Bool(false)
			cfg.Projects["p1"] = config.ProjectPolicy{DryRun: config.Bool(false)}
			if stop == "dry_run" {
				cfg.Defaults.DryRun = config.Bool(true)
			} else {
				cfg.EnforcementEnabled = false
			}
			client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}}
			response := serveOrganization(t, cfg, client, organizationPush(t, nil))
			if response.Code != 200 || client.disableCalls != 0 {
				t.Fatalf("global stop bypassed: %d calls=%d", response.Code, client.disableCalls)
			}
		})
	}
}

func TestBusyClaimRequestsRedelivery(t *testing.T) {
	cfg := organizationConfig(t, "live")
	body := organizationPush(t, nil)
	envelope, err := pubsub.DecodeEnvelope(body)
	if err != nil {
		t.Fatal(err)
	}
	start, _ := envelope.Alert.PeriodStart()
	store := &eventstate.MemoryStore{}
	event := eventstate.Event{MessageID: envelope.MessageID, PublishTime: envelope.PublishTime, CostAmount: 80}
	_, err = store.Claim(context.Background(), eventstate.Key(cfg.Budgets[0].BudgetResourceName, "p1", start.Format("2006-01")), &event)
	if err != nil {
		t.Fatal(err)
	}
	client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}}
	server, _ := NewServer(Options{ReadinessCheck: testReady, Billing: client, EventStore: store, LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }})
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(body)))
	if response.Code < 500 || client.disableCalls != 0 {
		t.Fatalf("unfinished event acknowledged: %d calls=%d", response.Code, client.disableCalls)
	}
}
