# Retention Semantics For Generations, Facts, And Content

Issue: #2248
Parent: #2242

Status: proposed implementation gate. This record defines the contract that
automated cleanup, relationship retraction, orphan sweep, and sync-generation
dead-letter work must preserve before those implementation issues start.

Source check date: 2026-06-12.

## Purpose

Eshu already has source-local retirement semantics: active reads join through
`ingestion_scopes.active_generation_id`, tombstone-aware readers filter
`is_tombstone = FALSE`, and reducer domains retract their prior graph edges
before rewriting current graph truth. Those mechanisms stop stale evidence from
answering current queries, but they do not prune durable history. Superseded
`scope_generations`, `fact_records`, work items, and some content/read-model
rows can grow without bound.

This ADR decides when history is retained, when it becomes eligible for bounded
cleanup, which rows may cascade, and what operators must be able to observe.

## Decision Summary

- Retention is evaluated per ingestion scope. A global default policy seeds
  scopes that have no explicit policy, but the effective cleanup decision is
  per `scope_id`, `scope_kind`, `source_system`, and `collector_kind`.
- The default retained history is the active generation plus superseded
  generations that are either among the last 24 superseded generations for the
  scope or superseded less than 7 days ago. A superseded generation is eligible
  only when it is outside both bounds.
- Pending, active, running, claimed, retrying, failed, and first-generation
  states are not retention candidates. Failed current generations belong to
  recovery or dead-letter workflow, not automated history cleanup.
- Changed-since remains exact inside the retained window. A request whose
  `since_generation_id` or `since_observed_at` resolves only to pruned history
  must return an explicit unavailable/not-found state, never a zero-delta answer.
- Generation cleanup is not a graph retraction mechanism. Reducer graph truth
  must already agree with active facts before historical rows are pruned.
- Content rows are not blindly cascaded by generation id because
  `content_files`, `content_entities`, and `content_file_references` are keyed
  by current repository/path/entity identity, not by generation. They may be
  pruned only after active and retained fact evidence no longer references the
  same repo-local identity.
- Cleanup runs in bounded batches under row locks and must be idempotent under
  retry, duplicate execution, worker crash, and concurrent generation promotion.

## Current Contract

### Source-local truth

The current generation lineage is stored in:

- `ingestion_scopes.active_generation_id`;
- `scope_generations.status`, `activated_at`, and `superseded_at`;
- generation-scoped `fact_records`;
- generation-scoped `fact_work_items` and replay/backfill audit rows.

The storage proof matrix documents the existing no-delete retirement contract:
active source-local reads join the active generation pointer and current
generation status, while tombstone-aware readers also filter tombstones. This
ADR does not change that query contract.

### Changed-since

Repository changed-since compares one prior generation against the current
active generation for the same scope. It classifies stable fact keys as added,
updated, unchanged, retired, or superseded. That diff is meaningful only while
both compared generations remain queryable.

Retention therefore defines a support window, not a new diff algorithm.
Changed-since inside the retained window must keep the same exact-count and
bounded-sample behavior. Changed-since outside the retained window must fail
closed with an explicit reason such as `retention_expired`, not by treating the
missing prior generation as an empty snapshot.

### Graph and read-model truth

Graph projections are reducer-owned derived state. Deleting historical
`fact_records` does not prove graph truth changed. Relationship retraction and
orphan cleanup remain separate work:

- #2250 must retract edges when the active evidence set disappears.
- #2251 must sweep eligible orphan nodes and expose orphan-count telemetry.

Generation retention may enqueue repair or status checks when it detects stale
derived state, but it must not delete facts and hope that graph truth catches up.

## Effective Policy

Each source scope has an effective retention policy:

