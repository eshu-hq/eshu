# ADR: Webhook-Triggered Repository Refresh Runtime

**Date:** 2026-05-12
**Status:** Accepted
**Authors:** Allen Sanabria
**Deciders:** Platform Engineering
**Related:**

- Issue: #211
- `2026-04-19-multi-source-correlation-dsl-and-collector-readiness.md`
- `2026-04-20-workflow-coordinator-and-multi-collector-runtime-contract.md`
- `2026-04-20-workflow-coordinator-claiming-fencing-and-convergence.md`
- `docs/docs/deployment/service-runtimes.md`
- `docs/docs/reference/telemetry/index.md`

---

## Context

Hosted Eshu deployments need fresher repository truth than the current timed
repository sync can provide.

The current Git path is already incremental in the important correctness
sense. The ingester polls, discovers repositories, syncs Git checkouts, selects
only repositories whose local default-branch checkout changed, snapshots the
changed repository, and commits a new repository generation. If the generated
freshness hint matches the latest pending or active generation, the Postgres
commit boundary skips the refresh.

That means the current problem is not "Eshu re-indexes every repository on
every cycle." The problem is that the change detector is still timer-driven.
After a pull request or merge request lands, graph and content truth can stay
stale until the next polling cycle reaches that repository.

Webhook providers already emit event streams that are closer to the freshness
contract Eshu needs:

- GitHub sends `push` and `pull_request` webhook events and signs payloads
  with `X-Hub-Signature-256` when a secret is configured.
- GitLab sends push and merge request webhook events; push webhooks identify
  branch refs, and merge request webhooks identify merge actions.
- Bitbucket Cloud sends `repo:push` and `pullrequest:fulfilled` webhook
  events; signed hooks use `X-Hub-Signature` with HMAC-SHA256.

Eshu should use those events as a wake-up signal. It must not use them as
canonical source truth.

## Problem Statement

Eshu needs a public EKS-facing service that can receive GitHub, GitLab, and
Bitbucket webhooks, validate them, and turn default-branch changes into
targeted repository refresh work without weakening graph truth.

The service must answer these questions safely:

1. Is the request from a configured provider and hook secret?
2. Did it describe a default-branch update for a repository Eshu is allowed to
   index?
3. Has this provider delivery or target commit already been accepted?
4. Which repository refresh should run, and why?
5. Did the downstream Git generation path actually observe the new repository
   state?
6. Can operators see accepted, ignored, duplicate, rejected, queued, running,
   failed, and stale trigger states?

It must not answer graph or deployment questions directly from webhook
metadata.

## Decision

### Add A Separate Public Webhook Listener Runtime

Introduce a new long-running runtime named `webhook-listener`, packaged as
`/usr/local/bin/eshu-webhook-listener`.

The runtime is a separate Kubernetes `Deployment` in the Helm chart. It is
publicly reachable through an explicitly configured ingress or gateway route on
EKS so GitHub, GitLab, and Bitbucket can deliver webhooks. It does not mount the
repository workspace PVC and it does not connect to the graph backend.

The listener owns:

- provider HTTP endpoints
- provider signature and shared-secret verification
- payload size limits and request deadlines
- event normalization
- default-branch filtering
- duplicate delivery suppression
- durable trigger insertion
- trigger intake telemetry and status

The listener does not own:

- repository cloning or fetching
- snapshot collection
- parsing
- fact emission
- projector or reducer queues
- graph writes
- API or MCP answer shaping

### Keep The Existing Git Generation Path Authoritative

Webhook events are trigger evidence only. A webhook-triggered refresh must still
flow through the normal facts-first path:

```text
webhook-listener
  -> durable refresh trigger
  -> workflow coordinator / Git collector claim
  -> git sync of the target repository default branch
  -> repository snapshot
  -> fact emission
  -> projector and reducer queues
  -> graph/content projection
  -> API and MCP query truth
```

The default-branch commit observed by Git after fetch is the freshness truth.
The webhook payload's `after`, `merge_commit_sha`, or equivalent target SHA is
evidence for prioritization, dedupe, and operator diagnosis. It does not
replace the fetched repository state.

### Prefer Default-Branch Push Events As The Commit Boundary

GitHub pull request, GitLab merge request, and Bitbucket fulfilled pull request
events are useful context, but default-branch push events are the stronger
cross-provider commit boundary.
They cover:

- merge commits
- squash merges
- rebase merges
- merge queues
- direct pushes
- provider-specific auto-merge behavior

The listener accepts provider merge events only when they target the configured
default branch and carry enough repository identity to enqueue a safe refresh.
The downstream Git sync still verifies whether the default-branch checkout
advanced.

### Use A Durable Trigger Contract

Webhook intake must persist a normalized trigger before returning success to
the provider.

The trigger identity is:

```text
(provider, provider_delivery_id, repository_external_id)
```

The refresh identity is:

```text
(provider, repository_external_id, default_branch, target_sha)
```

The trigger payload should preserve enough provenance for auditing without
storing full webhook bodies by default:

