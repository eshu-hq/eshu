# #6693 checklist step 11: `cloud/aws/` leaf

Baseline: rebased onto the step-10 `vulnerability/` head; no intervening
commit touched this destination. Change: `git mv
aws_pagination_checkpoint.go
go/internal/storage/postgres/cloud/aws/pagination_checkpoint.go`, `git mv
aws_scan_status.go
go/internal/storage/postgres/cloud/aws/scan_status.go`, and their two test
files, per the target-tree mapping in
`docs/internal/design/6693-postgres-target-tree/cloud.md` (`cloud/aws/`
section, 2 non-test, 2 test). Package clause changed `postgres` -> `awsstore`
(the `storage/postgres/semantic`/`scope` naming convention: plain directory
word plus `store`, per the target-tree doc's own worked example naming
`cloud/aws` as `awsstore`); checked for collisions first (`rg -n '^package
awsstore$' go/ --glob '*.go' -l`, no hits).

Both moved production files are independent of each other and of the rest of
root: `pagination_checkpoint.go` declares `AWSPaginationCheckpointStore` and
depends only on `internal/storage/postgres/db`,
`internal/collector/awscloud/checkpoint`, and `internal/telemetry`;
`scan_status.go` declares `AWSScanStatusStore` and depends only on
`internal/storage/postgres/db` and `internal/collector/awscloud`. Neither
file's private helpers (`checkpointKeyArgs`, `encode`/`decodeCheckpointPayload`,
`validateCheckpointMutation`, `validateAWSScanBoundary`,
`validateAWSScanStatusMutation`) are referenced anywhere else in root
(checked with `rg`), so nothing needed exporting for the two production
files.

## Test placement (unannotated: in-package)

Both test files kept `package awsstore` (no external-test annotation in the
mapping). `pagination_checkpoint_test.go` and `scan_status_test.go` share two
root-private test doubles defined in the pagination file's test
(`awsCheckpointExec`, `awsCheckpointRowsResult`) and one defined in the
scan-status file's test (`assertPostgresPlaceholdersMatchArgs`); since both
test files moved together into the same new package, these stayed exactly as
written -- no rename or export needed.

## Shared test helper: already handled by step 10

Before step 10, root's `vulnerability_source_state_test.go` used two of the
moving `scan_status_test.go` helpers (`awsScanStatusTestDB`/
`awsCheckpointRowsResult` and `assertPostgresPlaceholdersMatchArgs`). Step 10
(`vulnerability/`) moved that test and made it standalone: it uses
`internal/storage/postgres/fake` and keeps its own copy of
`assertPostgresPlaceholdersMatchArgs`. This PR only updates that copy's
comment to name its origin, `cloud/aws/scan_status_test.go`.

No other root file referenced either moved test file's private symbols
(checked with `rg` for `awsCheckpointExec`, `awsCheckpointRowsResult`,
`awsScanStatusTestDB`, `assertPostgresPlaceholdersMatchArgs` across
`internal/storage/postgres/*.go` before and after the move).

## Caller repoint

One caller outside `internal/storage/postgres` used the moved exported
symbols: `cmd/collector-aws-cloud/service.go` called
`postgres.NewAWSPaginationCheckpointStore(database)` and
`postgres.NewAWSScanStatusStore(database)`. It now imports
`internal/storage/postgres/cloud/aws` (`awsstore`) unaliased, alongside the
existing `internal/storage/postgres` import (still needed for
`postgres.SQLDB`, `postgres.NewIngestionStore`, and
`postgres.NewWorkflowControlStore`, none of which moved), and calls
`awsstore.NewAWSPaginationCheckpointStore(database)` /
`awsstore.NewAWSScanStatusStore(database)`.

No other file under `go/` referenced `AWSPaginationCheckpointStore`,
`NewAWSPaginationCheckpointStore`, `AWSScanStatusStore`, or
`NewAWSScanStatusStore` qualified through `postgres.` (checked with `rg`
across `go/`); the remaining matches were all prose mentions of the type
names by comments in `cmd/mcp-server/status_store_wiring.go`,
`cmd/api/status_store_wiring.go`, `internal/storage/postgres/status.go`, and
`internal/storage/postgres/cicd/doc.go`/`watermark.go` -- none import the
type, so none needed a code change (`watermark.go`'s comment referencing the
old file path was repointed; see below).

## Stale path references repointed

`rg` for the two moved files' old paths outside
`docs/internal/design`/`docs/internal/evidence` found five prose references,
all repointed to the new `cloud/aws/` path:
`go/internal/storage/postgres/AGENTS.md` (two "Read first" entries plus one
"Common changes" entry), `go/internal/storage/postgres/cicd/watermark.go`
(a comment contrasting watermark reads with checkpoint reads),
`go/internal/collector/awscloud/checkpoint/AGENTS.md` (a "Read First" entry),
and `go/internal/collector/awscloud/AGENTS.md` (a dependent-file list entry).

`bash scripts/verify-moved-file-refs.sh` (which does not carve out
`docs/internal/design`) caught a sixth: `docs/internal/design/1286-postgres-ownership-inventory.md`'s
"Sources used" list names both old paths. Both lines are repointed to the
new `cloud/aws/` paths, matching how the `maintenance/` and `vulnerability/`
moves repointed their entries in the same list.

## Doc trio and root docs

