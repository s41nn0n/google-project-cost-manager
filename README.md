# Organization-Wide Google Cloud Billing Guard

A reusable Terraform and Go FinOps framework for discovering every project and billing account in one Google Cloud organization, managing one canonical guard budget per eligible project, and optionally unlinking billing when actual monthly spend reaches 80%.

This public repository contains no organization IDs, project IDs, billing-account IDs, amounts, exceptions, imports, or Terraform backend configuration. Keep all of those in a separate private deployment repository and consume this repository at an immutable release commit. Disabling billing can terminate services and cause unrecoverable resource loss; 80% is a delayed safety signal, not a guaranteed hard cap.

## Components

- `cmd/discover`: read-only Cloud Asset Inventory, Cloud Billing, and Billing Budgets scanner.
- `deployments/terraform/modules/bootstrap`: locked/versioned GCS state, read/control/per-account identities, and numeric-ID/main/workflow/actor-restricted GitHub WIF.
- `deployments/terraform/modules/control-plane`: isolated event/admin Cloud Run services, Pub/Sub/DLQ, Firestore state, Secret Manager, Scheduler, monitoring, and service accounts.
- `deployments/terraform/modules/billing-account`: canonical single-project monthly budgets, coverage, and generated runtime policy; no IAM writes or budget deletes.
- `deployments/terraform/modules/iam-onboarding`: separately reviewed operator-owned runtime billing permissions; never run as a budget writer.
- `cmd/server`: hardened event receiver and administrative reconciler/self-test process.
- `cmd/plancheck`, `cmd/readiness`, `cmd/verify-rollout`, `cmd/enforcement`: scoped plan checks, persisted readiness verification, explicit disposable testing, and independent operator enable/stop.

Terraform 1.12 or newer is required. The public modules constrain the Google provider to tested major version 7; the root example commits its generated lock file. Production consumers should pin this repository by release commit and deploy the image by digest.

## Discovery

Authenticate as a read-only identity with organization Cloud Asset search, billing-account list, project billing-info read, project metadata read, and budget list/get permissions. Then run:

```sh
go run ./cmd/discover \
  -organization organizations/123456789 \
  -policy private/policy/reviewed.yaml \
  -output private/generated
```

Outputs are deterministic and contain no timestamps:

- `inventory.yaml`: normalized accounts, project numbers/states, billing links, classifications, and budget observations.
- `coverage.json`: counts and the complete/incomplete rollout gate.
- `imports.json` and `imports.tf`: exact zero-change Terraform adoption candidates.
- `terraform.auto.tfvars.json`: module input derived from reviewed policy and inventory.
- `diagnostics.json`: machine-readable visibility, amount, and ambiguity failures.

The command exits nonzero for incomplete visibility, inaccessible accounts, missing defaults affecting active projects, or ambiguous exact matches. Existing non-matching budgets are left alone; the module creates `billing-guard-<project-id>` alongside them after review.

## Runtime safety contract

Schema version 2 policy is assembled from Terraform outputs after all account applies. Live enforcement requires the canonical budget resource name, billing account, project ID/number, a single-project scope, reviewed amount/currency, persistent Firestore state, `unknownAlertPolicy: ignore`, and `maxProjectsDisabledPerEvent: 1`. Legacy display-name matching is dry-run only; legacy live configuration is rejected.

The receiver rejects unknown, malformed, stale-period, forecast-only, protected, and multi-project alerts. Actual spend—not an asserted threshold—must reach 80% of the reviewed amount. Global disabled/dry-run always wins over local live settings; the control project is unconditionally protected. Expiring Firestore claims retry unfinished work and fence stale workers. The current billing link is checked again before unlink; already-unlinked is success. It never restores production billing.

Every real unlink requires inventory verified within 48 hours, seven distinct consecutive successful daily reconciliations tied to the release/policy, an observed disposable toggle within 30 days, and an independent persistent operator switch. CI cannot undo an operator stop by republishing a policy. The HTTP toggle route is forbidden; only the explicit operator CLI may restore billing during its disposable test. See [rollout and recovery](docs/reconciliation.md).

After unlink, discovery can retain a uniquely exact guard budget independently of the current billing link with per-project dry-run policy. Other protected/inactive/reassignment transitions require explicit lifecycle review; managed budgets use `prevent_destroy` and `ABANDON`.

The Terraform control plane deploys separate Cloud Run services from the same image using `ROUTE_MODE=receiver|admin`; the Pub/Sub identity cannot invoke reconciliation or self-test routes. Failed deliveries go to a DLQ and monitoring watches both structured safety failures and queued dead letters.

## Development

```sh
test -z "$(gofmt -l .)"
go test ./...
go vet ./...
terraform -chdir=deployments/terraform fmt -check -recursive
terraform -chdir=deployments/terraform init -backend=false -input=false
terraform -chdir=deployments/terraform validate
docker build -t billing-guard:ci .
```

See [the deployment guide](docs/using-in-your-google-project.md), [reconciliation guide](docs/reconciliation.md), and [ADR 0001](docs/adr/0001-billing-budgets-source-of-truth.md).
