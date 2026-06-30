# Evidence: #4232 Bounded Canonical Retract Drain Loop

## Root Cause

Unbounded `DETACH DELETE` on NornicDB for full-refresh File/Directory/Entity
retracts times out at the 2-minute executor deadline under full-corpus
full-refresh (5000+ files + 10000+ entities — ~23–39 s per statement observed
live on v1.1.9 at corpus scale).

The four affected Cypher statements (produced by
`canonical_node_writer_retract.go`):

1. `canonicalNodeRetractFilesCypher` — stale :File nodes by generation
2. `canonicalNodeRetractRemovedFilesCypher` — :File nodes not in keep-list
3. `canonicalNodeRetractDirectoriesCypher` — stale :Directory nodes
4. `canonicalNodeRetractEntityTemplate` (per label) — stale entity nodes

Delta retracts and positive-list retracts (`canonicalNodeRetractParametersCypher`,
`canonicalNodeRetractDeltaDeletedFilesCypher`,
`canonicalNodeRetractDeltaEmptyDirectoriesCypher`,
`canonicalNodeRetractDeltaEntityTemplate`) are bounded by construction and are
NOT affected.

## Fix

`Statement.Drain` / `Statement.DrainVar` fields (added to `writer.go`) mark the
four unbounded statements. At execution time on NornicDB,
`executeDrainLoop` (in `wiring_nornicdb_phase_group.go`) rewrites each marked
statement via `BuildBoundedRetractDrainCypher` (in `bounded_retract_drain.go`)
into a loop that iterates until zero nodes are deleted. Safety cap:
`5_000_000 / batch + 2` iterations. Neo4j uses the original single-statement
path unchanged.

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
automatically. The shape classification is pinned by
`TestBuildBoundedRetractDrainCypherShapeClassification` which runs all four
production constants through the rewriter and asserts the exact WITH clause.

## Performance Evidence

### Before (unbounded single DETACH DELETE, NornicDB v1.1.9)

```
5000 :File nodes + REPO_CONTAINS edges, full-refresh generation fence:
  File retract (single statement):       ~23–39 s
  Entity retract per label (5000 nodes): proportional — hits 2 m budget at corpus scale
```

### After (bounded drain loop, NornicDB v1.1.9, batch=2000)

Reproducer: bolt://localhost:7688, database "nornic", no-auth,
scope `repository:__retracttest__`, synthetic nodes only.

```
File retract (relationship-anchored, WITH f LIMIT $batch):
  5000 nodes drained, 3 iterations, ~14.5 s total, 0 stale remaining

Directory / Entity retract (bare-label, WITH n ORDER BY elementId(n) LIMIT $batch):
  ~5000 nodes drained, 3 iterations, ~61.5 s total, 0 stale remaining

Wrong clause per shape (ORDER BY on anchored, or bare LIMIT on bare-label):
  __drained = 0 on every iteration — 0 nodes deleted, all stale remaining
  (this is the failure mode that motivates shape-dependent rewriting)
```

### Small corpus no-regression (batch=5, 12 old-gen + 3 new-gen nodes)

```
FileRetractProof (anchored):   drained=12 iters=4 108ms remaining=3 ✓
DirRetractProof  (bare-label): drained=12 iters=4 123ms remaining=3 ✓
FuncRetractProof (bare-label): drained=12 iters=4 105ms remaining=3 ✓
PASS: all 3 shapes drain correctly on NornicDB v1.1.9
```

### Zero-stale corpus

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
go test ./internal/storage/cypher/... ./cmd/ingester/... \
         ./internal/reducer/... ./internal/projector/... -count=1
  3527 passed (8 packages)

go test ./cmd/golden-corpus-gate/... -count=1
  59 passed (snapshot unchanged)

golangci-lint run ./internal/storage/cypher/... ./cmd/ingester/...
  (no output — clean)

gofumpt -l (all changed files)
  (no output — clean)

git diff --check
  clean
```
