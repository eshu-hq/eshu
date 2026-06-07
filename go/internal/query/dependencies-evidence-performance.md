# Dependency Inventory Read Performance

This note records the query-shape contract and measured proof for
`GET /api/v0/dependencies` (issue #1646).

## Query shape

Two bounded, anchored traversals over the package-native dependency chain
`Package -[:HAS_VERSION]-> PackageVersion -[:DECLARES_DEPENDENCY]->
PackageDependency -[:DEPENDS_ON_PACKAGE]-> Package`:

- Forward (`direction=forward`): anchors on the indexed
  `Package {normalized_name}` (backed by the `package_normalized_name` index),
  filters ecosystem on the anchor, RETURNs narrow scalar properties directly,
  orders by `(d.dependency_normalized, d.uid)`, applies a two-part keyset cursor
  (`after_name`, `after_edge`), and `LIMIT $limit` last. Unanchored browse drops
  the `{normalized_name}` filter but keeps the same projection, order, and limit.
- Reverse (`direction=reverse`): anchors on the target `Package` and walks back
  to the declaring `PackageVersion`. Declaring packages are not always
  materialized as `Package` nodes, so the dependent identity is reported from the
  version (`package_id`, `name`, `version`), keyed on `(v.package_id, d.uid)`.

Both shapes avoid computed `WITH ... AS x` re-projection: on NornicDB the Bolt /
HTTP path returns such carried-through expression aliases as their literal alias
text instead of the evaluated value, so all returned columns are direct node
properties or `coalesce(...)`/literal expressions in the RETURN clause itself.

Input cardinality on the live remote-e2e corpus: 517 `Package`, 49
`PackageVersion`, 403 `PackageDependency`, 403 `DEPENDS_ON_PACKAGE`, 403
`DECLARES_DEPENDENCY`. Anchor fan-out is small (largest reverse anchor `tslib`
has 28 dependents).

No-Regression Evidence: new read-only endpoint; no existing query, write, queue,
reducer, or schema path changed, so the proof is a same-shape latency baseline on
the pinned NornicDB backend (Compose remote-e2e stack, `nornic` database). The
anchored forward Cypher (`@aws-sdk/client-kinesis`, `limit=51`) ran in 0.0226s /
0.0162s / 0.0033s over the Bolt-HTTP `tx/commit` endpoint; the reverse Cypher
(`tslib`, `limit=51`) ran in 0.0385s / 0.0117s / 0.0032s. End-to-end through the
API binary built from this branch: default forward `limit=5` returned HTTP 200 in
0.091s cold then 0.0068s warm; anchored forward `limit=3` in 0.0068s; reverse
`limit=3` in 0.0058s. Each page enforces `limit` (1..200, default 50), requests
`limit+1` to detect truncation, and returns `truncated` plus a `next_cursor`
keyset. The query context carries a 10s read budget.

Observability Evidence: the handler opens the `query.dependencies` span with
stable `http.route`, `eshu.capability`, and `eshu.dependency_direction`
attributes, records the `eshu_dp_dependency_list_duration_seconds` histogram
(labeled by bounded `direction`), and increments
`eshu_dp_dependency_list_errors_total` (labeled by `direction`) on graph-read
failure. Both instruments are registered in
`go/internal/telemetry/instruments.go` and asserted by
`TestInstrumentsRegistered`. The response envelope reports
`truth.level=exact`, `truth.basis=authoritative_graph`,
`truth.freshness.state=fresh`, plus `count`, `limit`, `truncated`, and
`next_cursor` for operator triage.

## Focused proof

`go test ./internal/query -run TestDependencies -count=1` and
`go test ./internal/telemetry -run TestInstrumentsRegistered -count=1`.
