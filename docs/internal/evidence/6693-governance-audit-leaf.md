# #6693 checklist step 13: `governance/audit/` (2 files)

Baseline: rebased onto the step-12 `code/taint/` head. Change: move `governance_audit_store.go`
and `governance_audit_store_helpers.go` (plus 4 of their 5 mapped tests) to the
new non-root package `go/internal/storage/postgres/governance/audit` (package
clause `auditstore`); no existing package of that name (`rg -n '^package
auditstore$' go/ --glob '*.go' -l` found none).

## What moved

- `governance_audit_store.go` -> `governance/audit/store.go`.
- `governance_audit_store_helpers.go` -> `governance/audit/helpers.go`.
- `governance_audit_append_bench_test.go` -> `governance/audit/append_bench_test.go`
  (in-package test, per the mapping's unannotated line; no root-private
  symbols read).
- `governance_audit_list_warn_test.go` -> `governance/audit/list_warn_test.go`
  (in-package test).
- `governance_audit_scan_tolerance_test.go` -> `governance/audit/scan_tolerance_test.go`
  (in-package test).
- `governance_audit_tenant_test.go` -> `governance/audit/tenant_test.go`
  (in-package test). Its event-id-uniqueness tests
  (`TestGovernanceAuditEventIDDistinctAcrossTenants`,
  `TestGovernanceAuditEventIDStableWithinTenant`,
  `TestGovernanceAuditAppendBothTenantsPersistedDistinctly`) were split into a
  new sibling `governance/audit/tenant_eventid_test.go` at move time: the
  moved file plus its added `fake` import landed at 515 lines, over the
  repo's 500-line cap. This is a same-destination file split (both files stay
  in package `auditstore`), not a symbol or package remapping, so it does not
  change the mapping's file counts.

