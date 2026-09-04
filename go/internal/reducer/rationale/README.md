# rationale

Turns parser-emitted intent-comment metadata (WHY/HACK/NOTE/TODO/FIXME) into
durable shared-projection intents for the `EXPLAINS` edge from an
identity-only `Rationale` node to the code entity the comment precedes (issue
#2230).

This package moved out of the flat `internal/reducer` root under issue #6061.
It is a domain family: it owns one handler and the pipeline behind it, and
nothing else in the reducer depends on its internals.

## Exported surface

| symbol | file | what it does |
|---|---|---|
| `MaterializationHandler` | `materialization.go` | the reducer handler the runtime registers for `rationale_materialization` |
| `IntentWriter` | `materialization.go` | the durable-intent sink the handler writes through |
| `EvidenceSource` | `materialization.go` | `reducer/rationale`, the evidence source the refresh intent carries |
| `ExtractRows` | `materialization.go` | builds EXPLAINS edge rows from parser-emitted rationale_comments metadata |
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

## Rationale EXPLAINS reconciliation

`MaterializationHandler` loads the admitted repository and `content_entity`
facts, extracts parser-emitted rationale comments, and emits one refresh
intent per repository plus one `EXPLAINS` intent per exact comment identity.
Edge payloads keep repo-relative target paths for partition identity; full
refreshes retract repo-wide, while delta refreshes use separately
repository-qualified changed and deleted paths. The unconditional Git
follow-up also runs zero-positive generations so stale rationale edges can
retract. See
[`docs/internal/evidence/5998-rationale-relative-path.md`](../../../../docs/internal/evidence/5998-rationale-relative-path.md)
for the fixture, benchmark boundary, and completed live proof on the supported backend.

## How an edge gets made

`ExtractRows` reads a generation's `content_entity` facts for parser-emitted
`rationale_comments` metadata (top-level, falling back to `entity_metadata`)
and produces one row per distinct `(entity, comment kind, comment text)`,
identified by a `rationale:<entity>:<kind>:<excerpt hash>` uid.

`BuildSharedIntentRows` then emits **two** kinds of intent. Each repository
gets exactly one whole-scope refresh intent that owns the domain's single
retract, and each edge gets a write-only per-edge intent under a file-scoped
partition key, marked `retract_via_refresh` so the worker fences the write
behind that refresh (#2869/#2898). The per-edge key hashes the repo, the
target entity's repo-relative path **and** the edge identity, not the file
alone: the partitioned runner deduplicates by `(acceptance key, partition
key)`, so two edges sharing one key would collapse and one would be silently
dropped.

## Package boundary

Imports point strictly downward. This package reaches `reducer/contract`,
`reducer/factload`, `reducer/payloadcore`, `reducer/sharedintent`,
`reducer/schemadecode`, `internal/facts` and `pkg/log`, and it never imports
the parent `internal/reducer` package. The dependency runs the other way: the
root's handler catalog constructs `MaterializationHandler` and its
shared-projection runner names `EvidenceSource`.

Relocating this family reached only leaves the inheritance move already
hoisted (`payloadcore`, `sharedintent`, `schemadecode`, `factload`); no new
root symbol needed hoisting for this move.

## Telemetry

This package registers no metric instrument of its own. The
`rationale_materialization` domain runs as a standard reducer execution
covered by `eshu_dp_reducer_executions_total` and
`eshu_dp_reducer_run_duration_seconds`, under the `reducer.run` span — the
domain is an attribute on those metrics, not a span of its own, and the span
carries no domain attribute either, so isolate this family through the
domain-tagged metrics and the structured logs rather than by filtering
traces; the edges it projects are written through the shared edge-write path
covered by `eshu_dp_shared_edge_write_groups_total`.

`MaterializationHandler.Handle` emits an "rationale materialization
started"/"rationale materialization completed" structured log pair carrying
`edge_count`, `repo_count` and `intent_count`.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Almost every hunk inside the moved production files is
package-clause and import requalification: symbols the reducer root used to
supply as one-line forwarders are now imported from the leaf that already
owned them (`payloadcore`, `sharedintent`, `schemadecode`, `factload`,
`contract`), all of which the earlier inheritance relocation (#6477) already
hoisted. No new symbol needed hoisting for this move. The only renames are the
family's own exported identifiers, which lost their now-redundant `Rationale`/
`RationaleEdge` prefix. A Go import change and a type alias add no indirection
at runtime. Verified on this branch against its current merge-base with
`origin/main`: `go build ./...` exits 0, `go vet ./...` exits 0, and
`go test ./internal/reducer/... ./internal/ifa/... ./internal/storage/cypher/...
-count=1` passes, including this package.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph or
Postgres operation, runtime setting, metric instrument, metric label, span, or
log field. This package registers no instrument; the reducer executions that
wrap it, the span over them, and every structured-log key listed above are the
same before and after the move.

## Gotchas / invariants

- **Do not import the reducer root from here.** If this package needs a symbol
  the root defines, the symbol is in the wrong place: it belongs in a
  shared-core tier (`payloadcore` for generic helpers, `sharedintent` for
  intent-building shapes and vocabulary, `schemadecode` for fact decoding,
  `contract` for result vocabulary), not reached upward.
- **The per-edge partition key must stay edge-unique, not file-unique.** It
  reads file-scoped and is anchored on the target path, but the edge identity
  (`rationale_uid->target_entity_id`) is in the hash on purpose. Dropping it
  silently loses every edge but one per file.
- **`WholeScopePartitionKey` must equal what the #2898 fence reconstructs.** It
  delegates to `sharedintent.RepoWideRetractRefreshPartitionKey` instead of
  minting a rationale-only key: a key the fence cannot rebuild makes it miss
  the refresh and defer every cross-partition edge forever.
- **`target_path` reads `relative_path`, and there is no `path` fallback.** A
  `content_entity` fact carries no top-level `path` key in any shape this repo
  emits, so reading `path` blanked the partition-key anchor and the
  `target_path` provenance field on every edge in production (#5998).

## Related docs

- [Reducer package](../README.md)
- [Package restructure design](../../../../docs/internal/design/package-restructure.md)
- [Telemetry coverage](../../../../docs/public/observability/telemetry-coverage.md)
