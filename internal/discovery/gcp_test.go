package discovery

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	cloudasset "google.golang.org/api/cloudasset/v1"
	"google.golang.org/api/option"
)

func TestListProjectsPaginates(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "next" {
			_, _ = fmt.Fprint(w, `{"results":[{"name":"//cloudresourcemanager.googleapis.com/projects/22","displayName":"two","project":"projects/22","state":"ACTIVE","additionalAttributes":{"projectId":"p-two"}}]}`)
			return
		}
		_, _ = fmt.Fprint(w, `{"nextPageToken":"next","results":[{"name":"//cloudresourcemanager.googleapis.com/projects/11","displayName":"one","project":"projects/11","state":"ACTIVE","additionalAttributes":{"projectId":"p-one"}}]}`)
	}))
	defer server.Close()
	service, err := cloudasset.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	projects, err := (&GCPClient{assets: service}).ListProjects(context.Background(), "organizations/123")
	if err != nil {
		t.Fatal(err)
	}
	if requests != 2 || len(projects) != 2 || projects[1].ProjectID != "p-two" || projects[1].ProjectNumber != "22" {
		t.Fatalf("requests=%d projects=%+v", requests, projects)
	}
}

func TestWriteOutputIsDeterministic(t *testing.T) {
	result := Result{
		Inventory: Inventory{SchemaVersion: 1, Organization: "organizations/123", BillingAccounts: []BillingAccount{{Name: "billingAccounts/A"}}, Projects: []ProjectInventory{{Project: Project{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"}, BillingLink: BillingLink{BillingAccountName: "billingAccounts/A", BillingEnabled: true}, Classification: ClassManaged, MonthlyAmount: 100, CurrencyCode: "USD", CanonicalDisplayName: "billing-guard-p1"}}},
		Coverage:  Coverage{TotalProjects: 1, Classifications: map[string]int{ClassManaged: 1}, Complete: true},
		Imports:   []ImportCandidate{{BillingAccountName: "billingAccounts/A", ProjectID: "p1", TerraformAddress: `module.billing_account["billingAccounts/A"].google_billing_budget.project["p1"]`, RemoteID: "billingAccounts/A/budgets/B"}},
	}
	policy := basePolicy()
	first, second := t.TempDir(), t.TempDir()
	if err := WriteOutput(first, result, policy); err != nil {
		t.Fatal(err)
	}
	if err := WriteOutput(second, result, policy); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inventory.yaml", "coverage.json", "imports.json", "imports.tf", "diagnostics.json", "terraform.auto.tfvars.json"} {
		a, err := osRead(first + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		b, err := osRead(second + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("%s is non-deterministic", name)
		}
		if name == "imports.tf" && !strings.Contains(string(a), `id = "billingAccounts/A/budgets/B"`) {
			t.Fatalf("bad import golden: %s", a)
		}
	}
}
