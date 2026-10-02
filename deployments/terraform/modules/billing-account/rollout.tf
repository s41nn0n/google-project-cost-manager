variable "successful_daily_reconciliations" {
  type        = number
  description = "Deprecated and ignored; verified Firestore evidence is required instead."
  default     = 0
}

variable "billing_toggle_test_passed" {
  type        = bool
  description = "Deprecated and ignored; an observed operator test is recorded in Firestore."
  default     = false
}

# Numeric/boolean variables are deprecated compatibility inputs, not readiness evidence.
# Live readiness is verified against persistent daily results and toggle evidence
# both before policy publication and on each enforcement request.
removed {
  from = terraform_data.rollout_gate
  lifecycle { destroy = false }
}
