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
limit. A same-scope generation that becomes `failed` through projector Fail or
`superseded` through the projector Claim sweep while the active pointer remains
NULL now returns `(false, nil)`, so runtime dispatch can terminalize its
deferred reducer intent as superseded before handler entry. The direct identity
handler proof enters the handler and reads the epoch before its freshness check.

| Snapshot state | Freshness result | Safety status |
| --- | --- | --- |
| Known scope, NULL active, same-scope pending intent | typed deferral | Proved in live queue/handler/Ack path |
| Known scope, NULL active, failed/superseded intent | `(false, nil)` | Proved through actual projector failure/supersession and reducer replay |
| Known scope, NULL active, missing/other intent status | legacy `(true, nil)` | Compatibility preserved; safety NOT_CHECKED |
| Unknown scope | legacy `(true, nil)` | Compatibility preserved; handler-specific safety NOT_CHECKED |
| Exact active generation | `(true, nil)` | Existing contract |
| Non-NULL active, newer pending intent | typed deferral | Existing #6686 regression |
| Non-NULL active, older/failed/superseded/missing intent | `(false, nil)` | Existing terminal contract |
| Active generation without identity epoch | handler hard error | Existing corruption control |

## No-Regression Evidence

No-Regression Evidence: PostgreSQL 18.6 on a disposable local container;
the classifier uses the same one-statement SQL over one scope row and the
same-scope generation row. The bounded matched baseline/candidate timing is
recorded below. The controlled live queue rows each begin with one reducer
intent and one projector work item. No extra query, lock, queue retry policy,
or worker reduction was added.

A disposable PostgreSQL 18.6 classifier benchmark compiled the original
`generation_freshness.go` from base commit
`2640c1d21f18ca123fa9861104c37a61912a5221` (source SHA-256
`1574641a6b67a0beaaa93c652a2627ca9b666b2969bc3136c37fb4a027060d89`)
through a Go source overlay, then compiled the candidate source (SHA-256
`f2ec8ff2bb18f5dd30dadd699cdf0cce5453e60546916f98baa71481f9502d7b`)
against separate schemas with identical four-row input shapes. Each
case made three runs of 200 measured database calls (600 measured calls plus
three one-call Go benchmark calibrations per state and variant). Median
wall time per call was:

| Scope state | Baseline ns/op | Candidate ns/op | Candidate outcome |
| --- | ---: | ---: | --- |
| Active exact generation | 87,599 | 78,590 | current |
| Pending, NULL active | 78,630 | 82,085 | typed deferral |
| Failed, NULL active | 72,082 | 84,237 | terminal |
| Superseded, NULL active | 81,571 | 77,727 | terminal |

The baseline classified all three NULL-active cases as current. Candidate
allocations matched the baseline for active and terminal cases (28 and 26
allocations per call respectively); the typed pending deferral used 27 versus
26. The samples overlap and the failed-case median rose, so these small
local samples do not establish a speedup or a regression budget. The SQL is
byte-identical and no extra database round trip was introduced. The
coordinator-held local proof bundle retains the benchmark source/overlay and
raw baseline/candidate logs (SHA-256 `a198518ae398280528256c11e42e645213d927e4ed363d5a84140a9877300bcd`
and `5064033f5a1bbc51102436a6770c1704ff13ec5a56af5837385671df3aa67224`).

The initial post-change live first-generation proof used real
`ReducerQueue.Enqueue/Claim/Fail`, the identity handler, and
`ProjectorQueue.Ack`. Three pre-Ack handler attempts left the row `retrying`
with class `generation_activation_not_ready`, attempt count 1, zero dead
letters, and zero writer calls. Ack then established a positive epoch. The
same intent was reclaimed, handled once, and acknowledged `succeeded`, still
at attempt count 1. The gate uses a test-only empty fact loader and writer
stub for decision writes; it does not prove a full built reducer binary or
graph projection. A real Go JSON event recorded the test's PASS, elapsed
2.31 seconds, at `firstgen-real-go-final.jsonl` in the coordinator-held local proof bundle.