Added `doc.go`, `README.md`, `AGENTS.md` for `cloud/aws/`, modeled on
`storage/postgres/iac/`. `cloud/` itself carries no `.go` files (only the
`aws/`, `inventory/`, and `multi/` subdirectories do), so it needs no doc
trio of its own -- `verify-package-docs.sh` only requires the trio for
directories with `.go` files.

Root `doc.go` mentioned neither moved file nor either store type (checked
with `rg`). Root `README.md` and `AGENTS.md` each had one qualified mention
of `AWSPaginationCheckpointStore`/`AWSScanStatusStore` in prose (observability
and fencing invariant bullets); both are now qualified `awsstore.*`. Root
`exported-surface-guide.md` listed both stores' constructors in its
`/admin/status` and "AWS pagination checkpoints" sections; both are now
`awsstore.AWSPaginationCheckpointStore` / `awsstore.NewAWSPaginationCheckpointStore`
and `awsstore.AWSScanStatusStore` / `awsstore.NewAWSScanStatusStore`.

Two non-test files leave root, so the `internal/storage/postgres` row in
`scripts/lib/dirgate-grandfather.tsv` is re-pinned to what
`bash scripts/verify-dirgate.sh --digest internal/storage/postgres` prints
for this tree (each rebase onto a sibling move re-derives it), and
`bash scripts/generate-dirgate-grandfather-go.sh` regenerates
`tools/golangci-lint-dirgate/grandfather.go` from it.

Ticked checklist item 11 in `docs/internal/design/6693-postgres-target-tree.md`.
The per-directory counts table there already lists `cloud/aws/` as `2 | 2 |
ok` (it states the epic's end-state target, not a per-step live count) and
needed no edit; no mapped file stayed in root, so the target-tree's
membership-correction tables needed no edit either.

## Test-repoint proof (exact names, both packages)

For each moved test, `go test ./internal/storage/postgres/cloud/aws/...
-list "^<Name>$" -count=1 | rg -q "^<Name>$"` (new package) and the same
against `./internal/storage/postgres` (old package) with exit codes
captured directly:

```
TestAWSPaginationCheckpointSchemaSQL:                                new exit 0 / old exit 1
TestAWSPaginationCheckpointStoreSaveRejectsOlderFence:               new exit 0 / old exit 1
TestAWSPaginationCheckpointStoreExpireStaleScopesByGeneration:       new exit 0 / old exit 1
TestAWSPaginationCheckpointStoreRecordsStableEventKinds:             new exit 0 / old exit 1
TestAWSPaginationCheckpointStoreRecordEventAllowsPartialInstruments: new exit 0 / old exit 1
TestAWSScanStatusSchemaSQL:                                          new exit 0 / old exit 1
TestAWSScanStatusStoreUsesFenceGuard:                                new exit 0 / old exit 1
TestAWSScanStatusStoreAllowsNewGenerationAfterTerminalPriorScan:     new exit 0 / old exit 1
TestAWSScanStatusStoreAllowsNewGenerationAfterTerminalPermissionGap: new exit 0 / old exit 1
TestAWSScanStatusStoreAllowsNewGenerationOverOrphanedRunningRow:     new exit 0 / old exit 1
TestAWSScanStatusStoreReturnsTypedStaleFenceError:                   new exit 0 / old exit 1
TestAWSScanStatusStoreUsesExactFenceForObserveAndCommit:             new exit 0 / old exit 1
TestAWSScanStatusStoreClearsCommitFailureAfterSuccessfulCommit:      new exit 0 / old exit 1
```

## No-Regression Evidence

No-Regression Evidence: `cd go && gofumpt -l` on every changed Go file reports nothing. `go build
./...` and `go vet ./...` are clean across the whole module. `go vet -tags
"integration perf5854_ack perf5740_completion perf6785_wait"
./internal/storage/postgres/...` is clean. `go test
./internal/storage/postgres/... -race -count=1` passes for every
subpackage, including root and the new `cloud/aws/`. `go test
./cmd/collector-aws-cloud/... -race -count=1` (the repointed caller) passes.
`go test ./internal/query -run QueryPlan -count=1` passes;
`queryplan_production_variants_test.go` pins no source hash for either moved
file, so no pin refresh was needed. `go test -list '.*'
./internal/storage/postgres/cloud/aws/` shows all 13 moved tests. From the
repo root: `bash scripts/verify-dirgate.sh --all` exits 0;
`bash scripts/verify-moved-file-refs.sh` exits 0 (no dangling references
after the repoint above); `bash scripts/verify-doc-citations.sh` exits 0;
`bash scripts/verify-package-docs.sh` and `bash
scripts/verify-performance-evidence.sh origin/main` are clean; `git diff
--check` is clean; `rg` for `aws_pagination_checkpoint.go`,
`aws_scan_status.go`, and their `_test.go` names outside
`docs/internal/design` and `docs/internal/evidence` returns nothing after
the five prose repoints above.

## No-Observability-Change

No-Observability-Change: no metric, span, log key, worker, queue, lease, retry, or durable write
shape changed. This is a path/package move plus a comment update in
`vulnerability/source_state_test.go`;
`AWSPaginationCheckpointStore`'s `eshu_dp_aws_pagination_checkpoint_events_total`
counter and `AWSScanStatusStore`'s fencing SQL are untouched code, only their
package location and import path changed.
