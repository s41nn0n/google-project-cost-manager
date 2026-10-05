package reconcile

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	budgets "google.golang.org/api/billingbudgets/v1"
	"google.golang.org/api/option"
)

func TestCloudBudgetClientPreservesExplicitSpendCapTypeAcrossPages(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("pageToken") == "next" {
			fmt.Fprint(w, `{"budgets":[{"name":"billingAccounts/A/budgets/cap","displayName":"billing-guard-p1","spendCap":{}}]}`)
		} else {
			fmt.Fprint(w, `{"nextPageToken":"next","budgets":[{"name":"billingAccounts/A/budgets/standard","displayName":"Generated spend cap (Firebase Console)"}]}`)
		}
	}))
	defer server.Close()
	service, err := budgets.NewService(context.Background(), option.WithEndpoint(server.URL+"/"), option.WithoutAuthentication())
	if err != nil {
		t.Fatal(err)
	}
	listed, err := (&CloudBudgetClient{svc: service}).ListBudgets(context.Background(), "billingAccounts/A")
	if err != nil || requests != 2 || len(listed) != 2 || listed[0].SpendCap || !listed[1].SpendCap {
		t.Fatalf("cap type was guessed from names or dropped: requests=%d budgets=%+v error=%v", requests, listed, err)
	}
}
