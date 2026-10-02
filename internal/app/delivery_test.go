package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/example/google-project-cost-manager/internal/billing"
	"github.com/example/google-project-cost-manager/internal/config"
	"github.com/example/google-project-cost-manager/internal/eventstate"
)

func rewriteMessage(t *testing.T, body []byte, id string, published time.Time) []byte {
	t.Helper()
	var push map[string]any
	if err := json.Unmarshal(body, &push); err != nil {
		t.Fatal(err)
	}
	message := push["message"].(map[string]any)
	message["messageId"] = id
	message["publishTime"] = published.UTC().Format(time.RFC3339Nano)
	out, _ := json.Marshal(push)
	return out
}

func TestDuplicateAndOutOfOrderDeliveriesDoNotDisableTwice(t *testing.T) {
	cfg := organizationConfig(t, "live")
	client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}}
	server, err := NewServer(Options{ReadinessCheck: testReady, Billing: client, EventStore: &eventstate.MemoryStore{}, LoadConfig: func(context.Context) (*config.Config, error) { return cfg, nil }})
	if err != nil {
		t.Fatal(err)
	}
	published := time.Now().UTC()
	first := rewriteMessage(t, organizationPush(t, nil), "one", published)
	response := httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(first)))
	if response.Code != http.StatusOK || client.disableCalls != 1 {
		t.Fatalf("first: %d calls=%d body=%s", response.Code, client.disableCalls, response.Body.String())
	}

	// Simulate an eventually consistent billing read still reporting the original link.
	client.info = billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}
	response = httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(first)))
	if client.disableCalls != 1 || !bytes.Contains(response.Body.Bytes(), []byte("duplicate_event")) {
		t.Fatalf("duplicate calls=%d body=%s", client.disableCalls, response.Body.String())
	}

	older := rewriteMessage(t, organizationPush(t, map[string]any{"costAmount": 80.0}), "older", published.Add(-time.Minute))
	response = httptest.NewRecorder()
	server.Router().ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/pubsub/billing-alert", bytes.NewReader(older)))
	if client.disableCalls != 1 || !bytes.Contains(response.Body.Bytes(), []byte("out_of_order_event")) {
		t.Fatalf("out-of-order calls=%d body=%s", client.disableCalls, response.Body.String())
	}
}

func TestProtectedAndBelowThresholdNotificationsAreRejected(t *testing.T) {
	cfg := organizationConfig(t, "live")
	cfg.ProtectedProjects = append(cfg.ProtectedProjects, "p1")
	client := &trackingBilling{info: billing.ProjectBillingInfo{BillingEnabled: true, BillingAccountName: "billingAccounts/AAA"}}
	response := serveOrganization(t, cfg, client, organizationPush(t, nil))
	if client.disableCalls != 0 || !bytes.Contains(response.Body.Bytes(), []byte("protected_project")) {
		t.Fatalf("protected body=%s", response.Body.String())
	}

	cfg = organizationConfig(t, "live")
	response = serveOrganization(t, cfg, client, organizationPush(t, map[string]any{"alertThresholdExceeded": 0.5, "costAmount": 50.0}))
	if client.disableCalls != 0 || !bytes.Contains(response.Body.Bytes(), []byte("below_threshold")) {
		t.Fatalf("below body=%s", response.Body.String())
	}
}
