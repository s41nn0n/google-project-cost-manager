package pubsub

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

type PushMessage struct {
	Message struct {
		Data       string            `json:"data"`
		Attributes map[string]string `json:"attributes"`
		MessageID  string            `json:"messageId"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

type BudgetAlert struct {
	BudgetDisplayName      string  `json:"budgetDisplayName"`
	DisplayName            string  `json:"displayName"`
	Name                   string  `json:"name"`
	ResourceName           string  `json:"resourceName"`
	CostAmount             float64 `json:"costAmount"`
	BudgetAmount           float64 `json:"budgetAmount"`
	AlertThresholdExceeded float64 `json:"alertThresholdExceeded"`
}

func DecodePush(body []byte) (*BudgetAlert, error) {
	var p PushMessage
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, err
	}
	if p.Message.Data == "" {
		return nil, errors.New("missing message.data")
	}
	raw, err := base64.StdEncoding.DecodeString(p.Message.Data)
	if err != nil {
		return nil, err
	}
	var a BudgetAlert
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	return &a, nil
}

func (a BudgetAlert) Ratio() float64 {
	if a.AlertThresholdExceeded > 0 {
		return a.AlertThresholdExceeded
	}
	if a.BudgetAmount > 0 {
		return a.CostAmount / a.BudgetAmount
	}
	return 0
}
func (a BudgetAlert) Names() []string {
	return []string{a.BudgetDisplayName, a.DisplayName, a.Name, a.ResourceName}
}
