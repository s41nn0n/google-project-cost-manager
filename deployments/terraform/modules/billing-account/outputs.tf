output "managed_budget_resource_names" {
  # Google 8 exposes only the budget UUID in name; id is account-qualified.
  value = { for id, budget in google_billing_budget.project : id => budget.id }
}

output "coverage_summary" {
  value = {
    managed   = length(local.managed)
    protected = length(local.protected)
    total     = length(var.projects)
  }
}

output "enforcement_policy" {
  value = {
    billing_account_name = var.billing_account_name
    budgets = {
      for id, project in local.owned : id => {
        budget_resource_name = google_billing_budget.project[id].id
        billing_account_name = var.billing_account_name
        project_id           = id
        project_number       = project.project_number
        enforcement_mode     = project.classification == "managed" ? var.enforcement_mode : "dry_run"
        threshold            = var.enforcement_threshold
        monthly_amount       = project.monthly_amount > 0 ? project.monthly_amount : var.default_monthly_amount
        currency_code        = project.currency_code != "" ? project.currency_code : var.currency_code
      }
    }
    protected_projects = local.protected
  }
}
