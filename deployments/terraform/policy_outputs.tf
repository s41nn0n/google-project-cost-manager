output "policy_secret_id" {
  value = module.control_plane.policy_secret_id
}

output "generated_enforcement_policy_yaml" {
  value     = yamlencode(local.generated_policy)
  sensitive = true
}
