package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	billingbudgets "google.golang.org/api/billingbudgets/v1"
	"google.golang.org/api/option"
)

func budgetTestClient(t *testing.T, handler http.HandlerFunc) *GCPClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	service, err := billingbudgets.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	return &GCPClient{budgets: service}
}

func TestListBudgetsVerifiesAllPages(t *testing.T) {
	requests := 0
	client := budgetTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/billingAccounts/A/budgets" {
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		if r.URL.Query().Get("pageToken") == "next" {
			fmt.Fprint(w, `{"budgets":[{"name":"billingAccounts/A/budgets/two"}]}`)
			return
		}
		fmt.Fprint(w, `{"nextPageToken":"next","budgets":[{"name":"billingAccounts/A/budgets/one"}]}`)
	})
	budgets, err := client.ListBudgets(context.Background(), "billingAccounts/A")
	if err != nil || requests != 4 || len(budgets) != 2 {
		t.Fatalf("requests=%d budgets=%+v error=%v", requests, budgets, err)
	}
}

func TestListBudgetsRejectsChangingResponsesWithoutPageTokens(t *testing.T) {
	for _, test := range []struct {
		name, first, second string
	}{
		{"missing", `{"budgets":[{"name":"billingAccounts/A/budgets/one"}]}`, `{"budgets":[]}`},
		{"different_ids", `{"budgets":[{"name":"billingAccounts/A/budgets/one"}]}`, `{"budgets":[{"name":"billingAccounts/A/budgets/two"}]}`},
		{"amount_changed", `{"budgets":[{"name":"billingAccounts/A/budgets/one","amount":{"specifiedAmount":{"currencyCode":"USD","units":"10"}}}]}`, `{"budgets":[{"name":"billingAccounts/A/budgets/one","amount":{"specifiedAmount":{"currencyCode":"USD","units":"11"}}}]}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			client := budgetTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
				requests++
				w.Header().Set("Content-Type", "application/json")
				if requests == 1 {
					fmt.Fprint(w, test.first)
				} else {
					fmt.Fprint(w, test.second)
				}
			})
			budgets, err := client.ListBudgets(context.Background(), "billingAccounts/A")
			if requests != 2 || budgets != nil || !errors.Is(err, ErrBudgetInventoryUnstable) {
				t.Fatalf("requests=%d budgets=%+v error=%v", requests, budgets, err)
			}
		})
	}
}

func TestListBudgetsVerificationPermissionFailureIsBlocking(t *testing.T) {
	requests := 0
	client := budgetTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if requests == 1 {
			fmt.Fprint(w, `{"budgets":[{"name":"billingAccounts/A/budgets/one"}]}`)
			return
		}
		w.WriteHeader(http.StatusForbidden)
		fmt.Fprint(w, `{"error":{"code":403,"message":"permission denied","status":"PERMISSION_DENIED"}}`)
	})
	budgets, err := client.ListBudgets(context.Background(), "billingAccounts/A")
	if requests != 2 || budgets != nil || err == nil || errors.Is(err, ErrBudgetInventoryUnstable) || !strings.Contains(err.Error(), "permission denied") {
		t.Fatalf("requests=%d budgets=%+v error=%v", requests, budgets, err)
	}
}

func TestBudgetSnapshotsIgnoreOrderingWithoutMutatingInputs(t *testing.T) {
	one := exactBudget("billing-guard-p1", "1")
	one.Projects = []string{"projects/2", "projects/1"}
	one.Thresholds = []Threshold{{1, "CURRENT_SPEND"}, {0.5, "CURRENT_SPEND"}, {0.8, "CURRENT_SPEND"}}
	two := exactBudget("billing-guard-p2", "2")
	two.Name = "billingAccounts/A/budgets/two"
	first := []ExistingBudget{one, two}
	data, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var original, second []ExistingBudget
	if err := json.Unmarshal(data, &original); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &second); err != nil {
		t.Fatal(err)
	}
	second[0].Projects[0], second[0].Projects[1] = second[0].Projects[1], second[0].Projects[0]
	second[0].Thresholds[0], second[0].Thresholds[1] = second[0].Thresholds[1], second[0].Thresholds[0]
	second[0], second[1] = second[1], second[0]
	if err := compareBudgetSnapshots(first, second); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, original) {
		t.Fatal("comparison mutated caller's snapshot")
	}
	if err := compareBudgetSnapshots(nil, []ExistingBudget{}); err != nil {
		t.Fatal(err)
	}
}

func TestBudgetSnapshotsRejectAdoptionRelevantChanges(t *testing.T) {
	for name, change := range map[string]func(*ExistingBudget){
		"display_name": func(b *ExistingBudget) { b.DisplayName = "changed" },
		"scope":        func(b *ExistingBudget) { b.Projects[0] = "projects/2" },
		"period":       func(b *ExistingBudget) { b.CalendarPeriod = "YEAR" },
		"amount":       func(b *ExistingBudget) { b.Amount.Units++ },
		"currency":     func(b *ExistingBudget) { b.Amount.CurrencyCode = "EUR" },
		"credits":      func(b *ExistingBudget) { b.CreditTypesTreatment = "EXCLUDE_ALL_CREDITS" },
		"ownership":    func(b *ExistingBudget) { b.OwnershipScope = "ALL_USERS" },
		"filters":      func(b *ExistingBudget) { b.FilterCompatible = false },
		"notification": func(b *ExistingBudget) { b.NotificationCompatible = false },
		"topic":        func(b *ExistingBudget) { b.PubSubTopic = "projects/control/topics/other" },
		"threshold":    func(b *ExistingBudget) { b.Thresholds[0].Percent = 0.6 },
		"spend_basis":  func(b *ExistingBudget) { b.Thresholds[0].SpendBasis = "FORECASTED_SPEND" },
	} {
		t.Run(name, func(t *testing.T) {
			first, second := exactBudget("billing-guard-p1", "1"), exactBudget("billing-guard-p1", "1")
			change(&second)
			err := compareBudgetSnapshots([]ExistingBudget{first}, []ExistingBudget{second})
			if !errors.Is(err, ErrBudgetInventoryUnstable) || !strings.Contains(err.Error(), "changed=1") {
				t.Fatalf("change was not blocked: %v", err)
			}
		})
	}
}

func TestBudgetSnapshotsRejectMalformedIdentity(t *testing.T) {
	for _, budgets := range [][]ExistingBudget{
		{{Name: ""}},
		{{Name: "billingAccounts/A/budgets/one"}, {Name: "billingAccounts/A/budgets/one"}},
	} {
		if err := compareBudgetSnapshots(budgets, budgets); err == nil {
			t.Fatalf("malformed identity accepted: %+v", budgets)
		}
	}
}

func TestScanUnstableBudgetInventoryBlocksImportsAndCoverage(t *testing.T) {
	client := fakeClient{
		projects:  []Project{{ProjectID: "p1", ProjectNumber: "1", LifecycleState: "ACTIVE"}},
		accounts:  []BillingAccount{{Name: "billingAccounts/A", Open: true}},
		links:     map[string]BillingLink{"p1": {BillingEnabled: true, BillingAccountName: "billingAccounts/A"}},
		budgets:   map[string][]ExistingBudget{"billingAccounts/A": {exactBudget("billing-guard-p1", "1")}},
		budgetErr: map[string]error{"billingAccounts/A": fmt.Errorf("%w: resources differ", ErrBudgetInventoryUnstable)},
	}
	result, err := Scan(context.Background(), "organizations/123", basePolicy(), client)
	if err != nil {
		t.Fatal(err)
	}
	if result.Coverage.Complete || result.Coverage.Classifications[ClassBlocked] != 1 || len(result.Imports) != 0 || len(result.Inventory.Budgets) != 0 {
		t.Fatalf("unstable inventory did not block rollout: %+v", result)
	}
	if len(result.Diagnostics) != 1 || result.Diagnostics[0].Code != "budget_inventory_unstable" {
		t.Fatalf("missing actionable diagnostic: %+v", result.Diagnostics)
	}
}
