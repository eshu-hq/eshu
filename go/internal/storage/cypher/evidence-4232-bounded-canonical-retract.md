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

## Performance Evidence

Method: synthetic corpus using parameterized path array (no string concatenation),
scope `repository:__retracttest__`.

**Before (single unbounded DETACH DELETE)**

```
5000 :File nodes + 10000 :Entity nodes, full-refresh generation fence:
  File retract statement:   ~23 s  (hits 2 m timeout at full corpus)
  Entity retract (×N labels): proportional
```

**After (bounded drain loop, batch=2000)**

```
  5000 :File nodes, batch=2000 → 3 iterations, ~420 ms total
  10000 :Entity nodes, batch=2000 → 6 iterations, ~890 ms total
  Zero stale nodes remaining (verified via COUNT query post-loop)
  Safety cap (5_000_000/2000 + 2 = 2502) never approached
```

**No-regression on small corpus (100 files / 200 entities)**

```
  1 iteration each (all deleted in first pass), ~40 ms total
  Behaviour identical to single-statement path for small deletes
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
