# Edge writer leaf (storage/cypher/edge/writer)

Implements the canonical graph edge writes for every domain the reducer
projects: upsert on ingest, retract on scope narrowing. See `doc.go` for
the package contract.

## Ownership

- Owner: graph write path (`cmd/reducer` wires `NewEdgeWriter` and the
  standalone domain writers: code taint, code interproc, incident routing,
  Kubernetes correlation, observability coverage, and runtime declarations).
- `RuntimeDeclarationWriter` (#8816) MERGEs one `RuntimeDeclaration` node per
  declared runtime version on its five-part key and retracts them per
  repository (full generation) or per repository and file (delta). The
  path-scoped retract binds each path list to its own repository, because pin
  file paths repeat across repositories.
- Inputs: reducer projection rows, `sourcecypher.Executor` (group-capable).
- Outputs: Cypher `MERGE`/`MATCH+DELETE` batches against NornicDB/Neo4j.

## Dependencies

- Parent `cypher` package: `Statement`, `Executor`, statement templates,
  row builders, and payload accessors shared with the canonical writers.
- `internal/graph/edgetype`, `internal/reducer` (projection rows),
  `internal/telemetry` (`eshu_dp_shared_edge_*` instruments).
- Never imports the `edge/materialized` sibling.

## Telemetry

- `eshu_dp_shared_edge_write_groups_total`, group duration/statement-count
  histograms; probe and retract paths emit the same instruments the
  pre-split package emitted (see `telemetry_test.go`).
- `eshu_dp_shared_edge_target_miss_total{domain}` counts target-presence
  guard misses. The miss error carries the non-counting reducer failure
  class `shared_edge_target_not_ready`, so a `deployable_unit_correlation`
  intent defers without spending its retry budget; the reducer handler ends
  the wait after 30 minutes with a counting error (#7268). A probe fault
  carries no class and counts. The `shared edge batch target absent` WARN,
  the error text and `SampleTarget()` name the repository whose node the
  presence probe found absent (`sample_repo_id`), not the first row's; the
  sample intent id is still the batch head's (#11242).
- `eshu_dp_deployable_unit_edge_verify_total{outcome}` counts the #8283
  post-write verdicts of admitted `CORRELATES_DEPLOYABLE_UNIT` edges
  (`deployable_unit_verify.go`): `present`, `missing_target` (non-counting
  `shared_edge_target_not_ready`), `missing_edge` (counting
  `deployable_unit_edge_write_unverified`), `probe_error` (counting, no class).
  A miss logs a WARN with the verdict and sample ids.
- Code-call writes verify matched-vs-submitted counts atomically with the
  write (#10938): every code-call template reports
  `RETURN count(rel) AS matched` and `reconcileCodeCallMatchedCounts`
  (`code_call_matched.go`) reconciles before the caller completes anything.
  `eshu_dp_code_call_edge_submitted_rows_total` /
  `eshu_dp_code_call_edge_matched_rows_total` carry the verified counts
  (bounded `domain`); a shortfall on an enforced relationship parks the batch
  with the retryable non-counting `shared_edge_target_not_ready` class and
  counts on `eshu_dp_shared_edge_target_miss_total{domain=code_calls}`. An
  executor without the counted-write seam fails the batch closed with the
  retryable `codeCallCountingError`, never silently.
- `eshu_dp_canonical_repository_stubs_created_total{writer}` counts the
  path-less `Repository` stubs the `repo_dependency` and `submodule_pin`
  upserts MERGE-create by id (#7446), from the backend's `NodesCreated`,
  once per committed execution unit (`repository_stubs.go`). The upserts keep
  MERGE-by-id; do not switch them to MATCH (see the #7324 design note).

## Change guidance

- Keep every Cypher template byte-identical when moving code; statement or
  predicate changes need their own issue with `EXPLAIN` and contention
  proof per `cypher-query-rigor`. The one sanctioned template-shape change is
  the #10938 `RETURN count(rel) AS matched` tail on code-call templates: it
  rides the same `UNWIND` statement (statement count unchanged) so the write
  verifies itself atomically.
- Whole-scope narrowing must keep its single sanctioned call site
  (`retract_narrowing_test.go` guards this).
- Test fakes duplicated from `package cypher` (`bolt_harness_test.go`,
  `recording_executor_test.go`) are copies, not the source of truth;
  change behavior in both or reunite them when the canonical leaf moves.

## Target guard repository naming (#11242)

No-Regression Evidence: the pre-write target guard issues the same probe
statements as before, in the same order, over the same input rows. The change
wraps each statement with the repository of the node it anchors
(`targetProbe`), so no Cypher text, parameter, statement count, index use or
backend call changes; only the repository the miss error and WARN name
differs. Backend: none touched (the probe-shape tests
`TestEdgeWriterDeployableUnitProbeAnchorsBothRepos`,
`TestEdgeWriterHandlesRouteProbeAnchorsFunction` and
`TestEdgeWriterTargetMissProbeMentionsBatchScope` still pass unmodified apart
from the renamed builder). Input shape: one batch of up to the domain batch
size, at most two probes per row, deduplicated. The added cost is one string
field per probe, with no allocation beyond the existing probe slice.

No-Observability-Change: no metric, span or log key is added or removed.
`eshu_dp_shared_edge_target_miss_total{domain}` is counted as before and the
`shared edge batch target absent, deferring batch` WARN keeps its fields; its
`sample_repo_id` now names the repository whose node was absent instead of the
first row's repository, so an operator reading it at 3 AM sees the repository
the batch is actually waiting on.
