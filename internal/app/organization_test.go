package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/eventstate"
)

type trackingBilling struct {
	info         billing.ProjectBillingInfo
	disableCalls int
}

func testReady(context.Context, *config.Config) error { return nil }

func (b *trackingBilling) GetProjectBillingInfo(context.Context, string) (*billing.ProjectBillingInfo, error) {
	copy := b.info
	return &copy, nil
}
func (b *trackingBilling) DisableBilling(context.Context, string) error {
	b.disableCalls++
	b.info.BillingEnabled = false
	b.info.BillingAccountName = ""
	return nil
}
func (b *trackingBilling) SetBillingAccount(context.Context, string, string) error { return nil }

func organizationConfig(t *testing.T, mode string) *config.Config {
	t.Helper()
	yaml := `schemaVersion: 2
organizationId: "123"
enforcementEnabled: true
controlProjectId: control
defaults: {threshold: 0.8, dryRun: false, action: disable_billing}
unknownAlertPolicy: ignore
maxProjectsDisabledPerEvent: 1
protectedProjects: [control]
eventState: {backend: firestore, projectId: control, databaseId: billing-guard-events, collection: events}
budgets:
- budgetResourceName: billingAccounts/AAA/budgets/BBB
  billingAccountName: billingAccounts/AAA
  projectId: p1
  projectNumber: "111"
  enforcementMode: ` + mode + `
  projects: [p1]
  threshold: 0.8
  monthlyAmount: 100
  currencyCode: USD
`
	cfg, err := config.Parse([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func organizationPush(t *testing.T, overrides map[string]any) []byte {
	t.Helper()
	alert := map[string]any{
		"billingAccountId": "AAA", "budgetId": "BBB", "costAmount": 80.0,
		"budgetAmount": 100.0, "alertThresholdExceeded": 0.8,
		"currencyCode":      "USD",
		"costIntervalStart": time.Date(time.Now().UTC().Year(), time.Now().UTC().Month(), 1, 0, 0, 0, 0, time.UTC).Format(time.RFC3339),
	}
	for key, value := range overrides {
		alert[key] = value
	}
	data, _ := json.Marshal(alert)
	body, _ := json.Marshal(map[string]any{"message": map[string]any{
		"data": base64.StdEncoding.EncodeToString(data), "messageId": "message-one",
		"publishTime": time.Now().UTC().Format(time.RFC3339Nano),
	}})
	return body
}

func serveOrganization(t *testing.T, cfg *config.Config, client *trackingBilling, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	server, err := NewServer(Options{ReadinessCheck: testReady, Billing: client, EventStore: &eventstate.MemoryStore{}, LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(body)))
	return response
}

func TestCanonicalLiveEventDisablesExpectedBillingLink(t *testing.T) {
	client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}}
	response := serveOrganization(t, organizationConfig(t, "live"), client, organizationPush(t, nil))
	if response.Code != http.StatusOK || client.disableCalls != 1 || !bytes.Contains(response.Body.Bytes(), []byte("billing_disabled")) {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, client.disableCalls, response.Body.String())
	}
}

func TestCanonicalEventRejectsWrongAccountStaleForecastAndUnknown(t *testing.T) {
	cases := []struct {
		name        string
		infoAccount string
		overrides   map[string]any
		reason      string
	}{
		{"wrong linked account", "billingAccounts/OTHER", nil, "wrong_billing_account"},
		{"stale period", "billingAccounts/AAA", map[string]any{"costIntervalStart": "2020-01-01T00:00:00Z"}, "stale_period"},
		{"forecast only", "billingAccounts/AAA", map[string]any{"alertThresholdExceeded": 0, "forecastThresholdExceeded": 0.8}, "forecast_only"},
		{"unknown budget", "billingAccounts/AAA", map[string]any{"budgetId": "OTHER"}, "unknown_alert_ignored"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: test.infoAccount}}
			response := serveOrganization(t, organizationConfig(t, "live"), client, organizationPush(t, test.overrides))
			if response.Code != http.StatusOK || client.disableCalls != 0 || !bytes.Contains(response.Body.Bytes(), []byte(test.reason)) {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, client.disableCalls, response.Body.String())
			}
		})
	}
}

func TestAlreadyDisabledIsSuccessfulAndRouteModesAreIsolated(t *testing.T) {
	client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: false}}
	response := serveOrganization(t, organizationConfig(t, "live"), client, organizationPush(t, nil))
	if response.Code != http.StatusOK || !bytes.Contains(response.Body.Bytes(), []byte("already_disabled")) {
		t.Fatalf("body=%s", response.Body.String())
	}

	receiver, _ := NewServer(Options{RouteMode: RouteModeReceiver, LoadConfig: func(context.Context) (*config.Config, error) { return organizationConfig(t, "dry_run"), nil }})
	adminResponse := httptest.NewRecorder()
	receiver.Router().ServeHTTP(adminResponse, httptest.NewRequest(http.MethodPost, "/self-test", nil))
	if adminResponse.Code != http.StatusNotFound {
		t.Fatalf("receiver exposed admin route: %d", adminResponse.Code)
	}
	admin, _ := NewServer(Options{RouteMode: RouteModeAdmin, LoadConfig: func(context.Context) (*config.Config, error) { return organizationConfig(t, "dry_run"), nil }})
	eventResponse := httptest.NewRecorder()
	admin.Router().ServeHTTP(eventResponse, httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(organizationPush(t, nil))))
	if eventResponse.Code != http.StatusNotFound {
		t.Fatalf("admin exposed event route: %d", eventResponse.Code)
	}
}
