# internal/reducer/code/semantic

Turns parser-emitted `content_entity` facts into canonical semantic-entity
graph nodes (Annotation, Typedef, TypeAlias, TypeAnnotation, Component,
Module, ImplBlock, Protocol, ProtocolImplementation, and the per-language
Variable/Function subsets that qualify) and writes them through the graph
backend.

This package moved out of the flat `internal/reducer` root under issue #6061,
and relocated from `internal/reducer/semanticentity` to
`internal/reducer/code/semantic` (package `semantic`) under the same issue.
It is a domain family: it owns one handler and the extraction pipeline behind
it, and nothing else in the reducer depends on its internals.

## Purpose

This package is the reducer-side half of semantic-entity materialization: it
decides which `content_entity` facts are semantic entities, shapes them into
canonical rows, and drives the canonical write (and delta-scoped retract)
through an `EntityWriter`. The canonical Cypher-backed writer that satisfies
that interface lives in `internal/storage/cypher` (`semantic_entity.go`), not
here.

## Ownership boundary

This package owns:

- deciding whether a `content_entity` fact qualifies as a semantic entity
  (`isSemanticEntityType` and its per-language helpers in
  `materialization_helpers.go`);
- shaping qualifying facts into canonical `EntityRow` values in a
  deterministic sort order;
- delta-scoping the write/retract to the changed and deleted files a delta
  generation reports (`delta_scope.go`);
- publishing the `semantic_nodes_committed` graph-projection phase after a
  successful write, with a durable repair enqueue on publish failure.

It does not own the canonical graph write itself (the `EntityWriter`
implementation in `internal/storage/cypher`) or the reducer's queue/worker
machinery that claims and retries the `semantic_entity_materialization`
domain.

## Exported surface

| symbol | file | what it does |
|---|---|---|
| `Handler` | `materialization.go` | the reducer handler the runtime registers for `semantic_entity_materialization`; its `GenerationCheck` field (`generation_gates.go`) fences superseded generations pre-write and pre-failure |
| `EntityWriter` | `materialization.go` | the canonical graph-write sink the handler writes through |
| `EntityRow` | `materialization.go` | one canonical semantic-entity row |
| `EntityWrite` / `EntityWriteResult` | `materialization.go` | the write request/outcome shape the handler and writer exchange |
| `ExtractEntityRows` | `materialization.go` | extracts every repo's semantic rows from a generation's facts |
| `ExtractEntityRowsForRepo` | `materialization.go` | the same extraction, filtered to one repo acceptance unit |
| `GraphProjectionPhaseRepairQueue` / `GraphProjectionPhaseRepair` | `graph_ports.go` | local structural port for the durable repair queue, see below |

See `doc.go` for the godoc-rendered package contract.

## Dependencies

Imports point strictly downward. This package reaches `reducer/contract`
(aliased `reducercontract`), `reducer/factload`, `reducer/gpphase`,
`reducer/payloadcore`, `internal/facts` and `pkg/log`, and it never imports
the parent `internal/reducer` package. The dependency runs the other way: the
root's handler catalog (`defaults_domain_catalog.go`) constructs
`Handler` and wires its `FactLoader`, `Writer`, `PriorGenerationCheck`,
`GenerationCheck` (production: `postgres.NewGenerationFreshnessCheck`) and
`PhasePublisher` fields, plus `RepairQueue` when the
root repair queue is present (`defaults_domain_catalog.go:97-114`).

