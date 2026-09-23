# #6693 checklist step 7: `maintenance/` leaf move

Baseline: `origin/main` `a12cdb2c7` (written against `228ca326f`, rebased after #7026). Change: move
`status_requests.go` and its test to
`go/internal/storage/postgres/maintenance/` (package `maintenancestore`), the
scan/reindex request lifecycle store (`StatusRequestStore`,
`NewStatusRequestStore`) over `runtime_ingester_control`. No SQL text, logic,
or identifier renamed; only the package clause and import paths changed.

## What moved

- `status_requests.go` -> `maintenance/requests.go` (package `postgres` ->
  `maintenancestore`). Already fully exported (`StatusRequestStore`,
  `NewStatusRequestStore`); no new export was needed.
- `status_requests_test.go` -> `maintenance/requests_test.go`, changed from
  in-package (`package postgres`) to an external test package
  (`package maintenancestore_test`) plus an `export_test.go` shim, because it
  needs root's exported `BootstrapDefinitions()` (to prove the
  `runtime_ingester_control` migration is registered) and the package's own
  private `controlSchemaSQL` constant (for the DDL-column assertion). Root's
  private `fakeExecQueryer`/`queueFakeRows` test doubles were replaced with
  the shared `internal/storage/postgres/fake` package
  (`fake.ExecQueryer`/`fake.Rows`) per its README.
- Added `doc.go`, `README.md`, `AGENTS.md` for the new package, modelled on
  `internal/storage/postgres/semantic/`.

## Decision: test placement

The mapping line in
`docs/internal/design/6693-postgres-target-tree/control-plane.md` carried no
placement annotation for `status_requests_test.go`. Per the executor brief's
step 4 default, an unannotated test is assumed in-package; investigation
showed it cannot be in-package because it calls root's exported
`BootstrapDefinitions()` and root does not sit "below" the new package in its
own dependency graph. I classified it as "external test package plus
`export_test.go` shim" (the same form root's own `db_test.go` already uses to
assert against `postgres.SQLDB`/`SQLQueryer`/`SQLTx`). Root does not import
`maintenance` anywhere (the only prior root caller, `status_requests.go`
itself, moved), so this is not a hard import-cycle requirement, but it keeps
the new package's test consistent with the repo's established convention. The plan's
"Test placement" tally is updated to match (in-package 375, external test
package plus shim 88), and the `control-plane.md` mapping line is annotated.

## Callers repointed

Only caller outside `internal/storage/postgres` root:

- `go/cmd/api/wiring_router.go` — added an import for
  `github.com/eshu-hq/eshu/go/internal/storage/postgres/maintenance` and
  changed `pgstatus.NewStatusRequestStore(...)` to
  `maintenancestore.NewStatusRequestStore(...)`. `pgstatus.SQLDB` (root)
  still supplies the `db.ExecQueryer` argument; that type did not move.

Docs updated to match: `go/cmd/api/README.md` (moved `NewStatusRequestStore`
to its own `internal/storage/postgres/maintenance` bullet) and
`go/internal/storage/postgres/exported-surface-guide.md` (removed the
`StatusRequestStore`/`NewStatusRequestStore` line, matching how prior fully-
moved domains, e.g. `semantic/`, were dropped from that root-only guide).
`internal/runtime/README.md`'s `StatusRequestStore` entry documents the
`internal/runtime` interface of the same name, not the postgres
implementation, and needed no change.

## dirgate

One non-test file leaves root, so the `internal/storage/postgres` row in `scripts/lib/dirgate-grandfather.tsv` is re-pinned to what `bash scripts/verify-dirgate.sh --digest internal/storage/postgres` prints for the rebased tree (each rebase onto a sibling move re-derives it).
Regenerated `tools/golangci-lint-dirgate/grandfather.go` via
`bash scripts/generate-dirgate-grandfather-go.sh`.

## Test-name repoint proof

Every moved test name is discovered in the new package and absent from the
old package (`for n in TestStatusRequestStoreRequestScanExecutesUpsert TestStatusRequestStoreClaimScanQueryReturnsScanRequest TestStatusRequestStoreClaimScanReturnsErrorWhenNoPending TestStatusRequestStoreCompleteScanExecutesUpdate TestStatusRequestStoreRequestReindexExecutesUpsert TestStatusRequestStoreClaimReindexQueryReturnsReindexRequest TestStatusRequestStoreClaimReindexReturnsErrorWhenNoPending TestStatusRequestStoreGetScanStateReturnsIdleWhenNotFound TestStatusRequestStoreGetReindexStateReturnsIdleWhenNotFound TestStatusRequestStoreRequiresDB TestStatusRequestStoreControlSchemaIncludesExpectedColumns TestStatusRequestStoreBootstrapDefinitionRegistered; do go test ./internal/storage/postgres/maintenance/... -list "^$n\$" -count=1 | rg -q "^$n\$" || echo "missing $n"; done`, exit 0 = found):

The `new pkg` column is that loop's per-name exit code against
`./internal/storage/postgres/maintenance/...` (0 = registered). The `old pkg`
column is the same per-name check run against `./internal/storage/postgres`
(`go test ./internal/storage/postgres -list "^$n$" -count=1 | rg -q "^$n$"`),
where exit 1 means the name is no longer registered in root.

| test | new pkg | old pkg |
| --- | --- | --- |
| TestStatusRequestStoreRequestScanExecutesUpsert | 0 | 1 |
| TestStatusRequestStoreClaimScanQueryReturnsScanRequest | 0 | 1 |
| TestStatusRequestStoreClaimScanReturnsErrorWhenNoPending | 0 | 1 |
| TestStatusRequestStoreCompleteScanExecutesUpdate | 0 | 1 |
| TestStatusRequestStoreRequestReindexExecutesUpsert | 0 | 1 |
| TestStatusRequestStoreClaimReindexQueryReturnsReindexRequest | 0 | 1 |
| TestStatusRequestStoreClaimReindexReturnsErrorWhenNoPending | 0 | 1 |
| TestStatusRequestStoreGetScanStateReturnsIdleWhenNotFound | 0 | 1 |
| TestStatusRequestStoreGetReindexStateReturnsIdleWhenNotFound | 0 | 1 |
| TestStatusRequestStoreRequiresDB | 0 | 1 |
| TestStatusRequestStoreControlSchemaIncludesExpectedColumns | 0 | 1 |
| TestStatusRequestStoreBootstrapDefinitionRegistered | 0 | 1 |

No-Regression Evidence: every request/claim/complete/read SQL statement and
the `runtime_ingester_control` DDL text moved byte-identical; only the
package clause, import paths, and one caller's constructor reference changed.
`go build ./...`, `go vet ./...`, and
`go vet -tags "integration perf5854_ack perf5740_completion perf6785_wait" ./internal/storage/postgres/...`
all exit 0. `go test ./internal/storage/postgres/... -race -count=1` and
`go test ./cmd/api/... -count=1` both pass, covering every request, claim
success/failure, complete, and idle-read case unchanged. No lock, lease,
batch size, worker count, or query plan changed.

No-Observability-Change: this move relocates only the maintenance-request
store, its SQL text, and its DDL constant, plus repoints one constructor call
in `cmd/api`. No metric, span, log field, or status name changes.
