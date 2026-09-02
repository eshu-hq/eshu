# inheritance

Turns parser-emitted type-hierarchy metadata into durable shared-projection
intents for the `INHERITS`, `IMPLEMENTS`, `OVERRIDES` and `ALIASES` edges.

This package moved out of the flat `internal/reducer` root under issue #6061. It
is a domain family: it owns one handler and the pipeline behind it, and nothing
else in the reducer depends on its internals.

## Exported surface

| symbol | file | what it does |
|---|---|---|
| `MaterializationHandler` | `materialization.go` | the reducer handler the runtime registers for `inheritance_materialization` |
| `IntentWriter` | `materialization.go` | the durable-intent sink the handler writes through |
| `EvidenceSource` | `materialization.go` | `reducer/inheritance`, the evidence source the refresh intent carries |
| `ExtractRows` | `materialization.go` | resolves declared bases, interfaces and trait adaptations into canonical edge rows |
| `DeltaScope` | `delta_scope.go` | which repositories are on a delta generation and which of their files changed |
| `BuildDeltaScope` | `delta_scope.go` | builds that scope from a generation's repository facts |
| `BuildRetractRows` | `delta_scope.go` | the retract scope rows: file-scoped under a delta, repo-wide otherwise |
| `BuildSharedIntentRows` | `intents.go` | promotes edge rows to durable intents (one refresh per repo, one write per edge) |
| `BuildRefreshIntents` | `intents.go` | just the per-repo refresh intents that own the retract |
| `WholeScopePartitionKey` | `intents.go` | the whole-scope key the refresh is emitted under and the fence reconstructs |
| `PartitionKeyVersion` | `intents.go` | namespaces the per-edge partition keys so a key-shape change can run alongside the old one |

`DeltaScope`, `BuildRetractRows`, `BuildRefreshIntents`, `BuildSharedIntentRows`,
`WholeScopePartitionKey` and `PartitionKeyVersion` are exported for the reducer
root's cross-family gates — the sibling delta gate, the retract-reachability
proof, and the partition-convergence gate — which drive every fenced
repo-wide-retract domain through its own builder rather than a shared mock.

## How an edge gets made

`ExtractRows` indexes a generation's `content_entity` facts by
`(repo_id, entity_name)` and resolves each declared base, implemented interface
and PHP trait adaptation against that index. Resolution is intra-repository name
matching only: a base naming no in-corpus entity yields no edge, and cross-repo
inheritance is out of scope. `IMPLEMENTS` additionally checks the resolved
parent's label, so implementing something that is not an `Interface` or
`Protocol` produces nothing.

`BuildSharedIntentRows` then emits **two** kinds of intent. Each repository gets
exactly one whole-scope refresh intent that owns the domain's single retract, and
each edge gets a write-only per-edge intent under a file-scoped partition key,
marked `retract_via_refresh` so the worker fences the write behind that refresh
(#2867/#2898). The per-edge key hashes the repo, the child path **and** the edge
identity, not the file alone: the partitioned runner deduplicates by
`(acceptance key, partition key)`, so two edges sharing one key would collapse and
one would be silently dropped.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/factload`, `reducer/payloadcore`, `reducer/sharedintent`,
`internal/codeprovenance`, `internal/facts` and `pkg/log`, and it never imports
the parent `internal/reducer` package. The dependency runs the other way: the
root's handler catalog constructs `MaterializationHandler` and its
shared-projection runner names `EvidenceSource`.

Relocating this family hoisted the root symbols that have consumers on both
sides of the new boundary into the shared tier they belong in, each leaving a
root forwarder or alias:

- `payloadcore` gained `SemanticPayloadMetadataString`,
  `SemanticPayloadMetadataStringSlice`, `DedupeNonEmptyStrings`,
  `DeltaPayloadBool` and `QualifyDeltaPath`;
- `sharedintent` gained `ProjectionContext`, `BuildProjectionContexts`, the
  repo-refresh vocabulary, `RepoWideRetractRefreshPartitionKey`,
  `DeltaScopeRepositorySet` and `ApplyRepoRefreshDeltaScope`;
- `contract` gained `MaterializationDiagnosticSignals` and its two SubSignals
  keys.

## Telemetry

This package registers no metric instrument of its own. The
`inheritance_materialization` domain runs as a standard reducer execution
covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`, under the
`reducer.inheritance_materialization` span; the edges it projects are written
through the shared edge-write path covered by
`eshu_dp_shared_edge_write_groups_total`.

What the family adds is diagnostic detail on the result and in the logs.
`Handle` emits per-phase wall-times through `Result.SubDurations`
(`load_facts`, `build_intents`, `upsert_intents`, `total`) and the
`input_ready` / `written_rows` signals through `Result.SubSignals`, plus an
`inheritance materialization fact inputs` log line carrying
`content_entity_facts` and `entities_with_declared_parent`. Those counts are
what separates an upstream ordering stall from genuinely empty work when the
rc-12 (`INHERITS`) gate goes intermittently red on loaded CI and does not
reproduce locally (#3873): a low `content_entity_facts` points at a partial
upstream fact set, while `entities_with_declared_parent > 0` paired with
`edge_count = 0` points at declared parents that resolved to no in-corpus
entity.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Almost every hunk inside the moved production files is
package-clause and import requalification: symbols the reducer root used to
supply as one-line forwarders are now imported from the leaf that already owned
them. The symbols the root still genuinely owned were hoisted to a shared tier
by moving the body, not copying it, and the root now forwards to that tier, so
each has exactly one implementation before and after. The only renames are
`ProjectionContext.acceptanceUnitID` to `ResolveAcceptanceUnitID` (a Go struct
cannot carry a field and a method under one name) and the family's own exported
identifiers, which lost their now-redundant `Inheritance` prefix. A Go import
change and a type alias add no indirection at runtime. Verified on this branch against
`630115dc5`, its merge-base with `origin/main`: `go build ./...` exits 0,
`go vet ./...` exits 0, and `go test ./internal/reducer/... ./internal/ifa/...
./internal/storage/cypher/... ./internal/replay/... -count=1` passes, including
this package. Binary output was not compared and no such claim is made here.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. This package registers no instrument; the reducer executions that
wrap it, the span over them, and every structured-log key listed above are the
same before and after the move.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a symbol
  the root defines, the symbol is in the wrong place: hoist it to a shared-core
  tier (`payloadcore` for generic helpers, `sharedintent` for intent-building
  shapes and vocabulary, `contract` for result vocabulary) and leave a root
  forwarder, rather than reaching upward.
- **The per-edge partition key must stay edge-unique, not file-unique.** It
  reads file-scoped and is anchored on the child path, but the edge identity is
  in the hash on purpose. Dropping it silently loses every edge but one per
  file.
- **`WholeScopePartitionKey` must equal what the #2898 fence reconstructs.** It
  delegates to `sharedintent.RepoWideRetractRefreshPartitionKey` instead of
  minting an inheritance-only key: a key the fence cannot rebuild makes it miss
  the refresh and defer every cross-partition edge forever.
- **`inheritanceEntityPathKey` is `relative_path`, and there is no `path`
  fallback.** A `content_entity` fact carries no top-level `path` key in any
  shape this repo emits, so reading `path` blanked the partition-key anchor and
  the `child_path` provenance field on every edge in production (#5996). A
  fallback here would be dead code masking a real ordering bug.
- **Go's structural `implements` is deliberately out of scope.** Only explicit
  keyword relationships produce an `IMPLEMENTS` edge (gap analysis #2228).

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
