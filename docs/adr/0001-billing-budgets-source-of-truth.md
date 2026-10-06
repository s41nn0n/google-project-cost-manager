# ADR 0001: Terraform Owns Canonical Guard Budgets

## Status

Accepted, superseding the earlier decision that treated all GCP budgets as externally owned.

## Context

Organization-wide enforcement needs a deterministic one-to-one binding among a project, billing account, amount, Pub/Sub topic, actual-spend threshold, and runtime policy. Display names and arbitrary externally edited budgets do not provide a safe enforcement identity. At the same time, finance and application teams may already own budgets that this framework must not overwrite.

## Decision

Terraform owns exactly one canonical guard budget for each `managed` active billed project. Its default display name is `billing-guard-<project-id>` and its generated resource name is the live enforcement identity. Terraform does not own unrelated budgets.

Canonical guards are standard alerts-only budgets. Preview spend-cap budgets are
outside the managed surface and are identified only by the API's explicit
`spendCap` field, never their name or monetary amount. Their intermittent
visibility must not prevent complete standard-budget and organization-project
coverage. They do not enter adoption, retained guard budgets, generated policy,
or reconciliation matches. Inventory and coverage explicitly scope their budget
completeness claim to standard alerts-only budgets; preview-cap visibility is
not verified. No claim is made that a cap omitted by the API is absent or disabled.

An existing budget is eligible for adoption only when account, canonical display name, single-project scope, monthly period, specified amount/currency, credit treatment, Pub/Sub topic, and the 50/80/100 current-spend rules match exactly. Generated import blocks must plan with zero remote changes. No candidate creates a new canonical budget; multiple candidates block rollout.

Managed budget resources use both Terraform `prevent_destroy` and provider `deletion_policy = "ABANDON"`. Automatic account writers receive get/list/update, not budget creation/deletion or IAM-write permission. An administrator initializes new canonical budgets in the same account state using a reviewed create-only plan: connecting them to Pub/Sub requires caller topic IAM-write permission. CI blocks creates and changes to the whole notification rule; pregranting Google's publisher identity does not replace that caller permission. See [the initialization contract](../using-in-your-google-project.md#administrator-owned-budget-initialization). Removing a budget from configuration cannot delete the remote guard budget.

## Consequences

- Live alerts can be matched by billing-account and budget resource IDs rather than display name.
- Existing unrelated standard budgets remain untouched and visible in inventory.
  Preview caps remain untouched and externally managed, outside the deterministic
  catalog; future support is a separately reviewed capability, not a new IAM grant.
- Closed accounts remain visible and require full read visibility, but need a reviewed default only when an active billing link or retained canonical guard requires one. Open accounts always require reviewed defaults, including after reopening. Canonical names can block unsafe omissions but never prove ownership or justify adoption.
- New projects produce a narrow reviewed change: one administrator-initialized budget, separately reviewed billing-control IAM, and generated policy. Automatic deployment resumes only after initialization and refreshed inventory are reviewed.
- Adoption is intentionally strict; operators resolve any planned drift before import.
