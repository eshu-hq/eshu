# Evidence: C-14 (#4367) semantic entity retract dispatch fix

## Scope

`SemanticEntityWriter.WriteSemanticEntities` (`go/internal/storage/cypher/semantic_entity.go`)
previously dispatched its whole statement list — the delta/full **retract**
DETACH DELETEs plus the MERGE/SET **upserts** — through a single `ExecuteGroup`
(one managed Bolt transaction). This change splits the dispatch: retract
statements now run sequentially through `Execute` (one autocommit transaction
each), while upserts continue to batch through `ExecuteGroup`.

This is a **correctness fix**, not a performance change. Grouped DETACH DELETEs
under-apply on the pinned NornicDB v1.1.11 (the same managed-transaction
`tx.Run` deletes-zero-rows limitation the `EdgeWriter` shell/documentation
retracts already work around in `edge_writer_retract.go`, and issue #4902): a
grouped per-label retract silently left semantic nodes (e.g. `Variable`) in the
graph, so semantic delta retracts accumulated stale nodes in production.

## No-Regression Evidence:

- **Backend / version:** NornicDB v1.1.11 — the unpatched pinned image the
  replay-tier gate provisions,
  `timothyswt/nornicdb-cpu-bge:v1.1.11@sha256:51b6174ae65e4ce54a158ac2f9eace7d36a1971545824d22add0fe06d94c1090`
  (`scripts/verify-replay-tier.sh`), bolt, `ESHU_GRAPH_BACKEND=nornicdb`,
  database `nornic`. Both the baseline and after measurements below were taken
  against this exact base image (no #264/#4902 backend patches), which is what
  the fix has to hold on and what CI runs.
- **Input shape:** one repo, two `Variable` entities in two files, gen1 upsert
  then gen2 delta retract of one file path (the live regression
  `TestReducerSemanticVariableRetractGraphTruth`,
  `go/internal/replay/offlinetier/delta_tier_reducer_semantic_variable_retract_live_test.go`).
- **Baseline (grouped dispatch, before):** gen2 delta retract dispatched through
  `ExecuteGroup` (managed Bolt transaction) leaves the in-scope `Variable`
  present — read-back `count = 1`, want `0`. Silent data retention (a
  correctness defect), measured directly against the base v1.1.11 backend.
- **After (sequential retract dispatch):** gen2 delta retract dispatched through
  `Execute` (autocommit) removes the in-scope `Variable` — read-back
  `count = 0`; the out-of-scope `Variable` and both `File` nodes survive
  (`count = 1`). Failing-then-green regression, proven on the base v1.1.11 image.
- **Throughput safety:** the retract path emits at most one bounded statement
  per semantic label (`len(semanticEntityPlans())`, ~11), issued once per delta
  generation per file-path set — not a high-cardinality hot path. The
  high-cardinality write path (per-label UNWIND upserts) is unchanged and still
  batches through `ExecuteGroup` (one atomic transaction), so no per-row write
  amplification or extra round-trip is introduced on the throughput-sensitive
  path. Sequential autocommit of a handful of retract statements is negligible
  against the corpus-scale upsert cost it sits beside.
- **No-backend guard:** `semantic_entity_retract_dispatch_test.go` proves the
  retract DELETEs route through `Execute` and the upserts through `ExecuteGroup`
  on a `GroupExecutor`-capable recorder, so a future regrouping regresses a
  normal CI run rather than only the DSN-gated live tier.

## No-Observability-Change:

No instruments, spans, logs, or status surfaces are added, removed, or renamed.
The change only reorders how existing statements are dispatched; per-statement
metadata summaries and `WrapRetryableNeo4jError` retry classification are
unchanged. Operator-facing telemetry for semantic entity writes is identical.
