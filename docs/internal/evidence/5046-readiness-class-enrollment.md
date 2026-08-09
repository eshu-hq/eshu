# #5046 — seventeen readiness classes were eroding the retry budget

## What was wrong

A readiness-gate miss is not a failure on the intent's own merits: it means an
upstream phase has not published its `canonical_nodes_committed` marker yet.
`nonCountingReducerRetryFailureClasses`
(`go/internal/storage/postgres/reducer_queue_readiness_sql.go`) exists so such a
miss does not count toward `ESHU_REDUCER_MAX_ATTEMPTS`, because the
succeeded-only reopen path (`ReopenSucceeded` / `ReplayDomain`) never reopens a
dead letter — once a still-pending intent is dead-lettered, its edges are gone
until an operator notices.

Root-Cause Evidence: the reducer returns **25** distinct `*_not_ready` failure
classes from types that report `Retryable() bool { return true }`. The
registry recognised **8**. The other 17 took the dead-letter branch of
`ReducerQueue.Fail` once past the attempt budget.

Measured directly on clean `origin/main` (`b1b951045`), driving the real
`ReducerQueue.Fail` path at `AttemptCount: 42` with `MaxAttempts: 3`:

```
ON ORIGIN/MAIN aws_relationship_nodes_not_ready               deferred=false
ON ORIGIN/MAIN azure_relationship_nodes_not_ready             deferred=false
ON ORIGIN/MAIN workload_cloud_relationship_nodes_not_ready    deferred=false
ON ORIGIN/MAIN s3_logs_to_nodes_not_ready                     deferred=false
ON ORIGIN/MAIN ec2_uses_profile_nodes_not_ready               deferred=false
```

`deferred=false` means the query issued was the dead-letter UPDATE, not the
`status = 'retrying'` one. After this change all 25 report `deferred=true`,
asserted by `TestReducerQueueFailDefersEveryEnrolledReadinessClassPastAttemptBudget`.

The claim-time readiness CTE (`reducerClaimReadinessRequirementsSQL`) covers the
common case for most of these domains, so the exposure is the claim/handle race
window where the handler's own `ReadinessLookup` disagrees with the claim-time
gate. Narrow, but the outcome there is a permanent dead letter.

## Why enumeration was not the fix

The issue named three classes and asked for an audit of the rest. A hand sweep
of the source found eleven. The `go/ast` guard written alongside it found
**six more on its first run** — the sweep's own output had been truncated, and
nothing would have caught that.

That is the same failure the registry itself had: the list drifted because
adding a class does not add it to the list, and nothing checked.

`TestEveryReadinessFailureClassIsEnrolled`
(`reducer_queue_readiness_enrollment_test.go`) parses every non-test file in
`internal/reducer` with `go/ast`, finds each type that both returns `true` from
`Retryable()` and a `*_not_ready` string from `FailureClass()`, and fails when
that class is not registered. It reads the constant's declaration as well as a
bare literal, so exporting a constant does not hide a class from it. A class
assembled at runtime rather than returned as a literal is simply not scanned —
the guard reports what it can read rather than guessing.

The behavioural test is table-driven over the registry for the same reason: one
hand-written test per class is the pattern that let this drift, since adding a
class does not add its test. `TestReducerQueueFailDeadLettersAnUnenrolledClassPastAttemptBudget`
is the negative control, so the table cannot pass by `Fail` deferring
everything.

## The one domain with only a single layer of defense

Sixteen of the seventeen enrolled domains also have a claim-time row in
`reducerClaimReadinessRequirementsSQL`, so an intent is not claimed until its
upstream phase publishes and the in-handler miss only fires in the claim/handle
race window. `aws_cloud_image_materialization` does not. Its handler's
`sourceNodesReady` is the only defense — the shape #5047 called "the wide-open
case" when GCP relationship materialization was in it.

Enrollment still improves it: the miss now defers instead of dead-lettering.
But it does not add the missing claim gate, and adding one changes claim-time
behaviour for a domain, which needs its own claim-path proof rather than riding
along on a failure-class enrollment.

`TestReadinessDomainsWithoutAClaimGateAreTheKnownSet` records the gap as a
one-entry allowlist and fails if a NEW readiness domain lands without a claim
gate, so the single-layer set cannot grow quietly. It also fails if the listed
domain later gains a gate, so the note cannot outlive the gap.

That guard first reported `aws_relationship_ec2_instance_materialization` as
ungated, which is not a domain at all — `aws_relationship_ec2_instance_nodes_not_ready`
is a sub-readiness inside `aws_relationship_materialization`. The mapping is
now validated against `reducer.ParseDomain` rather than derived from the class
name, so the guard cannot invent a domain and then report it.

## No-Regression Evidence

No-Regression Evidence: the widened set reaches SQL through
`reducerNonCountingFailureClassPredicateSQL`, which grows from 8 to 25 chained
equality terms. That predicate appears in exactly one position in both claim
queries — inside the `SET attempt_count = CASE … END` assignment of
`reducer_queue_claim_query.go:99` and `reducer_queue_batch_query.go:272`. It is
never in a `WHERE`, a join condition, or an `ORDER BY`, so it cannot participate
in row selection, index choice, or the plan; it adds constant-time scalar
comparisons on rows the claim has already selected and locked, bounded by the
claim's own `LIMIT`.

That is a structural argument rather than a timing one, and deliberately so: a
benchmark over 17 extra string comparisons per claimed row would be measuring
noise. The claim that matters — that the predicate cannot move into a
selection position — is checked by reading both call sites, and both are quoted
above.

Focused verification, run after the final edit:

```bash
cd go && go test ./internal/storage/postgres/ ./internal/reducer/ \
  ./internal/projector/ ./cmd/reducer/ -count=1
```

All four packages `ok`.

## Observability Evidence

No-Observability-Change: enrollment routes a failure into the existing retrying
path rather than the existing dead-letter path. Both are already instrumented —
`eshu_dp_reducer_retry_surge_total{failure_class}` counts the retry with the
class as a label, and the durable row carries the same class in
`fact_work_items.failure_class` for the admin dead-letter and status surfaces.
No new stage, worker, queue, or query is introduced. The observable change is
that these 17 classes now appear on the retry counter instead of in the
dead-letter table, which is the intended correction.

## Scope note

The issue's second bullet also asks about `cmd/golden-corpus-gate/drains.go`,
which keeps its own `readinessDeferredFailureClasses` map and is likewise
missing most of these classes. That one is deliberately left alone: its own doc
comment states the trade ("a class missing from this list is reported as live
work, which is a slightly less precise message and never a wrong verdict"), so
its staleness costs a less precise failure message and never a wrong result —
unlike the registry here, where staleness costs the work itself.
