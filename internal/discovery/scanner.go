package discovery

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type Client interface {
	ListProjects(context.Context, string) ([]Project, error)
	ListBillingAccounts(context.Context, string) ([]BillingAccount, error)
	GetProjectBilling(context.Context, string) (BillingLink, error)
	ListBudgets(context.Context, string) ([]ExistingBudget, error)
}

func Scan(ctx context.Context, organization string, policy ReviewedPolicy, client Client) (Result, error) {
	policy.ApplyDefaults()
	if !strings.HasPrefix(organization, "organizations/") {
		return Result{}, fmt.Errorf("organization must use organizations/ID form")
	}
	if err := policy.Validate(); err != nil {
		return Result{}, err
	}
	projects, err := client.ListProjects(ctx, organization)
	if err != nil {
		return Result{}, fmt.Errorf("discover organization projects: %w", err)
	}
	accounts, err := client.ListBillingAccounts(ctx, organization)
	if err != nil {
		return Result{}, fmt.Errorf("list organization billing accounts: %w", err)
	}

	budgetCoverage := BudgetDiscoveryCoverage{Scope: BudgetScopeStandardAlertsOnly, PreviewSpendCaps: PreviewSpendCapVisibilityNotVerified}
	result := Result{Inventory: Inventory{SchemaVersion: 1, Organization: organization, BillingAccounts: accounts, BudgetDiscovery: budgetCoverage}, Coverage: Coverage{Classifications: map[string]int{}, Complete: true, BudgetDiscovery: budgetCoverage}}
	accountSet := map[string]bool{}
	budgets := map[string][]ExistingBudget{}
	inaccessibleAccounts := map[string]bool{}
	for _, account := range accounts {
		accountSet[account.Name] = true
		accountPolicy, configured := policy.BillingAccounts[account.Name]
		// Closed accounts remain visible, but need no invented financial policy
		// unless an active billing link or retained guard budget requires one.
		if account.Open && (!configured || accountPolicy.DefaultMonthlyAmount <= 0) {
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "missing_default_amount", Severity: "error", BillingAccount: account.Name, Message: "every open billing account requires a positive defaultMonthlyAmount"})
			result.Coverage.Complete = false
		}
		listed, listErr := client.ListBudgets(ctx, account.Name)
		if listErr != nil {
			inaccessibleAccounts[account.Name] = true
			code := "billing_account_inaccessible"
			if errors.Is(listErr, ErrBudgetInventoryUnstable) {
				code = "budget_inventory_unstable"
			}
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: code, Severity: "error", BillingAccount: account.Name, Message: listErr.Error()})
			result.Coverage.Complete = false
			continue
		}
		listed = standardBudgets(listed)
		for i := range listed {
			listed[i].Classification = "externally_owned"
		}
		budgets[account.Name] = listed
		result.Inventory.Budgets = append(result.Inventory.Budgets, listed...)
	}

	for _, project := range projects {
		entry := ProjectInventory{Project: project}
		if project.LifecycleState != "ACTIVE" {
			entry.Classification, entry.Reason = ClassInactive, "project lifecycle state is "+project.LifecycleState
			result.addProject(entry)
			continue
		}
		link, linkErr := client.GetProjectBilling(ctx, project.ProjectID)
		if linkErr != nil {
			entry.Classification, entry.Reason = ClassBlocked, "billing visibility: "+linkErr.Error()
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "project_billing_inaccessible", Severity: "error", ProjectID: project.ProjectID, Message: linkErr.Error()})
			result.Coverage.Complete = false
			result.addProject(entry)
			continue
		}
		entry.BillingLink = link
		if reason, ok := policy.ProtectedProjects[project.ProjectID]; ok || project.ProjectID == policy.ControlProjectID {
			entry.Classification = ClassProtected
			if project.ProjectID == policy.ControlProjectID {
				entry.Reason = "FinOps control plane project (automatically protected)"
			} else {
				entry.Reason = reason
			}
			result.addProject(entry)
			continue
		}
		if !link.BillingEnabled || link.BillingAccountName == "" {
			entry.Classification, entry.Reason = ClassUnbilled, "project has no active billing link"
			// Budget ownership survives billing unlink. Never invent a billing link:
			// retain only a unique, exactly matching canonical budget separately.
			accountNames := make([]string, 0, len(budgets))
			for name := range budgets {
				accountNames = append(accountNames, name)
			}
			sort.Strings(accountNames)
			for _, name := range accountNames {
				candidates := budgets[name]
				account, configured := policy.BillingAccounts[name]
				if !configured || account.DefaultMonthlyAmount <= 0 {
					// A canonical name is not proof of ownership or safe adoption.
					// It does require review before omitting a potentially retained
					// guard budget; never infer a default from the remote amount.
					if hasCanonicalBudget(candidates, project.ProjectID) {
						entry.Classification, entry.Reason = ClassBlocked, "retained canonical guard budget requires a positive reviewed default monthly amount"
						result.Coverage.Complete = false
						result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "missing_default_amount", Severity: "error", ProjectID: project.ProjectID, BillingAccount: name, Message: entry.Reason})
					}
					continue
				}
				candidate := entry
				candidate.MonthlyAmount = account.DefaultMonthlyAmount
				if override := account.ProjectOverrides[project.ProjectID]; override > 0 {
					candidate.MonthlyAmount = override
				}
				candidate.CurrencyCode = account.CurrencyCode
				candidate.CanonicalDisplayName = "billing-guard-" + project.ProjectID
				for _, budget := range compatibleBudgets(budgetsForProject(candidates, project.ProjectNumber), candidate, policy) {
					if entry.ImportCandidate != "" {
						entry.Classification, entry.Reason = ClassBlocked, "ambiguous retained guard budgets"
						result.Coverage.Complete = false
						result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "ambiguous_retained_budget", Severity: "error", ProjectID: project.ProjectID, Message: entry.Reason})
						continue
					}
					entry.MonthlyAmount, entry.CurrencyCode = candidate.MonthlyAmount, candidate.CurrencyCode
					entry.CanonicalDisplayName, entry.ImportCandidate = candidate.CanonicalDisplayName, budget.Name
					entry.BudgetAccountName, entry.BudgetClassification = name, "retained_unbilled"
					entry.ExistingBudgets = []ExistingBudget{budget}
				}
			}
			if entry.Classification == ClassUnbilled && entry.ImportCandidate != "" {
				result.Imports = append(result.Imports, ImportCandidate{BillingAccountName: entry.BudgetAccountName, ProjectID: project.ProjectID, TerraformAddress: fmt.Sprintf("module.billing_account[%q].google_billing_budget.project[%q]", entry.BudgetAccountName, project.ProjectID), RemoteID: entry.ImportCandidate})
			}
			result.addProject(entry)
			continue
		}
		if inaccessibleAccounts[link.BillingAccountName] {
			entry.Classification, entry.Reason = ClassBlocked, "billing budgets are not visible for the linked account"
			result.Coverage.Complete = false
			result.addProject(entry)
			continue
		}
		if !accountSet[link.BillingAccountName] {
			entry.Classification, entry.Reason = ClassBlocked, "linked billing account is not visible under the organization"
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "billing_account_not_discovered", Severity: "error", ProjectID: project.ProjectID, BillingAccount: link.BillingAccountName, Message: entry.Reason})
			result.Coverage.Complete = false
			result.addProject(entry)
			continue
		}
		accountPolicy, configured := policy.BillingAccounts[link.BillingAccountName]
		if !configured || accountPolicy.DefaultMonthlyAmount <= 0 {
			entry.Classification, entry.Reason = ClassBlocked, "billing account has no positive default monthly amount"
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "missing_default_amount", Severity: "error", ProjectID: project.ProjectID, BillingAccount: link.BillingAccountName, Message: entry.Reason})
			result.Coverage.Complete = false
			result.addProject(entry)
			continue
		}
		entry.Classification = ClassManaged
		entry.MonthlyAmount = accountPolicy.DefaultMonthlyAmount
		if override := accountPolicy.ProjectOverrides[project.ProjectID]; override > 0 {
			entry.MonthlyAmount = override
		}
		entry.CurrencyCode = accountPolicy.CurrencyCode
		entry.CanonicalDisplayName = "billing-guard-" + project.ProjectID
		entry.ExistingBudgets = budgetsForProject(budgets[link.BillingAccountName], project.ProjectNumber)
		compatible := compatibleBudgets(entry.ExistingBudgets, entry, policy)
		switch len(compatible) {
		case 0:
			entry.BudgetClassification = "create_canonical"
		case 1:
			entry.BudgetClassification = "exact_import_candidate"
			entry.ImportCandidate = compatible[0].Name
			for i := range entry.ExistingBudgets {
				if entry.ExistingBudgets[i].Name == compatible[0].Name {
					entry.ExistingBudgets[i].Classification = "exact_import_candidate"
				}
			}
			for i := range result.Inventory.Budgets {
				if result.Inventory.Budgets[i].Name == compatible[0].Name {
					result.Inventory.Budgets[i].Classification = "exact_import_candidate"
				}
			}
			result.Imports = append(result.Imports, ImportCandidate{
				BillingAccountName: link.BillingAccountName,
				ProjectID:          project.ProjectID,
				TerraformAddress:   fmt.Sprintf("module.billing_account[\"%s\"].google_billing_budget.project[\"%s\"]", link.BillingAccountName, project.ProjectID),
				RemoteID:           compatible[0].Name,
			})
		default:
			entry.Classification, entry.BudgetClassification, entry.Reason = ClassBlocked, "ambiguous_import_candidates", "more than one existing budget exactly matches the canonical policy"
			result.Diagnostics = append(result.Diagnostics, Diagnostic{Code: "ambiguous_budget_match", Severity: "error", ProjectID: project.ProjectID, BillingAccount: link.BillingAccountName, Message: entry.Reason})
			result.Coverage.Complete = false
		}
		result.addProject(entry)
	}
	result.Normalize()
	return result, nil
}

