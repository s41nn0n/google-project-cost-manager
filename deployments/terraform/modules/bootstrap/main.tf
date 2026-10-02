data "google_project" "control" { project_id = var.control_project_id }

resource "google_project_service" "bootstrap" {
  for_each           = toset(["iam.googleapis.com", "iamcredentials.googleapis.com", "storage.googleapis.com", "sts.googleapis.com"])
  project            = var.control_project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_storage_bucket" "state" {
  project                     = var.control_project_id
  name                        = var.state_bucket_name
  location                    = var.state_bucket_location
  uniform_bucket_level_access = true
  public_access_prevention    = "enforced"
  force_destroy               = false
  versioning { enabled = true }
  lifecycle { prevent_destroy = true }
  depends_on = [google_project_service.bootstrap]
}

resource "google_service_account" "plan" {
  project      = var.control_project_id
  account_id   = "billing-guard-plan"
  display_name = "Billing Guard read-only discovery and plan"
}

resource "google_service_account" "apply" {
  project      = var.control_project_id
  account_id   = "billing-guard-apply"
  display_name = "Billing Guard reviewed Terraform apply"
}

resource "google_storage_bucket_iam_member" "state_plan" {
  bucket = google_storage_bucket.state.name
  role   = "roles/storage.objectViewer"
  member = "serviceAccount:${google_service_account.plan.email}"
}

resource "google_iam_workload_identity_pool" "github" {
  project                   = var.control_project_id
  workload_identity_pool_id = var.workload_identity_pool_id
  # WIF pool and provider display names have a 32-character API limit.
  display_name = "Billing Guard GitHub pool"
  depends_on   = [google_project_service.bootstrap]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  project                            = var.control_project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github"
  display_name                       = "Billing Guard GitHub provider"
  attribute_mapping = {
    "google.subject"          = "assertion.sub"
    "attribute.repository"    = "assertion.repository"
    "attribute.ref"           = "assertion.ref"
    "attribute.repository_id" = "assertion.repository_id"
    "attribute.apply"         = "assertion.ref == 'refs/heads/main' && assertion.event_name in ['push', 'workflow_dispatch', 'schedule'] && assertion.actor_id in [${join(",", [for id in var.github_apply_actor_ids : "'${id}'"])}] && assertion.workflow_ref == '${var.github_repository}/.github/workflows/terraform.yml@refs/heads/main' ? assertion.repository_id : 'denied'"
  }
  attribute_condition = "assertion.repository == '${var.github_repository}' && assertion.repository_id == '${var.github_repository_id}' && assertion.repository_owner_id == '${var.github_repository_owner_id}'"
  oidc { issuer_uri = "https://token.actions.githubusercontent.com" }
}

locals {
  github_principal       = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository_id/${var.github_repository_id}"
  github_apply_principal = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.apply/${var.github_repository_id}"
}

resource "google_service_account_iam_member" "github_plan" {
  service_account_id = google_service_account.plan.name
  role               = "roles/iam.workloadIdentityUser"
  member             = local.github_principal
}

resource "google_service_account_iam_member" "github_control" {
  service_account_id = google_service_account.control.name
  role               = "roles/iam.workloadIdentityUser"
  member             = local.github_apply_principal
}

resource "google_service_account" "control" {
  project      = var.control_project_id
  account_id   = "billing-guard-control"
  display_name = "FinOps control-plane apply only"
}

resource "google_service_account" "budget" {
  for_each     = var.billing_account_ids
  project      = var.control_project_id
  account_id   = "finops-budget-${substr(sha256(each.value), 0, 12)}"
  display_name = "FinOps budget writer ${each.value}"
}

resource "google_service_account_iam_member" "github_budget" {
  for_each           = var.billing_account_ids
  service_account_id = google_service_account.budget[each.value].name
  role               = "roles/iam.workloadIdentityUser"
  member             = local.github_apply_principal
}

resource "google_storage_bucket_iam_member" "control_state" {
  bucket = google_storage_bucket.state.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.control.email}"
  condition {
    title      = "control_state_only"
    expression = "resource.name.startsWith('projects/_/buckets/${var.state_bucket_name}/objects/control-plane/')"
  }
}

resource "google_storage_bucket_iam_member" "budget_state" {
  for_each = var.billing_account_ids
  bucket   = google_storage_bucket.state.name
  role     = "roles/storage.objectUser"
  member   = "serviceAccount:${google_service_account.budget[each.value].email}"
  condition {
    title      = "billing_account_state_only"
    expression = "resource.name.startsWith('projects/_/buckets/${var.state_bucket_name}/objects/billing-accounts/${each.value}/')"
  }
}
