# #10060 — CVE-keyed holder fan-out continuation stranded by a newer generation

## Diagnosis (established before any behavior change)

Root-Cause Evidence: on the clean base (`4e935ef`), the live regression over
the real queue committed a source retraction, recorded the capped
continuation through the production `UpdateFanOutState` seam, activated a
newer source generation, and drained through the production
`ReducerQueue.Claim` + `Runtime.Execute` +
`postgres.NewGenerationFreshnessCheck` path. Observed:
`continuation item status = "superseded"` with
`failure_class="reducer_superseded_by_newer_active_generation"` and the
holder copy still `active` while the source copy converged to
`advisory_retracted` (`TestFanOutRaceStrandedContinuationDrainsAfterNewerGenerationActivatesLive`,
exit 1). Two independent barriers strand the debt, each proven separately:

1. The claim statement's supersede sweep
   (`supersedeInactiveReducerGenerationsCTE`,
   `reducer_generation_filter_sql.go`) terminalizes the pending
   older-generation row inside the first claim after activation — including
   when the debt write commits while the sweep's lock step is already
   waiting on the row (`TestFanOutRaceSweepRechecksDebtCommittedWhileWaitingLive`,
   exit 1 pre-fix: claim returned no work, item `superseded`).
2. Even when claimed first, `Runtime.execute`'s `GenerationCheck` returns a
   superseded result without calling the handler
   (`TestRuntimeExecuteRunsStaleFanOutContinuation`, exit 1 pre-fix: handler
   calls 0 on all three debt arms), and the service acks that result away.

Rejected hypotheses:

- "The successor generation's pass covers the debt, like the #9874 marker
  stamp." Rejected by the same live run: the successor pass executed
  (handled shows its claims) and the holder still read `active`. Its
  changed-CVE diff is taken against the previous generation's stored rows,
  which already carry the committing change, so it fans out to nobody.
- "`cross_scope_replay_required` is the durable debt signal." Rejected: the
  acknowledgement consumes the flag when the item re-pends (payload after ack
  carries `fanout_remaining`, flag false — logged by the regression and
  pinned by `TestUpdateFanOutStateCarriesAContinuationAcrossClaimsLive`).
  The fix therefore keys on the payload (`fanout_remaining` / `fanout_owed`).
- "Manifest markers strand the same way and need the same rescue." Rejected
  by design: a marker stamps completion only after its last chunk, and the
  next marker pass diffs from the newest stamped baseline (`HOLDER_FANOUT.md`,
  manifest section), so a superseded marker is covered. Guard cases pin that
  markers keep superseding.
- "Sparing the sweep alone (or the runtime alone) suffices." Rejected: each
  barrier strands the debt on its own (queue RED and runtime RED are
  independent).

## Fix

- `supersedeInactiveReducerGenerationsPredicate` gains a row-local NOT arm:
  a non-marker `supply_chain_impact` row with `fanout_owed` true or a
  non-empty `fanout_remaining` array is held out of the sweep (single and
  batch claim share the CTE). The CASE keeps `jsonb_array_length` off
  non-arrays and the COALESCE keeps an absent owed key FALSE rather than
  NULL (a debt-free regression case caught the NULL form sparing every
  `supply_chain_impact` row before this shipped).
- `Runtime.execute` lets a stale intent run when
  `supplychaincore.IntentCarriesFanOutDebt` holds (same domain, marker, and
  payload rules in Go and SQL). The writer's admission fence is per
  (scope, generation), so the stale pass rewrites only its own invisible
  generation rows and then drains the stored holder list. No worker, batch,
  lease, or concurrency knob changes: the spared row claims under the
  existing per-scope conflict fence like any other row.

## Replay / retry matrix (all over the real queue unless noted)

