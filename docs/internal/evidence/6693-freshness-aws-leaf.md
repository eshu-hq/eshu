# #6693 freshness/aws/ leaf

Change: checklist step 18 of `docs/internal/design/6693-postgres-target-tree.md`.
It moves the three `freshness/aws/` files (plus their two mapped tests) out of
root `internal/storage/postgres` into a new
`go/internal/storage/postgres/freshness/aws` leaf. Per decision N4, this is
one of the three `freshness/{aws,gcp,incident}/` trigger stores that share a
`StoreTrigger`/`ClaimQueuedTriggers`/`ReapExpiredTriggerClaims`/
`MarkTriggersHandedOff`/`MarkTriggersFailed` lifecycle the workflow
coordinator and webhook listener build, and stay together for that reason.

Package clause: `awsfreshnessstore`, not `awsstore` -- `cloud/aws/` already
holds `package awsstore` (checklist step already merged), and the naming
plan (`docs/internal/design/6693-postgres-target-tree.md`'s "Package clauses"
bullet) resolves the three `freshness/` children to
`awsfreshnessstore`/`gcpfreshnessstore`/`incidentfreshnessstore` precisely so
they do not collide with `cloud/aws` (`awsstore`) or `incident/`
(`incidentstore`). `rg -n '^package awsfreshnessstore$' go/ --glob '*.go' -l`
found no pre-existing collision.

Files moved (production, byte-identical apart from the package clause):
`aws_freshness_schema_sql.go` -> `freshness/aws/schema_sql.go`,
`aws_freshness_sql.go` -> `freshness/aws/sql.go`,
`aws_freshness_store.go` -> `freshness/aws/store.go`. No exported identifier
was renamed; `AWSFreshnessStore`, `NewAWSFreshnessStore`, and
`AWSFreshnessSchemaSQL` were already exported and moved unchanged.

Callers repointed (imports/qualifiers only, no behavior change):

- `cmd/workflow-coordinator/main.go`: `postgres.NewAWSFreshnessStore` ->
  `awsfreshnessstore.NewAWSFreshnessStore`, with the new package imported
  unaliased (matching `storage/postgres/cloud/aws`'s own `awsstore`
  precedent).
- `cmd/webhook-listener/main.go`: same constructor repoint, plus
  `*postgres.AWSFreshnessStore` -> `*awsfreshnessstore.AWSFreshnessStore` for
  the local variable declaration. `cmd/webhook-listener/handler.go` needed no
  change: its `awsFreshnessStore` field is a locally declared interface, not
  the concrete type.
