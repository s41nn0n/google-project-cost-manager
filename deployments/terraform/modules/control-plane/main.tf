data "google_project" "control" {
  project_id = var.control_project_id
}

locals {
  required_services = toset([
    "billingbudgets.googleapis.com",
    "cloudbilling.googleapis.com",
    "cloudresourcemanager.googleapis.com",
    "cloudscheduler.googleapis.com",
    "firestore.googleapis.com",
    "logging.googleapis.com",
    "monitoring.googleapis.com",
    "pubsub.googleapis.com",
    "run.googleapis.com",
    "secretmanager.googleapis.com",
  ])
  policy_secret_id = coalesce(var.existing_policy_secret_id, "projects/${var.control_project_id}/secrets/billing-guard-policy")
  runtime_member   = "serviceAccount:billing-guard-runtime@${var.control_project_id}.iam.gserviceaccount.com"
}

resource "google_project_service" "required" {
  for_each           = local.required_services
  project            = var.control_project_id
  service            = each.value
  disable_on_destroy = false
}

resource "google_service_account" "runtime" {
  project      = var.control_project_id
  account_id   = "billing-guard-runtime"
  display_name = "Organization Billing Guard runtime"
  depends_on   = [google_project_service.required]
}

resource "google_service_account" "pubsub_invoker" {
  project      = var.control_project_id
  account_id   = "billing-guard-pubsub"
  display_name = "Billing Guard event receiver invoker"
  depends_on   = [google_project_service.required]
}

resource "google_service_account" "scheduler_invoker" {
  project      = var.control_project_id
  account_id   = "billing-guard-scheduler"
  display_name = "Billing Guard administrative scheduler invoker"
  depends_on   = [google_project_service.required]
}

resource "google_firestore_database" "events" {
  project                     = var.control_project_id
  name                        = "billing-guard-events"
  location_id                 = var.firestore_location
  type                        = "FIRESTORE_NATIVE"
  concurrency_mode            = "PESSIMISTIC"
  app_engine_integration_mode = "DISABLED"
  deletion_policy             = "ABANDON"
  depends_on                  = [google_project_service.required]
}

resource "google_project_iam_member" "runtime_firestore" {
  project    = var.control_project_id
  role       = "roles/datastore.user"
  member     = local.runtime_member
  depends_on = [google_service_account.runtime]
}

resource "google_secret_manager_secret" "policy" {
  count     = var.existing_policy_secret_id == null ? 1 : 0
  project   = var.control_project_id
  secret_id = "billing-guard-policy"
  replication {
    auto {}
  }
  depends_on = [google_project_service.required]
}

resource "google_secret_manager_secret_iam_member" "runtime_policy" {
  project    = var.control_project_id
  secret_id  = local.policy_secret_id
  role       = "roles/secretmanager.secretAccessor"
  member     = local.runtime_member
  depends_on = [google_service_account.runtime, google_secret_manager_secret.policy]
}

resource "google_pubsub_topic" "billing_alerts" {
  project    = var.control_project_id
  name       = "billing-budget-alerts"
  depends_on = [google_project_service.required]
}

resource "google_pubsub_topic" "dead_letter" {
  project    = var.control_project_id
  name       = "billing-budget-alerts-dead-letter"
  depends_on = [google_project_service.required]
}

resource "google_cloud_run_v2_service" "receiver" {
  project             = var.control_project_id
  name                = "billing-guard-receiver"
  location            = var.region
  deletion_protection = true
  ingress             = "INGRESS_TRAFFIC_ALL"
  template {
    service_account = "billing-guard-runtime@${var.control_project_id}.iam.gserviceaccount.com"
    containers {
      image = var.image_digest
      env {
        name  = "ROUTE_MODE"
        value = "receiver"
      }
      env {
        name  = "CONFIG_BACKEND"
        value = "secretmanager"
      }
      env {
        name  = "CONFIG_SECRET_NAME"
        value = local.policy_secret_id
      }
      env {
        name  = "CONFIG_SECRET_VERSION"
        value = "latest"
      }
    }
  }
  depends_on = [google_project_service.required, google_service_account.runtime, google_secret_manager_secret_iam_member.runtime_policy, google_secret_manager_secret_version.bootstrap_policy]
}

resource "google_cloud_run_v2_service" "admin" {
  project             = var.control_project_id
  name                = "billing-guard-admin"
  location            = var.region
  deletion_protection = true
  ingress             = "INGRESS_TRAFFIC_ALL"
  template {
    service_account = "billing-guard-runtime@${var.control_project_id}.iam.gserviceaccount.com"
    containers {
      image = var.image_digest
      env {
        name  = "ROUTE_MODE"
        value = "admin"
      }
      env {
        name  = "CONFIG_BACKEND"
        value = "secretmanager"
      }
      env {
        name  = "CONFIG_SECRET_NAME"
        value = local.policy_secret_id
      }
      env {
        name  = "CONFIG_SECRET_VERSION"
        value = "latest"
      }
    }
  }
  depends_on = [google_project_service.required, google_service_account.runtime, google_secret_manager_secret_iam_member.runtime_policy, google_secret_manager_secret_version.bootstrap_policy]
}

resource "google_cloud_run_v2_service_iam_member" "pubsub_receiver" {
  project    = var.control_project_id
  location   = google_cloud_run_v2_service.receiver.location
  name       = "billing-guard-receiver"
  role       = "roles/run.invoker"
  member     = "serviceAccount:billing-guard-pubsub@${var.control_project_id}.iam.gserviceaccount.com"
  depends_on = [google_cloud_run_v2_service.receiver]
}

