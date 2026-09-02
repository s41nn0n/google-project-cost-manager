package pubsub

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

type PushMessage struct {
	Message struct {
		Data        string            `json:"data"`
		Attributes  map[string]string `json:"attributes"`
		MessageID   string            `json:"messageId"`
		PublishTime string            `json:"publishTime"`
	} `json:"message"`
	Subscription string `json:"subscription"`
}

type Envelope struct {
	Alert        BudgetAlert
	MessageID    string
	PublishTime  time.Time
	Subscription string
}

type BudgetAlert struct {
	BudgetDisplayName         string  `json:"budgetDisplayName"`
	DisplayName               string  `json:"displayName"`
	Name                      string  `json:"name"`
	ResourceName              string  `json:"resourceName"`
	BillingAccountID          string  `json:"billingAccountId"`
	BudgetID                  string  `json:"budgetId"`
	CostAmount                float64 `json:"costAmount"`
	BudgetAmount              float64 `json:"budgetAmount"`
	AlertThresholdExceeded    float64 `json:"alertThresholdExceeded"`
	ForecastThresholdExceeded float64 `json:"forecastThresholdExceeded"`
	CostIntervalStart         string  `json:"costIntervalStart"`
	CurrencyCode              string  `json:"currencyCode"`
}

func DecodePush(body []byte) (*BudgetAlert, error) {
	envelope, err := DecodeEnvelope(body)
	if err != nil {
		return nil, err
	}
	return &envelope.Alert, nil
}

func DecodeEnvelope(body []byte) (*Envelope, error) {
	var push PushMessage
	if err := json.Unmarshal(body, &push); err != nil {
		return nil, err
	}
	if push.Message.Data == "" {
		return nil, errors.New("missing message.data")
	}
	raw, err := base64.StdEncoding.DecodeString(push.Message.Data)
	if err != nil {
		return nil, err
	}
	var alert BudgetAlert
	if err := json.Unmarshal(raw, &alert); err != nil {
		return nil, err
	}
	if alert.BillingAccountID == "" {
		alert.BillingAccountID = push.Message.Attributes["billingAccountId"]
	}
	if alert.BudgetID == "" {
		alert.BudgetID = push.Message.Attributes["budgetId"]
	}
	envelope := &Envelope{Alert: alert, MessageID: push.Message.MessageID, Subscription: push.Subscription}
	if push.Message.PublishTime != "" {
		envelope.PublishTime, err = time.Parse(time.RFC3339Nano, push.Message.PublishTime)
		if err != nil {
			return nil, errors.New("invalid message.publishTime")
		}
	}
	return envelope, nil
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

func (a BudgetAlert) BillingAccountName() string {
	if strings.HasPrefix(a.BillingAccountID, "billingAccounts/") {
		return a.BillingAccountID
	}
	if a.BillingAccountID == "" {
		return ""
	}
	return "billingAccounts/" + a.BillingAccountID
}

func (a BudgetAlert) CanonicalBudgetName() string {
	for _, name := range []string{a.ResourceName, a.Name, a.BudgetID} {
		if strings.HasPrefix(name, "billingAccounts/") {
			return name
		}
	}
	if a.BillingAccountID != "" && a.BudgetID != "" {
		return a.BillingAccountName() + "/budgets/" + strings.TrimPrefix(a.BudgetID, "budgets/")
	}
	return ""
}

func (a BudgetAlert) PeriodStart() (time.Time, error) {
	if a.CostIntervalStart == "" {
		return time.Time{}, errors.New("missing costIntervalStart")
	}
	return time.Parse(time.RFC3339, a.CostIntervalStart)
}
