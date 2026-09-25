# #6693 checklist step 17: `code/reachability/` leaf

Baseline: origin/main at the step-16 admission merge (rebased). Change: `git mv`
of the three non-test files plus the three mapped test files into
`go/internal/storage/postgres/code/reachability/` (`store.go`, `loader.go`,
`helpers.go`, `store_test.go`, `store_sql_shape_test.go`,
`store_route_liveness_live_test.go`), per the target-tree mapping in
`docs/internal/design/6693-postgres-target-tree/code.md`
(`code/reachability/` section). Package clause changed `postgres` ->
`reachabilitystore`; checked for collisions first (`rg -n
'^package reachabilitystore$' go/ --glob '*.go' -l` returns only the new
leaf files).

The non-test files are self-contained: `helpers.go` and `loader.go` import
only `internal/reducer/codeintel` (plus stdlib); `store.go` adds only
`internal/storage/postgres/db` (`ExecQueryer`). No non-test file imports the
parent `postgres` package: leaves under `storage/postgres` must not depend
on root, per the leaf `doc.go`.

## Test placement (mapping annotation "external test package + own export_test.go shim")

`store_test.go` stays in-package (`package reachabilitystore`).
`sharedIntentResult{}` (a root test-only `sql.Result` defined in
`shared_intents_test.go`, unimportable across the new package boundary) is
replaced with `fake.Result{}` from the D6
`internal/storage/postgres/fake` package -- identical behavior
(`LastInsertId` returns zero and no error, `RowsAffected` returns one and
no error), confirmed by reading both definitions. This is the same swap the
`iac/` leaf evidence note records for its own moved test.
`decodeStringArrayJSON` travels with the file, still duplicated in-file with
its provenance comment; no new copies were made.

`store_sql_shape_test.go` stays in-package and is byte-identical apart from
the package clause.

`store_route_liveness_live_test.go` becomes `package
reachabilitystore_test` (it was in-package `postgres` at base) and reaches
the private loader methods through the new `export_test.go` shim
(`LoadCodeReachabilityRubyClasses`, `LoadCodeReachabilityRoots`,
`LoadCodeReachabilityRailsRouteFacts`, each a method-expression alias taking
the store first). It imports root only for `ApplyBootstrap`/`SQLDB`. Its
DSN-open, suffix, and cleanup helpers are its own copies of
`code_reachability_upgrade_backfill_live_test.go`'s helpers (the file's
comment says so), because that file stays in root: Go test-only symbols do
not cross package boundaries.

`code_reachability_upgrade_backfill_live_test.go` stays in root per
`root.md`: it defines `testSuffix`, `openUpgradeBackfillLiveDB`, and
`registerUpgradeBackfillCleanup`, and `testSuffix` is also used by unrelated
root live tests (`recovery_refinalize_*`,
`reducer_queue_workload_replay_live_test.go`,
`repo_dependency_acceptance_gate_expiry_test.go`,
`recovery_claim_token_fence_live_test.go`) -- confirmed with `rg -ln
testSuffix go/internal/storage/postgres/`.

## Caller repoint

Three files outside the leaf used the moved exported symbols, all repointed
to the unaliased `reachabilitystore` import:

- `go/cmd/reducer/main_helpers.go` (`NewCodeReachabilityStore(database)`).
- `go/internal/query/downgraded_code_root_kinds_roundtrip_live_test.go`
  (`NewCodeReachabilityStore(storagepostgres.SQLDB{DB: db})`; the root
  `storagepostgres` import stays for `SQLDB`, which did not move).
- `go/internal/storage/postgres/code_reachability_upgrade_backfill_live_test.go`
  (stays-in-root file; uses `reachabilitystore.NewCodeReachabilityStore`
  and `reachabilitystore.CodeReachabilityVerdictSchemaEpoch`).

No other file under `go/` references the store type, schema constructor, or
epoch constant qualified through `postgres.` (checked with `rg`); the only
remaining unqualified mentions are historical prose (CHANGELOG, evidence
narratives about the bump event) that name the symbol, not its home.

## Tallies, cross-references, and dirgate

- `code.md`: header now reads 3 non-test plus 3 test; the
  route-liveness annotation records the shim plus the duplicated-helper
  rationale, and the stays-in-root paragraph names the `testSuffix`
  sharers. `root.md`: header now reads 4 non-test plus 115 test with the
  stays line. The main checklist (`6693-postgres-target-tree.md`) marks
  step 17 done; its form table moves the staying file from the external
  bucket to the stays-in-root bucket (the mapping line's annotation
  bucket: the file stays in root), and both per-destination rows agree
  with `code.md`/`root.md`.
- `docs/internal/evidence/6693-iac-leaf.md`: reworded the two pointers that
  named the old root basename `code_reachability_test.go` to the leaf's
  new `store_test.go` location. No iac claim changed; the duplication
  arrangement that note describes is otherwise untouched.
- `go/internal/reducer/evidence-5500-lexical-scope-restriction.md`: the
  `loader.go` citation now carries the `go/` prefix matching that file's
  convention (one-word fix on top of the WIP repoint).
- `specs/live-tests.v1.yaml`: the route-liveness entry points at the new
  path. `scripts/lib/dirgate-grandfather.tsv` and
  `tools/golangci-lint-dirgate/grandfather.go` are re-pinned to what
  `bash scripts/verify-dirgate.sh --digest internal/storage/postgres`
  prints for this tree.

## Test-repoint proof (exact names, both packages)

`go test ./internal/storage/postgres/code/reachability/ -list '.*'`
lists the moved tests under the new package (upsert batching, repository
replace and watermark, active-generation lookup, schema SQL and verdict
epoch, loader query shapes, and both route-liveness live proofs), while
`-list` for the moved names against `internal/storage/postgres` prints no
test names (all gone from root; only the staying upgrade-backfill tests
remain there).

## Verification runs

From `go/` with `GOTOOLCHAIN=go1.26.6`: `go build ./...` exits zero; `go
vet` on the leaf, the parent package, `cmd/reducer`, and `internal/query`
exits zero; `go test
./internal/storage/postgres/code/reachability/...
./internal/storage/postgres/ -count=1` passes for both packages; `go test
./cmd/reducer/ -count=1` (repointed caller) passes; the filtered live
proofs (`TestCodeReachabilityRailsRouteFactsLoaderRoundTrip`,
`TestCodeReachabilityUpgradeBackfill*`,
`TestDowngradedCodeRootKindsRoundTripLive`) each report RUN lines and SKIP
without `ESHU_POSTGRES_DSN`, as designed. From the repo root:
`scripts/verify-dirgate.sh --digest internal/storage/postgres` matches the
ledger row; `git diff --check` is clean; `gofumpt -l` on every touched Go
file reports nothing; `scripts/test-verify-performance-evidence.sh` and
`scripts/verify-performance-evidence.sh` both exit zero.

No-Regression Evidence: focused package tests and every repointed caller suite pass on the moved tree, with live proofs skipping cleanly where no database is configured.
No-Observability-Change: package move only, with no metric, span, log field, worker, queue, lease, retry, or runtime-knob edit anywhere in the diff.
