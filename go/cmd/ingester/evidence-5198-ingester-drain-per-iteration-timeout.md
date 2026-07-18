# Evidence: ingester NornicDB retract drain gets a per-iteration client deadline (#5198)

## Problem

The ingester full-refresh DETACH DELETE drain loop
(`nornicdb.PhaseGroupExecutor.executeDrainLoop`) intentionally bypasses the
grouped `TimeoutExecutor` so one phase-wide deadline cannot cancel a correctly
progressing multi-iteration drain. But the ingester wired the **raw**
`DrainReader` with no per-iteration deadline, so a single lost Bolt response
could hold one drain iteration open indefinitely. The standalone projector
already applies a fresh per-iteration timeout (#5122); the ingester did not.

## Fix

`ingesterTimeoutDrainReader` (`go/cmd/ingester/wiring_gated_drain_reader.go`)
wraps each `RunWrite` in a fresh `context.WithTimeout(ctx, nornicDBTimeout)`.
On the client deadline (parent context still live) it returns the shared
retryable `sourcecypher.GraphWriteTimeoutError`
(`FailureClass() == "graph_write_timeout"`, `Retryable() == true`), so the
reducer queue keeps its existing graph_write_timeout retry/dead-letter
classification. A parent-driven cancellation is forwarded unchanged, and a
non-positive timeout is a passthrough (mirrors `TimeoutExecutor`). It is wired
inside the existing canonical gate, matching the projector layering.

The timeout is **per iteration, not phase-wide**: the deadline resets every
iteration, so a drain that keeps making progress across many iterations is
never canceled by an earlier one. Worker count, entity fan-out, the shared
graph-write gate, lease/heartbeat behavior, and retract ordering are unchanged.

## No-Regression Evidence:

Focused unit + race (per-iteration deadline maps a blocked iteration to a
retryable graph_write_timeout well before the outer deadline; a slow-but-
progressing 5-iteration drain completes and is not canceled):

```
cd go
go test ./cmd/ingester -run 'TestIngesterNornicDBDrainUsesPerIterationClientTimeout|TestIngesterNornicDBMultiIterationDrainNotCanceledByEarlierIteration' -race -count=20   # PASS
go test ./cmd/ingester ./internal/storage/nornicdb -count=1                                                                                                             # ok
go test ./internal/storage/nornicdb -run Drain -race -count=5                                                                                                           # ok
```

Representative live drain on both pinned NornicDB images (real
`ingesterNeo4jExecutor.RunWrite` through the per-iteration timeout wrapper +
gate; 250 stale-generation File nodes, retract batch 50 -> 5 drain iterations,
backlog drained to 0). Run once per image (each line is a complete, runnable
command):

```bash
cd go

# timothyswt/nornicdb-cpu-bge:v1.1.11 -> total_drained=250, File count after = 0
ESHU_INGESTER_DRAIN_PROVE_LIVE=1 ESHU_NEO4J_DATABASE=nornic ESHU_NEO4J_URI=bolt://127.0.0.1:17688 go test ./cmd/ingester -run TestIngesterNornicDBDrainLiveDeletesEntireBacklog -count=1

# eshu-nornicdb-pr261 (docker-compose default) -> drained to 0
ESHU_INGESTER_DRAIN_PROVE_LIVE=1 ESHU_NEO4J_DATABASE=nornic ESHU_NEO4J_URI=bolt://127.0.0.1:17689 go test ./cmd/ingester -run TestIngesterNornicDBDrainLiveDeletesEntireBacklog -count=1
```

This is a reliability wrapper; it adds one `context.WithTimeout` per bounded
drain iteration (negligible) and does not change the drain Cypher, batch size,
or write concurrency.

## No-Observability-Change:

No new metric, span, log field, queue stage, worker knob, or status field. The
per-iteration timeout reuses the existing `graph_write_timeout` failure class
(`GraphWriteTimeoutError.FailureClass`) already counted by the reducer queue and
the write-backpressure gate, and the existing `nornicdb retract drain iteration`
/ `nornicdb retract drain completed` logs plus reconciliation-drift counters
remain the operator surface.
