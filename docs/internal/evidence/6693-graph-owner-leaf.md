# #6693 checklist step 14: `graph/owner/` leaf

Baseline: `origin/main`, worktree branched fresh for this step. Change: `git mv
graph_node_owner_backfill_store.go go/internal/storage/postgres/graph/owner/backfill.go`,
`git mv graph_node_owner_store.go go/internal/storage/postgres/graph/owner/store.go`,
`git mv graph_node_owner_backfill_store_live_test.go go/internal/storage/postgres/graph/owner/backfill_live_test.go`,
`git mv graph_node_owner_backfill_store_test.go go/internal/storage/postgres/graph/owner/backfill_test.go`,
`git mv graph_node_owner_store_integration_test.go go/internal/storage/postgres/graph/owner/store_integration_test.go`,
per the target-tree mapping in `docs/internal/design/6693-postgres-target-tree/code.md`
(`graph/owner/` section). Package clause changed `postgres` -> `ownerstore`
(the `storage/postgres/semantic`/`scope` naming convention); checked for
collisions first (`rg -n '^package ownerstore$' go/ --glob '*.go' -l`, no
hits). `graph/` itself gained no `.go` files directly (only the `owner/`
subdirectory), per the checklist's own note that step 40 later moves a
separate 1-file `graph/` leaf and must not be pre-empted.

`store.go`'s `graphNodeOwnerSchemaSQL` read root's `BootstrapDefinitions()`,
which now wraps `migrations.BootstrapDefinitions()` (landed under D2); the
moved file calls `migrations.BootstrapDefinitions()` directly instead,
avoiding a root import (an import cycle, since `graph/owner` must not import
the parent `postgres` package).

## Membership and exports

Both non-test files were self-contained apart from `BootstrapDefinitions`
above: their only other cross-file dependency is `internal/storage/postgres/db`
(`Executor`, `ExecQueryer`, `Beginner`, `Transaction`), a sibling package.
`GraphNodeOwnerStore`, `GraphNodeOwnerEntry`, `NewGraphNodeOwnerStore`,
`GraphNodeOwnerBackfillStore`, `NewGraphNodeOwnerBackfillStore`,
`GraphNodeOwnerBackfillDB`, and `GraphNodeOwnerBackfillMinimumOrderKeyPrefix`
were already exported and needed no change beyond the package move.

Five additional symbols were promoted from unexported to exported --
`GraphNodeOwnerSchemaSQL`, `GraphNodeOwnerUpsertSuffix`,
`GraphNodeOwnerAcquireLocksSQL`, `GraphNodeOwnerAdvisoryKey`, and
`DedupeOwnerEntries` -- solely because the root `#6693` SPLIT test
`graph_node_owner_store_test.go` (`docs/internal/design/6693-postgres-target-tree/root.md`:
"SPLIT: reads private symbols of graph/owner, lock") still asserts on them
directly and also compares against root-private
`packageRegistryIdentityAdvisoryLockKey` (the future `lock/` leaf, checklist
step 21, not yet extracted). Every production caller of these five symbols
stays inside `graph/owner` itself; no other package should depend on them.
This is the same pattern the `iac/` leaf's evidence note used for
`iacstore.ListAppliedPagerDutyServiceRoutingQuery`.

`cloudResourceOwnerBackfillKey` (used only by the moved
`backfill_test.go`, which needs its own `package ownerstore_test`) is exposed
through a new `export_test.go` shim (`CloudResourceOwnerBackfillKey`), since
it has no non-test caller and does not belong in the production exported
surface.

## Test placement

- `backfill_test.go` (mapping annotation "external test package +
  `export_test.go` shim: imports root"): `package ownerstore_test`, importing
  `internal/storage/postgres` (root, for `MigrationSQL`) and
  `internal/storage/postgres/graph/owner` (referenced as `ownerstore`,
  matching its package clause). Root's private `recordingExecQueryer` (used
  by the pre-move file) is not importable, so the two tests were rewritten
  against the shared `internal/storage/postgres/fake` package per the
  checklist's own instruction. `GraphNodeOwnerBackfillDB` needs
  `db.Beginner` in addition to `db.ExecQueryer`, which `fake.ExecQueryer`
  does not implement on its own (it only implements
  `db.ReadOnlyRepeatableReadBeginner`); a small local `backfillDB` wrapper in
  the test file adds `Begin` by delegating to
  `BeginReadOnlyRepeatableRead`, reusing the fake's existing call-recording
  fields (`BeginReadOnlyRepeatableReadCalls`, `Execs`, `Queries`) instead of
  adding new plumbing. The empty-ledger completion-check test stages
  `fake.Rows{{}}` (zero rows) to reproduce the old fake's
  always-`Next()==false` behavior.
