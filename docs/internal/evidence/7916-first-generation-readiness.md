# First-generation reducer activation readiness (#7916)

## Root-Cause Evidence

The `generationFreshnessSQL` statement reads the scope's active pointer and the
same-scope intent generation status in one PostgreSQL snapshot. Before this
change, `NewGenerationFreshnessCheck` returned `(true, nil)` whenever the active
pointer was SQL `NULL`. A first generation can already have an enqueued reducer
intent while its projector work is still running. The container-image identity
handler then read the activation epoch before `ProjectorQueue.Ack` had created
it, and the durable reducer queue dead-lettered that intent.

`TestContainerImageIdentityFirstGenerationEpochBarrierLive` held the actual
projector Ack behind a controlled barrier on disposable PostgreSQL 18.6. Before
the classifier change, the test failed (exit 1): with active pointer `NULL`,
intent generation `pending`, and no activation epoch, the handler returned
`container image identity generation is not active`; `ReducerQueue.Fail` set
`dead_letter/projection_bug`, attempt 1, dead-letter count 1. The later actual
`ProjectorQueue.Ack` activated the generation and created epoch 2, but the
reducer intent stayed dead-lettered. The coordinator-held local `red.log` receipt has SHA-256
`dd04c8f9e3c621f6a1cd1527b4f4f56ea1842c86d294fe0a3a5d2200b0adf3a3`.

## Change and contract boundary

For the exact tuple `scope exists` + `active_generation_id IS NULL` +
`same-scope intent generation is pending`, the check now returns
`(false, GenerationNotYetActiveError)` with an empty `ActiveGenerationID`.
That error already carries the `generation_activation_not_ready` failure class.
The queue already treats the class as a non-counting retry. The classifier keeps
the existing one-statement SQL, joins, and active-generation ordering. There
is no added database query, lock, transaction, queue policy, timeout, or worker
limit.

| Snapshot state | Freshness result | Safety status |
| --- | --- | --- |
| Known scope, NULL active, same-scope pending intent | typed deferral | Proved in live queue/handler/Ack path |
| Known scope, NULL active, missing/failed/superseded intent | legacy `(true, nil)` | NOT_CHECKED as safe; see failed replay below |
| Unknown scope | legacy `(true, nil)` | Compatibility preserved; handler-specific safety NOT_CHECKED |
| Exact active generation | `(true, nil)` | Existing contract |
| Non-NULL active, newer pending intent | typed deferral | Existing #6686 regression |
| Non-NULL active, older/failed/superseded/missing intent | `(false, nil)` | Existing terminal contract |
| Active generation without identity epoch | handler hard error | Existing corruption control |

## No-Regression Evidence

The post-change live first-generation proof uses real
`ReducerQueue.Enqueue/Claim/Fail`, the identity handler, and
`ProjectorQueue.Ack`. Three pre-Ack handler attempts left the row `retrying`
with class `generation_activation_not_ready`, attempt count 1, zero dead
letters, and zero writer calls. Ack then established a positive epoch. The
same intent was reclaimed, handled once, and acknowledged `succeeded`, still
at attempt count 1. The gate uses a test-only empty fact loader and writer
stub for decision writes; it does not prove a full built reducer binary or
graph projection. A real Go JSON event recorded the test's PASS, elapsed
2.31 seconds, at `firstgen-real-go-final.jsonl` in the coordinator-held local proof bundle.

The `go test` unit matrix pins the legacy and new freshness outcomes and the
typed error's empty active ID. The existing active-A/pending-B live barrier,
missing activation epoch sentinel, and reducer pre-activation controls remain
separate controls. The full built binary determinism, corpus truth, and matched
performance evidence are NOT_CHECKED here.

An independently seeded verifier check used the actual first-generation Go JSON
events. With the real PASS event intact, `verify_results` returned 0 and
reported `1/1 PASS`. Removing only that PASS event returned 1 with
`missing or duplicate event (run=1, terminal=0)`. The new file has an exact
`postgres_ci` ledger row and runner tuple. `verify-ledger` selects 80 files and
182 tests. This focused check is not a full readiness job result.

## Known failed-first-generation exit

A separate controlled probe deferred the same first-generation intent, then
used actual `ProjectorQueue.Fail` to mark that generation `failed` while the
active pointer remained `NULL`. The next `Claim/Handle/Fail` read the absent
epoch and dead-lettered the reducer intent as `projection_bug`, attempt 1.
This is a demonstrated limitation of the legacy NULL-active/non-pending result,
not a claimed safe terminal exit. The temporary probe source and RED log are
preserved in the coordinator-held local proof bundle as
`failed-firstgen-probe-source.txt`
(SHA-256 `ac7ea9e9c46ac0d06155b493e2c14bf9a504095b6ab6962bd60adf30cab5dc26`)
and `failed-firstgen-probe-red.log`
(SHA-256 `705e8d22070e19f1bab4e5aaa25b45bc7db656f4aaee938c91ae42583006d328`).
Changing that outcome requires a separate semantic ruling.

## Concurrency and operator signals

The classifier reads one statement snapshot and takes no explicit locks. Its
new branch only maps one result tuple to the existing typed deferral. Projector
Ack retains ownership of activation and epoch creation; a reducer retry cannot
force activation. The durable row records `retrying`,
`failure_class=generation_activation_not_ready`, and a stable attempt count.
The existing retry surge counter carries that failure class, so this change
adds no metric, span, log field, or cardinality. Same-scope contention and
unrelated ready-work progress under concurrent workers are NOT_CHECKED here.
