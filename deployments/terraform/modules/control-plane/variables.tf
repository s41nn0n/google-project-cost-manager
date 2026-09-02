variable "control_project_id" {
  type        = string
  description = "Protected project that hosts the FinOps control plane."
}

variable "region" {
  type    = string
  default = "us-central1"
}

variable "firestore_location" {
  type    = string
  default = "nam5"
}

variable "image_digest" {
  type        = string
  description = "Immutable container reference including @sha256:digest."
  validation {
    condition     = can(regex("@sha256:[0-9a-f]{64}$", var.image_digest))
    error_message = "image_digest must be an immutable @sha256 reference."
  }
}

variable "existing_policy_secret_id" {
  type    = string
  default = null
}

variable "admin_invoker_members" {
  type    = set(string)
  default = []
}

variable "reconcile_schedule" {
  type    = string
  default = "0 6 * * *"
}

variable "self_test_schedule" {
  type    = string
  default = "0 7 * * *"
}

variable "notification_channels" {
  type    = list(string)
  default = []
}
