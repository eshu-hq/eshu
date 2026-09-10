# internal/storage/postgres/rebuildreset

Clears the Postgres dedup state a graph rebuild-from-facts has to get past
(#4594).

## Why this package exists

Eshu's graph is a projection of Postgres facts. Disaster recovery is therefore
supposed to be: restore Postgres, wipe the graph, `POST
/api/v0/admin/recover-generations` with `all_scopes: true`, wait. That rebuilt
source-local structure — repositories, files, functions, classes, directories —
and stopped there. Everything a reducer domain owns stayed missing.

The cause is that four pieces of Postgres state survive a graph wipe, and each
one independently tells the pipeline the work is already finished:

| State | Why it blocks the rebuild |
| --- | --- |
| Succeeded reducer `fact_work_items` | The re-projection re-derives the same intent ids, and the enqueue is `ON CONFLICT (work_item_id) DO NOTHING`. Every one collides and is dropped. |
| `shared_projection_intents` with `completed_at` set | Partition workers drain only `completed_at IS NULL`, and the upsert's `COALESCE` refuses to reopen a completed row. |
| `graph_projection_phase_state` rows | They assert canonical nodes are committed. After a wipe that is false, and the edge Cypher is `MATCH`-only — so admitted work matches nothing, writes nothing, and still acks `succeeded`. |
| Active `relationship_generations` | The phase wipe does not touch them, so the re-projection's resolved read keeps serving the prior wave's rows as current truth. |

The third is the dangerous one: it fails silently and reports success. The
fourth gets a fence of its own (see `refinalize.go`): retirement commits only
over drained reducer leases, so a running resolver cannot re-activate its
generation stale.

## What it does not do

It does not change either dedup guard. Both are correct for ordinary operation,
where shard drains, reopens, and retries all depend on completed work staying
completed. The reset is scoped to the recovery path and to the generations one
refinalize is rebuilding, so ordinary indexing pays nothing for it.

## Exported surface

- `AffectedGenerationsQuery(filter) (string, []any)` — the read that materializes
  the `(scope_id, generation_id)` set one refinalize covers. The caller runs it
  once, first, inside its transaction.
- `Generations` — that set, held as two index-aligned arrays. Build it with
  `Append`; `Args` hands it to a statement.
- `Apply(ctx, tx, generations) (Counts, error)` — runs the four resets inside
  the caller's transaction, against the set it was given.
- `ReadAffectedGenerations`, `EnqueueProjectorWork`, `WaitForReducerDrain`,
  `AssertRetirementFenced` — the ordered coordination prelude the caller runs
  in its transaction before `Apply`: read the set once, wait out in-flight
  reducer leases (bounded), re-enqueue projector work, then confirm the
  retirement actually committed. `InflightReducersError` is the abort signal.
  `Queryer`/`Rows` are narrow local interfaces so this package never imports
  its caller.
- `Counts` — how many rows each reset touched, surfaced to the operator in the
  `recover-generations` response.
- `Execer` — the narrow `ExecContext` surface, declared here so the dependency
  runs one way: `postgres` imports `rebuildreset`, never the reverse.

## Invariants

- **Terminal state only.** The reducer delete is scoped to `succeeded`. Claimed
  and running rows hold live leases a rebuild must not yank; `dead_letter` and
  `failed` belong to the replay endpoint and contributed nothing to the pre-wipe
  graph.
- **Retire only over drained reducers.** The generation retirement carries an
  atomic live-lease guard: it commits only when no reducer row holds a live
  lease (`claimed`/`running` with `claim_until > now()`) on the refinalized
  pairs. A resolver that claimed before the refinalize would otherwise get its
  generation retired mid-flight, then re-activate it with stale rows while its
  success ack dedupes the re-emitted intent (Codex #6184 P1). The caller waits
  out the drain first and aborts past its bound; the guard closes the
  poll-to-commit window in the same statement. Expired leases are reclaimable,
  not in-flight: whoever reclaims such a row resolves post-retirement, which is
  the legitimate re-projection direction, so crashed workers never wedge
  recovery.
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
- **One read of `ingestion_scopes` per refinalize.** The transaction is READ
  COMMITTED, so every statement that reads that table gets its own snapshot. Two
  reads mean an ingester activating a generation mid-refinalize can have the
  enqueue rebuild G1 while a reset clears G2's dedup state: G1 stays
  deduplicated and never rebuilds, G2 loses state it will never replay. The read
  happens once and every statement binds its arrays.

## Known limits

The read takes no row locks, so an activation racing a rebuild wins and simply
lands outside that refinalize — the operator rebuilds the generation that was
active when the command ran. Locking `ingestion_scopes` instead would put an
ingester behind a whole-deployment rebuild and buy no truth.

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
