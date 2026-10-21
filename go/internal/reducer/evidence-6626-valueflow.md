# Evidence: retire value-flow/codecall compat aliases (#6626 value-flow lane)

## What changed

The last two live stanzas of `compat_projection.go` (the value-flow fixpoint
stanza from `value_flow_compat.go`, and the code-call family stanza from the
#6609 move) are deleted once their final callers moved. 31 exported aliases /
forwarders plus 4 unexported helpers, all `=` into their leaves:

- value-flow fixpoint -> `value.*`: `CodeValueFlowBackfillStateMarker`,
  `GraphValueFlowCloudSinkTargetLoader`,
  `ValueFlowCloudSinkWorkloadRowsCypher`,
  `ValueFlowCloudSinkTargetsByPairCypher`, `ValueFlowFixpointComponentStore`,
  `NewValueFlowFixpointCache`, `ValueFlowProgramInput`, `ValueFlowCallEdge`,
  `BuildValueFlowProgram`, `FunctionSummarySnapshotLoader`,
  `FunctionSourceSnapshotLoader`, `FunctionGraphIDSnapshotLoader`,
  `ValueFlowFixpointEvidenceLoader`, `ValueFlowFixpointEvidenceProjector`
- stale cleanup -> `cleanup.*`: `CodeValueFlowStaleCleanupRunner`,
  `CodeValueFlowStaleCleanupRunnerConfig`, `CodeValueFlowCurrentGeneration`,
  `CodeTaintStaleEvidenceRetractor`, `CodeInterprocStaleEvidenceRetractor`
- code-call projection -> `projection.*`: `CodeCallProjectionRunner`,
  `CodeCallProjectionRunnerConfig`, `DefaultCodeCallProjectionLeaseOwnerPrefix`,
  `DefaultCodeCallAcceptanceScanLimit`,
  `CodeCallProjectionFilePartitionKeyPrefix`,
  `CodeCallProjectionPartitionCandidateReader`,
  `CodeCallProjectionUnhashedCandidateReader`,
  `CodeCallProjectionRefreshFenceLookup`
- code-call materialization -> `materialization.*`:
  `CodeCallMaterializationHandler`, `CodeCallIntentWriter`,
  `ExtractSymbolRuntimeIntentRows`, `BuildHandlesRouteIntentRowsForQueryProof`
- helpers -> owning leaf: `buildCodeCallProjectionContexts` ->
  `schemadecode.BuildProjectionContexts`, `mapSlice` ->
  `payloadcore.MapSlice`, `pythonMetaclassEvidenceSource` ->
  `codecall.PythonMetaclassEvidenceSource`, `maxSharedSelectionScanLimit` ->
  literal `10_000` in the one root test that read it (the `worker` const is
  unexported; const aliases cannot cross packages to an unexported name).

Pre-census on `origin/main 7d6d375081` (`git grep`, so no #7750 pruning):

- External `reducer.X` caller files: 9 in `cmd/reducer`
  (stale-cleanup config/wiring, projection config, `main.go`,
  value-flow wiring + tests, lease-owner test), 2 in
  `internal/backendconformance`, 1 in `internal/ifa/materializededges`
  (`symbol_runtime_shared.go`), 1 in `internal/mcp`
  (`read_surface_query_proof.go`, Chain prose x8), 2 in
  `internal/query/codequery`, 8 in `internal/storage/postgres`
  (generation store, program loader, intent history, partition candidates,
  lease-heartbeat proof + tests). Union: 23 files.
- In-root users: `service.go` (field types only, names kept),
  `defaults.go` / `defaults_domain_catalog.go` (handler + writer wiring),
  `projected_source_edge_backfill.go`, `rationale_edge_materialization.go`
  (+ partition test), `sibling_edge_intent_delta_gate_test.go`,
  `code_import_repo_edge*.go`, `python_metaclass_materialization_test.go`,
  `shared_projection_worker_test.go`, plus root tests constructing
  `materialization.Handler` / `projection.Runner` / `cleanup.Runner`.
- Comment-only references repointed in the same edit: `familyodu`
  (`symbol_runtime_family_odu.go`, `shell_exec_family_odu.go`),
  `sql_relationship_odu.go`, ifa guard messages
  (`handles_route.go` / `runs_in.go` / `invokes_cloud_action.go`, matching the
  leaf-naming convention of the sibling guards), leaf `doc.go` files
  (`code/call`, `materialization`, `projection`, `value`, `cleanup`,
  `code/taint`), package `README.md` / `AGENTS.md` compat claims, the
  reducer `README.md` exported-surface bullet, `doc.go`, and the
  `telemetry-coverage.md` value-flow forwarder row.
- Left untouched: `Service.CodeCallProjectionRunner` /
  `Service.CodeValueFlowStaleCleanupRunner` field names (root-owned, kept per
  gpphase precedent), `DefaultHandlers.CodeCallIntentWriter` field name,
  historical evidence notes and dated design docs, and the already-stale
  `DefaultSharedProjectionLeaseOwnerPrefix` README half (tail bucket 2's
  scope; this lane only corrected its own code-call half).

Rewrite: qualifier swap `reducer.X` -> leaf over code spans, leaf imports
added where missing (`codecall` aliased where the package name is `call`),
now-unused `reducer` imports dropped. Hand work, all disclosed:

- `service.go` fields keep their names with repointed types
  (`*projection.Runner`, `*cleanup.Runner`).
- The metaclass test imports both `codecall` (aliased) and
  `materialization`.
- `shared_projection_worker_test.go` carries the literal `10_000` with a
  pointer at `worker/selection.go`.
- `compat_projection.go` keeps the retired ledgers (design-doc and telemetry
  rows name the file; precedent keeps ledgers in place) and is now
  import-free.
- New guard `TestValueflowFamilyRetired` in
  `compat_alias_retirement_valueflow_test.go` (bucket conventions) scans the
  whole reducer package directory for any re-declared alias.

## Why no regression

No-Regression Evidence: every alias is the identical canonical type or
function, so each repoint is spelling-only; no signature, envelope, row,
ordering, or string-value change (storage SQL and Cypher string values are
untouched — only Go identifiers). Proof on this head: RED build on the
deleted stanza (sibling delta-gate test + metaclass test failed to compile
until repointed during development); GREEN `go build` over
`internal/reducer/...`, `cmd/reducer/...`, `internal/storage/postgres/...`,
`internal/ifa/...`, `internal/query/...`, `internal/mcp/...`,
`internal/backendconformance/...`, `internal/exposure/...`; `go vet` exit 0
on the touched trees; gofmt clean over every touched file;
`git diff --check` clean; `go test -count=1` green for the full
`internal/reducer` package plus `cmd/reducer`, `storage/postgres`,
`backendconformance`, `ifa/...`, `query/codequery`, and `mcp` focused runs
(named runs confirm each touched test actually ran); guard RED (seeded
re-declaration fails) / GREEN (clean tree passes);
`ci-gates validate --drift` pass; `verify-telemetry-coverage.sh` pass
(registrations agree, no new untracked stages). Zero residual qualified
`reducer.X` for all 31 retired names outside the compat buckets, field
names, and historical evidence records. Live Docker/NornicDB/Postgres lanes
NOT_CHECKED (deferred to CI per lane instructions).

## Observability

No-Observability-Change: no new metric instrument, metric label, span,
structured log field, queue domain, worker, lease, or runtime knob.
Stale-cleanup keeps reporting through the existing runner spans and counters;
only the static type spelling at call sites changed.
