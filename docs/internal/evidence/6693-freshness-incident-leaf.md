# #6693 freshness/incident/ leaf

Change: checklist step 20 of `docs/internal/design/6693-postgres-target-tree.md`.
It moves the three `freshness/incident/` files (plus their one mapped test)
out of root `internal/storage/postgres` into a new
`go/internal/storage/postgres/freshness/incident` leaf. Per decision N4,
this is one of the three `freshness/{aws,gcp,incident}/` trigger stores that
share a `StoreTrigger`/`ClaimQueuedTriggers`/`MarkTriggersHandedOff`/
`MarkTriggersFailed` lifecycle the workflow coordinator and webhook listener
build, and stay together for that reason. This step also removes the three
`incident_freshness_*` naming-violation flags the index-key sweep reports
for the old root basenames.

Package clause: `incidentfreshnessstore` -- the sibling `freshness/aws/`
and `freshness/gcp/` leaves are `awsfreshnessstore` and
`gcpfreshnessstore`, and the naming plan
(`docs/internal/design/6693-postgres-target-tree.md`'s "Package clauses"
bullet) resolves the three `freshness/` children to
`awsfreshnessstore`/`gcpfreshnessstore`/`incidentfreshnessstore` precisely
so they stay distinct. `rg -n '^package incidentfreshnessstore$' go/
--glob '*.go' -l` found no pre-existing collision.

Files moved (production, byte-identical apart from the package clause):
`incident_freshness_schema_sql.go` -> `freshness/incident/schema_sql.go`,
`incident_freshness_sql.go` -> `freshness/incident/sql.go`,
`incident_freshness_store.go` -> `freshness/incident/store.go`. No exported
identifier was renamed; `IncidentFreshnessStore`,
`NewIncidentFreshnessStore`, and `IncidentFreshnessSchemaSQL` were already
exported and moved unchanged. The root files carried a pre-placed
`//nolint:dirgate // #6693 checklist step 20 ... not yet executed` trailer
on the package line; the trailer was stripped on the move since the move is
now executed and the new directory is under the cap (no suppression owed).

Callers repointed (imports/qualifiers only, no behavior change):

- `cmd/workflow-coordinator/main.go`: `postgres.NewIncidentFreshnessStore`
  -> `incidentfreshnessstore.NewIncidentFreshnessStore`, with the new
  package imported unaliased (matching the `freshness/aws` and
  `freshness/gcp` precedents).
- `cmd/webhook-listener/main.go`: same constructor repoint, plus
  `*postgres.IncidentFreshnessStore` ->
  `*incidentfreshnessstore.IncidentFreshnessStore` for the local variable
  declaration. `cmd/webhook-listener/handler.go` needed no change: its
  `incidentFreshnessStore` field is a locally declared interface, not the
  concrete type (same as the AWS/GCP handlers).

`rg` for the old unqualified names (`IncidentFreshnessStore`,
`NewIncidentFreshnessStore`, `IncidentFreshnessSchemaSQL`) and old basenames
across `go/` returns nothing outside the new package, the repointed call
sites above, and the qualified mentions in root's `doc.go`,
`gotchas-and-invariants.md` (both now carry the `incidentfreshnessstore.`
prefix, matching the `awsfreshnessstore.` precedent), and the target-tree
mapping lines (`current -> new`).

Test form: `store_test.go` is `package incidentfreshnessstore`
(in-package, no export needed -- matches the mapping's unannotated line).

Membership correction the mapping did not cover (root cause, not
improvisation -- the test file is mapped, but its fake double is not):
`store_test.go`'s fake database double used root's private
`fakeExecQueryer`/`queueFakeRows` (`work_queue_lifecycle_test.go`), which a
different package cannot import. Repointed to the shared
`internal/storage/postgres/fake` package (`fake.ExecQueryer`, `fake.Rows`):
`db.queries` -> `db.Queries`, `.query` -> `.Query`. Every staged row already
matched `scanIncidentFreshnessTrigger`'s scan exactly, so no
`fake.LegacyQueueRowAdapter` was needed (same as the AWS/GCP leaves).
Unlike the AWS sibling, this package needs no `bindings_test.go`:
`internal/webhook`'s trigger types are self-contained, so `store_test.go`
fixtures need no scanner-registry `init()`. Unlike AWS/GCP, no proof-DB
helper duplication was needed: the incident leaf has no live-Postgres
integration test sharing root helpers.

New directory doc trio: `doc.go`, `README.md`, `AGENTS.md`, modeled on
`freshness/aws/` and `freshness/gcp/`.

Adding the `freshness/incident/` directory drops the root file count;
dirgate's `internal/storage/postgres` row in
`scripts/lib/dirgate-grandfather.tsv` was re-pinned to what `bash
scripts/verify-dirgate.sh --digest internal/storage/postgres` prints for
this tree, and `tools/golangci-lint-dirgate/grandfather.go` was regenerated
to match. The sweep no longer flags the three `incident_freshness_*` root
basenames.

## Test-repoint proof (exact names, both packages)

`go test ./internal/storage/postgres/freshness/incident/ -list '.*'`
lists the moved tests under the new package, while `-list` for the moved
names against `internal/storage/postgres` prints no test names (all gone
from root).

## Verification runs

From `go/` with `GOTOOLCHAIN=go1.26.6`: `go build ./...` exits zero; `go
vet` on the leaf, the parent tree, `cmd/workflow-coordinator`,
`cmd/webhook-listener` exits zero; `go test
./internal/storage/postgres/... -race -count=1` passes (root plus every
subpackage, including the new `freshness/incident` package); `go build` on
both repointed cmds exits zero; `go test ./cmd/webhook-listener/...
-count=1` passes. `go test -list '.*'
./internal/storage/postgres/freshness/incident/` lists all moved tests. `go
test ./internal/query -run QueryPlan -count=1` was not re-run: no moved
file's source hash is pinned there (`rg` for `incident_freshness` in
`queryplan_production_variants_test.go` returns nothing). From the repo
root: `scripts/verify-dirgate.sh --digest internal/storage/postgres`
matches the ledger row; `git diff --check` is clean; `gofmt -l` on every
touched Go file reports nothing;
`scripts/test-verify-performance-evidence.sh` and
`scripts/verify-performance-evidence.sh` both exit zero.

No-Regression Evidence: focused package tests and every repointed caller suite pass on the moved tree, with live proofs skipping cleanly where no database is configured.
No-Observability-Change: package move only, with no metric, span, log field, worker, queue, lease, retry, or runtime-knob edit anywhere in the diff.
