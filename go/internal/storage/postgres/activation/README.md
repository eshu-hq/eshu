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
                       -> maintenance port (publishes the phase)
                       -> Finalize tx: scope lock -> obligation lock
                            -> obsolete | phase_not_ready | wake <=32 rows -> work_pending | completed
                       CatchUp (bounded scope pages) and Prune (bounded, finished rows)
```

## States and outcomes

| State | Meaning |
| --- | --- |
| `pending` | Owed, not leased. |
| `leased` | A consumer holds the lease (`lease_owner`, `lease_until`, `claim_token`). An expired lease is claimable again. |
| `completed` | Phase existed, every waiting row was woken, no handler in flight. |
| `obsolete` | The scope moved to another generation before completion. |

`Finalize` outcomes: `completed`, `phase_not_ready`, `work_pending`,
`obsolete`, `not_owner`, `missing`. They are a closed set and safe as metric
labels. `ErrLeaseLost` means the lease expired inside the Finalize transaction
after the wake ran; the transaction rolled back.

## Consumer port

`RunnerStore` adapts `Store` to `reducer/maintenance.ActivationObligationStore`,
the storage port of `maintenance.ActivationObligationRunner`, and maps
`ErrLeaseLost` to `maintenance.ErrActivationLeaseLost`.

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
