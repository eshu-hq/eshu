# #6693 freshness/gcp/ leaf

Change: checklist step 19 of `docs/internal/design/6693-postgres-target-tree.md`.
It moves the three `freshness/gcp/` files (plus their two mapped tests) out of
root `internal/storage/postgres` into a new
`go/internal/storage/postgres/freshness/gcp` leaf. Per decision N4, this is
one of the three `freshness/{aws,gcp,incident}/` trigger stores that share a
`StoreTrigger`/`ClaimQueuedTriggers`/`ReapExpiredTriggerClaims`/
`MarkTriggersHandedOff`/`MarkTriggersFailed` lifecycle the workflow
coordinator and webhook listener build, and stay together for that reason.

Package clause: `gcpfreshnessstore` -- the sibling `freshness/aws/` leaf is
`awsfreshnessstore` and `cloud/aws/` is `awsstore`, and the naming plan
(`docs/internal/design/6693-postgres-target-tree.md`'s "Package clauses"
bullet) resolves the three `freshness/` children to
`awsfreshnessstore`/`gcpfreshnessstore`/`incidentfreshnessstore` precisely so
they stay distinct. `rg -n '^package gcpfreshnessstore$' go/ --glob '*.go'
-l` found no pre-existing collision.

Files moved (production, byte-identical apart from the package clause):
`gcp_freshness_schema_sql.go` -> `freshness/gcp/schema_sql.go`,
`gcp_freshness_sql.go` -> `freshness/gcp/sql.go`,
`gcp_freshness_store.go` -> `freshness/gcp/store.go`. No exported identifier
was renamed; `GCPFreshnessStore`, `NewGCPFreshnessStore`, and
`GCPFreshnessSchemaSQL` were already exported and moved unchanged.

Callers repointed (imports/qualifiers only, no behavior change):

- `cmd/workflow-coordinator/main.go`: `postgres.NewGCPFreshnessStore` ->
  `gcpfreshnessstore.NewGCPFreshnessStore`, with the new package imported
  unaliased (matching the `freshness/aws` precedent from step 18).
- `cmd/webhook-listener/main.go`: same constructor repoint, plus
  `*postgres.GCPFreshnessStore` -> `*gcpfreshnessstore.GCPFreshnessStore` for
  the local variable declaration. `cmd/webhook-listener/handler.go` needed no
  change: its `gcpFreshnessStore` field is a locally declared interface, not
  the concrete type (same as the AWS handler).
- `internal/coordinator/service_gcp_freshness.go`: doc comment repointed to
  `gcpfreshnessstore.GCPFreshnessStore`.
