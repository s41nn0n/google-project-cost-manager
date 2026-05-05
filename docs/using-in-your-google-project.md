# Using GCP Project Cost Manager in Your Google Project

This guide explains how to deploy this repo into your own Google Cloud environment.

## What you will deploy

The recommended setup is:

```text
Cloud Billing Budget
        ↓
Pub/Sub topic
        ↓
Pub/Sub push subscription with OIDC auth
        ↓
Private Cloud Run service
        ↓
Cloud Billing API
        ↓
Disable billing on configured projects when policy allows it
```

Optional self-test:

```text
Cloud Scheduler
        ↓
Cloud Run /self-test
        ↓
Read billing state / optionally toggle billing on a dedicated test project
```

## Important safety warning

This application can disable billing on Google Cloud projects. That can stop workloads, delete ephemeral resources, or cause outages. Start with `dryRun: true`, use a dedicated test project, and only enable real enforcement after reviewing logs.

## Prerequisites

You need:

- A core Google Cloud project where Cloud Run will live.
- A Cloud Billing account.
- Permission to deploy Cloud Run, Pub/Sub, Scheduler, Secret Manager, and service accounts in the core project.
- Permission to grant billing IAM on the Billing Account, usually from a Billing Account Administrator.
- Docker or Cloud Build to build the container image.
- Terraform if using the Terraform example.

Recommended local tools:

```sh
gcloud
terraform
go
```

## 1. Choose your projects

Use at least two projects:

1. **Core/control project** — hosts this service.
2. **Test project** — used for self-test billing toggles.

Then list any projects that the service may protect or disable.

Example:

```text
core project: my-billing-guard-core
test project: billing-guard-test-123
managed project: dev-sandbox-123
protected project: prod-core-999
```

## 2. Prepare configuration

Start from `configs/example.yaml` and create your own config.

Example safe starting config:

```yaml
defaults:
  threshold: 0.8
  dryRun: true
  action: disable_billing

unknownAlertPolicy: dry_run
unknownAlertProjects: []
maxProjectsDisabledPerEvent: 1

protectedProjects:
  - prod-core-999
  - my-billing-guard-core

reconcile:
  enabled: true
  sourceOfTruth: gcp_budgets
  mode: diff_only
  billingAccountName: billingAccounts/000000-000000-000000
  requiredPubSubTopic: projects/my-billing-guard-core/topics/billing-budget-alerts

budgets:
  - names:
      - Dev Sandbox Budget
    projects:
      - dev-sandbox-123
    threshold: 0.8
    dryRun: true

selfTest:
  enabled: true
  mode: dry_run
  testProjectId: billing-guard-test-123
  allowedProjectIdPattern: ^billing-guard-test-
  minimumIntervalHours: 24
```

### One budget per project

```yaml
budgets:
  - names:
      - Dev Sandbox Budget
    projects:
      - dev-sandbox-123
    threshold: 0.8
    dryRun: false
```

### One budget covering multiple projects

```yaml
budgets:
  - names:
      - Shared Labs Budget
    projects:
      - lab-a-123
      - lab-b-456
      - lab-c-789
    threshold: 0.75
    dryRun: false
```

### Unknown alert behavior

By default, unknown alerts do not disable anything:

```yaml
unknownAlertPolicy: dry_run
unknownAlertProjects: []
```

If you want unknown alerts to affect explicit fallback projects, configure them deliberately:

```yaml
unknownAlertPolicy: disable_billing
unknownAlertProjects:
  - fallback-sandbox-123
```

Do not use this for production projects.

## 3. Store config in Secret Manager

For production, store config in Secret Manager instead of baking it into the container.

```sh
gcloud secrets create billing-guard-config \
  --project=my-billing-guard-core \
  --replication-policy=automatic

gcloud secrets versions add billing-guard-config \
  --project=my-billing-guard-core \
  --data-file=config.yaml
```

The service reads this secret using its Cloud Run runtime service account.

## 4. Store the self-test restore billing account secret, optional

For `billing_toggle`, the service needs to know which billing account to restore on the test project.

Secret value format:

```text
billingAccounts/000000-000000-000000
```

Create the secret:

```sh
printf 'billingAccounts/000000-000000-000000' | \
  gcloud secrets create billing-guard-test-billing-account \
    --project=my-billing-guard-core \
    --replication-policy=automatic \
    --data-file=-
```