`governance_audit_store_test.go` (the mapping's 5th test, annotated "external
test package: imports root") **stays in root** instead: it asserts root's
`BootstrapDefinitions()` against `orderedBootstrapDefinitionNames`, a
package-level var declared in root's own `schema_order_test.go` (a `_test.go`
file). That var is compiled only into root's own test binary; an external
`auditstore_test` package in a different directory never sees it, with or
without an `export_test.go` shim (a shim only re-exports symbols within the
package under test's own directory-scoped test run — it cannot reach into
another package's `_test.go`-only content). This matches
`TestCICDRunWatermarkSchemaMatchesBootstrapMigration`'s precedent for the
`cicd/` leaf (itself following `TestBootstrapDefinitionsIncludeSemanticExtractionQueue`
for `semantic/`). The plan was corrected in the same commit (see "Plan
correction" below) rather than improvising a workaround.

## Export

No new export was needed. `GovernanceAuditStore`, `NewGovernanceAuditStore`,
`GovernanceAuditQuery`, `GovernanceAuditEventsSchemaSQL`, and
`ErrGovernanceAuditQueryUnauthorized` were already exported and needed no
further change. `governanceAuditColumnsPerRow`, `buildGovernanceAuditListQuery`,
`scanGovernanceAuditEvent`, `governanceAuditEventID`, and the other private
helpers stayed unexported: every remaining reader is in-package (the four
moved test files) or, for the one root-kept test, a normal call through the
already-exported surface (`auditstore.NewGovernanceAuditStore`,
`auditstore.GovernanceAuditQuery`).

Tests moving out of root could no longer use root's private `fakeExecQueryer`
test double (`work_queue_lifecycle_test.go`); each was repointed to the
shared `internal/storage/postgres/fake` package
(`fake.ExecQueryer`/`fake.Rows`/`fake.ExecCall`/`fake.Result`), per
`fake/README.md`. `governance_audit_store_test.go` stayed in root, so it kept
using `fakeExecQueryer`/`queueFakeRows`/`fakeResult`/`fakeExecCall`/
`rowsAffectedResult` unchanged (same package, no move).

## Plan correction (root-kept test)

Per the executor brief's "must stay in root" guidance:

- Removed `governance_audit_store_test.go -> governance/audit/store_test.go`
  from `docs/internal/design/6693-postgres-target-tree/identity.md`'s
  `governance/audit/` section; its test count there dropped from 5 to 4.
- Added `governance_audit_store_test.go -> governance_audit_store_test.go`
  (alphabetically, with a `# asserts root BootstrapDefinitions()...` reason
  comment) to `root.md`'s tests list; its header test count rose by 1.
- Updated `docs/internal/design/6693-postgres-target-tree.md`'s
  "Per-directory counts" table: `storage/postgres` (root) row up by 1 test,
  `governance/audit/` row 5 -> 4.
- Updated the "Test placement" tally: "in-package test" minus 1, "stays in
  root" plus 1 (an unannotated mapping line moved from the in-package-test
  bucket to the stays-in-root bucket).
- Ticked `13. [x] governance/audit/ (2 files)` in the checklist.
- Rebases onto sibling moves can merge two identical count edits as one
  change without a conflict (the same line, changed the same way by both
  sides). After each rebase the counts were re-derived from the listed
  mapping lines: section headers match their lines, the counts table matches
  the headers, and the placement tallies sum to the 713-test census.

## Callers repointed

- `go/cmd/workflow-coordinator/main.go`: added the
  `.../storage/postgres/governance/audit` import (unaliased; declared package
  name is `auditstore`) and requalified `postgres.NewGovernanceAuditStore` to
  `auditstore.NewGovernanceAuditStore`.
- `go/cmd/api/admin_identity_reads.go`: added the import; requalified the
  `adminGovernanceAuditReader.store` field type, `NewGovernanceAuditStore`,
  and `GovernanceAuditQuery` to `auditstore.*`.
- `go/cmd/api/governance_audit_store.go`: added the import; requalified
  `NewGovernanceAuditStore`.
- `go/cmd/api/governance_audit_store_test.go`: added the import; requalified
  the `reader.summary.(pgstatus.GovernanceAuditStore)` type assertion (and its
  failure message) to `auditstore.GovernanceAuditStore`.
- `go/cmd/api/seed_initial_admin_persistence_test.go`: added the import;
  requalified `NewGovernanceAuditStore`/`GovernanceAuditQuery` and its
  doc-comment mention of `pgstorage.GovernanceAuditStore`.
- `go/cmd/mcp-server/wiring.go` and `wiring_router.go`: added the import;
  requalified `NewGovernanceAuditStore`.
- `go/internal/cli/admin/credential_audit.go`: added the import; requalified
  `NewGovernanceAuditStore`.
- `go/internal/cli/admin/credential_audit_test.go`: added the import;
  requalified `NewGovernanceAuditStore`/`GovernanceAuditQuery` and doc
  comments.
- `go/internal/cli/admin/credential_invariant_test.go`: comment-only fix
  (`pgstorage.GovernanceAuditStore` -> `auditstore.GovernanceAuditStore`; also
  the bare `governance_audit_store.go` filename mention -> the new path).
- Comment-only path/identifier fixes (no behavior change):
  `go/internal/governanceauditasync/doc.go` and `appender.go`
  (`storage/postgres.GovernanceAuditStore` -> `storage/postgres/governance/audit.GovernanceAuditStore`),
  `go/internal/query/admin/identity/reads.go` and
  `go/internal/query/auth_allowed_read_audit_bench_test.go` (old file paths in
  doc comments, now `storage/postgres/governance/audit/...`), and
  `docs/public/reference/telemetry/index.md` (two old file-path citations).

## Root docs (symbol qualification / removal)

- `go/internal/storage/postgres/doc.go`: removed the `GovernanceAuditStore`
  paragraph entirely (root's own doc.go only documents root's own exported
  types now that the type moved), matching the existing precedent that no
  already-moved store — `DecisionStore`, `IaCReachabilityStore`,
  `CICDRunWatermarkStore`, `PostgresAppliedPagerDutyServiceRoutingLoader` —
  is still named there.
- `go/internal/storage/postgres/README.md`: qualified both mentions
  (`GovernanceAuditStore` -> `auditstore.GovernanceAuditStore`) in the
  mermaid diagram and the validation-behavior bullet, matching the
  `decisionsstore.`/`iacstore.` precedent already in this file.
- `go/internal/storage/postgres/exported-surface-guide.md`: qualified both
  bullets (`GovernanceAuditStore`/`NewGovernanceAuditStore` and
  `GovernanceAuditEventsSchemaSQL`) with `auditstore.`, matching the
  `tenantstore.`/`iacstore.` precedent.

## New package docs

Added `governance/audit/doc.go`, `README.md`, and `AGENTS.md`, modelled on
`incident/` and `iac/`, documenting: the `governance_audit_events` sink and
its retry-idempotent `Append`; the #6574 unknown-enum List tolerance and its
once-per-field WARN; the #3717 tenant-isolation contract
(`GovernanceAuditQuery.TenantID`); the event-id derivation and the
cross-tenant collision regression it guards; and the "must not import the
parent postgres package" invariant. `AGENTS.md` also notes the
`tenant_test.go`/`tenant_eventid_test.go` fixture split.

## dirgate

`internal/storage/postgres`'s row in `scripts/lib/dirgate-grandfather.tsv`
was re-pinned (two non-test files leave root) to what
`bash scripts/verify-dirgate.sh --digest internal/storage/postgres` prints for
the rebased tree, and `tools/golangci-lint-dirgate/grandfather.go` was
regenerated. Each rebase onto a sibling move re-derives the row the same way. `bash scripts/verify-dirgate.sh
--all` prints the pre-existing, unrelated `naming_violation` lines for files
mapped to later checklist steps (already excused there) and exits 0.

## Verification (from `go/` unless noted)

- `gofumpt -l -w` on every changed file: exit 0, no files listed (already
  formatted).
- `go build ./...`: exit 0.
- `go vet ./...`: exit 0.
- `go vet -tags "integration perf5854_ack perf5740_completion perf6785_wait" ./internal/storage/postgres/...`: exit 0.
- `go test ./internal/storage/postgres/... -race -count=1`: exit 0, all 18
  tested packages pass (`ok`) including the new
  `.../postgres/governance/audit` (1.310s) and root (34.835s); zero `FAIL`,
  panic, or DATA RACE lines.
- `go test ./internal/governanceauditasync/... ./internal/cli/admin/...
  ./internal/query/... ./cmd/api/... ./cmd/mcp-server/...
  ./cmd/workflow-coordinator/... -count=1`: 60 `ok` package results, zero
  `FAIL` lines.
- `go test -list '.*' ./internal/storage/postgres/governance/audit/`: lists
  all 18 moved test names plus `BenchmarkGovernanceAuditStoreAppendSingleEvent`.
- Exact-name repoint proof for each of the 18 moved names: `for n in
  TestGovernanceAuditStoreListWarnsOncePerUnknownEnumField
  TestGovernanceAuditStoreListWarnsThroughDefaultLoggerWhenUnset
  TestGovernanceAuditStoreListKeepsUnknownEnumValuesVerbatim
  TestGovernanceAuditStoreListStillRejectsUnsafeStoredRows
  TestGovernanceAuditStoreAppendRejectsUnknownActorClass
  TestGovernanceAuditEventIDDistinctAcrossTenants
  TestGovernanceAuditEventIDStableWithinTenant
  TestGovernanceAuditAppendBothTenantsPersistedDistinctly
  TestGovernanceAuditSchemaDDLIncludesTenantID
  TestGovernanceAuditListQueryIncludesTenantIDPredicate
  TestGovernanceAuditListQueryNoTenantHasNoTenantPredicate
  TestGovernanceAuditAppendStoresTenantID TestGovernanceAuditAppendStoresWorkspaceID
  TestGovernanceAuditAppendSystemEventStoresNullTenantID
  TestGovernanceAuditSummaryScopedToTenant TestGovernanceAuditListCrossTenantIsolation
  TestGovernanceAuditListGlobalOperatorSeesBothTenants
  TestGovernanceAuditListGlobalEventsHiddenFromTenantAdmin; do go test
  ./internal/storage/postgres/governance/audit/... -list "^$n\$" -count=1 |
  rg -q "^$n\$" || echo "missing $n"; done` printed no "missing" line for any
  name (all 18 found), and the same loop against `./internal/storage/postgres`
  printed no "still present" line for any name (all 18 absent from root).
- Root's 7 kept tests are still registered: `go test
  ./internal/storage/postgres -list
  "^(TestBootstrapDefinitionsIncludeGovernanceAuditEvents|TestGovernanceAuditStoreAppendNormalizesAndDeduplicatesRetry|TestGovernanceAuditStoreAppendRejectsUnsafeEventWithoutEcho|TestGovernanceAuditStoreListRequiresOperatorAuthorization|TestGovernanceAuditStoreListAppliesBoundsAndOrdering|TestGovernanceAuditStoreDeleteExpiredUsesCutoff|TestGovernanceAuditStoreSummaryAggregatesWithoutBodies)\$"
  -count=1`: exit 0, lists all 7 names.
- From the repo root: `bash scripts/verify-dirgate.sh --all`: exit 0, no
  output (besides the pre-existing unrelated naming-violation lines noted
  above). `bash scripts/verify-package-docs.sh`: exit 0. `git diff --check`:
  exit 0, no output.
- `rg` for the old filenames (`governance_audit_store.go`,
  `governance_audit_store_helpers.go`, `governance_audit_append_bench_test.go`,
  `governance_audit_list_warn_test.go`, `governance_audit_scan_tolerance_test.go`,
  `governance_audit_tenant_test.go`) across the repo: no hits outside this
  evidence note, the design docs under
  `docs/internal/design/6693-postgres-target-tree*`, and one historical
  evidence note (`docs/internal/evidence/6450-all-scope-browser-session-admission.md`)
  that legitimately records the path as it stood at the time.

No-Regression Evidence: the DDL, `Append`'s batching and
`ON CONFLICT (event_id) DO NOTHING` idempotency, `List`'s operator-authorization
gate and #6574 unknown-enum tolerance, `Summary`/`SummaryForTenant`'s
aggregate-only column set, and the event-id derivation are byte-identical to
the pre-move code — only paths, the package clause, and doc/comment
qualifications changed. `go test ./internal/storage/postgres/... -race
-count=1` and the caller-package suite above are green on the moved tree.

No-Observability-Change: no metric, span, log key, or status field was added,
removed, or renamed; the store still executes the same bounded SQL and emits
the same #6574 WARN through the same injected `*slog.Logger` (or
`slog.Default`).
