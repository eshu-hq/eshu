# #6693: rename pgarray to array and rebuildreset to rebuild/reset

Baseline: `origin/main` `c3845859b`. Change: `git mv
go/internal/storage/postgres/pgarray go/internal/storage/postgres/array`
(package `pgarray` -> `array`) and `git mv
go/internal/storage/postgres/rebuildreset
go/internal/storage/postgres/rebuild/reset` (package `rebuildreset` ->
`reset`, nested one level under the new plain `rebuild/` directory), per the
target-tree checklist in `docs/internal/design/6693-postgres-target-tree.md`.
Both new names were checked for collisions (`rg '^package array$'`, `rg
'^package reset$'`) before adoption; neither exists elsewhere in `go/`.
Every importer (129 files) was repointed to the new import path and
qualifier. Two local identifiers that stuttered after the rename were
renamed instead of left glued: `internal/mcp`'s
`pgarrayArraySliceFactKinds`/`pgarrayArrayKinds` became
`arrayBoundSliceFactKinds`/`arrayBoundKinds`, and
`internal/storage/postgres/recovery.go`'s `rebuildresetQueryer` became
`resetQueryer` (a direct substring rename, since `rebuildreset` -> `reset`
already removes the stutter). No behavior, statement, query, lock, lease,
batch size, or worker count changed; this is a pure rename plus import
repoint. The root `internal/storage/postgres` directory's own file count is
unchanged (362), and the dirgate grandfather ledger required no edits: the
moved packages were never grandfathered rows.

No-Regression Evidence: `cd go && go build ./...` and `go vet ./...` are
clean across the whole module. `go test ./internal/storage/postgres/...
-count=1 -race` passes for every subpackage, including the renamed `array`
and `rebuild/reset` packages. `go test -list '.*'
./internal/storage/postgres/array/... ./internal/storage/postgres/rebuild/reset/...`
confirms every test name the move carried (9 in `array`, 8 in `rebuild/reset`)
is still discovered under its new package path, not silently dropped.
`gofumpt -l` on every changed Go file reports nothing after formatting the
files whose import block reordered under the new path names.

No-Observability-Change: no metric, span, log key, or status field is added,
removed, or renamed. The renamed packages carry no telemetry of their own;
callers' existing instrumentation is untouched because only the import path
and package qualifier changed, not call sites' behavior.
