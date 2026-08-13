# internal/storage/postgres/rebuildreset

Clears the Postgres dedup state a graph rebuild-from-facts has to get past
(#4594).

## Why this package exists

Eshu's graph is a projection of Postgres facts. Disaster recovery is therefore
supposed to be: restore Postgres, wipe the graph, `POST
/api/v0/admin/recover-generations` with `all_scopes: true`, wait. That rebuilt
source-local structure — repositories, files, functions, classes, directories —
and stopped there. Everything a reducer domain owns stayed missing.

The cause is that three pieces of Postgres state survive a graph wipe, and each
one independently tells the pipeline the work is already finished:

| State | Why it blocks the rebuild |
| --- | --- |
| Succeeded reducer `fact_work_items` | The re-projection re-derives the same intent ids, and the enqueue is `ON CONFLICT (work_item_id) DO NOTHING`. Every one collides and is dropped. |
| `shared_projection_intents` with `completed_at` set | Partition workers drain only `completed_at IS NULL`, and the upsert's `COALESCE` refuses to reopen a completed row. |
| `graph_projection_phase_state` rows | They assert canonical nodes are committed. After a wipe that is false, and the edge Cypher is `MATCH`-only — so admitted work matches nothing, writes nothing, and still acks `succeeded`. |

The third is the dangerous one: it fails silently and reports success.

## What it does not do

It does not change either dedup guard. Both are correct for ordinary operation,
where shard drains, reopens, and retries all depend on completed work staying
completed. The reset is scoped to the recovery path and to the generations one
refinalize is rebuilding, so ordinary indexing pays nothing for it.

## Exported surface

- `Apply(ctx, tx, filter) (Counts, error)` — runs the three resets inside the
  caller's transaction.
- `Counts` — how many rows each reset touched, surfaced to the operator in the
  `recover-generations` response.
- `ScopePredicate(filter, placeholder)` — the scope predicate. Exported because
  the caller's projector re-enqueue renders the same one; sharing it is what
  stops the enqueue and the resets from selecting different scopes.
- `AffectedGenerationsSubquery` — the `(scope_id, generation_id)` selection the
  three reset statements share.
- `Execer` — the narrow `ExecContext` surface, declared here so the dependency
  runs one way: `postgres` imports `rebuildreset`, never the reverse.

## Invariants

- **Terminal state only.** The reducer delete is scoped to `succeeded`. Claimed
  and running rows hold live leases a rebuild must not yank; `dead_letter` and
  `failed` belong to the replay endpoint and contributed nothing to the pre-wipe
  graph.
- **Delete, do not reset to pending.** A pending row is claimable before the
  projector re-run that owns its inputs has committed anything, which is the same
  silent-incompleteness defect this package exists to fix. Reset-to-pending also
  violates `fact_work_items_container_image_identity_v2_status_check` outright.
- **Reopen shared intents, do not delete them.** The payload is the drain's
  input. A domain that does not re-emit an identical intent would otherwise lose
  its edges entirely.
- **One transaction with the re-enqueue.** A refinalize must not leave the queue
  re-enqueued while its downstream state still says the work is done — that
  half-applied state is invisible until the graph comes back short.
- **All-scopes drops the clause.** It never passes an empty array:
  `scope_id = ANY('{}')` matches no rows, so a rebuild would report success and
  leave the graph empty.

## Known limits

`AffectedGenerationsSubquery` is shared by the three reset statements only. The
projector re-enqueue is an `INSERT ... SELECT` that carries its own copy of the
`FROM` and the two scope guards; it shares `ScopePredicate` but not the guards.
Change `active_generation_id IS NOT NULL` or `status = 'active'` in one place and
you must change it in `../recovery.go` too.

Restoring projector→reducer causality does not buy reducer→reducer ordering. A
cross-repository edge whose intent drains before the second repository's
canonical nodes are committed is still missed on a single pass. See
`docs/internal/evidence/4594-graph-rebuild-from-facts.md`.

## Verification

Live Postgres tests live in the parent package, because they drive the whole
operation through `RecoveryStore.RefinalizeScopeProjections`:

```bash
cd go && go test ./internal/storage/postgres -run RefinalizeRebuildReset -count=1
```

End-to-end: `scripts/verify-graph-rebuild-from-facts.sh`.