`GraphProjectionPhaseRepairQueue` and `GraphProjectionPhaseRepair` are
declared locally in `graph_ports.go` rather than reusing `gpphase`'s
`PhaseRepairQueue`/`PhaseRepair` directly (the retired root aliases lived in
the `compat_correlation.go` bucket until #6626 retired them): the concrete
repair queue this package's `RepairQueue` field is wired to at runtime is
drained by `intents/phase/repair/runner.go` and held by the reducer root
handlers (`workload_materialization_handler.go`,
`workload_materialization_repo_phase.go`) as `gpphase.PhaseRepairQueue`.
Unlike the codetaint ports this pattern follows, this interface's `Enqueue`
method takes a named struct parameter, and Go requires exact type identity
for that, not just a matching method set — a queue built against
`gpphase`'s `PhaseRepair` cannot satisfy
this package's `GraphProjectionPhaseRepairQueue` directly, even though every
field matches. The root wires it through
`semanticEntityRepairQueueAdapter`
(`internal/reducer/semantic_entity_repair_queue_adapter.go`), which converts
between the two named repair-row types field-by-field; only when
`handlers.GraphProjectionRepairQueue` is non-nil, to avoid handing the
handler a non-nil adapter interface wrapping a nil queue.
`graphProjectionPhaseRepairsFromStates` is a byte-for-byte copy of the root's
`GraphProjectionPhaseRepairsFromStates` body for the same reason
`GraphProjectionPhaseRepair` is declared locally.

A handful of one-line forwarders that lived alongside the family in the
former `semantic_entity_*.go` root files stayed in root instead of moving,
because other root families that have not moved out yet still call them by
their unqualified root spelling: `payloadMap`, `semanticPayloadString`,
`semanticPayloadStringSlice`, `semanticQualifyDeltaPath`,
`semanticDeltaPayloadBool`, `deltaScopeRepositorySet`, and
`applyRepoRefreshDeltaScope` now live in
the shared-payload-delta stanza of `compat_decode.go`. This package calls the
shared-tier functions they forward to directly instead of reaching back into
root for them. In practice that means `payloadcore` only: the two
`sharedintent` forwarders were cross-family helpers that merely lived in the
old `semantic_entity_delta_scope.go`, and this family's own logic never called
them, so `sharedintent` is not among this package's imports.

## Telemetry

This package registers no metric instrument of its own. The
`semantic_entity_materialization` domain runs as a standard reducer
execution covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`, under the `reducer.run` span. The
domain is an attribute on those metrics rather than a span of its own, and
the span carries no domain attribute either, so isolate this family through
the domain-tagged metrics and the structured log below rather than by
filtering traces.

`Handler.Handle` emits one "semantic entity
materialization completed" structured log per execution, carrying
`fact_count`, `repo_count`, `row_count`, `skip_retract`,
`delta_projection`, `delta_file_count`,
`rows_attempted_by_label` / `rows_matched_by_label` /
`unmatched_uid_count` (#8359), and the
`load_facts_duration_seconds` / `extract_duration_seconds` /
`retract_decision_duration_seconds` / `graph_write_duration_seconds` /
`phase_publish_duration_seconds` / `total_duration_seconds` per-stage
timings. When the writer reports admitted rows that matched no canonical
node identity, `Handle` additionally emits one "semantic entity rows matched
no canonical node" WARN carrying the exact `unmatched_uid_count` and the
first `unmatched_uids` (bounded at 10, with `unmatched_uids_truncated`
counting the rest) plus the same per-label counts. The WARN is
deliberately not an error: projector-to-reducer ordering is not proven
total, so a miss can be a race rather than a defect. The status summary
says "admitted N semantic entity rows (M matched)", never "materialized":
admitted rows are not materialized until their statements land.

When `GenerationCheck` is wired (#8611), `Handle` fences superseded
generations twice: after the retract decision but before any graph write,
and again before the identity-divergence failure. Each fence exit emits
one "semantic entity materialization skipped: generation superseded" Info
log carrying `scope_id`, `generation_id`, `domain`, and `phase`
(`pre_write`, no write ran; `post_write`, the write's `CanonicalWrites`
ride the superseded result). A fence lookup error propagates wrapped and
fail-closed — a pending generation (`GenerationNotYetActiveError`,
retryable) is about to activate, not superseded, so it must retry rather
than skip.

When the writer's `WithGenerationCheck` fence is armed (#8628, production
wiring via `postgres.NewGenerationFreshnessCheck`), the writer probes the
stamped write before every dispatched group — including the first and
retract-only writes, on both dispatch paths — and aborts a superseded run
with a partial `EntityWriteResult` plus `ErrGenerationSuperseded`, which
`Handle` maps to the `mid_write` superseded exit with the partial
`CanonicalWrites` and no phase publish, poison, or retry. A single
post-write probe after the completion log then feeds both branches:
fresh+unmatched keeps the #8587 poison-pill failure, fresh+matched
succeeds, stale exits `post_write` superseded, and a probe error fails
closed. The closed `phase` label set is `pre_write`/`mid_write`/`post_write`.

No-Regression Evidence: #6061 relocates this family from
`internal/reducer/semanticentity` to `internal/reducer/code/semantic`
without changing its behavior. Every hunk in the moved production files is
package-clause, identity, or import requalification: the exported
`SemanticEntity*` identifiers dropped that prefix per
`docs/internal/naming.md` (`SemanticEntityMaterializationHandler` ->
`Handler`, `SemanticEntityRow` -> `EntityRow`,
`SemanticEntityWrite`/`SemanticEntityWriteResult` ->
`EntityWrite`/`EntityWriteResult`, `SemanticEntityWriter` -> `EntityWriter`,
`ExtractSemanticEntityRows`/`ExtractSemanticEntityRowsForRepo` ->
`ExtractEntityRows`/`ExtractEntityRowsForRepo`). Every importer (`cmd/reducer`,
the reducer root, `internal/replay/costcounting`,
`internal/replay/offlinetier`, `internal/storage/cypher`) was updated to the
new import path and identifier spellings in the same commit; there is no
compat shim, because this family was already a separate package before the
move (a path/name relocation, not an extraction out of root). Wire strings —
the `semantic_entity_materialization` domain, the `semantic_nodes_committed`
phase key, entity keys, and the structured-log message and fields — are
byte-identical. The `GraphProjectionPhaseRepairQueue`/`GraphProjectionPhaseRepair`
port and the `graphProjectionPhaseRepairsFromStates` conversion stay declared
locally, unchanged, for the reason given under Dependencies above. Measured
from `go/`, with `GOROOT` unset: `go build ./...`, `go vet
./internal/reducer/... ./cmd/reducer/... ./internal/storage/...`, and `go
test ./internal/reducer/... ./cmd/reducer/... ./internal/storage/cypher/...
./internal/replay/... -count=1` all exited 0 on this branch. `git diff
--check` exited 0.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span,
or log field. This package registers no instrument; the reducer executions
that wrap it, the span over them, and the structured-log fields listed above
are the same before and after the move.

No-Regression Evidence: #8359 adds attempted-vs-matched reconciliation to
the semantic write path without changing any statement. The writer computes
per-label counts and the unmatched uid list in pure Go over admitted rows
(O(rows) map builds plus one blake2s derivation per MATCH-owned row that
the projector re-derives), then dispatches the byte-identical retract and
upsert statements it always did; no Cypher text, shape, batching,
transaction grouping, retry classification, or executor path changes. No new
backend read (the check compares against the production derivation, not the
graph), no new worker, queue domain, or runtime knob.
Proof: `go test ./internal/projector/canonical/... ./internal/reducer/code/semantic/... ./internal/storage/cypher/ ./internal/reducer/ -count=1`
all exit 0, including the seeded RED/GREEN telemetry tests (divergent uid
WARNs with counts, agreed uid stays silent) and the pre-existing handler,
delta, and writer suites unchanged.

Observability Evidence: #8359 adds operator signals without a new metric
instrument. The completion log gains `rows_attempted_by_label`,
`rows_matched_by_label`, and `unmatched_uid_count`; a new bounded WARN
names unmatched uids; the status summary stops calling admitted rows
"materialized". This package still registers no instrument: executions stay
covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds` under the `reducer.run` span, and the
new fields ride the existing structured completion log, so no dashboard or
alert rule needs a new series.

No-Regression Evidence: #8611 adds two in-process freshness probes and two
early returns to the semantic handler without changing any statement. The
fence reads generation activation state through the already-wired
`postgres.NewGenerationFreshnessCheck` (one indexed activation lookup per
probe, the same statement family the retract path already issues, and only
when `GenerationCheck` is set; nil keeps the legacy path byte-identical).
It emits no new Cypher, performs no new graph read, and changes no
batching, transaction grouping, retry classification, or executor path. Proof: `go test ./internal/reducer/code/semantic/ ./internal/reducer/ -count=1`
all exit 0, including the seven seeded RED/GREEN fence tests
(pre-write superseded exits with zero writes, current generation still
fails projection_bug, post-write superseded exits with CanonicalWrites,
not-yet-active stays retryable, lookup errors fail closed, retract-only
skips, nil check keeps legacy) and the catalog wiring pair, with the
pre-existing handler, delta, and writer suites unchanged.

Observability Evidence: #8611 adds one operator signal and no metric
instrument. The new "semantic entity materialization skipped: generation
superseded" Info log (scope_id, generation_id, domain, closed phase
pre_write/post_write) marks every fence exit; executions stay covered by
`eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds` under the `reducer.run` span, so no
dashboard or alert rule needs a new series. The new
`generation_gates.go` file shares the family's `No-Observability-Change`
row with `graph_ports.go` in
`docs/public/observability/telemetry-coverage.md` (one merged row: the
coverage file is grandfathered at its line cap and must not grow).