| Case | Proof | Result |
| --- | --- | --- |
| Continuation claimed after newer generation activates | `TestFanOutRaceStrandedContinuationDrainsAfterNewerGenerationActivatesLive` | RED (superseded, holder stale) → GREEN (succeeded, holder `advisory_retracted`) |
| Stale replay after partial drain (activation between chunks) | `TestFanOutRacePartiallyDrainedContinuationSurvivesActivationLive` | RED (remainder stranded) → GREEN |
| Debt predicate arms: remaining+owed, remaining only, owed only | `TestReducerClaimDoesNotSupersedeFanOutDebtBehindNewerGeneration` | RED → GREEN (spared and claimed) |
| Marker continuation, debt-free row, debt keys on another domain | same table | still `superseded` before and after (no over-sparing) |
| Dead letter carrying debt | same table | stays `dead_letter` (loud, operator-replayable) instead of `superseded` |
| Retry preserves transient failure across activation | `TestFanOutRaceDebtRetrySurvivesActivationLive` | stays `retrying`, failure class intact |
| Debt committed while the sweep lock waits (EvalPlanQual recheck) | `TestFanOutRaceSweepRechecksDebtCommittedWhileWaitingLive` | RED → GREEN: claim blocks on the held row, then claims it once the debt commits |
| Concurrent workers, same conflict domain, activation mid-drain | `TestFanOutRaceStrandedContinuationConcurrentClaimersConvergeLive` (3 rounds × 4 workers, also under `-race`) | converges every round, 0 deadlocks, 0 data races |
| Duplicate delivery of already-succeeded stale work | same test, post-quiescence arm | debt-free re-pend is terminalized unclaimed (`superseded`), never re-executed |
| Empty / drained queue | same test | final claim returns no work, no error |
| Not-yet-active generation with debt still defers | `TestRuntimeExecuteRunsStaleFanOutContinuation` | `ErrGenerationNotYetActive`, handler untouched |

## Commands and exits (disposable postgres:18 on 127.0.0.1:35560)

- RED: runtime (exit 1, 3 debt arms), queue (exit 1, spare cases), e2e +
  partial (exit 1), EPQ interleave (exit 1).
- GREEN: same suites exit 0; `-race` over the stranded/EPQ/retry proofs
  exit 0 with 0 `DATA RACE` reports.
- Neighbors exit 0: readiness supersede family, sweep lock-order live
  proofs (359 s suite incl. projector/supersede contention), claim/batch
  queries, prior-failure fold guard, fan-out and manifest live families,
  `internal/reducer/supplychain/...`, focused `internal/reducer` runtime
  and generation-check tests. A first neighbor run showed 10 failures that
  were solely a missing `ESHU_REDUCER_FAIRNESS_PROOF_*` disposable opt-in
  (0.00 s env refusals); with the family env set the same tests pass.

## Impact declarations

No-Regression Evidence: the sweep change is one row-local predicate arm on
rows the sweep already fetches and locks (domain equality + payload probes;
no new join, scan, lock, or ordering change), and the runtime change is one
payload predicate on the already-claimed intent. Proof: the claim, batch
claim, sweep lock-order (150-round contention), readiness-supersede, and
fan-out suites above all pass unchanged, including the active-generation
guard and dropped-claimed-row EvalPlanQual cases; worker counts, batch
sizes, leases, and retry policy are untouched, and useful concurrency is
preserved (four concurrent claimers drain the same conflict domain in the
interleave proof).

No-Observability-Change: no metric, span, log, or status surface is added.
A rescued continuation is visible through the existing fan-out counter
(`eshu_dp_supply_chain_impact_fanout_total` reopened/deferred outcomes), the
`supply_chain_impact.fanout` span, reducer execution counters, and the queue
row itself (spared rows keep their payload debt and failure evidence instead
of disappearing into `superseded`). The status observer CTEs keep hiding
older-generation rows exactly as they do for readiness-spared rows (#4445
precedent), so no readiness surface changes meaning.

Residual risk (unchanged by this fix, stated plainly): if generation
retention pruned the stale generation's facts before a spared continuation
drains, its findings pass re-derives from an empty scope load; the holder
list still drains (it comes from the payload), and any retraction touches
only the pruned generation's own invisible rows. Not verified live.
