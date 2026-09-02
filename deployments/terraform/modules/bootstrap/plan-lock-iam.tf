# The plan identity remains read-only for state data. It can only create/delete
# Terraform's lock objects so plans retain normal backend consistency checks.
resource "google_storage_bucket_iam_member" "state_plan_lock" {
  bucket = google_storage_bucket.state.name
  role   = "roles/storage.objectUser"
  member = "serviceAccount:${google_service_account.plan.email}"
  condition {
    title       = "terraform_lock_objects_only"
    description = "Allow normal GCS backend locking without state mutation"
    expression  = "resource.name.endsWith('.tflock')"
  }
}
