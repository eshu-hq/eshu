# #5837 / #5848 — `aws_cloud_runtime_drift` cross-scope race, reopen, and insert admission

## Status

**Closed by this branch.** Unlike `docs/internal/evidence/5847-container-image-identity-retire.md`
(the sibling domain, where the retire was withdrawn under review and #5854 stayed
open), `aws_cloud_runtime_drift` ships all four pieces in one change: a bounded
readiness defer at the source of #5837's race, the bootstrap/ingester reopen that
lets a lost race recover, a generation-authoritative retire that makes the reopen
safe, and the begin-before-mutate insert-admission check #5848 asked for. The
prerequisite work #5848's issue text assumed already existed (a `#5837`-landed
retire fence) did not exist on `origin/main` at the time this branch started —
verified by direct search (`rg -n "fencing_token" go/internal/reducer/aws_cloud_runtime_drift*.go`
returned nothing, and no `aws_cloud_runtime_drift_retire_fence_live_test.go` file
existed). This branch builds the retire fence AND the insert-admission check
together, rather than reconstructing a design that was never landed.

## Cross-PR interactions

**Migration-number collision, resolved, then rebased.** This branch forked
from `ba2b7b80b`. `origin/main` advanced past that point with `f1fd95dbc`
(#5469, "resolve the judged version from the strongest deployment-truth
tier"), which claimed migration number `086`
(`086_cloud_resource_owner_runtime_digest_index.sql`) — the same number this
branch first used for `086_aws_cloud_runtime_drift_write_admission.sql`. The
collision was found before rebasing, by diffing `origin/main...HEAD`
(merge-base relative) against a fresh `git fetch origin main` and comparing
changed-file sets, then confirmed by inspecting `f1fd95dbc`'s own migration
directory diff. First resolved by renumbering this branch's migration to
`087` in place (`git mv`, plus the matching `orderedBootstrapDefinitionNames`
entry in `schema_order_test.go`), then the branch was actually rebased onto
`origin/main` (`git fetch origin && git rebase origin/main`) once the
coordinator confirmed the collision was real and requested it. The rebase
conflicted exactly where expected — twice in
`go/internal/storage/postgres/schema_order_test.go`, both times because both
`origin/main` and this branch's own commits touch the tail of
`orderedBootstrapDefinitionNames` — and was resolved by hand to keep `086`
for #5469's `cloud_resource_owner_runtime_digest_index` and `087` for this
branch's `aws_cloud_runtime_drift_write_admission`, consistently across the
migration filename, `schema_order_test.go`, and this doc. The rebase
preserved all 6 original commits (the renumber commit still does real work
post-rebase: the actual `git mv` from `086_...` to `087_...` had not yet
happened at the point the conflict was resolved, so that commit's rename is
not a no-op).

The transient shape where two DIFFERENT migration files briefly share the
numeric prefix `086` across intermediate commits (before the renumber commit
replays) is not a functional defect, even transiently: `BootstrapDefinitions()`
sorts and derives each definition's name from the FULL filename minus its
numeric prefix, and the codebase already has a documented precedent for
exactly this shape —
`schema_order_test.go`'s own comment on migration prefix `075`
("a duplicate-number merge artifact from two independently-landed PRs...
`BootstrapDefinitions()` sorts by migration filename, so
'075_fact_records_active_container_image_slsa_idx' (f) deterministically
precedes '075_kubernetes_live_pod_template_object_index' (k)"). The
renumbering to `087` was still the right call for tidiness and to avoid
ambiguity, not because a shared prefix breaks anything.

Confirmed via `rg -n "086|087" go/internal/storage/postgres/` (every hit
accounted for: `086` names #5469's index consistently, `087` names this
branch's migration consistently, `evidence-5092-relationship-family-guard.md`'s
hits are unrelated timing numbers containing the same digits) and a clean
`go build ./...`/`go vet ./...` on the rebased head.

**`container_image_identity` (#5847/#5851, `a2a5340a9`) is untouched, read-only
reference.** `a2a5340a9` is in this branch's base (an ancestor of `ba2b7b80b`,
the fork point) — it landed before this branch started, so no rebase is
involved in seeing it. `git diff origin/main...HEAD` (merge-base relative,
confirming no drift) shows zero changes to any `container_image_identity`,
`ci_cd_run_correlation`, provenance-edge, or `supply_chain_impact`/suppression
file; the `AWSCloudRuntimeDriftAdmissionBeginner`/`AWSCloudRuntimeDriftTx`
pattern in this branch mirrors `ServiceMaterializationBeginner`'s shape
(#1943), not `a2a5340a9`'s fencing pattern directly, since
`container_image_identity` uses the unversioned `reducerFactRow` family and a
bare execer, while `aws_cloud_runtime_drift` uses the versioned family and
now a transaction.

**#5759 (`DomainMultiCloudRuntimeDrift`) cannot be affected by this branch's
readiness defer — structurally, not by convention.** `#5759` newly enqueues
`DomainMultiCloudRuntimeDrift` for GCP/Azure scopes, and in the golden corpus
those scopes can legitimately have NO Terraform state ever (not late — absent
by design). A readiness defer that could not tell "not yet" from "never" would
either suppress valid orphan findings for those providers or defer them until
the terminal fallback fires on every run. This branch's defer cannot reach
that domain at all: `MultiCloudRuntimeDriftHandler`
(`go/internal/reducer/multi_cloud_runtime_drift.go`) is a SEPARATE Go struct
and `Handle` method from `AWSCloudRuntimeDriftHandler`, with its own field set
(`EvidenceLoader`, `Writer`, `Instruments`, `Logger` — no `ReadinessChecker`
field exists on it, confirmed by direct read of the struct literal). The
`AWSCloudRuntimeDriftReadinessChecker` interface and
`PostgresAWSCloudRuntimeDriftReadinessChecker` implementation are wired
exactly once, in `go/cmd/reducer/wiring_handlers.go`, into
`DriftHandlers.AWSCloudRuntimeDriftReadinessChecker`, which
`defaults_additive_domains_secrets_drift.go` threads only into
`AWSCloudRuntimeDriftHandler{ReadinessChecker: ...}` — never into
`MultiCloudRuntimeDriftHandler`. `rg -n "PostgresAWSCloudRuntimeDriftReadinessChecker|HasPendingStateSnapshotEvidence"
go/ -g '*.go'` outside tests returns exactly those wiring lines and the
implementation itself — no other call site exists. For AWS itself, the
distinction the coordinator's question raises still matters and is answered
by the design already documented above: the checker's signal is coarse
("is ANY `state_snapshot:*` scope pending", not "is the specific backend for
THIS ARN pending"), which is DELIBERATE — it can only ever cause an
unnecessary, bounded (30-minute ELAPSED-TIME, not retry-count — see
`awsCloudRuntimeDriftStatePendingMaxWait`, round-3 review) defer, never an
incorrect verdict, because the terminal fallback always commits the
classification the evidence actually supports once the bound is reached,
whether or not any state scope ever activates. Extending this pattern to
`MultiCloudRuntimeDriftHandler` in the
future would need its own analysis of what "pending" means for GCP/Azure
state backends; this branch does not attempt that and cannot regress it.

## The four pieces, and why each is required

1. **Readiness defer** (`go/internal/reducer/awscloud/aws_cloud_runtime_drift_readiness.go`).
   `AWSCloudRuntimeDriftHandler.shouldDeferForLoadedEvidence` (checked BEFORE the
   evidence load — see the round-5 P1 addendum below) holds back an
   `orphaned_cloud_resource` classification when a Terraform `state_snapshot:*`
   scope is still mid-ingestion (`scope_generations.status = 'pending'`), for up
   to `awsCloudRuntimeDriftStatePendingMaxWait` (30 minutes of ELAPSED WALL-CLOCK
   TIME since `Intent.CycleStartedAt`, not a retry-count bound — round-2/round-3
   review; see the round-3 P2-2 addendum). Past the bound, Handle commits its
   best-available verdict — the terminal fallback a genuine orphan (no Terraform
   anywhere, ever) needs, so a permanently-pending or never-registered state
   scope cannot starve the intent forever. This addresses #5837's actual root
   cause directly: the AWS scope's drift intent draining before the tfstate
   scope's generation activates, which made `cloudruntime.Classify` read absent
   state as a VERDICT rather than as "not ready".
2. **Bootstrap/ingester reopen**
   (`go/internal/storage/postgres/ingestion_reopen_correlation.go`).
   `aws_cloud_runtime_drift` is added to `CrossScopeCorrelationReopenDomains()`,
   mirroring the `container_image_identity` precedent (#5846). Before this, the
   domain's intent ran exactly once per (scope, generation) — the projector's
   `EntityKey` dedupes on scope, and re-enqueue hits `ON CONFLICT DO NOTHING` — so
   a pass that lost the readiness race (or ran before this branch existed at all)
   had nothing to replay it.
3. **Generation-authoritative retire**
   (`go/internal/reducer/awscloud/aws_cloud_runtime_drift_writer_queries.go`). The reopen
   alone would make #5837 WORSE without this: the fact identity embeds
   `finding_kind`, so a corrected replay reclassifying an ARN mints a NEW
   `fact_id` beside the old one. The retire removes a stale finding for any ARN
   the current pass evaluated whose fact_id is not among the rows it just wrote,
   bounded by the same fencing token as the insert so it can never delete a
   fresher row.
4. **Insert-admission check** (`go/internal/reducer/awscloud/aws_cloud_runtime_drift_admission.go`).
   #5848's actual ask: a begin-before-mutate check so a pass whose evidence-read
   watermark is older than one already admitted for the same (scope, generation)
   is rejected BEFORE it inserts or retires anything. Closes the residual the
   retire alone leaves open: two DIFFERENT fact_ids (a reclassification) never
   collide on the insert's own `ON CONFLICT` guard, so without this check a
   stalled worker's stale insert can still land unopposed after a fresher
   worker's pass has already committed.

## Design: why a transaction, not three independent statements

`PostgresAWSCloudRuntimeDriftWriter.WriteAWSCloudRuntimeDriftFindings` runs the
admission check, the versioned insert, and the retire inside ONE transaction
(`AWSCloudRuntimeDriftBeginner`/`AWSCloudRuntimeDriftTx`,
`go/internal/storage/postgres/aws_cloud_runtime_drift_admission_beginner.go`
provides the production adapter over the shared `postgres.Beginner`). Three
separate auto-committed statements would leave a gap: a pass could pass the
admission check, then stall before issuing its insert, while a fresher pass ran
its own admission-check-through-retire to completion in between — the admission
check alone, without a transaction, only orders WHEN a pass is admitted, not
whether it can still be preempted before it finishes writing. Wrapping all three
in one transaction closes that: Postgres's own row lock on the admission table's
`INSERT ... ON CONFLICT DO UPDATE` serializes two concurrent attempts against the
SAME (scope, generation) key, and whichever commits first sets the value the
loser's own `WHERE fencing_token <= EXCLUDED.fencing_token` check re-evaluates
against once its lock is granted.

The writer's `DB` field changed from a bare `workloadIdentityExecer` to
`AWSCloudRuntimeDriftBeginner` for this reason — the original issue text's
caveat ("the drift writer currently holds a bare execer rather than a
transaction-capable handle") is resolved rather than worked around.

## Concurrency analysis

- **Shared state / conflict domain:** one row in
  `aws_cloud_runtime_drift_write_admission` keyed by `(scope_id, generation_id)`;
  the `reducer_aws_cloud_runtime_drift_finding` rows in `fact_records` for that
  same (scope, generation), further partitioned by ARN via `payload->>'arn'`.
- **Lock/claim ordering:** admission CAS first, unconditionally, inside the
  transaction; insert and retire only run if admission succeeds. Two concurrent
  transactions targeting the same (scope, generation) serialize on Postgres's
  row lock for the conflicting admission key; the second to acquire the lock
  re-evaluates its own `WHERE` against the first's now-committed value.
- **Transaction scope:** admission + insert + retire commit or roll back
  together, one transaction per `WriteAWSCloudRuntimeDriftFindings` call.
- **Retry scope:** the whole `Handle` call is one retry unit at the reducer queue
  level; a rejected write (or a deferred readiness gate) returns a
  `Retryable() == true` error so the queue reschedules the intent.
- **Idempotency key:** `fact_id` (candidate identity, including `finding_kind`)
  for the insert's own upsert; `(scope_id, generation_id)` for the admission
  watermark. `FencingToken` (round-5 P1 addendum: now a database-issued
  Postgres sequence value, not `EvidenceAsOf.UnixMicro()`) is captured once per
  `AWSCloudRuntimeDriftHandler.Handle` call at evidence-read time; a caller that
  legitimately reuses one already-issued token across a retry of the identical
  write attempt is admitted again (`<=`, not `<`), so redelivery of that shape
  is a no-op, not a rejection.
- **Starvation:** none introduced. A rejected pass is not serialized behind
  anything — it is told to retry, and a retry that reads fresher evidence gets a
  higher token and is very likely admitted (unless an even-fresher concurrent
  pass wins first, in which case it correctly defers again). The readiness
  defer's bound (30 minutes of elapsed wall-clock time since
  `Intent.CycleStartedAt`, not a retry-count bound — round-2/round-3 review) is
  the other starvation guard: it prevents an indefinite hold on a state scope
  that will never activate.
- **Dead-letter behavior:** both `awsCloudRuntimeDriftWriteSupersededError` and
  `awsCloudRuntimeDriftStatePendingError` self-classify as non-counting retry
  classes (`AWSCloudRuntimeDriftWriteSupersededFailureClass`,
  `AWSCloudRuntimeDriftStatePendingFailureClass`, both added to
  `nonCountingReducerRetryFailureClasses`,
  `go/internal/storage/postgres/reducer_queue_readiness_sql.go`), so losing a
  normal race or waiting on a pending state scope never erodes the retry budget
  or dead-letters the intent — the acceptance criterion "refusing a pass does
  not consume its retry budget in a way that turns a normal race into a
  dead-letter" is satisfied by reusing the exact mechanism EC2/GCP/Kubernetes
  readiness misses already use, not a new one.
- **Serialization avoided:** no worker-count reduction, no batch-size-1, no
  domain-wide exclusive lock. The conflict domain is partitioned by (scope,
  generation) — concurrent passes for DIFFERENT scopes or generations never
  contend, and a fresher pass for the SAME scope/generation is never blocked by
  an older one; it wins the race and the older one is told to retry.

### P0 found and fixed by pre-PR review: the two failure classes were never registered

The "Dead-letter behavior" bullet above states both new failure classes are in
`nonCountingReducerRetryFailureClasses`. That statement was FALSE from the
first commit that declared the two classes through the rebase-completion
commit: the classes existed only as `const` declarations next to the error
types that return them
(`go/internal/reducer/awscloud/aws_cloud_runtime_drift_admission.go`,
`aws_cloud_runtime_drift_readiness.go`) — `go/internal/storage/postgres/reducer_queue_readiness_sql.go`,
the actual registry, was never touched. A declared-but-unregistered class is
invisible to both `retryable()`'s non-counting check and
`reducerClaimAttemptCountCaseSQL()`'s CASE, so it silently falls through to
counting behavior: `attempt_count` increments on every retrying claim exactly
like an ordinary failure, and `ReducerQueue.Fail` dead-letters once
`attempt_count >= MaxAttempts` (default `ESHU_REDUCER_MAX_ATTEMPTS=3` —
numerically identical to `awsCloudRuntimeDriftStatePendingMaxAttempts`, so the
two thresholds only failed to collide by coincidence in the local proofs run
so far). Because the domain is also in the bootstrap/ingester reopen slice
(succeeded-only), a dead-lettered item is never recovered by that path either
— the net effect this branch would have shipped is a PERMANENTLY ABSENT
finding for an ARN, which is strictly worse than the #5837 bug being replaced
(a wrong-but-present, later-reparable finding). This falsified #5848's own
acceptance criterion ("refusing a pass does not consume its retry budget in a
way that turns a normal race into a dead-letter") as shipped, despite every
other piece of the design being correct and separately proven.

Found by pre-PR hostile review, not by any test in this branch:
`TestAWSCloudRuntimeDriftHandlerCommitsAfterAttemptBoundReached` (the existing
Handler-level unit test) cannot catch this class of bug because it hand-sets
`Intent.AttemptCount` and never exercises `ReducerQueue`'s real claim/retry
SQL or `Fail` path — it only proves the HANDLER's own bound logic, not that
the QUEUE agrees the class is exempt.

Fixed by registering both classes in `nonCountingReducerRetryFailureClasses`
(`reducer_queue_readiness_sql.go`), and adding four queue-level regression
tests (`go/internal/storage/postgres/reducer_queue_aws_cloud_runtime_drift_readiness_test.go`)
following the existing GCP/EC2/cross-scope precedent
(`TestReducerQueueFailDefersGCPRelationshipReadinessPastAttemptBudget`,
`TestReducerQueueFailDefersEC2InstanceIdentityReadinessPastAttemptBudget`) —
two `Fail()`-at-`AttemptCount=42` tests (one per class) proving the row is
re-queued `retrying`, not `dead_letter`, and two claim-query tests (single and
batch) proving the attempt-count CASE keeps both classes' `attempt_count`
frozen. Failing-before/passing-after, by temporarily reverting the
registration and rerunning:

```
$ go test ./internal/storage/postgres -run "TestReducerQueueFailDefersAWSCloudRuntimeDrift|TestReducerQueueClaimDoesNotCountAWSCloudRuntimeDrift|TestClaimBatchDoesNotCountAWSCloudRuntimeDrift" -v -count=1
--- FAIL: TestReducerQueueFailDefersAWSCloudRuntimeDriftStatePendingPastAttemptBudget (0.00s)
    deferred retry query missing "status = 'retrying'":
    UPDATE fact_work_items
    SET status = 'dead_letter', ...
--- FAIL: TestReducerQueueFailDefersAWSCloudRuntimeDriftWriteSupersededPastAttemptBudget (0.00s)
    deferred retry query missing "status = 'retrying'":
    UPDATE fact_work_items
    SET status = 'dead_letter', ...
--- FAIL: TestReducerQueueClaimDoesNotCountAWSCloudRuntimeDriftReadinessDefers (0.00s)
--- FAIL: TestClaimBatchDoesNotCountAWSCloudRuntimeDriftReadinessDefers (0.00s)
# registration restored
--- PASS: TestReducerQueueFailDefersAWSCloudRuntimeDriftStatePendingPastAttemptBudget (0.00s)
--- PASS: TestReducerQueueFailDefersAWSCloudRuntimeDriftWriteSupersededPastAttemptBudget (0.00s)
--- PASS: TestReducerQueueClaimDoesNotCountAWSCloudRuntimeDriftReadinessDefers (0.00s)
--- PASS: TestClaimBatchDoesNotCountAWSCloudRuntimeDriftReadinessDefers (0.00s)
```

The two red `Fail()` transcripts show the literal bug: `status = 'dead_letter'`
where `status = 'retrying'` was expected — a normal admission-race loss or a
still-pending state scope, both bounded and expected to self-correct, instead
permanently terminalizing the work item at `MaxAttempts`.

The source doc comments on `AWSCloudRuntimeDriftWriteSupersededFailureClass`
and `AWSCloudRuntimeDriftStatePendingFailureClass` (both already stated "the
reducer queue treats it as a non-counting retry class... added to
nonCountingReducerRetryFailureClasses") did not need editing once the
registration landed — they described the intended, now-actual, state
correctly; they were simply describing code that had not been written yet.
One earlier commit message on this branch ("Refs #5848: transactional
insert-admission, retire, and readiness defer for aws_cloud_runtime_drift")
also asserts the non-counting registration; that commit's message is not
rewritten (this branch was explicitly told not to rebase again, and rewriting
a non-tip commit message without an interactive rebase — barred by
`CLAUDE.md` — is not attempted), but the claim is corrected here, in the
current source, and in the tests, which is what a reader or a future `git
blame` will actually execute against.

### P0 (second, caused by the first P0's own fix): registering the classes as non-counting froze the counter the terminal-fallback bound read

Registering both classes in `nonCountingReducerRetryFailureClasses` (the fix
immediately above) closed the dead-letter hole, but it did so by making
`reducerClaimAttemptCountCaseSQL()` freeze `fact_work_items.attempt_count` for
any row claimed while `status='retrying'` under either class — and the
readiness defer's own terminal fallback was, at that point, still comparing
against exactly that frozen counter:
`intent.AttemptCount >= awsCloudRuntimeDriftStatePendingMaxAttempts`.

Concrete trace, reproduced live against a fake `ReducerQueue` backend driven
through real `Claim`/`Fail` cycles: first claim increments `attempt_count`
0→1 (the row is still `'pending'`, so the non-counting CASE branch does not
apply yet — it only applies once the row has ALREADY been retried once under
the class). `Handle()` sees `AttemptCount=1`, `1 < 3`, defers. `Fail()` sets
`status='retrying'` with the state-pending failure class and — correctly,
per the fix above — leaves `attempt_count` untouched. Every subsequent claim
now matches `status='retrying' AND failure_class IN (...)`, so the CASE keeps
`attempt_count` unchanged FOREVER. `Handle()` sees `AttemptCount=1` on every
future call; `1 >= 3` is never true. If any `state_snapshot:*` scope gets
durably stuck in `'pending'` — an ordinary operational fault, not a rare
race — every orphaned-candidate intent for that account would defer forever:
a **permanently absent** finding, silent and gate-invisible in an environment
(like the golden corpus's) where the state scope does eventually activate on
its own.

This is the same class of bug as the first P0 — a promise made by a doc
comment that the code did not actually keep — but caused BY that P0's own
fix, not independent of it: closing the dead-letter hole is what exposed the
counter-freeze, since before registration the counter was (wrongly)
incrementing every cycle and would have crossed the bound eventually, just by
dead-lettering first.

**Fix: the domain's bound is now elapsed wall-clock time since
`Intent.EnqueuedAt`, not `Intent.AttemptCount`.**
`awsCloudRuntimeDriftStatePendingMaxAttempts` (an int, 3) is replaced by
`awsCloudRuntimeDriftStatePendingMaxWait` (a `time.Duration`, 30 minutes).
`Intent.EnqueuedAt` is populated from `fact_work_items.created_at`
(`go/internal/storage/postgres/reducer_queue_helpers.go`), which no claim,
retry, or fail statement in this package ever writes — it is immune to the
same freeze precisely because nothing needs it to change across retries. "How
long have we been waiting for a sibling scope to activate" is a time concept,
not a retry-count concept, so bounding it on elapsed time is also the more
honest match for what is actually being bounded, independent of the freeze
bug. A zero `EnqueuedAt` (unreachable against the real queue, but reachable
from a hand-built `Intent` in a test or a future caller) is treated as
"elapsed time unknown" and defers rather than committing immediately — the
safe direction, since a misread zero timestamp must not manufacture a false
"expired" signal that writes a possibly-wrong verdict.

Considered and rejected: a domain-owned defer counter (a payload field bumped
unconditionally on every `state_pending` fail), the other option raised
during review. Elapsed time was preferred because it matches the actual thing
being bounded, needs no new durable field or idempotency story for that
field's own updates, and cannot recreate the SAME class of coupling defect in
a new place — a payload-field counter would still need its own proof that
something actually bumps it on every cycle, which is exactly the kind of
implicit coupling this bug came from in the first place.

**The regression that must exist, and could not be a Handler-level test
alone:** `TestAWSCloudRuntimeDriftHandlerCommitsAfterElapsedBoundReached`
(reducer package) proves the Handler's OWN bound logic given an
already-elapsed `EnqueuedAt`, but a hand-set field cannot prove the QUEUE ever
delivers one — that is exactly what the ORIGINAL, wrong
`AttemptCount`-comparison version of this same test (hand-setting
`AttemptCount: awsCloudRuntimeDriftStatePendingMaxAttempts`) proved instead,
and why it missed the bug entirely. The real proof is
`TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_elapsed_bound_queue_test.go`):
it drives the REAL `reducer.ReducerQueue.Claim`/`Fail` through repeated
cycles against a fake backend that computes `attempt_count` with the exact
production CASE semantics, advancing a fake wall clock between cycles (a real
30-minute wait is infeasible to run in a test), and asserts the domain
eventually reaches a terminal commit. Failing-before/passing-after, by
temporarily reverting `shouldDeferForStatePending`'s bound check back to the
original `intent.AttemptCount >= 3` shape and rerunning:

```
$ go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive -v -count=1
--- FAIL: TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive (0.00s)
    domain never reached a terminal commit within 10 cycles (2h0m0s of simulated elapsed time):
    attempt_count sequence seen = [1 1 1 1 1 1 1 1 1 1] -- the elapsed-time bound did not fire
# fix restored
--- PASS: TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive (0.00s)
    converged after 4 claim/fail cycles, 36m0s elapsed, attempt_count sequence = [1 1 1 1] (frozen), writer.calls = 1
```

The red transcript's `attempt_count` sequence — `[1 1 1 1 1 1 1 1 1 1]`,
frozen from the first cycle onward across 2 simulated hours — is the exact
mechanism the review predicted, reproduced independently. The green
transcript shows the SAME frozen sequence (proving the freeze itself is real
and expected, not something the fix "solves" by un-freezing the counter) but
now converges at 36 minutes — past the 30-minute bound — because the
comparison no longer depends on that frozen value at all.

`awsCloudRuntimeDriftStatePendingMaxWait`'s own doc comment
(`go/internal/reducer/awscloud/aws_cloud_runtime_drift_readiness.go`) now states this
history and the reason directly, so a future reader does not reintroduce an
`AttemptCount` comparison believing it to be equivalent.

**Confirmation: a genuine orphan still converges.** Two shapes matter, and
only one needs the bound at all. (1) No `state_snapshot:*` scope exists for
the account -- `HasPendingStateSnapshotEvidence` returns `false` on the very
first call, `shouldDeferForStatePending` returns `false` immediately, and
`Handle` writes the correct `orphaned_cloud_resource` verdict on the FIRST
attempt, never touching the bound at all (existing coverage:
`TestAWSCloudRuntimeDriftHandlerDoesNotDeferWhenStateNotPending`, still
green). (2) A `state_snapshot:*` scope exists but is durably stuck in
`'pending'` -- an ordinary operational fault (a crashed or hung ingester),
not a rare race -- and the readiness checker is deliberately coarse (see
`AWSCloudRuntimeDriftReadinessChecker`'s doc comment): it cannot tell "the
ONE scope that would resolve THIS ARN is stuck" apart from "SOME unrelated
scope is stuck", so a resource that is a genuine orphan (no Terraform state
anywhere will ever claim it) gets held back by a sibling scope's fault that
has nothing to do with it. This is exactly what
`TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive`
proves: `alwaysOrphanedAWSCloudRuntimeDriftEvidenceLoader` returns a resource
with no Terraform match at all (an orphan by construction, not by a
transient miss), `alwaysPendingAWSCloudRuntimeDriftReadinessChecker` never
resolves (the permanently-stuck-scope shape), and the handler still reaches
`writer.calls == 1` with `CanonicalWrites` covering the one orphaned
candidate once elapsed time crosses the bound -- the genuine orphan's correct
verdict is written, not a placeholder or an error. Shape (2) is the only one
that was ever at risk from the freeze; the test proves it converges.

**Two P3s recorded, not built:** (1) no signal distinguishes "converging
soon" from "will never converge" for a `state_pending` defer; only the
generic `ReducerRetrySurge{failure_class=...}` counter exists. Moot once this
fix lands (the elapsed bound guarantees convergence), but worth an
operator-facing gauge if this pattern is reused by a future domain with a
longer bound. (2) commit `c161855f8` still asserts the ORIGINAL registration
as accomplished fact in its message; per the no-rebase-yet constraint that
message is not rewritten here either, and is left for the coordinator's own
planned rebase before push.

### P1 found by pre-PR review: exact-tie watermarks resolve last-committer-wins, not fresher-wins

`fencingToken = evidenceAsOf.UnixMicro()` is a wall-clock LABEL taken when a
pass starts reading evidence, not a database read snapshot or a logical
clock. Two genuinely independent passes — a live pass racing a
maintenance-reopen replay of the same intent, or a duplicate claim after
lease theft — can therefore read DIFFERENT evidence yet stamp the IDENTICAL
microsecond token. On that exact tie, `stored <= EXCLUDED` is satisfied by
equality (required so the equal-token retry case stays admitted — the SAME
pass redelivered must not be rejected), so BOTH transactions are admitted,
and the retire step then means whichever transaction COMMITS SECOND wins:
its retire deletes whatever the first transaction just inserted before
inserting its own row. This is last-committer-wins, not fresher-wins — on an
exact tie the token cannot express which pass read evidence more recently,
because both stamped the same value.

The existing tests before this fix covered the same-pass retry tie
(`TestAWSCloudRuntimeDriftInsertAdmissionAppliesEqualTokenRetryLive`, one
candidate set, redelivered) and the strictly-ordered stale-then-fresh case,
but nothing drove two DIFFERENT candidate sets through a genuinely tied
token — so the actual tie-breaking rule was unproven, and the doc comments on
`awsCloudRuntimeDriftAdmissionQuery` and the two error types implied
"fresher wins" without qualification.

Fixed by adding
`TestAWSCloudRuntimeDriftInsertAdmissionResolvesExactTieByLastCommitLive`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_admission_live_test.go`),
which drives the SAME tied watermark through two DIFFERENT classifications
(`orphaned_cloud_resource` vs. `image_version_drift`) for one ARN, in BOTH
call orders, and asserts the pass called SECOND wins in both orderings —
exactly what last-committer-wins predicts and fresher-wins (a property of the
evidence, not the call order) would not, since neither candidate is
chronologically distinguishable from the other by construction:

```
$ go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftInsertAdmissionResolvesExactTieByLastCommitLive -v -count=1
=== RUN   TestAWSCloudRuntimeDriftInsertAdmissionResolvesExactTieByLastCommitLive/orphaned_called_first,_image_version_drift_called_second
=== RUN   TestAWSCloudRuntimeDriftInsertAdmissionResolvesExactTieByLastCommitLive/image_version_drift_called_first,_orphaned_called_second
--- PASS: TestAWSCloudRuntimeDriftInsertAdmissionResolvesExactTieByLastCommitLive (0.66s)
    --- PASS: .../orphaned_called_first,_image_version_drift_called_second (0.02s)
    --- PASS: .../image_version_drift_called_first,_orphaned_called_second (0.02s)
```

Both subtests pass: reversing call order reverses the surviving
classification, confirming the winner tracks commit order, not evidence
freshness. This is a characterization test of the intended, correct-by-design
behavior, not a bug fix — there is no code change backing it, only the test
and the doc correction. `awsCloudRuntimeDriftAdmissionQuery`'s own doc
comment (`go/internal/reducer/awscloud/aws_cloud_runtime_drift_admission.go`) now
states the actual rule explicitly under a `# The actual rule on an exact
watermark tie is last-committer-wins, not fresher-wins` heading, rather than
leaving "fresher wins" implied.

Real-world impact is judged low and left unresolved on purpose rather than
forcing a stronger guarantee: a later untied pass, or the reopen slice's own
next replay, corrects a wrong tie-broken verdict the same way it corrects any
other stale one. Making the tie deterministic on a different axis (e.g.
tie-breaking on `fact_id` or a monotonic counter alongside the wall-clock
label) is a design change with its own concurrency argument and was judged
out of scope for closing #5848/#5837; documenting the real rule, so nobody
relies on a stronger guarantee than what ships, is what this fix provides.

### P2 found by round-3 review, closed by round-4 review: a reopened row got no grace window at all

Closing the round-2 P0 above (bounding the defer on elapsed time instead of
`AttemptCount`) fixed the freeze but exposed a second gap: the elapsed bound
was anchored on `Intent.EnqueuedAt` alone, which is populated from
`fact_work_items.created_at` -- immutable for the life of the row, INCLUDING
across a reopen. `aws_cloud_runtime_drift` is unconditionally reopened on
every ingester shard drain and bootstrap maintenance pass
(`postgres.CrossScopeCorrelationReopenDomains`), so for essentially every
real-deployment reopen, more than the bound has already elapsed since the
row's ORIGINAL enqueue. Anchored on `EnqueuedAt` alone,
`shouldDeferForStatePending` would return `false` on the very first claim of
a reopened row WITHOUT EVER CONSULTING THE READINESS CHECKER -- a degraded
verdict committed immediately, even when the `state_snapshot` scope is
freshly, legitimately still pending for that repair cycle. This is the exact
no-grace-window regression the pre-#5848 `AttemptCount = 0` reset on reopen
used to prevent (before its own freeze bug made it not actually work).

**Fix (round-3, option (b) as directed -- "anchor the bound to the current
repair cycle, not the original enqueue"):** `fact_work_items.reopened_at`
(migration `088_reducer_work_item_reopened_at.sql`), reset by
`ReopenSucceeded`/`ReplayDomain` (`reducer_queue_replay.go`) in the SAME
statement that already resets `attempt_count = 0`. The claim query
(`reducer_queue_claim_query.go`, `reducer_queue_batch_query.go`) now returns
`COALESCE(work.reopened_at, work.created_at) AS cycle_started_at`, surfaced on
`reducer.Intent` as the new `CycleStartedAt` field. `awsCloudRuntimeDriftCycleAnchor`
(`aws_cloud_runtime_drift_readiness.go`) prefers `CycleStartedAt` when set,
falling back to `EnqueuedAt` only for a hand-built `Intent` (a test, or a
future caller) that does not populate it -- against the real queue,
`CycleStartedAt` is always populated, so the fallback never fires there.

**Freeze-immunity, verified the same way `created_at`'s was:** every
non-claim statement that mutates `fact_work_items` was read directly --
`ackReducerWorkQuery`, `failReducerWorkQuery`, `retryReducerWorkQuery`, and
`heartbeatReducerWorkQuery` (`reducer_queue.go`) -- and none sets
`reopened_at`. Only `reopenSucceededReducerWorkQuery` and
`replaySucceededReducerDomainQuery` do, and both gate on `WHERE status =
'succeeded'`. A row mid-defer is `'retrying'`, never `'succeeded'`, so no
maintenance cadence, however frequent, can reset the anchor mid-cycle --
confirmed independently by the round-4 review reading the same statements.
Only an already-terminal row reopens, starting a fresh, independently-bounded
30-minute cycle.

**A commit made during an upstream API outage (`d7cd88cab`) cited a live test
by name -- `TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive`
-- in three places (this domain's readiness doc comment,
`go/internal/storage/postgres/AGENTS.md`, and a test-file comment) before
that test existed.** The round-4 review caught it: the coordinator wrote that
commit message to preserve 31 uncommitted files across a session death, and
lifted the test name out of in-progress doc comments without checking it had
actually been written -- the same failure shape (a doc comment asserting a
registration/test that was never made) the round-1 P0 caught, this time in
the coordinator's own recovery commit rather than the executor's. The test
now exists, at
`go/internal/storage/postgres/aws_cloud_runtime_drift_reopen_anchor_live_test.go`,
so all three citations resolve.

It drives the REAL `ReducerQueue.Enqueue`/`Claim`/`Ack`/`ReopenSucceeded`/`Claim`
sequence against real Postgres, then the REAL `AWSCloudRuntimeDriftHandler`:
enqueue a work item with `created_at` far in the past, claim and ack it to
`'succeeded'`, advance the clock more than `awsCloudRuntimeDriftStatePendingMaxWait`
past the original enqueue, `ReopenSucceeded` it, claim it again, and confirm
the returned `Intent.CycleStartedAt` reflects the REOPEN time (not the
original enqueue) before feeding it through `Handle` with a permanently
`'pending'` `state_snapshot` scope. Failing-before/passing-after, by
temporarily reverting `awsCloudRuntimeDriftCycleAnchor` to `return
intent.EnqueuedAt` (bypassing `CycleStartedAt` entirely) and rerunning:

```
$ ESHU_POSTGRES_DSN=postgres://eshu:change-me@localhost:15948/eshu?sslmode=disable \
    go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive -v -count=1
--- FAIL: TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive (5.23s)
    Handle() error = nil, want a deferred (retryable) error: a reopened claim whose ORIGINAL
    enqueue is long past the bound must still get a fresh grace window for its own repair
    cycle, not commit a degraded verdict on the very first reopened claim
# fix restored
--- PASS: TestAWSCloudRuntimeDriftReopenGetsFreshElapsedBoundWhileStatePendingLive (1.35s)
    reopen anchor held: EnqueuedAt 2h0m0s old, CycleStartedAt 0s old at reopen -- Handle
    deferred, zero rows written
```

**Unit coverage added for the previously-uncovered anchor selection itself**
(round-4 P1: `awsCloudRuntimeDriftCycleAnchor` and `Intent.CycleStartedAt` had
zero direct test coverage before this fix -- `reducer_queue_replay_test.go`'s
`reopened_at = $1` assertions are `strings.Contains` checks against a
hand-built fake and do not count): `TestAWSCloudRuntimeDriftCycleAnchorPrefersCycleStartedAt`
(table-driven over the CycleStartedAt-set, CycleStartedAt-zero-falls-back-to-EnqueuedAt,
and both-zero branches) and `TestAWSCloudRuntimeDriftHandlerDefersOnFreshCycleStartedAtDespiteStaleEnqueuedAt`
(a fast, in-process Handle()-level companion to the live test above, proving
`CycleStartedAt` governs the decision even when `EnqueuedAt` alone is already
past the bound) -- both in
`go/internal/reducer/awscloud/aws_cloud_runtime_drift_readiness_test.go`.

**Convergence in both directions, confirmed:** a genuinely permanent orphan
(no Terraform state anywhere, ever, and no reopen) still converges through
the existing elapsed-bound mechanism (`TestAWSCloudRuntimeDriftHandlerConvergesAfterElapsedBoundOverRealQueueLive`,
documented above). A transient case -- a `state_snapshot` scope that is
merely slow, not permanently stuck -- is revisited and reclassified once
state activates, whether via an ordinary retry within the same cycle
(`TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive`, Phase 3)
or via a maintenance-triggered reopen starting a fresh cycle (this section's
live test proves the fresh cycle is granted; a subsequent Handle call within
that fresh cycle, once state activates, reclassifies exactly as Phase 3
does). The round-4 review noted, and this doc records as an accepted
trade-off requiring no further action: a genuinely permanent orphan re-runs
the up-to-30-minute defer on every maintenance reopen forever, which is
wasteful (repeated deferred cycles that always converge) but never a
correctness defect, since each cycle is independently bounded and always
reaches its own terminal commit.

### Round-5 review (codex, PR #5875): two P1s found after the README split and rebase

A hostile review against the post-split, post-rebase diff found two P1s in
`aws_cloud_runtime_drift.go`, both fixed on this branch with TDD failing-first
regressions.

#### P1: readiness was checked AFTER the evidence snapshot (TOCTOU)

If a `state_snapshot` generation activated AFTER `LoadAWSCloudRuntimeDriftEvidence`
snapshotted the active rows but BEFORE the (then-)post-load readiness check,
the checker reported "ready" and Handle did not defer, persisting the STALE
`orphaned_cloud_resource` computed from the pre-activation snapshot. Worse,
the repair path could not save it: `ReopenSucceeded`'s reopen selects only
`'succeeded'` rows, so a maintenance pass racing this exact intent while it
was still claimed/running would skip it, leaving no guaranteed later repair —
the same "written durably as a success, never repaired" shape #5837 is about,
reintroduced through a narrower window.

**Fix:** `Handle` now checks readiness BEFORE the evidence load (`aws_cloud_runtime_drift.go`),
capturing the pending signal into a local `awsCloudRuntimeDriftReadinessSignal`
that the ACTUAL defer decision (`shouldDeferForLoadedEvidence`, in
`aws_cloud_runtime_drift_readiness.go`) later combines with whether the
freshly loaded evidence contains an orphaned candidate — the check is never
re-run against the loader's own, possibly-newer, view of the world. This
preserves the "only defer when it could actually matter" optimization instead
of broadening every pass to defer whenever any `state_snapshot` scope
anywhere is pending. The reverse race — state activating BETWEEN the check
and the load — is benign, and in fact costs NOTHING when the fresher evidence
resolves cleanly (proven by the second test below), since evidence is always
loaded regardless of the pre-load signal.

Deterministic (not statistical) interleaving proof, mirroring the technique
`TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive` already uses
for the original #5837 race: `transitioningAWSCloudRuntimeDriftEvidenceLoader`
(`aws_cloud_runtime_drift_toctou_test.go`) flips the SAME readiness-checker
stub's `pending` flag the moment `LoadAWSCloudRuntimeDriftEvidence` is called,
forcing the exact interleaving into existence rather than racing goroutines
against a clock. Failing-before/passing-after, by reverting the
check-before-load ordering back to check-after-load and rerunning:

```
$ go test ./internal/reducer -run TestAWSCloudRuntimeDriftHandlerDefersDespiteStateActivatingDuringEvidenceLoad -v
--- FAIL: TestAWSCloudRuntimeDriftHandlerDefersDespiteStateActivatingDuringEvidenceLoad (0.00s)
    Handle() error = nil, want a deferred error: the readiness check must observe the PRE-transition
    pending state, not a state the evidence load's own timing could have already raced past
# fix restored
--- PASS: TestAWSCloudRuntimeDriftHandlerDefersDespiteStateActivatingDuringEvidenceLoad (0.00s)
```

`TestAWSCloudRuntimeDriftHandlerWritesFresherEvidenceWhenStateActivatesBeforeLoad`
proves the reverse race's actual safety property directly: a stale
pending=true pre-load signal combined with freshly-loaded, already-matched
evidence writes the correct verdict immediately, with zero wasted defers.

#### P1: the fencing token was the reducer host's wall clock, not a durable ordering value

`fencingToken` was `evidenceAsOf.UnixMicro()` — the REDUCER HOST'S wall clock.
With modest clock skew between reducer replicas, an OLDER worker on a
fast-clock host could carry a LARGER token than a LATER worker on a correct
clock. If the fresher worker committed first, the stale older worker was then
admitted afterward and its retire could replace correct truth with stale
truth — defeating the whole point of the admission CAS this branch's #5848
piece exists to provide. This is the general form of the round-1 exact-tie
finding: skew does not just tie the ordering, it can invert it outright.

**Fix:** `AWSCloudRuntimeDriftFencingTokenIssuer`
(`aws_cloud_runtime_drift_admission.go`), backed in production by
`PostgresAWSCloudRuntimeDriftFencingTokenIssuer`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_fencing_token.go`)
issuing `nextval()` from a new Postgres sequence
(`aws_cloud_runtime_drift_fencing_token_seq`, migration
`089_aws_cloud_runtime_drift_fencing_token_sequence.sql`, seeded above any
pre-existing wall-clock-derived watermark for a safe live-deployment
rollout). `AWSCloudRuntimeDriftHandler.Handle` calls it at the SAME point
`evidenceAsOf` used to be captured — before the evidence load, not at
write-commit time. This ordering is load-bearing, not a style choice: a
write-time token would order admission by COMMIT order instead of EVIDENCE
RECENCY, silently reintroducing the ORIGINAL #5848 bug (a stalled worker's
stale evidence landing after a fresher worker's committed write) while fixing
the clock-skew one. `AWSCloudRuntimeDriftFencingTokenIssuer` is a required
field, like `EvidenceLoader`/`Writer`: a nil issuer is a hard `Handle()`
error and the registry (`defaults_additive_domains_secrets_drift.go`) will
not register the domain without one, never a silent fallback to the host
clock.

A useful side effect: since a Postgres sequence never returns the same value
twice, the round-1 exact-watermark-tie scenario is now categorically
impossible — `TestAWSCloudRuntimeDriftInsertAdmissionResolvesExactTieByLastCommitLive`
was deleted (it characterized behavior for an input that can no longer occur)
and replaced by `TestAWSCloudRuntimeDriftFencingTokenIssuerIssuesStrictlyIncreasingValuesLive`,
which proves sequential AND concurrent issuance (10 goroutines × 20 calls
each against the real sequence) never collides and stays strictly ordered by
issuance — the concurrency proof this shared-ordering-primitive change needs,
not just a single-threaded correctness check. The pre-existing
`TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive`
and `TestAWSCloudRuntimeDriftInsertAdmissionAppliesEqualTokenRetryLive` were
updated to drive the admission CAS with explicit `FencingToken` values
directly (the write-level ordering contract they pin is independent of how a
caller obtains the token) rather than relying on `EvidenceAsOf` to derive it.

```
$ ESHU_POSTGRES_DSN=postgres://eshu:change-me@localhost:15948/eshu?sslmode=disable \
    go test ./internal/storage/postgres -run 'AWSCloudRuntimeDrift(InsertAdmission|FencingToken|Retire)' -v
--- PASS: TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive (1.72s)
--- PASS: TestAWSCloudRuntimeDriftInsertAdmissionAppliesEqualTokenRetryLive (1.99s)
--- PASS: TestAWSCloudRuntimeDriftFencingTokenIssuerIssuesStrictlyIncreasingValuesLive (7.30s)
--- PASS: TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive (7.49s)
```

Fast, in-process regression proving `Handle` actually stamps the write with
the ISSUER's value (not a value derivable from `h.Now()`), by wiring a clock
that would produce a LARGER wall-clock-derived token than the issuer's
explicit value if the host clock still drove it:

```
$ go test ./internal/reducer -run TestAWSCloudRuntimeDriftHandlerStampsWriteWithIssuedFencingTokenNotHostClock -v
--- PASS: TestAWSCloudRuntimeDriftHandlerStampsWriteWithIssuedFencingTokenNotHostClock (0.00s)
```

`TestAWSCloudRuntimeDriftHandlerRequiresFencingTokenIssuer` and
`TestAWSCloudRuntimeDriftHandlerPropagatesFencingTokenIssuerError` pin the
required-field and no-silent-fallback contract; both pass.

#### P2s (round-5): stale doc references, and the README split

Five stale `awsCloudRuntimeDriftStatePendingMaxAttempts` / "3 attempts"
references in this doc (the synopsis, the starvation-analysis bullet, an
initial design description, and two more) were corrected to describe the
30-minute elapsed-time bound with the cycle anchor — see the corrections
throughout "The four pieces", "Concurrency analysis", and the readiness-test
coverage description above. The `fencingToken` doc comment in
`aws_cloud_runtime_drift_admission.go` (describing it as a wall-clock
"last-committer-wins" ordering) was rewritten to describe the database-issued
sequence mechanism.

Separately, `origin/main` landed #5865 (split the 4,338-line
`go/internal/reducer/README.md` into topic docs) while this branch was in
flight, conflicting the rebase on that file. This branch's content —
the admission CAS, readiness defer, reopen-slice enrolment, and cycle anchor
— was placed in
[`gotchas-supply-chain-and-vulnerabilities.md`](../../../go/internal/reducer/gotchas-supply-chain-and-vulnerabilities.md),
appended to the SAME `aws_cloud_runtime_drift` bullet the split already
carried there (verified: that file already contained this domain's
"AWS runtime drift publication is graph-neutral for this slice" bullet,
directly following the "cross-source, cross-scope, and truth-emitting" top
invariant this branch's original hunk was appended after in the pre-split
monolith) — not re-inflated back into `README.md`, which the split
deliberately trimmed to a short pointer list.

### Round-6 review (#5848): migration 089's seed query had no supporting index

A hostile review of migration `089_aws_cloud_runtime_drift_fencing_token_sequence.sql`
found its seed query —
`SELECT MAX(fencing_token) FROM fact_records WHERE fact_kind = 'reducer_aws_cloud_runtime_drift_finding'`
— resolved through `fact_records_scope_generation_idx`, where `fact_kind` is
the THIRD column: a full index traversal checking every entry, not a seek.
Cost scales with total `fact_records` size, and this query re-runs on every
reducer process start (bootstrap SQL is re-applied idempotently, not run
once). This is the identical anti-pattern `#3389` already paid down elsewhere
in `003_fact_records.sql` with ~15 per-`fact_kind` partial indexes.

Per Prove-The-Theory-First, the theory was measured before building anything:
an isolated scratch `postgres:18-alpine` instance (private port, matching
this repo's `docker-compose.yaml` / `docker-compose.neo4j.yml` pin — no
shared container touched), bootstrapped with this branch's full schema,
seeded with 200,000 synthetic `fact_records` rows across 40 `fact_kind`
values (1,000 matching `reducer_aws_cloud_runtime_drift_finding` — the
reviewer's own representative shape) via `generate_series`, `ANALYZE`d, then
`EXPLAIN (ANALYZE, BUFFERS)` against the actual seed query, both before and
after the candidate index (the index was applied via the real migration,
then dropped to capture the OLD plan on the identical dataset, then
recreated):

```
OLD (fact_records_scope_generation_idx, fact_kind third column):
 Aggregate  (cost=1742.79..1742.80 rows=1 width=8) (actual time=0.205..0.205 rows=1.00 loops=1)
   Buffers: shared hit=51
   ->  Index Scan using fact_records_scope_generation_idx on fact_records  (cost=0.42..1740.19 rows=1040 width=8) (actual time=0.049..0.160 rows=1000.00 loops=1)
         Index Cond: (fact_kind = 'reducer_aws_cloud_runtime_drift_finding'::text)
 Execution Time: 0.242 ms

NEW (fact_records_aws_cloud_runtime_drift_fencing_token_idx, migration 090):
 Result  (cost=0.57..0.58 rows=1 width=8) (actual time=0.027..0.027 rows=1.00 loops=1)
   InitPlan 1
     ->  Limit  (cost=0.28..0.57 rows=1 width=8) (actual time=0.025..0.025 rows=1.00 loops=1)
           ->  Index Only Scan Backward using fact_records_aws_cloud_runtime_drift_fencing_token_idx on fact_records  (cost=0.28..304.88 rows=1040 width=8) (actual time=0.024..0.024 rows=1.00 loops=1)
                 Heap Fetches: 1
 Execution Time: 0.046 ms
```

**Numbers restated as approximate, not exact, and pinned to the dataset that
produced them**: an independent reproduction of an earlier version of this
proof (run against a different scratch Postgres instance/dataset state) got
`1826.92 → 0.32` (~5,700x); this exact run got `1742.79 → 0.57` (~3,000x); an
even earlier run against `postgres:16` got `3788.52 → 0.31` (~12,000x). Same
order of magnitude every time (planner cost drops roughly three to four
orders of magnitude), directionally consistent, but the specific multiplier
does not reproduce bit-for-bit across runs — dataset and cache state, not the
mechanism, explain the spread, so this doc no longer cites one exact figure
as if it were reproducible. Wall-clock execution time at this 200k-row scale
also moves with cache state (0.242ms → 0.046ms this run) and matters far less
than the plan shape change itself: `Aggregate` over an `Index Scan` bounded
only by `fact_kind` (cost scales with that fact_kind's row count, which
scales with total ingested `aws_cloud_runtime_drift` findings over the
table's life) versus a `Result`/`Limit` over an `Index Only Scan Backward`
(cost is O(1), Postgres's standard `MAX(col)` rewrite once a leading-column
index on `fencing_token` exists).

Unlike the `#3389` precedent's `(scope_id, generation_id, fact_id)`
enumeration shape — which still scales with the matching `fact_kind`'s own
row count — a leading-`fencing_token` index makes this specific `MAX()`
query's cost independent of table size or how many
`reducer_aws_cloud_runtime_drift_finding` rows accumulate, so the
recommendation is to deviate from the `#3389` column order for this one
aggregate-shaped query.

**`CONCURRENTLY`, not plain `CREATE INDEX`, and split into its own migration
(090)**: an initial version of this index used plain `CREATE INDEX IF NOT
EXISTS` in migration 089 itself, citing `003_fact_records.sql`'s ~15
`#3389`-era indexes as precedent. A follow-up review correctly rejected that
precedent: `003_fact_records.sql` predates the repo's current convention, and
21 more recent migrations — including 069, 081, 082, 084, 085, and 086,
three of them before this one, all touching `fact_records` or
`graph_node_owner` — use `CREATE INDEX CONCURRENTLY IF NOT EXISTS`. A plain
`CREATE INDEX` takes a `SHARE` lock for the full build, blocking every write
to `fact_records` (the central facts table) for that duration on a large
production table; `CONCURRENTLY` avoids that. The repo has purpose-built
infrastructure for it: `withSchemaBootstrapLock`'s `schemaConnectionExecutor`
runs each migration file's SQL as one statement on a single
non-transactional `*sql.Conn` specifically so `CONCURRENTLY` works, and
`dropInvalidConcurrentIndexes` cleans up a build that failed to complete.

`CONCURRENTLY` cannot run inside a transaction block, and `ApplyBootstrap`
sends each migration FILE's entire SQL as one multi-statement string to a
single `ExecContext` call — Postgres's simple query protocol implicitly
wraps a multi-statement string in a transaction. Migration 089 (this domain's
sequence + seeding `DO` block) therefore cannot share a file with the
`CONCURRENTLY` index statement. The index moved to a NEW migration, 090
(`fact_records_aws_cloud_runtime_drift_fencing_token_idx.sql`), containing
ONLY the `CREATE INDEX CONCURRENTLY IF NOT EXISTS` statement — matching every
one of the six precedent migrations' own single-statement shape exactly.
Because 089 applies before 090 by filename order, the very first bootstrap
apply's seed query runs unindexed once; every later bootstrap-SQL re-apply
(the actual repeated cost this fix targets) runs against the now-existing
index.

Proven correct in context, not just in isolation: `ApplyBootstrap` run TWICE
against a clean `postgres:18-alpine` scratch instance (private port). First
apply: `CREATE INDEX CONCURRENTLY` succeeded (`indisvalid = t`, confirming a
clean concurrent build, not a failed-and-abandoned one). Second apply: a
verified no-op (`CREATE INDEX ... IF NOT EXISTS` skips; exactly one index row
after both applies; `aws_cloud_runtime_drift_fencing_token_seq`'s `last_value`
unchanged). The repo's own pre-existing regression test,
`TestBootstrapDefinitionsDoNotBundleConcurrentIndexStatements`
(`cloud_resource_owner_page_index_schema_test.go`), independently guards the
exact hazard this split avoids — it counts literal `CREATE INDEX CONCURRENTLY`
occurrences per bootstrap definition's SQL and fails past 1; an early draft
of migration 090's own explanatory COMMENT (not the SQL) quoted that phrase a
second time and tripped it, which is itself confirmation the check works.

Live suite re-run against `eshu-pg-5848` after the split: the index applies
there too (`indisvalid = t`), and every `AWSCloudRuntimeDrift`-scoped test
stays green.

Index added as migration 090:

```sql
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_aws_cloud_runtime_drift_fencing_token_idx
    ON fact_records (fencing_token)
    WHERE fact_kind = 'reducer_aws_cloud_runtime_drift_finding';
```

Scratch proof environments torn down after each measurement (`docker rm -f`);
no shared container's `fact_records` table was touched by any synthetic seed
data.

### Round-6 P0: the branch broke the test its own Performance Evidence cites

Adding `AWSCloudRuntimeDriftWrite.FencingToken` as a required, validated
field (the round-5 P1 fencing-token fix, above) left three fixture writes in
`go/internal/replay/costcounting/aws_cloud_runtime_drift_cost_test.go`
(`TestCostBudget_AWSCloudRuntimeDrift` and its two N+1 negative controls)
with no `FencingToken` set — `WriteAWSCloudRuntimeDriftFindings` correctly
rejected all three with "requires a non-zero fencing_token". This mattered
beyond a red test: this doc's `Performance Evidence:` marker (below) cites
this EXACT test family as the measured proof that the aws_cloud_runtime_drift
Postgres cost budget rose from 1 to 3 statements — so the branch's own cited
measurement was not reproducible on its own HEAD, an evidence-integrity
failure, not merely a broken fixture. It also meant
`scripts/generate-code-coverage-report.sh` aborted under `set -euo pipefail`
on the failing `go test ./...`, so the previously-committed file count was
stale (generated before the round-5 fencing-token commit even existed).

Fixed by adding `awsCloudRuntimeDriftFixtureFencingToken` (a named constant,
commented as load-bearing, mirroring `awsCloudRuntimeDriftFixtureEvidenceAsOf`'s
existing pattern) and wiring it into all three fixture writes:

```
$ go test ./internal/replay/costcounting/... -run TestCostBudget_AWSCloudRuntimeDrift -v -count=1
--- PASS: TestCostBudget_AWSCloudRuntimeDrift (0.00s)
    eshu_dp_postgres_query_duration_seconds_observations=3 (budget=3) statements_executed=3 (budget=3)
--- PASS: TestCostBudget_AWSCloudRuntimeDrift_N1_ExceedsBudget (0.00s)
    N+1 negative control passed: ... observations = 6 > budget 3
--- PASS: TestCostBudget_AWSCloudRuntimeDrift_WithinCallN1_ExceedsBudget (0.00s)
    within-call N+1 negative control passed: ... observations = 6 > budget 3
```

The cited numbers themselves did not move: `statements_executed=3 (budget=3)`
matches the `Performance Evidence:` marker's "1 to 3 statements" exactly —
the fencing-token fix only gates whether the write is ACCEPTED
(a validation guard), not how many statements an accepted write executes, so
the marker below needed no correction.

`scripts/generate-code-coverage-report.sh` re-run after the fix completes
(previously aborted): file count moved from the stale committed 4741 (a
delta of `+6` against `origin/main`'s 4735, generated before the round-5
fencing-token commit existed) to 4742 (`+7`), matching an independent
reviewer's own count exactly. `docs/public/reference/code-coverage-shield.json`
did not change (one file out of 4742 does not move the aggregate percentage).

### Round-6 P2: stale doc comment sweep

`go/internal/storage/postgres/reducer_queue_aws_cloud_runtime_drift_readiness_test.go`
still described the readiness defer as "capped at 3 attempts" — this
branch's own superseded design, replaced by the elapsed-time bound
(`awsCloudRuntimeDriftStatePendingMaxWait`, 30 minutes) precisely because
registering the failure class as non-counting froze `attempt_count` and made
the retry-count bound unreachable (the round-2 P0 above). Fixed. A full-repo
sweep (`rg -n "3 attempts|StatePendingMaxAttempts"`) found no other
current-state stale claim: the remaining hits in this doc's own P0/P1
sections are accurate, deliberately past-tense narration of the superseded
design (unchanged from the round-5 sweep), and
`go/internal/status/README.md`'s "3 attempts" is the unrelated, generic
`DefaultRetryPolicies()` queue-wide default, not this domain's bound.

## Adjacent defect found and fixed: `InstrumentedDB.Begin` silently dropped observability

`InstrumentedDB.Begin` returned the inner `Beginner`'s transaction UNWRAPPED, so
every `ExecContext`/`QueryContext` call issued through it bypassed
`eshu_dp_postgres_query_duration_seconds` entirely, silently. This was latent
because no production writer routed real per-write statements through a
transaction over an `InstrumentedDB`-wrapped connection before this branch (only
`PostgresServiceMaterializationWriter` uses a transaction today, and this fixes
that path too). `aws_cloud_runtime_drift`'s cost-budget gate
(`go/internal/replay/costcounting/aws_cloud_runtime_drift_cost_test.go`) caught
it before it shipped: the positive scenario read 0 histogram observations
instead of the expected 3.

Fixed in `go/internal/storage/postgres/instrumented_transaction.go`: `Begin` now
wraps the returned transaction so its `ExecContext`/`QueryContext` calls record
the same histogram non-transactional calls do. Proven with a temporary revert:

```
$ go test ./internal/storage/postgres -run TestInstrumentedDBBeginWrapsTransactionWithMetrics -v
--- FAIL: TestInstrumentedDBBeginWrapsTransactionWithMetrics (0.00s)
    eshu_dp_postgres_query_duration_seconds observations = 0, want 2
# fix restored
--- PASS: TestInstrumentedDBBeginWrapsTransactionWithMetrics (0.00s)
```

## Proof

All Postgres proofs ran against an isolated `postgres:16` container
(`eshu-pg-5848`, host port 15948) started specifically for this branch, not the
shared default Compose ports.

### Failing-then-green: the #5848 interleaving (mandatory regression)

`TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_admission_live_test.go`)
reproduces the exact interleaving #5848 describes, sequentially (no timing
dependency: the hazard only needs the stale pass's evidence read to predate the
fresh pass's commit for the SAME scope/generation, not true concurrent
overlap):

1. Seed one scope and one active generation.
2. Worker B (fresh): `EvidenceAsOf = now`, one candidate for ARN X classified
   `image_version_drift`. Run to completion (admission + insert; retire is a
   no-op on a first pass).
3. Worker A (stale): `EvidenceAsOf = now - 5m`, one candidate for the SAME ARN X
   classified `orphaned_cloud_resource`. Different classification means a
   different `fact_id`.

Temporarily forcing `admitted = true` unconditionally (bypassing the admission
check) reproduces the pre-#5848 defect:

```
$ go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive -v
--- FAIL: TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive (0.33s)
    worker A (stale) write error = nil, want superseded rejection
# fix restored
--- PASS: TestAWSCloudRuntimeDriftInsertAdmissionRejectsStaleWorkerAfterFreshWriteLive (0.92s)
```

After the fix, exactly one row survives — `SELECT payload->>'finding_kind' FROM
fact_records WHERE scope_id = $1 AND generation_id = $2 AND fact_kind =
'reducer_aws_cloud_runtime_drift_finding'` returns `["image_version_drift"]` —
and `AWSCloudRuntimeDriftFindingStore.ListActiveFindings` agrees: one finding,
`FindingKind = "image_version_drift"`, `ARN` matching worker B's.

`TestAWSCloudRuntimeDriftInsertAdmissionAppliesEqualTokenRetryLive` pins the
`<=` boundary: a retry carrying the SAME watermark must be admitted, not
rejected — a `<` guard would silently discard every retry of a pass whose
evidence is read exactly once.

### Failing-then-green: the retire (the more common real-world shape)

`TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive` covers
the shape a bootstrap/ingester reopen actually produces: an OLDER pass runs
FIRST (unconditionally admitted — nothing existed yet to compare against), then
a fresher reopen-replay reclassifies the same ARN. Neutralizing
`retireAWSCloudRuntimeDriftFindings` to a no-op reproduces the pre-fix defect:

```
$ go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive -v
--- FAIL: TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive (1.65s)
    reopen replay Retired = 0, want 1 (the stale orphaned_cloud_resource row)
# fix restored
--- PASS: TestAWSCloudRuntimeDriftRetireRemovesStaleFindingOnReclassificationLive (0.24s)
```

After the fix: `WriteAWSCloudRuntimeDriftWriteResult.Retired == 1`, exactly one
row survives (`image_version_drift`), and the stale pass's own `fact_id` (an
independent computation via `facts.StableID`, matching the production writer's
own identity derivation) no longer exists in `fact_records`.

### Deterministic reproduction of #5837's actual root cause

`TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_readiness_live_test.go`)
forces the evidence-state ordering directly rather than racing real collectors
for the 1-in-3 shape the issue observed: a `state_snapshot:*` scope registered
with a `'pending'` generation reports `HasPendingStateSnapshotEvidence() ==
true`; once that generation activates (`status = 'active'`,
`ingestion_scopes.active_generation_id` set), the same check reports `false`.
Breaking the query's status predicate reproduces the defect:

```
$ go test ./internal/storage/postgres -run TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive -v
--- FAIL: TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive/registered_but_pending_state_snapshot_generation_is_pending (0.02s)
    HasPendingStateSnapshotEvidence() = false, want true
# fix restored
--- PASS: TestPostgresAWSCloudRuntimeDriftReadinessCheckerLive (0.22s)
```

`go/internal/reducer/awscloud/aws_cloud_runtime_drift_readiness_test.go` covers the
Handler-level bound logic without a database: a pending state scope defers an
orphaned classification (writer never called), the bound (30 minutes of
elapsed wall-clock time since `Intent.CycleStartedAt`, not a retry-count
bound — round-2/round-3 review) commits the best-available verdict regardless
of pending state, a non-pending state never defers, and a nil
`ReadinessChecker` never defers (opt-in, matches pre-#5848 behavior
byte-for-byte). Round-5 P1 addendum: the readiness checker is now called
unconditionally BEFORE the evidence load (see that section below), so a
non-orphaned candidate no longer skips the readiness-checker CALL the way it
used to — it still cannot be DEFERRED by a pending state scope, since
`shouldDeferForLoadedEvidence` only defers when the freshly loaded evidence
actually contains an orphaned candidate.

### Deterministic end-to-end reproduction (real Handler, EvidenceLoader, Writer)

`TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive`
(`go/internal/storage/postgres/aws_cloud_runtime_drift_readiness_deterministic_live_test.go`)
goes one level further than the readiness-checker-only proof above: it drives
the REAL `AWSCloudRuntimeDriftHandler`, the real
`PostgresAWSCloudRuntimeDriftEvidenceLoader`, the real
`PostgresAWSCloudRuntimeDriftReadinessChecker`, and the real
`PostgresAWSCloudRuntimeDriftWriter` together, end to end, with no stubs. This
is the closest a Docker-free lane can get to the owner's own technique of
forcing the collector order (awscloud drains before terraformstate) to turn
the 1-in-3 race into a 100% reproduction: instead of forcing two collectors to
drain in a specific order, it forces the EVIDENCE STATE they would leave
behind directly (a `state_snapshot:*` scope registered with a `'pending'`
generation) — deterministic by construction, not timing, so it reproduces on
every run rather than roughly one in three.

Three phases, one continuous run, verified deterministic over 5 repeated
`-count=5` runs:

```
$ go test ./internal/storage/postgres -run TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive -v -count=1
    BEFORE (no ReadinessChecker): durably wrote [orphaned_cloud_resource] while state scope was pending -- the #5837 bug
    AFTER (ReadinessChecker wired, attempt 0): deferred, zero rows written -- the #5837 fix
    AFTER (attempt 1, state active): reclassified to [unknown_cloud_resource] once state activated
--- PASS: TestAWSCloudRuntimeDriftReadinessDeterministicReproductionLive (0.66s)
```

Phase 1 (`AWSCloudRuntimeDriftHandler` with `ReadinessChecker` left nil —
byte-identical to the code that shipped before this branch) durably writes
`orphaned_cloud_resource` while the state scope is pending: the bug,
reproduced on demand rather than waited for. Phase 2 (the same evidence shape,
`ReadinessChecker` wired) defers instead — zero rows, a retryable error, not a
silently degraded success. Phase 3 activates the state scope with matching
Terraform state for the same ARN and replays `Handle` (simulating the
reopen-triggered retry, `AttemptCount: 1`): the verdict is
`unknown_cloud_resource`, not `orphaned_cloud_resource` — reclassified once
state became visible. (`unknown_cloud_resource`, not `image_version_drift`,
because this fixture's `EvidenceLoader` has no `ConfigResolver` wired —
`aws_cloud_runtime_drift_evidence.go`'s `loadConfigByStateScope` forces
`unknown_cloud_resource` whenever `ConfigResolver` is nil. The specific
non-orphaned kind is incidental to this proof; what matters is that it is no
longer the degraded verdict.)

### Unrelated `internal/storage/postgres` failures, confirmed pre-existing via baseline

A full `go test ./internal/storage/postgres/... -count=1` run on this branch
hits four failures outside anything this branch touches:
`TestIngestionStoreCommitScopeGenerationFencesDerivedRelationshipEvidence` (a
`pg_trgm` operator-class visibility race across parallel tests' custom
Postgres schemas — self-heals once the extension exists database-wide),
`TestIngestionCommitScopeGenerationHoldsBarrierOnlyForAtomicCommit` (a
400,000-fact synchronous-backfill timing assertion),
`TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks`, and
`TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath`. None of
the four files these tests live in were touched by this branch.

Ruled out as a regression by baseline comparison, not by file-adjacency
reasoning alone: an `origin/main` worktree (`ba2b7b80b`, no `#5848` code at
all) was checked out fresh, given its own isolated `postgres:16` container on
a private port, and driven through the SAME three timing-sensitive tests
(`-run "TestIngestionCommitScopeGenerationHoldsBarrierOnlyForAtomicCommit|
TestIngestionCommitAndMaintenanceLockOrderingNeverDeadlocks|
TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath" -p 1`).
All three failed identically on that clean baseline, with the SAME error
signatures (`relationship_evidence_facts row count = 0`; `relation
"ingestion_scopes" does not exist` cascading from the heavy test's timeout
into the next). The baseline run's own timings (`CommitScopeGeneration
onboarding ... took 8.09s` on the first baseline attempt, `82.73s` on a repeat
against the same idle-looking machine) show wide variance consistent with
contention, not a stable per-test cost.

Machine-state honesty: this repo's other concurrent work was not quiesced for
either run. `ps aux` during the branch run showed a second, unrelated agent
session (`5594-local-backend-default-path`) actively running its own
`go test ./internal/storage/postgres/...` against a different private-port
Postgres at the same time, and the coordinator separately reported starting a
`make pre-pr` run (which holds the cross-worktree live-gate mutex and adds its
own Docker/CPU load) around the same window as the baseline comparison. Both
runs above are therefore "branch vs. baseline under real, uncontrolled,
comparable background load on the same shared host" — not "quiet machine vs.
loaded machine". Given identical failure signatures on a codebase with ZERO
`#5848` changes, under directly comparable (if not literally simultaneous)
contention, these four are classified as pre-existing environment/timing
sensitivities in `internal/storage/postgres`'s heavier integration tests, not
regressions introduced by this branch. `TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath`
specifically: a sibling executor is independently changing that exact query on
`5594-local-backend-default-path` (adding a `LEFT JOIN` and a column); that
work is not in this branch's base and cannot be causing this failure, but it
does mean the test is in active flux — its baseline failure here is against
UNMODIFIED `origin/main`, not against that sibling's in-flight work.

Both isolated containers used for this comparison (host ports 15949 and
15950) were created and torn down specifically for this check; neither reused
nor collided with `eshu-pg-5848` (the container this branch's other live
proofs run against) or the shared default Compose port set.

### Credential-free

```
$ go test ./internal/reducer/... ./internal/replay/costcounting/... ./cmd/reducer/... ./cmd/bootstrap-index/... -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer
ok  	github.com/eshu-hq/eshu/go/internal/reducer/dsl
ok  	github.com/eshu-hq/eshu/go/internal/reducer/tags
ok  	github.com/eshu-hq/eshu/go/internal/reducer/tfstate
ok  	github.com/eshu-hq/eshu/go/internal/replay/costcounting
ok  	github.com/eshu-hq/eshu/go/cmd/reducer
ok  	github.com/eshu-hq/eshu/go/cmd/bootstrap-index
$ go build ./... && go vet ./...
(clean)
```

## The #5831 / #5837 golden-gate flake: causal verdict

**#5848 was NOT the cause of the `list_aws_runtime_drift_findings` empty
`drifted_attributes` flake.** The owner's own root-cause comment on #5837
(refuting their own and #5831's earlier hypotheses) is correct and independently
verified against the code in this branch's base:

- `drifted_attributes` is derived in-process from the SAME finding row's own
  evidence array (`iac_management_transform.go` ->
  `driftedAttributesFromAWSEvidence`) — finding and attributes are physically
  inseparable, so "attributes projected after the finding" cannot happen.
- The golden gate evaluates `required_json_values` in SORTED path order and
  returns on first failure (`go/internal/goldengate/query_shape_paths.go`).
  Sorted, `drift_findings[].drifted_attributes[].attribute` precedes
  `drift_findings[].finding_kind`, so the empty-attributes failure is the
  symptom of a WRONG finding_kind, not a timing gap.
- The actual mechanism is `cloudruntime.Classify` reading `state == nil` as a
  VERDICT (`orphaned_cloud_resource`, no drifted attributes) when the state
  scope has simply not activated yet — confirmed directly against
  `go/internal/storage/postgres/aws_cloud_runtime_drift_evidence_sql.go`'s
  `listActiveStateResourcesForAWSARNsQuery`, whose join requires
  `ingestion_scopes.active_generation_id = fact.generation_id`: a
  `state_snapshot` scope with a pending generation joins ZERO rows regardless of
  whether matching state data exists.

Why #5848's insert-admission mechanism does not explain the flake: at the point
#5837/#5831 were filed, `aws_cloud_runtime_drift` had NO retire and NO insert
race at all (verified above — no `fencing_token`, no retire query, no admission
table existed on `origin/main`). The flake's shape is ONE finding written
(`drift_findings has 1 results`) with the WRONG `finding_kind`, not two
contradictory findings for one ARN. #5848's insert-admission and retire
mechanisms answer "what happens when a SECOND, differently-classified pass
writes for the same ARN" — a question that only arises once something (the
reopen this same branch adds) causes a second pass to run at all. Before this
branch, the domain ran exactly once per (scope, generation), so there was never
a second write to race against; the flake is a ONE-write, wrong-verdict defect,
which is what the readiness defer (piece 1 above) fixes at the source.

The readiness defer does directly address #5837/#5831's mechanism: it stops
`Handle` from committing `orphaned_cloud_resource` as a durable verdict while
the state scope is still pending, which is the exact condition the owner's
root-cause comment identifies. The golden assertion itself
(`drift_findings[].drifted_attributes[].attribute`,
`drift_findings[].finding_kind`) is unchanged and unweakened by this branch —
closing the race at the source is what should let it stop being vacuously
satisfied by a degraded finding.

**What this branch does not prove:** full end-to-end confirmation on the real
B-7 20-repo/17-collector corpus requires
`scripts/verify-golden-corpus-gate.sh`, a Docker-based live gate this executor
lane does not run (orchestrator-serialized, per
`docs/internal/agent-guide.md#live-gate-serialization-and-contention`). The
deterministic Postgres-level reproduction above proves the READINESS DEFER
MECHANISM works correctly in isolation; it is not a substitute for the
orchestrator running the golden-corpus gate to confirm the flake stops
reproducing on the real corpus.

Performance Evidence: the aws_cloud_runtime_drift Postgres cost budget rose from
1 to 3 statements per `WriteAWSCloudRuntimeDriftFindings` call (measured, not
asserted — see `testdata/cassettes/replayoffline/aws-cloud-runtime-drift.cost-budget.json`
and the `TestCostBudget_AWSCloudRuntimeDrift` family in
`go/internal/replay/costcounting`): the insert-admission check and the
generation-authoritative retire are each one additional round-trip, required
for correctness (closing #5848), not an unmeasured regression. No other
hot-path Cypher, graph write, or query handler changed. The readiness defer adds
one `EXISTS` query per `Handle` call only when `ReadinessChecker` is wired
(opt-in; nil leaves the domain's cost unchanged).

Observability Evidence: `awsCloudRuntimeDriftWriteSupersededError` and
`awsCloudRuntimeDriftStatePendingError` each self-report a distinct
`FailureClass()` (`aws_cloud_runtime_drift_write_superseded`,
`aws_cloud_runtime_drift_state_pending`), which the existing
`ReducerQueue.failIntent` path already records on the
`ReducerRetrySurge` counter labeled by `failure_class`
(`go/internal/storage/postgres/reducer_queue_helpers.go`) and on the durable
`fact_work_items.failure_class`/`failure_message` columns — an operator can
distinguish a superseded-pass rejection, a readiness defer, and a genuine
handler error from each other and from ordinary retries, without a new metric
instrument. `AWSCloudRuntimeDriftHandler` additionally emits its own structured
`slog` line for each of the two cases (`logWriteSuperseded`,
`logStatePendingDefer`), distinct from the existing admitted-findings log line.
The `InstrumentedDB.Begin` fix restores `eshu_dp_postgres_query_duration_seconds`
coverage for every statement this writer's transaction issues, closing a gap
this branch's own architecture change would otherwise have introduced silently.

## Follow-on work carried on top of the landed design

The sections below were developed alongside the reopen and admission work but do
not depend on it. They survive the mechanism this branch shipped unchanged: the
value axis is a `Classify` precondition the retire relies on regardless of how
admission is ordered, and the two gate sections are accounting and coverage
findings with no drift-domain coupling at all. The rest of that line of work --
a per-ARN evidence watermark ranking a fenced delete-then-insert -- was
superseded by the transactional admission design documented above, which orders
passes by a database-issued sequence rather than by comparing two collectors'
clocks, and is deliberately not carried here.

### The retire's second precondition: the VALUE axis (#5837)

The retire above is only correct while every path that drops an ARN from the
keep-set is a real verdict, because a retire deletes on the strength of
ABSENCE. `Classify` satisfies that on the EXISTENCE axis by construction — a
missing state row writes `orphaned_cloud_resource` and a missing config row
writes `unmanaged_cloud_resource`, so degraded evidence still produces a row.

It did NOT satisfy it on the VALUE axis. With cloud, state, and config all
present and no comparable attribute left to compare, `classify.go` returned
`""`, `candidate.go` did `kind == "" -> continue`, the keep-set emptied, and the
retire deleted a still-true `image_version_drift` finding.

Probed through the real handler on one `(scope, generation)`:

```
PASS 1: state carries attributes.ami   candidates=1  keep_set=[reducer_aws_cloud_runtime_drift_finding:f4e35f...]
PASS 2: SAME state row, ami absent     candidates=0  keep_set=[]
```

Reachable with nothing upstream asserting a failure. The terraform-state
collector fail-closed-redacts scalars when its provider-schema resolver is nil
or a bundle fails to parse, silently:
`LoadPackagedSchemaResolver` returns `nil, nil`, `parseSchemaInto` skips a
corrupt bundle (`terraformstate/schema_resolver.go`), and
`go/cmd/collector-terraform-state/config.go` accepts a nil resolver as
non-fatal. For ECS the equivalent is `container_image_extract.go`'s
`containerDefinitions.(string)` assertion, which a redaction-marker MAP fails,
yielding an empty image set with a nil error and therefore an ambiguous
comparison.

Those conditions are STICKY — a corrupt bundle persists for the life of a
deployment — so a replay repeats the deletion rather than healing it. That is
what makes it a data-loss hazard rather than a transient one.

## The B-7 gate cannot reach the ECS value-comparison branch at all

Recorded because a green gate on this branch must not be read as coverage of
the classifier change, and because that gap is exactly what let a defect
through three green gate runs before review caught it.

`ClassifyValueComparison`'s ECS branch is guarded by
`state.ResourceType == "aws_ecs_task_definition"`, and value drift only runs
once cloud, state, AND config rows all exist for the ARN. The golden corpus has
no declared-side ECS task definition anywhere:

```
$ rg -l "aws_ecs_task_definition" testdata/ tests/fixtures/
$ echo $?
1
```

The cloud side alone is present -- `testdata/cassettes/awscloud/supply-chain-demo.json`
carries one `ecs.task_definition` `aws_resource` fact -- but with no
Terraform-state row for that ARN the join never produces a state side, so the
branch is unreachable. Both ECS cloud rows in the cassette also carry exactly
one container, so even the reachable half never exercises a multi-image
observed set.

The consequence is concrete. The `value_comparison_inconclusive` outcome
originally fired for EVERY ambiguous container-image shape, including a
converged multi-container task definition whose declared and observed image
sets are identical -- an un-actionable finding on healthy input. Three gate
runs passed green over that defect, and could not have done otherwise: no
corpus input reaches the code. It was found by running the production
classifier directly on a converged multi-container pair, and is now pinned by
`TestClassifyHealthyMultiContainerTaskDefinitionStaysSilent` and
`TestClassifyDegradedContainerImagesIsStillInconclusive` in
`go/internal/correlation/drift/cloudruntime/value_comparison_test.go`.

Closing the gate-level gap needs a declared/observed ECS pair added to the
corpus -- a terraform-state `aws_ecs_task_definition` row plus its config row,
plus snapshot recalibration for the nodes and edges they add. That is corpus
coverage work rather than part of this fix, and it is deliberately NOT done
here: adding it late to an otherwise-green branch trades a real regression fix
for an unbounded recalibration loop. Until it lands, the unit tests above are
the only coverage of the ECS value-comparison branch, and a green B-7 asserts
nothing about it.

## Golden-gate phase-timing note: the suppression proof's 20s floor vs the graph_query phase's 8s ceiling

Unrelated to the retire fence above, but discovered on this branch and worth a
committed home: `scripts/verify-golden-corpus-gate.sh`,
`scripts/lib/golden-corpus-phase-timings.sh`, and
`scripts/lib/golden-corpus-phase-timing-cases.sh`, and the `graph_query` note in
`testdata/golden/e2e-baseline.json` all reference a "4s -> 23s" reading of the
gate's `graph_query` phase before it was fixed. Exactly one of the four states
the figure (`golden-corpus-phase-timing-cases.sh`); the other three only
disclaim it, and all four now carry an explicit not-captured qualifier. None
cites a captured `phase-timings.json`, a host, or a run manifest —
this repository has no such artifact checked in (`rg --files -g
'phase-timings*.json'` over the whole tree returns nothing, and `git log --all
-- '**/phase-timings.json'` returns no history). Treat the "4s -> 23s" figure
in those four files as **illustrative**, not as measured evidence cited from
an artifact. What follows instead is the structural defect, provable from
source alone with no timing capture required.

### The defect, independent of any measurement

The gate's `graph_query` phase has an effective pass ceiling of 8 seconds when
run blocking (`GATE_PHASE_REGRESSION_ADVISORY=false`), derived from the
committed baseline, not from a live run:

- `testdata/golden/e2e-baseline.json:34` — `graph_query.baseline_seconds: 3`.
- `testdata/golden/e2e-baseline.json:11` — `absolute_slack_seconds: 5` (repo-wide).
- `testdata/golden/e2e-baseline.json:40` — the regression rule: `observed_seconds
  <= baseline_seconds * (1 + regression_band) OR observed_seconds <=
  baseline_seconds + absolute_slack_seconds`.
- For `graph_query`, the absolute-slack branch governs (`3 + 5 = 8` seconds
  exceeds `3 * 1.15 = 3.45` seconds), so **8 seconds is the effective ceiling**
  for a blocking run, fixed by these three committed numbers alone.

Meanwhile `#5465`'s suppression-producer proof floors itself at a **fixed 20
seconds**, also fixed by source, also independent of any live run:

- `scripts/lib/golden-corpus-vulnerability-suppression.sh:34` —
  `golden_suppression_prepare_payloads` sets
  `golden_suppression_expiry_epoch=$(( $(date -u '+%s') + 20 ))`.
- `scripts/lib/golden-corpus-vulnerability-suppression.sh:338-339` —
  `golden_suppression_wait_for_expiry` spins `while (( $(date -u '+%s') <
  golden_suppression_expiry_epoch )); do ...`, so it cannot return before that
  20-second deadline on any host, however fast.
- `scripts/lib/golden-corpus-vulnerability-suppression.sh:380-410` —
  `golden_suppression_verify_producer_truth` calls
  `golden_suppression_prepare_payloads` at `:384` and, later in the same
  function, `golden_suppression_wait_for_expiry` at `:410`, so every
  invocation of the producer-truth proof unconditionally blocks for at least
  20 seconds.
- `scripts/verify-golden-corpus-gate.sh:355` opens the `graph_query` phase's
  timing window (`phase_graph_query_start="${pipeline_end}"`), and
  `scripts/verify-golden-corpus-gate.sh:385` invokes
  `golden_suppression_verify_producer_truth` — the 20-second-floored proof —
  inside that window, now bracketed at `:384`/`:386`
  (`phase_graph_query_excluded_starts+=`/`_ends+=`, appended rather than
  assigned so multiple brackets accumulate instead of the last one silently
  discarding the rest -- #5837 P2-8/156c8d2d1) so the wait is subtracted
  before the phase duration is computed
  (`scripts/lib/golden-corpus-phase-timings.sh:103`).

`20 > 8` by construction: the fixed wait (source items above) exceeds the
effective ceiling (baseline-file items above) on every host, for every corpus,
on every run, with no relationship to actual pipeline speed. Before the
bracket existed, any invocation of the suppression-producer proof inside the
unbracketed `graph_query` window made a blocking run of the phase-regression
check fail deterministically. The "4s -> 23s" framing is
consistent with this structural bound (23 falls inside a 20s-wait-plus-phase-
work range, and 23 > 8), but the structural bound above holds regardless of
whether that specific pair of numbers was ever captured to an artifact.

### What this section does not claim

This section does not assert that "4s -> 23s" is a verified measurement — no
`phase-timings.json`, host record, or run manifest for that specific pair was
found on this branch or in git history. It asserts only the source-provable
defect (20s fixed floor inside an 8s-ceiling phase) and cites the bracket
(`scripts/verify-golden-corpus-gate.sh:384-386`,
`scripts/lib/golden-corpus-phase-timings.sh:73-103`) that now excludes the
floor from the phase's billed duration. Anyone who wants the "4s -> 23s" claim
backed by a captured artifact should re-run
`scripts/test-verify-golden-corpus-gate.sh` (or the full gate) against this
branch and this branch's predecessor with `-phase-timings-file` capture
enabled, and commit the resulting `phase-timings.json` alongside the host and
commit SHA it was captured on.

### Proof status on this HEAD

This is a gate-harness accounting fix, not a runtime code path, so "proof"
here means the gate's own bookkeeping is correct, not a measured speedup.
Three tiers of proof exist on this commit, and a fourth is explicitly
outstanding:

- **Source-provable arithmetic**: `20 > 8` (the suppression proof's fixed
  20-second floor against the phase's 8-second effective ceiling) follows
  from the committed baseline numbers and the fixed expiry wait cited above,
  with no measurement required.
- **Static mirror**: `bash scripts/test-verify-golden-corpus-gate.sh` passes
  on this HEAD (Docker/Postgres-free structural test; it does not execute
  `verify-golden-corpus-gate.sh`, only asserts its source shape).
- **Mutation kills**: the phase-timing-cases mirror
  (`scripts/lib/golden-corpus-phase-timing-cases.sh`) fails loudly for
  unequal-length exclusion arrays, a negative bracket span, disallowed
  content between the exclusion stamps, and (added in the same P2 pass that
  produced this note) a bracket whose two stamp lines are relocated together
  to sit wholly above the `phase_graph_query_start=` assignment -- a shape
  the content allowlist alone cannot see, since it checks what rides between
  the stamps, not where the stamps themselves sit in the file. Each case was
  confirmed by applying the mutation, watching the assertion fail, and
  reverting.
- **Live gate run: PENDING.** `scripts/verify-golden-corpus-gate.sh` itself
  (the Docker-based end-to-end run that actually exercises the bracketed
  suppression proof against a live Postgres/NornicDB stack) has NOT been run
  on this HEAD as part of this fix. `verify-golden-corpus-gate.sh` binds
  fixed host ports and holds a cross-worktree mutex, so it is sequenced by
  the orchestrating session rather than run ad hoc from this worktree. Until
  that run lands, this section's claim is bounded to the three tiers above,
  not to an observed passing live run.

## No-regression measurement for the sixth reopen domain

`crossScopeCorrelationReopenDomains` gained `aws_cloud_runtime_drift` as a sixth
entry, but `TestCorrelationReopenPerDrainCostProof` and the README section that
cites it as the authority both still described a five-domain pass, and the
per-drain cost proof carried no evidence marker for the added domain. The proof
drives its round trips off `CrossScopeCorrelationReopenDomains()` directly, so
the row counts were already a mechanical recount; the wall clock was not, and
scaling it on paper is the kind of unmeasured claim the evidence rules exclude.

Six-domain provenance: measured on the pre-reconcile branch tip
(`5837-drift-reopen-prereconcile` = `12bd12dc8`), same test, same corpus
shape, same loopback topology, Postgres 16.14 -- carried forward unchanged
into this reconciled branch rather than re-measured on this HEAD. The
figures, the sublinearity argument, and this same provenance caveat live with
the proof they belong to, in `go/internal/storage/postgres/README.md`'s
cross-scope correlation reopen section under its `No-Regression Evidence:`
marker. Summary: bounded pass 5.5 s / 4500 rows -> 5.927 s / 5400 rows,
listings only 74 ms -> 91 ms, unbounded pass 2 m 17 s / 112 500 ->
2 m 31.7 s / 135 000. About +0.4 s per drain against +20% reopened rows, so
the pass got proportionally bigger rather than slower per unit of work.
