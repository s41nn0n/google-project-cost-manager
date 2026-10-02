# Security implementation verification

Local verification on 2026-10-02 is not evidence of deployed IAM or production
acceptance. Organization-specific migration commands and remaining operator
decisions belong in the private repository.

## Checks

- Go unit tests, race tests, and vet.
- Local Firestore emulator: claim recovery/fencing, duplicate delivery, distinct
  daily evidence, sticky failures, and independent persistent operator stop.
- Terraform root and bootstrap/control/billing/IAM modules validate without GCP
  credentials. Google provider major 8; all tested locks use 8.5.0.
- Six credential-free Terraform budget contract tests cover canonical scope,
  amount/currency precision, current-spend thresholds, dry-run defaults, retained
  unbilled budgets, and rejection of blocked or invalid policy inputs.
- Terraform 1.12.2 and 1.15.9 both pass the module tests and validate the roots.
- An offline real-provider 8.5.0 create plan passes the scoped plan checker.
  Its captured plan fixture tests 15 valid/invalid period and selector cases.
- Private roots validate in isolated scratch copies against these local modules;
  no backend state or live credentials were used for those validations.
- Public/private workflow lint and shell parsing checks.
- Source govulncheck: zero reachable vulnerabilities.
- Final rebuilt image Trivy scan: zero HIGH/CRITICAL findings in Debian and the
  Go server binary, with no ignore-unfixed setting.
- Binary govulncheck requires the narrowly reviewed exception below; all other
  vulnerable symbol findings fail. The exception parser has regression tests.

## Google provider 8 upgrade

All public modules and private deployment roots require Google provider
`>= 8.5.0, < 9.0.0`, with generated 8.5.0 lock files. The existing security
controls remain in place; this upgrade does not broaden deployment authority.
The [official version 8 upgrade guide](https://registry.terraform.io/providers/hashicorp/google/8.5.0/docs/guides/version_8_upgrade)
was checked against the resources this framework manages. Its Secret Manager
write-only argument change does not affect our use of `secret_data`.

The real provider suppresses explicit `MONTH` against an absent period and
leaves empty optional-computed labels unknown on creation. The checker accepts
the API's [documented monthly default](https://docs.cloud.google.com/billing/docs/reference/budget/rest/v1/billingAccounts.budgets#Filter)
only when the period/custom-period fields are known. Unknown creation labels
require proof of the exact constant-empty configuration in the saved plan;
unknown update/import labels and other selectors remain rejected. The module
also sets services explicitly empty. Mock tests alone do not exercise these
provider planning behaviors, so the real-plan fixture is retained as a regression.

Do not apply a saved provider-7 plan after upgrading. Publish the pinned framework,
run `terraform init -upgrade`, create a fresh plan against the existing state,
and review it before applying. No live state migration or GCP apply was performed
during this upgrade. Unexpected infrastructure deletes or replacements must
stop the rollout; the documented bootstrap removal of legacy IAM grants is
intentional and remains an administrator-reviewed step.

## Version-bound binary scan exception

GO-2026-6443 is reported against the linked
google.golang.org/grpc/internal/transport.http2Server.HandleStreams symbol in
grpc v1.84.0. The Go database currently treats that version as affected, but the
[upstream gRPC advisory](https://github.com/grpc/grpc-go/security/advisories/GHSA-2v4p-qf9q-27wj)
explicitly lists stable v1.84.0 as patched. The downloaded v1.84.0 source includes
the rejection of requests missing both host and authority headers; the
[upstream fix and stable backport](https://github.com/grpc/grpc-go/pull/9365)
describe that change. This application serves net/http, not an xDS/gRPC server;
its gRPC use is outbound Google API clients.

scripts/check-vulnerability-report allows only this exact ID, module, version,
package, receiver and symbol. It does not suppress another vulnerability or an
older version. Review this exception when updating gRPC or the vulnerability
database, and remove it when the database reflects the stable backport.
No prerelease dependency was substituted solely to satisfy scanner version order.

Module-only GO-2026-5932 concerns unmaintained x/crypto/openpgp; application source
and built binary do not import that package. It is not a reachable-symbol finding
and is not an allowlisted symbol exception.

## Still required before production

Publish the verified public commit and immutable image; migrate WIF/IAM and
separated state grants; configure and test access-review notifications and real
alert recipients; inspect zero-change imports and real onboarding plans; verify
dry-run delivery and control protection; perform an explicitly approved disposable
billing cycle; collect seven actual error-free days; enable the independent
operator switch explicitly. Account/state isolation and denied impersonation need
live operator checks. No production resource or billing mutation was performed
by the local verification described here.
