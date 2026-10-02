# Exercise the real provider schema with mock operations: no credentials or GCP writes.
mock_provider "google" {}

variables {
  billing_account_name          = "billingAccounts/AAAAAA-BBBBBB-CCCCCC"
  pubsub_topic                  = "projects/test-control/topics/billing-guard"
  runtime_service_account_email = "billing-guard-runtime@test-control.iam.gserviceaccount.com"
  default_monthly_amount        = 100
  currency_code                 = "USD"
  projects = {
    test-workload = {
      project_number = "123456789012"
      classification = "managed"
      monthly_amount = 12.5
      currency_code  = "EUR"
    }
  }
}

run "canonical_budget_contract" {
  command = plan

  assert {
    condition = (
      google_billing_budget.project["test-workload"].deletion_policy == "ABANDON" &&
      google_billing_budget.project["test-workload"].ownership_scope == "BILLING_ACCOUNT" &&
      google_billing_budget.project["test-workload"].display_name == "billing-guard-test-workload"
    )
    error_message = "Canonical budgets must retain their ownership and abandonment safeguards."
  }
  assert {
    condition = (
      google_billing_budget.project["test-workload"].budget_filter[0].calendar_period == "MONTH" &&
      google_billing_budget.project["test-workload"].budget_filter[0].credit_types_treatment == "INCLUDE_ALL_CREDITS" &&
      toset(google_billing_budget.project["test-workload"].budget_filter[0].projects) == toset(["projects/123456789012"])
    )
    error_message = "Budgets must have exact single-project monthly scope."
  }
  assert {
    condition = (
      google_billing_budget.project["test-workload"].amount[0].specified_amount[0].currency_code == "EUR" &&
      tonumber(google_billing_budget.project["test-workload"].amount[0].specified_amount[0].units) == 12 &&
      google_billing_budget.project["test-workload"].amount[0].specified_amount[0].nanos == 500000000
    )
    error_message = "Project amount and currency overrides must survive provider upgrades exactly."
  }
  assert {
    condition = (
      toset([for rule in google_billing_budget.project["test-workload"].threshold_rules : rule.threshold_percent]) == toset([0.5, 0.8, 1]) &&
      alltrue([for rule in google_billing_budget.project["test-workload"].threshold_rules : rule.spend_basis == "CURRENT_SPEND"]) &&
      google_billing_budget.project["test-workload"].all_updates_rule[0].pubsub_topic == var.pubsub_topic &&
      google_billing_budget.project["test-workload"].all_updates_rule[0].schema_version == "1.0" &&
      output.enforcement_policy.budgets["test-workload"].enforcement_mode == "dry_run"
    )
    error_message = "Current-spend notifications and dry-run defaults must remain unchanged."
  }
}

run "retain_unbilled_without_live_enforcement" {
  command = plan
  variables {
    enforcement_mode = "live"
    projects = {
      test-workload = {
        project_number = "123456789012"
        classification = "managed"
      }
      test-disabled = {
        project_number       = "123456789013"
        classification       = "unbilled"
        budget_resource_name = "billingAccounts/AAAAAA-BBBBBB-CCCCCC/budgets/retained"
      }
      test-unbilled = {
        project_number = "123456789014"
        classification = "unbilled"
      }
      test-control = {
        project_number   = "123456789015"
        classification   = "protected"
        protected_reason = "Test control plane"
      }
      test-inactive = {
        project_number = "123456789016"
        classification = "inactive"
      }
    }
  }
  assert {
    condition = (
      toset(keys(google_billing_budget.project)) == toset(["test-workload", "test-disabled"]) &&
      output.enforcement_policy.budgets["test-workload"].enforcement_mode == "live" &&
      output.enforcement_policy.budgets["test-disabled"].enforcement_mode == "dry_run" &&
      output.enforcement_policy.budgets["test-workload"].monthly_amount == 100 &&
      output.enforcement_policy.budgets["test-workload"].currency_code == "USD" &&
      output.coverage_summary.managed == 1 &&
      output.coverage_summary.protected == 1 &&
      output.coverage_summary.total == 5
    )
    error_message = "Retain owned unbilled budgets without enabling them or managing exempt projects."
  }
}

run "reject_blocked_projects" {
  command = plan
  variables {
    projects = {
      test-blocked = {
        project_number = "123456789017"
        classification = "blocked"
      }
    }
  }
  expect_failures = [var.projects]
}

run "reject_unreviewed_protection" {
  command = plan
  variables {
    projects = {
      test-protected = {
        project_number = "123456789018"
        classification = "protected"
      }
    }
  }
  expect_failures = [var.projects]
}

run "reject_missing_positive_default" {
  command = plan
  variables {
    default_monthly_amount = 0
  }
  expect_failures = [var.default_monthly_amount]
}

run "reject_other_enforcement_threshold" {
  command = plan
  variables {
    enforcement_threshold = 0.9
  }
  expect_failures = [var.enforcement_threshold]
}
