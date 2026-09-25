# #6693 checklist step 5: `iac/` leaf

Baseline: `origin/main` `d36974b92` (written against `aa7cc0d1d`, rebased after #7005, #7012 and #7017). Change: `git mv
iac_reachability.go
go/internal/storage/postgres/iac/reachability.go` and `git mv
iac_reachability_test.go
go/internal/storage/postgres/iac/reachability_test.go`, per the target-tree
mapping in `docs/internal/design/6693-postgres-target-tree/code.md`
(`iac/` section). Package clause changed `postgres` -> `iacstore` (the
`storage/postgres/semantic`/`scope` naming convention: plain directory word
plus `store`); checked for collisions first (`rg -n '^package iacstore$'
go/ --glob '*.go' -l`, no hits).

`iac_reachability.go` was self-contained: its only cross-file dependency is
`internal/storage/postgres/db` (`ExecQueryer`, `Rows`), and its two private
helpers (`appendPlaceholders`, `buildFamilyFilterClause`) are used only
within that file, so nothing needed exporting.

`iac_reachability_materializer.go` (declares methods on `IngestionStore`,
not `IaCReachabilityStore`) stays in root per the target-tree's membership
corrections table -- it belongs to `ingestion/` (checklist step 47) -- and
now imports the new `iac` package (`iacstore.NewIaCReachabilityStore`,
`iacstore.IaCReachabilityRow`, `iacstore.IaCReachability`,
`iacstore.IaCFinding`) unaliased, matching the `semantic`/`scope`
convention. This created a same-directory naming collision dirgate flags on
any root file whose name starts with a sibling subpackage's name
(`iac_reachability_materializer.go` starts with `iac_`); since the file's
real home is the later `ingestion/` step, it keeps its `package postgres`
line with a justified `//nolint:dirgate` marker naming the step and
destination, per the checklist's own escape-hatch rule.

## Test placement (mapping annotation "external test package: imports
ingestion")

The moved test file has three tests. Two (`TestIaCReachabilityStoreUpsertAndListCleanupFindings`,
`TestIaCReachabilitySchemaSQL`) exercise only `IaCReachabilityStore` via its
exported surface. The third (`TestIngestionStoreMaterializeIaCReachabilityWritesActiveCorpusRows`)
exercises `IngestionStore.MaterializeIaCReachability`, which stays in root
until the `ingestion/` move -- the mapping's "imports ingestion" names that
future package; today it means importing root's exported `NewIngestionStore`.
All three moved together as one file: `package iacstore_test`, importing
`github.com/eshu-hq/eshu/go/internal/storage/postgres` (root, for
`NewIngestionStore`) and `github.com/eshu-hq/eshu/go/internal/storage/postgres/iac`
(referenced as `iacstore`, matching its package clause). No `export_test.go`
shim was needed: every root/iacstore symbol the test touches is already
exported.

Two root-private test doubles could not cross the package boundary (Go test
files are not importable, exported or not):

- `sharedIntentResult{}` (defined in `shared_intents_test.go`, a trivial
  `LastInsertId() (0, nil)` / `RowsAffected() (1, nil)` `sql.Result`) is
  replaced with `fake.Result{}` from the D6 `internal/storage/postgres/fake`
  package -- identical behavior, confirmed by reading both definitions.
- Root's private `fakeExecQueryer`/`queueFakeRows`/`fakeExecCall` (used only
  by the third test) are replaced with `fake.ExecQueryer`/`fake.Rows`/
  `fake.ExecCall` per the checklist's own instruction. The swap is
  mechanical: `queryResponses: []queueFakeRows{{rows: ...}}` ->
  `QueryResponses: []fake.Rows{{Data: ...}}`, `database.execs` ->
  `database.Execs`, `execCall.query`/`.args` -> `execCall.Query`/`.Args`.
  `fake.LegacyQueueRowAdapter` was not needed: this test's rows are 3-column
  `(repo_id, ...)` shapes, not the 8/9- or 10-/11-column reducer-queue rows
  that adapter reshapes.

`decodeStringArrayJSON` was defined in the moved test file but also used by
the code-reachability store's test file (moved in a later #6693 leaf to
`internal/storage/postgres/code/reachability/store_test.go`). Since Go
cannot import one package's test files from another, the 7-line helper is
duplicated into that file with a comment noting the duplication's origin,
matching the precedent the `fake/` package's own README documents for
pre-D6 test doubles.

