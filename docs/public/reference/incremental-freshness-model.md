# Incremental Freshness Model

Eshu keeps its context graph current by re-observing sources incrementally
rather than rebuilding everything on every run. This page describes how the
incremental machinery works today: how unchanged work is skipped, how webhook
triggers relate to authoritative polling, how a scope's generations move
through their lifecycle, and which surfaces answer freshness questions.

It documents currently implemented behavior only. Eshu exposes a bounded
repository-scope changed-since delta surface (`GET /api/v0/freshness/changed-since`,
the `get_changed_since` MCP tool, and `eshu freshness changed-since`) that diffs
a prior generation's fact set against the current active generation's fact set;
the lower-level freshness signal is still observed through scope generations and
status surfaces. Service-scope deltas are now **partially available**: the
ownership, deployment, runtime, and dependencies families ship through
`GET /api/v0/freshness/services/changed-since`, the `get_service_changed_since`
MCP tool, and `eshu freshness service-changed-since`, backed by a per-service
generation lineage (`service_materialization_generations`) and generation-stable
evidence snapshots (`service_evidence_snapshots`). The remaining service families
(docs, incidents, vulnerabilities) reuse the same lineage and snapshot foundation
and are tracked as follow-up work.

## What incremental refresh means

The normal Eshu path is incremental. A collector re-observes a source, produces
a new **scope generation** (one observed snapshot of one scope), and hands its
facts to projection. When a fresh observation matches what is already active for
that scope, Eshu skips the commit instead of reprojecting identical truth. When
it differs, the new generation is committed, projected, and promoted to active,
and the previous generation is retired into a terminal state.

A scope is the durable identity for a source-local unit of truth (a repository
snapshot, a cloud account, a region, a state snapshot, and so on). Each scope
has at most one **active** generation at a time, tracked by
`ingestion_scopes.active_generation_id`. Reads answer from that active
generation; new observations land as pending generations that are promoted only
after their projection succeeds.

The pipeline a generation flows through is the standard ingestion flow:

```text
sync -> discover -> parse -> emit facts -> enqueue work -> reducer
     -> graph/content projection -> query surface
```

## How freshness hints skip unchanged generations

A scope generation may carry a `freshness_hint`: a short, opaque token a
collector computes to summarize the observed state (for example a commit SHA for
a repository snapshot). The hint is persisted on `scope_generations.freshness_hint`.

When a collector commits a generation, `CommitScopeGeneration`
(`go/internal/storage/postgres/ingestion.go`) first calls
`shouldSkipUnchangedGeneration`. That helper looks up the most recent
`pending` or `active` generation for the scope that has a non-empty
`freshness_hint` and compares it to the incoming hint:

```sql
SELECT generation.generation_id, COALESCE(generation.freshness_hint, '')
FROM scope_generations AS generation
WHERE generation.scope_id = $1
  AND generation.status IN ('pending', 'active')
  AND COALESCE(generation.freshness_hint, '') <> ''
ORDER BY generation.ingested_at DESC, generation.generation_id DESC
LIMIT 1
```

If the trimmed hints match, the commit is skipped: the fact stream is drained,
no transaction opens, and no projector work is enqueued. The skip increments a
process-local counter via `telemetry.RecordSkippedRefresh()` and emits a
structured log line keyed on `refresh_skipped=true` with the scope ID, scope
kind, source system, collector kind, and generation ID. This is what makes a
re-observation of an unchanged source cheap.

The hint is a fast-path optimization, not the authority. An empty hint, or a
missing scope ID, never triggers a skip. The comparison only looks at the latest
pending or active generation with a hint, so a hint never resurrects a terminal
generation.

## How webhook triggers differ from source truth

Webhooks make refresh timely; they never substitute for source observation.

The webhook package (`go/internal/webhook`) verifies provider authentication
(GitHub, GitLab, Bitbucket, PagerDuty, Jira) and normalizes a verified delivery
into a `Trigger` or `IncidentFreshnessTrigger` decision. That decision is
persisted as a durable trigger; the webhook listener runtime then hands a
**targeted refresh** to the normal claim-driven collector path. A webhook is a
wake-up signal only.

Concretely, a webhook trigger does not write graph truth and does not shortcut
snapshotting:

- The collector still fetches source state, creates a scope generation, emits
  facts, and lets projection update graph and content state.
- Merged pull-request number, URL, and title fields on a GitHub trigger are
  provider provenance for read-model enrichment. They do not skip repository
  refresh or create graph truth directly.
