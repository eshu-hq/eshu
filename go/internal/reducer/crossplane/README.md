# internal/reducer/crossplane

## Purpose

Materializes the `crossplane_satisfied_by_materialization` reducer domain
(issue #5347): Crossplane Claim -> XRD classification decisions projected
into canonical `SATISFIED_BY` graph edges between a `K8sResource` node (the
Claim) and the `CrossplaneXRD` node it resolved against. A Claim is never
parser-labeled — it is a generic `K8sResource` content-entity row that this
package classifies by resolving `(group, kind)` — derived from
`api_version`/`kind` — against exactly one XRD's `(spec.group,
spec.claimNames.kind)`.

The join is cross-scope: a platform repo's XRDs are joined against Claims in
app repos through `ListActiveCrossplaneXRDFacts`, mirroring
`kubernetesCorrelationSourceFactLoader`'s cross-scope OCI source join (issue
#388 PR3). Issue #5476 adds the cross-scope redrive path: when an app repo's
Claim projects before its XRD repo, the handler resolves zero edges on that
pass, and a later redrive sweep (`internal/storage/postgres`) re-enqueues the
same intent once the XRD activates.

## Ownership boundary

**Owns:** `CrossplaneSatisfiedByMaterializationHandler` (the reducer intent
handler), its loader/writer ports, the row-extraction/resolution logic
(`ExtractCrossplaneSatisfiedByEdgeRows`), and the post-write edge-existence
confirmation that gates the redrive ledger write.

**Does not own:** the Cypher `MATCH-MATCH-MERGE` write itself
(`cypher.CrossplaneSatisfiedByEdgeWriter`,
`internal/storage/cypher/crossplane_satisfied_by_edge_writer.go`), the
durable redrive ledger and cross-scope redrive sweep
(`internal/storage/postgres/crossplane_satisfied_by_redrive_*.go`), or the
projector-side intent trigger that enqueues this domain's intent
(`internal/projector/crossplanesatisfiedby`).

## Exported surface

| symbol | what it is |
|---|---|
| `CrossplaneSatisfiedByMaterializationHandler` | the reducer intent handler |
| `CrossplaneSatisfiedByEdgeWriter` | the graph write/retract port |
| `CrossplaneRedriveTargetLedgerWriter` | the cross-scope redrive-ledger write port (issue #5476) |
| `ExtractCrossplaneSatisfiedByEdgeRows` | pure in-memory hash-join row extraction, O(n) over candidates |
| `CrossplaneSatisfiedByMaterializationDomainDefinition()` | `DomainDefinition` constructor for root's additive-domain registration |
| `GraphQueryRunner` | locally-declared port (see Dependencies) |

The reducer root wires `CrossplaneHandlers` (`defaults_handlers.go`) to this
package's interfaces and constructs the handler in
`defaults_additive_domains_crossplane.go`. `cmd/reducer` constructs the
concrete Cypher edge writer and the Postgres-backed redrive ledger and
existence reader (`wiring_handlers.go`'s `buildReducerCrossplaneHandlers`).

## Dependencies

`internal/reducer/contract` (the `Intent`/`Result`/`Domain` shapes, aliased
`reducercontract`), `internal/reducer/factload` (`FactLoader`, fact-kind
constants, `LoadFactsForKinds`/`ClassifyFactLoadError`),
`internal/reducer/payloadcore` (`AnyToString`/`PayloadStr`), `internal/facts`,
`internal/graph/edgetype`, and `internal/telemetry`. No dependency on the
reducer root, and none of the root's other family subpackages.

`GraphQueryRunner` (the graph read port `EdgeExistenceReader` uses) is
**locally redeclared** in `graph_ports.go` rather than imported from the
reducer root: root's own `GraphQueryRunner`
(`infrastructure_platform_lookup.go`) is shared by several families that have
not moved out of root yet, so importing it would violate the "a family never
imports the reducer root" rule (issue #6061). Go interfaces are satisfied
structurally, so the same concrete graph-query implementation `cmd/reducer`
wires into other families' readers also satisfies this local declaration
with no logic duplicated.

## Telemetry

`eshu_dp_crossplane_satisfied_by_edges_total` (dimensioned by
`resolution_mode`), plus the standard `eshu_dp_reducer_executions_total` /
`eshu_dp_reducer_run_duration_seconds` for handler execution and
`eshu_dp_canonical_write_duration_seconds` for the graph write. The handler
also emits one structured "crossplane satisfied-by materialization
completed" log per intent. Unchanged by this move: same metric names, same
emission sites, only the package that owns the code moved. See
`docs/public/observability/telemetry-coverage.md`'s "Crossplane" rows.

## Gotchas / invariants

- **`GraphQueryRunner` is intentionally re-declared here, not imported.** Do
  not "fix" this by importing the reducer root — see Dependencies above. If a
  future move hoists it to a shared leaf package, replace the declaration
  with an import of that leaf, not with a root import.
- **The redrive ledger is written only after the edge write is confirmed
  present in the graph** (issue #5476 P1-b), never merely after
  `WriteCrossplaneSatisfiedByEdges` returns nil: the writer's
  `MATCH-MATCH-MERGE` deliberately no-ops (nil error, no edge) when an
  endpoint node is absent, so a nil write error alone does not prove a row's
  edge committed. Do not shortcut `recordRedriveLedgerForConfirmedEdges` to
  skip the existence-check read.
- **Ambiguous candidates (2+ matching XRDs) never fabricate a representative
  edge.** They are counted in the tally's `ambiguousSkipped` and produce no
  row. Do not "resolve" an ambiguity by picking the first match.
- **`ExtractCrossplaneSatisfiedByEdgeRows` dedupes XRD candidates by uid per
  join key** because the handler's `loadEdgeFacts` appends the cross-scope
  `ListActiveCrossplaneXRDFacts` load unconditionally to the own-scope
  content_entity facts, so a same-repo XRD can appear twice in `envelopes`.
  Do not remove the uid-dedup or a same-repo Claim/XRD pair will read as a
  false ambiguity (this exact regression is the B-7 golden-corpus rc-151
  assertion `crossplane_satisfied_by_edge_rows_test.go` guards).

## Related docs

- `go/internal/projector/crossplanesatisfiedby/README.md` — the projector-side intent trigger this package's handler consumes
- `go/internal/storage/cypher/crossplane_satisfied_by_edge_writer.go` — the concrete graph write this package's `CrossplaneSatisfiedByEdgeWriter` port wraps
- `go/internal/storage/postgres/README.md` — the cross-scope redrive ledger and sweep (issue #5476)
- `docs/public/observability/telemetry-coverage.md` — the coverage rows for this domain
- `docs/public/languages/crossplane.md` — the language-support entry for Crossplane Claims

No-Regression Evidence: #6061 moves the `crossplane_satisfied_by_materialization`
handler, its loader/writer ports, and the row-extraction/edge-existence
helpers out of the reducer root into this new package, without changing any
field, exported behavior, or call order. The one root-owned interface the
handler needs (`GraphQueryRunner`) is locally redeclared with the identical
method set rather than imported, so the existing concrete implementation
still satisfies it with no new indirection. Every outward caller —
`cmd/reducer` (`wiring_handlers.go`), `internal/reducer` root
(`defaults_handlers.go`, `defaults_additive_domains_crossplane.go`),
`internal/storage/postgres` (the redrive live tests), and
`internal/projector/crossplanesatisfiedby` (doc references) — was updated to
the qualified `crossplane.` symbol in the same commit. From `go/`, with
`GOROOT` unset and `GOCACHE` pointed at this worktree: `go build ./...`,
`go vet ./...`, and `go test ./internal/reducer/... ./cmd/reducer
./internal/storage/postgres ./internal/projector/... -count=1` each exited 0
on the branch. `git diff --check` exited 0. Binary output was not compared
and no such claim is made here.

No-Observability-Change: this move adds no queue domain, worker, lease,
graph or Postgres operation, runtime setting, metric instrument, metric
label, span, or status surface. The metric name, emission site, and
structured-log message listed under Telemetry above are unchanged; only the
package that owns the code moved.
