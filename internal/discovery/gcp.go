package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	billingbudgets "google.golang.org/api/billingbudgets/v1"
	cloudasset "google.golang.org/api/cloudasset/v1"
	cloudbilling "google.golang.org/api/cloudbilling/v1"
	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v1"
)

type GCPClient struct {
	assets   *cloudasset.Service
	billing  *cloudbilling.APIService
	budgets  *billingbudgets.Service
	projects *cloudresourcemanager.Service
}

func NewGCPClient(ctx context.Context) (*GCPClient, error) {
	assets, err := cloudasset.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloud asset client: %w", err)
	}
	billing, err := cloudbilling.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("cloud billing client: %w", err)
	}
	budgets, err := billingbudgets.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("billing budgets client: %w", err)
	}
	projects, err := cloudresourcemanager.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("project metadata client: %w", err)
	}
	return &GCPClient{assets: assets, billing: billing, budgets: budgets, projects: projects}, nil
}

func (c *GCPClient) ListProjects(ctx context.Context, organization string) ([]Project, error) {
	var out []Project
	call := c.assets.V1.SearchAllResources(organization).AssetTypes("cloudresourcemanager.googleapis.com/Project").PageSize(500)
	err := call.Pages(ctx, func(resp *cloudasset.SearchAllResourcesResponse) error {
		for _, resource := range resp.Results {
			var attrs struct {
				ProjectID string `json:"projectId"`
			}
			_ = json.Unmarshal(resource.AdditionalAttributes, &attrs)
			number := strings.TrimPrefix(resource.Project, "projects/")
			if number == "" {
				number = strings.TrimPrefix(resource.Name, "//cloudresourcemanager.googleapis.com/projects/")
			}
			state := resource.State
			if attrs.ProjectID == "" && number != "" {
				if c.projects == nil {
					return fmt.Errorf("project metadata lookup required for %s", number)
				}
				metadata, err := c.projects.Projects.Get(number).Context(ctx).Do()
				if err != nil {
					return fmt.Errorf("resolve project identity %s: %w", number, err)
				}
				attrs.ProjectID, state = metadata.ProjectId, metadata.LifecycleState
			}
			if attrs.ProjectID == "" || number == "" {
				return fmt.Errorf("Cloud Asset result lacks project identity: %s", resource.Name)
			}
			out = append(out, Project{ProjectID: attrs.ProjectID, ProjectNumber: number, DisplayName: resource.DisplayName, LifecycleState: state})
		}
		return nil
	})
	return out, err
}

func (c *GCPClient) ListBillingAccounts(ctx context.Context, organization string) ([]BillingAccount, error) {
	var out []BillingAccount
	err := c.billing.BillingAccounts.List().Parent(organization).PageSize(100).Pages(ctx, func(resp *cloudbilling.ListBillingAccountsResponse) error {
		for _, account := range resp.BillingAccounts {
			out = append(out, BillingAccount{Name: account.Name, DisplayName: account.DisplayName, Open: account.Open})
		}
		return nil
	})
	return out, err
}

func (c *GCPClient) GetProjectBilling(ctx context.Context, projectID string) (BillingLink, error) {
	info, err := c.billing.Projects.GetBillingInfo("projects/" + projectID).Context(ctx).Do()
	if err != nil {
		return BillingLink{}, err
	}
	return BillingLink{BillingAccountName: info.BillingAccountName, BillingEnabled: info.BillingEnabled}, nil
}

func (c *GCPClient) ListBudgets(ctx context.Context, account string) ([]ExistingBudget, error) {
	var out []ExistingBudget
	err := c.budgets.BillingAccounts.Budgets.List(account).Pages(ctx, func(resp *billingbudgets.GoogleCloudBillingBudgetsV1ListBudgetsResponse) error {
		for _, budget := range resp.Budgets {
			item := ExistingBudget{Name: budget.Name, DisplayName: budget.DisplayName, OwnershipScope: budget.OwnershipScope}
			if budget.BudgetFilter != nil {
				item.Projects = append([]string(nil), budget.BudgetFilter.Projects...)
				item.CalendarPeriod = budget.BudgetFilter.CalendarPeriod
				item.CreditTypesTreatment = budget.BudgetFilter.CreditTypesTreatment
				item.FilterCompatible = len(budget.BudgetFilter.Services) == 0 && len(budget.BudgetFilter.Subaccounts) == 0 && len(budget.BudgetFilter.ResourceAncestors) == 0 && len(budget.BudgetFilter.Labels) == 0 && len(budget.BudgetFilter.CreditTypes) == 0 && budget.BudgetFilter.CustomPeriod == nil
			}
			if budget.Amount != nil && budget.Amount.SpecifiedAmount != nil {
				money := budget.Amount.SpecifiedAmount
				item.Amount = Money{CurrencyCode: money.CurrencyCode, Units: money.Units, Nanos: money.Nanos}
			}
			if budget.NotificationsRule != nil {
				item.PubSubTopic = budget.NotificationsRule.PubsubTopic
				item.NotificationCompatible = budget.NotificationsRule.SchemaVersion == "1.0" && !budget.NotificationsRule.DisableDefaultIamRecipients && !budget.NotificationsRule.EnableProjectLevelRecipients && len(budget.NotificationsRule.MonitoringNotificationChannels) == 0
			}
			for _, threshold := range budget.ThresholdRules {
				item.Thresholds = append(item.Thresholds, Threshold{Percent: threshold.ThresholdPercent, SpendBasis: threshold.SpendBasis})
			}
			out = append(out, item)
		}
		return nil
	})
	return out, err
}
