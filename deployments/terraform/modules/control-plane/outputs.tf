output "runtime_service_account_email" { value = google_service_account.runtime.email }
output "pubsub_invoker_service_account_email" { value = google_service_account.pubsub_invoker.email }
output "scheduler_invoker_service_account_email" { value = google_service_account.scheduler_invoker.email }
output "pubsub_topic" { value = google_pubsub_topic.billing_alerts.id }
output "policy_secret_id" { value = local.policy_secret_id }
output "event_database" { value = google_firestore_database.events.name }
output "receiver_uri" { value = google_cloud_run_v2_service.receiver.uri }
output "admin_uri" { value = google_cloud_run_v2_service.admin.uri }
