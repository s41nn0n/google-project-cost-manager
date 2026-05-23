# Metrics Alert Trigger Design

## Context

`google-project-cost-manager` currently enforces billing policy from Cloud
Billing Budget Pub/Sub alerts only. The active trigger is
`POST /pubsub/billing-alert`, which decodes a Pub/Sub push envelope, converts
the encoded message into `pubsub.BudgetAlert`, evaluates policy, and disables
billing only when policy allows it.

The new capability adds a metrics-driven trigger. The first supported source is
a Pub/Sub push envelope whose `message.data` contains a Cloud Monitoring alert
notification JSON payload. This is distinct from a direct webhook endpoint.

AI Task Manager remains documentation and coordination only. This feature does
not add executor/runtime-control behavior.

## Goals

- Add a dedicated `POST /pubsub/metrics-alert` endpoint.
- Decode Cloud Monitoring incident notifications carried inside Pub/Sub push
  `message.data`.
- Trigger enforcement only for open incidents.
- Resolve the target project from `incident.resource.labels.project_id` by
  default.
- Allow operators to override the project ID field path in config.
- Require explicit metric policy configuration before a metric alert can disable
  billing.
- Reuse the existing billing disable path and safety controls where possible.
- Use `log/slog` structured logging so logs map cleanly into Google Cloud
  Logging fields.

## Non-Goals

- Do not support direct Cloud Monitoring webhook delivery in this first slice.
- Do not infer arbitrary projects from metrics scopes when the configured field
  path is missing.
- Do not log full Cloud Monitoring incident payloads.
- Do not mutate Cloud Monitoring alert policies or metric descriptors.
- Do not change the existing Billing Budget source-of-truth decision.

## Proposed API

Add:

```text
POST /pubsub/metrics-alert
```

The route accepts the same Pub/Sub push envelope shape as the billing alert
endpoint. The decoded `message.data` payload is expected to contain a Cloud
Monitoring notification with an `incident` object.

The endpoint response should use the same JSON evaluation style as the billing
alert route, extended only as needed to explain metric-specific no-action
reasons.

## Configuration

Add a `metrics` config block:

```yaml
metrics:
  enabled: false
  projectIdFieldPath: incident.resource.labels.project_id
  requirePolicyMatch: true
  policies:
    - names:
        - projects/my-core-project/alertPolicies/123456789
        - High CPU policy
      conditions:
        - CPU usage above 90 percent
      projects:
        - service-project-a
      threshold: 1.0
      dryRun: true
      action: disable_billing
```

Default values:

- `metrics.enabled`: `false`
- `metrics.projectIdFieldPath`: `incident.resource.labels.project_id`
- `metrics.requirePolicyMatch`: `true`

`metrics.policies` is intentionally separate from `budgets` to avoid making
Cloud Monitoring policies look like Cloud Billing Budgets. A policy matches
when the incoming incident `policy_name`, policy display name if present, or
`condition_name` matches a configured policy `names` or `conditions` entry.

The configured field path supplies the target project. `metrics.policies`
controls whether that alert is allowed to act and can override threshold,
dry-run, and action behavior. If a policy entry lists `projects`, those projects
act as an allowlist. If omitted, the extracted project is still required and is
used as the target.

## Data Flow

1. `POST /pubsub/metrics-alert` receives a Pub/Sub push request.
2. The server decodes the push envelope and base64-decodes `message.data`.
3. The metrics parser unmarshals the Cloud Monitoring notification payload.
4. The endpoint loads app config.
5. If `metrics.enabled` is false, the endpoint returns `400` with no action.
6. If `incident.state` is not `open`, the endpoint returns `200` with no
   action and reason `incident_not_open`.
7. The endpoint extracts the project ID from `metrics.projectIdFieldPath`,
   defaulting to `incident.resource.labels.project_id`.
8. If the project ID is missing, the endpoint returns `200` with no action and
   reason `missing_metric_project_id`.
9. The endpoint matches the incident policy or condition against
   `metrics.policies`.
10. If `metrics.requirePolicyMatch` is true and no policy matches, the endpoint
    returns `200` with no action and reason `metric_policy_not_configured`.
11. The endpoint normalizes the incident into an internal enforcement input:
    target project, ratio, threshold, dry-run, action, policy name, condition
    name, metric type, and incident URL.
12. Existing safety checks apply: protected projects, dry-run, action,
    max-projects-disabled-per-event, and billing-disable failure handling.
