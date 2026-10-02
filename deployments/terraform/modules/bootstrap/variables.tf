variable "control_project_id" { type = string }
variable "state_bucket_name" { type = string }
variable "state_bucket_location" {
  type    = string
  default = "US"
}
variable "github_repository" {
  type        = string
  description = "Exact private GitHub repository in owner/name form."
  validation {
    condition     = can(regex("^[^/]+/[^/]+$", var.github_repository))
    error_message = "github_repository must be owner/name."
  }
}
variable "github_repository_id" {
  type = string
  validation {
    condition     = can(regex("^[0-9]+$", var.github_repository_id))
    error_message = "GitHub's immutable numeric repository ID is required."
  }
}
variable "github_repository_owner_id" {
  type = string
  validation {
    condition     = can(regex("^[0-9]+$", var.github_repository_owner_id))
    error_message = "GitHub's immutable numeric owner ID is required."
  }
}
variable "github_apply_actor_ids" {
  type        = set(string)
  description = "Reviewed numeric GitHub users permitted to initiate apply, including scheduled workflow actor."
  validation {
    condition     = length(var.github_apply_actor_ids) > 0 && alltrue([for id in var.github_apply_actor_ids : can(regex("^[0-9]+$", id))])
    error_message = "At least one reviewed numeric apply actor ID is required."
  }
}
variable "billing_account_ids" {
  type    = set(string)
  default = []
  validation {
    condition     = alltrue([for id in var.billing_account_ids : can(regex("^[0-9A-F]{6}-[0-9A-F]{6}-[0-9A-F]{6}$", id))])
    error_message = "Use billing account IDs without billingAccounts/."
  }
}
variable "workload_identity_pool_id" {
  type    = string
  default = "billing-guard-github"
}
