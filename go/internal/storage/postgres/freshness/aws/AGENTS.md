# AGENTS.md — Postgres AWS freshness trigger store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `store.go` for the store, `sql.go` for the statement text, and
   `schema_sql.go` for the DDL.
4. `store_test.go` / `claim_lease_integration_test.go` for the claim/lease
   and fencing contracts.

## Invariants

- Keep the DDL, upsert, claim, reap, and completion SQL byte-identical when
  moving code; predicate or lifecycle changes need their own issue with
  EXPLAIN and contention proof per `eshu-postgres-rigor`.
- Keep `ClaimQueuedTriggers`' `claim_expires_at` lease assignment and
  `ReapExpiredTriggerClaims`' `FOR UPDATE SKIP LOCKED` + `claim_expires_at <
  $1` predicate: this is what recovers a trigger stranded at `claimed` by a
  mid-batch handoff abort or coordinator crash (#4576), and what stops a reap
  from racing a still-live claim-holder within its lease window.
- Keep `MarkTriggersHandedOff`/`MarkTriggersFailed`'s `claim_fencing_token`
  fencing predicate: it stops a stale claimant whose lease was reaped and
  re-claimed by a different owner from completing a claim it no longer holds
  (#4576, raised in PR #4682 review).
- Keep the package clause as `package awsfreshnessstore`: `cloud/aws` already
  uses `awsstore`, and this package's own `freshness` last-directory-word
  would collide with the sibling `freshness/gcp/` and `freshness/incident/`
  clauses, so the naming plan (#6693 decision N4) adds the parent word.
  Callers import the `storage/postgres/freshness/aws` path without an alias.
- Never import the parent `postgres` package from here: `cmd/webhook-listener`
  and `cmd/workflow-coordinator` import this package directly, and importing
  back would be a cycle through their own imports of `postgres`.
- Tests here use `internal/storage/postgres/fake` (`fake.ExecQueryer`,
  `fake.Rows`) for the fake database double, not a package-private
  `fakeExecQueryer`/`queueFakeRows`: those stayed unexported test-file code in
  root and cannot be imported from here.
- `bindings_test.go`'s blank import of `awscloud/awsruntime/bindings` is
  required: `store_test.go` builds fixtures through
  `freshness.NewStoredTrigger`, which validates `ServiceKind` against the AWS
  scanner registry, and that registry is empty until a runtimebind package's
  `init()` runs. Root keeps its own copy (`aws_bindings_test.go`) for tests
  that still live there; Go test binaries are built and initialized per
  package, so each needs its own registration.

## Common changes

- Change the DDL only with `TestAWSFreshnessSchemaDefinesCoalescingKeys`'
  expected fragments updated in lockstep.
- Change a claim, reap, or completion predicate only with the fencing tests
  (`store_test.go`) and the live-Postgres proofs
  (`claim_lease_integration_test.go`) updated, plus a contention/idempotency
  proof for the affected `ON CONFLICT`/`UPDATE`.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Loosening the `claim_expires_at` lease or dropping `FOR UPDATE SKIP LOCKED`
  from the reap query reintroduces the #4576 stuck-claim bug or lets two
  concurrent reap passes double-reclaim the same row.
- Dropping the `claim_fencing_token` match from `MarkTriggersHandedOff`/
  `MarkTriggersFailed` lets a stale claimant complete a claim a different
  owner already re-claimed.
- Forgetting `bindings_test.go` (or its blank import) makes any test that
  builds a fixture through `freshness.NewStoredTrigger` panic with
  "unsupported service_kind" instead of failing an assertion.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/freshness/aws/... -race -count=1
go vet ./internal/storage/postgres/freshness/aws/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
