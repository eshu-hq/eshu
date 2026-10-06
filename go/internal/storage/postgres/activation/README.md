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
                                 port ErrActivationInapplicable -> RetireInapplicable
                                 port ErrActivationCatalogChanged -> hold lease, retry at lease cadence
                            -> wake <=32 rows -> work_pending | completed
                       CatchUp (bounded scope pages) and Prune (bounded, completed/obsolete rows)
```

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

`RunnerStore` adapts `Store` to `reducer/maintenance.ActivationObligationStore`,
the storage port of `maintenance.ActivationObligationRunner`, and maps
`ErrLeaseLost` to `maintenance.ErrActivationLeaseLost`. The production
maintenance port is `postgres.ActivationMaintainer` in the parent package (it
needs `IngestionStore`, which this package cannot import): it runs the
partition-scoped pass for the obligation's own partition and maps
`not_active` to nil (Finalize retires obsolete), `inapplicable` to
`maintenance.ErrActivationInapplicable`, the typed `catalog_changed`,
`no_memo_baseline` and `closure_too_deep` refusals to holds, `published` to nil
(Finalize wakes and completes), and `retry` or any other error to a
maintenance failure.

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

## Rollout order

Migration 160 must exist before any binary that Acks runs
(`eshu-projector`, `eshu-ingester`, `eshu-bootstrap-index`): every accepted Ack
writes `activation_obligations`. This follows the existing convention:
`eshu-bootstrap-data-plane` applies Postgres migrations and exits before the
services start (`docs/public/deployment/service-runtimes-bootstrap.md`), and the
Helm chart runs it as a `pre-install,pre-upgrade` hook
(`deploy/helm/eshu/templates/job-schema-bootstrap.yaml`). If a new binary Acks
before the migration, the Ack fails with SQLSTATE 42P01 (`relation
"activation_obligations" does not exist`) and the transaction rolls back, so no
partial activation is written. That error is not a lock wait, so it is not
`ErrWorkAckDeferred`: the projector service returns it from
`processWork` (`ack projector work: ...`), `Service.Run` ends, the process
restarts, and the claim is reclaimed after its lease. Rolling back to an older
binary is safe, because older binaries ignore the table. The idle cost of the
consumer's catch-up page, prune and census per poll is a D3 step-3 measurement
line and is NOT_CHECKED here.

## Foreign key policy

One foreign key, `generation_id -> scope_generations ON DELETE CASCADE`, like
every other generation-owned table. The generation-retention prune deletes
superseded generations and their obligations go with them
(`TestActivationObligationRetentionCascadeLive`). The primary key leads with
`generation_id` so that cascade seeks (#7419). There is no foreign key to
`fact_work_items`.

## Proof

Live proofs run on the full bootstrap schema in an isolated schema of a
disposable PostgreSQL (`ESHU_DEFERRED_PARTITION_PROOF_DSN`,
`ESHU_DEFERRED_PARTITION_PROOF_DISPOSABLE=1`), enrolled in the
`live-postgres-readiness` runner:

- `activation_obligation_ack_live_test.go` — atomic Ack insert.
- `activation_obligation_claim_live_test.go` — claim never blocks on a held
  row or starves on finished rows; a lease lost inside Finalize rolls back.
- `activation_obligation_consumer_live_test.go`,
  `activation_obligation_matrix_live_test.go`,
  `activation_obligation_recovery_live_test.go` — the consumer protocol.
- `activation_obligation_retention_live_test.go` — FK cascade and prune.
- `activation_obligation_terminal_live_test.go` — `inapplicable` (cloud scope,
  collision loser, fenced retire), catalog-changed hold, NULL pointer.
- `activation_obligation_targeted_live_test.go` — the production maintainer:
  real `catalog_changed` and `no_memo_baseline` holds that complete after the
  epoch pass, and a real collision loser retired `inapplicable`.
- `quiet_generation_maintenance_live_test.go` (in the parent package) — the
  quiet-generation proof: a source generation committed, maintained and Acked
  through the production commit, Claim and Ack, then quiet polls; the consumer
  publishes the exact phase with the production maintainer, and a labelled
  control arm uses the whole pass.
- `activation_obligation_composed_live_test.go`,
  `activation_obligation_scope_lock_live_test.go`,
  `activation_obligation_lease_restart_live_test.go`,
  `activation_obligation_redelivery_live_test.go` — composed concurrency
  (D3 step 4) with the production maintainer: a consumer cycle racing the
  epoch pass on overlapping repositories (forced lock-holder interleavings
  and a 20-iteration barrier race), two replicas, an ingestion commit and a
  projector Ack racing Finalize's scope lock (including its lock timeout),
  lease expiry mid-maintenance, a crash between the evidence commit and the
  phase, Ack redelivery and catch-up re-owe races, and supersession between
  claim and finalize. Each asserts the final obligation, phase, evidence and
  queue rows.

## Performance and observability evidence

No-Regression Evidence: the Ack transaction runs one more statement,
`insertObligationQuery`, with the consumer flag off or on
(`projector_queue.go`, the `activation.Insert` call before the commit): 8
statements between BEGIN and COMMIT instead of 7. Its deterministic cost, from
`pg_stat_statements` reset per sample, is 12.8 shared blocks and 0.038 to
0.044 ms of server time per Ack for the insert, plus about 4 blocks per Ack
that no named line carries and that by shape are its foreign-key probe on
`scope_generations` (identity NOT_CHECKED); the re-owe path (an `obsolete`
row, `ON CONFLICT DO UPDATE`) costs 10.8 blocks and 0.031 to 0.044 ms with no
probe. The counts are identical in every sample, and 0.038 to 0.044 ms is the
insert line only, not the whole server cost. Wall time, which is reported and
not gated: the median Ack went from 1.218 ms to 1.329 ms, +0.111 ms (+9.1% as
the median of the per-sample medians; the paired deltas ranged from -19% to
+22%, mean +6.2%, standard deviation 14.6 points; the pooled 3,600-Ack median
moved +0.100 ms, +8.0%), and on the re-owe path from 1.289 ms to 1.395 ms,
+0.106 ms (+8.2%) over 4 pairs. Per-sample p99 without a prior row was
1.917 to 3.080 ms for the base and 2.087 to 2.642 ms for this branch, inside
the base range. On the re-owe path it was 2.974 to 3.780 ms for the base (4
samples) and 2.541 to 4.415 ms for this branch (5 samples, one of them
unpaired), and two of those five exceed the base maximum. The pooled re-owe
p99, 3.620 ms against 3.984 ms (linear interpolation over every Ack of the 4
complete pairs; the ruling's 3.674 and 3.955 ms were not reproduced), is one
pooled figure; the per-sample spread is wider. The base arm's spread across
its samples was 51%, so the server cost is the gate and the wall delta is
not. The scope-row hold, measured on
the client from the scope update to the commit, grows by the same amount
(+0.119 ms median); Finalize bounds its wait for that row at 1 s
(`activation/sql.go`, `finalizeLockTimeoutQuery`). The residual above the
server cost is, as a hypothesis, the size of one loopback round trip; the
round trip was not measured. In a cluster the delta would be one network
round trip plus about 0.05 ms per activated generation, so about 0.1 s per
1,000 activated generations at the measured delta. Environment: PostgreSQL 18
`postgres@sha256:54451ecb…`, 4 CPU / 4 GiB Docker Desktop on macOS over
loopback; base `5c4e03613` against `19a36bda9` (test binaries `d4863fb1…` and
`083a4888…`; the Ack path is unchanged after `19a36bda9`); 900 restored scopes
with a completed obligation per prior generation; 600 Acks per arm; first
mover alternating; load1 6.7 to 8.4 at each arm, load5 and load15 10 to 14,
and no 1-second in-run load sampling. The host was not proven quiet: another
executor in the same session ran a mutation sweep, static gates, race tests
and a PostgreSQL container between 03:30 and 04:05 EDT, overlapping this run
(03:41 to 03:44), so the wall delta is a noisy figure whose true value may be
higher or lower; the deterministic server cost above is unaffected and is
the gate. An earlier run was discarded (its first
mover did not alternate), and the load guard stopped 3 arms of the counted
run. The gate and this wording come from the #7584 Ack bound ruling
(2026-10-06), which accepts this run as the pre-PR Ack line. The scope-update
statement's own `pg_stat_statements` line is empty in every receipt (the
harness compared the raw constant to the normalized text), so the hold figure
is the client-side measurement only. The partition-scoped callback's cost is in
`docs/internal/evidence/7584-partition-scoped-maintenance.md` (D3 step 3, a
tiny-facts fixture). What is proven here is correctness on PostgreSQL 18 (disposable
`postgres@sha256:54451ecb…`, isolated schema, full bootstrap): the live
test functions listed above plus the two quiet-generation proofs (38 in all),
and 74 of 76 distinct semantic mutations killed, including every branch of
`postgres.ActivationMaintainer`'s mapping, the wiring flag, the re-owe of an
obsolete row (Ack and catch-up, including a catch-up that waited on another
catch-up's re-owe), the maintenance deadline, the settle span, Finalize's
scope lock (dropped, `NOWAIT`) and lock timeout, the `finalize_lock_timeout`
mapping (55P03 not mapped, cause dropped, reason ignored, logged at Error),
an Ack insert without `ON
CONFLICT`, and the maintenance passes' exclusive repository locks. Flipping
only the CTE copy of the wake failure-class or prune state predicate is
killed by the starvation tests in the matrix and retention files. Two
survivors are equivalent. Removing `!active.Valid` from Finalize's pointer
check changes nothing because a NULL pointer scans as an empty string that
never equals a generation id; the mutant that treats NULL as current is
killed. Taking the exclusive repository locks unsorted changes nothing for
today's callers, which both sort the repository ids before batching
(`ingestion_backfill.go` and `ingestion_targeted_maintenance_write.go`). The structural bounds are as follows. `Ack` gains one insert (`ON CONFLICT ... DO UPDATE ... WHERE state =
'obsolete'`) inside its existing transaction, after the scope lock it already
holds. It costs a primary-key probe, the foreign-key check's KEY SHARE probe on
`scope_generations`, and maintenance of the primary key and the open partial
index; on a conflict it locks the existing obligation row.
`Claim` locks one row through the open partial index. `Finalize` locks one
scope row and one obligation row, seeks one repository fact on the
`(scope_id, generation_id, fact_kind, ...)` index when the phase is absent,
and wakes at most 32 rows through the
existing `fact_work_items` scope/generation indexes. `CatchUp` reads at most
`pageSize` scope rows per call. `Prune` deletes at most `limit` rows through
the finished partial index. The retention cascade seeks the
generation-leading primary key. Consumer throughput at fleet scale and the
lease against a representative maintenance p99 are NOT_CHECKED; they gate the
consumer's default-on change, not this slice.

Observability Evidence: `eshu_dp_activation_obligations{status}`,
`eshu_dp_activation_obligation_oldest_open_age_seconds`,
`eshu_dp_activation_obligation_claim_age_seconds`,
`eshu_dp_activation_obligation_finalize_total{outcome}`,
`eshu_dp_activation_obligation_woken_total`,
`eshu_dp_activation_obligation_maintenance_duration_seconds{outcome}`,
`eshu_dp_activation_obligation_catch_up_inserted_total`,
`eshu_dp_activation_obligation_pruned_total` and
`eshu_dp_activation_obligation_failures_total{reason}`, asserted in
`reducer/maintenance/activation_obligation_runner_test.go`; per-obligation
logs `activation obligation finalized` and `activation obligation step failed`
carry `scope_id` and `generation_id`. The Ack insert adds no metric: it is one
statement of the existing Ack transaction, which the existing Ack instruments
already cover.
