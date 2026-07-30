# Materialization Edge-Confidence Weights And Sub-Duration Telemetry Evidence (#3518, #3624)

Moved from `go/internal/reducer/README.md` (issue #5786) to keep the
package README under the repository's 500-line cap. Content is
unchanged from the original section.

## Materialization edge-confidence weights (#3518)

The settled confidence stamped onto materialized graph edges
(`PROVISIONS_PLATFORM`, runtime-services `DEPENDS_ON`, and the `RUNS_ON`
fallback) is sourced from named, documented constants in
`materialization_edge_confidence.go` instead of bare Cypher literals. Each
weight is injected into its UNWIND batch statement as the statement-scoped
`$edge_confidence` parameter via `executeBatchedWithParams`, so the graph-write
template carries no magic number and the value has a single documented home.
These weights are deliberately separate from the per-`EvidenceKind`
`DefaultConfidenceRegistry` priors: the registry feeds pre-admission
corroboration math, whereas these are the post-admission confidence written onto
an already-materialized edge.

No-Regression Evidence: this is a pure value-parameterization. Each bare literal
(`0.98` PROVISIONS_PLATFORM, `0.9` repo/workload `DEPENDS_ON`) is replaced by the
identical constant passed as a fixed statement-scoped scalar parameter; the
UNWIND batch shape, MERGE identity, anchors, predicates, and index usage are
unchanged, so the materialization hot path has no plan change and no measurable
regression. `go test ./internal/reducer -count=1` (2312 tests) stays green.

No-Observability-Change: no metrics, spans, or structured logs are added or
altered; only the in-query confidence literal is sourced from a documented
constant. Edge-property values written to the graph are byte-for-byte identical
to before.

## Materialization phase sub-duration telemetry (#3624)

The reducer result now carries `Result.SubDurations` (a `map[string]float64` of
per-phase handler timings) and the service log line emits them as
`sub_duration_<key>_seconds` attributes. This surfaces the materialization
long pole identified in #3624 — the `platform_graph` conflict domain shared by
`WorkloadMaterialization`, `DeploymentMapping`, `WorkloadIdentity`, and
`DeployableUnitCorrelation` serializes ~26k intents per scope — so an operator
(and the remote full-corpus e2e proof) can see which phase dominates. The
underlying serialization fix is tracked in #3672 (conflict-key partitioning);
this change is observability only.

Benchmark Evidence (Apple M5 Max, darwin/arm64, `workload_materialization_subduration_bench_test.go`):
building `SubDurations` is ~190 ns/op at 2 allocs/op; the incremental log-path
cost of the 7 added attributes is ~1.6 µs/op. Backend-agnostic (no graph write
changed). Input shape: a fully-populated workload-materialization timing struct.

No-Regression Evidence: this change adds no graph read/write and no new Cypher;
it only converts an already-computed internal timing struct into a result map
and log attributes. The materialization hot-path query shapes, batching, MERGE
identity, and conflict domains are unchanged, so there is no measurable
regression — `go test ./internal/reducer -count=1` (8950 tests) stays green.

Observability Evidence: adds per-phase `sub_duration_<key>_seconds` structured-log
attributes to the reducer result log line (via `recordReducerResult` in
`service.go` / `service_batch.go`), giving operators per-phase visibility into the
materialization drain so the long pole is diagnosable from logs at 3 AM.

## Sub-Duration Telemetry — Long-Pole Domains (issue #3624)

This section tracks the observability-only instrumentation added to the four
highest-pending materialization domains identified in issue #3624 (~26k total
intents, ~3,255 pending per domain). No graph write shapes, worker counts,
batch sizes, or conflict keys were changed.

### Two emission channels: durations vs. signals

Per-phase **wall-times** live in `Result.SubDurations` and are emitted by the
service layer (`recordReducerResult` in `service.go`) as
`sub_duration_<key>_seconds`. Non-duration **diagnostic signals** (counts and
flags) live in a separate `Result.SubSignals` map and are emitted as
`sub_signal_<key>` with **no** `_seconds` suffix. Splitting the channels keeps
the suffix honest: `written_rows=42` renders as `sub_signal_written_rows=42`,
never `sub_duration_written_rows_seconds=42` (which an operator could misread as
42 seconds).

The two signals are produced by a single shared helper,
`materializationDiagnosticSignals(inputReady bool, writtenRows int)` in
`materialization_diagnostics.go`, so every domain sets both keys with identical
encoding and the contract cannot drift.

- `input_ready` — **input presence**, not write count. `1.0` when the handler's
  upstream input was present; `0.0` on an ordering stall (the handler ran before
  its inputs existed). For the writer-based domains input presence means the
  request carried entity keys (the writer runs unconditionally, so zero writes
  is genuine empty work, NOT a stall). For the fact-loading domains it means a
  non-empty projection context was built from the loaded facts.
- `written_rows` — count of canonical writes (writer-based domains) or durable
  intent rows (intent-emitting domains) produced this run.

### Domains Instrumented

| Domain | Handler file | SubDurations (`sub_duration_*_seconds`) | input_ready (`sub_signal_input_ready`) |
|--------|-------------|------------------------------------------|-----------------------------------------|
| `deployment_mapping` | `platform_materialization.go` | `platform_write`, `load_facts`, `infra_extract`, `infra_graph_write`, `cross_repo_resolve`, `workload_replay`, `phase_publish`, `total` | 1.0 when request has entity keys; 0.0 only if absent (stall). Zero writes with input present = genuine empty work, not a stall. |
| `workload_identity` | `workload_identity.go` | `graph_write`, `phase_publish`, `total` | 1.0 when request has entity keys; 0.0 if absent. Zero writes with input present = genuine empty work. |
| `inheritance_materialization` | `inheritance_materialization.go` | `load_facts`, `build_intents`, `upsert_intents`, `total` | 1.0 when a projection context was built from facts (even if no inheritance entities → genuine empty); 0.0 only when no repo context found (ordering stall). |
| `code_call_materialization` | `code_call_materialization.go` | `load_facts`, `build_context`, `load_symbols`, `extract_rows`, `build_intents`, `upsert_intents`, `total` | 1.0 when a projection context was built; 0.0 when no context (ordering stall). |

