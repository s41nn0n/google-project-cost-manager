# Safety observations and failed disable attempts

Both `/reconcile` and the scheduled `setup_validation` self-test record real
observations in the existing Firestore event database. A failed setup validation
emits the `reconciliation_error` log entry consumed by the safety alert and marks
the affected current project observations failed. Policy-loading and client-setup
errors also emit a monitored alert. No workload-management permissions are needed.

Reconciliation records have separate start and completion timestamps. Current
health is ordered by start time, so a slow older check cannot overwrite a newer
failure merely by finishing later. Completion time determines freshness and the
daily audit date. Timestamps are compared at Firestore's microsecond precision;
failures win ties. Records from older images use their previous completion time
as a conservative ordering fence until a new check completes. Daily failed entries
remain false even when a genuine later observation restores current readiness.

The notification ledger distinguishes completed work from failed or expired work.
A failed attempt at higher spend is not a successful-spend watermark: a newer,
still-qualifying notification can retry even if reported spend decreased. Active
leases still prevent concurrent processing; older publication times and completed
duplicate messages are still rejected. Existing event keys and stored claims remain
compatible. Billing is never automatically restored.

Tests cover overlapping HTTP checks using the local Firestore emulator, timestamp
precision and legacy ordering, failed/expired claims, cross-client Firestore retry,
duplicate delivery, and setup-validation alert emission. A production upgrade uses
a scanned immutable image and a disabled runtime preflight of the exact policy,
followed by an explicit release qualification before live publication. The original
validation, test timestamps, and historical failures are not rewritten.
