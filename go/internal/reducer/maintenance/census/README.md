# Id-Anchor Census Runner

## Purpose

Samples the Neo4j id-anchor census (#7212) on an interval: the number of
graph nodes with an `id` that the labeled entity-context anchor cannot
reach. Zero is the invariant. The census is one read-only `AllNodesScan`
through the reducer's graph read port.

## Ownership boundary

Owns the pass loop, the per-pass deadline, and the gauge, counter,
histogram, and log recording. Does not own the census Cypher or the
anchor label sets (`internal/graph/anchor`), the Bolt read port
(`cmd/reducer`), or the `ESHU_ID_ANCHOR_CENSUS_*` configuration.

## Exported surface

- `Runner` — the pass loop (`Run`) and one pass (`RunOnce`); its `Source`
  is an `anchor.CensusSource`

See `doc.go` for the full contract.

## Dependencies

- `internal/graph/anchor` — the `CensusSource` port and `Census` result
- `internal/telemetry` — the census instruments and log attributes

Never `internal/reducer`.

## Telemetry

- Metrics: `eshu_dp_graph_id_anchor_unreachable_nodes` (gauge),
  `eshu_dp_graph_id_anchor_id_bearing_nodes` (gauge, same pass),
  `eshu_dp_graph_id_anchor_census_last_success_unixtime` (gauge),
  `eshu_dp_graph_id_anchor_census_passes_total{outcome}` (counter),
  `eshu_dp_graph_id_anchor_census_duration_seconds{outcome}` (histogram)
- Spans: none
- Logs: `id anchor census` per successful pass (`snapshot=true`, the
  counts, `first_pass`; WARN when the residual is nonzero) and
  `id anchor census pass failed` at ERROR with
  `failure_class=id_anchor_census_error`

## Gotchas / invariants

- The gauge and the last-success time move only on a successful pass.
- Each replica reports its own snapshot; alert on the maximum across
  replicas.

## Related docs

- `docs/public/reference/telemetry/id-anchor-census.md`
- `go/cmd/reducer/background-maintenance.md`
- `docs/internal/evidence/7212-id-anchor-census.md`