## Caller repoint

One caller outside `internal/storage/postgres` used the moved exported
symbols: `internal/query/iac/reachability_store.go` held `*postgres.IaCReachabilityStore`
and called `postgres.NewIaCReachabilityStore(postgres.SQLDB{DB: db})`. It now
imports `internal/storage/postgres/iac` (`iacstore`) alongside the existing
`internal/storage/postgres` import (still needed for `postgres.SQLDB`, which
did not move) and calls `iacstore.NewIaCReachabilityStore(postgres.SQLDB{DB: db})`.
`internal/query/iac` is a distinct package (different import path) from the
new `internal/storage/postgres/iac`; the shared "iac" directory basename is
not a Go collision.

No other file under `go/` referenced `IaCReachabilityStore`,
`NewIaCReachabilityStore`, `IaCReachabilityRow`, `IaCReachability`, or
`IaCFinding` qualified through `postgres.` (checked with `rg`); every other
match is either the new `iac/` files themselves, `iac_reachability_materializer.go`
in root, or unrelated identifiers (`IaCReachabilityMaterializationDuration`,
telemetry span/phase names, the local-supervisor finalizer) that never named
the store type.

## Doc trio and dirgate

Added `doc.go`, `README.md`, `AGENTS.md` for `iac/`, modeled on
`storage/postgres/semantic/`. Root's `doc.go`/`README.md`/`AGENTS.md`
mentioned neither the moved file names nor `IaCReachabilityStore` (checked
with `rg`), so the only root doc edit is `exported-surface-guide.md`, which now lists the moved symbols as `iacstore.*`.

One non-test file leaves root, so the `internal/storage/postgres` row in
`scripts/lib/dirgate-grandfather.tsv` is re-pinned to what
`bash scripts/verify-dirgate.sh --digest internal/storage/postgres` prints for
the rebased tree (each rebase onto a sibling move re-derives it), and
`bash scripts/generate-dirgate-grandfather-go.sh` regenerates
`tools/golangci-lint-dirgate/grandfather.go` from it.

## Test-repoint proof (exact names, both packages)

For each moved test, `for n in TestIaCReachabilityStoreUpsertAndListCleanupFindings TestIaCReachabilitySchemaSQL TestIngestionStoreMaterializeIaCReachabilityWritesActiveCorpusRows; do go test ./internal/storage/postgres/iac/... -list "^$n\$" -count=1 | rg -q "^$n\$" || echo "missing $n"; done` against the new
package exits 0 and matches; against `internal/storage/postgres` it exits 1
(name gone):

```
TestIaCReachabilityStoreUpsertAndListCleanupFindings:              new exit 0 / old exit 1
TestIaCReachabilitySchemaSQL:                                       new exit 0 / old exit 1
TestIngestionStoreMaterializeIaCReachabilityWritesActiveCorpusRows: new exit 0 / old exit 1
```

## No-Regression Evidence

`cd go && gofumpt -l` on every changed file reports nothing. `go build ./...`
and `go vet ./...` are clean across the whole module. `go vet -tags
"integration perf5854_ack perf5740_completion perf6785_wait"
./internal/storage/postgres/...` is clean. `go test
./internal/storage/postgres/... -race -count=1` passes for every
subpackage, including the new `iac/`. `go test ./internal/query/iac/...
-race -count=1` (the repointed caller) passes. `go test ./internal/query
-run QueryPlan -count=1` passes; `queryplan_production_variants_test.go`
pins no source hash for either moved file, so no pin refresh was needed.
`go test -list '.*' ./internal/storage/postgres/iac/` shows all three moved
tests. From the repo root: `bash scripts/verify-dirgate.sh --all` exits 0;
`bash scripts/verify-package-docs.sh` and `bash
scripts/verify-performance-evidence.sh origin/main` are clean after commit;
`git diff --check` is clean; `rg` for `iac_reachability.go` and
`iac_reachability_test.go` outside `docs/internal/design` and
`docs/internal/evidence` returns nothing (one hit, a provenance comment in
`internal/storage/postgres/code/reachability/store_test.go` (that leaf's
new location), was reworded to not spell the old filename).

## No-Observability-Change

No metric, span, log key, worker, queue, lease, retry, or durable write
shape changed. This is a path/package move plus the two mechanical fixture
substitutions above; `MaterializeIaCReachability`'s telemetry (duration
histogram, row counter, structured log) is untouched code, only its import
of the now-moved store type changed.
