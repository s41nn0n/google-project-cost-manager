# Reconciliation, rollout, and recovery

Reconciliation is read-only. Terraform owns canonical guard budgets; unrelated
budgets remain externally owned. POST /reconcile is exposed only by the admin
Cloud Run service. The Pub/Sub identity cannot invoke it or self-test routes.

Canonical checks cover exact resource identity, single-project scope, monthly
amount/currency/credit treatment, current-spend 50/80/100 thresholds, notification
topic, current billing account, and the runtime's billing-unlink permission.
Canonical drift or missing visibility/permissions is an error, not a warning.
An already-unbilled project can reconcile successfully if its guard budget is
retained; no billing assignment is invented or restored.

## Evidence and enablement

Daily reconciliation writes Firestore evidence keyed by the reviewed policy and
release fingerprint. Seven distinct consecutive successful UTC dates are needed;
repeated runs on one day do not advance the gate, and a same-day failure is sticky.
The streak ends today or yesterday; today's explicit failure always blocks.
Material changes reset evidence. Inventory observation must be within 48 hours,
verified by a fresh complete discovery before policy publication.

The operator explicitly approves one disposable project with no workloads:

~~~sh
go run ./cmd/verify-rollout -policy /private/enforcement-policy.json \
  -disposable-project APPROVED_DISPOSABLE_PROJECT \
  -billing-account billingAccounts/ACCOUNT-ID \
  -confirm billing-toggle:APPROVED_DISPOSABLE_PROJECT
go run ./cmd/readiness -policy /private/enforcement-policy.json
~~~

This test observes disable and explicit restoration using operator credentials,
then records evidence. It refuses the control project and an unexpected initial
account. Evidence expires after 30 days; schedule an explicit retest. The HTTP
billing-toggle route is always forbidden. Runtime never restores production
billing, including on failures.

Publish reviewed live policy only after readiness succeeds. Global enabled/live
policy is necessary but insufficient: the independent Firestore operator switch
defaults off. Enable after publication using the current policy:

~~~sh
go run ./cmd/enforcement -policy /private/enforcement-policy.json \
  -enable -confirm enable-billing-unlink
~~~

Every real unlink verifies persisted readiness and the switch again immediately
before its billing API call. Missing, expired, stale, or failed evidence stops it.
Do not substitute mutable GitHub counters for evidence.

## Emergency stop

~~~sh
go run ./cmd/enforcement -policy /private/enforcement-policy.json -disable
~~~

This stop survives subsequent CI policy publication. Also set deployment mode to
dry_run, global enable false, and pause automation when investigating trust.
An already-running API call may complete; a stop is not transactional cancellation.
Any actual billing recovery remains an explicit operator action.

## Delivery recovery and alert acceptance

Claims have a bounded lease. Busy claims return a retryable failure rather than
acknowledging success; expired claims may be recovered; completion validates claim
ownership. Before retrying, check the current account; already-disabled is success,
a different account is refused. Firestore cannot make an external billing call
exactly-once, so interrupted operations are recovered by observing billing state.

The module requires real notification channels and creates safety-failure and
dead-letter-backlog alerts. Verify delivery for disable failures, missing
permissions, protected attempts, stale inventory and reconciliation errors.
Inspect failed messages in the dead-letter subscription explicitly; diagnose the
cause before any operator-approved replay. No unattended replay/restoration path
is provided.

## Terraform acceptance

Regenerate discovery twice and compare committed inventory before every apply.
Reject destroys, replacements, unexpected IAM/account/project targets, critical
unknown values, and uncovered active billed projects using the pinned public
cmd/plancheck. Adoption requires at least one import with every managed action
no-op; import plans mixed with additions/changes are rejected. Use separate
operator-reviewed IAM onboarding; budget writers have no IAM write or delete.

After billing unlink, retain a unique exactly matching guard budget with dry-run
per-project policy. Protected/inactive/reassigned projects still require explicit
lifecycle review rather than accidental budget removal.

Local tests are not live acceptance: validate authenticated dry-run delivery,
route isolation, account/state isolation, alert receipt, the control-project
safety check, the disposable cycle, and seven actual days before enabling.
