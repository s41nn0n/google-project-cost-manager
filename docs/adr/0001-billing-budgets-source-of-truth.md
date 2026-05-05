# ADR 0001: Use GCP Billing Budgets as the Source of Truth

## Status

Accepted

## Context

The service receives Cloud Billing Budget Pub/Sub alerts and uses local configuration to decide whether to disable billing on one or more projects.

Initially, we considered managing Billing Budgets directly from this application or from Terraform. That works when budgets and project scopes are static, but in the intended environment budgets and projects may change over time. Budgets may be created, deleted, renamed, or have project filters updated by finance/platform teams outside this application.

If this repo tried to own all budgets as the source of truth, it could drift from the actual Billing Budget setup or overwrite changes made elsewhere.

## Decision

GCP Cloud Billing Budgets are the source of truth for budget existence, project scope, thresholds, and Pub/Sub alert wiring.

This repo's configuration is the source of truth for enforcement policy only:

- dry-run vs enforcement
- protected projects
- threshold used for enforcement decisions
- per-budget/per-project action overrides
- unknown alert behavior
- self-test settings
- safety limits

The application should not mutate Billing Budgets by default.

## Consequences

### Positive

- Existing operational ownership of Billing Budgets is preserved.
- Finance/platform teams can update budgets without needing to update Terraform in this repo first.
- The app can detect and report drift instead of silently assuming config and GCP match.
- Safer open-source default: no automated budget rewrites.

### Negative

- The app needs read access to Billing Budgets for reconciliation/diff checks.
- The app may receive alerts for budgets that are not configured for enforcement.
- Config must still be maintained for enforcement policy.

## Follow-up design

Add a reconciliation/diff feature that compares configured enforcement policy to current GCP Billing Budgets and reports differences.

The default mode should be read-only.
