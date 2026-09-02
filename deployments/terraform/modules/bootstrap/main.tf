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

resource "google_storage_bucket_iam_member" "state_apply" {
  bucket = google_storage_bucket.state.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.apply.email}"
}

resource "google_iam_workload_identity_pool" "github" {
  project                   = var.control_project_id
  workload_identity_pool_id = var.workload_identity_pool_id
  display_name              = "Billing Guard private GitHub repository"
  depends_on                = [google_project_service.bootstrap]
}

resource "google_iam_workload_identity_pool_provider" "github" {
  project                            = var.control_project_id
  workload_identity_pool_id          = google_iam_workload_identity_pool.github.workload_identity_pool_id
  workload_identity_pool_provider_id = "github"
  display_name                       = "Private deployment GitHub Actions"
  attribute_mapping = {
    "google.subject"       = "assertion.sub"
    "attribute.repository" = "assertion.repository"
    "attribute.ref"        = "assertion.ref"
  }
  attribute_condition = "assertion.repository == '${var.github_repository}'"
  oidc { issuer_uri = "https://token.actions.githubusercontent.com" }
}

locals {
  github_principal       = "principalSet://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/attribute.repository/${var.github_repository}"
  github_apply_principal = "principal://iam.googleapis.com/${google_iam_workload_identity_pool.github.name}/subject/repo:${var.github_repository}:environment:${var.github_apply_environment}"
}

resource "google_service_account_iam_member" "github_plan" {
  service_account_id = google_service_account.plan.name
  role               = "roles/iam.workloadIdentityUser"
  member             = local.github_principal
}

resource "google_service_account_iam_member" "github_apply" {
  service_account_id = google_service_account.apply.name
  role               = "roles/iam.workloadIdentityUser"
  member             = local.github_apply_principal
}