- `internal/storage/postgres/freshness_claim_lease_migration_backfill_integration_test.go`
  (root, stays in root -- not part of this leaf's mapping): its call site
  `NewGCPFreshnessStore(SQLDB{DB: db})` (same-package, unqualified) became
  `gcpfreshnessstore.NewGCPFreshnessStore(SQLDB{DB: db})` with the new package
  imported; `SQLDB` stays unqualified since this file is still `package
  postgres`.

`rg` for the old unqualified names (`GCPFreshnessStore`,
`NewGCPFreshnessStore`, `GCPFreshnessSchemaSQL`) and old basenames across
`go/` returns nothing outside the new package and the repointed call sites
above; the only remaining mentions are the target-tree mapping lines
(`current -> new`) and historical prose that name the symbol, not its home.

Test form: `store_test.go` is `package gcpfreshnessstore` (in-package, no
export needed -- matches the mapping's unannotated line).
`claim_lease_integration_test.go` is `package gcpfreshnessstore_test`
(external, per the mapping's `# external test package: imports root`
annotation): it imports `internal/storage/postgres` for `postgres.SQLDB` and
this package for `gcpfreshnessstore.NewGCPFreshnessStore`.

Membership correction the mapping did not cover (root cause, not
improvisation -- neither file is one of this leaf's three mapped files, but
both broke without a fix once the move landed):

- `store_test.go`'s fake database double: the moved file used root's private
  `fakeExecQueryer`/`queueFakeRows` (`work_queue_lifecycle_test.go`), which a
  different package cannot import. Repointed to the shared
  `internal/storage/postgres/fake` package (`fake.ExecQueryer`, `fake.Rows`):
  `db.queries`/`db.execs` -> `db.Queries`/`db.Execs`,
  `.query`/`.args` -> `.Query`/`.Args`, `rows:` -> `Data:`. Every staged row
  already matched `scanGCPFreshnessTrigger`'s scan exactly, so no
  `fake.LegacyQueueRowAdapter` was needed (same as the AWS leaf).
- `claim_lease_integration_test.go` (moving with this leaf) and
  `freshness_claim_lease_migration_backfill_integration_test.go` (root, stays)
  shared one root-private proof-DB helper pair,
  `freshnessClaimLeaseProofDSNEnv`/`freshnessLeaseProofDB`, previously defined
  in the file this step moves. The moved package kept its copy (already in
  the file being moved, self-contained, comment updated to record the new
  ownership); a second copy of the same helper pair was added to the staying
  backfill test so its consumer still has it. This mirrors the step-18 AWS
  arrangement and the existing repo pattern of each domain integration-test
  file owning its own proof-DB helper.
- Unlike the AWS sibling, this package needs no `bindings_test.go`:
  `gcpcloud/freshness`'s `Trigger.Validate` is self-contained (event kind,
  scope, and asset fields), so `store_test.go` fixtures need no
  scanner-registry `init()`.

New directory doc trio: `doc.go`, `README.md`, `AGENTS.md`, modeled on
`freshness/aws/`.

Root docs needed no GCP repoints: `exported-surface-guide.md`,
`gotchas-and-invariants.md`, and the storage README name no GCP file paths
or symbols, and `internal/coordinator/README.md`'s Freshness reproduction
command already uses the `...` form covering subpackages.

Adding the `freshness/gcp/` directory drops the root file count; dirgate's
`internal/storage/postgres` row in `scripts/lib/dirgate-grandfather.tsv` was
re-pinned to what `bash scripts/verify-dirgate.sh --digest internal/storage/postgres`
prints for this tree, and `tools/golangci-lint-dirgate/grandfather.go`
was regenerated to match. No new sibling-name dirgate violation was flagged
for this move.

## Test-repoint proof (exact names, both packages)

`go test ./internal/storage/postgres/freshness/gcp/ -list '.*'`
lists the moved tests under the new package, while `-list` for the moved
names against `internal/storage/postgres` prints no test names (all gone
from root; only the staying backfill tests remain there).

## Verification runs

From `go/` with `GOTOOLCHAIN=go1.26.6`: `go build ./...` exits zero; `go
vet` on the leaf, the parent tree, `cmd/workflow-coordinator`,
`cmd/webhook-listener`, and `internal/coordinator` exits zero; `go test
./internal/storage/postgres/... -race -count=1` passes (root plus every
subpackage, including the new `freshness/gcp` package); `go build` on both
repointed cmds exits zero; `go test ./cmd/webhook-listener/... -count=1`
passes. `go test -list '.*' ./internal/storage/postgres/freshness/gcp/`
lists all moved tests. `go test ./internal/query -run QueryPlan -count=1`
was not re-run: no moved file's source hash is pinned there (`rg` for
`gcp_freshness` in `queryplan_production_variants_test.go` returns nothing).
From the repo root: `scripts/verify-dirgate.sh --digest
internal/storage/postgres` matches the ledger row; `git diff --check` is
clean; `gofmt -l` on every touched Go file reports nothing;
`scripts/test-verify-performance-evidence.sh` and
`scripts/verify-performance-evidence.sh` both exit zero.

No-Regression Evidence: focused package tests and every repointed caller suite pass on the moved tree, with live proofs skipping cleanly where no database is configured.
No-Observability-Change: package move only, with no metric, span, log field, worker, queue, lease, retry, or runtime-knob edit anywhere in the diff.