- provider
- event kind
- delivery ID
- installation or project ID where available
- repository full name or path
- repository external ID where available
- default branch
- ref
- before SHA
- after or target SHA
- sender/login where safe
- received timestamp
- decision: accepted, ignored, duplicate, rejected
- reason

Full raw payload retention is a deployment option only, disabled by default.
If enabled, it must be bounded by size and retention and must pass the shared
redaction posture before production use.

### Handoff Through The Coordinator Contract

The long-term owner of trigger-to-work orchestration is the workflow
coordinator. The listener should write triggers into the coordinator's durable
control plane, and the coordinator should turn accepted triggers into bounded
Git collector claims.

The Git collector remains the source owner. It receives a targeted repository
claim, fetches that repository, computes the snapshot freshness hint, and emits
facts exactly as scheduled refreshes do.

Until production claim ownership is enabled, an implementation may use a narrow
compatibility handoff from the trigger store to the existing ingester selector.
That compatibility path must be explicitly temporary and must preserve the same
dedupe, freshness, and status fields so it can be removed without changing the
public contract.

### EKS Exposure And Security Model

The runtime is intentionally public, but only the webhook endpoints are public.

Required deployment posture:

- public ingress/gateway routes only to provider webhook paths
- internal-only access for `/healthz`, `/readyz`, `/metrics`, and
  `/admin/status` unless an operator explicitly exposes them
- provider secrets loaded from Kubernetes `Secret` objects
- no static Git credentials in the listener pod
- no graph credentials in the listener pod
- no repository workspace volume mounted
- request body size cap
- request read timeout
- provider-specific signature failure counters
- optional ingress-controller IP allowlists where the operator can maintain
  them
- TLS terminated at the ingress/gateway layer or stronger

GitHub verification must use `X-Hub-Signature-256` with HMAC-SHA256. The
legacy SHA-1 signature is not an acceptance path.

GitLab verification must use the configured GitLab webhook token or equivalent
secret header supported by the deployed GitLab flavor. Missing or mismatched
secrets reject the request before payload normalization.

Bitbucket Cloud verification must use `X-Hub-Signature` with HMAC-SHA256 when a
webhook secret is configured. SHA-1 is not an acceptance path for Bitbucket.

### Idempotency And Ordering

The listener and coordinator must handle normal provider delivery behavior:

- duplicate deliveries
- retries after 5xx or timeout
- out-of-order merge and push events
- direct push without a merge event
- merge event without a later push delivery
- branch delete and tag push noise
- fork-origin pull requests
- repository rename
- unsupported repository

Duplicate deliveries must not enqueue duplicate refresh claims. An older target
SHA must not supersede a newer active generation; the Git collector's fetched
default-branch state remains the final arbiter.

### Observability

The runtime mounts the shared Go admin contract:

- `/healthz`
- `/readyz`
- `/admin/status`
- `/metrics`

Required status fields:

- configured providers
- accepted trigger count
- ignored trigger count by reason
- rejected trigger count by reason
- duplicate trigger count
- queued trigger count
- claim handoff count
- last accepted trigger timestamp
- last handoff timestamp
- oldest queued trigger age
- failed trigger count
- dead-letter trigger count if a durable retry path exists

Required telemetry:

- request count by provider, event kind, and outcome
- signature verification failures by provider
- trigger normalization duration
- trigger persistence duration
- handoff duration
- queued trigger age
- trigger retry count
- structured logs with delivery ID, repository identity, branch, target SHA,
  decision, and reason

Metric labels must avoid high-cardinality raw repository names, delivery IDs,
branch names, and SHAs. Those belong in logs and spans.

## Truth Contract

Webhook metadata is not graph truth.

The listener may say:

- a provider delivered an event
- the event passed authentication
- the event appears to target a default branch
- the event requested a repository refresh
- a trigger was accepted, ignored, duplicated, rejected, or handed off

The listener must not say:

- which services changed
- which workloads changed
- which deployment relationships changed
- which environments changed
- that a target SHA is now canonical graph truth

Those claims become true only after the Git collector emits a new generation and
the projector/reducer finish the normal graph/content projection path.

## Rejected Options

### Keep Only Timed Sync

Rejected. Timed sync is a useful safety net, but it is not the right primary
freshness contract for shared deployments. It leaves query truth stale for the
entire poll interval and wastes work checking repositories that did not change.

### Put Public Webhook Endpoints On The API Service

Rejected. The API owns read/query traffic. Webhook intake is write-side control
plane traffic with different authentication, request-shape, rate-limit, and
blast-radius requirements. Mixing the two would make the public API harder to
secure and operate.

### Put Public Webhook Endpoints On The Ingester

Rejected. The ingester owns workspace-mounted Git sync and fact emission. It
should not also be the public internet-facing webhook process. Keeping the
listener separate lets EKS expose the smallest possible public workload and
keeps the workspace PVC off the public surface.

### Parse Webhook Payloads Into Graph Facts Directly

Rejected. Provider events do not contain enough source truth to update code,
deployment, or service graph state. Direct graph writes from webhook metadata
would create false freshness and bypass reducer correctness gates.

