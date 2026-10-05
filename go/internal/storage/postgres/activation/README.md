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

## Performance and observability evidence

No-Regression Evidence: NOT MEASURED. This slice ran on a shared host where
timing runs were not allowed, so it carries no before/after numbers and makes
no speed claim. What is proven is correctness on PostgreSQL 18 (disposable
`postgres@sha256:54451ecb…`, isolated schema, full bootstrap): the live
test functions listed above plus the two quiet-generation proofs (27 in all),
and 52 of 55 distinct semantic mutations killed, including every branch of
`postgres.ActivationMaintainer`'s mapping and the wiring flag. Two survivors
flip only the CTE copy of a predicate that the statement repeats on the
locked row (wake failure class, prune state); flipping both copies is
killed. The third removes `!active.Valid` from Finalize's pointer check,
which is equivalent because a NULL pointer scans as an empty string that
never equals a generation id; the mutant that treats NULL as current is
killed. The structural
bounds are as
follows. `Ack` gains one primary-key insert (`ON CONFLICT DO NOTHING`, no read)
inside its existing transaction, after the scope lock it already holds.
`Claim` locks one row through the open partial index. `Finalize` locks one
scope row and one obligation row, seeks one repository fact on the
`(scope_id, generation_id, fact_kind, ...)` index when the phase is absent,
and wakes at most 32 rows through the
existing `fact_work_items` scope/generation indexes. `CatchUp` reads at most
`pageSize` scope rows per call. `Prune` deletes at most `limit` rows through
the finished partial index. The retention cascade seeks the
generation-leading primary key. Ack latency at fleet scale, consumer
throughput, and the cost of the real (targeted) maintenance callback are
NOT_CHECKED; the #7584 D3 slice owns them, and they gate the PR.

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
