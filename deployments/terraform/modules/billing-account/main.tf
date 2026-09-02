locals {
  account_id = trimprefix(var.billing_account_name, "billingAccounts/")
  managed = {
    for id, project in var.projects : id => project
    if project.classification == "managed"
  }
  protected = {
    for id, project in var.projects : id => project.protected_reason
    if project.classification == "protected"
  }
}

resource "google_billing_budget" "project" {
  depends_on      = [terraform_data.rollout_gate]
  for_each        = local.managed
  billing_account = local.account_id
  display_name    = each.value.budget_display_name != "" ? each.value.budget_display_name : "billing-guard-${each.key}"
  ownership_scope = "BILLING_ACCOUNT"
  budget_filter {
    projects               = ["projects/${each.value.project_number}"]
    calendar_period        = "MONTH"
    credit_types_treatment = "INCLUDE_ALL_CREDITS"
  }
  amount {
    specified_amount {
      currency_code = each.value.currency_code != "" ? each.value.currency_code : var.currency_code
      units         = floor(each.value.monthly_amount > 0 ? each.value.monthly_amount : var.default_monthly_amount)
      nanos         = floor((((each.value.monthly_amount > 0 ? each.value.monthly_amount : var.default_monthly_amount) - floor(each.value.monthly_amount > 0 ? each.value.monthly_amount : var.default_monthly_amount)) * 1000000000) + 0.5)
    }
  }
  threshold_rules {
    threshold_percent = 0.5
    spend_basis       = "CURRENT_SPEND"
  }
  threshold_rules {
    threshold_percent = var.enforcement_threshold
    spend_basis       = "CURRENT_SPEND"
  }
  threshold_rules {
    threshold_percent = 1.0
    spend_basis       = "CURRENT_SPEND"
  }
  all_updates_rule {
    pubsub_topic   = var.pubsub_topic
    schema_version = "1.0"
  }
  deletion_policy = "ABANDON"

  lifecycle { prevent_destroy = true }
}

resource "google_project_iam_member" "runtime_billing_unlink" {
  for_each = local.managed
  project  = each.key
  role     = "roles/billing.projectManager"
  member   = "serviceAccount:${var.runtime_service_account_email}"
}

resource "google_billing_account_iam_member" "runtime_budget_viewer" {
  billing_account_id = local.account_id
  role               = "roles/billing.viewer"
  member             = "serviceAccount:${var.runtime_service_account_email}"
}
