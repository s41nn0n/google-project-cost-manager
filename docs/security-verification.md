# Security implementation verification

Local verification on 2026-10-02 is not evidence of deployed IAM or production
acceptance. Organization-specific migration commands and remaining operator
decisions belong in the private repository.

## Checks

- Go unit tests, race tests, and vet.
- Local Firestore emulator: claim recovery/fencing, duplicate delivery, distinct
  daily evidence, sticky failures, and independent persistent operator stop.
- Terraform root and bootstrap/control/billing/IAM modules validate without GCP
  credentials. Google provider major 7; tested locks 7.46.0 and 7.46.1.
- Private roots validate in isolated scratch copies against these local modules;
  no backend state or live credentials were used for those validations.
- Public/private workflow lint and shell parsing checks.
- Source govulncheck: zero reachable vulnerabilities.
- Final rebuilt image Trivy scan: zero HIGH/CRITICAL findings in Debian and the
  Go server binary, with no ignore-unfixed setting.
- Binary govulncheck requires the narrowly reviewed exception below; all other
  vulnerable symbol findings fail. The exception parser has regression tests.

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
