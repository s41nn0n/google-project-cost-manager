variable "control_project_id" { type = string }
variable "notification_channels" { type = list(string) }
variable "inventory_observed_at" { type = string }
variable "organization_id" { type = string }
variable "readiness_mode" {
  type    = string
  default = ""
  validation {
    condition     = contains(["", "per_project"], var.readiness_mode)
    error_message = "readiness_mode must be empty (legacy) or per_project."
  }
}
variable "region" {
  type    = string
  default = "us-central1"
}
variable "image_digest" { type = string }
variable "existing_policy_secret_id" {
  type    = string
  default = null
}
variable "enforcement_mode" {
  type    = string
  default = "dry_run"
}
variable "billing_accounts" {
  type = map(object({
    default_monthly_amount = number
    currency_code          = string
    projects = map(object({
      project_number       = string
      classification       = string
      protected_reason     = optional(string, "")
      monthly_amount       = optional(number, 0)
      currency_code        = optional(string, "")
      budget_display_name  = optional(string, "")
      budget_resource_name = optional(string, "")
      enforcement_mode     = optional(string, "")
    }))
  }))
}