resource "google_cloud_run_v2_service_iam_member" "scheduler_admin" {
  project    = var.control_project_id
  location   = google_cloud_run_v2_service.admin.location
  name       = "billing-guard-admin"
  role       = "roles/run.invoker"
  member     = "serviceAccount:billing-guard-scheduler@${var.control_project_id}.iam.gserviceaccount.com"
  depends_on = [google_cloud_run_v2_service.admin]
}

resource "google_cloud_run_v2_service_iam_member" "admin" {
  for_each = var.admin_invoker_members
  project  = var.control_project_id
  location = google_cloud_run_v2_service.admin.location
  name     = google_cloud_run_v2_service.admin.name
  role     = "roles/run.invoker"
  member   = each.value
}

resource "google_service_account_iam_member" "pubsub_token_creator" {
  service_account_id = "projects/${var.control_project_id}/serviceAccounts/billing-guard-pubsub@${var.control_project_id}.iam.gserviceaccount.com"
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:service-${data.google_project.control.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
  depends_on         = [google_project_service.required, google_service_account.pubsub_invoker, google_pubsub_topic.billing_alerts]
}

resource "google_pubsub_subscription" "receiver" {
  project              = var.control_project_id
  name                 = "billing-guard-receiver"
  topic                = google_pubsub_topic.billing_alerts.id
  ack_deadline_seconds = 60
  push_config {
    push_endpoint = "${google_cloud_run_v2_service.receiver.uri}/pubsub/billing-alert"
    oidc_token {
      service_account_email = google_service_account.pubsub_invoker.email
      audience              = google_cloud_run_v2_service.receiver.uri
    }
  }
  dead_letter_policy {
    dead_letter_topic     = google_pubsub_topic.dead_letter.id
    max_delivery_attempts = 5
  }
  retry_policy {
    minimum_backoff = "10s"
    maximum_backoff = "600s"
  }
  depends_on = [google_service_account_iam_member.pubsub_token_creator]
}

resource "google_pubsub_subscription" "dead_letter" {
  project = var.control_project_id
  name    = "billing-guard-dead-letter-review"
  topic   = google_pubsub_topic.dead_letter.id
}

resource "google_cloud_scheduler_job" "reconcile" {
  depends_on = [google_service_account_iam_member.scheduler_token_creator]
  project    = var.control_project_id
  name       = "billing-guard-reconcile"
  region     = var.region
  schedule   = var.reconcile_schedule
  http_target {
    http_method = "POST"
    uri         = "${google_cloud_run_v2_service.admin.uri}/reconcile"
    oidc_token {
      service_account_email = google_service_account.scheduler_invoker.email
      audience              = google_cloud_run_v2_service.admin.uri
    }
  }
}

resource "google_cloud_scheduler_job" "self_test" {
  depends_on = [google_service_account_iam_member.scheduler_token_creator]
  project    = var.control_project_id
  name       = "billing-guard-self-test"
  region     = var.region
  schedule   = var.self_test_schedule
  http_target {
    http_method = "POST"
    uri         = "${google_cloud_run_v2_service.admin.uri}/self-test"
    headers     = { "Content-Type" = "application/json" }
    body        = base64encode(jsonencode({ mode = "setup_validation" }))
    oidc_token {
      service_account_email = google_service_account.scheduler_invoker.email
      audience              = google_cloud_run_v2_service.admin.uri
    }
  }
}

resource "google_logging_metric" "failures" {
  project = var.control_project_id
  name    = "billing_guard_failures"
  filter  = "resource.type=\"cloud_run_revision\" AND jsonPayload.alert_kind=(\"disable_failed\" OR \"permission_missing\" OR \"protected_attempt\" OR \"reconciliation_error\" OR \"stale_inventory\")"
  metric_descriptor {
    metric_kind = "DELTA"
    value_type  = "INT64"
  }
}

resource "google_monitoring_alert_policy" "failures" {
  project               = var.control_project_id
  display_name          = "Billing Guard enforcement or reconciliation failure"
  combiner              = "OR"
  notification_channels = var.notification_channels
  conditions {
    display_name = "Billing Guard logged a safety failure"
    condition_threshold {
      filter          = "resource.type = \"cloud_run_revision\" AND metric.type = \"logging.googleapis.com/user/${google_logging_metric.failures.name}\""
      comparison      = "COMPARISON_GT"
      threshold_value = 0
      duration        = "0s"
      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_SUM"
      }
    }
  }
}

resource "google_monitoring_alert_policy" "dead_letter" {
  project               = var.control_project_id
  display_name          = "Billing Guard dead-letter messages"
  combiner              = "OR"
  notification_channels = var.notification_channels
  conditions {
    display_name = "Dead-letter subscription is non-empty"
    condition_threshold {
      filter          = "resource.type = \"pubsub_subscription\" AND resource.label.subscription_id = \"${google_pubsub_subscription.dead_letter.name}\" AND metric.type = \"pubsub.googleapis.com/subscription/num_undelivered_messages\""
      comparison      = "COMPARISON_GT"
      threshold_value = 0
      duration        = "0s"
      aggregations {
        alignment_period   = "300s"
        per_series_aligner = "ALIGN_MAX"
      }
    }
  }
}
