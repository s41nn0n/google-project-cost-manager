# Qualified systems and project onboarding

Initial rollout requires seven genuine successful daily reconciliations and an
observed disposable-project billing test. Adding another project is not another
initial rollout. An explicitly qualified release can use that completed system
validation while requiring current checks for every project that it enforces.

This is an operator-owned migration from the older, whole-policy transition
approval. Nothing automatically creates a qualification. The public
`qualify-system` CLI requires the original policy, its immutable historical
transition approval, an operator, a reason, and explicit confirmation. It checks
the original evidence at the recorded validation time and the new release's real
project preflights. It creates an append-only `system-qualifications` document in
the existing Firestore database. It never changes daily history or records a new
billing test. There is no HTTP qualification endpoint.

The qualification is bound to the exact release and global safety controls,
including organization/control identity, notification topic, event state,
threshold, unknown-alert handling, and one-project-per-event limit. A different
image/release or changed global controls cannot reuse it. Routine onboarding and
changes to another project's budget cannot invalidate an existing project's
scope. The original 30-day test-age check remains unchanged for legacy gates;
qualified systems treat the test as completed initial validation, and use fresh
runtime checks for ongoing readiness rather than periodically requiring another
destructive test.

Use `readinessMode: per_project` and explicitly list reviewed live projects in
`projectEnforcementModes`. Newly discovered projects default to `dry_run`.

1. Discover with complete organization visibility. Keep every existing live
   project's terms unchanged; report additions as pending onboarding.
2. An administrator plans and applies only the new canonical budget in the
   account state. Keep budget writers without Pub/Sub IAM or budget-delete rights.
   A separate administrator-owned IAM plan grants only that project's runtime
   billing-unlink permission. Run the scoped plan checker and the private
   `check-budget-write-plan --operator-onboarding` guard before applying.
3. Regenerate inventory, keep the addition in dry-run, and publish it alongside
   the existing live projects. Its real runtime reconciliation must verify the
   canonical budget, amount, currency, actual thresholds, topic, scope, billing
   account, and unlink permission.
4. Review and change only that project's enforcement mode to `live`. Readiness
   checks its own exact scope and current reconciliation before publication and
   again before unlinking billing. No additional seven-day wait is required for
   a qualified system.

Qualified enforcement requires inventory observed within 48 hours, a successful
project reconciliation within 36 hours, and the independent operator switch.
Current observations are stored separately from daily history. A later successful
preflight can restore current health after a repair while the failed daily entry
remains false. Old or simultaneous successes cannot overwrite a newer failure.
Errors attributed to one project do not poison other projects' event checks;
incomplete/unattributed results or stale inventory fail every affected scope.

The private deployment must refresh the observation of unchanged live coverage
even when an inventory PR is waiting for administrator onboarding. Such a refresh
must verify complete deterministic discovery, all existing project identities,
budget resources, amounts, currencies, modes and protection rules, and update
only `inventoryObservedAt`. It must not add a pending project or call it protected.
Changed existing terms or incomplete visibility block that refresh.

`/readyz`, setup validation, and reconciliation return an unsuccessful HTTP status
when live enforcement is unready. Scheduler success is therefore meaningful.
The receiver still has no administrative or qualification routes. Billing is
never automatically restored.