Then configure:

```yaml
selfTest:
  mode: billing_toggle
  testProjectId: billing-guard-test-123
  restoreBillingAccountNameSecret: projects/my-billing-guard-core/secrets/billing-guard-test-billing-account
  allowedProjectIdPattern: ^billing-guard-test-
  minimumIntervalHours: 24
```

## 5. Build and publish the container

Example using Artifact Registry:

```sh
gcloud artifacts repositories create billing-guard \
  --project=my-billing-guard-core \
  --repository-format=docker \
  --location=us-central1

gcloud builds submit \
  --project=my-billing-guard-core \
  --tag us-central1-docker.pkg.dev/my-billing-guard-core/billing-guard/billing-guard:latest
```

## 6. Deploy with Terraform

The repo includes an example in `deployments/terraform`.

Create `terraform.tfvars`:

```hcl
project_id = "my-billing-guard-core"
region     = "us-central1"
image      = "us-central1-docker.pkg.dev/my-billing-guard-core/billing-guard/billing-guard:latest"

# Recommended: create config secret out-of-band and pass its id.
existing_config_secret_id = "projects/my-billing-guard-core/secrets/billing-guard-config"

scheduler_cron = "0 8 * * *"
```

Then run:

```sh
cd deployments/terraform
terraform init
terraform plan
terraform apply
```

### Terraform config secret warning

The Terraform module also supports `config_yaml`, but secret values stored this way are written to Terraform state. Prefer `existing_config_secret_id` with an out-of-band Secret Manager secret unless your Terraform state is encrypted and access-controlled.

## 7. Grant IAM permissions

The Cloud Run runtime service account needs access to:

- Read config secrets.
- Read and update project billing info for enforcement.
- List/read Billing Budgets on the billing account for reconciliation.
- Read project metadata through Cloud Resource Manager for project number to ID resolution.

The Terraform example grants Secret Manager access to the config secret.

You must grant billing permissions separately, often on the billing account:

```sh
gcloud beta billing accounts add-iam-policy-binding 000000-000000-000000 \
  --member='serviceAccount:billing-guard@my-billing-guard-core.iam.gserviceaccount.com' \
  --role='roles/billing.projectManager'
```

Also enable reconciliation APIs in the core project if Terraform has not already done so:

```sh
gcloud services enable billingbudgets.googleapis.com cloudresourcemanager.googleapis.com \
  --project=my-billing-guard-core
```

Billing Budget list/read IAM is commonly granted at the billing-account level and may require a custom role or organization-specific role containing Billing Budgets read/list permissions. Project metadata read access can be granted with a read-only project/browser-style role according to your organization policy.

If using self-test restore secrets, also grant access to that secret:

```sh
gcloud secrets add-iam-policy-binding billing-guard-test-billing-account \
  --project=my-billing-guard-core \
  --member='serviceAccount:billing-guard@my-billing-guard-core.iam.gserviceaccount.com' \
  --role='roles/secretmanager.secretAccessor'
```

## 8. Connect Billing Budgets to Pub/Sub

The Terraform example can create an optional sample `google_billing_budget` if you provide `billing_account_id`, but many users will already have budgets.

For an existing budget:

1. Open **Billing > Budgets & alerts**.
2. Select your budget.
3. Configure Pub/Sub notifications.
4. Set the topic to the topic created by Terraform, usually:

```text
projects/my-billing-guard-core/topics/billing-budget-alerts
```

Make sure the budget display name or resource name matches one of the `budgets[].names` values in your config. Set `reconcile.requiredPubSubTopic` to this exact topic and `reconcile.billingAccountName` to the parent billing account, for example `billingAccounts/000000-000000-000000`.

## 9. Validate budget setup with read-only reconciliation

Start a local authenticated proxy to the private Cloud Run service:

```sh
gcloud run services proxy billing-guard \
  --project=my-billing-guard-core \
  --region=us-central1
```

In another shell, run the diff-only reconciler:

```sh
curl -X POST http://localhost:8080/reconcile
```

Then run setup validation through self-test:

```sh
curl -X POST http://localhost:8080/self-test \
  -H 'Content-Type: application/json' \
  -d '{"mode":"setup_validation"}'
```

Interpret diffs as follows:

