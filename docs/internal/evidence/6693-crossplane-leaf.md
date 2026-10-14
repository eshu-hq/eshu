# #6693 crossplane/ leaf

Change: checklist step 23 of `docs/internal/design/6693-postgres-target-tree.md`.
It moves the five `crossplane/` files (plus their five mapped tests) out of
root `internal/storage/postgres` into a new
`go/internal/storage/postgres/crossplane` leaf. The sweep discovers OTHER
scopes' active, unresolved K8sResource Claims matching one XRD's (group,
claim_kind) identity and re-enqueues each target scope's SATISFIED_BY
materialization intent, closing the XRD-lag false-negative window (#5476).
Durable claim/completion per (xrd_scope_id, xrd_generation_id) uses
`FOR UPDATE SKIP LOCKED` convergence with fencing-token completion, and the
already-satisfied ledger is keyed by (target scope, group, claim_kind)
identity, written only by the reducer handler after it commits an edge.

Package clause: `crossplanestore` -- plain directory word plus `store`, with
no pre-existing `^package crossplanestore$` collision in `go/`. Callers
import the `storage/postgres/crossplane` path without an alias (the path's
last element differs from the package name, so no alias is needed and none
is used).

Files moved (byte-identical apart from the package clause; no exported
identifier renamed, no SQL or lifecycle change):

- `crossplane_satisfied_by_redrive_ledger.go` -> `crossplane/ledger.go`
- `crossplane_satisfied_by_redrive_query.go` -> `crossplane/query.go`
- `crossplane_satisfied_by_redrive_state.go` -> `crossplane/state.go`
- `crossplane_satisfied_by_redrive_sweep.go` -> `crossplane/sweep.go`
- `crossplane_satisfied_by_redrive_sweep_batch.go` ->
  `crossplane/sweep_batch.go`

No production export was needed: every staying production caller uses
already-exported types (`CrossplaneSatisfiedByRedriveSweeper`,
`CrossplaneRedriveStateStore`, `CrossplaneRedriveTargetLedgerStore`,
`CrossplaneRedriveSweepResult`). The staying
`projector_queue_crossplane_redrive_hook.go` (step 27's `queue/projector/`
move) keeps the `CrossplaneRedriveSweeper` interface and qualifies the
result type; root imports the leaf, never the reverse.

Test form: all five mapped tests are `package crossplanestore_test`
(external). They must be: every one uses root symbols (`NewFactStore`,
`NewReducerQueue`, `SQLDB`/`SQLQueryer`, `ApplyBootstrap`) and root now
imports the leaf. The shared proof-schema/seed/assertion helpers stay
defined once across the external test files (Go test-only symbols do not
cross package boundaries). `export_test.go` shims the only two private
production symbols external tests need
(`ListCrossplaneRedriveTargetScopesQueryForTest`,
`CrossplaneRedriveOutcomeCompletedForTest`); nothing is hand-copied.

Mapping correction the plan did not cover: the mapping lists
`crossplane_satisfied_by_redrive_query_plan_live_test.go` with no test-form
note, but it shares the external proof-schema/seed helpers and EXPLAINs
the shipped query const, so it moves as external plus the shim like the
other four. The `cloud.md` mapping line now says so (same precedent as the
two corrected lines in steps 2-4).

Callers repointed (imports/qualifiers only, no behavior change):

- `cmd/projector/runtime_wiring.go`: sweeper literal and constructor
  repointed to `crossplanestore.*`.
- `cmd/projector/crossplane_redrive_catchup.go` and
  `crossplane_redrive_catchup_test.go`: `CrossplaneRedriveSweepResult`
  repointed; the catch-up files now import the leaf instead of root.
- `cmd/reducer/wiring_handlers.go`: target-ledger constructor repointed.
- `go/internal/storage/postgres/projector_queue_crossplane_redrive_hook.go`
  and its test: result type qualified with `crossplanestore.`.
- `go/internal/payloadusage/rawpayload.go`: exemption path repointed to
  `crossplane/sweep.go` (same "exemption follows the code" precedent as the
  catalog move).
- `specs/live-tests.d/live-tests-05.yaml`: five ledger rows repointed.
- `go/internal/reducer/crossplane/README.md`,
  `docs/public/languages/crossplane.md`,
  `docs/public/observability/telemetry-coverage.md`: file pointers
  repointed (path column only; no signal change).
- Root `doc.go`, `README.md`, `AGENTS.md`: the crossplane sections moved
  into the leaf trio (same full-removal precedent as the maintenance and
  scope/completion leaves; no pointer left behind).
- Target-tree checklist step 23 ticked.

Wiring check (issue Order step 2): root never constructs these stores.
`NewCrossplaneRedriveStateStore` is called from `cmd/projector` and
`cmd/reducer` only; the sweeper is built in `cmd/projector` and injected
into `ProjectorQueue.CrossplaneRedrive`. No wiring moves up. The five
files' only non-stdlib imports are `db` and `telemetry`, both already
leaves, so the package graph gains no edge back into root.

`rg` for the old unqualified type names and old basenames across `go/`,
`docs/`, `specs/`, and `.github` returns nothing outside the new package,
the repointed call sites above, the historical evidence notes, and the
target-tree mapping lines (`current -> new`). The remaining
`crossplane_satisfied_by_redrive_state` hit in `schema_order_test.go` is a
migration table name, not a file path. No workflow names these files, so
no Ifa trigger update was needed.

New directory doc trio: `doc.go`, `README.md`, `AGENTS.md`, modeled on the
maintenance and scope/completion leaves.

Adding the `crossplane/` directory drops the root file count; dirgate's
`internal/storage/postgres` row in `scripts/lib/dirgate-grandfather.tsv`
was re-pinned to what `bash scripts/verify-dirgate.sh --digest
internal/storage/postgres` prints for this tree, and
`tools/golangci-lint-dirgate/grandfather.go` was regenerated to match.
The live-tests ledger's five `scheduled` rows moved to the new paths with
their class and reason unchanged; the `legacy_scheduled_exemptions`
`sha256` in `specs/live-tests.v1.yaml` was re-pinned to the verifier's
recomputed digest with `count` unchanged at 572 (same 572 proofs, five
renamed paths under the approved #6693 move; no exemption added or
removed).

## Test-repoint proof (exact names, both packages)

`go test ./internal/storage/postgres/crossplane/ -list '.*'`
lists the moved tests under the new package, while `-list` for the moved
names against `internal/storage/postgres` prints no test names (all gone
from root).

## Verification runs

From `go/`:
`go build ./internal/storage/postgres/... ./cmd/projector/...
./cmd/reducer/...` exits zero; `go vet` on the leaf, the parent tree,
`internal/payloadusage`, `internal/reducer/crossplane`, `cmd/projector`,
and `cmd/reducer` exits zero; `go test
./internal/storage/postgres/crossplane/... -count=1` passes (live tests
skip without `ESHU_CROSSPLANE_REDRIVE_PROOF_DSN`; the passing gate is that
all five external test files compile against the new package and the
shim); `go test ./internal/storage/postgres/ -run
'TestProjectorQueueAckInvokesCrossplaneRedriveHookAfterCommit|TestProjectorQueueAckSkipsHookWhenNilCrossplaneRedrive'
-count=1` passes; `go test ./cmd/projector/... -count=1` passes; `go test
./internal/payloadusage/... ./internal/reducer/crossplane/... -count=1`
passes. `go test -list '.*' ./internal/storage/postgres/crossplane/`
lists all 9 moved tests; the same `-list` for the moved names against
`internal/storage/postgres` prints only the 2 staying hook tests.
`gofmt -l` and `gofumpt -l` on every touched Go file report nothing;
`scripts/verify-license-header.sh` passes; `git diff --check` is clean.

No-Regression Evidence: pure move (package clauses, imports, qualifiers,
doc/prose relocation only); every statement byte-identical, so the
committed query-plan, concurrency, crash-recovery, and behavior proofs
hold unchanged and move with the code.
No-Observability-Change: no metric, span, log field, worker, queue, lease,
retry, or runtime-knob edit anywhere in the diff; telemetry-coverage rows
change path column only.