func (r *Result) addProject(project ProjectInventory) {
	r.Inventory.Projects = append(r.Inventory.Projects, project)
	r.Coverage.TotalProjects++
	r.Coverage.Classifications[project.Classification]++
}

func hasCanonicalBudget(budgets []ExistingBudget, projectID string) bool {
	for _, budget := range budgets {
		if budget.DisplayName == "billing-guard-"+projectID {
			return true
		}
	}
	return false
}

func budgetsForProject(all []ExistingBudget, projectNumber string) []ExistingBudget {
	want := "projects/" + projectNumber
	var out []ExistingBudget
	for _, budget := range all {
		for _, project := range budget.Projects {
			if project == want {
				out = append(out, budget)
				break
			}
		}
	}
	return out
}

func compatibleBudgets(candidates []ExistingBudget, project ProjectInventory, policy ReviewedPolicy) []ExistingBudget {
	wantMoney := MoneyFromFloat(project.CurrencyCode, project.MonthlyAmount)
	wantProject := "projects/" + project.ProjectNumber
	var compatible []ExistingBudget
	for _, budget := range candidates {
		period := budget.CalendarPeriod
		if period == "" {
			period = "MONTH"
		}
		credits := budget.CreditTypesTreatment
		if credits == "" || credits == "CREDIT_TYPES_TREATMENT_UNSPECIFIED" {
			credits = "INCLUDE_ALL_CREDITS"
		}
		if budget.SpendCap || budget.DisplayName != project.CanonicalDisplayName || budget.OwnershipScope != "BILLING_ACCOUNT" || !budget.FilterCompatible || !budget.NotificationCompatible || len(budget.Projects) != 1 || budget.Projects[0] != wantProject || period != "MONTH" || !budget.Amount.Equal(wantMoney) || credits != "INCLUDE_ALL_CREDITS" || budget.PubSubTopic != policy.RequiredPubSubTopic || !exactThresholds(budget.Thresholds) {
			continue
		}
		compatible = append(compatible, budget)
	}
	return compatible
}

func exactThresholds(thresholds []Threshold) bool {
	if len(thresholds) != 3 {
		return false
	}
	want := map[float64]bool{0.5: true, 0.8: true, 1.0: true}
	for _, threshold := range thresholds {
		basis := threshold.SpendBasis
		if basis == "" || basis == "BASIS_UNSPECIFIED" {
			basis = "CURRENT_SPEND"
		}
		if basis != "CURRENT_SPEND" || !want[threshold.Percent] {
			return false
		}
		delete(want, threshold.Percent)
	}
	return len(want) == 0
}
