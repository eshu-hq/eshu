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
| `delta_scope.go` | Delta-generation file scopes, the unusable-scope reasons, and the closed `DeltaScopeUnusableReason` set (#10023) |
| `delta_partitions.go` | File-scoped delta partition derivation |
| `file_scope.go` | `BuildFileScopesByRepoID` (`FileScopeBuildResult`: scopes, full-refresh counts, `UnusableDeltaScopes`), `AcceptanceScanLimit` |
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

A repository whose repository fact carries `delta_generation` is on a delta
generation whether or not its scope is usable. A delta repository whose
changed paths cannot be qualified (no local path, an unnormalizable path,
or no changed path at all) is listed in `UnusableDeltaScopes` with a bounded
`DeltaScopeUnusableReason` and is absent from `ScopesByRepoID`. Before #10023
it was silently dropped, so `BuildRefreshIntentsWithDeltaFileScopes` gave it
the whole-repository refresh, whose retract rewrites the repository from the
delta's rows only. That builder still treats an unscoped repository as
whole-scope, so a caller MUST check `UnusableDeltaScopes` first:
`materialization.Handler` fails the work item closed there.

Reachability: the git collector emits the `code_call_materialization`
marker on delta generations (#10049), so every delta work item passes this
fence; its counter counts only the delta repositories the fence turns away.

Self-heal: the graph keeps its existing CALLS edges, and the delta's call
changes appear at the repository's next full generation. With reconciliation
on, the sweep forces one once the scope has gone
`ESHU_REPO_RECONCILE_INTERVAL_HOURS` (default 24) without a full observation.
With `ESHU_REPO_RECONCILE_INTERVAL_HOURS=0` there is no sweep, and the changes
wait for a reindex request or another full generation. No repair job exists,
because nothing was retracted. The code-call reducer owns this path.

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

No-Regression Evidence (#10023): the delta scope collector now also keeps
the delta repository set and the unusable-scope reasons, and
`materialization.Handler` builds the file scopes once, earlier (before the
symbol load) instead of inside the intent build. No query, queue, lease,
graph write, or concurrency path changes: the new check is one map lookup
per unusable delta repository, and a fenced work item writes nothing.

- Baseline: `origin/main` at `b16cb9689b`; after: this branch. Command, run
  in each tree with the same benchmark file:
  `go test ./internal/reducer/code/call/ -run '^$' -bench
  BenchmarkBuildFileScopesByRepoID -benchmem -count=6 -cpu 1`.
- Input shape: one delta repository with 2,000 changed paths plus one
  full-generation repository with 5,000 file facts (2 scopes built).
- Result: median 10,456,558 ns/op before and 10,613,462 ns/op after
  (+1.5%, inside run-to-run spread; the two six-run ranges overlap, 8.5 to
  11.5 ms and 8.1 to 13.5 ms). Allocations 14,138 to 14,140 allocs/op and
  1,877,555 to 1,877,811 B/op, which are deterministic.
- Backend/version: go1.26.10 linux/amd64, Intel i7-8700K, a shared host
  (hence the spread); in-memory test backend, no end-to-end reducer run.
- Terminal counts: the fenced handler writes 0 shared intents
  (`TestCodeCallMaterializationHandlerFailsClosedOnUnusableDeltaScope`), and
  the live queue proof
  (`TestCodeCallDeltaScopeUnusableDeadLettersOnFirstAttemptLive`) ends with
  the work item at `dead_letter`, attempt 1, no re-claim.
- Why safe: the usable-delta and full-generation paths build the same
  scopes as before (`TestCodeCallMaterializationHandlerAlignsDeltaEdgePartitions`,
  `TestBuildRefreshIntentsKeepsWholeScopeForUnscopedFullGeneration`, and the
  existing file-scope tests pass unchanged).

Observability Evidence (#10023): a new counter,
`eshu_dp_code_call_delta_scope_unusable_total{reason}`, one ERROR log,
`code call delta scope unusable, failing closed`, and the dead-letter
`failure_class=code_call_delta_scope_unusable`. Operator guide:
[Shared-Write Operations](../../../../../docs/public/reference/telemetry/shared-write-operations.md#code-call-unusable-delta-scopes).
