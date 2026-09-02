variable "billing_account_name" {
  type = string
  validation {
    condition     = can(regex("^billingAccounts/[0-9A-Fa-f-]+$", var.billing_account_name))
    error_message = "billing_account_name must use billingAccounts/ID form."
  }
}

variable "pubsub_topic" { type = string }
variable "runtime_service_account_email" { type = string }
variable "default_monthly_amount" {
  type = number
  validation {
    condition     = var.default_monthly_amount > 0
    error_message = "default_monthly_amount must be positive; the module intentionally has no monetary default."
  }
}
variable "currency_code" { type = string }
variable "enforcement_threshold" {
  type    = number
  default = 0.8
  validation {
    condition     = var.enforcement_threshold == 0.8
    error_message = "Organization-wide automatic enforcement is fixed at the reviewed 80% trigger."
  }
}
variable "enforcement_mode" {
  type    = string
  default = "dry_run"
  validation {
    condition     = contains(["dry_run", "live"], var.enforcement_mode)
    error_message = "enforcement_mode must be dry_run or live."
  }
}
variable "projects" {
  type = map(object({
    project_number       = string
    classification       = string
    protected_reason     = optional(string, "")
    monthly_amount       = optional(number, 0)
    currency_code        = optional(string, "")
    budget_display_name  = optional(string, "")
    budget_resource_name = optional(string, "")
  }))
  validation {
    condition     = alltrue([for p in values(var.projects) : contains(["managed", "protected", "unbilled", "inactive", "blocked"], p.classification)])
    error_message = "Every project must have an explicit inventory classification."
  }
  validation {
    condition     = alltrue([for p in values(var.projects) : p.classification != "protected" || trimspace(p.protected_reason) != ""])
    error_message = "Protected projects require a non-empty reviewed reason."
  }
  validation {
    condition     = alltrue([for p in values(var.projects) : p.classification != "blocked"])
    error_message = "Blocked projects prevent rollout. Resolve discovery and policy diagnostics first."
  }
}
