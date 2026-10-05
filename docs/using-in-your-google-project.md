# Private Organization Deployment Guide

## Repository boundary

Fork or reference this public framework from a private deployment repository. Only the private repository may contain reviewed policy, generated inventory/imports, state bucket/prefixes, organization and billing IDs, amounts, or exceptions. Pin module sources to an immutable release commit and the container to its published `@sha256:` digest.

## Bootstrap once

Run `modules/bootstrap` locally as an organization administrator. It creates a versioned, uniformly access-controlled GCS bucket with Terraform native locking and deletion protection, a read-only identity, a protected control-project writer, and one budget writer per account. WIF requires immutable repository/owner IDs, reviewed numeric actor IDs, main deployment events, and the exact trusted workflow. Use a separate workflow for manual PR review; never run PR code under an apply-capable main-dispatch token.

Use independent GCS prefixes:

```text
bootstrap
control-plane
billing-accounts/<billing-account-id>
iam-onboarding
```

Grant discovery/plan read access plus conditioned .tflock writes. Budget writers get only account-scoped budget create/get/list/update and their own state prefix, never IAM-policy writes, workload authority, or budget delete. Control administration stays in the protected project and its state. Runtime billing unlink is granted only on managed projects through an explicit administrator-owned `iam-onboarding` state, not through budget-writer CI. Never create GCP service-account keys. Review the residual authority of an identity that can change the runtime image/policy.

## Reviewed policy

Start with [the example](../examples/private-deployment/policy/reviewed.example.yaml). Every open billing account needs a positive `defaultMonthlyAmount` in its actual currency, even when no projects currently use it; project overrides are optional. The control project is automatically protected. Every additional protected project requires an explicit ID and non-empty reason.

Closed accounts remain in inventory, with full standard alert-budget visibility still required, but may be omitted from reviewed policy when no active billed project or retained canonical guard budget requires them. Unrelated closed-account standard budgets remain externally owned; discovery does not reopen accounts or manage those budgets. If an unbilled project's canonical budget is present without a reviewed account default, discovery blocks rather than silently dropping it or inferring an amount. A canonical name alone does not qualify a budget for import: all existing exact-match checks still apply. Reopening an unconfigured account requires a reviewed default before rollout can proceed.

Discovery assigns exactly one class:

- `managed`: active, billed, visible, amount resolved, and eligible for a guard budget.
- `protected`: explicit reviewed exemption or the control project.
- `unbilled`: no active billing link.
- `inactive`: deletion lifecycle state.
- `blocked`: incomplete visibility, unknown account, missing amount, or ambiguous adoption.

Any active billed `blocked` project prevents rollout.

### Budget discovery scope and preview spend caps

The scanner reads every account's complete paginated budget list twice, then
excludes only resources carrying the API's explicit `spendCap` field. Names,
amounts, service filters, and ownership scope are not spend-cap type evidence.
Standard alerts-only budgets, including unrelated externally owned budgets,
remain in deterministic inventory. If their resource sets or adoption-relevant
fields differ, discovery returns
`budget_inventory_unstable`, marks coverage incomplete, and selects neither
snapshot for imports. Permission failures remain blocking even on the second
read. Resource, project-filter, and threshold ordering are normalized. Error
details appear in both stderr and private `diagnostics.json`.

[Spend caps are a preview capability](https://docs.cloud.google.com/billing/docs/how-to/budgets-spend-caps)
outside this framework's current managed surface. Intermittent exposure of typed
spend caps must not block stable standard-budget coverage. Caps are excluded from
imports, retained guard budgets, generated enforcement policy, and runtime
reconciliation matches. Both inventory and coverage explicitly declare:

```yaml
budgetDiscovery:
  scope: standard_alerts_only
  previewSpendCaps: not_verified
```

This is not a complete preview-cap catalog. Counts, resource observations, and
cap states from intermittent responses do not enter committed inventory, and an
absent cap must not be treated as deleted, disabled, or lifted. Leave existing
caps under their current operator ownership. They can pause a service separately
from this framework's project-wide billing-unlink action. No additional IAM
permissions, cap deletion, or automatic lifting are required for this correction.

This is a detection guard, not a guarantee that two identical API responses are
complete. Keep the workflow's independent two-scan comparison and comparison
against reviewed inventory. Do not union responses, filter by display name, ignore
ordinary externally owned budgets, weaken project coverage, or remove determinism
checks. If standard-budget responses still differ, investigate concurrent edits,
permissions, and API visibility before deploying. The API's
[pagination contract](https://docs.cloud.google.com/billing/docs/reference/budget/rest/v1/billingAccounts.budgets/list)
defines when a continuation token is required. Future spend-cap support is tracked
in [the capability TODO](../README.md#todo-spend-cap-capabilities).

Organization inventory and account billing links are different scopes. Compare
the account's [linked project list](https://docs.cloud.google.com/billing/docs/reference/rest/v1/billingAccounts.projects/list)
with organization inventory before accepting coverage. Billing accounts can pay
for projects in other organizations; an absent linked project is not evidence
that it is unbilled or protected. An administrator with project metadata access
must establish its organization membership. Resolve missing visibility for
in-scope projects; handling projects outside the selected organization requires
an explicit scope decision. Never silently add them to organization inventory,
drop them from the review, or move them between organizations as a discovery fix.

## State and apply ordering

Require complete fresh discovery and checked plans before any write. Initially create the dry-run control plane, then apply each billing account, then publish the combined policy secret only after every account succeeds. Runtime IAM onboarding is a separate explicit operator plan. Imports require a separate plan with at least one import and zero remote changes. After merge, re-plan the exact commit against current state and apply that checked saved plan using the corresponding scoped identity.

Prefer independent approval/branch protection where available. A single-operator exception must be explicitly accepted and documented privately, monitored for access changes, and revisited before adding writers. Use default read workflow tokens, an isolated publisher with generated-only writes and no cloud identity, and a fail-closed access preflight.

Initial policy is globally disabled/dry-run. Mutable readiness counters are deprecated and ignored. [Live readiness](reconciliation.md) requires seven actual consecutive successful days, fresh verified inventory, a recent observed disposable cycle, and an explicit independent persistent operator switch. Mandatory alert channels must reach a tested real recipient before production.

## Recovery and warnings

Budget notifications may be delayed, duplicated, or out of order, and 80% cannot guarantee a hard cap. Unlinking billing can stop services and destroy resources. The receiver never relinks billing. Recovery is a deliberate operator action after investigating cost and workload impact.

## Analytics milestone

After enforcement is stable, create billing-export projects and a consistently located BigQuery dataset for each account. Enable standard and detailed exports, consolidate with authorized views or scheduled transformations, and build account/project/service/trend dashboards plus anomaly notifications. Keep analytics identities read-only and completely outside the billing-disable path.
