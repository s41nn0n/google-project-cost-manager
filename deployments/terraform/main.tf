provider "google" {
  project = var.control_project_id
  region  = var.region
}

module "control_plane" {
  source                    = "./modules/control-plane"
  control_project_id        = var.control_project_id
  region                    = var.region
  image_digest              = var.image_digest
  existing_policy_secret_id = var.existing_policy_secret_id
  notification_channels     = var.notification_channels
}

module "billing_account" {
  for_each                         = var.billing_accounts
  source                           = "./modules/billing-account"
  billing_account_name             = each.key
  default_monthly_amount           = each.value.default_monthly_amount
  currency_code                    = each.value.currency_code
  projects                         = each.value.projects
  pubsub_topic                     = module.control_plane.pubsub_topic
  runtime_service_account_email    = module.control_plane.runtime_service_account_email
  enforcement_mode                 = var.enforcement_mode
  successful_daily_reconciliations = var.successful_daily_reconciliations
  billing_toggle_test_passed       = var.billing_toggle_test_passed
}

locals {
  generated_policy = {
    schemaVersion               = 2
    enforcementEnabled          = false
    inventoryObservedAt         = var.inventory_observed_at
    releaseId                   = var.image_digest
    organizationId              = var.organization_id
    controlProjectId            = var.control_project_id
    defaults                    = { threshold = 0.8, dryRun = var.enforcement_mode != "live", action = "disable_billing" }
    maxProjectsDisabledPerEvent = 1
    unknownAlertPolicy          = "ignore"
    protectedProjects           = distinct(concat([var.control_project_id], flatten([for account in values(module.billing_account) : keys(account.enforcement_policy.protected_projects)])))
    eventState = {
      backend    = "firestore"
      projectId  = var.control_project_id
      databaseId = module.control_plane.event_database
      collection = "budget-events"
    }
    reconcile = {
      enabled             = true
      sourceOfTruth       = "terraform"
      mode                = "diff_only"
      billingAccountNames = keys(var.billing_accounts)
      requiredPubSubTopic = module.control_plane.pubsub_topic
    }
    budgets = flatten([
      for account in values(module.billing_account) : [
        for budget in values(account.enforcement_policy.budgets) : {
          budgetResourceName = budget.budget_resource_name
          billingAccountName = budget.billing_account_name
          projectId          = budget.project_id
          projectNumber      = budget.project_number
          enforcementMode    = budget.enforcement_mode
          threshold          = budget.threshold
          monthlyAmount      = budget.monthly_amount
          currencyCode       = budget.currency_code
          names              = []
          projects           = [budget.project_id]
        }
      ]
    ])
  }
}
