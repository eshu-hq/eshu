# 6060 nesting leaf 5: imports leaf (import dependencies)

## What moved

`go/internal/query/codequery/import_dependencies_execution.go`
shrunk to the `importDependencyRows` dispatcher forwarder, the
`uniqueImportDependencyScopes` forwarder, and the file header: the row
readers (`ImportRows`, `CycleRows`, `CrossModuleCalls`,
`ModuleScopes`), scope shaping (`UniqueScopes`, `ScopePaths`), and the
parameter builder (`Params`) live in `imports/rows.go` and
`imports/params.go` with the doc trio and leaf contract tests.
`import_dependencies.go` keeps the handler, the data dispatcher, the
capability, the sentinel, and the exported `ImportDependencyParams`
forwarder (the parent package's live grant proof calls it so the
statement under proof is the one the handler sends).

Deleted from codequery: the fourteen codemodel pass-through
forwarders (builders are called qualified from the leaf now),
`importDependencyRequest` (no code referenced it), and the
`fileImportCycleRows` / `importDependencyModuleScopes` forwarders
(their only caller was the `live_import_cycle_proof`-tagged live
test, updated to `imports.CycleRows`).

## Pin handling

- Four `query-source-coverage.yaml` hot pins repathed
  `import_dependencies_execution.go` →
  `imports/rows.go` with recomputed `source_sha256` from the repo
  go/parser extraction; `entry_ids`, `count`, `key_bound`, and
  `max_results` unchanged. Verified by
  `go test ./internal/queryplan/`.
- No `grandfathered_non_hot.go` entries name this family.

## No-Regression Evidence

- `go test ./internal/query/codequery/imports/ -count=1` — ok
  (5 tests, incl. dispatch through a stub graph).
- `go test ./internal/query/... -count=1` — 33 packages ok, no FAIL.
- `go vet` clean on the default build and on all eight live-tag
  variants (`live_import_cycle_proof`, `live_nornicdb_call_chain`,
  `live_story_property_proof`, `live_nornicdb_complexity_grant`,
  `live_nornicdb_relationship_story`,
  `live_nornicdb_dead_code_incoming`,
  `live_nornicdb_relationships_proof`,
  `call_graph_metrics_slo_live`).
- Scoped `precommit-go.sh lint` — 0 issues.

## No-Observability-Change

No telemetry, span, metric, or log line changed: the handler keeps
its span and attributes; the leaf adds no instrumentation.
