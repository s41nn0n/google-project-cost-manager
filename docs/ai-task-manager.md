# AI Task Manager Operating Guide

This repository uses AI Task Manager at `https://tasks.myw.io` as the delivery
coordination system for the `google-project-cost-manager` workspace.

AI Task Manager is used to record work items, hierarchy, comments, handoffs,
work packages, evidence references, and status. It is not an agent runtime.
Do not add work items, docs, or APIs that treat it as a system for starting,
stopping, scheduling, supervising, cancelling, or streaming agent execution.
External executors own execution and report facts back as comments, evidence,
and handoff events.

## Workspace

- Workspace ID: `google-project-cost-manager`
- Product work item: `task_8fd398fa4c33ab6c17302ae27b2688d1`
- Project work item: `task_5d3d19ac7c4c5864a5f62c376c495e38`
- Source guide reviewed: `https://tasks.myw.io/project-llm.txt`
- Repository path used during setup:
  `/media/new/Code/mywio/google-project-cost-manager`

## Product And Project

The root product is `GCP Project Cost Manager / Billing Guard`. It represents
the long-lived product surface in this repository: a Go Cloud Run service that
receives Cloud Billing Budget Pub/Sub alerts, reconciles GCP budget setup,
supports setup validation/self-tests, and can dry-run or disable billing
according to explicit policy.

The initial project is `Adopt AI Task Manager for repository delivery`. It is a
bounded project for documenting and operating this repository through AI Task
Manager. Its scope is the workspace taxonomy, operating guide, initial
hierarchy, evidence expectations, and handoff conventions. It does not change
the Go service runtime.

## Work Item Type Policy

The workspace is configured with the following type rules.

| Type | Root capable | Parent required | Allowed parents |
| --- | --- | --- | --- |
| `product` | yes | no | none |
| `roadmap` | yes | no | none |
| `release` | yes | no | `product`, `project`, `roadmap`, `epic` |
| `decision` | yes | no | `project`, `story`, `task`, `bug`, `decision` |
| `project` | no | yes | `product` |
| `epic` | no | yes | `product`, `project` |
| `story` | no | yes | `product`, `project`, `epic` |
| `task` | no | yes | `product`, `project`, `epic`, `story`, `bug`, `incident`, `task` |
| `milestone` | no | yes | `project`, `roadmap`, `epic` |
| `architecture` | no | yes | `project`, `epic`, `story`, `task` |
| `backend_change` | no | yes | `project`, `epic`, `story`, `task`, `architecture` |
| `frontend_change` | no | yes | `project`, `epic`, `story`, `task`, `ux_design` |
| `ux_design` | no | yes | `project`, `epic`, `story` |
| `migration` | no | yes | `project`, `epic`, `story`, `task`, `release` |
| `deployment` | no | yes | `project`, `epic`, `story`, `task`, `release`, `migration` |
| `documentation` | no | yes | `project`, `epic`, `story`, `task`, `architecture`, `migration`, `deployment`, `release`, `incident` |
| `implementation_review` | no | yes | `project`, `epic`, `story`, `task`, `backend_change`, `frontend_change`, `migration` |
| `security_review` | no | yes | `project`, `epic`, `story`, `task`, `backend_change`, `frontend_change`, `migration`, `architecture` |
| `test_plan` | no | yes | `project`, `epic`, `story`, `task`, `bug`, `migration`, `release` |
| `bug` | no | yes | `product`, `project`, `epic`, `story` |
| `incident` | no | yes | `product`, `project`, `epic` |
| `memory_curation` | no | yes | `project`, `epic`, `story`, `task` |

No custom required metadata keys are enforced. Persona-aligned execution and
review types use default metadata with `evidence_required` set to `"true"`.

Known issue: the `documentation` type is expected for this workspace, but a
follow-up check of `GET /v1/work-item-types` did not return `documentation`.
During setup, creating a work item with `work_item_type="documentation"` also
returned `invalid_task`. Until the workspace/API type configuration is fixed,
track documentation work as `task` with `metadata.record_type` set to
`documentation`. The follow-up bug is
`task_bc67feb381b9174b52cd44f0e7937fd6`.

Recommended persona metadata is configured for:

- `architecture`: Backend/API Architect
- `backend_change`: Backend Engineer
- `frontend_change`: Frontend Engineer
- `ux_design`: UX Workflow Designer
- `migration`: Database Migration Steward
- `deployment`: Deployment Release Steward
- `documentation`: Technical Documenter
- `implementation_review`: Implementation Reviewer
- `security_review`: Security RBAC Reviewer
- `test_plan`: QA Test Strategy Reviewer
- `incident`: Incident Commander
- `memory_curation`: Memory Curator

## Recommended Hierarchy

Use this structure for normal delivery work:

```text
product
  project
    epic
      story
        task
        backend_change
        frontend_change
        architecture
        ux_design
        test_plan
        implementation_review
        security_review
        documentation
    milestone
    release
    incident
    bug
roadmap
release
decision
```

Keep each work item small enough that another human or agent can understand the
objective, context, evidence, and current status in a few minutes.

## Operating Loop

Before changing files or state:

1. Read the relevant parent work item, subtree, comments, and work packages.
2. Add a comment with role, scope, intended files/docs, and expected evidence.
3. Split large outcomes into bounded child work items.
4. Tie implementation to acceptance criteria and validation commands.
5. Use separate implementation and test-quality review work for important
   changes.
6. Complete work only with durable evidence, such as a commit, artifact,
   validation log, deployment/smoke result, or documentation path.

Comments should be factual and concise. Include exact file paths, command names,
commit hashes, and evidence IDs when relevant. Do not put secrets, private keys,
credentials, or raw customer data in comments, metadata, evidence URIs, logs, or
errors.

## Repository-Specific Guardrails

- GCP Cloud Billing Budgets remain the source of truth for budget existence,
  scope, thresholds, and Pub/Sub alert wiring.
- This repository's config remains the source of truth for enforcement policy:
  dry-run behavior, protected projects, thresholds used for enforcement
  decisions, action overrides, unknown alert behavior, self-test settings, and
  safety limits.
- Reconciliation and setup validation are read-only by default.
- Deployment documentation must preserve the warning that this service can
  disable billing on Google Cloud projects.
- Start enforcement work with `dryRun: true` and use dedicated test projects
  before enabling billing-disabling behavior.
- Do not grant unauthenticated Cloud Run invoker access for production-shaped
  deployments.
