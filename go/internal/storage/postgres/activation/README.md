# activation

Durable exact-generation activation obligations for issue #7584
(`activation_obligations`, migration 160).

## Why it exists

The ingester runs deferred relationship maintenance after a shard drains. A
quiet repository can have its new generation activated by the projector's
`ProjectorQueue.Ack` after that pass already ran. Nothing then publishes the
new generation's own `backward_evidence_committed` phase, so every
`deployment_mapping` row for it retries on the not-ready schedule until some
unrelated commit triggers another pass. The obligation records the gap in the
Ack transaction itself, so it cannot be lost.

## Flow

```text
ProjectorQueue.Ack tx: scope lock -> work -> generations -> activate -> Insert(obligation) -> commit
resolution engine:     Claim (SKIP LOCKED, lease + token)
                       -> Finalize tx: scope lock -> obligation lock
                            -> obsolete (pointer moved or NULL)
                            -> inapplicable (no phase and no repository fact)
                            -> phase_not_ready -> maintenance port -> Finalize again
                                 port obligation.ErrInapplicable -> RetireInapplicable
                                 port obligation.ErrCatalogChanged -> hold lease, retry at lease cadence
                            -> wake <=32 rows -> work_pending | completed
                       CatchUp (bounded scope pages) and Prune (bounded, completed/obsolete rows)
```

Since #11104 the deferred-backfill phase publish also wakes these rows. A
row parked through the reducer waiting set-aside carries an exact-phase
wait, and the publish reopens it by that key: inside the publishing
transaction, and again after the commit. Finalize's wake still matches only
rows deferred into the future, so after a publish it usually finds nothing
and completes with `Woken: 0`. A low
`eshu_dp_activation_obligation_woken_total` is therefore expected, not a
stall. The publish's wakes are counted on
`eshu_dp_reducer_waiting_wake_total` at the `phase_exact_in_tx` and
`phase_exact_post_commit` wake sites.

Finalize's wake remains the path for a parked row with no exact-phase wait
row. That covers two cases:

- A row parked by an older binary during a rolling deploy. It carries the
  coarse `target_committed` wait, which the exact publish wake cannot match.
- A row whose wait insert failed after its retry update committed.

The live proofs here detach the wait row to exercise that path. `publish_wake_overlap_live_test.go` pins the
overlap itself: the publish wakes the row, Finalize completes with nothing
left to wake, and the row is claimable once.

## States and outcomes

| State | Meaning |
| --- | --- |
| `pending` | Owed, not leased. |
| `leased` | A consumer holds the lease (`lease_owner`, `lease_until`, `claim_token`). An expired lease is claimable again. |
| `completed` | Phase existed, every waiting row was woken, no handler in flight. |
| `obsolete` | The scope's active pointer moved to another generation, or is NULL because the generation failed, before completion. |
| `inapplicable` | The generation can never carry a backward-evidence phase: it has no repository fact (Finalize decides with one index seek), or the maintainer found no repository maps to it in the shipped active-repository read (`RetireInapplicable`, repo_id collision loser). Never pruned; only the generation cascade removes it, so CatchUp cannot owe it again. |

`Finalize` outcomes: `completed`, `phase_not_ready`, `work_pending`,
`obsolete`, `inapplicable`, `not_owner`, `missing`. They are a closed set and safe as metric
labels. `ErrLeaseLost` means the lease expired inside the Finalize transaction
after the wake ran; the transaction rolled back.

## Consumer port

`RunnerStore` adapts `Store` to `reducer/maintenance/obligation.Store`,
the storage port of `obligation.Runner`, and maps
`ErrLeaseLost` to `obligation.ErrLeaseLost`. The production
maintenance port is `postgres.ActivationMaintainer` in the parent package (it
needs `IngestionStore`, which this package cannot import): it runs the
partition-scoped pass for the obligation's own partition and maps
`not_active` to nil (Finalize retires obsolete), `inapplicable` to
`obligation.ErrInapplicable`, the typed `catalog_changed`,
`no_memo_baseline` and `closure_too_deep` refusals to holds, `published` to nil
(Finalize wakes and completes), and `retry` or any other error to a
maintenance failure.