The `go test` unit matrix pins the NULL-active pending, failed, superseded,
missing, and unknown-scope outcomes and the typed error's empty active ID.
Actual sequential and batch `Service.Run` each processed a pending first
generation and an unrelated ready scope: before Ack, the pending row was
`retrying/generation_activation_not_ready`, attempt 1, no dead letter, no
handler entry or writer call, while the ready scope succeeded. Actual
projector Ack established a positive epoch; the next service run entered the
pending handler once and succeeded at attempt 1. The existing active-A/pending-B
live barrier, missing activation epoch sentinel, and reducer pre-activation
controls remain separate controls. The separate built-binary cassette proof below covers the complete unchanged
determinism gate; deployed whole-workload corpus truth remains NOT_CHECKED.

An independently seeded verifier check used the actual first-generation Go JSON
events. With the real PASS event intact, `verify_results` returned 0 and
reported `1/1 PASS`. Removing only that PASS event returned 1 with
`missing or duplicate event (run=1, terminal=0)`. The new file has an exact
`postgres_ci` ledger row and runner tuple. `verify-ledger` selects 81 files and
185 tests after enrolling the failure, supersession, and service dispatch
regressions. This focused check is not a full readiness job result.

A final real `go test -json` run emitted one RUN and one PASS for each of the
four newly enrolled tests (elapsed 1.39, 1.43, 2.99, and 1.47 seconds). The
focused result verifier, using the runner's actual package/file tuples,
accepted those events `4/4 PASS`. Removing only the supersession test's real
PASS event made it fail with `run=1, terminal=0`. The rest of the real event
stream, including its package PASS, was preserved in that seeded RED. The
coordinator-held `all-firstgen-real-go.jsonl` and
`all-firstgen-event-verifier.log` receipts have SHA-256
`bbbed4df191b503b2e8620d846188f64b09fd588f7e43e946d461f2a63aa64f7`
and `5c2a3b5040677c2d62dcb72f7d21636e6672ad069adb32519693ce4420758834`.

## Terminal first-generation exits

A permanent controlled RED deferred a first-generation reducer intent, used
actual `ProjectorQueue.Fail` to mark that generation `failed` while active
remained NULL, and observed the replay's absent-epoch hard error. With the
failed tuple classified terminal, the same replay returned superseded with
zero canonical writes; real reducer Ack left the row succeeded, attempt 1,
unclaimable, and with zero reducer dead letters. The projector dead letter
remained intact.

A second permanent controlled RED deferred an older pending first generation,
added a newer full generation, and used actual `ProjectorQueue.Claim` to
supersede the older projector work and generation. The active pointer remained
SQL NULL and no epoch existed; the older reducer retry hit the absent-epoch
hard error. With the superseded tuple classified terminal, the retry returned
superseded with zero writes and Ack left it succeeded, attempt 1, unclaimable,
with zero reducer dead letters. These are exact same-scope terminal tuples;
missing and unknown lifecycle rows keep their compatibility outcomes.

## Concurrency and operator signals

The classifier reads one statement snapshot and takes no explicit locks.
Projector Ack retains ownership of activation and epoch creation; a reducer
retry cannot force activation. No-Observability-Change: the durable reducer
row records `retrying`, `failure_class=generation_activation_not_ready`, and a
stable attempt count while pending; terminal replay records `succeeded` with
its superseded result. The existing retry surge counter carries the deferral
class, so no metric, span, log field, or cardinality was added. Batch service
dispatch proves unrelated ready-work progress while the first generation is
deferred. Same-scope contention beyond the controlled projector/reducer
transitions is NOT_CHECKED here.

## Complete built-binary cassette proof

The unchanged `scripts/verify-ifa-determinism.sh --keep` gate passed with
exit 0 at source head `702e406a11e6683c7bc12beb32bd9762f2ebf118`, tree
`920c1d7bce592f9582e38f24ee36a390889ed26f`, on base
`dc1cdff0085a93d43dc02a2f64316327c670853b`. It built the six normal host
binaries with Go 1.26.9 and `CGO_CFLAGS=-std=gnu17`, used the gate's
NornicDB/PostgreSQL Compose lane, and ran every N=1/2/4 cell and both standalone
cells. Each N cell produced digest
`5d23e7270963a2d0940900aeb44d7b88db78588f8e80d51dd38320d5af97449e`.

