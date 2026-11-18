# #10916 — batch-claim candidate CTE reads reps, no fact_work_items rejoin

## What changed

In `claimReducerWorkBatchQuery`
(`go/internal/storage/postgres/reducer_queue_batch_query.go`), the
`candidate` CTE joined back to `fact_work_items` by primary key only to
re-read columns `base` had already snapshotted onto `reps`:
`work_item_id`, `domain`, `conflict_domain`,
`COALESCE(conflict_key, scope_id)` (carried as `reps.ckey`), and
`updated_at`. The candidate CTE now reads those columns from `reps`
directly and the join is gone. The `live_fence_keys` anti-join compares
`reps.conflict_domain` / `reps.ckey`, the identical key pair `base`
computed. Nothing else moved: the locked arms keep their
`fact_work_items AS lock_target` primary-key probes (those predicates are
the EvalPlanQual row-self recheck on the live row, which a snapshot
column cannot replace), the single-claim query, the gates, and every
ranking window are untouched. The funnel census composes the same
candidate chain from the same Go source, so its membership follows
automatically (proved below, not assumed).

## Correctness evidence

Bidirectional differential
(`TestClaimBatchCandidateRejoinRemovalMatchesPreChangeSetAndOrder`,
`reducer_queue_batch_candidate_rejoin_diff_test.go`): the OLD side is the
read-only candidate SELECT machine-derived from the shipped query at the
pre-change base (`deriveReadOnlyCandidateSelect`, frozen verbatim in
`testdata/claim_candidate_pre_candidate_rejoin.sql`, never hand-copied);
the NEW side derives from the shipped query at test time. Both are
projected over every candidate column (id, domain priority, source/domain
inflight counts, source/domain fair ranks, `updated_at`), so the proof is
value-identical rows in identical order, with the set difference empty in
both directions. 19 matrix cells (2 clocks × projector-drain on/off ×
semantic cap points, plus LIMIT 1 and a domain filter), all non-empty,
over one fixture covering:

- NULL `conflict_key` rows riding the `scope_id` ckey fallback, with a
  live holder fencing its scope sibling;
- a live-fenced representative (pending sibling refused by
  `live_fence_keys` while the holder's lease is live; holder reclaims the
  key after expiry);
- projector-blocked and semantic-blocked representatives, including the
  semantic rank-cap tail refusal;
- the same vs fair_same representative divergence: an expired-lease
  claimed holder stays the representative while its older pending sibling
  is the fairness peer (the fair-rank non-member shape), and an older
  search document loses its shared key to a newer plain sibling;
- batch LIMIT 1 (the head row equals the full order's head, both sides);
- the search-doc starvation shape: the oldest rows are priority-1 search
  documents and every priority-0 candidate still sorts ahead of them,
  asserted on the moved priority column's value.

Funnel identity is asserted in the same test: over the fixture, the
funnel census representative count equals the candidate row count. The
hermetic gate `TestClaimBatchCandidateReadsRepsWithoutRejoin` pins the
candidate body free of `fact_work_items` reads, the two `lock_target`
probes intact, the frozen OLD still carrying the rejoin, and OLD/NEW
byte-identical upstream of `candidate`.

Existing suites stay green unweakened on the same disposable Postgres:
rank-once, gate-precompute, and fence-precompute differentials; reserved
slot (`TestClaimBatchReservedSlotMovesLive`), search-doc starvation
(live reproduction and concurrent claimers), funnel reconciliation
(`TestClaimBatchFunnelReconcilesLive`) and the verbatim chain guards;
batch gate/fence concurrency and storms; and the #10863 stale-gate,
storm, and sweep tests. Three literal pins moved only where the SQL text
itself moved (the fence key now reads `reps.*`; the candidate priority
CASE reads `reps.domain`), each annotated in place.

## Performance Evidence: before/after on the replay shape

Method (identical both sides): disposable Docker Postgres 18-alpine on
loopback, the #10862 replay seed (`seedReducerQueue10862Replay`, 8,713
waiting rows — the QA waiting depth recorded in
`docs/internal/evidence/10862-qa-reducer-baseline-2026-11-18.md`).
Candidate chain: `EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON)` of the frozen
pre-change candidate SELECT vs the derived shipped candidate SELECT,
2 warmups + 7 interleaved reps, median execution ms and top-node buffers
(hit+read+dirtied+written). Built path:
`TestReducerQueue10863BatchClaimReplay` (the #10863 harness) run on the
unmodified base build and on this change, same container, draining the
8,713-row seed through `ClaimBatch(16)`+`Ack`.

| Surface | Before (rejoin) | After (reps) |
| --- | --- | --- |
| Candidate SELECT, median ms / buffers | 49.128 / 4,327 | 48.343 / 2,527 |
| Full claim EXPLAIN (replay test), median ms / buffers | 49.632 / 4,847 | 48.827 / 3,047 |
| Built drain, ClaimBatch p50 / p95 ms, wall | 25.581 / 43.339, 21.2 s | 25.214 / 42.702, 21.0 s |

Both EXPLAIN surfaces drop exactly 1,800 buffers: one primary-key heap
fetch per representative row the rejoin used to re-read and `reps` now
carries. Latency at this depth is unchanged within noise (the ranking
windows both sides share dominate it), so the measured win is buffer
traffic per claim (−41.6% on the candidate chain, −37.1% on the full
claim statement), not wall time on this seed. The before column is
baseline-consistent with the #10863 record (its after-side full-claim
explain measured 49.592 ms / 4,847 buffers and drain p50 26.179 ms /
p95 43.737 ms on the same seed and method; see
`docs/internal/evidence/10863-claim-supersede-out.md`).

No QA percentage is claimed: QA is buffer-bound at a different scale and
these numbers prove mechanism on the replay shape only. This change does
NOT claim the #10864 flatness target: claim cost still grows with the
candidate set through the shared ranking windows; removing the rejoin
only stops paying a second heap fetch for rows already in hand. The
17,426-row doubling was not run (the replay seed is a fixed recorded mix
with no scale parameter; listed as not checked rather than approximated).

No-Observability-Change: no metrics, spans, logs, or telemetry surfaces
change. The claim emits the same funnel census from the same chain, and
an operator sees this change only as lower per-claim buffer counts in
`pg_stat_statements` / EXPLAIN output for the batch claim statement.
