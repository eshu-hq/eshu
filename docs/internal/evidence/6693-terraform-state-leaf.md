# #6693 terraform/state/ leaf

Change: checklist step 9 of `docs/internal/design/6693-postgres-target-tree.md`.
It adds the `go/internal/storage/postgres/terraform/state` leaf (`package
statestore`) and moves `tfstate_status.go` -> `terraform/state/status.go`
(1 non-test file) plus `tfstate_status_test.go` -> `terraform/state/status_test.go`
(1 in-package test file), matching the mapping's `cloud.md` group entry with
no annotation change needed.

The mapping's "Prerequisite hoists" table also moves two SQL constants from
root's `status_queries.go` into this leaf, byte-identically, because
"Terraform-only SQL moves with its reader":

- `terraformStateLastSerialQuery`
- `terraformStateRecentWarningsQuery`

Both stay unexported (only the moved reader in the same package uses them).
The reader function and its return type are exported because the status
family (still in the postgres root until its own #6693 leaf, checklist step
50) reads through them, matching the `semanticstore.ReadSemanticExtractionObservability`
precedent:

- `readTerraformStateAdminEvidence` -> `statestore.ReadTerraformStateAdminEvidence`
- `terraformStateAdminEvidence` -> `statestore.TerraformStateAdminEvidence`
  (its `LastSerials`/`RecentWarnings` fields were already exported)

`listTerraformStateLastSerials` and `listTerraformStateRecentWarnings` stay
unexported; both are only called from the moved reader in the same package.

Callers repointed: `internal/storage/postgres/status.go` (root `StatusStore.
Snapshot`) now imports `github.com/eshu-hq/eshu/go/internal/storage/postgres/terraform/state`
without an alias and calls `statestore.ReadTerraformStateAdminEvidence`,
matching how the same function already calls `semanticstore.
ReadSemanticExtractionObservability` a few lines below it. No caller outside
`internal/storage/postgres` referenced any of the moved symbols. Root's
`doc.go` and `exported-surface-guide.md` had prose naming `tfstate_status.go`
directly; both now name `statestore.ReadTerraformStateAdminEvidence`
(`terraform/state/status.go`) instead.

The moved test used root-private `fakeQueryer`/`fakeRows` (a `db.Queryer`-only
double, not the queue-shaped `fakeExecQueryer` this repo already extracted).
Per the executor brief it was switched to the shared
`internal/storage/postgres/fake` double (`fake.ExecQueryer`/`fake.Rows`,
`QueryResponses`/`Data`/`Queries` fields). `fake.Rows.Scan`'s `*sql.NullTime`
case requires the staged row value to already be a `sql.NullTime` (unlike
root's `fakeRows.Scan`, which also accepted a bare `time.Time` and wrapped
it), so every staged `observedAt` column is now
`sql.NullTime{Time: ..., Valid: true}` instead of a bare `time.Time`; this is
a test-fixture-only change, not a change to the query, the scan target, or
the production row-assembly logic.

No caller outside `internal/storage/postgres` referenced either moved
symbol; `rg` across the repo for the old file name and symbol names finds
matches only inside the new `terraform/state/` package itself (its own
prod/test/doc files), none in `docs/internal/design` or
`docs/internal/evidence` beyond this note and the mapping tables that already
name the destination.

No-Regression Evidence: both list-query bodies, both SQL constants, and the
reader's evidence-assembly logic are unchanged apart from two identifier
exports, so no SQL text, predicate, lock, lease, batch size, worker count, or
graph write changed. One non-test file leaves root, so the `internal/storage/postgres` row in `scripts/lib/dirgate-grandfather.tsv` is re-pinned to what `bash scripts/verify-dirgate.sh --digest internal/storage/postgres` prints for the rebased tree (each rebase onto a sibling move re-derives it), which is
the pinned row in `scripts/lib/dirgate-grandfather.tsv`, regenerated via
`bash scripts/generate-dirgate-grandfather-go.sh`. After the final edit, from
`go/`: `gofumpt -l -w` on every changed file reports no diffs; `go build
./...` exits 0; `go vet ./...` exits 0; `go vet -tags "integration
perf5854_ack perf5740_completion perf6785_wait" ./internal/storage/postgres/...`
exits 0; `go test ./internal/storage/postgres/... -race -count=1` passes,
including the moved package (`ok
github.com/eshu-hq/eshu/go/internal/storage/postgres/terraform/state`); `go
test -list '.*' ./internal/storage/postgres/terraform/state/` shows all 5
moved test names. For each moved test, `for n in TestListTerraformStateLastSerialsParsesGenerationID TestListTerraformStateRecentWarningsBoundsLimit TestListTerraformStateRecentWarningsIncludesGitBackendExpressionWarnings TestListTerraformStateRecentWarningsAppliesContractDefaultLimit TestListTerraformStateLastSerialsSkipsMalformedRows; do go test ./internal/storage/postgres/terraform/state/... -list "^$n\$" -count=1 | rg -q "^$n\$" || echo "missing $n"; done` exits 0, and the same assertion against
`./internal/storage/postgres` exits 1, for all five:
`TestListTerraformStateLastSerialsParsesGenerationID`,
`TestListTerraformStateRecentWarningsBoundsLimit`,
`TestListTerraformStateRecentWarningsIncludesGitBackendExpressionWarnings`,
`TestListTerraformStateRecentWarningsAppliesContractDefaultLimit`,
`TestListTerraformStateLastSerialsSkipsMalformedRows`. `go test
./internal/query -run QueryPlan -count=1` passes (no source-hash pin in
`queryplan_production_variants_test.go` names `status_queries.go` or
`tfstate_status.go`). From the repo root: `bash scripts/verify-dirgate.sh
--all` exits 0; `bash scripts/verify-performance-evidence.sh origin/main`
exits 0; `git diff --check` exits 0 clean.

No-Observability-Change: both queries and the reader are pure read paths with
no metric, span, or log of their own; no metric, span, log key, or status
field is added, removed, or renamed.
