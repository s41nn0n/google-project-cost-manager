output "state_bucket" { value = google_storage_bucket.state.name }
output "state_prefixes" {
  value = {
    bootstrap      = "bootstrap"
    control_plane  = "control-plane"
    billing        = "billing-accounts/ACCOUNT_ID"
    iam_onboarding = "iam-onboarding"
  }
}
output "plan_service_account" { value = google_service_account.plan.email }
output "apply_service_account" { value = google_service_account.apply.email }
output "control_service_account" { value = google_service_account.control.email }
output "budget_service_accounts" { value = { for id, sa in google_service_account.budget : id => sa.email } }
output "workload_identity_provider" { value = google_iam_workload_identity_pool_provider.github.name }