- Tag events, non-default-branch events, default-branch deletes, and merge
  events without a provider merge commit are ignored with explicit decision
  reasons.
- PagerDuty and Jira deliveries are scoped refresh triggers only; they do not
  emit incident, change, work-item, pull-request, deployment, image, or code
  facts. They require a configured collector `scope_id`, and the coordinator
  rejects stale or unauthorized scope IDs before creating collector work.

Polling remains the authoritative backfill. If a webhook is missed, delayed, or
filtered, the next scheduled poll re-observes the source and produces the
generation that catches the answer up. Treat webhooks as a latency improvement
on top of polling, not as the source of record.

## How generations behave through their lifecycle

A generation's lifecycle is the `scope.GenerationStatus` enum in
`go/internal/scope/scope.go`. There are five statuses, and the allowed
transitions are enforced by `allowedGenerationTransitions`:

| Status | Meaning | Allowed next |
| --- | --- | --- |
| `pending` | Committed but not yet authoritative. | `active`, `failed` |
| `active` | Currently authoritative for the scope. | `superseded`, `completed`, `failed` |
| `superseded` | Replaced by a newer generation. | terminal |
| `completed` | Finished successfully. | terminal |
| `failed` | Finished unsuccessfully. | terminal |

`superseded`, `completed`, and `failed` are terminal: a terminal generation
cannot transition again. There is no separate "retired" status; a generation
that is no longer active is in one of these three terminal states.

A scope has at most one active generation, named by
`ingestion_scopes.active_generation_id`. Promotion happens at projection
acknowledgement, not at commit. When a projector finishes a generation's work,
`ProjectorQueue.Ack` (`go/internal/storage/postgres/projector_queue.go`) runs
five ordered steps in a single transaction:

1. Supersede the scope's current active generation.
2. Supersede obsolete terminal generations for the scope.
3. Activate the target generation.
4. Update the scope's `active_generation_id` to the target generation.
5. Mark the projector work item succeeded.

Because these run in one transaction, a reader never observes two active
generations for a scope, and supersession of the old generation and activation
of the new one are atomic. If a newer generation arrives while a projector is
still working, the heartbeat path supersedes the in-flight work
(`ErrWorkSuperseded`) so stale projection cannot overwrite newer truth.

A failed first-generation attempt leaves no active generation. Projection uses
`IngestionScope.PreviousGenerationExists` (not the presence of an active
generation) to decide whether prior state needs cleanup, because a failed or
superseded prior generation may leave `active_generation_id` empty.

## How to diagnose stale answers

Query, MCP, and CLI responses carry a truth label. The freshness portion of that
label tells a consumer whether the answer is current and, when it is not, why.
See [Truth Label Protocol](truth-label-protocol.md) for the full envelope.

Freshness state is one of:

- `fresh` — the answer reflects current indexed truth.
- `stale` — the answer was correct at `observed_at` and has a known reason for
  lagging.
- `building` — indexing for the scope is in progress.
- `unavailable` — the capability cannot be answered from current state.

A `stale`, `building`, or `unavailable` answer is not a wrong answer. It reflects
truth that was correct at `freshness.observed_at` and has a named reason for
lagging. Correctness is governed by `level` and `basis`; freshness explains
timing, not validity.

When a handler holds the evidence, it attaches a bounded `cause` and a
`next_check`. The cause enumeration and the cause-to-next-check mapping live in
`go/internal/query/freshness_causality.go`. Causes are wired into handlers
incrementally and a handler that cannot prove a cause leaves it unset. The closed
cause set is:

| Cause | Meaning |
| --- | --- |
| `pending_repo_generation` | A repo's graph generation has not yet completed. |
| `reducer_backlog` | Queued reducer projection has not yet drained. |
| `dead_lettered_domain` | A domain's projection failed and is parked for repair. |
| `missing_collector_completion` | A collector has not reported a completed run for the coverage. |
| `content_coverage_unavailable` | Content coverage is not yet indexed for the scope. |
| `unsupported_profile` | The active profile cannot serve authoritative truth for the capability. |

Each cause carries a `next_check` pointing at a status, generation, coverage, or
queue surface (for example `GET /api/v0/status` or the `get_index_status` MCP
tool) where a consumer can learn when the answer will catch up.

A typical diagnosis path:

1. Read the truth label on the answer. If `freshness.state` is not `fresh`, read
   the `cause` and `next_check`.
