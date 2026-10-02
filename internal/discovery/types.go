package discovery

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

const (
	ClassManaged   = "managed"
	ClassProtected = "protected"
	ClassUnbilled  = "unbilled"
	ClassInactive  = "inactive"
	ClassBlocked   = "blocked"
)

type ReviewedPolicy struct {
	ControlProjectID     string                   `yaml:"controlProjectId" json:"controlProjectId"`
	RequiredPubSubTopic  string                   `yaml:"requiredPubSubTopic" json:"requiredPubSubTopic"`
	EnforcementMode      string                   `yaml:"enforcementMode" json:"enforcementMode"`
	EnforcementThreshold float64                  `yaml:"enforcementThreshold" json:"enforcementThreshold"`
	BillingAccounts      map[string]AccountPolicy `yaml:"billingAccounts" json:"billingAccounts"`
	ProtectedProjects    map[string]string        `yaml:"protectedProjects" json:"protectedProjects"`
}

type AccountPolicy struct {
	DefaultMonthlyAmount float64            `yaml:"defaultMonthlyAmount" json:"defaultMonthlyAmount"`
	CurrencyCode         string             `yaml:"currencyCode" json:"currencyCode"`
	ProjectOverrides     map[string]float64 `yaml:"projectOverrides" json:"projectOverrides"`
}

func (p *ReviewedPolicy) ApplyDefaults() {
	if p.EnforcementMode == "" {
		p.EnforcementMode = "dry_run"
	}
	if p.EnforcementThreshold == 0 {
		p.EnforcementThreshold = 0.8
	}
	if p.ProtectedProjects == nil {
		p.ProtectedProjects = map[string]string{}
	}
	if p.BillingAccounts == nil {
		p.BillingAccounts = map[string]AccountPolicy{}
	}
	for name, account := range p.BillingAccounts {
		if account.CurrencyCode == "" {
			account.CurrencyCode = "USD"
		}
		if account.ProjectOverrides == nil {
			account.ProjectOverrides = map[string]float64{}
		}
		p.BillingAccounts[name] = account
	}
}

func (p ReviewedPolicy) Validate() error {
	if p.ControlProjectID == "" {
		return fmt.Errorf("controlProjectId is required")
	}
	if p.RequiredPubSubTopic == "" {
		return fmt.Errorf("requiredPubSubTopic is required")
	}
	if p.EnforcementMode != "dry_run" && p.EnforcementMode != "live" {
		return fmt.Errorf("enforcementMode must be dry_run or live")
	}
	if p.EnforcementThreshold != 0.8 {
		return fmt.Errorf("enforcementThreshold must be 0.8")
	}
	for project, reason := range p.ProtectedProjects {
		if project == "" || strings.TrimSpace(reason) == "" {
			return fmt.Errorf("every protected project requires an explicit project ID and non-empty reason")
		}
	}
	for name, account := range p.BillingAccounts {
		if !strings.HasPrefix(name, "billingAccounts/") {
			return fmt.Errorf("billing account %q must use billingAccounts/ID form", name)
		}
		if account.DefaultMonthlyAmount <= 0 {
			return fmt.Errorf("billing account %s requires a positive defaultMonthlyAmount", name)
		}
		if account.CurrencyCode == "" {
			return fmt.Errorf("billing account %s requires currencyCode", name)
		}
		for project, amount := range account.ProjectOverrides {
			if project == "" || amount <= 0 {
				return fmt.Errorf("billing account %s has an invalid project override", name)
			}
		}
	}
	return nil
}

type Project struct {
	ProjectID      string `json:"projectId" yaml:"projectId"`
	ProjectNumber  string `json:"projectNumber" yaml:"projectNumber"`
	DisplayName    string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	LifecycleState string `json:"lifecycleState" yaml:"lifecycleState"`
}

type BillingLink struct {
	BillingAccountName string `json:"billingAccountName,omitempty" yaml:"billingAccountName,omitempty"`
	BillingEnabled     bool   `json:"billingEnabled" yaml:"billingEnabled"`
}

type ExistingBudget struct {
	Name                   string      `json:"name" yaml:"name"`
	DisplayName            string      `json:"displayName" yaml:"displayName"`
	Projects               []string    `json:"projects" yaml:"projects"`
	CalendarPeriod         string      `json:"calendarPeriod" yaml:"calendarPeriod"`
	Amount                 Money       `json:"amount" yaml:"amount"`
	CreditTypesTreatment   string      `json:"creditTypesTreatment" yaml:"creditTypesTreatment"`
	OwnershipScope         string      `json:"ownershipScope" yaml:"ownershipScope"`
	FilterCompatible       bool        `json:"filterCompatible" yaml:"filterCompatible"`
	NotificationCompatible bool        `json:"notificationCompatible" yaml:"notificationCompatible"`
	PubSubTopic            string      `json:"pubsubTopic" yaml:"pubsubTopic"`
	Thresholds             []Threshold `json:"thresholds" yaml:"thresholds"`
	Classification         string      `json:"classification" yaml:"classification"`
}

