# #6502 container image identity epoch gate

## Root-Cause Evidence

The dead-lettered `container_image_identity` item is a misclassified epoch
miss, not a projection bug. The chain, each link observed:

1. The projection reads the activation epoch through a three-way join over
   `container_image_identity_scope_state`, `ingestion_scopes`, and
   `scope_generations` (`containerImageIdentityActivationEpochQuery`). The
   join returns no row whenever the intent's generation is not the scope's
   active one: pending, superseded, or missing are indistinguishable.
2. The store returned that miss as a plain `fmt.Errorf` value. Handle wrapped
   it and returned it. `reducercontract.IsRetryable` returns false for any
   error that does not implement `Retryable() bool`, so `failIntent` took the
   dead-letter path on the FIRST attempt with triage class `projection_bug`.
   No retry budget is involved: the item never retries.
3. The race window is real under load. The runtime freshness check (#7157)
   runs before Handle, but the activation can flip between that check and the
   epoch read (a retry for generation N executes while N+1 activates), or a
   path without the runtime check can run a pending generation directly. In
   both cases the epoch read misses and the item dead-letters immediately.
4. The scope-state row cannot lag the activation: migration 092 writes it
   from an AFTER trigger on `ingestion_scopes.active_generation_id`, so the
   row and the flip commit in the same transaction. An epoch miss therefore
   always means "not the active generation", never "activation not yet
   visible". Rejected hypothesis: a separate scope-state writer racing the
   activation flip (no such writer exists; the only writer is the trigger).

The barrier test reproduced the exact production symptom on clean main before
the fix: `pending outcome = (dead_letter, projection_bug, 100)`.

## Fix

The miss is now a matchable sentinel
(`contract.ErrContainerImageIdentityGenerationNotActive`, same message), and
Handle routes it through a generation-freshness gate
(`containerimage.gatedActivationEpoch`, wired to the shared
`postgres.NewGenerationFreshnessCheck` at the production assembly site
(`buildReducerService`, consumed through the handler registry)):

| Freshness verdict | Outcome |
|---|---|
| Pending and newer than active | `GenerationNotYetActiveError`: the queue retries without counting against the attempt budget until the generation activates, fails, or is superseded |
| Superseded, failed, or missing | Early `Result` with status `superseded`, acked succeeded |
| Still current | One epoch re-read (the activation may have landed between the reads); a still-missing row re-consults the check once (a flip in that window supersedes) and surfaces loudly only when still current — the genuinely missing epoch |
| Check lookup failure | Loud wrapped error, fails closed |

A nil check preserves the legacy loud error, so unwired callers and old tests
keep their behavior. Verdicts are stable under concurrency: activation only
moves forward, so a generation once superseded never becomes active again,
and a deferral re-evaluates on every retry. Nothing is serialized and no
worker count, batch size, or lease changes.

Projected graph truth is unchanged: defer and supersede only change the
intent's disposition, never a decision or edge. The B-7 golden-corpus gate in
CI is the end-to-end proof that collector, graph, and query truth still agree.

## No-Regression Evidence (#6502):

The happy path is byte-identical: one indexed epoch join per intent, no added
query. The freshness check and the re-read run only on an epoch miss, which
was already a terminal failure before this change. Local proof timings on
disposable PostgreSQL 18 (single shared host, warm cache):

- `TestContainerImageIdentityEpochBarrierDefersPendingLive`: 1.36s GREEN
  (three legs: defer plus Fail to `retrying`, release plus Handle plus Ack,
  superseded Result; dead-letter count 0).
- `TestContainerImageIdentityActivationEpochMissIsSentinelLive`: GREEN
  (active hit plus pending, superseded, missing, and unknown-scope misses).
- `TestGatedActivationEpochClassifiesMiss`: 11/11 subtests GREEN in 0.00s
  (includes the second-miss re-consult, supersede-between-check-and-reread,
  and second-consult lookup-failure cases).
- Full `containerimage`, `reducer`, `contract`, `factload`, `cmd/reducer`,
  and `recordpseudo` suites GREEN; store-touching Postgres live tests GREEN.

The full `storage/postgres` package run with a live DSN shows failures that
are identical on clean main and unrelated to this change: 13 fence/cutover
proofs assert the pre-096 loud-rejection contract while migration 096's
enforce trigger now fences those transitions back to pending (filed as
#7790), and a `pg_trgm` search_path/pin ordering hazard needs a fresh
database. This PR's own live proofs are green; `make pre-push` is the
credential-free floor (live proofs skip without a DSN) and CI runs the
ledger-enrolled proofs.

## No-Observability-Change: eshu_dp_reducer_retry_surge_total, eshu_dp_reducer_executions_total

No new instrument. Each gate outcome reuses an existing operator signal:

- Defer: `failIntent` records `eshu_dp_reducer_retry_surge_total` with
  `failure_class="generation_activation_not_ready"` and leaves a `retrying`
  row carrying that class.
- Supersede: the service records `eshu_dp_reducer_executions_total` with
  status `superseded` and its `reducer intent superseded` log line; the queue
  acks the item succeeded.
- Loud: the existing failure counters and dead-letter triage apply unchanged.

## Proofs enrolled

Both live tests run in the `live-postgres-readiness` CI runner
(`class: postgres_ci`), with the family DSN
`ESHU_CONTAINER_IMAGE_IDENTITY_EPOCH_PROOF_DSN` bridged onto the shared
helper. Ledger, selection, and runner self-tests pass.
