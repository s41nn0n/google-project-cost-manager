# Reconciliation and Rollout Gate

Reconciliation is read-only. Terraform is the canonical source for guard budgets; GCP remains the observed source for their current remote state and for unrelated budgets.

`POST /reconcile` is exposed only by the administrative Cloud Run service. It lists every configured billing account and checks canonical budget existence, project scope, 80% enforcement threshold, and Pub/Sub wiring. `POST /self-test` with `{"mode":"setup_validation"}` fails when reconciliation reports error-severity diffs.

Run a daily reconciliation in dry-run mode for at least seven consecutive days. Do not switch any budget to `enforcementMode: live` until all seven runs have zero errors and a controlled `billing_toggle` self-test has disabled and restored billing on a disposable project. Restoration is part of that explicit test only; alert handling never restores billing.

Alert on:

- disable failures or missing billing permissions;
- protected-project attempts;
- stale private inventory;
- reconciliation errors;
- dead-letter subscription backlog.

The control-plane module creates a structured-log metric and a DLQ backlog policy. The private repository should additionally compare the last merged inventory commit/run time to its freshness objective and emit `alert_kind=stale_inventory` when overdue.

Pull-request automation must regenerate discovery outputs twice or compare against committed output, reject non-determinism, and inspect `terraform show -json` to reject deletes, replacements, unrelated IAM, or uncovered active projects. Imports are accepted only when the post-import saved plan reports zero adds, changes, replacements, and destroys.
