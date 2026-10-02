resource "google_pubsub_topic_iam_member" "billing_budget_publisher" {
  project = var.control_project_id
  topic   = google_pubsub_topic.billing_alerts.name
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:billing-budget-alerts@system.gserviceaccount.com"
}

resource "google_pubsub_topic_iam_member" "dead_letter_forwarder" {
  project = var.control_project_id
  topic   = google_pubsub_topic.dead_letter.name
  role    = "roles/pubsub.publisher"
  member  = "serviceAccount:service-${data.google_project.control.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

resource "google_pubsub_subscription_iam_member" "dead_letter_subscriber" {
  project      = var.control_project_id
  subscription = google_pubsub_subscription.receiver.name
  role         = "roles/pubsub.subscriber"
  member       = "serviceAccount:service-${data.google_project.control.number}@gcp-sa-pubsub.iam.gserviceaccount.com"
}

resource "google_service_account_iam_member" "scheduler_token_creator" {
  service_account_id = "projects/${var.control_project_id}/serviceAccounts/billing-guard-scheduler@${var.control_project_id}.iam.gserviceaccount.com"
  role               = "roles/iam.serviceAccountTokenCreator"
  member             = "serviceAccount:service-${data.google_project.control.number}@gcp-sa-cloudscheduler.iam.gserviceaccount.com"
  depends_on         = [google_project_service.required, google_service_account.scheduler_invoker]
}
