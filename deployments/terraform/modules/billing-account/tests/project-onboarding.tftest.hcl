mock_provider "google" {}

variables {
  billing_account_name          = "billingAccounts/AAAAAA-BBBBBB-CCCCCC"
  pubsub_topic                  = "projects/test-control/topics/billing-guard"
  runtime_service_account_email = "runtime@test-control.iam.gserviceaccount.com"
  default_monthly_amount        = 20
  currency_code                 = "USD"
  enforcement_mode              = "live"
  projects = {
    existing = { project_number = "1", classification = "managed", enforcement_mode = "live" }
    pending  = { project_number = "2", classification = "managed", enforcement_mode = "dry_run", monthly_amount = 5 }
  }
}

run "keep_existing_live_while_new_project_observes" {
  command = plan
  assert {
    condition = (
      output.enforcement_policy.readiness_scope_version == 1 &&
      output.enforcement_policy.budgets["existing"].enforcement_mode == "live" &&
      output.enforcement_policy.budgets["pending"].enforcement_mode == "dry_run" &&
      output.enforcement_policy.budgets["pending"].monthly_amount == 5
    )
    error_message = "A staged project must not disable existing live projects or bypass dry-run onboarding."
  }
}

run "global_dry_run_cannot_be_overridden" {
  command = plan
  variables { enforcement_mode = "dry_run" }
  assert {
    condition     = alltrue([for b in values(output.enforcement_policy.budgets) : b.enforcement_mode == "dry_run"])
    error_message = "A project override must not defeat the global dry-run stop."
  }
}
