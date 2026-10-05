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

Terraform 1.12 or newer is required. The public modules require Google provider 8.5.0 or newer within major version 8; all committed lock files use the tested 8.5.0 release. Production consumers should pin this repository by release commit and deploy the image by digest.

## Releases and private version pins

Release Please maintains the version PR. After its reviewed merge, it creates a
draft and directly calls the artifact workflow; a release created with the
default `GITHUB_TOKEN` does not trigger a separate `release: published` workflow.
See [Release Please's token guidance](https://github.com/googleapis/release-please-action#github-credentials).
No PAT or access-monitor App permissions are needed.

Artifacts are built from the exact trusted main commit, not a draft tag. The
workflow tests/scans source and operator binaries, scans the exact container
before pushing it, and attests both. Publication waits for both artifact jobs.
Operator tools retain symbols for accurate binary vulnerability checks; stripped
binaries cause [govulncheck to fall back to module-level findings](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck).
The release includes five Linux/amd64 tools, `SOURCE_COMMIT`, `SHA256SUMS`, and an
`IMAGE_DIGEST` reference; the notes also record the source commit and image digest.
Retry failed jobs on the original run if interrupted. Existing draft assets are
reused only when their bytes match, never overwritten. A mismatch or source SHA
different from the original workflow requires explicit operator investigation.

Before publishing the next version, enable immutable releases in this public
repository's Settings > General > Releases. This locks version tags/assets after
publication, so all assets must be attached to the draft first. Enabling it does
not retroactively lock older releases. See [GitHub's immutable releases guide](https://docs.github.com/en/code-security/concepts/supply-chain-security/immutable-releases).
Verify a candidate using:

```sh
gh api "repos/$FRAMEWORK_REPO/releases/tags/$FRAMEWORK_VERSION" \
  --jq '{tag_name, target_commitish, immutable, assets: [.assets[].name]}'
```

Keep full commit pins until the required fixes are in a published release with
successful artifact jobs. Use `?ref=vX.Y.Z` for Terraform and the same version in
private workflow checkouts only after verifying `immutable: true`, the expected
source commit, assets, and attestations; use the recorded digest for the image.
Never replace or retag an existing version. `v1.0.0` predates the closed-account
scanner and artifact-publishing fixes; consumers needing those fixes must wait
for a newer release or use the exact fixed commit.

## Discovery

Authenticate as a read-only identity with organization Cloud Asset search, billing-account list, project billing-info read, project metadata read, and budget list/get permissions. Then run:

```sh
go run ./cmd/discover \
  -organization organizations/123456789 \
  -policy private/policy/reviewed.yaml \
  -output private/generated
```

Outputs are deterministic and contain no timestamps:

- `inventory.yaml`: normalized accounts, project numbers/states, billing links, classifications, and standard alert-budget observations.
- `coverage.json`: counts, explicit budget-discovery scope, and the complete/incomplete rollout gate.
- `imports.json` and `imports.tf`: exact zero-change Terraform adoption candidates.
- `terraform.auto.tfvars.json`: module input derived from reviewed policy and inventory.
- `diagnostics.json`: machine-readable visibility, amount, and ambiguity failures.

Every open billing account requires a positive reviewed `defaultMonthlyAmount`, even if it currently has no billed projects. Closed accounts remain visible in inventory and their budgets are still inspected, but unused closed accounts may be omitted from reviewed policy and generated Terraform inputs. A closed account still needs a reviewed default if an active billed project or a potentially retained canonical guard budget depends on it. Reopening an unconfigured account blocks discovery until its financial policy is reviewed.

The command exits nonzero for incomplete visibility, inaccessible accounts (including closed accounts), missing required defaults, or ambiguous exact matches. Existing non-matching budgets are left alone; the module creates `billing-guard-<project-id>` alongside them after review. A canonical name alone never authorizes adoption or supplies a budget amount; it only prevents silently excluding a potentially retained guard when its account policy is missing.

Budget discovery covers **standard alerts-only budgets**. Preview spend caps are
identified only by the API's explicit `spendCap` field, remain externally managed,
and never enter imports, generated enforcement policy, or reconciliation matches.
Their API visibility can be incomplete: both inventory and coverage declare
`budgetDiscovery.scope: standard_alerts_only` and
`budgetDiscovery.previewSpendCaps: not_verified`. Absence from an API response
does not mean that a cap is absent or disabled. The scanner compares two complete
paginated standard-budget lists and blocks on changed resources or
adoption-relevant fields; permission failures and project-coverage gaps still
block. Keep the independent workflow comparisons. See
[the discovery scope and troubleshooting guide](docs/using-in-your-google-project.md#budget-discovery-scope-and-preview-spend-caps).

## TODO: Spend-cap capabilities

- Add a separately opted-in capability for discovering, reviewing, and eventually
  managing service-specific spend caps when API/provider support and visibility
  are reliable; do not make preview caps part of standard-budget completeness.
- Model project/service scope, spend-cap type and state, gross-cost treatment,
  overlapping controls, notifications, and the effects on existing workloads.
- Define separately scoped read/write permissions and explicit adoption and
  lifecycle rules. Preserve existing externally managed caps by default.
- Test complete pagination, configured/enforced/lifted states, permission failures,
  zero-change adoption, and disposable-project behavior. Do not automatically
  lift caps, restore billing, or replace the current 80% actual-spend path.

This is future work, not an enabled capability. Existing caps remain operator-owned.

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