type Money struct {
	CurrencyCode string `json:"currencyCode" yaml:"currencyCode"`
	Units        int64  `json:"units" yaml:"units"`
	Nanos        int64  `json:"nanos" yaml:"nanos"`
}

func MoneyFromFloat(currency string, amount float64) Money {
	units := int64(amount)
	nanos := int64((amount-float64(units))*1_000_000_000 + 0.5)
	return Money{CurrencyCode: currency, Units: units, Nanos: nanos}
}

func (m Money) Equal(other Money) bool {
	return m.CurrencyCode == other.CurrencyCode && m.Units == other.Units && m.Nanos == other.Nanos
}

type Threshold struct {
	Percent    float64 `json:"percent" yaml:"percent"`
	SpendBasis string  `json:"spendBasis" yaml:"spendBasis"`
}

type BillingAccount struct {
	Name        string `json:"name" yaml:"name"`
	DisplayName string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	Open        bool   `json:"open" yaml:"open"`
}

type ProjectInventory struct {
	Project
	BillingLink
	Classification       string           `json:"classification" yaml:"classification"`
	Reason               string           `json:"reason,omitempty" yaml:"reason,omitempty"`
	MonthlyAmount        float64          `json:"monthlyAmount,omitempty" yaml:"monthlyAmount,omitempty"`
	CurrencyCode         string           `json:"currencyCode,omitempty" yaml:"currencyCode,omitempty"`
	CanonicalDisplayName string           `json:"canonicalDisplayName,omitempty" yaml:"canonicalDisplayName,omitempty"`
	ImportCandidate      string           `json:"importCandidate,omitempty" yaml:"importCandidate,omitempty"`
	BudgetClassification string           `json:"budgetClassification,omitempty" yaml:"budgetClassification,omitempty"`
	BudgetAccountName    string           `json:"budgetAccountName,omitempty" yaml:"budgetAccountName,omitempty"`
	ExistingBudgets      []ExistingBudget `json:"existingBudgets,omitempty" yaml:"existingBudgets,omitempty"`
}

type Inventory struct {
	SchemaVersion   int                `json:"schemaVersion" yaml:"schemaVersion"`
	Organization    string             `json:"organization" yaml:"organization"`
	BillingAccounts []BillingAccount   `json:"billingAccounts" yaml:"billingAccounts"`
	Projects        []ProjectInventory `json:"projects" yaml:"projects"`
	Budgets         []ExistingBudget   `json:"budgets" yaml:"budgets"`
}

type ImportCandidate struct {
	BillingAccountName string `json:"billingAccountName"`
	ProjectID          string `json:"projectId"`
	TerraformAddress   string `json:"terraformAddress"`
	RemoteID           string `json:"remoteId"`
}

type Diagnostic struct {
	Code           string `json:"code"`
	Severity       string `json:"severity"`
	ProjectID      string `json:"projectId,omitempty"`
	BillingAccount string `json:"billingAccount,omitempty"`
	Message        string `json:"message"`
}

type Coverage struct {
	TotalProjects   int            `json:"totalProjects"`
	Classifications map[string]int `json:"classifications"`
	Complete        bool           `json:"complete"`
}

type Result struct {
	Inventory   Inventory         `json:"inventory"`
	Coverage    Coverage          `json:"coverage"`
	Imports     []ImportCandidate `json:"imports"`
	Diagnostics []Diagnostic      `json:"diagnostics"`
}

func (r *Result) Normalize() {
	sort.Slice(r.Inventory.BillingAccounts, func(i, j int) bool { return r.Inventory.BillingAccounts[i].Name < r.Inventory.BillingAccounts[j].Name })
	sort.Slice(r.Inventory.Projects, func(i, j int) bool { return r.Inventory.Projects[i].ProjectID < r.Inventory.Projects[j].ProjectID })
	sort.Slice(r.Inventory.Budgets, func(i, j int) bool { return r.Inventory.Budgets[i].Name < r.Inventory.Budgets[j].Name })
	for i := range r.Inventory.Projects {
		sort.Slice(r.Inventory.Projects[i].ExistingBudgets, func(a, b int) bool {
			return r.Inventory.Projects[i].ExistingBudgets[a].Name < r.Inventory.Projects[i].ExistingBudgets[b].Name
		})
	}
	sort.Slice(r.Imports, func(i, j int) bool { return r.Imports[i].TerraformAddress < r.Imports[j].TerraformAddress })
	sort.Slice(r.Diagnostics, func(i, j int) bool {
		a, _ := json.Marshal(r.Diagnostics[i])
		b, _ := json.Marshal(r.Diagnostics[j])
		return string(a) < string(b)
	})
}