- `internal/storage/postgres/freshness_claim_lease_migration_backfill_integration_test.go`
  (root, stays in root -- not part of this leaf's mapping): its call site
  `NewAWSFreshnessStore(SQLDB{DB: db})` (same-package, unqualified) became
  `awsfreshnessstore.NewAWSFreshnessStore(SQLDB{DB: db})` with the new package
  imported; `SQLDB` stays unqualified since this file is still `package
  postgres`.

`rg` for the old unqualified names (`AWSFreshnessStore`, `NewAWSFreshnessStore`,
`AWSFreshnessSchemaSQL`) across `go/` returns nothing outside the new package,
the three repointed call sites above, and `gcp_freshness_claim_lease_integration_test.go`'s
doc-comment cross-references to the AWS test names (comment text, not a
symbol reference).

Test form: `store_test.go` is `package awsfreshnessstore` (in-package, no
export needed -- matches the mapping's unannotated line).
`claim_lease_integration_test.go` is `package awsfreshnessstore_test`
(external, per the mapping's `# external test package: imports root`
annotation): it imports `internal/storage/postgres` for `postgres.SQLDB` and
this package for `awsfreshnessstore.NewAWSFreshnessStore`.

Membership correction the mapping did not cover (root cause, not
improvisation -- neither file is one of this leaf's three mapped files, but
both broke without a fix once the move landed):

- `store_test.go`'s fake database double: the moved file used root's private
  `fakeExecQueryer`/`queueFakeRows` (`work_queue_lifecycle_test.go`), which a
  different package cannot import. Repointed to the shared
  `internal/storage/postgres/fake` package (`fake.ExecQueryer`, `fake.Rows`):
  `db.queries`/`db.execs` -> `db.Queries`/`db.Execs`,
  `.query`/`.args` -> `.Query`/`.Args`. Every staged row already matched
  `scanAWSFreshnessTrigger`'s 16-destination scan exactly, so no
  `fake.LegacyQueueRowAdapter` was needed.
- `claim_lease_integration_test.go` and `gcp_freshness_claim_lease_integration_test.go`
  (root, GCP's own leaf has not moved yet) and
  `freshness_claim_lease_migration_backfill_integration_test.go` (root) all
  shared one root-private proof-DB helper pair,
  `freshnessClaimLeaseProofDSNEnv`/`freshnessLeaseProofDB`, previously defined
  in the file this step moved. Moving that file without addressing the
  helper broke both remaining root files (`go vet` failure:
  `undefined: freshnessClaimLeaseProofDSNEnv`,
  `undefined: freshnessLeaseProofDB`). The moved package kept its own copy
  (already in the file being moved, self-contained); a second copy of the
  same helper pair was added to `gcp_freshness_claim_lease_integration_test.go`
  so the two remaining root consumers still have it. This mirrors the
  existing repo pattern of each domain integration-test file owning its own
  proof-DB helper rather than sharing one across domains (see
  `webhook_refresh_proof_integration_test.go`'s own `webhookRefreshProofDSNEnv`,
  and three more independently named `*ProofDSNEnv` helpers already in root).
- `store_test.go` calls `freshness.NewStoredTrigger`, which validates
  `ServiceKind` through `awsruntime.SupportsServiceKind`; the registry is
  populated only by a runtimebind package's `init()`. Root supplied this via
  `aws_bindings_test.go`'s blank import, which stays in root per the mapping
  doc's own row (`root.md:19`, `# no production references`) but cannot be
  imported cross-package. Confirmed by a RED/GREEN run: without a copy, `go
  test ./internal/storage/postgres/freshness/aws/...` panics
  `unsupported service_kind "lambda"`; a new `freshness/aws/bindings_test.go`
  with the same blank import (documented as package-scoped, not
  duplicated-by-mistake) fixes it. Go test binaries are built and initialized
  per package, so this duplication is unavoidable, not a mapping error.

New directory doc trio: `doc.go`, `README.md`, `AGENTS.md`, modeled on
`vulnerability/` and `cloud/aws/`.

Root docs updated to qualify the moved exported symbols with the new package
(matching the existing `webhookstore.`/`vulnerabilitystore.` precedent in the
same files): `exported-surface-guide.md` and `gotchas-and-invariants.md`.
`docs/internal/design/1286-postgres-ownership-inventory.md`'s "Sources used"
list (already tracking prior moves like `cloud/aws/scan_status.go`) was
repointed from `aws_freshness_schema_sql.go` to
`freshness/aws/schema_sql.go`. `docs/internal/design/storage-migration-hardening.md`'s
`fmt.Sprintf` gosec note was repointed from `aws_freshness_store.go:211,218,248`
to `freshness/aws/store.go` (no line-number suffix, per prior review
feedback). `internal/coordinator/README.md`'s No-Regression-Evidence
reproduction command was widened from
`go test ./internal/storage/postgres -run 'Freshness' ...` to
`go test ./internal/storage/postgres/... -run 'Freshness' ...` since the AWS
freshness tests it names no longer live under root alone.
`go/internal/storage/postgres/migrations/076_crossplane_satisfied_by_redrive_state.sql`'s
comment still names `aws_freshness_sql.go` by its old path -- left unchanged,
since migration SQL files are excluded from this move (checksums).

Adding the `freshness/aws/` directory drops the root file count; dirgate's
`internal/storage/postgres` row in `scripts/lib/dirgate-grandfather.tsv` was
re-pinned to what `bash scripts/verify-dirgate.sh --digest internal/storage/postgres`
prints for the rebased tree, and `tools/golangci-lint-dirgate/grandfather.go`
was regenerated to match. No new sibling-name dirgate violation was flagged
for this move.

No-Regression Evidence: the DDL, the coalescing upsert, the claim/reap/
completion statements (including `FOR UPDATE SKIP LOCKED` and the
`claim_fencing_token` fencing predicates), and every helper body are
byte-identical -- no SQL text, predicate, lock, lease, or worker-count
change. After the final edit, from `go/`: `gofumpt -l -w` on every changed
file reports no diffs; `go build ./...` exits 0; `go vet ./...` exits 0;
`go vet -tags "integration perf5854_ack perf5740_completion perf6785_wait"
./internal/storage/postgres/...` exits 0; `go test ./internal/storage/postgres/...
-race -count=1` passes (root plus every subpackage, including the new
`freshness/aws` package); `go build ./cmd/workflow-coordinator/...
./cmd/webhook-listener/...` exits 0; `go test ./cmd/webhook-listener/...
-count=1` passes. `go test -list '.*' ./internal/storage/postgres/freshness/aws/`
lists all ten moved tests. For each of the ten
(`TestAWSFreshnessSchemaDefinesCoalescingKeys`,
`TestAWSFreshnessStoreStoreTriggerUpsertsByFreshnessKey`,
`TestAWSFreshnessStoreClaimQueuedTriggersUsesSkipLocked`,
`TestAWSFreshnessStoreClaimQueuedTriggersRequiresPositiveLease`,
`TestAWSFreshnessStoreReapExpiredTriggerClaimsUsesSkipLockedAndLeaseExpiry`,
`TestAWSFreshnessStoreReapExpiredTriggerClaimsRequiresPositiveLimit`,
`TestAWSFreshnessStoreMarkTriggersHandedOffUsesIndividualIDParameters`,
`TestAWSFreshnessStoreReapExpiredTriggerClaimsIntegration`,
`TestAWSFreshnessStoreReapExpiredTriggerClaimsConcurrentSafety`,
`TestAWSFreshnessStoreStaleHolderCannotCompleteReapedClaimIntegration`):
`go test ./internal/storage/postgres/freshness/aws/... -list "^NAME\$" -count=1 | rg -q "^NAME\$"`
exits 0, and the same command against `./internal/storage/postgres` exits 1
for every one of the ten, proving the tests moved rather than being
duplicated or dropped. `go test ./internal/query -run QueryPlan -count=1`
was not re-run: no moved file's source hash is pinned there (`rg` for
`aws_freshness` in `queryplan_production_variants_test.go` returns nothing).

No-Observability-Change: the store executes bounded SQL through its injected
database handle with no metric, span, or log of its own before or after the
move. `cmd/webhook-listener` and `cmd/workflow-coordinator` construct the
same `InstrumentedDB` wrapper (`StoreName: "aws_freshness_triggers"`) around
it as before; neither changed.