| Field | Meaning | Default |
| --- | --- | --- |
| `min_superseded_generations` | Minimum superseded generations to keep after the active one. | `24` |
| `max_superseded_age` | Age since `superseded_at` below which a superseded generation is still retained. | `168h` |
| `batch_generation_limit` | Maximum candidate generations deleted in one transaction. | `100` |
| `batch_row_limit` | Maximum estimated dependent rows deleted in one transaction. | implementation-defined conservative cap |
| `policy_scope` | Policy source: global default, source-system override, collector-kind override, or exact scope override. | global default |

A generation is eligible only when all of these are true:

1. `scope_generations.status = 'superseded'`.
2. It is not `ingestion_scopes.active_generation_id`.
3. Its `superseded_at` is present.
4. It is older than `max_superseded_age`.
5. Its descending superseded rank for the scope is greater than
   `min_superseded_generations`.
6. It has no claimed, running, or retrying work that could still write side
   effects.
7. The generation is not referenced by an unexpired explicit operator hold.

The policy is intentionally per-scope because repository, hosted collector,
documentation source, cloud account, and scanner scopes have different cadence,
data classes, and legal/audit posture. The global default exists for simple
installations and for deterministic fallback when no scope-specific policy is
configured.

## Cascade Order

Cleanup must run in a single bounded transaction per candidate batch and must
record safe audit/status facts before removing rows. The order is:

1. Select candidates by effective policy and lock the owning
   `ingestion_scopes` rows plus candidate `scope_generations` rows.
2. Re-read `active_generation_id`, candidate status, `superseded_at`, and
   work-item state under the lock. Abort the candidate if any value changed.
3. Record a retention event with only safe fields: scope class,
   `scope_id_hash`, `generation_id_hash`, policy scope, policy revision/hash,
   row counts, reason, and timestamp. Audit and status rows must not store raw
   scope ids or raw generation ids because those identifiers can include
   source-shaped details in some collectors. Do not include source names, paths,
   payload excerpts, private URLs, credentials, or raw provider identifiers.
4. Delete or let foreign keys cascade generation-owned rows, including
   `fact_records`, `fact_work_items`, fact replay events, graph projection
   phase state, shared projection acceptance rows, and other rows whose schema
   references the candidate `scope_generations.generation_id`.
5. Prune current-content rows only when a repo-local content identity is absent
   from active and retained non-tombstone facts for the same repository. Content
   pruning is identity-based, not generation-cascade-based.
6. Delete the candidate `scope_generations` rows last.
7. Commit metrics/status counters for deleted generations, rows by table/data
   class, duration, oldest eligible age, and retained-window floor.

If any step fails, the transaction rolls back. A retry must converge on the same
remaining candidate set without duplicate audit rows or missed rows.

## Concurrency Contract

Retention is a shared-write workflow. Implementations must preserve useful
projection and reducer concurrency while preventing unsafe overlap with
generation promotion.

Required coordination:

- Candidate selection uses `FOR UPDATE SKIP LOCKED` or an equivalent row-locking
  strategy over the owning scope and candidate generations.
- The conflict domain is `retention:<scope_id>`. Two cleanup workers must not
  prune the same scope concurrently.
- Projection ack/promotion remains the owner of `active_generation_id`.
  Retention only prunes rows that are already superseded and revalidated under
  lock.
- Transaction scope is one bounded candidate batch. Retry scope is the whole
  batch. Idempotency keys are `(scope_id, generation_id, policy_revision)`.
- Claimed/running/retrying work prevents deletion. Superseded terminal work may
  be pruned only with the generation that owns it and only outside the retained
  window.
- Serialization by reducing projector or reducer worker counts is not a fix.

Bad interleaving to prevent:

```text
projector ack promotes generation G2
retention worker selected old candidates before G2 was active
retention worker deletes G2 or its facts because rank/age was computed stale
changed-since and active reads lose the current generation
```

The locked re-read of `active_generation_id`, status, and superseded rank must
make this interleaving impossible.

## Observability Requirements

Implementation PRs must add or identify operator signals that answer:

- how many generations are eligible for cleanup by scope class, source system,
  collector kind, and authorized safe scope hash drilldown;
