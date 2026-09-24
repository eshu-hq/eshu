# #6693 checklist step 12: `code/taint/` (2 files)

Baseline: rebased onto the step-11 `cloud/aws/` head; no sibling step touches
the files this step moves. Change: move `code_interproc_projected_edge_store.go` and
`code_taint_evidence_projected_node_store.go` (and their tests) to the new
non-root package `go/internal/storage/postgres/code/taint` (package clause
`taintstore`; no existing package of that name — `rg -n '^package taintstore$'
go/ --glob '*.go' -l` found none, so no collision with `internal/reducer/code/
taint`'s `taint` package clause).

## What moved

- `code_interproc_projected_edge_store.go` ->
  `code/taint/interprocedural_edge.go`.
- `code_taint_evidence_projected_node_store.go` ->
  `code/taint/projected_node.go`.
- `code_interproc_projected_edge_store_test.go` ->
  `code/taint/interprocedural_edge_test.go` (external test package
  `taintstore_test`, per the mapping's annotation: it imports root for
  `MigrationSQL`'s DDL-parity assertion).
- `code_taint_evidence_projected_node_store_test.go` ->
  `code/taint/projected_node_test.go` (external test package
  `taintstore_test`, same reason).

Both production files only used the standard library and
`internal/storage/postgres/db`; no root symbols and no root<->child import
cycle risk.

## Export

No new exports were needed: `CodeInterprocProjectedEdgeStore`,
`NewCodeInterprocProjectedEdgeStore`, `CodeInterprocProjectedEdgeSchemaSQL`,
`CodeTaintEvidenceProjectedNodeStore`, `NewCodeTaintEvidenceProjectedNodeStore`,
and `CodeTaintEvidenceProjectedNodeSchemaSQL` were already exported.

## Test-double fix: root-private helper used outside the moving files

Both moved test files used the root-private `recordingExecQueryer` (defined
in `projector_queue_heartbeat_test.go`, which stays in root and is still
used by many other root tests) to drive an `ExecQueryer` double. An external
test package cannot read a root-private symbol, so both moved test files now
use the shared `internal/storage/postgres/fake` package instead
(`fake.ExecQueryer`, `fake.Rows`), matching the `iac`/`incident` leaf
precedent. No assertions changed: `fake.ExecQueryer` records the same
query/exec calls and args, and staged `fake.Rows{}`/`fake.Rows{Data: ...}`
reproduce the same zero-row and single-boolean-row shapes the previous
root-private doubles served.

One of the moved test files (`code_interproc_projected_edge_store_test.go`)
also *defined* `ledgerHasRowsDB`/`ledgerHasRowsRows` — a second, more specific
root-private double used by both moved files AND by an unrelated root test,
`code_value_flow_backfill_state_store_test.go` (`TestCodeValueFlowBackfillStateStoreIsCompleteTrue`/
`...False`). Moving the file that defined it would have broken that unrelated
root test's compile. Since it is now that root test's only remaining consumer
in the root package, the type definition moved into
`code_value_flow_backfill_state_store_test.go` itself rather than the file
being moved out to `code/taint`; the two moved files use `fake.ExecQueryer`
instead, as above. This is a mechanical relocation of an unexported test
helper — no assertion or behavior changed in either file.

## Callers repointed

- `go/cmd/reducer/wiring_handlers.go`: added the
  `github.com/eshu-hq/eshu/go/internal/storage/postgres/code/taint` import
  (declared name `taintstore`, distinct from the file's existing
  `internal/reducer/code/taint` import, declared name `taint` — no alias
  needed) and requalified both `postgres.NewCode*` constructors to
  `taintstore.NewCode*`.
- `go/cmd/reducer/canonical_graph_writers.go`: same import addition and
  requalification, inside `seedReducerProjectedSourceLedgers`.
- `go/cmd/reducer/code_value_flow_stale_cleanup_wiring.go`: same import
  addition and requalification, inside `codeValueFlowStaleCleanupRunnerFor`.
  All three call sites assign the constructed store to an interface-typed
  field (`taint.InterprocProjectedEdgeLedger`, `taint.ProjectedNodeLedger`,
  and the `reducer.CodeValueFlowStaleCleanupRunner`'s `TaintLedger`/
  `InterprocLedger` fields), so no type assertion or interface satisfaction
  changed — only the constructor's package qualifier did.
- Comment-only path/identifier fixes (no behavior change):
  `go/cmd/reducer/main.go` (a doc comment naming
  `postgres.NewCodeInterprocProjectedEdgeStore`, now
  `taintstore.NewCodeInterprocProjectedEdgeStore`) and
  `go/internal/storage/postgres/projected_source_edge_store.go` (a doc
  comment naming `CodeInterprocProjectedEdgeStore`, now
  `taintstore.CodeInterprocProjectedEdgeStore`).
- `go/internal/storage/postgres/AGENTS.md`: requalified both symbol names in
  the `#4893` history entry with `taintstore.` and repointed its
  `No-Regression Evidence` test command from `go test
  ./internal/storage/postgres -run '...'` to `go test
  ./internal/storage/postgres/code/taint/... -count=1`.

## dirgate

Creating the `code/taint/` subpackage dropped `internal/storage/postgres`'s
non-test `.go` file count by 2 (2 files moved). Re-pinned via
`bash scripts/dev/precommit-go.sh dirgate-digest internal/storage/postgres`
and regenerated `tools/golangci-lint-dirgate/grandfather.go` with `bash
scripts/generate-dirgate-grandfather-go.sh`. The digest run additionally
printed pre-existing `naming_violation` rows for files this step does not
touch (`iac_reachability_materializer.go`, three `incident_freshness_*.go`
files, `incident_routing_evidence_loader.go`, `scope_quiescence.go`) — all
already-mapped, not-yet-moved root files from earlier plan sections; none is
new and none is this step's concern.

## Verification (from `go/` unless noted)

- `gofumpt -l -w` on every changed file: exit 0, no files listed (already
  formatted).
- `go build ./...`: exit 0.
- `go vet ./...`: exit 0.
- `go vet -tags "integration perf5854_ack perf5740_completion perf6785_wait" ./internal/storage/postgres/...`: exit 0.
- `go test ./internal/storage/postgres/... -race -count=1`: exit 0, all
  packages pass including the new `.../postgres/code/taint`.
- `go test ./cmd/reducer/... -count=1`: exit 0.
- `go test -list '.*' ./internal/storage/postgres/code/taint/`: lists all 22
  moved test names.
- Exact-name repoint proof for all 22 names (both `CodeInterprocProjectedEdge*`
  and `CodeTaintEvidenceProjectedNode*`), the real names substituted for each
  run, for example:
  `go test ./internal/storage/postgres/code/taint/... -list
  '^TestCodeInterprocProjectedEdgeStoreSchemaSQL$' -count=1 | rg -q
  '^TestCodeInterprocProjectedEdgeStoreSchemaSQL$'` exits 0 against the new
  package and the same command against `./internal/storage/postgres` exits 1
  (test no longer registered at root), repeated for all 22 names: 0 failures
  either direction.
- From the repo root: `bash scripts/verify-dirgate.sh --all`: exit 0, no
  output. `bash scripts/verify-package-docs.sh`: exit 0, "changed Go package
  docs present". `bash scripts/verify-performance-evidence.sh origin/main`:
  exit 0, "benchmark and observability markers found for hot-path changes".
  `bash scripts/verify-moved-file-refs.sh`: exit 0, no dangling
  references. `bash scripts/verify-doc-citations.sh`: exit 0, "257 test
  citation(s) checked (0 baselined dead), 289 fixture citation(s) checked (11
  baselined unresolved), 435 raw line citation occurrence(s) tracked".
  `git diff --check`: exit 0, no output.
- `rg` for `code_interproc_projected_edge_store`,
  `code_taint_evidence_projected_node_store` (old filenames, without
  extension so both the `.go` and `_test.go` forms match) across the repo,
  excluding `docs/internal/design` and `docs/internal/evidence`: no hits.

No-Regression Evidence: the DDL, batching, in-batch dedupe, enumeration query
shapes, and prune predicates in both stores are byte-identical to the
pre-move code; only paths, the package clause, and constructor call-site
qualifiers changed. `go test ./internal/storage/postgres/... -race -count=1`
and the `cmd/reducer` package tests above are green on the moved tree.

No-Observability-Change: no metric, span, log key, or status field was added,
removed, or renamed; both stores still execute the same bounded, parameterized
SQL through the same injected `db.ExecQueryer`.
