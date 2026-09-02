# Private Organization Deployment Guide

## Repository boundary

Fork or reference this public framework from a private deployment repository. Only the private repository may contain reviewed policy, generated inventory/imports, state bucket/prefixes, organization and billing IDs, amounts, or exceptions. Pin module sources to an immutable release commit and the container to its published `@sha256:` digest.

## Bootstrap once

Run `modules/bootstrap` locally as an organization administrator. It creates a versioned, uniformly access-controlled GCS bucket with Terraform native state locking and deletion protection, read-only plan/discovery and reviewed-apply service accounts, and GitHub WIF restricted to one exact private `owner/repository`. Review the globally unique bucket name before apply.

Use independent GCS prefixes:

```text
bootstrap
control-plane
billing-accounts/<billing-account-id>
```

Grant the plan identity only Cloud Asset organization search, billing-account list/get, project billing-info read, project metadata read, budget list/get, and state read. Grant the apply identity the exact control-plane, budget create/get/list/update, and billing-control IAM permissions needed by reviewed plans. Do not grant `billing.budgets.delete`; do not create service-account keys.

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

Apply separate billing-account states first, including reviewed imports, and require their plans to contain only expected budgets and FinOps IAM. Apply/update the control-plane policy secret last so it never references a budget that has not reached state. After merge, re-plan the exact commit against current state, save the binary plan, pass the protected GitHub environment approval, and apply that same plan with the write identity.

## Recovery and warnings

Budget notifications may be delayed, duplicated, or out of order, and 80% cannot guarantee a hard cap. Unlinking billing can stop services and destroy resources. The receiver never relinks billing. Recovery is a deliberate operator action after investigating cost and workload impact.

## Analytics milestone

After enforcement is stable, create billing-export projects and a consistently located BigQuery dataset for each account. Enable standard and detailed exports, consolidate with authorized views or scheduled transformations, and build account/project/service/trend dashboards plus anomaly notifications. Keep analytics identities read-only and completely outside the billing-disable path.
