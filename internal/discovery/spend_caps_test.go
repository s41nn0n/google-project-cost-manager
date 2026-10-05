package discovery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	budgets "google.golang.org/api/billingbudgets/v1"
)

func TestListBudgetsScopesPreviewCapsUsingExplicitAPIType(t *testing.T) {
	for _, capFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("caps_first_%t", capFirst), func(t *testing.T) {
			// A cap-like display name without spendCap is still an ordinary budget.
			standard := []*budgets.GoogleCloudBillingBudgetsV1Budget{
				{Name: "billingAccounts/A/budgets/one", DisplayName: "one"},
				{Name: "billingAccounts/A/budgets/two", DisplayName: "two"},
				{Name: "billingAccounts/A/budgets/three", DisplayName: "Generated spend cap (Firebase Console)"},
			}
			withCaps := append([]*budgets.GoogleCloudBillingBudgetsV1Budget(nil), standard...)
			for i := 0; i < 21; i++ {
				withCaps = append(withCaps, &budgets.GoogleCloudBillingBudgetsV1Budget{
					Name:        fmt.Sprintf("billingAccounts/A/budgets/cap-%02d", i),
					DisplayName: fmt.Sprintf("billing-guard-cap-%02d", i),
					// Presence, even an empty object, identifies this resource type.
					SpendCap: &budgets.GoogleCloudBillingBudgetsV1SpendCap{},
				})
			}
			requests := 0
			client := budgetTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				returned := standard
				if (requests == 1) == capFirst {
					returned = withCaps
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"budgets": returned}); err != nil {
					t.Error(err)
				}
			})
			listed, err := client.ListBudgets(context.Background(), "billingAccounts/A")
			if err != nil || requests != 2 || len(listed) != 3 {
				t.Fatalf("preview catalog blocked standard discovery: requests=%d budgets=%+v error=%v", requests, listed, err)
			}
			for _, budget := range listed {
				if budget.SpendCap {
					t.Fatalf("preview cap entered standard inventory: %+v", budget)
				}
			}
			if listed[2].DisplayName != "Generated spend cap (Firebase Console)" {
				t.Fatal("display name was incorrectly used as a type discriminator")
			}
		})
	}
}

func TestListBudgetsResourceTypeTransitionStillBlocks(t *testing.T) {
	for _, capFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("caps_first_%t", capFirst), func(t *testing.T) {
			requests := 0
			client := budgetTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				budget := &budgets.GoogleCloudBillingBudgetsV1Budget{Name: "billingAccounts/A/budgets/one", DisplayName: "billing-guard-p1"}
				if (requests == 1) == capFirst {
					budget.SpendCap = &budgets.GoogleCloudBillingBudgetsV1SpendCap{InputState: "CONFIGURED"}
				}
				if err := json.NewEncoder(w).Encode(map[string]any{"budgets": []*budgets.GoogleCloudBillingBudgetsV1Budget{budget}}); err != nil {
					t.Error(err)
				}
			})
			listed, err := client.ListBudgets(context.Background(), "billingAccounts/A")
			if listed != nil || !errors.Is(err, ErrBudgetInventoryUnstable) {
				t.Fatalf("standard budget type transition accepted: budgets=%+v error=%v", listed, err)
			}
		})
	}
}

func TestListBudgetsCapPresenceCannotMaskStandardChanges(t *testing.T) {
	requests := 0
	client := budgetTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			fmt.Fprint(w, `{"budgets":[{"name":"billingAccounts/A/budgets/one","displayName":"ordinary"},{"name":"billingAccounts/A/budgets/cap","spendCap":{"inputState":"CONFIGURED"}}]}`)
		} else {
			fmt.Fprint(w, `{"budgets":[{"name":"billingAccounts/A/budgets/one","displayName":"changed"}]}`)
		}
	})
	listed, err := client.ListBudgets(context.Background(), "billingAccounts/A")
	if listed != nil || !errors.Is(err, ErrBudgetInventoryUnstable) || !strings.Contains(err.Error(), "changed=1") {
		t.Fatalf("cap disappearance masked standard drift: budgets=%+v error=%v", listed, err)
	}
}

func TestExactAdoptionRejectsSpendCapEvenWithCanonicalShape(t *testing.T) {
	entry := ProjectInventory{Project: Project{ProjectID: "p1", ProjectNumber: "1"}, MonthlyAmount: 100, CurrencyCode: "USD", CanonicalDisplayName: "billing-guard-p1"}
	budget := exactBudget(entry.CanonicalDisplayName, "1")
	budget.SpendCap = true
	if matches := compatibleBudgets([]ExistingBudget{budget}, entry, basePolicy()); len(matches) != 0 {
		t.Fatalf("preview cap was eligible for adoption: %+v", matches)
	}
}