- oldest eligible age;
- rows pruned by table or data class;
- cleanup duration and batch size;
- cleanup failures by reason;
- cleanup skipped because of active/running/retrying work;
- changed-since requests that fail because the prior generation expired;
- graph repair or retraction backlog, when cleanup discovers derived-state
  disagreement.

Metric labels must remain bounded. Raw scope ids, raw generation ids,
repository paths, source names, file paths, private URLs, provider resource
identifiers, and payload details must not appear in audit rows, status rows, or
metric labels. Raw ids may appear only in redacted operator logs or spans under
the existing access controls; shared readbacks use safe hashes.

## Proof Matrix For #2249

The cleanup implementation must add failing tests first and then prove:

| Scenario | Required proof |
| --- | --- |
| Retained window | Active generation and all superseded generations inside the count/age window remain. |
| Eligible history | Superseded generations outside both bounds are deleted with dependent generation-owned rows. |
| Changed-since compatibility | Diffs work for retained prior generations and fail explicitly for expired prior generations. |
| Active safety | Active, pending, failed-current, claimed, running, and retrying generations are never deleted. |
| Content safety | Content rows referenced by active or retained facts survive; unreferenced stale rows are pruned only by repo-local identity. |
| Retry/idempotency | Re-running the same cleanup after partial failure converges without duplicate audit/status rows. |
| Concurrent promotion | A retention worker racing with projector ack cannot delete the new active generation or its facts. |
| Batch bounds | Batch generation and row limits are honored; cleanup resumes from the next eligible batch. |
| Observability | Metrics/status/logs expose counts, duration, oldest age, skips, and failures without sensitive labels. |

Performance Evidence required for #2249: run the cleanup against a large fixture
dataset with before/after row counts, candidate generations, retained-window
floor, wall time, and Postgres query timing. Stop and profile if cleanup slows
the active projection or changed-since path by more than 10 percent or 60
seconds on the same input shape.

No-Regression Evidence required for #2249: active repository reads, changed-since
inside the retained window, generation lifecycle status, and reducer queue
readiness must remain correct after pruning.

## Non-Goals

- This ADR does not implement cleanup code.
- This ADR does not define privacy deletion, tenant offboarding, or backup
  retention. Those remain governed by the hosted retention and deletion policy.
- This ADR does not retract relationship edges. That is #2250.
- This ADR does not delete graph orphan nodes. That is #2251.
- This ADR does not implement sync collector dead-letter storage or replay. That
  is #2252.
- This ADR does not change active-generation read semantics.

## Evidence For This ADR

No-Regression Evidence: this design record changes no Go code, schema DDL,
queue claim behavior, graph write, query handler, runtime setting, or Helm
profile. The implementation proof requirements above preserve the existing
active-generation and changed-since contracts.

No-Observability-Change: this ADR emits no metrics, spans, logs, status rows,
facts, graph writes, API responses, MCP payloads, audit events, or deletion
jobs. It records the observability requirements for the follow-up
implementation issues.

Sources used:

- `AGENTS.md`
- `docs/internal/agent-guide.md`
- `docs/public/architecture.md`
- `docs/public/deployment/service-runtimes.md`
- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
- `docs/public/reference/hosted-retention-deletion-policy.md`
- `docs/internal/design/1943-service-scope-changed-since-deltas.md`
- `go/internal/storage/postgres/schema.go`
- `go/internal/storage/postgres/schema_fact_records.go`
- `go/internal/storage/postgres/changed_since.go`
- `go/internal/storage/postgres/changed_since_sql.go`
- `go/internal/storage/postgres/generation_lifecycle.go`
- `go/internal/storage/postgres/reducer_generation_filter_sql.go`
- `go/internal/storage/postgres/retirement-proof-matrix.md`
- `go/internal/storage/cypher/canonical_retract.go`
- `go/internal/storage/cypher/edge_writer_retract.go`
