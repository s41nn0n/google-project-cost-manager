# Administrator-owned state. Never apply with a budget writer or runtime identity.
resource "google_project_iam_member" "runtime_billing_unlink" {
  for_each = var.managed_project_ids
  project  = each.value
  role     = "roles/billing.projectManager"
  member   = "serviceAccount:${var.runtime_service_account_email}"
}
resource "google_billing_account_iam_member" "runtime_budget_viewer" {
  for_each           = var.billing_account_ids
  billing_account_id = each.value
  role               = "roles/billing.viewer"
  member             = "serviceAccount:${var.runtime_service_account_email}"
}
variable "managed_project_ids" { type = set(string) }
variable "billing_account_ids" { type = set(string) }
variable "runtime_service_account_email" { type = string }
variable "control_project_id" {
  type = string
  validation {
    condition     = !contains(var.managed_project_ids, var.control_project_id)
    error_message = "The control project must never receive runtime billing-unlink grants."
  }
}