func TestScanPreviewCapsDoNotChangeGeneratedDocuments(t *testing.T) {
	policy := basePolicy()
	cap := exactBudget("billing-guard-p1", "1")
	cap.SpendCap = true
	retainedCap := exactBudget("billing-guard-p2", "2")
	retainedCap.Name = "billingAccounts/B/budgets/cap-two"
	retainedCap.SpendCap = true
	external := exactBudget("ordinary-external", "1")
	external.Name = "billingAccounts/A/budgets/external"
	client := fakeClient{
		projects: []Project{
			{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"},
			{ProjectID: "p2", ProjectNumber: "2", LifecycleState: "ACTIVE"},
			{ProjectID: "control", ProjectNumber: "3", LifecycleState: "ACTIVE"},
		},
		accounts: []BillingAccount{{Name: "billingAccounts/A", Open: true}, {Name: "billingAccounts/B"}},
		links: map[string]BillingLink{
			"p1":      {BillingEnabled: true, BillingAccountName: "billingAccounts/A"},
			"p2":      {BillingEnabled: false, BillingAccountName: "billingAccounts/B"},
			"control": {BillingEnabled: true, BillingAccountName: "billingAccounts/A"},
		},
		budgets: map[string][]ExistingBudget{"billingAccounts/A": {cap, external}, "billingAccounts/B": {retainedCap}},
	}
	first, err := Scan(context.Background(), "organizations/123", policy, client)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Coverage.Complete || len(first.Imports) != 0 || len(first.Diagnostics) != 0 || len(first.Inventory.Budgets) != 1 || first.Inventory.Budgets[0].Name != external.Name {
		t.Fatalf("unexpected standard inventory: %+v", first)
	}
	if first.Inventory.Projects[0].Classification != ClassProtected || first.Inventory.Projects[1].BudgetClassification != "create_canonical" || first.Inventory.Projects[2].ImportCandidate != "" {
		t.Fatalf("caps changed project policy or were retained: %+v", first.Inventory.Projects)
	}
	firstDir, secondDir := t.TempDir(), t.TempDir()
	if err := WriteOutput(firstDir, first, policy); err != nil {
		t.Fatal(err)
	}
	client.budgets = map[string][]ExistingBudget{"billingAccounts/A": {external}}
	second, err := Scan(context.Background(), "organizations/123", policy, client)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteOutput(secondDir, second, policy); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"inventory.yaml", "coverage.json", "imports.json", "imports.tf", "diagnostics.json", "terraform.auto.tfvars.json"} {
		a, err := osRead(filepath.Join(firstDir, name))
		if err != nil {
			t.Fatal(err)
		}
		b, err := osRead(filepath.Join(secondDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(a, b) {
			t.Fatalf("%s changed with preview-cap visibility", name)
		}
	}
	wantScope := BudgetDiscoveryCoverage{Scope: BudgetScopeStandardAlertsOnly, PreviewSpendCaps: PreviewSpendCapVisibilityNotVerified}
	if first.Inventory.BudgetDiscovery != wantScope || first.Coverage.BudgetDiscovery != wantScope {
		t.Fatal("coverage did not explicitly limit preview-cap visibility")
	}
}

func TestScanPreviewExclusionDoesNotBypassPermissionFailures(t *testing.T) {
	for _, failure := range []string{"project", "account"} {
		t.Run(failure, func(t *testing.T) {
			cap := exactBudget("billing-guard-p1", "1")
			cap.SpendCap = true
			client := fakeClient{
				projects: []Project{{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"}},
				accounts: []BillingAccount{{Name: "billingAccounts/A", Open: true}},
				links:    map[string]BillingLink{"p1": {BillingEnabled: true, BillingAccountName: "billingAccounts/A"}},
				budgets:  map[string][]ExistingBudget{"billingAccounts/A": {cap}},
			}
			if failure == "project" {
				client.linkErr = map[string]error{"p1": errors.New("permission denied")}
			} else {
				client.budgetErr = map[string]error{"billingAccounts/A": errors.New("permission denied")}
			}
			result, err := Scan(context.Background(), "organizations/123", basePolicy(), client)
			if err != nil {
				t.Fatal(err)
			}
			if result.Coverage.Complete || result.Coverage.Classifications[ClassBlocked] != 1 || len(result.Imports) != 0 {
				t.Fatalf("permission failure bypassed: %+v", result)
			}
		})
	}
}
