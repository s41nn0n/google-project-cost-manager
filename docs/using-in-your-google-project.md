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

Start with [the example](../examples/private-deployment/policy/reviewed.example.yaml). Every discovered billing account needs a positive `defaultMonthlyAmount`; project overrides are optional. The control project is automatically protected. Every additional protected project requires an explicit ID and non-empty reason.

Discovery assigns exactly one class:

- `managed`: active, billed, visible, amount resolved, and eligible for a guard budget.
- `protected`: explicit reviewed exemption or the control project.
- `unbilled`: no active billing link.
- `inactive`: deletion lifecycle state.
- `blocked`: incomplete visibility, unknown account, missing amount, or ambiguous adoption.

Any active billed `blocked` project prevents rollout.

## State and apply ordering

Require complete fresh discovery and checked plans before any write. Initially create the dry-run control plane, then apply each billing account, then publish the combined policy secret only after every account succeeds. Runtime IAM onboarding is a separate explicit operator plan. Imports require a separate plan with at least one import and zero remote changes. After merge, re-plan the exact commit against current state and apply that checked saved plan using the corresponding scoped identity.

Prefer independent approval/branch protection where available. A single-operator exception must be explicitly accepted and documented privately, monitored for access changes, and revisited before adding writers. Use default read workflow tokens, an isolated publisher with generated-only writes and no cloud identity, and a fail-closed access preflight.

Initial policy is globally disabled/dry-run. Mutable readiness counters are deprecated and ignored. [Live readiness](reconciliation.md) requires seven actual consecutive successful days, fresh verified inventory, a recent observed disposable cycle, and an explicit independent persistent operator switch. Mandatory alert channels must reach a tested real recipient before production.

## Recovery and warnings

Budget notifications may be delayed, duplicated, or out of order, and 80% cannot guarantee a hard cap. Unlinking billing can stop services and destroy resources. The receiver never relinks billing. Recovery is a deliberate operator action after investigating cost and workload impact.

## Analytics milestone

After enforcement is stable, create billing-export projects and a consistently located BigQuery dataset for each account. Enable standard and detailed exports, consolidate with authorized views or scheduled transformations, and build account/project/service/trend dashboards plus anomaly notifications. Keep analytics identities read-only and completely outside the billing-disable path.
