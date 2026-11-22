# Evidence: advisory-group reconcile FOREACH-free rewrite costs more per statement (#11161)

Records the performance and observability evidence for the #11161 rewrite of
`go/internal/storage/cypher/edge/writer/advisory_group.go` (one
`MERGE`/`FOREACH`/`MERGE`/`FOREACH` upsert split into node-create,
node-update, edge-merge, edge-update plus reshaped retracts), so the
hot-path evidence gate (`scripts/verify-performance-evidence.sh`) has a
tracked, in-repo record specific to this change rather than relying on PR
prose.

## Change shape

The production write path for one advisory group goes from 3 statements
(1 UNWIND upsert batch per batch + stale-edge retract + edgeless cleanup) to
6 at production batching (node create + node update + 1 edge-merge batch +
1 edge-update batch + stale-edge retract + edgeless cleanup), with more
batches at smaller batch sizes (merge and update each batch independently).
No worker knob, queue, lease, retry, batch-size default, or dispatch path
changes: `execute()` stays sequential autocommit, the stale retract stays
off `ExecuteGroup`, and the convergence tests drive the same production
`RetryingExecutor` seam the reducer uses. The split is forced, not chosen:
every single-statement conditional form was measured broken on the pinned
NornicDB build (see the `FOREACH`-after-`MERGE` pitfall in
`docs/public/reference/nornicdb-write-shape-pitfalls.md`), while the pre-fix shape
dead-letters on the gate-default backend — it never completes at any speed.

## Performance Evidence:

Same host, same pinned Neo4j (`neo4j:2026-community`
`sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`),
same 50-edge / 5-repository group, `batchSize 500` (one batch each way),
unique `advisory_group_id` constraint plus `repository_id` constraint
applied, 10 timed reconciles per cell (throwaway `TestZZTimingAdvisoryReconcile`,
removed before merge):

| path | base per-op | branch per-op |
|---|---|---|
| fresh-group reconcile (create) | 11.6 ms | 32.6 ms |
| no-op replay (all zero) | 1.76 ms | 15.8 ms |

Per-statement replay profile of the new shape (same backend, 50 rows):
nodeCreate 0.80 ms, nodeUpdate 1.39 ms, edgeMerge 4.11 ms, edgeUpdate
5.30 ms, staleRetract 0.66 ms, edgeless 0.41 ms. The cost is the extra
statements (6 vs 3) plus the conditional evaluations the single-statement
shape did not need; per-statement work is comparable (old upsert batch
~10 ms per 50 rows vs new merge 4.1 ms + update 5.3 ms per 50 rows).

Gate-default backend (NornicDB v1.3.3 line, image digest
`sha256:74a8ed7b36f37bdd1a7e32d8bc6aa3fa88908b7207bfa6568567ab94e4a4b3b1`):
the pre-fix baseline is not slower, it is a hard error — the RED run fails
every reconcile with `UNWIND MATCH failed: end node variable ...`. Post-fix
on the same image and fixture: create 328 ms, replay 260 ms per 50-edge
group, replay write stats exactly zero. Throughput goes from zero
(dead-letter) to these numbers.

The advisory projection is an async queue-drained reducer path, not request
latency: absolute per-group times stay in tens of milliseconds on Neo4j and
hundreds on NornicDB. Batching is unchanged (`DefaultBatchSize = 500`),
so larger groups scale with the same batch count as before plus one update
batch per merge batch.

## No-Regression Evidence:

No hot-path contract regresses: idempotent replay still reports exactly zero
write stats on both backends, stamp updates still resync, retract and empty
cleanup still converge, and the parallel-convergence contract still holds on
pinned Neo4j (see the live proofs in the pitfalls entry). The counter
accounting the handler reads (`WriteStats` sums) is unchanged in shape; only
the per-template split moved (node create vs update, edge merge vs update).

## Observability Evidence:

No metric, span, log key, or status surface added or altered. The handler's
`RecordAdvisoryGroupReconciled` telemetry reads the same `WriteStats`
fields. One new observable behavior to watch, not a new signal: under
concurrent duplicate same-group reconciles, the conditional-update
transactions can hit transient upgrade deadlocks that the production
`RetryingExecutor` absorbs and retries (existing retry counter, no new
instrument). A sustained rise in that existing counter on the advisory
projection would mean duplicate delivery storms, not a writer bug.
