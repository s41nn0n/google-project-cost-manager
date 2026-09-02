# ADR 0001: Terraform Owns Canonical Guard Budgets

## Status

Accepted, superseding the earlier decision that treated all GCP budgets as externally owned.

## Context

Organization-wide enforcement needs a deterministic one-to-one binding among a project, billing account, amount, Pub/Sub topic, actual-spend threshold, and runtime policy. Display names and arbitrary externally edited budgets do not provide a safe enforcement identity. At the same time, finance and application teams may already own budgets that this framework must not overwrite.

## Decision

Terraform owns exactly one canonical guard budget for each `managed` active billed project. Its default display name is `billing-guard-<project-id>` and its generated resource name is the live enforcement identity. Terraform does not own unrelated budgets.

An existing budget is eligible for adoption only when account, canonical display name, single-project scope, monthly period, specified amount/currency, credit treatment, Pub/Sub topic, and the 50/80/100 current-spend rules match exactly. Generated import blocks must plan with zero remote changes. No candidate creates a new canonical budget; multiple candidates block rollout.

Managed budget resources use both Terraform `prevent_destroy` and provider `deletion_policy = "ABANDON"`. The apply identity must be granted create/get/list/update but not budget delete permission. Removing a budget from configuration therefore cannot delete the remote guard budget.

## Consequences

- Live alerts can be matched by billing-account and budget resource IDs rather than display name.
- Existing unrelated budgets remain untouched and visible in inventory.
- New projects produce a narrow reviewed change: one budget, billing-control IAM, and generated policy.
- Adoption is intentionally strict; operators resolve any planned drift before import.