- `backfill_live_test.go` (mapping annotation "external test package: imports
  root"): `package ownerstore_test`, importing root for `MigrationSQL` and
  `SQLDB` (unchanged, already exported) plus the local package for
  `GraphNodeOwnerEntry`/`GraphNodeOwnerBackfillMinimumOrderKeyPrefix`/
  `NewGraphNodeOwnerBackfillStore`. No shim needed.
- `store_integration_test.go` (no mapping annotation, so in-package):
  `package ownerstore`. Every symbol it touches (`NewGraphNodeOwnerStore`,
  `GraphNodeOwnerEntry`, `ResolveOwnedUIDs`) was already exported, so only
  the package clause changed. Its local `sqlTxExecQueryer` (an `*sql.Tx` ->
  `db.ExecQueryer` adapter) is unrelated to the root-private helper of the
  same name discussed below and was left as its own small in-package type.

`graph_node_owner_store_test.go` is NOT part of this mapping: it is annotated
`SPLIT: reads private symbols of graph/owner, lock` in
`docs/internal/design/6693-postgres-target-tree/root.md` and stays in root
until the future `lock/` leaf (checklist step 21) also exists. It was updated
in place to import `internal/storage/postgres/graph/owner` (as `ownerstore`)
and qualify every reference to the five newly-exported symbols above, plus
`GraphNodeOwnerEntry` and `NewGraphNodeOwnerStore`; its own root-private
`recordingExecQueryer` and `packageRegistryIdentityAdvisoryLockKey`
references are untouched (both stay in root).

## Root-private helper collision: `sqlTxExecQueryer`

The moved `store_integration_test.go` originally defined a root-private
`sqlTxExecQueryer` type (`*sql.Tx` -> `db.ExecQueryer` adapter) that two
OTHER root live tests also used --
`cloud_resource_liveness_live_test.go` and
`value_flow_inputs_liveness_live_test.go` -- neither of which is part of this
mapping. Moving the type out of root broke both (Go test files are not
importable). Root's own exported `SQLTx` (`adapters.go`, which stays in
root) is functionally identical (the same `*sql.Tx` -> `db.ExecQueryer`
wrapper), so both callers now use `SQLTx{Tx: tx}` instead of a duplicated
private type -- a strict simplification, not a workaround. The moved file
keeps its own small `sqlTxExecQueryer` copy for its own use, since Go test
files still cannot share it across the package boundary.

## Caller repoint

Callers outside `internal/storage/postgres` (imports/qualifiers and comments
only):

- `internal/query/cloud_resource_owner_backfill.go` /
  `cloud_resource_owner_backfill_test.go`: added the
  `internal/storage/postgres/graph/owner` import alongside the existing
  `internal/storage/postgres` import (still needed for `SQLDB`); qualified
  `GraphNodeOwnerBackfillMinimumOrderKeyPrefix`, `GraphNodeOwnerEntry`, and
  `NewGraphNodeOwnerBackfillStore` with `ownerstore.`.
- `internal/graphowner/gated_writer.go`, `lock_only_gate.go`: swapped the
  `internal/storage/postgres` import for `internal/storage/postgres/graph/owner`
  outright (their only use of `postgres.` was `GraphNodeOwnerStore`/
  `NewGraphNodeOwnerStore`); qualified doc comments too.
- `internal/graphowner/lock_only_gate_perf_live_test.go`,
  `lock_only_gate_prove_theory_live_test.go`, `gated_writer_chunk_live_test.go`,
  `gated_writer_live_test.go`, `retract_live_test.go`: kept the
  `internal/storage/postgres` import (still needed for `SQLDB`/`SQLTx`) and
  added the new import for `NewGraphNodeOwnerStore`/`GraphNodeOwnerEntry`.
- `internal/graphowner/gated_writer_chunk_test.go`, `retract_test.go`: swapped
  the import outright (their only use of `postgres.` was
  `GraphNodeOwnerEntry`).
- `internal/graphowner/doc.go`: comment-only, no import; qualified two
  `GraphNodeOwnerStore` mentions.
- `internal/graphowner/README.md`, `AGENTS.md`,
  `evidence-5062-lock-only-gate.md`: qualified `GraphNodeOwnerStore`/
  `GraphNodeOwnerAdvisoryKey` mentions and repointed the
  `graph_node_owner_store.go`/`graph_node_owner_backfill_store.go` old-path
  references to their new locations.
- `internal/replay/costcounting/{kubernetes_workload_node_cost,
  gcp_resource_materialization_cost,ec2_instance_node_cost,
  azure_resource_materialization_cost}_test.go`: comment-only (`postgres.` was
  never imported in these files -- the references were prose only); qualified
  to `ownerstore.`.
- `internal/storage/postgres/cloud_resource_liveness_live_test.go` (stays in
  root): added the new import and qualified `NewGraphNodeOwnerStore`/
  `GraphNodeOwnerEntry`.
