# #11366 older baseline windows in the batch claim gate set: performance and observability evidence

The NVD baseline gate boost (`gate_waited_generations`) keyed on the checkpoint's
last committed generation only, so an older window whose reducer never ran fell
back to FIFO behind the whole backlog (QA: four 2026 windows pending for days,
oldest since Nov 19). The fix adds a second UNION arm covering every live
generation of an incomplete year's baseline scope family, keeping the
one-boosted-slot bound, the claim-time fences, and the auto-expiry. See
eshu-hq/eshu#11366.

Performance Evidence: claim-path benchmark before and after, PostgreSQL 18.

Backend: PostgreSQL 18 disposable database on the local host (`initdb`,
port 55434), shared laptop host so absolute numbers are noisy; compare the
pairs, not the absolute values.

Command, run once on the base claim statements (throwaway worktree at
`origin/main`, same benchmark code) and once on the branch:

```
ESHU_REDUCER_CLAIM_BENCH_DEPTHS=1000,10000 go test -p 1 -run '^$' \
  -bench BenchmarkReducerQueueClaimDeepQueue -benchtime 200x -count 3 \
  ./internal/storage/postgres/
```

| Depth | Before (ns/op, 3 runs) | After (ns/op, 3 runs) |
| --- | --- | --- |
| 1,000 | 3,382,877 / 3,309,241 / 3,374,452 | 3,383,308 / 3,304,227 / 3,370,435 |
| 10,000 | 31,333,412 / 32,582,448 / 30,965,411 | 31,528,517 / 31,344,890 / 30,917,973 |

Allocations are identical at 97 allocs/op and ~4,830 B/op on every run. The
added arm is a grouped checkpoint/live-row probe evaluated once per claim
(MATERIALIZED); the checkpoints table holds one row per year and the work-item
side filters on stage plus a four-state status match, so it adds no measurable
cost at either depth.

Why the change is safe: the per-candidate rejoin guard
(`TestClaimBatchCandidateReadsRepsWithoutRejoin`, extended with an explicit
no-per-candidate-rejoin assertion over the gate span) stays green, the
pre/post set-and-order differential
(`TestClaimBatchCandidateRejoinRemovalMatchesPreChangeSetAndOrder`) proves the
candidate set is unchanged, and the lock arms keep their two
`FOR UPDATE ... SKIP LOCKED` probes with row-self rechecks.

No-Regression Evidence: bounded boost and unchanged fences, live PostgreSQL.

- `TestClaimBatchGatePriorityBoostsOlderBaselineWindowLive` fails without the
  arm (batch takes ordinary backlog plus the newest window) and passes with it;
  the newer window keeps its ordinary place, pinning the one-boosted-slot bound.
- The de-boost contract for non-baseline scopes is untouched
  (`TestClaimBatchGatePriorityIgnoresSupersededGenerationLive` green: the new
  arm only matches the `vuln-intel://nvd/_baseline/<run>/<year>/` family).
- The #11234 change-detector digest for the batch statement was re-pinned with
  the #11366 justification; its seeded lock-text edits are still caught.
- Full `Batch` suite (56 tests: gate priority, funnel, precompute plan guards,
  rank-once differential, expiry holds) green on the rebased tree.

No-Observability-Change: the fix adds no new metrics, spans, or log lines. The
boosted slot is visible through the existing claim funnel census and
`BatchClaimSize` histogram; the production proof is the QA drain of the four
orphaned 2026 windows, watched on eshu-hq/eshu#10728.
