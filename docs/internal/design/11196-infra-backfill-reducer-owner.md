# Infra read model backfill: reducer owner (#11196)

Refs #11196, #10860, #11170, #6793.

**The reducer starts the infra read model backfill, and every replica races on
the existing per-repository lock.** This note records the owner decision, the
lock and ordering analysis written before the code, and the open-question
answers the issue asks for.

## Why this exists

`inventory.Backfiller` is the only writer of the marker row
`infra_resource_entities_v2` (`backfillMarkSQL` in
`go/internal/storage/postgres/infra/inventory/backfill.go`). Its only caller,
the API startup path, was deleted with the in-tree API (a012da4557). A fresh
install, a wiped database, or a future `BackfillMarker` bump never records the
marker. Readers then stay on the per-label graph path, and the reducer's own
reconcile cycle returns `Ready=false` until the marker exists, so it cannot
record it either. No wrong result follows; the cost is performance and a
`/admin/status` `infra_inventory` state of `backfilling` that never ends.

## Owner

The arbiter ruling for #11170 (`arbiter-11170`, 2026-11-22) names the reducer,
next to `infraInventoryReconcileRunnerFor`. core-api and bootstrap-data-plane
were rejected. The reducer already hosts the reconcile runner and the
config-artifact walker, which records its own backfill marker.

The new runner is `infraInventoryBackfillRunner` in
`go/cmd/reducer/infra_inventory_backfill_wiring.go`. It calls
`inventory.RunBackground`, which already retries with a doubling backoff,
counts every attempt in `eshu_dp_infra_inventory_backfill_runs_total`, and
exits once the marker exists. `reducer.Service` starts it beside the other side
runners (`startSideRunners` in `go/internal/reducer/service_side_runners.go`).

The runner returns nil when its context ends. `Service.Run` cancels every other
component when any side runner returns an error, so a shutdown-time
`context.Canceled` must not surface as a failure.

## Concurrency design

### What one backfill pass takes

`Backfiller.Run` lists repositories, then calls `MirrorRepo` once per
repository. `MirrorRepo` is one transaction:

1. `pg_advisory_xact_lock(hashtextextended('infra_resource_entities:' || repo, 0))`,
   the first statement (`deriveLockSQL` in `derive.go`).
2. `clearDirtySQL`, a row delete in `infra_resource_entity_dirty_repos`.
3. `DELETE` then `INSERT ... SELECT ... ON CONFLICT (entity_id) DO UPDATE` on
   `infra_resource_entities`.
4. Commit, which releases the lock.

After the last repository, a separate statement inserts the marker with
`ON CONFLICT (marker_name) DO NOTHING`. It holds no repository lock.

### Lock ordering

A transaction holds exactly one advisory lock, taken first, and never waits for
a second lock while holding it. The live content writer, the reconcile runner,
retention and the backfill all use the same lock key and the same
lock-first shape. With one lock per transaction there is no lock-order cycle
among replicas or against the other derive paths, so an advisory-lock deadlock
cannot form. A row-level wait is still possible between the `INSERT ... ON
CONFLICT (entity_id) DO UPDATE` of two transactions that touch the same
entity_id, but the advisory lock serializes every writer of one repository, so
two transactions never reach those rows for the same repository at once. An
entity_id that moves between repositories is the one cross-repository case;
`insertConflictClause` handles it and it is outside this change.

The marker insert is a single-row `ON CONFLICT DO NOTHING`. Two replicas that
finish together race on the primary key; one inserts, the other no-ops. The
marker row is therefore written once and keeps its first `completed_at`.

### Fork: single owner by claim, or every replica races

| | Every replica races (chosen) | Single owner by claim |
| --- | --- | --- |
| Mechanism | each reducer runs `RunBackground`; the per-repository lock and the idempotent marker already make that safe | a try-lock or lease row picks one runner; the others wait |
| Extra state | none | a session-level lock on a pinned connection, or a new lease table |
| Failure of the owner | none; any replica finishes | a dead owner needs lease expiry before another starts |
| Duplicate work | up to N full passes on a fresh install, serialized per repository | one pass |
| Precedent | the API and MCP server both ran it (the removed `StartInfraInventoryBackfill`) | the secret-lines repairer, which needed a dedicated lock pool |

The #11170 ruling states the acceptance as "the per-repository lock and the
idempotent marker hold under concurrent reducers", which presupposes racing
replicas. The duplicate cost is a one-time cost, bounded by the replica count,
and the passes serialize per repository, so they do not run at once on the same
repository. The measured cost is in the Proof section. Three replicas took 2.0x the wall time and wrote 2.4x the WAL of one, once. A single-owner claim is a follow-up if that cost matters on a large install; it adds a lease or pinned-connection failure mode and is not needed for correctness.

