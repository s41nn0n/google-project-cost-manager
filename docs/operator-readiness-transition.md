# Explicit operator approval for urgent onboarding

Default rollout verification still requires seven genuine daily observations and
a recent observed billing-toggle test for the exact policy/release. An operator
can explicitly accept an already validated deployment when adding one project,
after reviewing a compatible release and the new project's configuration.

This exception references the original evidence document. It does not copy days,
renew a test timestamp, or silently approve a new project. It requires:

- A live legacy baseline that passes the original gates without another approval.
- A live legacy target containing exactly one explicitly named project addition.
  All existing project terms and global safety/protection settings must match.
  The immutable release identity may change as part of the reviewed transition.
- Today's actual successful reconciliation for the complete target, including
  runtime permissions, exact budget scope/amount/currency/thresholds/topic, and
  the live billing links. Run it using the target image and a separate disabled
  candidate policy before touching the existing receiver's policy.
- An explicit operator identity, reason, and project-specific confirmation.

```sh
go run ./cmd/approve-readiness \
  -baseline "$PRIVATE_BASELINE_POLICY" \
  -target "$PRIVATE_TARGET_POLICY" \
  -new-project "$NEW_PROJECT_ID" \
  -operator "$OPERATOR_IDENTITY" \
  -reason "$REVIEWED_REASON" \
  -confirm "accept-existing-validation:$NEW_PROJECT_ID"
go run ./cmd/readiness -policy "$PRIVATE_TARGET_POLICY"
```

The CLI creates an append-only `operator-readiness-approvals` document in the
existing event-state database, bound to the entire target fingerprint. Its
expiry is the original test's timestamp plus 30 days; approving a transition
cannot extend that deadline. Approval chaining and overwriting are rejected.
The original evidence remains unchanged. There is no approval HTTP route.

At runtime, fresh inventory, a current successful target reconciliation, and
the independent operator switch remain mandatory. Today's failed observation
cannot be overridden. Changes to the target release or policy invalidate the
approval. Missing/unreadable/expired approvals fail closed. The normal seven-day
path continues to work, and scoped readiness policies retain their existing gates.

Approval is an operator action, using the operator's existing GCP authority.
Do not grant CI or runtime new approval-management privileges for this feature.
The current database IAM already permits the runtime to write event/evidence
documents; review that database trust boundary as part of your IAM model.
Keep organization/project identifiers, amounts, private policies, and receipts
in the private deployment repository.