No-Regression Evidence: #8628 arms the writer-side generation fence without
changing any statement. The writer probes the stamped write's freshness
before every dispatched group (first, retract-only, and fallback dispatches
included) through the production `postgres.NewGenerationFreshnessCheck` the
handler already uses — one read-only primary-key lookup on
`scope_generations` per group, the same statement the #8611 probes issue.
Conflict domain: semantic entity repo node/edge rows across row-bounded
groups; contested resources are the fact_work_items intent row (claim/ack),
semantic graph nodes/edges by (repo_id, evidence_source), and phase-state
rows by (scope, generation). No new locks; worker, lease, batch, and retry
settings are unchanged. It emits no new Cypher, performs no new graph read,
and changes no batching, transaction grouping, retry classification, or
executor path: the dispatch loop is a straight move from `writer.go` to
`writer_fence.go`, and the handler's matched branch gains the post-write
probe the unmatched branch already ran. Proof: `go test
./internal/storage/cypher/semantic/entity/ ./internal/reducer/code/semantic/
./internal/reducer/ ./cmd/reducer/ -count=1` all exit 0, including the ten
seeded RED/GREEN writer fence tests (pre-first-group abort, mid-write
partial, fence-error wrap, unstamped fail-closed, nil-check legacy,
retract-only, fallback, empty-write skip, label-keyed partial count,
abort-then-converge), the five new handler tests (matched post-write
supersede RED on the pre-change tree, mid_write abort, error propagation,
write stamp, fresh rerun publish), and the both-branches wiring pair, with
the pre-existing suites unchanged.