All four domains emit `sub_signal_input_ready` and `sub_signal_written_rows`.

### Reachable diagnostic states

- **deployment_mapping / workload_identity** (writer-based): all three states
  reachable — work-happened (`input_ready=1, written_rows>0`), genuine-empty
  (`input_ready=1, written_rows=0`), stall (`input_ready=0`).
- **inheritance_materialization** (intent-emitting): all three states reachable.
  Genuine-empty (`input_ready=1, written_rows=0`) occurs when a projection
  context exists but the loaded facts carry no inheritance content entities, so
  `ExtractInheritanceRows` yields no repos and the handler returns at the
  context-present/no-entities branch before emitting any refresh intent (codex
  #3681). Work-happened (`input_ready=1, written_rows>=1`) and stall
  (`input_ready=0, written_rows=0`, no context) round out the three states.
- **code_call_materialization** (intent-emitting): a present projection context
  always emits ≥1 whole-scope refresh intent per repo, so genuine-empty
  (`input_ready=1, written_rows=0`) is **not reachable** through `Handle`.
  Reachable states are work-happened (`input_ready=1, written_rows>=1`, where a
  context with no code-call edges still yields one refresh intent) and stall
  (`input_ready=0, written_rows=0`, no context). The handler still passes
  `materializationDiagnosticSignals(true, 0)` on the defensive
  `len(intentRows)==0` branch so the signal stays correct if upstream behavior
  ever changes.

### Deferred Domains (follow-up PR)

The remaining long-pole domains from issue #3624 are deferred to a follow-up PR
to keep this change focused and reviewable:
- `sql_relationship_materialization`
- `shell_exec_materialization`
- `deployable_unit_correlation`
- `workload_materialization` (already has SubDurations; needs `SubSignals`
  input_ready + written_rows backfill)

### Operator Diagnostic Pattern

To diagnose an ordering stall vs. genuine empty work at 3 AM, read the
`reducer execution succeeded` log line for a domain:

```
sub_signal_input_ready=0                       → upstream data not ready (ordering stall)
sub_signal_input_ready=1 + sub_signal_written_rows=0 → input present, genuinely no rows (NOT a stall)
sub_signal_input_ready=1 + sub_signal_written_rows=N → normal work, N rows produced
sub_duration_load_facts_seconds=N              → fact load wall time (may indicate large fact set)
sub_duration_graph_write_seconds=N             → graph write wall time (may indicate backend contention)
sub_duration_phase_publish_seconds=N           → phase-gate publication cost
sub_duration_total_seconds=N                   → end-to-end handler wall time
```

All attributes appear on the `reducer execution succeeded` log line emitted by
`recordReducerResult` in `service.go`, alongside `handler_duration_seconds` and
`queue_wait_seconds`. Duration keys are `sub_duration_<key>_seconds`; signal
keys are `sub_signal_<key>` (no `_seconds`).

### Observability Evidence

New attributes added per domain (all emitted via `Result.SubDurations` /
`Result.SubSignals` → service layer log attributes — no new Cypher, no graph
writes changed):

- **deployment_mapping**: durations `sub_duration_{platform_write,load_facts,infra_extract,infra_graph_write,cross_repo_resolve,workload_replay,phase_publish,total}_seconds`; signals `sub_signal_input_ready`, `sub_signal_written_rows`
- **workload_identity**: durations `sub_duration_{graph_write,phase_publish,total}_seconds`; signals `sub_signal_input_ready`, `sub_signal_written_rows`
- **inheritance_materialization**: durations `sub_duration_{load_facts,build_intents,upsert_intents,total}_seconds`; signals `sub_signal_input_ready`, `sub_signal_written_rows`. An `inheritance materialization fact inputs` log line (emitted on every exit path, including the empty/stall early returns) carries `content_entity_facts` and `entities_with_declared_parent` so an intermittent rc-12 (`INHERITS`) gate flake is root-causable from logs (#3873): low `content_entity_facts` = partial upstream fact set; `entities_with_declared_parent > 0` with `edge_count = 0` = declared parents resolving to no in-corpus entity.
- **code_call_materialization**: durations `sub_duration_{load_facts,build_context,load_symbols,extract_rows,build_intents,upsert_intents,total}_seconds`; signals `sub_signal_input_ready`, `sub_signal_written_rows`

### No-Regression Evidence

Timing wrappers are `time.Now()` diffs around existing work — same pattern as
`workload_materialization_subduration_bench_test.go` which measured ~190 ns/op /
2 allocs/op for the map construction step. The added `SubSignals` map is one
allocation of two float64 entries per intent. No Cypher, no graph writes, no
worker counts, no batch sizes changed.

Full reducer test suite (`go test ./internal/reducer/... -count=1`) stays green.
TDD tests in `materialization_subduration_test.go` cover, per domain,
work-happened / genuine-empty (writer-based domains plus inheritance's
context-present/no-entities case) / stall, asserting `input_ready` and reading
`written_rows` from `SubSignals` (not `result.CanonicalWrites`) so a missing-key
defect fails red.
