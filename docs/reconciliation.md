# Reconciliation and Setup Validation

## Purpose

Budgets and project scopes may change outside this application. Reconciliation exists to compare the application's enforcement configuration with the current state of GCP Cloud Billing Budgets.

The goal is visibility, not mutation.

## Source of truth

GCP Cloud Billing Budgets are the source of truth for:

- budget existence
- budget display names/resource names
- budget project filters
- budget thresholds
- budget Pub/Sub notification wiring

This repo's config is the source of truth for enforcement policy:

- whether enforcement is enabled
- dry-run behavior
- project actions
- protected projects
- unknown alert policy
- self-test behavior
- safety limits

## Recommended default behavior

```yaml
reconcile:
  enabled: true
  sourceOfTruth: gcp_budgets
  mode: diff_only
  billingAccountName: billingAccounts/000000-000000-000000
  requiredPubSubTopic: projects/CORE_PROJECT/topics/billing-budget-alerts
```

`billingAccountName` is required when `reconcile.enabled: true`; the app uses it as the Billing Budgets API parent for `billingAccounts/{id}/budgets:list` and does not infer a billing account from display names.

Default mode is `diff_only`. The app reports differences but does not change GCP budgets.

## Endpoints

```text
POST /reconcile
```

Runs reconciliation and returns a structured diff.

```text
POST /self-test
{
  "mode": "setup_validation"
}
```

Runs reconciliation as part of setup validation and can fail if critical wiring is missing.

## What reconciliation checks

### 1. GCP budget exists but is not configured

This means GCP may send an alert that has no explicit enforcement policy.

Example diff:

```json
{
  "type": "gcp_budget_not_configured",
  "severity": "warning",
  "budgetDisplayName": "Marketing Budget",
  "budgetName": "billingAccounts/000000-000000-000000/budgets/abc",
  "message": "Budget exists in GCP but has no enforcement policy in config"
}
```

### 2. Configured budget missing in GCP

This means the app has policy for a budget that may never send alerts.

```json
{
  "type": "configured_budget_missing_in_gcp",
  "severity": "error",
  "configuredName": "Dev Sandbox Budget",
  "message": "Config has policy for a budget that does not exist in GCP"
}
```

### 3. Project scope drift

This means the projects covered by the GCP budget do not match the projects listed in enforcement config.

```json
{
  "type": "project_scope_diff",
  "severity": "warning",
  "budgetDisplayName": "Shared Labs Budget",
  "projectsInGcpNotConfig": ["lab-c-789"],
  "projectsInConfigNotGcp": ["lab-b-456"]
}
```

### 4. Threshold drift

This means the configured enforcement threshold and GCP budget thresholds differ.

```json
{
  "type": "threshold_diff",
  "severity": "warning",
  "budgetDisplayName": "Dev Sandbox Budget",
  "gcpThresholds": [0.5, 0.8, 1.0],
  "configuredThreshold": 0.8
}
```

### 5. Pub/Sub alert wiring drift

This means a budget may not send alerts to this service.

```json
{
  "type": "pubsub_topic_missing_or_mismatch",
  "severity": "error",
  "budgetDisplayName": "Dev Sandbox Budget",
  "expectedTopic": "projects/core-project/topics/billing-budget-alerts",
  "actualTopic": ""
}
```

## Response shape

```json
{
  "sourceOfTruth": "gcp_budgets",
  "mode": "diff_only",
  "summary": {
    "gcpBudgets": 12,
    "configuredBudgets": 10,
    "errors": 2,
    "warnings": 5
  },
  "diffs": [
    {
      "type": "configured_budget_missing_in_gcp",
      "severity": "error",
      "configuredName": "Old Sandbox Budget"
    },
    {
      "type": "gcp_budget_not_configured",
      "severity": "warning",
      "budgetDisplayName": "New Lab Budget"
    }
  ]
}
```

## Required GCP APIs

Reconciliation requires:

- Cloud Billing Budget API: `billingbudgets.googleapis.com`
- Cloud Billing API: `cloudbilling.googleapis.com`
- Cloud Resource Manager API: `cloudresourcemanager.googleapis.com`

Cloud Resource Manager may be needed because Billing Budgets often refer to projects by project number, while config usually uses project ID.

## Required IAM

The runtime service account needs read access to billing budgets and project metadata, for example:

- billing budget read/list permissions on the billing account
- project metadata read access for project ID/project number resolution

Exact permissions may vary by organization policy.

## Non-goals

The reconciler does not:

- create budgets
- delete budgets
- rewrite budget thresholds
- rewrite budget project filters
- mutate Pub/Sub notification settings

A future opt-in `config` source-of-truth mode could support upsert/prune behavior, but that is intentionally not the default.