- `configured_budget_missing_in_gcp` (error): create/restore the GCP Billing Budget or remove stale enforcement policy from config.
- `pubsub_topic_missing_or_mismatch` (error): update the GCP budget notification topic to `reconcile.requiredPubSubTopic`.
- `gcp_budget_not_configured` (warning): add explicit enforcement policy for that budget, or intentionally rely on the unknown alert policy.
- `project_scope_diff` (warning): align GCP budget project filters with config policy, or update config if GCP is correct.
- `threshold_diff` (warning): align the GCP alert thresholds with the enforcement threshold you want the app to use.

Reconciliation and `setup_validation` are read-only and never create, update, or delete GCP Billing Budgets.

## 10. Test in dry-run mode

Using the same local Cloud Run proxy from the previous step, send a dry-run self-test:

```sh
curl -X POST http://localhost:8080/self-test \
  -H 'Content-Type: application/json' \
  -d '{"mode":"dry_run"}'
```

Expected result includes successful config validation and no billing mutations.

## 11. Test billing read access

```sh
curl -X POST http://localhost:8080/self-test \
  -H 'Content-Type: application/json' \
  -d '{"mode":"billing_read_check"}'
```

This verifies the service can read billing info for `selfTest.testProjectId`.

## 12. Optional full billing toggle self-test

Only run this against a dedicated disposable test project.

Config requirements:

```yaml
selfTest:
  mode: billing_toggle
  testProjectId: billing-guard-test-123
  restoreBillingAccountNameSecret: projects/my-billing-guard-core/secrets/billing-guard-test-billing-account
  allowedProjectIdPattern: ^billing-guard-test-
  minimumIntervalHours: 24
```

Run:

```sh
curl -X POST http://localhost:8080/self-test \
  -H 'Content-Type: application/json' \
  -d '{"mode":"billing_toggle"}'
```

The service should:

1. Read current billing state.
2. Disable billing.
3. Confirm disabled.
4. Restore billing.
5. Confirm final state is `billing_enabled`.

## 13. Enable enforcement gradually

Recommended rollout:

1. Keep global `defaults.dryRun: true`.
2. Configure one sandbox budget and project.
3. Watch Cloud Run logs for policy decisions.
4. Set only that sandbox budget to `dryRun: false`.
5. Keep `maxProjectsDisabledPerEvent` low, e.g. `1`.
6. Expand to other non-production projects.

Example:

```yaml
defaults:
  dryRun: true

budgets:
  - names:
      - Sandbox Budget
    projects:
      - sandbox-project-123
    threshold: 0.8
    dryRun: false
```

## 14. Check logs

Example log query:

```sh
gcloud logging read \
  'resource.type="cloud_run_revision" AND resource.labels.service_name="billing-guard"' \
  --project=my-billing-guard-core \
  --limit=50 \
  --format=json
```

## 15. Emergency restore billing

If billing is disabled and you need to restore it manually:

```sh
gcloud beta billing projects link PROJECT_ID \
  --billing-account=000000-000000-000000
```

You can also temporarily force dry-run by updating the config secret:

```yaml
defaults:
  dryRun: true
```

Then add a new Secret Manager version and redeploy/restart if needed.

## 16. Endpoint security checklist

- Do not allow unauthenticated Cloud Run invocations.
- Only Pub/Sub push service account should invoke `/pubsub/billing-alert`.
- Only Scheduler service account or operators should invoke `/self-test`.
- Do not store service account keys in config.
- Use Cloud Run runtime identity / ADC.
- Keep protected projects configured.
- Keep unknown alerts in `dry_run` unless there is a strong reason not to.

## Troubleshooting

### Service returns 503 on `/readyz`

Config cannot be loaded. Check:

- `CONFIG_BACKEND`
- `CONFIG_SECRET_NAME`
- Secret Manager IAM
- YAML syntax

### Budget alerts do not arrive

Check:

- Billing budget Pub/Sub topic is configured.
- Pub/Sub subscription exists.
- Push endpoint is `/pubsub/billing-alert`.
- Pub/Sub invoker service account has `roles/run.invoker`.

### Billing disable fails

Check:

- Runtime service account has `roles/billing.projectManager`.
- Billing API is enabled.
- Target project is linked to the expected billing account.
- Organization policies do not block billing updates.

### Self-test restore fails

Immediately restore manually:

```sh
gcloud beta billing projects link TEST_PROJECT_ID \
  --billing-account=000000-000000-000000
```

Then check the restore billing account secret value and service account permissions.
