# #6307 — the batch reducer acked work it had already failed

The batch path forwarded every intent to the acker, including ones
`executeAndReport` had just terminalized through `WorkSink.Fail`. On two domains
that turns a routine retryable handler error into a fatal process exit.

## The chain

1. A cross-scope consumer whose declared producer has not activated returns an
   error. Routine, expected, retryable —
   `go/internal/reducer/crossscope/readiness_floor.go:372` (moved from
   `cross_scope_readiness_floor.go:365` by #6061) logs it at INFO and the
   deferral has a 30-minute `max_wait`.
2. `executeAndReport` calls `WorkSink.Fail(...)` and returns
   `Result{Status: ResultStatusFailed}, nil`.
3. `Fail` → `failIntent` → `retryReducerWorkQuery`
   (`storage/postgres/reducer_queue.go:90`) sets `status='retrying'` and
   `lease_owner=NULL`.
4. The worker forwarded the result to the acker anyway — no status filter.
5. `ackCICDRunCorrelationReducerWorkBatchQuery` updates
   `WHERE … lease_owner=$2 AND status IN ('claimed','running')` → **0 rows**.
6. `AckBatch` returns `ErrReducerClaimRejected` on `rowsAffected == 0`.
7. The acker's `appendErr` cancels the run context. Every in-flight worker
   aborts, and `cmd/reducer` exits.

The per-item path never had this: `service.go` `reduceOnce` returns immediately
after its own `Fail` and never reaches `Ack`. The batch path diverged from that
contract silently.

## Why it reads as flakiness

Only `container_image_identity` and `ci_cd_run_correlation` check `rowsAffected`.
Every other domain's stale ack updates 0 rows silently, so the item is acked into
the void harmlessly and the reducer lives. The process dies only when a flush
batch's subset for those two domains is entirely failed items. With a 100 ms
flush timer and four workers, one succeeded sibling in the same flush hides it.

Observed on `main` three times, each verified individually rather than inferred:
08-12 `b3e7023599`, 08-14 `17a749ff05`, 08-25 `ed0b103bad`. The 08-25 run carries
the byte-identical drain signature (`residual=624, required intents=1, populated
domains=0/1`) that had been read as PR-specific — so that signature is a
fingerprint of this defect, not of a diff.

## The fix

`executeAndReport` returns an explicit "does the caller still owe this intent an
ack" boolean, false exactly when `Fail` already terminalized the row. The worker
skips those.

A boolean rather than testing `result.Status == ResultStatusFailed`, because the
per-item path *does* ack a handler-returned `ResultStatusFailed`. No production
handler emits one today, but keying on the value would couple the fix to that
accident.

No-Regression Evidence: `Correctness win`, no wall-clock claim. The change is one
boolean returned through an existing call and one `continue` in the worker loop —
no new query, no new round trip, no allocation in the hot path, and strictly
fewer `AckBatch` rows than before, because the intents it now skips were the ones
matching zero rows. Terminal queue state is unchanged for every domain that was
already working: a failed intent still sits at `status='retrying'` with
`lease_owner=NULL`, exactly as `Fail` left it, and is re-claimed on the next
sweep. What changes is that the reducer survives to do that sweep.

Verified with `go test ./internal/reducer ./cmd/reducer ./internal/storage/postgres`
exit 0, the same set under `-race` exit 0, and `go vet` exit 0.

Mutation proof, `go vet` exit 0 on the mutant first so the red is behavioural:
`if !needsAck` → `if !needsAck && false` (subs=1) reds
`TestServiceRunBatchDoesNotAckIntentsItAlreadyFailed` on both its
`ci_cd_run_correlation` and `code_call_materialization` subtests, and
`TestServiceRunBatchStillAcksSucceededSiblingsOfAFailedIntent`. Restored, exit 0.
That second test is the one that pins the fix does not over-correct: succeeded
siblings in the same batch must still be acked.

Observability Evidence: no metric, span or log line is added or removed. The
operator-visible change is the absence of a fatal
`ERROR reducer failed error="batch ack reducer work: reducer work claim rejected"`
and the process staying alive; the routine deferral it was triggered by continues
to log at INFO through `cross-scope consumer deferred`, unchanged.

## Not established

- The Postgres half is read from committed SQL, not from a live `-tags live`
  AckBatch-after-Fail test.
- `failIntent` has a second instance of the same `0-rows → ErrReducerClaimRejected`
  shape for `container_image_identity` that has not been investigated.
- That this makes the golden-corpus gate green is an inference from the proven
  chain, not an observation — the live gate was not run.
