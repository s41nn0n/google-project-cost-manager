# Mock operations use the real provider schema, without credentials or GCP writes.
mock_provider "google" {}

override_data {
  target = data.google_project.control
  values = {
    number = "123456789012"
  }
}

variables {
  control_project_id    = "test-control"
  image_digest          = "example.invalid/billing-guard@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
  notification_channels = ["projects/test-control/notificationChannels/123"]
}

run "documented_budget_publisher_identity" {
  command = plan

  assert {
    condition = (
      google_pubsub_topic_iam_member.billing_budget_publisher.member == "serviceAccount:billing-budget-alert@system.gserviceaccount.com" &&
      google_pubsub_topic_iam_member.billing_budget_publisher.role == "roles/pubsub.publisher" &&
      google_pubsub_topic_iam_member.billing_budget_publisher.project == "test-control" &&
      google_pubsub_topic_iam_member.billing_budget_publisher.topic == "billing-budget-alerts"
    )
    error_message = "Use Google's singular billing-budget-alert identity, with publisher access only to the existing plural-named budget topic."
  }
}

run "delivery_service_agents_remain_separate" {
  command = plan

  assert {
    condition = (
      google_pubsub_topic_iam_member.dead_letter_forwarder.member == "serviceAccount:service-123456789012@gcp-sa-pubsub.iam.gserviceaccount.com" &&
      google_pubsub_topic_iam_member.dead_letter_forwarder.role == "roles/pubsub.publisher" &&
      google_pubsub_topic_iam_member.dead_letter_forwarder.topic == "billing-budget-alerts-dead-letter" &&
      google_cloud_run_v2_service_iam_member.pubsub_receiver.member == "serviceAccount:billing-guard-pubsub@test-control.iam.gserviceaccount.com" &&
      google_cloud_run_v2_service_iam_member.pubsub_receiver.name == "billing-guard-receiver" &&
      google_cloud_run_v2_service_iam_member.scheduler_admin.member == "serviceAccount:billing-guard-scheduler@test-control.iam.gserviceaccount.com" &&
      google_cloud_run_v2_service_iam_member.scheduler_admin.name == "billing-guard-admin"
    )
    error_message = "The budget publisher, dead-letter forwarder, event receiver, and administrative invoker must retain separate identities and targets."
  }
}

run "bootstrap_policy_stays_non_enforcing" {
  command = plan

  assert {
    condition = (
      yamldecode(google_secret_manager_secret_version.bootstrap_policy[0].secret_data).defaults.dryRun == true &&
      length(yamldecode(google_secret_manager_secret_version.bootstrap_policy[0].secret_data).budgets) == 0 &&
      yamldecode(google_secret_manager_secret_version.bootstrap_policy[0].secret_data).protectedProjects == ["test-control"] &&
      yamldecode(google_secret_manager_secret_version.bootstrap_policy[0].secret_data).unknownAlertPolicy == "ignore" &&
      yamldecode(google_secret_manager_secret_version.bootstrap_policy[0].secret_data).maxProjectsDisabledPerEvent == 1
    )
    error_message = "An incomplete initial deployment must retain a dry-run, empty-budget policy that protects the control project."
  }
}
