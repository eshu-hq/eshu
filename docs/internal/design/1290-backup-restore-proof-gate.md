# NornicDB Durable-State Backup Restore Proof Gate

Status: implementation contract for issue #1290 and parent issue #431.
This PR extends the pure `storageeval` validation package and design guidance
only; it does not implement backup tooling, restore runners, production storage
ownership, runtime defaults, or fallback reads.

Owners: storage, graph backend, reducer, query, runtime, and reliability
maintainers.

## 1. Purpose

The #1290 gate blocks production Postgres removal until every migrated durable
state class has its own NornicDB backup, clean restore, parity, fallback, and
rollback proof.

Graph backup proof is not content, fact-family, read-model, search-document, or
relationship-evidence proof. A future cutover must prove the exact durable state
class it wants to move.

## 2. Durable State Classes

The proof contract covers these candidate classes:

| State class | Required consistency checks |
| --- | --- |
| `content_read_model` | content and read-model parity |
| `fact_family` | fact-family parity |
| `relationship_evidence` | graph, fact, and relationship-evidence parity |
| `search_document` | content and read-model parity |
| `graph_schema_state` | graph/schema parity |

Production Postgres removal is blocked for any class without a passing proof.
Passing proof for one class does not promote another class.

## 3. Evidence Contract

Every `BackupRestoreProof` record must include:

- proof id;
- durable state class;
- bounded scope;
- NornicDB image and commit;
- Eshu commit;
- backup artifact id, kind, status, digest, schema version, generation id,
  creation time, and size;
- clean restore target, start time, duration, and completion state;
- Postgres baseline snapshot;
- NornicDB restored snapshot;
- state-class-specific consistency checks;
- required failure scenarios;
- `keep_postgres` fallback behavior;
- rollback behavior;
- restore observability;
- `match` verdict and `none` failure class for passing evidence.

The baseline and restored snapshots must match on state class, schema version,
generation id, count, and digest. The proof must be bounded; unbounded restore
parity is not accepted.

## 4. Required Scenarios

The proof must cover these scenarios with executable evidence or an accepted
proof plan:

| Scenario | Required question |
| --- | --- |
| Missing artifact | Does a missing backup fail closed without hiding drift? |
| Corrupt artifact | Does artifact integrity failure block promotion? |
| Version mismatch | Does incompatible schema or generation evidence block restore? |
| Partial restore | Does count or digest drift fail the proof? |
| Stale generation | Does an old generation fail instead of becoming production truth? |
| Fallback to Postgres baseline | Does production stay on Postgres when restore proof fails? |

Fallback reads cannot be used to mask parity drift. They are only evidence that
Postgres remains the production baseline until the candidate proves parity.

## 5. Required Observability

Future proof runners must expose:

- backup artifact age;
- artifact size;
- restore duration;
- restore failure class;
- parity drift.

High-cardinality values such as repository ids, document ids, fact ids,
generation ids, artifact ids, and digests belong in logs or traces, not metric
labels.

No-Observability-Change: this PR defines the required evidence signals but does
not alter hosted runtime telemetry.

## 6. Rejected States

The gate rejects:

- missing NornicDB image, NornicDB commit, or Eshu commit;
- missing, corrupt, unsupported, or undigested artifacts;
- non-clean restore targets;
- incomplete restores;
- missing baseline or restored snapshots;
- schema-version, generation, count, or digest mismatch;
- unbounded restore parity;
- graph-only proof for non-graph durable state classes;
- missing required failure scenarios;
- failed required scenarios;
- fallback behavior that does not keep Postgres as baseline;
- missing rollback behavior;
- missing backup/restore observability;
- non-match verdicts or non-none failure classes.

## 7. Runtime Behavior

Production behavior remains unchanged:

```text
current Postgres owner
  -> backup/restore proof record
  -> clean target restore
  -> state-class consistency checks
  -> parity and fallback evidence
  -> later cutover ADR for exactly one durable state class
```

NornicDB may own a migrated class only after that class has passing proof and a
separate cutover ADR updates storage ownership.

## 8. Non-Goals

This PR does not:

- change backup tooling;
- implement a restore runner;
- change Postgres, NornicDB, Compose, Helm, API, MCP, reducer, ingester, or
  collector behavior;
- add fallback reads;
- remove Postgres;
- close parent issue #431.

## 9. Evidence For This PR

No-Regression Evidence: `go test ./internal/storageeval -count=1` proves the
gate accepts covered clean restore evidence and graph-schema restore evidence,
then rejects missing state class, missing image or commit evidence, missing
artifact id, digest, or size, missing artifact, corrupt artifact, version
mismatch, partial restore, stale generation, non-clean restore target, missing
restore duration, unbounded restore, digest drift, graph-only proof for content
state, missing state-class consistency, missing or failed required scenarios,
missing backup-age observability, non-Postgres fallback, missing rollback,
non-match verdict, and missing failure class.

No-Observability-Change: the package is pure and emits no hosted metrics,
spans, or logs. Future proof runners must emit the signals listed above.

Source check date: 2026-06-02.

Sources used:

- `go/internal/storageeval/README.md`
- `docs/internal/design/431-nornicdb-primary-store-evaluation.md`
- `docs/internal/design/1286-postgres-ownership-inventory.md`
- `docs/internal/design/1287-shadow-read-comparison-gate.md`
- `docs/internal/design/1288-shadow-write-comparison-gate.md`
- `docs/internal/design/1289-queue-substrate-evaluation-gate.md`
- `docs/public/reference/nornicdb-tuning.md`
- `docs/public/reference/nornicdb-pitfalls.md`
