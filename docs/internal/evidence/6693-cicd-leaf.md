# #6693 checklist step 2: `cicd/` leaf

Baseline: `origin/main` `874012542` (written against `aa7cc0d1d`, rebased twice without conflicts in the moved code). Change: moved the single-file `cicd/`
domain out of the `internal/storage/postgres` root per the target-tree
checklist in `docs/internal/design/6693-postgres-target-tree.md` and its
group file `docs/internal/design/6693-postgres-target-tree/control-plane.md`.

## What moved

- `git mv go/internal/storage/postgres/cicd_run_watermark.go
  go/internal/storage/postgres/cicd/watermark.go` (package `postgres` ->
  `cicdstore`).
- `git mv go/internal/storage/postgres/cicd_run_watermark_test.go
  go/internal/storage/postgres/cicd/watermark_test.go` (package `postgres`
  -> `cicdstore`, in-package test; it defines its own fake `ExecQueryer`
  and uses only exported symbols, so no root private symbol or
  `storage/postgres/fake` dependency was needed).
- Package clause `cicdstore` checked for collision first:
  `rg -n '^package cicdstore$' go/ --glob '*.go' -l` found nothing.

## What stayed in root, and why

`cicd_run_watermark_schema_test.go` (renamed nothing, still
`go/internal/storage/postgres/cicd_run_watermark_schema_test.go`) stays in
root. The mapping doc flagged it as spanning "root=75% cicd=25%" and asked
the executing PR to decide its form. It asserts against root's
`BootstrapDefinitions()` bootstrap registry (iterating root's
`Definition` list to find the `cicd_run_watermarks` migration), so moving it
to `cicd/` would require either an import of the parent `postgres` package
from a `cicdstore_test` external package, or reaching into the sibling
`migrations` package directly. The exact same shape of test already exists
for the landed `semantic/` leaf
(`TestBootstrapDefinitionsIncludeSemanticExtractionQueue` in
`go/internal/storage/postgres/semantic_extraction_queue_test.go`), and it
was kept in root rather than moved with `semantic/`'s production code (see
`go/internal/storage/postgres/semantic/AGENTS.md`'s "Read first" section,
which points at it living in root "beside the shared queue fakes and the
bootstrap registry they assert against"). Following that precedent exactly,
`cicd_run_watermark_schema_test.go` stays in root, `package postgres`,
updated only to import the new `cicd` package (referenced by its
`cicdstore` package clause, no alias, matching `semantic`'s convention) and
call `cicdstore.CICDRunWatermarkSchemaSQL()` instead of the former
in-package call.

## Callers repointed

- `go/cmd/collector-cicd-run/service.go`: added the
  `storage/postgres/cicd` import, changed
  `postgres.NewCICDRunWatermarkStore(database)` to
  `cicdstore.NewCICDRunWatermarkStore(database)`, and fixed the file-path
  comment above the call site. The `postgres` import stays: the file still
  calls `postgres.NewIngestionStore` and `postgres.NewWorkflowControlStore`.
- No other production callers exist: `rg -ln 'CICDRunWatermark'
  --glob '*.go'` across `go/` before the move returned only
  `cmd/collector-cicd-run/service.go` and the three moved/root test files.

## Symbols exported

None newly exported. `CICDRunWatermarkStore`, `NewCICDRunWatermarkStore`,
`CICDRunWatermarkSchemaSQL`, and `EnsureSchema` were already exported in
root; they kept their exact names, method sets, and semantics across the
move.

## Docs updated

- New doc trio: `go/internal/storage/postgres/cicd/doc.go`, `README.md`,
  `AGENTS.md`, modelled on `go/internal/storage/postgres/db/` (a comparably
  small single-purpose leaf).
- Root docs: removed the `CICDRunWatermarkStore` bullets from
  `go/internal/storage/postgres/README.md` (surface description,
  `Dependencies`, and `Telemetry` sections), `AGENTS.md` (the
  "CI/CD run watermark fencing (#5429)" invariant bullet), and
  `exported-surface-guide.md` (the "CI/CD run watermark (#5429)" entry) --
  the same clean-removal pattern the landed `semantic/` move used (verified
  by `rg -c -i SemanticExtractionQueueStore
  go/internal/storage/postgres/README.md` returning nothing before this
  change).
- Updated stale file-path references in
  `go/internal/collector/cicdrun/runwatermark/AGENTS.md`,
  `go/internal/collector/cicdrun/ghactionsruntime/README.md` (two spots),
  and `go/cmd/collector-cicd-run/README.md` (two spots, one also updating
  the `postgres.CICDRunWatermarkStore` mention to
  `cicdstore.CICDRunWatermarkStore`).

## dirgate

`bash scripts/verify-dirgate.sh --digest internal/storage/postgres` prints
`count 361` (was 362) and a new digest. The
`scripts/lib/dirgate-grandfather.tsv` row for `internal/storage/postgres`
was updated to `361` and the new digest, then
`scripts/generate-dirgate-grandfather-go.sh` regenerated
`tools/golangci-lint-dirgate/grandfather.go`. `cicd/` itself needs no
grandfather row (1 file, far under the 40-file cap); no other landed leaf
under this tree has one either.

## Verification (from `go/` unless noted)

- `gofumpt -l -w` on every changed file: no diffs reported.
- `go build ./...`: exit 0.
- `go vet ./...`: exit 0.
- `go vet -tags "integration perf5854_ack perf5740_completion perf6785_wait" ./internal/storage/postgres/...`: exit 0.
- `go test ./internal/storage/postgres/... -race -count=1`: all packages
  `ok`, including the new `cicd` package (1.139s) and root (17.951s).
- `go test ./cmd/collector-cicd-run/... -race -count=1`: `ok`.
- `go test -list '.*' ./internal/storage/postgres/cicd/` lists all 7 moved
  test names (`TestCICDRunWatermarkSchemaSQL`,
  `TestCICDRunWatermarkStoreSaveThenLoadRoundTrips`,
  `TestCICDRunWatermarkStoreLoadMissReturnsNotFound`,
  `TestCICDRunWatermarkStoreSaveRejectsOlderFence`,
  `TestCICDRunWatermarkStoreSaveRejectsInvalidWatermark`,
  `TestCICDRunWatermarkStoreLoadRejectsInvalidKey`,
  `TestCICDRunWatermarkStoreRequiresDatabase`).
- `go test ./internal/query -run QueryPlan -count=1`: `ok`; `rg -n cicd
  internal/query/queryplan_production_variants_test.go` found no pin on the
  moved file, so no pin refresh was needed.
- From repo root: `bash scripts/verify-dirgate.sh --all`: exit 0, no
  violations printed. `bash scripts/verify-package-docs.sh`: exit 0.
  `bash scripts/verify-performance-evidence.sh origin/main`: exit 0.
  `git diff --check`: exit 0.
- `rg -n 'cicd_run_watermark\.go|cicd_run_watermark_test\.go'` outside
  `docs/internal/design` and `docs/internal/evidence` returns nothing;
  `rg -n 'postgres\.NewCICDRunWatermarkStore|postgres\.CICDRunWatermarkStore'`
  across the repo returns nothing.

Test repoints, checked with exact-name assertions after the rebase onto
`874012542`: for each of the seven moved tests (`TestCICDRunWatermarkSchemaSQL`,
`TestCICDRunWatermarkStoreSaveThenLoadRoundTrips`,
`TestCICDRunWatermarkStoreLoadMissReturnsNotFound`,
`TestCICDRunWatermarkStoreSaveRejectsOlderFence`,
`TestCICDRunWatermarkStoreSaveRejectsInvalidWatermark`,
`TestCICDRunWatermarkStoreLoadRejectsInvalidKey`,
`TestCICDRunWatermarkStoreRequiresDatabase`),
`go test ./internal/storage/postgres/cicd/... -list '^Name$' -count=1 | rg -q '^Name$'`
exits 0, and the same assertion against `./internal/storage/postgres` exits 1.

No-Regression Evidence: this is a package-path move with no SQL, query,
lock, lease, batch, or concurrency change. `Save`'s
`fencing_token <= EXCLUDED.fencing_token` conflict guard and `Load`'s
absence of a generation/fencing predicate are byte-identical to the
pre-move source. All listed `go build`/`go vet`/`go test` commands above
are green on the moved and moving-adjacent packages; `go test
./internal/storage/postgres/... -race -count=1` is the same test count as
before the move (the same 7 tests moved to `cicd/`, 1 stayed in root),
confirming no test was dropped by the move.

No-Observability-Change: no metric, span, log key, status field, worker,
queue, lease, or retry was added, changed, or removed. `CICDRunWatermarkStore`
carried no telemetry of its own before the move and carries none after;
`ghactionsruntime`'s existing `eshu_dp_ci_cd_run_partial_generations_total`
and `ci_cd_run.observe` span instrumentation are untouched because only the
store's package path and import qualifier changed, not its call sites'
behavior.
