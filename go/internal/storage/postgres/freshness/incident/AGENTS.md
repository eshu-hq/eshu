# AGENTS.md — Postgres incident freshness trigger store guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../AGENTS.md` for Postgres storage conventions.
3. `store.go` for the store, `sql.go` for the statement text, and
   `schema_sql.go` for the DDL.
4. `store_test.go` for the claim/lease and fencing contracts.

## Invariants

- Keep the DDL, upsert, claim, and completion SQL byte-identical when moving
  code; predicate or lifecycle changes need their own issue with EXPLAIN and
  contention proof per `eshu-postgres-rigor`.
- Keep `ClaimQueuedTriggers`' `claim_expires_at` lease assignment and
  `FOR UPDATE SKIP LOCKED`: this is what recovers a trigger stranded at
  `claimed` by a mid-batch handoff abort or coordinator crash, and what
  stops a reap from racing a still-live claim-holder within its lease
  window.
- Keep `MarkTriggersHandedOff`/`MarkTriggersFailed`'s `claim_fencing_token`
  fencing predicate: it stops a stale claimant whose lease was reaped and
  re-claimed by a different owner from completing a claim it no longer holds.
- Keep the package clause as `package incidentfreshnessstore`: the sibling
  `freshness/aws/` and `freshness/gcp/` leaves are `awsfreshnessstore` and
  `gcpfreshnessstore`, so the naming plan (#6693 decision N4) adds the
  parent word to keep the three `freshness/` children distinct. Callers
  import the `storage/postgres/freshness/incident` path without an alias.
- Never import the parent `postgres` package from here:
  `cmd/webhook-listener` and `cmd/workflow-coordinator` import this package
  directly, and importing back would be a cycle through their own imports of
  `postgres`.
- Tests here use `internal/storage/postgres/fake` (`fake.ExecQueryer`,
  `fake.Rows`) for the fake database double, not a package-private
  `fakeExecQueryer`/`queueFakeRows`: those stayed unexported test-file code in
  root and cannot be imported from here.
- Unlike the AWS sibling, this package needs no `bindings_test.go`:
  `internal/webhook`'s trigger types are self-contained, so `store_test.go`
  fixtures need no scanner-registry `init()`.

## Common changes

- Change the DDL only with
  `TestIncidentFreshnessSchemaDefinesCoalescingKeys`' expected fragments
  updated in lockstep.
- Change a claim or completion predicate only with the fencing tests
  (`store_test.go`) updated, plus a contention/idempotency proof for the
  affected `ON CONFLICT`/`UPDATE`.

## Failure modes

- Importing the parent `postgres` package creates an import cycle.
- Loosening the `claim_expires_at` lease or dropping `FOR UPDATE SKIP LOCKED`
  from the claim query reintroduces stuck-claim rows or lets two concurrent
  claim passes double-claim the same row.
- Dropping the `claim_fencing_token` match from `MarkTriggersHandedOff`/
  `MarkTriggersFailed` lets a stale claimant complete a claim a different
  owner already re-claimed.

## Verification

From `go/`, run:

```bash
go test ./internal/storage/postgres/freshness/incident/... -race -count=1
go vet ./internal/storage/postgres/freshness/incident/...
```

Run `scripts/verify-package-docs.sh` from the repository root.
