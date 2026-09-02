variable "successful_daily_reconciliations" {
  type        = number
  description = "Consecutive daily reconciliation runs with zero errors."
  default     = 0
}

variable "billing_toggle_test_passed" {
  type        = bool
  description = "Whether a controlled disable/restore cycle passed on a disposable project."
  default     = false
}

resource "terraform_data" "rollout_gate" {
  input = {
    enforcement_mode                 = var.enforcement_mode
    successful_daily_reconciliations = var.successful_daily_reconciliations
    billing_toggle_test_passed       = var.billing_toggle_test_passed
  }
  lifecycle {
    precondition {
      condition     = var.enforcement_mode != "live" || (var.successful_daily_reconciliations >= 7 && var.billing_toggle_test_passed)
      error_message = "Live enforcement requires seven consecutive zero-error daily reconciliations and a successful disposable-project billing toggle test."
    }
  }
}