- `go/cmd/read-api-latency-gate/seed_owner_ledger.go`,
  `seed_content_entities_test.go`: comment-only path repoint (no import; the
  gate deliberately duplicates two literals rather than importing the store).
- `specs/live-tests.v1.yaml`, `specs/ci-gates.v1.yaml`,
  `scripts/lib/ifa_live_gate_selector_cases.sh`: literal old file paths
  repointed to the new location.

No other file under `go/` referenced `GraphNodeOwnerStore`,
`GraphNodeOwnerEntry`, `GraphNodeOwnerBackfillStore`, or
`NewGraphNodeOwnerBackfillStore` qualified through `postgres.` (checked with
`rg`); the one remaining `postgres.GraphNodeOwnerStore` hit repo-wide is a
historical, point-in-time evidence note for an unrelated issue
(`docs/internal/evidence/5652-nornic-bare-match-writeloss.md`), left as-is
per the checklist's own evidence-doc exemption.

## Doc trio and dirgate

Added `doc.go`, `README.md`, `AGENTS.md` for `graph/owner/`, modeled on
`storage/postgres/incident/` and `storage/postgres/iac/`. Root's
`README.md` and `exported-surface-guide.md` named the moved symbols and were
updated to qualify them as `ownerstore.*` (root's own `doc.go`/`AGENTS.md`
named neither, checked with `rg`).

Two non-test files leave root, so the `internal/storage/postgres` row in
`scripts/lib/dirgate-grandfather.tsv` is re-pinned to what
`bash scripts/verify-dirgate.sh --digest internal/storage/postgres` prints
for this tree, and `bash scripts/generate-dirgate-grandfather-go.sh`
regenerates `tools/golangci-lint-dirgate/grandfather.go` from it. The new
`graph/` directory gained no naming-violation candidates: `dirgate`'s
sibling-subpackage check only counts a directory as a live sibling once it
holds a `.go` file directly inside it, and `graph/` holds none (only the
`owner/` subdirectory) -- `bash scripts/verify-dirgate.sh --digest
internal/storage/postgres` printed no `naming_violation` line for
`graph_endpoint_presence.go` (the root file step 40 will later move into
`graph/`), so no `//nolint:dirgate` marker was needed this step.

## Test-repoint proof (exact names, both packages)

```
TestGraphNodeOwnerBackfillStateMigrationDeclaresDurableMarker: new exit 0 / old exit 1
TestGraphNodeOwnerBackfillStoreSeedUsesLockedMaxUpsert: new exit 0 / old exit 1
TestGraphNodeOwnerBackfillStoreStateQueriesUseStableKey: new exit 0 / old exit 1
TestLiveGraphNodeOwnerBackfillPreservesRealOwnersAndScales: new exit 0 / old exit 1
TestGraphNodeOwnerStoreIntegration: new exit 0 / old exit 1
```

(`new` = `go test ./internal/storage/postgres/graph/owner/... -list "^<name>\$" -count=1 | rg -q "^<name>\$"`;
`old` = the same against `./internal/storage/postgres`.)

## No-Regression Evidence

`cd go && gofumpt -l` on every changed file reports nothing (one file,
`internal/graphowner/retract_test.go`, needed an import-order fix, applied).
`go build ./...` and `go vet ./...` are clean across the whole module. `go
vet -tags "integration perf5854_ack perf5740_completion perf6785_wait"
./internal/storage/postgres/...` is clean. `go test
./internal/storage/postgres/... -race -count=1` passes for every
subpackage, including the new `graph/owner/`. `go test
./internal/query/... ./internal/graphowner/... ./internal/replay/costcounting/...
-count=1` passes (all repointed callers). `go test ./internal/query -run
QueryPlan -count=1` passes; `queryplan_production_variants_test.go` pins no
source hash for any moved file, so no pin refresh was needed. `go test -list
'.*' ./internal/storage/postgres/graph/owner/` shows all five moved tests.
From the repo root: `bash scripts/verify-dirgate.sh --all` exits 0; `bash
scripts/verify-package-docs.sh`, `bash scripts/verify-moved-file-refs.sh`,
`bash scripts/verify-doc-citations.sh`, and `bash
scripts/verify-performance-evidence.sh origin/main` are clean after commit;
`git diff --check` is clean; `rg` for every moved file's old basename and
every promoted symbol's old (lowercase) name outside
`docs/internal/design` and `docs/internal/evidence` returns nothing.

## No-Observability-Change

No metric, span, log key, worker, queue, lease, retry, or durable write
shape changed. This is a path/package move plus five unexported-to-exported
symbol promotions (test-visibility only) and one dead-code-free
type-deduplication (`sqlTxExecQueryer` -> `SQLTx` in two unrelated root
tests); every production code path, SQL statement, and DDL is
byte-identical to before the move.
