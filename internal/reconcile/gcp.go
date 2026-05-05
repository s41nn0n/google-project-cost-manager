package reconcile

import (
	"context"
	"strings"

	billingbudgets "google.golang.org/api/billingbudgets/v1"
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
			for _, tr := range b.ThresholdRules {
				thresholds = append(thresholds, tr.ThresholdPercent)
			}
			topic := ""
			if b.NotificationsRule != nil {
				topic = b.NotificationsRule.PubsubTopic
			}
			out = append(out, Budget{Name: b.Name, DisplayName: b.DisplayName, Projects: projects, Thresholds: thresholds, PubSubTopic: topic})
		}
		return nil
	})
	return out, err
}

type CloudProjectResolver struct{ svc *cloudresourcemanager.Service }

func NewCloudProjectResolver(ctx context.Context) (*CloudProjectResolver, error) {
	svc, err := cloudresourcemanager.NewService(ctx)
	if err != nil {
		return nil, err
	}
	return &CloudProjectResolver{svc: svc}, nil
}

func (r *CloudProjectResolver) ResolveProjectID(ctx context.Context, project string) (string, error) {
	project = strings.TrimPrefix(project, "projects/")
	p, err := r.svc.Projects.Get(project).Context(ctx).Do()
	if err != nil {
		return "", err
	}
	return p.ProjectId, nil
}