13. If enforcement is allowed and not dry-run, the endpoint disables billing for
    the target project with the existing billing client.

## Ratio And Threshold Handling

Cloud Monitoring payload fields can vary by policy and condition type. The
parser should try to parse numeric `incident.observed_value` and
`incident.threshold_value` when present. If both are present and threshold is
greater than zero, compute:

```text
ratio = observed_value / threshold_value
```

If the metric policy config provides `threshold`, compare the computed ratio to
that threshold. A threshold of `1.0` means "the observed value reached the
Cloud Monitoring threshold." If numeric values are missing, the incident being
`open` can be treated as threshold reached for configured policies, with the
decision reason making that clear.

## Logging

Use `log/slog`, not `log.Printf`, for the new endpoint and metric-specific
logging. Logs should be structured and concise so Google Cloud Logging can index
the fields. Configure application logging with a JSON `slog` handler for Cloud
Run output, and map slog levels to a Cloud Logging-compatible severity field if
the default handler does not do that in the deployed runtime.

Use context-aware calls such as `InfoContext`, `WarnContext`, and
`ErrorContext` where request context is available.

Required attributes for metric alert logs when available:

- `trigger`: `metrics_alert`
- `reason`
- `project_id`
- `incident_state`
- `policy_name`
- `condition_name`
- `metric_type`
- `ratio`
- `threshold`
- `dry_run`
- `action`

No-action log examples:

- metrics disabled: reason `metrics_disabled`
- incident closed or non-open: reason `incident_not_open`
- missing project ID: reason `missing_metric_project_id`
- no configured policy: reason `metric_policy_not_configured`

Action path log examples:

- dry-run decision: reason `dry_run`
- real disable attempt: reason `disable_billing`
- disable failure: reason `disable_failed`

Do not log the full incident payload, full documentation blocks, credentials,
raw labels beyond the selected target project, or other free-form notification
content.

## Error Handling

- Invalid Pub/Sub envelope: `400`, no action.
- Invalid base64 message data: `400`, no action.
- Invalid Cloud Monitoring JSON: `400`, no action.
- `metrics.enabled` false: `400`, no action.
- Missing or non-open incident state: `200`, no action,
  `incident_not_open`.
- Missing configured project ID field: `200`, no action,
  `missing_metric_project_id`.
- Unknown metric policy with `requirePolicyMatch: true`: `200`, no action,
  `metric_policy_not_configured`.
- Billing client construction failure: `500`, no action completed.
- Billing disable failure: `500`, decision reason `disable_failed: <error>`.

The endpoint fails closed: missing config, missing project ID, unknown policy,
or malformed payload must never disable billing.

## Testing

Add focused tests for:

- Pub/Sub metrics parser decodes a Cloud Monitoring incident payload.
- Default project ID extraction from
  `incident.resource.labels.project_id`.
- Configured project ID field path override.
- Closed incident returns `200` with no disable call.
- Missing project ID returns `200` with no disable call.
- Unknown policy returns no action when `requirePolicyMatch` is true.
- Configured policy dry-runs by default.
- Configured policy can disable billing when `dryRun: false`.
- Protected project is not disabled.
- Disable failure returns `500` and includes `disable_failed`.
- Invalid Pub/Sub envelope, base64, and metrics JSON return `400`.

Existing `go test ./...` remains the primary verification command.

## Documentation Updates

Update:

- `README.md`: list `POST /pubsub/metrics-alert` and summarize safe defaults.
- `configs/example.yaml`: add a disabled metrics block with comments.
- `docs/using-in-your-google-project.md`: show how to wire Cloud Monitoring
  alert notifications through Pub/Sub to the new endpoint.
- `deployments/terraform/main.tf`: consider a commented example or optional
  subscription variable in a separate Terraform-focused change; do not make
  Terraform changes unless implementation scope explicitly includes it.

## Open Implementation Notes

- Existing enforcement logic is budget-shaped. The implementation should avoid a
  broad refactor unless tests show duplication or type confusion. A small
  shared helper for "apply decisions and disable billing" is acceptable.
- `log/slog` should be introduced in a way that does not force all existing logs
  to change in this slice. Converting the existing billing-disable failure log
  can be included if it keeps the code simpler.
- The field-path extractor only needs dotted map traversal for JSON objects in
  this slice. It does not need full JSONPath support.
