package pubsub

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestDecodePush(t *testing.T) {
	alert := `{"budgetDisplayName":"prod","costAmount":90,"budgetAmount":100}`
	body, _ := json.Marshal(PushMessage{})
	var p PushMessage
	_ = json.Unmarshal(body, &p)
	p.Message.Data = base64.StdEncoding.EncodeToString([]byte(alert))
	body, _ = json.Marshal(p)
	got, err := DecodePush(body)
	if err != nil {
		t.Fatal(err)
	}
	if got.BudgetDisplayName != "prod" || got.Ratio() != 0.9 {
		t.Fatalf("bad alert: %+v", got)
	}
}
