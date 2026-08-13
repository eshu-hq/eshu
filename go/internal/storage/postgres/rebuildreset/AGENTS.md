# AGENTS.md — internal/storage/postgres/rebuildreset guidance for LLM assistants

## Read first

1. `README.md` in this directory — why the package exists and what each reset is
   for
2. `reset.go` — the three SQL templates, `AffectedGenerationsQuery`, `Apply`
3. `../recovery.go` — `RecoveryStore.RefinalizeScopeProjections`, the only
   caller, and the projector re-enqueue that runs in the same transaction
4. `docs/internal/evidence/4594-graph-rebuild-from-facts.md` — the measured
   before/after, and the failures that are still open
5. `docs/public/operate/graph-rebuild-from-facts.md` — the operator runbook

## Invariants you must not break

- **Never widen the reducer delete past `succeeded`.** Claimed and running rows
  hold live leases. Deleting one lets a second worker claim the same conflict key
  while the first is still executing.
- **Never change the delete to a status reset.** Two independent reasons, both
  measured: a pending row is claimable before its producer has re-run, and the
  rewrite violates `fact_work_items_container_image_identity_v2_status_check`.
  Both are recorded in the evidence doc; do not rediscover them.
- **Never delete shared projection intents.** Clear `completed_at`. The payload
  is the drain's input.
- **Never let the all-scopes path pass an empty array.** `ANY('{}')` matches no
  rows and the rebuild would report success over an empty graph. Drop the clause.
- **Never let a statement re-read `ingestion_scopes`.** The refinalize
  transaction is READ COMMITTED, so a second read is a second snapshot. An
  ingester that activates a generation mid-refinalize would then have the enqueue
  rebuild G1 while a reset cleared G2 — G1 left deduplicated and never rebuilt,
  G2 damaged and never replayed. Read the set once with
  `AffectedGenerationsQuery` and bind the arrays.
- **Never fix a dedup problem by editing `../reducer_queue.go` or
  `../shared_intents_upsert.go`.** Both guards are correct for ordinary
  operation, and the whole design of this package is that recovery pays the cost
  instead of the hot path. If you think a guard is wrong, that is a separate
  change with its own proof.
- **Do not import the parent `postgres` package.** `Execer` is declared here on
  purpose so the dependency runs one way. An import back would be a cycle.

## When you change the scope guards

`AffectedGenerationsTemplate` is the only statement in a refinalize that reads
`ingestion_scopes`, so it is the only place the scope guards live. The projector
re-enqueue in `../recovery.go` and the three resets here all bind the
`Generations` it returned. Change a guard and every statement follows, because
none of them selects anything.

If you find yourself adding a `FROM ingestion_scopes` to any other statement,
stop: that is the defect this shape exists to prevent.

## Verification expected on any change here

This is Postgres SQL on the disaster-recovery path, so `eshu-postgres-rigor` and
`concurrency-deadlock-rigor` both apply.

```bash
cd go && go test ./internal/storage/postgres -run RefinalizeRebuildReset -count=1
cd go && go test ./internal/storage/postgres -run RefinalizeAllScopes -count=1
```

The `RefinalizeRebuildReset` tests need a live Postgres; they skip without one,
so confirm they actually RAN rather than reading a green skip as a pass.

End-to-end proof is `scripts/verify-graph-rebuild-from-facts.sh`. It exits
non-zero today for reasons that are not this package's — read the evidence doc's
"What still does not match" section before treating a red run as your
regression.

## What is still open

Cross-repository edge ordering. This package restores projector→reducer
causality; it does not order reducer against reducer. A `CALLS` edge into another
repository can still be missed on a single pass and recovered by a second
refinalize. Do not paper over that here with a retry loop — it belongs in the
readiness gate.
