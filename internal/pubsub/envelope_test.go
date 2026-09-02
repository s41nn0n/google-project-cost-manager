package pubsub

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeEnvelopeUsesCanonicalPubSubAttributes(t *testing.T) {
	data := base64.StdEncoding.EncodeToString([]byte(`{"budgetDisplayName":"ignored-name","costAmount":80,"budgetAmount":100,"alertThresholdExceeded":0.8,"costIntervalStart":"2026-09-01T00:00:00Z"}`))
	body, _ := json.Marshal(map[string]any{"message": map[string]any{
		"data": data, "messageId": "m1", "publishTime": time.Now().UTC().Format(time.RFC3339Nano),
		"attributes": map[string]string{"billingAccountId": "AAA", "budgetId": "BBB", "schemaVersion": "1.0"},
	}})
	envelope, err := DecodeEnvelope(body)
	if err != nil {
		t.Fatal(err)
	}
	if got := envelope.Alert.CanonicalBudgetName(); got != "billingAccounts/AAA/budgets/BBB" {
		t.Fatalf("canonical name = %q", got)
	}
}
