# NornicDB Fact-Family Shadow-Write Comparison Gate

Status: implementation contract for issue #1288 and parent issue #431.
This PR extends the pure `storageeval` validation package and design guidance
only; it does not change the production fact store, Postgres schema, NornicDB
schema, queue/workflow substrate, reducers, collectors, API/MCP routes, or
runtime defaults.

Owners: storage, reducer, ingester, collector, graph backend, and reliability
maintainers.

## 1. Purpose

The #1288 gate defines how Eshu will prove that one bounded fact family can be
shadow-written to NornicDB while Postgres remains the production fact ledger.

The gate compares Postgres fact-store write and read-back behavior with a
NornicDB shadow write and read-back summary. A passing comparison proves only
that one fact family and scope/generation matched under the recorded test
conditions. It does not migrate fact storage, make NornicDB a queue, or change
collector/reducer ownership.

## 2. Behaviors Under Test

The first contract focuses on fact-ledger properties that block Postgres
replacement:

| Behavior | Required evidence |
| --- | --- |
| Stable fact identity | `fact_id`, `stable_fact_key`, and `idempotency_key` match. |
| Duplicate replay | Replaying the same idempotency key converges to one matching write. |
| Active generation | Scope and generation match, and both sides report active generation state. |
| Supersession | Passing rows report current supersession state; superseded rows are rejected. |
| Tombstone state | Active and tombstone facts compare the same record state on both sides. |
| Schema evolution | `schema_version` is semantic and matches across stores. |
| Bounded read-back | Each side reads back a bounded result count no greater than `limit`. |
| Rollback/fallback | Production stays on Postgres and shadow writes are disposable or fail closed. |

The tests use a documentation-source-style fact family because it is low risk
and already has explicit schema versions. Future proof runners can add other
fact families only after source-specific privacy, schema, and rollback rules
are documented.

## 3. Evidence Contract

Every `FactWriteComparison` record must include:

- `fact_family`: the bounded migration lane being evaluated.
- `fact_kind`: the fact kind in the comparison.
- `scope_id` and `generation_id`: the durable scope/generation boundary.
- `idempotency_key`: the stable write key, matching `stable_fact_key`.
- `limit`: the bounded read-back limit.
- `replay_count`: duplicate replay count for the proof run.
- `baseline`: the Postgres fact-store write/read-back summary.
- `shadow`: the NornicDB shadow write/read-back summary.
- `verdict`: `match` for passing evidence.
- `fallback_behavior`: production fallback if shadow proof fails.
- `rollback_behavior`: how shadow writes are removed or isolated.
- `failure_class`: `none` for passing evidence.

Each result summary records:

- backend label;
- fact id and stable fact key;
- scope id and generation id;
- fact kind and schema version;
- active or tombstone record state;
- generation state and supersession state;
- digest of the canonicalized read-back row;
- observation time;
- latency;
- support status;
- bounded result count.

## 4. Passing Gate

`ValidateFactWriteComparison` accepts a record only when all of these are true:

- fact family, fact kind, scope id, generation id, and idempotency key are
  present;
- `limit` is positive and `replay_count` is not negative;
- fallback and rollback behavior are explicit;
- baseline backend is `postgres_fact_store`;
- shadow backend is `nornicdb_shadow_fact_store`;
- each side's stable fact key matches the idempotency key;
- each side's scope, generation, and fact kind match the comparison;
- schema versions are semantic and match exactly;
- generation state is active on both sides;
- supersession state is current on both sides;
- record state is either active or tombstone and matches across stores;
- write digest is present and matches across stores;
- bounded result count is non-negative and does not exceed `limit`;
- verdict is `match`;
- failure class is `none`.

## 5. Rejected States

The gate rejects and classifies these states:

| State | Why it blocks parity |
| --- | --- |
| Missing idempotency key | Duplicate replay cannot be proven. |
| Missing scope or generation | The fact write is not tied to a durable boundary. |
| Unbounded fact scan | Read-back parity could hide missing or extra rows. |
| Missing fallback or rollback behavior | Operators cannot keep production safe. |
| Missing shadow write | NornicDB did not reproduce the Postgres fact. |
| Stale generation | Equality against inactive generation data is not proof. |
| Superseded shadow state | The shadow write is not current. |
| Tombstone mismatch | Active/deleted state diverged. |
| Schema-version mismatch | The stores disagree on fact schema evolution. |
| Divergent active generation | Shadow data belongs to the wrong generation. |
| Unsupported capability | The shadow backend cannot answer this fact family. |
| Negative replay count | Replay evidence is invalid. |

Rejected rows may be retained as diagnostics by future proof runners, but they
must not count as passing parity evidence.

## 6. Runtime Behavior

Production behavior remains unchanged while this gate is in use:

```text
collector or ingester emits facts
  -> existing Postgres fact store writes production ledger rows
  -> optional proof runner writes the same canonicalized row to shadow NornicDB
  -> proof runner reads back bounded summaries from both stores
  -> storageeval validates comparison evidence
  -> parity evidence informs a later cutover ADR
```

Collectors remain source-fact emitters. Reducers and query surfaces remain
truth owners. NornicDB must not own claim, lease, retry, delayed retry,
dead-letter, or workflow coordination semantics through this gate.

## 7. Observability Requirements

Future proof runners must expose:

- comparison count by fact family and fact kind;
- shadow-write comparison duration;
- duplicate replay count;
- parity drift count and latest drift time;
- failure class counts;
- fallback and rollback count by reason;
- baseline and shadow write/read-back latency distribution;
- bounded fact count by comparison.

High-cardinality ids such as scope ids, generation ids, fact ids, stable fact
keys, source record ids, digests, and request ids belong in logs or traces, not
metric labels.

No-Observability-Change: this PR defines labels and validation behavior but
does not alter hosted runtime telemetry.

## 8. Non-Goals

This PR does not:

- migrate `fact_records`;
- add a NornicDB fact-store adapter;
- persist shadow-write reports;
- change fact schema;
- alter collector, ingester, projector, reducer, API, MCP, or CLI behavior;
- use NornicDB as a queue or workflow coordinator;
- close parent issue #431.

## 9. Evidence For This PR

No-Regression Evidence: `go test ./internal/storageeval -count=1` proves the
gate accepts duplicate replay and matching tombstone evidence, and rejects
missing fact family, missing idempotency key, missing scope, missing
generation, unbounded scans, missing fallback behavior, missing rollback
behavior, missing shadow writes, stale generation, superseded shadow state,
schema-version mismatch, invalid schema versions, divergent active generation,
tombstone mismatch, unsupported shadow capability, divergent shadow digest,
non-match verdicts, missing failure class, and negative replay count.

No-Observability-Change: the package is pure and emits no hosted metrics,
spans, or logs. Future proof runners must emit the signals listed above.

Source check date: 2026-06-02.

Sources used:

- `go/internal/facts/models.go`
- `go/internal/facts/stableid.go`
- `go/internal/facts/README.md`
- `go/internal/storage/postgres/facts.go`
- `go/internal/storage/postgres/facts_filtered.go`
- `go/internal/storage/postgres/schema_fact_records.go`
- `docs/internal/design/431-nornicdb-primary-store-evaluation.md`
- `docs/internal/design/1286-postgres-ownership-inventory.md`