2. Follow the `next_check` to a status surface and inspect generation and queue
   state for the affected scope.
3. Confirm whether a generation is `pending` (waiting on projection) or `failed`
   (parked for repair), and whether a domain is dead-lettered.

For local diagnosis, see [Local Testing](local-testing.md) for the gates and
harnesses that exercise these surfaces. For the metrics, spans, and logs behind
freshness and generation progress, see [Telemetry](telemetry/index.md), which
covers the structured `refresh_skipped` log line emitted on skipped generations.

## How changed-since deltas are computed

The changed-since surface answers "what changed in this repository scope since a
prior generation or instant?" without re-indexing. It diffs two generations of
one scope:

- The **prior generation** is the generation named by `since_generation_id`, or
  the generation observed at or before `since_observed_at` for the scope.
- The **current generation** is the scope's current
  `active_generation_id`.

The diff runs over `fact_records`, keyed by `(scope_id, generation_id,
stable_fact_key)`. Each stable fact key falls into exactly one verdict, grouped
into evidence categories (files, content entities, and the remaining facts):

| Verdict | Meaning |
| --- | --- |
| `added` | Key present in the current generation, absent in the prior. |
| `updated` | Key present in both; the payload hash (`md5(payload)`) differs. |
| `unchanged` | Key present in both; the payload hash matches. |
| `retired` | Key active in the prior generation, explicitly tombstoned in the current generation. |
| `superseded` | Key active in the prior generation, absent entirely from the current generation. |

Retired and superseded are never collapsed into `unchanged`. Counts are exact
per category; the per-classification sample handles are bounded by `sample_limit`
(default 25, max 200) and carry a per-classification `truncated` flag. Ordering
is deterministic by `stable_fact_key`.

Resolution failures are explicit, never confident emptiness. An unknown
scope/repository returns `scope_not_found`; a since reference that resolves to no
generation returns `not_found`; a scope with no current active generation returns
an `unavailable` diff (and a `building`/`unavailable` freshness state) rather than
all-zero deltas.