The producer family beside it (`InsertProducerActivation`,
`ClaimProducerActivation`, `BeginProducerSettle`, `PruneProducer`,
`StatsProducer`) owes, leases, settles, prunes and counts the
`producer_activation_obligations` rows (#7635): one obligation per activated
generation, settled by retiring non-producers as inapplicable, reopening
the dependent consumers of the rest, and completing under the same
token-fenced claim shape, with no catch-up (a catch-up would re-owe every
pruned generation; see `StatsProducer`). The production
consumer port is `postgres.ProducerActivationRunnerStore` in the parent
package (the settle needs `IngestionStore`, which this package cannot
import); the resolution engine's consumer is
`producer.Runner`, wired by `cmd/reducer`
(`producer_activation_wiring.go`) when
`ESHU_PRODUCER_ACTIVATION_CONSUMER_ENABLED=true`.

## Clock skew

Every obligation timestamp and lease comparison uses the database clock
(`clock_timestamp()`), and so does the wake (`visible_at = clock_timestamp()`).
`ReducerQueue.Claim` admits `visible_at <= $1`, where `$1` is the reducer
process clock. When the database clock runs ahead of the reducer host by δ, a
woken row becomes claimable δ later. The claim-age histogram
(`eshu_dp_activation_obligation_claim_age_seconds`) subtracts the database's
`created_at` from the process clock and is off by the same δ. Both are bounded
by NTP sync and are not correctness defects. The live fixtures read the database
clock (`activationDatabaseClock`) because a disposable container ran 32–79 ms
ahead of its host. The maintenance deadline's margin (a fifth of the lease) also
absorbs this skew.

## Off-state observability

The obligation gauges (`eshu_dp_activation_obligations{status}`,
`eshu_dp_activation_obligation_oldest_open_age_seconds`, and the rest of the
`eshu_dp_activation_obligation_*` family) are emitted by the consumer
(`Runner`; the census runs in `housekeep`), so while
`ESHU_ACTIVATION_OBLIGATION_CONSUMER_ENABLED=false` they are absent: the
wiring returns no runner and nothing publishes the census. The Ack path still
writes one `activation_obligations` row per activated generation, so the
pending pile is visible in SQL. Open states are `pending` and `leased`;
`completed`, `obsolete`, and `inapplicable` are terminal. This fallback mirrors
the census query (`statsQuery` in `backlog.go`, global derived by `max` in
`Stats`): per-state counts over all states plus a per-state oldest-open age.
The `FILTER` admits only pending/leased rows inside each group, so each row
carries its own state's oldest age — 0 on terminal-state rows — and the global
oldest-open age is the `MAX` over rows. States with no rows are absent, and an
empty table yields no rows at all; the gauges report explicit zeros instead.

```sql
SELECT state, count(*),
       COALESCE(EXTRACT(EPOCH FROM clock_timestamp() - min(created_at)
           FILTER (WHERE state IN ('pending', 'leased'))), 0)
           AS oldest_open_age_seconds
  FROM activation_obligations
 GROUP BY state;
```

## Rollout order

Migration 160 must exist before any binary that Acks runs
(`eshu-projector`, `eshu-ingester`, `eshu-bootstrap-index`): every accepted Ack
writes `activation_obligations`. This follows the existing convention:
`eshu-bootstrap-data-plane` applies Postgres migrations and exits before the
services start (`docs/public/deployment/service-runtimes-bootstrap.md`), and the
Helm chart (in the eshu-deploy checkout) runs it as a `pre-install,pre-upgrade` hook
(`helm/eshu/templates/job-schema-bootstrap.yaml`). If a new binary Acks
before the migration, the Ack fails with SQLSTATE 42P01 (`relation
"activation_obligations" does not exist`) and the transaction rolls back, so no
partial activation is written. That error is not a lock wait, so it is not
`ErrWorkAckDeferred`: the projector service returns it from
`processWork` (`ack projector work: ...`), `Service.Run` ends, the process
restarts, and the claim is reclaimed after its lease. Rolling back to an older
binary is safe, because older binaries ignore the table. The idle cost of the
consumer's catch-up page, prune and census per poll is a cost-harness measurement
line and is NOT_CHECKED here.

## Foreign key policy

One foreign key, `generation_id -> scope_generations ON DELETE CASCADE`, like
every other generation-owned table. The generation-retention prune deletes
superseded generations and their obligations go with them
(`TestActivationObligationRetentionCascadeLive`). The primary key leads with
`generation_id` so that cascade seeks (#7419). There is no foreign key to
`fact_work_items`.

## Proof

Live proofs run on the full bootstrap schema in a disposable PostgreSQL
database (`ESHU_DEFERRED_PARTITION_PROOF_DSN`,
`ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1`), enrolled in the
`live-postgres-readiness` runner. They are this package's external tests
(`package activation_test`):

- `ack_live_test.go` — atomic Ack insert.
- `claim_live_test.go` — claim never blocks on a held row or starves on
  finished rows; a lease lost inside Finalize rolls back.
- `consumer_live_test.go`, `matrix_live_test.go`, `recovery_live_test.go` —
  the consumer protocol.
- `retention_live_test.go` — FK cascade and prune.
- `terminal_live_test.go` — `inapplicable` (cloud scope, fenced retire),
  catalog-changed hold, NULL pointer. The repo_id collision-loser proof now
  lives in `backfill/targeted/terminal_live_test.go` with the
  partition-scoped engine proofs.
- `targeted_live_test.go` — the production maintainer: real `catalog_changed`
  and `no_memo_baseline` holds that complete after the epoch pass, and a real
  collision loser retired `inapplicable`.
- `quiet_generation_live_test.go` — the quiet-generation proof: a source
  generation committed, maintained and Acked through the production commit,
  Claim and Ack, then quiet polls; the consumer publishes the exact phase with
  the production maintainer, and a labelled control arm uses the whole pass.
- `composed_live_test.go`, `scope_lock_live_test.go`,
  `lease_restart_live_test.go`, `redelivery_live_test.go` — composed
  concurrency with the production maintainer: a consumer cycle
  racing the epoch pass on overlapping repositories (forced lock-holder
  interleavings and a 20-iteration barrier race), two replicas, an ingestion
  commit and a projector Ack racing Finalize's scope lock (including its lock
  timeout), lease expiry mid-maintenance, a crash between the evidence commit
  and the phase, Ack redelivery and catch-up re-owe races, and supersession
  between claim and finalize. Each asserts the final obligation, phase,
  evidence and queue rows.

They reach the parent package through its exported surface and
`testutil/postgresproof` (the isolated-schema opener and the proof DSN);
`fixtures_test.go` and `corpus_helpers_test.go` hold the small fixtures they
copy from the parent's test files. #7648 replaces the copies with one shared
live-fixture package.