## Retry, restart and re-trigger

- A failed attempt leaves no marker; `RunBackground` retries after 30s,
  doubling to 10m. Migration 109 not yet applied is the common first failure.
- A process that starts after the marker exists does one primary-key lookup and
  returns (`already_complete`).
- A marker bump ships with a new binary, so a restart re-runs it. The runner
  does not poll for a marker removed under a running process; an operator who
  drops the marker by hand restarts the reducer.
- The reconcile runner returns `Ready=false` until the marker exists and starts
  checking on the cycle after. It needs no signal from the backfill.

## Telemetry

No new instrument. `eshu_dp_infra_inventory_backfill_runs_total{outcome}` and
the `infra_inventory.backfill`, `infra_inventory.backfill.failed` logs are
emitted by `RunBackground` and `Backfiller.Run`; the reducer passes its meter
and logger. The runner logs `infra_inventory.backfill.runner` at start and finish so
the owner is visible in a reducer log.

## Proof

Both live proofs live in
`go/internal/storage/postgres/infra/inventory/backfill_replicas_live_test.go`
and ran on a private PostgreSQL 18 (host load1 about 29, iowait 34 to 44
percent, so only ratios are comparable).

- `TestBackfillLiveReplicasRaceOnTheLockAndTheMarker`: four replicas on four
  pools start together while a live writer deletes content entities and derives
  their paths. Six repositories of 2,000 rows. Result: no error, one marker row,
  table equal to `content_entities` in both directions, 24 derive transactions
  (each replica once per repository).
- Seeded violation: with the advisory lock removed from `deriveLockSQL`, the
  same test failed in 2 of 3 runs (`table differs from content: 1 table rows
  without content`, and `deadlock detected (SQLSTATE 40P01)`), and passed 3 of
  3 with the lock. The race is probabilistic; a pass without the lock is
  possible, so the lock proof is the pair, not one run.
- `TestBackfillLiveTimedCorpus` at 200 repositories of 1,000 rows (150,000
  table rows, the gate corpus shape of `infraSeedRepoCount`): one replica 16.3
  s and 123 MB of WAL; three replicas 32.8 s (2.0x), 600 derive transactions
  (3.0x) and 292 MB of WAL (2.4x). Replicas walk repositories in the same
  order, so they convoy on each repository's lock. That is the duplicate cost of
  the chosen design: once per install or marker bump, bounded by the replica
  count.

## P3 note: skipping a repository another replica already derived

A replica cannot tell, after it gets the lock, that another replica's backfill
pass already derived the repository. No per-repository completion state exists:
`infra_resource_entities` rows carry no backfill-pass marker, and a live derive
also rewrites them, so row presence or `updated_at` does not prove a whole-repo
pass ran. A skip needs new state (a per-repository pass marker written in the
`MirrorRepo` transaction), which is a schema change and not a few lines. Not
done here. If the convoy cost (2.0x wall, 2.4x WAL at three replicas) matters on
a large install, the marker or a single-owner claim is the follow-up.

## Owner ledger backfill (#5563): disposition

`BackfillCloudResourceOwnerLedger` seeds `graph_node_owner` from CloudResource
nodes that were written before the ledger existed. It has no caller since the
API startup path was removed. This change does not wire it.

- Readers: `PostgresCloudResourceListStore` (`cloud_resource_list_store.go`),
  the cloud inventory read model, and the Kubernetes runtime workload store
  read `graph_node_owner`. None reads `graph_node_owner_backfill_state`; only
  the backfiller reads its own marker, so the missing marker blocks no reader.
- Writers: the reducer's graph owner gate (`internal/graphowner`) writes the
  ledger on every CloudResource graph write, so a fresh install or a rebuilt
  graph fills it through the live path. The seed covers only a graph that
  predates the ledger.
- Without the seed, a graph that predates the ledger and has no later reducer
  write lists fewer cloud resources until the next cloud collector generation
  rewrites them. No wrong resource is listed. Whether QA holds such a graph is
  NOT_CHECKED.
- Risk of wiring it: its first page (`MATCH (n:CloudResource) ... ORDER BY
  n.uid LIMIT 500`) exceeded the 10 s graph read deadline on 150,000 nodes
  before the uid fence (#6797), and a failure there was fatal at startup.
  Running it in the reducer means a graph scan on every boot of an install
  without the marker.

The disposition is "no caller needed on the supported install path". The
orphan code stays, with a doc comment that says so; retiring it is a separate
change that the owner decides. No issue is filed here.
