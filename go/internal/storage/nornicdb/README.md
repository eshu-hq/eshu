# NornicDB Storage Adapters

## Purpose

This package owns the narrow NornicDB-specific adapter between Eshu's
backend-neutral canonical Cypher writer and command-owned Bolt executors.
`PhaseGroupExecutor` commits canonical dependency phases in bounded
transactions so later `MATCH` statements see nodes committed by earlier
phases. Entity and containment chunks retain bounded parallel fan-out.

## Ownership boundary

This package owns phase ordering dispatch, statement sanitization, bounded
chunking, concurrent entity admission, retract routing, default NornicDB
limits, and canonical writer row-shape configuration.

Command packages still own driver construction, environment parsing,
transaction timeouts, retry/instrumentation wrappers, process-wide
backpressure gates, and lifecycle logging. Backend-neutral statement builders
and writer phase order remain in `internal/storage/cypher`.

## Exported surface

- `PhaseGroupExecutor` implements `cypher.Executor` and
  `cypher.PhaseGroupExecutor`. It deliberately does not implement
  `cypher.GroupExecutor`.
- `DrainReader` and `DrainWriteResult` carry bounded full-refresh retract
  iterations through a command-owned live Bolt executor. `RunWrite` is
  `DrainReader`'s only method; it runs the drain write. The bounded existence
  probe that precedes a bare-label drain (#6822) is not a `DrainReader`
  method: `executeDrainLoop` dispatches it through `PhaseGroupExecutor.Inner`
  as a `sourcecypher.ProbeExecutor` carrying `sourcecypher.
  OperationCanonicalProbe` (#6852), the same instrumented executor chain
  (backpressure, timeout, `InstrumentedExecutor`, `RetryingExecutor`) every
  other canonical write goes through.
- `WriterConfig`, `DefaultWriterConfig`, and `ConfigureCanonicalWriter` apply
  the production file/entity/per-label row caps and containment shape.
- The exported default constants and `DefaultEntityPhaseConcurrency` define the shared
  evidence-backed defaults consumed by ingester and standalone projector.

## Transaction and concurrency contract

Canonical phases run in dependency order. Whole-materialization atomic writes
are unsupported on NornicDB because dependent `MATCH` statements do not have
the required same-transaction visibility for earlier `MERGE` statements.
Retractions stay sequential or use the bounded drain route. Bare-label drains
run one bounded probe read before the `WITH ... LIMIT ... DETACH DELETE` drain
loop and skip it when nothing matches, so a retract with nothing to delete ends
after one read (#6822);
relationship-anchored drains are not probed. Only entity and
entity-containment chunks fan out, and only across disjoint label/entity keys.

The command must wrap the inner `GroupExecutor` with one process-wide canonical
backpressure gate before constructing `PhaseGroupExecutor`. Gating the outer
phase call does not bound inner fan-out. A missing inner executor fails closed;
non-empty writes must never be acknowledged without reaching the graph.

## Dependencies

- `internal/storage/cypher` for executor interfaces, statements, phase
  metadata, sanitization, bounded retract rewriting, and writer options.
- `internal/cpubudget` for the cgroup-aware default entity concurrency.
- `internal/telemetry` for canonical chunk and reconciliation counters.

The package does not import a graph driver and does not read environment
variables. It exports the shared `MinCanonicalRetractBatchSize` and
`MaxCanonicalRetractBatchSize` safety bounds; command packages own environment
parsing and reject values outside that range.

## Telemetry

Grouped inner statements continue through the command-owned retry,
`cypher.InstrumentedExecutor`, client timeout, server transaction timeout, and
backpressure layers. Drain and autocommit retracts use a command-owned
`DrainReader`; the drain write shares the same backpressure gate and retains
the server transaction timeout while bypassing grouped retry and
instrumentation wrappers. The bounded existence probe that precedes a
bare-label drain (#6822) is NOT routed through `DrainReader`: it dispatches
through `PhaseGroupExecutor.Inner` as a `sourcecypher.ProbeExecutor` (#6852),
so it draws its permit and client deadline from the SAME grouped
backpressure/`TimeoutExecutor` chain as every other canonical write, under the
`canonical_probe` operation label, and is instrumented with a
`neo4j.execute_probe` span and a `Neo4jQueryDuration{operation=probe}` point
via `cypher.InstrumentedExecutor` -- telemetry the drain write's `RunWrite`
path does not get. An operator can therefore see probe cost (span/metric) and
backpressure wait (`canonical_probe` vs `canonical_retract_drain` gate labels)
separately from drain cost.
Both the standalone projector and the ingester apply a fresh client timeout to
each raw drain iteration and return the shared retryable graph-write timeout
shape, so one lost Bolt response cannot hold a worker indefinitely (#5122 for
the projector, #5198 for the ingester). The timeout is per iteration, not
phase-wide: the deadline resets every iteration, so a drain that keeps making
progress across many iterations is never canceled by an earlier one. The probe
now gets its per-call deadline from the grouped `sourcecypher.TimeoutExecutor`
instead (the same `ESHU_CANONICAL_WRITE_TIMEOUT` budget the drain
iteration budget is derived from): each `ExecuteProbe` call opens its own
fresh child context sized to that same duration, so the probe is bounded
identically to before, just under a `neo4j execute probe timed out` operation
label from `TimeoutExecutor` rather than the drain wrapper's
`nornicdb probe timed out` label.
Drain logs and reconciliation counters provide the dedicated operator surface.
The adapter also emits bounded phase/chunk logs and rolling entity-label
summaries. No metric label contains repository paths, entity IDs, or symbols.

## Failure behavior

- Missing `Inner` returns an error for every non-empty write.
- A phase error stops new admission, waits for already-admitted entity chunks,
  and returns the first observed buffered worker error.
- Retry remains owned by the inner idempotent executor; the adapter does not
  retry non-idempotent mixed groups.
- Invalid environment values fail in command wiring before this adapter is
  constructed.

## Gotchas

- Do not add `ExecuteGroup` to `PhaseGroupExecutor`; that changes canonical
  writer dispatch back to the unsupported whole-materialization route.
- Keep the process-wide gate in command wiring around the inner executor and
  drain reader; gating only the outer phase call does not bound inner fan-out.
- Do not group all retract statements into one managed transaction.
- Do not raise batch, statement, or concurrency defaults without same-data
  before/after timing and exact graph proof.
- Partial phase commits are intentional and must converge exactly on retry.

## Verification

```bash
cd go && go test ./internal/storage/nornicdb ./cmd/ingester ./cmd/projector -count=1
cd go && go test -race ./internal/storage/nornicdb ./cmd/ingester ./cmd/projector -count=1
bash scripts/verify-replay-tier.sh
bash scripts/verify-golden-corpus-gate.sh --keep
```

## Related docs

- `go/internal/storage/cypher/README.md`
- `docs/public/reference/cypher-performance.md`
- `docs/public/reference/nornicdb-tuning.md`
- `docs/public/reference/nornicdb-pitfalls.md`
- `docs/public/reference/backend-conformance.md`