The standalone `deployable_unit_edges` cell matched its exact one-edge set;
the `repo_dependency` cell matched its exact seven-edge set. Their strict
terminal drains reported zero fact residual, zero required nonterminal intents,
zero nonterminal completion events and zero unroutable quarantined intents.
The root wrapper independently captured gate exit 0, cleanup exit 0, identical
before/after source identities and clean status, then confirmed no containers
remained in its Compose project. The source was not edited during this run.

The retained gate log has SHA-256
`b1c369781d3ea145e3e6595d0e0a5792336116e7c4159d2e1bcfee8bb5ef4718`;
the six-binary hash manifest has SHA-256
`3468af302e1b63fe9b8250b0fd1ece98286a70f29d2701bf448796486b92ee39`.
The tested reducer binary has SHA-256
`b1e19f6e678fe26dc93dfac4a859c2a03fc15dfb0cb92aa660134ef20bd6b687`.
This is backend-required cassette/replay correctness evidence, not a deployed
full-corpus or qualified wall-time claim. Cell durations are diagnostic only.

## Final focused coverage

The final PostgreSQL 18.6 run of
`go -C go test ./internal/storage/postgres -run '^TestContainerImageIdentityFirstGeneration.*$' -count=1 -json`
passed all four enrolled tests with no skipped or failed events. The direct
barrier parent passed both `readiness_only` and `expired_lease`; the service
parent passed both `single` and `batch`. The event stream has SHA-256
`5f9661a2bb5ec7890720b7e41165199917a8f6392e8a20123d469456f0c6cf3e`.

The readiness-only case makes more deferrals than `MaxAttempts=3` and retains
attempt 1 through success. Both service claim modes make four pre-Ack
dispatches at `MaxAttempts=2`, retaining attempt 1, zero pending handler
entries and zero reducer dead letters while the unrelated scope remains
succeeded with one writer call. Duplicate enqueue during deferral and after
success admits zero rows; the original row remains unique and terminal work
stays unclaimable. Each direct scenario finishes with one writer call.

The expired-lease case reclaims the same pending first-generation row through
actual `ReducerQueue.Claim` with a higher claim epoch. Stale Ack and Fail are
rejected. Reclaiming claimed execution consumes attempt 2 under the existing
queue policy; subsequent typed readiness deferrals retain attempt 2, with zero
writes and reducer dead letters until activation. Actual projector Ack then
allows the current intent to succeed once. No lease or attempt policy changed.

The initial extension incorrectly expected expired execution reclaim to stay
at attempt 1. Its real PostgreSQL run failed at the attempt-2 assertion.
`reducerClaimAttemptCountCaseSQL` exempts retrying readiness-class rows, not
expired claimed execution. Only the test expectation and scenario structure
changed; the corrected four-test command exited 0. This assertion failure is
not a new production regression or a substitute for the original bug's RED.

The actual readiness result verifier accepted the final four parent test events
`4/4 PASS`. Removing only the barrier parent's PASS event, while preserving its
RUN, subtest PASS events and package PASS, returned 1 with
`missing or duplicate event (run=1, terminal=0)`. This is focused enrollment
proof, not the full 185-test readiness job.

A separate focused command exited 0 for runtime typed deferral, lookup-error
propagation and stale suppression; cloud typed deferral and stale suppression;
search pre-write and finalize lookup errors; repo-dependency freshness and stale
replay; and the non-counting claim/Fail policy units. Its log has SHA-256
`09456e1e48cce84a7696fb3ba95b984029e079d6b1705183011aca884e55441d`.

The preceding combined PostgreSQL command recorded individual PASS results for
the identity Ack reclaim/replay fence, stale/fresh batch Ack orderings, duplicate
terminal reprojection enqueue, the cross-snapshot projector scope fence, the
active-A/pending-B epoch barrier and the loud epoch-miss sentinel. That command
exited 1 solely on the corrected lease-test expectation described above; it is
not reported as a whole-command pass. These unchanged control receipts remain
applicable to their own subjects.
