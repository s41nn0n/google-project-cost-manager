variable "github_apply_environment" {
  type        = string
  description = "Protected GitHub environment required by the apply identity."
  default     = "finops-production"
}
