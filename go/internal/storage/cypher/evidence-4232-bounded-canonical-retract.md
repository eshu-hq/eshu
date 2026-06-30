# Evidence: #4232 Bounded Canonical Retract Drain Loop

## Root Cause

Unbounded `DETACH DELETE` on NornicDB for full-refresh File/Directory/Entity
retracts times out at the 2-minute executor deadline under full-corpus
full-refresh (5000 files + 10000 entities = ~23 s per statement observed live).

The four affected Cypher statements (produced by
`canonical_node_writer_retract.go`):

1. `canonicalNodeRetractStaleFilesCypher` — stale :File nodes by generation
2. `canonicalNodeRetractRemovedFilesCypher` — :File nodes not in keep-list
3. `canonicalNodeRetractStaleDirectoriesCypher` — stale :Directory nodes
4. `canonicalNodeRetractEntityByLabelCypher` (per label) — stale :Entity nodes

Delta retracts and positive-list retracts (`canonicalNodeRetractParametersCypher`,
`canonicalInheritanceRetractCypher`) are bounded by construction and are NOT
affected.

## Fix

`Statement.Drain` / `Statement.DrainVar` fields (added to `writer.go`) mark the
four unbounded statements. At execution time on NornicDB,
`executeDrainLoop` (in `wiring_nornicdb_phase_group.go`) rewrites each marked
statement via `BuildBoundedRetractDrainCypher` (in `bounded_retract_drain.go`)
into a `LIMIT $__retract_batch ... RETURN count(var) AS __drained` loop that
iterates until zero nodes are deleted. Safety cap: `5_000_000 / batch + 2`
iterations. Neo4j uses the original single-statement path unchanged.

Knob: `ESHU_CANONICAL_RETRACT_BATCH` (default 2000, range 1–10000).

### NornicDB v1.1.9 WITH-clause shape rule

Live testing on the v1.1.9 container (bolt://localhost:7688) revealed that the
correct `WITH` clause before `LIMIT` depends on the MATCH shape:

| Shape | Example | Required WITH clause |
|---|---|---|
| Relationship-anchored | `MATCH (r)-[:REPO_CONTAINS]->(f:File)` | `WITH f LIMIT $batch` |
| Bare-label | `MATCH (d:Directory) WHERE d.repo_id=…` | `WITH d ORDER BY elementId(d) LIMIT $batch` |

Adding `ORDER BY` to a relationship-anchored query returns `__drained=0` (no
deletes). Omitting `ORDER BY` from a bare-label query also returns `__drained=0`.

`BuildBoundedRetractDrainCypher` detects the shape by scanning the first MATCH
line for `)-[` (relationship pattern) and emits the appropriate clause
automatically.

## Performance Evidence

Method: synthetic corpus seeded into `repository:__retracttest__`, 12 old-gen +
3 new-gen nodes per label, batch=5. Run against NornicDB v1.1.9 at
bolt://localhost:7688, database "nornic", no-auth.

**Live gate run (v1.1.9, batch=5)**

```
Before: FileRetractProof=15 DirRetractProof=15 FuncRetractProof=15

FileRetractProof (relationship-anchored, WITH f LIMIT $batch):
  drained=12  iters=4  duration=108ms
  remaining=3  (want 3) ✓

DirRetractProof (bare-label, WITH d ORDER BY elementId(d) LIMIT $batch):
  drained=12  iters=4  duration=123ms
  remaining=3  (want 3) ✓

FuncRetractProof (bare-label entity template, WITH n ORDER BY elementId(n) LIMIT $batch):
  drained=12  iters=4  duration=105ms
  remaining=3  (want 3) ✓

PASS: all 3 shapes drain correctly on NornicDB v1.1.9
```

**No-regression on zero-stale corpus**

```
  First drain call returns __drained=0; loop exits after 1 iteration.
  Covered by TestNornicDBPhaseGroupExecutorDrainLoopStopsImmediatelyOnZero.
```

## No-Observability-Change

The drain loop reuses the existing canonical write spans and metrics emitted
by `executeSequentialRetractPhase` / `executeEntityPhaseGroup`. Each drain
iteration emits one `slog.Debug` line (key `__drained`) for troubleshooting
without adding metric cardinality. The final iteration emits one `slog.Info`
line with total iterations and total nodes drained. No new OTEL spans, counters,
or histograms are introduced; existing write-path latency histograms continue
to capture per-statement timing.

## Concurrency Safety

Retract conflict_domain = scope (one worker per scope). Deletes are idempotent:
a node deleted in iteration N is simply absent in iteration N+1 without error.
The loop is safe under concurrent writes to the same scope because new nodes
written during drain carry the current generation and are excluded from the
retract predicate (generation fence in the WHERE clause).

## Gate Output

```
go test ./internal/storage/cypher ./cmd/ingester -count=1
  803 passed (2 packages)

go test ./internal/reducer/ ./internal/projector/ -count=1
  2703 passed (2 packages)

go test ./cmd/golden-corpus-gate -count=1
  58 passed (snapshot unchanged)

golangci-lint run ./internal/storage/cypher/... ./cmd/ingester/...
  (no output — clean)

gofumpt -l (all changed files)
  (no output after gofumpt -w wiring.go — clean)

git diff --check
  clean
```
