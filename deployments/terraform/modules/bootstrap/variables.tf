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
variable "workload_identity_pool_id" {
  type    = string
  default = "billing-guard-github"
}
