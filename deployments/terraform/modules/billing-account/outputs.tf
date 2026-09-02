output "managed_budget_resource_names" {
  value = { for id, budget in google_billing_budget.project : id => budget.name }
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
      for id, project in local.managed : id => {
        budget_resource_name = google_billing_budget.project[id].name
        billing_account_name = var.billing_account_name
        project_id           = id
        project_number       = project.project_number
        enforcement_mode     = var.enforcement_mode
        threshold            = var.enforcement_threshold
      }
    }
    protected_projects = local.protected
  }
}
