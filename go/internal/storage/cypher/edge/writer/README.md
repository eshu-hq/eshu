# Edge writer leaf (storage/cypher/edge/writer)

Implements the canonical graph edge writes for every domain the reducer
projects: upsert on ingest, retract on scope narrowing. See `doc.go` for
the package contract.

## Ownership

- Owner: graph write path (`cmd/reducer` wires `NewEdgeWriter`).
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
  carries no class and counts.
- `eshu_dp_canonical_repository_stubs_created_total{writer}` counts the
  path-less `Repository` stubs the `repo_dependency` and `submodule_pin`
  upserts MERGE-create by id (#7446), from the backend's `NodesCreated`,
  once per committed execution unit (`repository_stubs.go`). The upserts keep
  MERGE-by-id; do not switch them to MATCH (see the #7324 design note).

## Change guidance

- Keep every Cypher template byte-identical when moving code; statement or
  predicate changes need their own issue with `EXPLAIN` and contention
  proof per `cypher-query-rigor`.
- Whole-scope narrowing must keep its single sanctioned call site
  (`retract_narrowing_test.go` guards this).
- Test fakes duplicated from `package cypher` (`bolt_harness_test.go`,
  `recording_executor_test.go`) are copies, not the source of truth;
  change behavior in both or reunite them when the canonical leaf moves.
