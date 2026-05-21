terraform {
  required_version = ">= 1.5"
  required_providers {
    google = { source = "hashicorp/google", version = ">= 5.0" }
  }
}

variable "project_id" {
  type = string
}

variable "region" {
  type    = string
  default = "us-central1"
}

variable "image" {
  type = string
}

variable "config_yaml" {
  type      = string
  sensitive = true
  default   = null
}

variable "existing_config_secret_id" {
  type    = string
  default = null
}

variable "scheduler_cron" {
  type    = string
  default = "0 9 * * *"
}

variable "billing_account_id" {
  type    = string
  default = ""
}

variable "budget_display_name" {
  type    = string
  default = "billing-guard-budget"
}

variable "budget_amount_units" {
  type    = string
  default = "1000"
}

provider "google" {
  project = var.project_id
  region  = var.region
}

locals {
  config_secret_id = var.existing_config_secret_id != null && var.existing_config_secret_id != "" ? var.existing_config_secret_id : try(google_secret_manager_secret.config[0].id, null)
}

data "google_project" "current" {
  project_id = var.project_id
}

resource "google_project_service" "required" {
  for_each = toset([
    "artifactregistry.googleapis.com",
    "billingbudgets.googleapis.com",
    "cloudbilling.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "cloudscheduler.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
    "pubsub.googleapis.com",
  ])
  project            = var.project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_service_account" "runtime" {
  account_id   = "billing-guard"
  display_name = "Billing Guard Cloud Run runtime"
  depends_on   = [google_project_service.required]
}

resource "google_secret_manager_secret" "config" {
  count     = var.existing_config_secret_id == null || var.existing_config_secret_id == "" ? 1 : 0
  secret_id = "billing-guard-config"
  replication {
    auto {}
  }
  depends_on = [google_project_service.required]
}

resource "google_secret_manager_secret_version" "config" {
  count       = (var.existing_config_secret_id == null || var.existing_config_secret_id == "") && var.config_yaml != null ? 1 : 0
  secret      = google_secret_manager_secret.config[0].id
  secret_data = var.config_yaml
}

resource "google_secret_manager_secret_iam_member" "runtime_config" {
  secret_id = local.config_secret_id
  role      = "roles/secretmanager.secretAccessor"
  member    = "serviceAccount:${google_service_account.runtime.email}"
}

resource "google_cloud_run_v2_service" "app" {
  name       = "billing-guard"
  location   = var.region
  depends_on = [google_project_service.required]

  lifecycle {
    precondition {
      condition     = (var.existing_config_secret_id != null && var.existing_config_secret_id != "") || var.config_yaml != null
      error_message = "Set config_yaml or existing_config_secret_id. Without a secret version the service cannot load config."
    }
  }

  template {
    service_account = google_service_account.runtime.email
    containers {
      image = var.image
      env {
        name  = "CONFIG_BACKEND"
        value = "secretmanager"
      }
      env {
        name  = "CONFIG_SECRET_NAME"
        value = local.config_secret_id
      }
      env {
        name  = "CONFIG_SECRET_VERSION"
        value = "latest"
      }
    }
  }
}

resource "google_pubsub_topic" "billing_alerts" {
  name       = "billing-budget-alerts"
  depends_on = [google_project_service.required]
}

# Optional sample/bootstrap budget only. Ongoing budget existence, scope,
# thresholds, and notification wiring are owned in GCP Billing Budgets, not app
# config. The app only reads budgets for reconciliation and enforces local policy.
resource "google_billing_budget" "alerts" {
  count           = var.billing_account_id != "" ? 1 : 0
  billing_account = var.billing_account_id
  display_name    = var.budget_display_name

  budget_filter {
    projects = ["projects/${data.google_project.current.number}"]
  }
  amount {
    specified_amount {
      currency_code = "USD"
      units         = var.budget_amount_units
    }
  }
  threshold_rules {
    threshold_percent = 0.8
  }
  all_updates_rule {
    pubsub_topic   = google_pubsub_topic.billing_alerts.id
    schema_version = "1.0"
  }
  depends_on = [google_project_service.required]
}

resource "google_service_account" "pubsub_invoker" {
  account_id   = "billing-guard-pubsub"
  display_name = "Pub/Sub invoker for Billing Guard"
}

resource "google_cloud_run_v2_service_iam_member" "pubsub_invoker" {
  location = google_cloud_run_v2_service.app.location
  name     = google_cloud_run_v2_service.app.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.pubsub_invoker.email}"
}

resource "google_pubsub_subscription" "push" {
  name  = "billing-guard-push"
  topic = google_pubsub_topic.billing_alerts.name
  push_config {
    push_endpoint = "${google_cloud_run_v2_service.app.uri}/pubsub/billing-alert"
    oidc_token {
      service_account_email = google_service_account.pubsub_invoker.email
      audience              = google_cloud_run_v2_service.app.uri
    }
  }
}

resource "google_service_account" "scheduler_invoker" {
  account_id   = "billing-guard-scheduler"
  display_name = "Scheduler invoker for Billing Guard"
}

resource "google_cloud_run_v2_service_iam_member" "scheduler_invoker" {
  location = google_cloud_run_v2_service.app.location
  name     = google_cloud_run_v2_service.app.name
  role     = "roles/run.invoker"
  member   = "serviceAccount:${google_service_account.scheduler_invoker.email}"
}

resource "google_cloud_scheduler_job" "self_test" {
  name     = "billing-guard-self-test"
  region   = var.region
  schedule = var.scheduler_cron
  http_target {
    http_method = "POST"
    uri         = "${google_cloud_run_v2_service.app.uri}/self-test"
    # Empty body lets the service use selfTest.mode from config. To force a mode,
    # change this to base64encode(jsonencode({ mode = "dry_run" })).
    body    = base64encode(jsonencode({}))
    headers = { "Content-Type" = "application/json" }
    oidc_token {
      service_account_email = google_service_account.scheduler_invoker.email
      audience              = google_cloud_run_v2_service.app.uri
    }
  }
}

# IAM placeholders:
# Grant the runtime service account Cloud Billing permissions needed to read and update
# project billing state. roles/billing.projectManager is usually granted on the
# billing account, which may be outside this project and must be granted by a billing admin.
# Example (outside this project, adapt to your org):
# gcloud beta billing accounts add-iam-policy-binding BILLING_ACCOUNT_ID \
#   --member="serviceAccount:${google_service_account.runtime.email}" \
#   --role="roles/billing.projectManager"
#
# For read-only reconciliation/setup_validation, also grant this runtime service
# account permission to list/read Billing Budgets on the billing account and read
# project metadata for project-number to project-ID resolution. Exact roles vary by
# organization; these grants are commonly made outside this project.
