# Object-prefix conditions cannot authorize bucket-level listing. Listing names
# does not grant access to another state object's contents.
resource "google_project_iam_custom_role" "state_list" {
  project     = var.control_project_id
  role_id     = "finopsStateList"
  title       = "FinOps state bucket listing"
  permissions = ["storage.buckets.get", "storage.objects.list"]
}
resource "google_storage_bucket_iam_member" "control_list" {
  bucket = google_storage_bucket.state.name
  role   = google_project_iam_custom_role.state_list.name
  member = "serviceAccount:${google_service_account.control.email}"
}
resource "google_storage_bucket_iam_member" "budget_list" {
  for_each = var.billing_account_ids
  bucket   = google_storage_bucket.state.name
  role     = google_project_iam_custom_role.state_list.name
  member   = "serviceAccount:${google_service_account.budget[each.value].email}"
}
