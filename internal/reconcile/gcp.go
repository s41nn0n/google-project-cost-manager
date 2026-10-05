package reconcile

import (
	"context"
	"fmt"
	"strings"

	billingbudgets "google.golang.org/api/billingbudgets/v1"
	cloudbilling "google.golang.org/api/cloudbilling/v1"
	cloudresourcemanager "google.golang.org/api/cloudresourcemanager/v1"
)

type CloudBudgetClient struct{ svc *billingbudgets.Service }

func NewCloudBudgetClient(ctx context.Context) (*CloudBudgetClient, error) {
	svc, err := billingbudgets.NewService(ctx)
	if err != nil {
		return nil, err
	}
	return &CloudBudgetClient{svc: svc}, nil
}

func (c *CloudBudgetClient) ListBudgets(ctx context.Context, billingAccountName string) ([]Budget, error) {
	var out []Budget
	err := c.svc.BillingAccounts.Budgets.List(billingAccountName).Pages(ctx, func(resp *billingbudgets.GoogleCloudBillingBudgetsV1ListBudgetsResponse) error {
		for _, b := range resp.Budgets {
			var projects []string
			if b.BudgetFilter != nil {
				projects = append(projects, b.BudgetFilter.Projects...)
			}
			var thresholds []float64
			var bases []string
			for _, tr := range b.ThresholdRules {
				thresholds = append(thresholds, tr.ThresholdPercent)
				basis := tr.SpendBasis
				if basis == "" {
					basis = "CURRENT_SPEND"
				}
				bases = append(bases, basis)
			}
			topic := ""
			if b.NotificationsRule != nil {
				topic = b.NotificationsRule.PubsubTopic
			}
			budget := Budget{Name: b.Name, DisplayName: b.DisplayName, Projects: projects, Thresholds: thresholds, PubSubTopic: topic, SpendBases: bases, SpendCap: b.SpendCap != nil}
			if b.BudgetFilter != nil {
				budget.CalendarPeriod = b.BudgetFilter.CalendarPeriod
				filter := b.BudgetFilter
				budget.RestrictedFilter = len(filter.Services) > 0 || len(filter.Subaccounts) > 0 || len(filter.ResourceAncestors) > 0 || len(filter.Labels) > 0 || len(filter.CreditTypes) > 0 || filter.CustomPeriod != nil
				budget.CreditTreatment = b.BudgetFilter.CreditTypesTreatment
				if budget.CreditTreatment == "" {
					budget.CreditTreatment = "INCLUDE_ALL_CREDITS"
				}
			}
			if b.Amount != nil && b.Amount.SpecifiedAmount != nil {
				money := b.Amount.SpecifiedAmount
				budget.MonthlyAmount = float64(money.Units) + float64(money.Nanos)/1e9
				budget.CurrencyCode = money.CurrencyCode
			}
			out = append(out, budget)
		}
		return nil
	})
	return out, err
}

type CloudProjectResolver struct {
	svc     *cloudresourcemanager.Service
	billing *cloudbilling.APIService
}

func NewCloudProjectResolver(ctx context.Context) (*CloudProjectResolver, error) {
	svc, err := cloudresourcemanager.NewService(ctx)
	if err != nil {
		return nil, err
	}
	billing, err := cloudbilling.NewService(ctx)
	if err != nil {
		return nil, err
	}
	return &CloudProjectResolver{svc: svc, billing: billing}, nil
}

func (r *CloudProjectResolver) BillingAccount(ctx context.Context, project string) (string, error) {
	permission := "resourcemanager.projects.deleteBillingAssignment"
	granted, err := r.svc.Projects.TestIamPermissions(strings.TrimPrefix(project, "projects/"), &cloudresourcemanager.TestIamPermissionsRequest{Permissions: []string{permission}}).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if len(granted.Permissions) != 1 || granted.Permissions[0] != permission {
		return "", fmt.Errorf("runtime lacks billing-unlink permission for %s", project)
	}
	info, err := r.billing.Projects.GetBillingInfo("projects/" + strings.TrimPrefix(project, "projects/")).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	if !info.BillingEnabled {
		return "", nil
	}
	return info.BillingAccountName, nil
}

func (r *CloudProjectResolver) ResolveProjectID(ctx context.Context, project string) (string, error) {
	project = strings.TrimPrefix(project, "projects/")
	p, err := r.svc.Projects.Get(project).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	return p.ProjectId, nil
}