Service-scope deltas are now **partially available**. A service is a
reducer-materialized correlation spread across many source scopes and
generations, not a single ingestion scope with one generation lineage, so the
repository-scope diff above does not apply directly. The fix (issue #1943) is a
versioned per-service materialization snapshot with a generation-independent
per-evidence diff key:

- `service_materialization_generations` is the per-service generation lineage
  (one active generation per `service_id`, enforced by a partial unique index,
  exactly like `scope_generations`). The reducer commits a new generation on each
  service re-materialization; an identical re-materialization is a no-op.
- `service_evidence_snapshots` holds generation-stable evidence rows keyed by a
  generation-independent `service_evidence_key` (for example
  `ownership:<service_id>:<owner_ref>`, `deployment:<service_id>:<identity>`
  (where the deployment identity is a digest of the resolved deployment
  relationship's generation-independent natural key — its `resolved_id` embeds
  the resolution generation and is therefore not a stable diff key), or
  `runtime:<service_id>:<platform_kind>:<environment>:<workload_ref>` (where
  `workload_ref` is the durable `WorkloadInstance` id
  `workload-instance:<workload_name>:<environment>`, which carries no resolution
  or materialization generation id), or `dependencies:<service_id>:<identity>`
  (where the dependency identity is a digest of the resolved dependency
  relationship's generation-independent natural key — `DEPENDS_ON` /
  `USES_MODULE` / `READS_CONFIG_FROM`, the complement of the deployment family
  from the same `resolved_relationships` source — and, like deployment, its
  `resolved_id` embeds the resolution generation and is therefore not a stable
  diff key), with a
  `payload_hash` so updated-vs-unchanged is detected the same way the
  repository-scope diff uses `md5(payload::text)`, and an `is_tombstone` flag so a
  dropped evidence row is retired explicitly rather than silently absent. The
  rows carry an `evidence_family` column, so the delta groups by family and a new
  family appears once its rows are written without a delta-SQL change.

`GET /api/v0/freshness/services/changed-since` (the `get_service_changed_since`
MCP tool and `eshu freshness service-changed-since`) diffs a prior service
generation against the current active generation over these snapshot rows, using
the same FULL OUTER JOIN classification (added/updated/unchanged/retired/
superseded), bounded `sample_limit`, deterministic ordering, and `unavailable`
handling as the repository-scope surface. An unknown `service_id` returns
`service_not_found`; an unresolved `since_generation_id` returns `not_found`; a
service with no current active generation returns an explicit `unavailable` diff
rather than zero deltas.

The **ownership** (#1943), **deployment** (#1985), **runtime** (#1986), and
**dependencies** (#1987) families ship. The remaining families (docs, incidents,
vulnerabilities) reuse this lineage and snapshot foundation and are tracked
follow-ups. The
investigation, the reason each evidence family needed this foundation, and the
recommended snapshot contract are recorded in the internal design note for issue
#1943.

## How hosted teams verify freshness without full re-index churn

Hosted teams do not need to re-index a repository to confirm it is current. The
incremental path already avoids churn, and the status surfaces report freshness
directly:

- Webhook-driven targeted refresh keeps active generations current as sources
  change, while scheduled polling backfills anything a webhook missed.
- Unchanged re-observations are skipped by the freshness-hint fast path, so a
  poll over an unchanged source does no projection work.
- To verify a scope is current, read its generation status from a status surface
  rather than forcing a re-index. A scope whose latest generation is `active`
  (or `completed`) with no `pending` successor and no `failed` generation is
  current. A `pending` generation indicates projection is still catching up.

The CLI scan-readiness path applies the same logic: a `failed` generation in the
generation history is treated as terminal, and a `pending` generation reports
that generations are still catching up
(`go/cmd/eshu/scan_status.go`).

For hosted runtime layout (which service owns each surface), see
[Service Runtimes — Core Services](../deployment/service-runtimes-core.md) and
[Runtime Admin API](runtime-admin-api.md).

## Which surfaces answer each freshness question

All routes are namespaced under the HTTP API. The pipeline status report is
rendered from Postgres, not from the graph backend.

| Question | Surface | Notes |
| --- | --- | --- |
| Is indexing healthy and how many repos are indexed? | `GET /api/v0/index-status` (alias `GET /api/v0/status/index`) | Returns `status`, `reasons`, `repository_count`, `queue`, `coordinator`, and `scope_activity`. |
| What is the full pipeline state, including generation lifecycle? | `GET /api/v0/status/pipeline` | Adds `generation_history` and `generation_transitions` to `scope_activity`, queue, and coordinator state. |
| What does the admin status report show end to end? | `GET /admin/status` | Includes `scope_activity`, `generation_history`, `generation_transitions`, `scopes`, and `generations`. See [Runtime Admin API](runtime-admin-api.md). |
| Why is this specific answer not fresh? | Truth label `freshness.cause` and `freshness.next_check` on the answer envelope | The `next_check` points at the status, generation, coverage, or queue surface to follow. See [Truth Label Protocol](truth-label-protocol.md). |
| Which MCP tool reports index progress? | `get_index_status` (the `next_check` target for most causes) | Bounded follow-up call carried on freshness causes. |
| Is a scope current from the CLI? | `eshu` scan-status readiness | Treats `failed` generations as terminal and reports `pending` generations as still catching up. |
| What changed in a repository scope since a prior generation or instant? | `GET /api/v0/freshness/changed-since` (`get_changed_since` MCP tool, `eshu freshness changed-since`) | Diffs the prior generation's fact set against the current active generation's fact set by `stable_fact_key`. Returns per-category (files, content entities, facts) added/updated/unchanged/retired/superseded counts with bounded sample handles. A scope with no current active generation returns an explicit unavailable diff, never zero deltas. |
| What changed for a service since a prior service generation? | `GET /api/v0/freshness/services/changed-since` (`get_service_changed_since` MCP tool, `eshu freshness service-changed-since`) | Diffs a prior service materialization generation against the current active generation over `service_evidence_snapshots`, keyed by generation-independent `service_evidence_key`. Reports the ownership (#1943), deployment (#1985), runtime (#1986), and dependencies (#1987) families; per-family added/updated/unchanged/retired/superseded counts with bounded sample handles. Unknown `service_id` returns `service_not_found`; no current active generation returns an explicit unavailable diff, never zero deltas. |

`scope_activity` summarizes per-scope observation activity. `generation_history`
summarizes generation counts by status (including pending and failed).
`generation_transitions` records recent status transitions. Use the pipeline or
admin report when you need generation lifecycle detail; use `index-status` for a
fast health-and-coverage check.

## Related references

- [Service Runtimes — Core Services](../deployment/service-runtimes-core.md)
- [Runtime Admin API](runtime-admin-api.md)
- [Truth Label Protocol](truth-label-protocol.md)
- [Telemetry](telemetry/index.md)
- [Local Testing](local-testing.md)
