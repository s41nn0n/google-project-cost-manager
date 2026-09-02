output "service_identities" {
  value = {
    runtime   = module.control_plane.runtime_service_account_email
    pubsub    = module.control_plane.pubsub_invoker_service_account_email
    scheduler = module.control_plane.scheduler_invoker_service_account_email
  }
}
output "pubsub_topic" { value = module.control_plane.pubsub_topic }
output "managed_budget_resource_names" { value = { for account, module_value in module.billing_account : account => module_value.managed_budget_resource_names } }
output "coverage_summary" { value = { for account, module_value in module.billing_account : account => module_value.coverage_summary } }
output "generated_enforcement_policy" {
  value     = local.generated_policy
  sensitive = true
}
