# #6693 lock/ leaf

Change: checklist step 21 of `docs/internal/design/6693-postgres-target-tree.md`.
It moves the three `lock/` files (plus their three mapped tests) out of root
`internal/storage/postgres` into a new
`go/internal/storage/postgres/lock` leaf. The lockers serialize concurrent
writers with transaction-level advisory locks in ascending-ID order, and the
deferred-maintenance helpers fence whole-corpus maintenance behind shared or
exclusive per-repository partition locks.

Package clause: `lockstore` -- plain directory word plus `store`, with no
pre-existing `^package lockstore$` collision in `go/`. Callers import the
`storage/postgres/lock` path without an alias (the path's last element
differs from the package name, so no alias is needed and none is used).

Files moved (byte-identical apart from the package clause, except for the
export renames below):
`deferred_maintenance_lock.go` -> `lock/deferred_maintenance.go`,
`package_registry_identity_locker.go` -> `lock/package_registry_identity.go`,
`platform_graph_locker.go` -> `lock/platform_graph.go`. No exported
identifier was renamed; `PackageRegistryIdentityLocker`,
`PlatformGraphLocker`, and their `With*` methods were already exported and
moved unchanged.

Membership correction the mapping did not cover (root cause, not
improvisation): root production callers stay in root until the ingestion
steps, but use the moved file's symbols, so the symbols had to become
importable:

- The eight deferred-maintenance helpers
  (`acquireDeferredMaintenanceRepoSharedLock`,
  `acquireDeferredMaintenanceRepoExclusiveLocks`, `sortedUniqueRepoKeys`,
  `deferredMaintenanceRepoLockKey`, `deferredMaintenanceRepoLockKeyFromID`,
  `deferredMaintenanceLockNamespace`,
  `deferredMaintenancePartitionedSharedLockSQL`,
  `deferredMaintenancePartitionedExclusiveLockSQL`) became exported
  (uppercase, bodies byte-identical) in `lock/deferred_maintenance.go`.
  Root users repointed with the `lockstore.` qualifier: `ingestion.go`,
  `ingestion_backfill_pool.go`, `ingestion_backfill_per_commit.go`,
  `ingestion_backfill.go`, `repo_dependency_acceptance_gate.go`,
  `shared_intent_acceptance_writer.go`, plus the staying root tests
  (`deferred_maintenance_lock_test.go`,
  `deferred_maintenance_concurrency_test.go`,
  `deferred_maintenance_lock_fakes_test.go`,
  `ingestion_tx_lock_split_{integration,deadlock,helpers}_test.go`,
  `deferred_maintenance_barrier_test.go`,
  `repo_dependency_acceptance_gate_expiry_test.go`). Exporting (not
  duplicating) is the only correct shape: duplicating lock predicates
  across packages would let the two copies drift on locking behavior.
- `packageRegistryIdentityAdvisoryLockKey` (used by the staying root
  `graph_node_owner_store_test.go`) became
  `PackageRegistryIdentityAdvisoryLockKey` for the same reason; the test
  was repointed.

Callers repointed (imports/qualifiers only, no behavior change):

- `cmd/ingester/package_registry_identity_locker.go`,
  `cmd/reducer/service_materialization.go`,
  `cmd/projector/runtime_wiring.go`, `cmd/bootstrap-index/wiring.go`:
  struct literals repointed to `lockstore.*Locker{DB: ...}` (fields stay
  exported, so literals keep working). The ingester wrapper dropped its
  now-unused root import; the others still use root for other symbols.
- `cmd/reducer/main_test.go`: concrete-type assertion repointed to
  `lockstore.PlatformGraphLocker`.
- `internal/projector/README.md`: observability-evidence pointer repointed
  to the leaf path. `handler.go` files needed no change: their `*Locker`
  fields are locally declared interfaces, not the concrete types.

`rg` for the old unqualified names and old basenames across `go/` returns
nothing outside the new package, the repointed call sites above, and the
target-tree mapping lines (`current -> new`).

Test form: `package_registry_identity_test.go` and `platform_graph_test.go`
are `package lockstore` (in-package, self-contained fakes travel in-file).
`shared_intent_acceptance_writer_test.go` is `package lockstore_test`
(external, per the mapping's annotation): it imports root `postgres` for
`NewSharedIntentAcceptanceWriter`, this package for the exported deferred
helpers, `fake` for `fake.ExecQueryer`/`fake.Rows`/`fake.Result`, and keeps
its own copy of the `advisoryLockManager`/`advisoryLockTx` in-memory
advisory-lock simulator (root's `deferred_maintenance_lock_fakes_test.go`
defines the original for the staying deferred concurrency proofs; Go
test-only symbols do not cross package boundaries). No `export_test.go`
shim was needed: after the helper exports, the external test touches no
unexported leaf symbol. The mapping's "imports intent" note refers to the
`internal/reducer` import the test already had.

New directory doc trio: `doc.go`, `README.md`, `AGENTS.md`, modeled on the
freshness leaves.

Adding the `lock/` directory drops the root file count; dirgate's
`internal/storage/postgres` row in `scripts/lib/dirgate-grandfather.tsv` was
re-pinned to what `bash scripts/verify-dirgate.sh --digest internal/storage/postgres`
prints for this tree, and `tools/golangci-lint-dirgate/grandfather.go`
was regenerated to match.

## Test-repoint proof (exact names, both packages)

`go test ./internal/storage/postgres/lock/ -list '.*'`
lists the moved tests under the new package, while `-list` for the moved
names against `internal/storage/postgres` prints no test names (all gone
from root).

## Verification runs

From `go/` with `GOTOOLCHAIN=go1.26.6`: `go build ./...` exits zero; `go
vet` on the leaf, the parent tree, `cmd/reducer`, `cmd/ingester`,
`cmd/projector`, and `cmd/bootstrap-index` exits zero; `go test
./internal/storage/postgres/... -race -count=1` passes (root plus every
subpackage, including the new `lock` package and the staying deferred
concurrency/split-lock proofs against the exported helpers); `go test` on
all four repointed cmds passes. `go test -list '.*'
./internal/storage/postgres/lock/` lists all moved tests. `go test
./internal/query -run QueryPlan -count=1` was not re-run: no moved file's
source hash is pinned there (`rg` for the moved basenames in
`queryplan_production_variants_test.go` returns nothing). From the repo
root: `scripts/verify-dirgate.sh --digest internal/storage/postgres`
matches the ledger row; `git diff --check` is clean; `gofmt -l` on every
touched Go file reports nothing;
`scripts/test-verify-performance-evidence.sh` and
`scripts/verify-performance-evidence.sh` both exit zero.

No-Regression Evidence: focused package tests and every repointed caller suite pass on the moved tree, including the staying concurrency proofs against the exported helpers.
No-Observability-Change: package move only, with no metric, span, log field, worker, queue, lease, retry, or runtime-knob edit anywhere in the diff.