### Treat Merge Events As Sufficient Without Fetching Git

Rejected. Merge events vary by provider and merge method. The fetched default
branch is the durable source state Eshu can parse, hash, and project.

## Rollout Plan

### Chunk 1: ADR And Contract

- Accept this ADR.
- Confirm the runtime name and EKS exposure boundary.
- Confirm whether the first implementation writes to a new trigger table or an
  extension of the workflow-control store.

### Chunk 2: Normalizer And Verification Package

- Add a Go package for provider verification and event normalization.
- Cover GitHub push, GitHub pull request closed/merged, GitLab push, GitLab
  merge request merged, Bitbucket push, and Bitbucket fulfilled pull request
  cases.
- Cover negative and ambiguous cases: invalid signature, non-default branch,
  tag push, closed-unmerged PR/MR, missing repo identity, duplicate delivery,
  stale older target SHA, oversized payload.

### Chunk 3: Durable Trigger Store

- Add idempotent trigger persistence.
- Add claim/handoff state transitions.
- Add status reader support.
- Add migration and downgrade-safe schema notes if a new table is required.

### Chunk 4: Runtime Binary

- Add `go/cmd/webhook-listener`.
- Mount provider routes and shared admin/status endpoints.
- Wire request limits, timeouts, structured logs, metrics, and graceful
  shutdown.

### Chunk 5: Coordinator / Git Collector Handoff

- Convert accepted triggers into targeted Git collector claims.
- Preserve scheduled polling as a backstop.
- Prove that one webhook-triggered repository refresh does not select unrelated
  repositories.

### Chunk 6: Helm And EKS Exposure

- Add Helm values and templates for the listener `Deployment`, internal
  service, optional public ingress/gateway, network policy, service monitor,
  and secrets references.
- Keep provider webhook routes public and admin/metrics routes internal by
  default.

### Chunk 7: End-To-End Proof

- Run a local or Compose proof with synthetic webhook payloads.
- Show accepted trigger -> targeted Git refresh -> new generation -> projector
  completion -> API/MCP truth on the new active generation.
- Show unrelated repositories remain unselected.

## Acceptance Gates

- Focused Go tests for signature verification, normalization, filtering,
  idempotency, stale-event handling, and handoff.
- Storage tests for trigger persistence and duplicate suppression.
- Runtime tests for request limits, provider routes, admin readiness, and
  graceful shutdown.
- Helm render/lint proof for enabled and disabled listener modes.
- Docs build and `git diff --check`.
- At least one integration proof showing a default-branch webhook refreshes one
  changed repository while unrelated repositories are not selected.

## Open Questions

1. Does the first trigger store extend existing workflow-control tables, or do
   we add a dedicated `webhook_refresh_triggers` table?
2. Should the listener support GitHub organization webhooks first, repository
   webhooks first, or both in the same slice?
3. Should GitLab group webhooks be accepted in the first slice, or only project
   webhooks?
4. What is the default public route shape: `/webhooks/github`,
   `/webhooks/gitlab`, and `/webhooks/bitbucket`, or provider-versioned paths?
5. Should missed webhook recovery stay as low-frequency scheduled polling, or
   should the coordinator also run periodic provider audit checks?

## Status Review

**Current disposition:** Accepted. Implementation is in progress on issue #211
and PR #213.

Issue #211 tracks implementation. The branch now contains the first complete
runtime slice:

- `go/internal/webhook` verifies provider authentication inputs and normalizes
  GitHub push, GitHub pull request merge, GitLab push, GitLab merge request
  merge, Bitbucket push, and Bitbucket fulfilled pull request events, including
  ignored outcomes for tag noise, non-default branches, malformed JSON,
  unsupported events, missing secrets, and invalid signatures.
- `webhook_refresh_triggers` persists normalized trigger decisions in Postgres
  with durable delivery and refresh keys, queued/ignored/claimed/handed-off
  status transitions, and `FOR UPDATE SKIP LOCKED` claim behavior.
- `go/cmd/webhook-listener` hosts GitHub, GitLab, and Bitbucket routes on the
  shared runtime mux, uses request body limits, writes trigger decisions before
  returning provider success, and keeps graph and workspace access out of the
  process.
- The webhook listener emits bounded OTEL request, decision, store-operation,
  request-duration, and store-duration telemetry plus `webhook.handle` and
  `webhook.store` spans.
- The ingester and local `collector-git` compatibility handoff can prioritize
  queued accepted triggers, sync only the referenced repositories, then fall
  back to scheduled polling as the safety net.
- The Helm chart renders a separate disabled-by-default `webhookListener`
  `Deployment`, `Service`, optional provider-path ingress, `ServiceMonitor`,
  `NetworkPolicy`, `PodDisruptionBudget`, and provider secret validation.

The remaining production acceptance gap is an end-to-end Compose or cluster
proof that follows one accepted provider delivery through targeted Git refresh,
new generation persistence, projector completion, and API/MCP truth on the new
active generation.
