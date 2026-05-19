# GCP Project Cost Manager / Billing Guard

Open-source Go Cloud Run service that receives Cloud Billing Budget Pub/Sub alerts and can dry-run or disable billing on configured projects.

GCP Cloud Billing Budgets are the source of truth for budget existence, scope, thresholds, and Pub/Sub wiring. App config is enforcement policy only and does not create, update, or delete budgets.

## Endpoints

- `GET /healthz` liveness
- `GET /readyz` loads and validates config
- `POST /pubsub/billing-alert` Pub/Sub push target for budget alerts
- `POST /reconcile` read-only/diff-only comparison of GCP Billing Budgets to enforcement config
- `POST /self-test` JSON `{ "mode": "dry_run|billing_read_check|billing_toggle|setup_validation" }`

## Configuration

Set `CONFIG_BACKEND=file|secretmanager`. File mode uses `CONFIG_PATH` (default `configs/example.yaml`). Secret Manager mode uses `CONFIG_SECRET_NAME` and optional `CONFIG_SECRET_VERSION` with Application Default Credentials; do not use service account keys. If using Terraform, note that `config_yaml` is stored in Terraform state even when marked sensitive; for stricter handling, create/update the Secret Manager secret out-of-band and pass its id as `existing_config_secret_id`.

Budgets may map to one project or many projects. Matching uses `budgetDisplayName`, `displayName`, `name`, or `resourceName` from the alert. Defaults are threshold `0.8`, dry-run `true`, action `disable_billing`, and unknown alerts are dry-run. Unknown/unconfigured alerts can be configured with `unknownAlertPolicy: ignore|dry_run|disable_billing`; only explicitly listed `unknownAlertProjects` can be acted on.

Enable read-only reconciliation with:

```yaml
reconcile:
  enabled: true
  sourceOfTruth: gcp_budgets
  mode: diff_only
  billingAccountNames:
    - billingAccounts/000000-000000-000000
  requiredPubSubTopic: projects/CORE_PROJECT/topics/billing-budget-alerts
```

`POST /reconcile` never mutates GCP. Prefer `reconcile.billingAccountNames` for one or more accounts; `reconcile.billingAccountName` remains supported as a legacy single-account shorthand. Error severity diffs indicate critical setup problems, such as configured budgets missing in GCP or Pub/Sub topic mismatch. Warning diffs indicate drift to review.

## IAM and endpoint security

Deploy Cloud Run as a private service; do **not** grant `allUsers`/unauthenticated invoker. The Terraform example grants `roles/run.invoker` only to dedicated Pub/Sub and Cloud Scheduler service accounts, which send OIDC-authenticated requests. Runtime service account needs Secret Manager access for config, Cloud Billing API permissions to read/update project billing for enforcement, Billing Budgets read/list access on the billing account for reconciliation, and project metadata read access for project number to ID resolution. Enable Cloud Billing, Billing Budgets, and Cloud Resource Manager APIs.

## Self-test

`setup_validation` runs read-only reconciliation and fails when error severity diffs are found; warnings are reported but do not fail the mode.

`billing_toggle` is guarded by explicit `selfTest.testProjectId`, not protected, optional `allowedProjectIdPattern`, best-effort `minimumIntervalHours`, and required restore billing account. Prefer `selfTest.restoreBillingAccountNameSecret` pointing to a Secret Manager secret whose value is `billingAccounts/...`; `billingAccountName` is available for local/non-sensitive testing. The test disables billing, confirms disabled, restores billing, and confirms final state `billing_enabled`.

## Development

```sh
go test ./...
docker build -t billing-guard .
```

See `deployments/terraform` for an example Cloud Run deployment. It enables required project APIs and creates the Pub/Sub push path; `billing_account_id` only creates an optional sample/bootstrap Cloud Billing Budget wired to the Pub/Sub topic.

## Design decisions

- [Using this repo in your Google project](docs/using-in-your-google-project.md)
- [Reconciliation and setup validation](docs/reconciliation.md)
- [AI Task Manager operating guide](docs/ai-task-manager.md)
- [ADR 0001: GCP Billing Budgets are the source of truth](docs/adr/0001-billing-budgets-source-of-truth.md)
