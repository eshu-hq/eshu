# NornicDB Primary Knowledge Store Evaluation

Status: proposed evaluation gate for issue #431. Design only; no code,
schema, Compose YAML, Helm chart, runtime default, API/MCP route, queue
substrate, or storage ownership change in this PR.

Owners: storage, graph backend, reducer, query, runtime, and reliability
maintainers.

## 1. Decision

Eshu must not replace Postgres as the primary durable store yet.

NornicDB remains the default canonical graph backend and may become a curated
search-lane backend after the issue #430 proof stack lands. That evidence does
not prove NornicDB can own facts, queue/workflow state, content/read models,
status, recovery, or migration rollback.

Future Postgres removal is allowed only as a phased migration after these gates
exist and pass:

1. Postgres ownership inventory: #1286.
2. Content/read-model shadow-read comparison: #1287.
3. Fact-family shadow-write comparison: #1288.
4. Queue/workflow substrate evaluation: #1289.
5. NornicDB backup/restore proof for every migrated durable state class: #1290.

Queue and workflow ownership must be decided separately from NornicDB storage.
NornicDB must not become Eshu's claim, lease, fencing, retry, delayed retry, or
dead-letter substrate unless a separate queue design proves those semantics.

## 2. Problem

Postgres is not only a search backend. It currently carries several durable
platform contracts:

| Responsibility | Current owner | Why this blocks direct replacement |
| --- | --- | --- |
| Fact ledger and active fact lookup | Postgres fact store | Requires stable fact ids, scope/generation filtering, supersession, tombstones, payload schema evolution, and idempotent re-ingestion. |
| Projector and reducer queues | Postgres queues | Requires claim, lease, fencing, retry, dead-letter, conflict-key ordering, and crash recovery. |
| Workflow coordinator runs and work items | Postgres workflow store | Requires durable collector scheduling, claim lifecycle, completeness, and reaping. |
| Status and readiness | Postgres status/admin rows | Requires bounded freshness, backlog, failure, and completeness reads for operators. |
| Recovery and replay | Postgres recovery rows and queue state | Requires repeatable failure classification, retry windows, and dead-letter replay. |
| Content files and entities | Postgres content store | Requires file, line, entity, structural inventory, and current content read models. |
| Relationship evidence/read models | Postgres fact-backed read models plus graph projection | Requires provenance drilldown to agree with canonical graph and API/MCP stories. |
| Collector freshness and provider status | Postgres source-specific status | Requires recent warnings, checkpoints, drift state, and source liveness. |
| Decisions and accepted generation state | Postgres decision/read stores | Requires accepted output to survive replay and remain auditable. |

Issue #430 narrows one storage concern: canonical graph startup must not be
coupled to whole-graph BM25/vector indexing. It does not answer whether
NornicDB can replace Postgres for the rest of Eshu's durable responsibilities.

## 3. Target Evaluation Shape

The storage migration model should be evidence-first:

```text
current Postgres owner
  -> ownership inventory
  -> shadow write or shadow read
  -> parity evidence
  -> rollback proof
  -> production cutover proposal
```

Every cutover proposal must identify:

- current owner runtime and package;
- candidate future owner;
- read/write API surface affected;
- idempotency key;
- transaction boundary;
- retry boundary;
- rollback behavior;
- backup/restore proof;
- observability used by a 3 AM operator.

## 4. Ownership Candidates

### 4.1 NornicDB can plausibly absorb

NornicDB is a candidate for:

- canonical graph storage, already current default;
- curated search documents after #430 and the search benchmark stack;
- selected content/read models after #1287 proves API/MCP parity;
- selected fact-family read/write storage after #1288 proves active generation,
  supersession, tombstone, and idempotency behavior;
- relationship evidence/read models only after graph truth and provenance
  drilldown agree.

### 4.2 NornicDB should not absorb without a separate queue proof

NornicDB should not own:

- claimable projector work;
- reducer shared intents;
- workflow coordinator work items;
- lease/fencing tokens;
- delayed retry;
- dead-letter queues;
- backpressure and fair scheduling;
- crash-recovery coordination.

Those responsibilities need #1289 and may remain in Postgres or move to a
dedicated queue/workflow system such as SQS, NATS JetStream, Temporal, or
another substrate with proven semantics.

### 4.3 Blob or object store may be needed

Large source snapshots, raw file bodies, or backup artifacts may belong in an
object/blob store instead of either Postgres or NornicDB. The decision depends
on access pattern, restore proof, storage cost, and whether the data belongs in
bounded query responses.

## 5. Migration Phases

1. Finish the #430 graph/search split stack.
2. Complete #1286 Postgres ownership inventory.
3. Run #1287 shadow-read comparison for content/read models without changing
   production reads.
4. Run #1288 shadow-write comparison for one low-risk fact family.
5. Decide queue/workflow ownership through #1289.
6. Prove NornicDB backup/restore and rollback through #1290 for each migrated
   durable state class.
7. Draft a production cutover ADR for one responsibility at a time.

No phase may skip parity evidence because a narrower NornicDB proof passed.
Graph backup proof is not content, fact, read-model, or queue proof.

## 6. Correctness Gates

Every future migration PR must prove:

- Postgres baseline and candidate owner answers match for the scoped
  responsibility;
- stale, missing, divergent, tombstoned, and duplicate states are classified;
- fallback behavior is explicit and observable;
- API/MCP truth labels do not silently downgrade;
- graph truth and read-model truth agree where both are involved;
- rollback restores the Postgres-backed answer without hiding parity drift.

## 7. Performance Gates

Every future migration PR must record:

- same-shape baseline and candidate latency;
- row, fact, document, or graph count for the scoped proof;
- memory and storage growth;
- startup and restore duration when durable state is added;
- p50 and p95 read latency for user-facing paths;
- queue depth, oldest age, retry count, and dead-letter count when workflow
  state is involved.

Stop threshold: do not cut over a responsibility if the candidate path is
slower, less diagnosable, loses fallback behavior, or introduces unbounded
queries.

## 8. Observability Requirements

Future storage migration work must expose or reuse signals for:

- parity drift count and latest drift time;
- shadow-read and shadow-write comparison duration;
- migrated row/document/fact counts;
- fallback count by reason;
- backup artifact age and size;
- restore duration and failure class;
- queue backlog, overdue claims, retries, and dead letters for the chosen
  workflow substrate.

High-cardinality ids such as repository ids, document ids, fact ids, work-item
ids, and graph handles belong in logs or traces, not metric labels.

## 9. Non-Goals

This PR does not:

- remove Postgres;
- change storage schema;
- add NornicDB fact/content/read-model writes;
- add a queue/workflow substrate;
- change API, MCP, CLI, reducer, ingester, or collector behavior;
- change backup tooling;
- close #431.

## 10. Evidence For This PR

No-Regression Evidence: design-only PR; no Go, Cypher, schema,
docker-compose YAML, Helm chart, OpenAPI, MCP, queue, storage, or
runtime-default files are changed.

No-Observability-Change: design-only PR; it names the telemetry required for
future storage, queue, and migration work and does not alter existing signals.

Source check date: 2026-06-02.

Sources used:

- [System Architecture](../../public/architecture.md)
- [Service Runtimes](../../public/deployment/service-runtimes.md)
- [Telemetry Overview](../../public/reference/telemetry/index.md)
- [Local Testing](../../public/reference/local-testing.md)
- [NornicDB Graph/Search Split](430-nornicdb-graph-search-split.md)
- [Search Benchmark Evidence](../../public/reference/search-benchmark-evidence.md)
- Postgres package contract: `go/internal/storage/postgres/doc.go`
