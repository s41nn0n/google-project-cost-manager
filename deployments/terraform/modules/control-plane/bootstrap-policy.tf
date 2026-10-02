# A newly-created secret needs a safe first version before Cloud Run can resolve
# "latest". The reviewed generated policy is added by the deployment root only
# after all billing-account states have applied.
resource "google_secret_manager_secret_version" "bootstrap_policy" {
  count      = var.existing_policy_secret_id == null ? 1 : 0
  secret     = local.policy_secret_id
  depends_on = [google_secret_manager_secret.policy]
  secret_data = yamlencode({
    defaults                    = { threshold = 0.8, dryRun = true, action = "disable_billing" }
    unknownAlertPolicy          = "ignore"
    maxProjectsDisabledPerEvent = 1
    protectedProjects           = [var.control_project_id]
    budgets                     = []
  })
}
