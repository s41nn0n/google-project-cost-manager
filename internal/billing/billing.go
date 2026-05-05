package billing

import (
	"context"
	"fmt"
	"strings"

	cloudbilling "google.golang.org/api/cloudbilling/v1"
	"google.golang.org/api/option"
)

type ProjectBillingInfo struct {
	Name               string `json:"name"`
	ProjectID          string `json:"projectId"`
	BillingAccountName string `json:"billingAccountName,omitempty"`
	BillingEnabled     bool   `json:"billingEnabled"`
}

type Client interface {
	GetProjectBillingInfo(ctx context.Context, projectID string) (*ProjectBillingInfo, error)
	DisableBilling(ctx context.Context, projectID string) error
	SetBillingAccount(ctx context.Context, projectID, billingAccountName string) error
}

type CloudClient struct{ svc *cloudbilling.APIService }

func NewCloudClient(ctx context.Context) (*CloudClient, error) {
	svc, err := cloudbilling.NewService(ctx, option.WithScopes(cloudbilling.CloudBillingScope))
	if err != nil {
		return nil, err
	}
	return &CloudClient{svc: svc}, nil
}

func projectName(projectID string) string {
	if strings.HasPrefix(projectID, "projects/") {
		return projectID
	}
	return "projects/" + projectID
}

func (c *CloudClient) GetProjectBillingInfo(ctx context.Context, projectID string) (*ProjectBillingInfo, error) {
	bi, err := c.svc.Projects.GetBillingInfo(projectName(projectID)).Context(ctx).Do()
	if err != nil {
		return nil, err
	}
	return &ProjectBillingInfo{Name: bi.Name, ProjectID: projectID, BillingAccountName: bi.BillingAccountName, BillingEnabled: bi.BillingEnabled}, nil
}

func (c *CloudClient) DisableBilling(ctx context.Context, projectID string) error {
	return c.SetBillingAccount(ctx, projectID, "")
}

func (c *CloudClient) SetBillingAccount(ctx context.Context, projectID, billingAccountName string) error {
	_, err := c.svc.Projects.UpdateBillingInfo(projectName(projectID), &cloudbilling.ProjectBillingInfo{BillingAccountName: billingAccountName}).Context(ctx).Do()
	if err != nil {
		return fmt.Errorf("update billing info: %w", err)
	}
	return nil
}
