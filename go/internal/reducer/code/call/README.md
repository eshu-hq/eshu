# call

The code-call dispatcher (issue #6061, moved under #6609; nested by language
under #6609's follow-up decision). Imported by the reducer root as `codecall`.

## Ownership

This package turns parser `file` and `repository` facts into code-call rows:
it resolves each call site to a caller and callee code entity (dispatching to
one of 14 per-language resolver leaves, the shared `EntityIndex`, import and
re-export resolution, Python metaclass edges via `code/call/python`) and
builds the `code_calls` shared-intent rows plus the per-repo refresh intents
that own the repo-wide CALLS retract. It writes nothing itself.
`materialization.Handler` persists the rows through its
`materialization.IntentWriter`; the code-call projection runner drains them
into graph edges.

## Files

11 non-test files. The entity-index substrate and generic candidate-name/path
helpers live in `code/call/shared`; each parser language's resolver lives in
its own leaf under `code/call` (`golang`, `java`, `jvm`, `kotlin`, `groovy`,
`javascript`, `typescript`, `python`, `dart`, `elixir`, `haskell`, `perl`,
`rust`, `swift`).

| File | Covers |
| --- | --- |
| `extract.go` | `ExtractRows`, `ExtractAllRelationshipRows`, `ExtractAllRelationshipRowsWithIndex`: codegraph decode, quarantine, row extraction |
| `rows.go` | Per-fact row building: SCIP rows, generic rows, same-file scoped resolution |
| `resolution.go` | `resolveGenericCallee`: the ordered generic dispatch |
| `languages.go` | `codeCallLanguageResolvers` explicit wiring, the repo-fallback-blocking switch |
| `relationship.go` | Relationship-type classification, row dedup keys |
| `instantiates.go` | `INSTANTIATES` edge emission |
| `intents.go` | `BuildSharedIntentRows`, `BuildRefreshIntentsWithDeltaFileScopes`, partition keys, `RepoRefreshEvidenceSource` |
| `delta_scope.go` | Delta-generation file scopes, the delta repository set, and the closed `DeltaScopeUnusableReason` set (#10023) |
| `delta_partitions.go` | File-scoped delta partition derivation |
| `file_scope.go` | `BuildFileScopesByRepoID` (`FileScopeBuildResult`: scopes, full-refresh counts, `DeltaRepositoryIDs`, `UnusableDeltaScopes`), `AcceptanceScanLimit` |
| `doc.go` | Package contract |

## Seams the reducer root uses

- Extraction: `ExtractAllRelationshipRowsWithIndex`,
  `shared.ReferencedSymbolKeys` (via the `compat_projection.go` stanza).
- Intents: `BuildSharedIntentRows`, `BuildRefreshIntentsWithDeltaFileScopes`,
  `BuildFileScopesByRepoID`, `FileScopeBuildResult`, `DeltaFileScope`,
  `DeltaScopeUnusableReason`.
- Entity resolution for the symbol-runtime families:
  `shared.EntityIndex`, `shared.BuildEntityIndex`,
  `shared.ResolveContainingEntityID`, `shared.EndpointEntityType`,
  `shared.PathKeys`, `shared.PayloadInt` (all retargeted from `codecall.X` to
  `shared.X` in the root's compat stanza).
- Runner vocabulary (the root runner reads these through the compat stanza,
  or directly for `AcceptanceScanLimit`): `PartitionKeyVersion`,
  `PayloadBool`, `AcceptanceScanLimit`, `EvidenceSource`,
  `PythonMetaclassEvidenceSource`.
- Named only by root tests: `RefreshPartitionKey`,
  `RefreshPartitionKeyForDelta`, `WholeScopePartitionKey`,
  `RepoRefreshEvidenceSource`. `python.ExtractMetaclassRows` has no caller
  outside the code/call family; it was already exported before the move.

## Delta repositories with an unusable scope (#10023)

Membership in `FileScopeBuildResult.DeltaRepositoryIDs`, not the presence of a
scope, marks a repository as being on a delta generation. A delta repository
whose changed paths cannot be qualified (no local path, an unnormalizable path,
or no changed path at all) is listed in `UnusableDeltaScopes` with a bounded
`DeltaScopeUnusableReason` and is absent from `ScopesByRepoID`. Before #10023
it was silently dropped, so `BuildRefreshIntentsWithDeltaFileScopes` gave it
the whole-repository refresh, whose retract rewrites the repository from the
delta's rows only. That builder still treats an unscoped repository as
whole-scope, so a caller MUST check `UnusableDeltaScopes` first:
`materialization.Handler` fails the work item closed there.

Self-heal: the graph keeps its existing CALLS edges, and the delta's call
changes appear by the repository's next full generation (the reconciliation
sweep, `ESHU_REPO_RECONCILE_INTERVAL_HOURS`, default 24 hours). No repair job
exists, because nothing was retracted. The code-call reducer owns this path.

## Dependency rule

One-way imports only. From the reducer tree: `code/call/shared`, every
`code/call/<language>` leaf, and the shared tier (`contract`, `factload`,
`factdecode`, `schemadecode`, `sharedintent`, `payloadcore`). Outside it:
`facts`, `codeprovenance`, the SDK `factschema`, and the standard library.
Never the parent reducer package. No leaf imports this package back, and
`code/call/shared` imports no leaf.

## Evidence

No-Regression Evidence: #6626 batch 1 retires the root
`reducer.ExtractCodeCallRows` alias (a deleted `compat_projection.go` stanza)
and retargets its 7 callers (4 test files plus doc references) to
`codecall.ExtractRows`. No logic, query, queue, lease, or concurrency path
changes: the alias forwarded to this package's `ExtractRows`, so the call
graph is identical before and after.

- Baseline: `cb5fa4a515`, `go test -count=1 ./internal/reducer/
  ./internal/reducer/code/call/ ./internal/accuracygate/
  ./internal/resolutionparity/`: 4 ok, 0 fail.
- After: measurement commit `371b572375` (docs-only refresh on top),
  same command:
  4 ok, 0 fail (identical ok-package set; input shape is the unchanged
  test corpus, terminal row counts unchanged because no projection code
  moved).
- Backend/version: go1.26.9 linux/amd64, in-memory test backends.
- Telemetry/status evidence: no new metric, span, or log; the retired alias
  emitted none, and the golden call-graph tests plus the accuracy-golden gate
  still pass, which is the suite that would catch a dispatch change.
- Why safe: compiler-checked symbol retarget; the RED run (`go vet` failing
  on the unresolved alias before the retarget) proves the alias is gone, and
  the GREEN run proves every former caller resolves to the same function.

No-Observability-Change: this batch adds, removes, and renames no operator
signal; `verify-telemetry-coverage.sh` passes unchanged.