Observability Evidence: #8628 extends the closed superseded-log phase set
with `mid_write` (writer fence abort, partial `CanonicalWrites` on the
result, no phase publish) and runs the post-write probe on matched writes
too, so a generation superseded after its write exits `post_write`
superseded instead of succeeding on stale truth. This package still
registers no instrument: executions stay covered by
`eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds` under the `reducer.run` span, the
`mid_write` exit rides the existing superseded Info log, and no dashboard
or alert rule needs a new series. The extended `generation_gates.go` keeps
its shared `No-Observability-Change` row with `graph_ports.go` in
`docs/public/observability/telemetry-coverage.md` (row text updated in
place; the coverage file is grandfathered at its line cap and must not
grow).

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a
  symbol the root defines, that symbol is either already a forwarder to a
  shared-tier package (call the shared-tier function directly) or genuinely
  root-owned logic shared with families that have not moved yet (declare a
  structurally identical local port in `graph_ports.go`, following the
  pattern `code/taint/graph_ports.go` established).
- **`payloadMap`, `semanticPayloadString`, `semanticPayloadStringSlice`,
  `semanticQualifyDeltaPath`, `semanticDeltaPayloadBool`,
  `deltaScopeRepositorySet`, and `applyRepoRefreshDeltaScope` are not here.**
  Their prefix looks like this family, but they are cross-family forwarders
  other root domains still call unqualified; they live in
  the shared-payload-delta stanza of `compat_decode.go`. Do not reintroduce a
  local copy — call the shared-tier function they forward to.
- **`GraphProjectionPhaseRepairQueue` here is narrower than `gpphase`'s.** It
  declares only `Enqueue`, the one method `Handler`
  calls, not `gpphase`'s full `Enqueue`/`ListDue`/`Delete`/`MarkFailed` set
  the repair runner needs. Narrowing the method set is not enough on its own:
  a wider implementation satisfies this interface only if its `Enqueue` takes
  `[]semantic.GraphProjectionPhaseRepair`. `gpphase`'s takes `gpphase`'s
  own struct, and Go requires exact type identity in a method signature, which
  is why `semanticEntityRepairQueueAdapter` exists. Do not delete it.
- **Delta scoping is per repository**, carried on each generation's
  repository fact (`delta_generation`, `delta_relative_paths`,
  `delta_deleted_relative_paths`). Never treat it as scope-wide.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `go/internal/reducer/code/README.md` — the `code/` namespace parent
- `docs/internal/design/package-restructure.md` — the #6061 restructure
- `docs/public/observability/telemetry-coverage.md` — the coverage rows for `semantic_entity_materialization`
