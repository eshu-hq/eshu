# Identity epoch cache: shared flight, active-set epoch, ordered page

Root-Cause Evidence: In the production environment the `container_image_identity`
domain finished one item every 10.7 minutes after a maintenance reopen of about
130 items. Three defects combined (#7805).

1. `IdentityEpochCache.get` made waiters of an in-flight load recurse into
   `get`, re-probe, and queue behind a new serial load. A load whose epoch moved
   mid-load was returned uncached to its leader only. The waiters never received
   the leader's rows.
2. The epoch probe counted identity facts over all generations. Retention
   deletes of superseded-generation rows moved the count every few minutes, which
   is shorter than one load, so nearly every load was discarded.
3. The page query joined `ingestion_scopes` and `scope_generations` to
   `fact_records`. The planner drove it from the roughly 1,400 active scopes,
   read the whole active identity set, and top-N sorted it for every 500-row
   page, so one full load was quadratic.

Shim: throwaway native PostgreSQL 18 cluster, schema from the repo migrations,
1,400 active scopes (200 OCI registry, 60 cloud account, 1,140 git), 2.95M
`fact_records` rows, 1,013,982 identity facts of which 546,562 are in active
generations. The load order and the active set size match the production
observations (production: 1,027,363 identity facts, a 618 to 640 s load).
Host load average during the runs was 7 to 17 (shared laptop), so absolute
seconds carry that noise. Before and after ran on the same cluster and corpus.

Performance Evidence: One full load of the active identity set
(`FactStore.loadIdentityFactsUncached`, 500-row keyset pages), raw captures
`before-load.txt` (base `01ceb1dd0`, JOIN page query) and `after-load-final.txt`
(final head), same cluster and rows (546,568 rows; six more than the first
runs because live tests left residue), host load average 8 to 21:

| Page query | Run 1 | Run 2 | Rows | Ordered fact-id digest |
| --- | --- | --- | --- | --- |
| JOIN form (before) | 12 min 4.2 s (724.2 s) | 9 min 48.7 s (588.7 s) | 546,568 | 54cd8324a2d7fc04 |
| Hashed SubPlan form (after) | 4.66 s | 4.71 s | 546,568 | 54cd8324a2d7fc04 |

An earlier session run on 546,562 rows measured 494.9 s and 504.9 s before and
4.99 s and 4.77 s after (digest 9875824362cef277 on both); that run was not
saved raw, and the table above supersedes it. The before time varies with host
load (495 to 724 s); the after time does not (4.7 to 5.0 s).

That is about 125 to 155 times faster on the raw runs, with an identical ordered row set. The
per-page `EXPLAIN (ANALYZE, BUFFERS)` on the same shim, first page and a page
at row 270,000: JOIN form 634 ms and 359 ms (1,676,069 and 844,240 shared
buffer hits, `Sort` over 546,562 and 274,261 rows); hashed SubPlan form 2.5 ms
and 2.5 ms (`Index Scan using fact_records_identity_epoch_idx_v2`, no `Sort`,
500 rows after removing 428 and 407 superseded rows by filter). A rewrite as
`EXISTS` or `IN (subquery)` is pulled up into the same semi-join and keeps the
slow plan. A `= ANY(ARRAY(subquery))` filter chose a bitmap scan and was slower
(821 ms and 3,135 ms). Only the `OR FALSE` form, which blocks the sublink
pull-up, keeps the ordered index scan.

Epoch probe, `EXPLAIN (ANALYZE, BUFFERS)` on the same shim:

| Probe | Time | Shared buffer hits | Notes |
| --- | --- | --- | --- |
| All generations (before) | 59 to 65 ms | 446,946 | Index-only scan, 1,013,982 rows counted |
| Active set, JOIN form | 550 to 593 ms | 1,676,082 | Scope-driven nested loop, rejected |
| Active set, hashed SubPlan (after) | 120 to 135 ms (EXPLAIN); 100 to 104 ms through the Go path (`after-load-final.txt`) | 51,015 | Parallel bitmap heap scan, 546,562 rows counted |

The after probe is about twice the before probe and needs no new index. It
reads the heap for `scope_id` and `generation_id` because the partial index
holds only `(observed_at, fact_id)`. An index that includes `generation_id`
would bring it back to an index-only scan; that needs a migration and is not
part of this change.

Drain of 130 items, 8 workers, 2 s of handler work per item, same shim, at the
final code: the waiter bound (3 flights or one heartbeat interval of total
waiting, 30 s), one final probe before a waiter gives up, and the rule that a call
with its wait budget used up never starts a load. Each item retries on an error up
to 3 attempts (`ESHU_REDUCER_MAX_ATTEMPTS` default 3), then counts as
dead-lettered. The churn is one active-generation fact inserted at the interval
shown. Before each run the harness times two uncached full loads on the host as
it is at that moment. The host was shared and heavily loaded during these runs
(load average 45 to 57), so loads cost 7 to 13 s here against 4.7 s on a quiet
host, and with 8 workers loading at once a load takes longer still. Raw captures:
`after-drain-head-superseded.txt`, `after-drain-final-active-10s.txt`,
`after-drain-final-active-7s.txt`, `after-drain-final-active-5s.txt`.

Failures split by who failed. "Leaders" counts flights whose leader discarded a torn
set (`passthrough_total{reason="epoch_moved"}`); "waiters" counts
`flight_waiter_total{outcome=gave_up_*}`. Their sum is the number of
`identity_epoch_unstable` errors the items saw.

| Churn | Calibrated full load | Elapsed | Items/h | Loads / in-flight retries | Hits | Leader failures | Waiter give-ups (flights / wall) | Dead-lettered | Max time in the cache by one call (led only / waited then led / waited only) |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Superseded row deleted every 10 s (quiet host, earlier run) | 4.7 s | 45.4 s | 10,302 | 1 / 0 | 122 | 0 | 0 | 0 | n/a |
| Active insert every 10 s | 10.3 s, 12.9 s | 6 min 1 s | 1,296 | 31 / 9 | 24 | 9 | 0 / 40 | 8 of 130 | 33.3 s / 45.8 s / 32.1 s |
| Active insert every 7 s | 7.3 s, 7.0 s | 12 min 39 s | 617 | 100 / 48 | 0 | 40 | 8 / 100 | 40 of 130 | 25.1 s / 60.6 s / 31.9 s |
| Active insert every 5 s | 7.2 s, 6.7 s | 26 min 31 s | 294 | 182 / 91 | 0 | 91 | 35 / 264 | 130 of 130 | 31.9 s / 64.6 s / 32.6 s |

Reading the table.

- The 5 s churn is outside today's production (one activation per 24 hours) and
  inside the repair and maintenance-reopen windows. At one change per 5 s against a
  7 to 13 s load, every flight tears, no cache entry survives, and every item
  dead-letters after 3 attempts with class `identity_epoch_unstable`. The run
  terminates. That is the F21 case and the reason for #7825. Dead letters with
  class `identity_epoch_unstable` are requeued per cycle through the deployment
  runbook. The eshu docs describe the guarded manual replay by `failure_class`
  (3 AM runbook, "Symptom: deadletter growth"); the class is not in the
  unsafe-replay list, so no `force` is needed. The eshu docs do not describe a
  per-cycle requeue, and none is added here.
- Most failures are waiters, and mostly `gave_up_wall`. With loads this slow, a
  waiter's 30 s budget runs out on a flight that is still loading or still torn.
  `gave_up_flights` dominant would mean churn on its own; here the slow host makes
  `gave_up_wall` dominant too. A slow but stable flight (a load over about 30 s)
  fails its waiters the same way, with the same class.
- The 10 s and 7 s rows are not comparable with the quiet-host figures of earlier
  rounds (0 errors at 10 s and 7 s with 4.7 s loads). The same churn that cost
  nothing at 4.7 s loads tears flights at 7 to 13 s loads, because the exposure is
  churn times load time.
- Measured worst case per call under the final rule: a call that led only took up
  to 33.3 s (two load attempts and their probes). A call that waited only took up
  to 32.6 s (the 30 s wait plus the final probe). A call that waited and then led
  took up to 64.6 s: 30 s of waiting plus up to two loads, each slower than the
  calibrated single load because 8 workers load at once. That is the bound: one
  heartbeat interval plus two load attempts and a few epoch probes. The earlier 49.5 s figure was this
  same case under the previous rule. Calls that exhausted their wait budget never
  start a load.

Earlier runs, kept as history and labelled.

Before the waiter bound existed (quiet host, loads 4.7 s):

| Churn | Elapsed | Items/h | Leader errors | Dead-lettered |
| --- | --- | --- | --- | --- |
| Active insert every 10 s | 70.8 s | 6,611 | 0 | 0 |
| Active insert every 7 s | 6 min 41 s | 1,167 | 10 | 0 |
| Active insert every 5 s | not finished in 600 s (stopped) | n/a | n/a | n/a |

Waiter bound without the wait-or-lead rule and without the final probe (commit
35f0abd65, host slowed, loads 21 to 29 s at 5 s churn). The earlier table called
all 280 errors "leader errors"; 82 were leaders and 198 were waiters:

| Churn | Elapsed | Items/h | Leaders / waiters failed | Dead-lettered | Longest call |
| --- | --- | --- | --- | --- | --- |
| Active insert every 10 s | 70.0 s | 6,683 | 0 / 0 | 0 | 6.0 s |
| Active insert every 7 s | 2 min 28.6 s | 3,150 | 0 / 0 | 0 | 11.7 s |
| Active insert every 5 s (loads 21 to 29 s) | 17 min 57.6 s | 434 | 82 / 198 | 80 of 130 | 49.5 s |

One more 10 s run on a loaded host before the calibration step existed took
8 min 21 s with 23 dead letters. Aborted runs (stopped by hand) are not kept.
Between those 35f0abd65 drains and its commit, the cache logic did not change;
the commit only moved the patience code into existing files so the directory file
cap holds. The final-rule runs above were taken after the wait-or-lead change.

Why the bound exists. Before it, a waiter whose probe never matched a stable
flight re-probed and re-joined the next flight without limit, while only the
leaders consumed claim attempts. The reducer keeps a claim alive with a
heartbeat every 30 s, so such a waiter did not lose its claim and there was no
duplicate execution. The harm is a stalled pool: each stuck waiter holds one of
the 8 workers for as long as the churn lasts, and under sustained churn every
worker ends up parked in the cache and no item makes progress. The bound
(3 unserved flights or 30 s of total waiting, the reducer heartbeat interval
taken from the same claim lease the queue uses, no new setting) fails the
waiter's item with the same retryable `identity_epoch_unstable` as a leader, so
the queue re-runs it under the existing attempt limit. The leader is bounded by
its two load attempts and is not cut off by wall time, so a slow healthy load
still finishes. Invariant: A call's time inside the identity cache is bounded by one heartbeat interval plus two load attempts and a few epoch probes (at most about five); a call that has exhausted its wait budget (3 flights or one heartbeat interval) never starts a load: it fails retryably with identity_epoch_unstable and the next caller leads with a fresh budget.
A caller that still has budget may lead (at most 2 load attempts). Waiter failures split
into two labels on one counter: `gave_up_flights` dominant means sustained churn,
`gave_up_wall` dominant means a slow stable flight (a load longer than about 30 s also
trips the wall bound, with the same class). Before a waiter gives up it makes one final
probe and is served a cache that a consistent flight filled in the meantime; the probe
never starts a load.
Tests: `TestContainerImageIdentityHandlerGivesUpAfterThreeTornFlights` (a
handler that joins three torn flights fails retryably with no decision write),
`TestIdentityEpochCacheWaiterGivesUpAfterOneHeartbeatInterval` and
`TestIdentityEpochCacheWaiterWaitBudgetIsCumulative` (the 30 s bound against an
injected clock, and that it is a total budget), and
`TestIdentityCacheWaitBoundIsOneHeartbeatOfTheClaimLease` in `cmd/reducer`.

The before drain was not run: with a 500 to 720 s load and an epoch that moves
every few minutes the cache never populates, so each load served one item. That
gives about 130 loads of 8 to 12 minutes each (17 to 26 hours at the shim load
time), consistent with the production projection of more than 12 hours. That
before figure is an estimate from the measured single-load time and the
production observation, not a measured drain.

Concurrency: one flight serves every caller that joined it. Arbiter ruling
(clarification of "every waiter gets the flight rows"): a waiter joins an
in-flight load only if its trigger committed before the leader's probe
snapshot, which the code checks as "the waiter's own epoch probe equals the
flight's start epoch". Otherwise the waiter waits for the flight and then
starts or joins a fresh load, so it never decides on a set that predates its own
trigger. Two tests pin it. `TestIdentityEpochCacheLateCallerAfterValidatingProbeDoesNotJoinFlight`
parks the leader after its validating post-load probe returned the start epoch
(so no in-flight retry can run), moves the epoch, and adds a late caller: it
must get a fresh load, and it fails under a `joinable := true` mutation.
`TestIdentityEpochCacheLateCallerDoesNotJoinStaleFlight` covers the other
window, a commit before the validating probe, which the in-flight retry also
covers.

The paged load is more than a thousand READ COMMITTED statements, so a
generation flip between pages can tear the set (old-generation rows after the
cursor drop out, new-generation rows before the cursor are never read). The
post-load probe detects the flip. The flight then loads once more from the moved
epoch (at most two attempts, `eshu_dp_identity_cache_load_retry_total`). If the
epoch is still moving after the second attempt, or the post-load probe fails,
the set is discarded: every waiter re-probes
(`flight_waiter_total{outcome="torn_set"}`) and the leader's own item fails
with a retryable `identityLoadUnstableError` (failure class
`identity_epoch_unstable`, a counting retry class bounded by the claim-attempt
limit; `TestIdentityEpochUnstableFailureClassCountsClaimAttempts` pins that it is
not in the non-counting list), so the queue re-runs it from a fresh probe. No item
is decided on a set whose post-load probe differs from its pre-load probe, and the
former passthrough of a known-mismatched set is gone.
`TestContainerImageIdentityHandlerDecidesNothingOnATornSet` drives the real
handler over the real cache with a flip on every attempt and asserts the
retryable error, the failure class, zero decision writes and two loads. The
code has no per-item direct query to fall back to, so the retryable error is the
passthrough. A single long REPEATABLE READ snapshot was refused: it
pins the vacuum horizon and blocks `CREATE INDEX CONCURRENTLY`. Snapshot
consistent paging without a long snapshot is a follow-up, not part of this
change. Unit tests
`TestIdentityEpochCacheRetriesInsideFlightWhenEpochMovesDuringLoad`,
`TestIdentityEpochCacheNeverDeliversTornSetToWaiters`,
`TestIdentityEpochCacheSharedFlightServesEveryWaiterWhenEpochStable`,
`...LeaderCancelDoesNotFailWaiters`, `...LoadErrorIsSharedWithWaiters`, and
`...LeaderPanicReleasesWaiters` cover these paths under `-race`.

Torn means any epoch move, and what that costs (F21, deferred by arbiter ruling).
The epoch fingerprint hashes the active generation of every scope, so a
generation activation in any scope, identity-relevant or not, moves the epoch
and can tear an in-flight load. Two kinds of caller fail with
`identity_epoch_unstable`, and both are counted in the churn table above.

- A leader fails when the epoch moves during both of its two load attempts. At
  the churn measured in production that is about 0.3 percent per leader item; if
  one activation landed per load, about 6 percent.
- A waiter fails when 3 flights in a row do not serve it, or when it has waited
  one heartbeat interval (30 s) in total, and a final probe finds no usable set.
  This path needs no churn. A set that stays over the cache cap is never cached,
  so every caller loads it; if one such load takes longer than about 30 s, every
  waiter of that load fails with `gave_up_wall` even though the epoch never moved.
  A slow but stable flight therefore dead-letters its waiters on the same
  attempts as churn does.

Either kind retries under the normal lease and retry policy. A dead letter needs
three consecutive failures of one item (`ESHU_REDUCER_MAX_ATTEMPTS`, default 3).
At the production churn that is rare for the leader path. It is not impossible
for the waiter path while loads stay slower than about 30 s with the set
uncached. The measured rates are in the churn table: at 5 s churn on a loaded
host, 91 leader failures and 299 waiter give-ups ended with all 130 items
dead-lettered. Measured production churn today is one activation in 24 hours (a
read-only count on the reader). The elevated windows are the delta-active repair
and maintenance reopens, which activate many scopes in a short time.

A repeatedly failing item is findable: the dead-letter row and
`eshu_dp_queue_dead_letters_total{queue="reducer",failure_class="identity_epoch_unstable"}`
carry the class, and `flight_waiter_total{outcome=gave_up_flights|gave_up_wall}`
says whether churn or a slow flight caused it. No code changes for this in this
PR. Follow-up: narrow the epoch fingerprint to identity-relevant activations only.
Follow-up: #7825

Raw outputs are attached to the PR (no private paths). File names:
`red-f6-f8.txt`, `red-joinable-mutation.txt`, `red-noreprobe-mutation.txt`,
`red-probeerr-mutation.txt`, `red-noclass-mutation.txt`,
`red-noflightbound-mutation.txt`, `red-nowallbound-mutation.txt`,
`red-wallleads-mutation.txt`, `red-nofinalprobe-mutation.txt`,
`red-heartbeat-wiring-mutation.txt`, `red-live-epoch.txt`,
`red-oldprobe-live-mutation.txt`, `red-orfalse-live-mutation.txt`,
`red-keyset-live-mutation.txt`, `green-unit-r7.txt`, `green-live-r7.txt`,
`before-load.txt`, `after-load-final.txt`, `after-drain-head-superseded.txt`,
`after-drain-final-active-10s.txt`, `after-drain-final-active-7s.txt`,
`after-drain-final-active-5s.txt`, `gates-r7.txt`. The captures `green-unit-r6.txt`,
`green-live-r6.txt`, and `gates-r6.txt` are the earlier run at the same logic;
the r7 files were taken after the final context change in the give-up probe.

Plan guard: the page SQL is Postgres, and `internal/queryplan` pins graph
(Cypher) reads only, so it has no entry for this query. The guard is
`TestIdentityPageQueryPlanRidesOrderedIndexLive`: on a private schema built by
the real migrations it runs `EXPLAIN (FORMAT JSON)` of the page query for the
first page and for a mid-load page, and asserts an Index Scan on
`fact_records_identity_epoch_idx_v2`, a hashed SubPlan filter, no Sort, no Seq
Scan on `fact_records`, and, on the mid-load page, the keyset comparison as an
Index Cond (not a Filter). It runs in the `postgres_ci` lane
(`live-postgres-readiness`, advisory today). The rewrite depends
on planner behavior verified on PostgreSQL 18; that test pins it, and the
text-shape asserts only stop the `OR FALSE` from being deleted.

Correctness: `TestIdentityEpochIgnoresSupersededGenerationRowsLive` shows a
delete on a superseded generation leaves the epoch unchanged and an insert or
delete on the active generation moves it (before the change the first assertion
failed with count 1,013,986 then 1,013,985, raw `red-live-epoch.txt`).
`TestIdentityPageQueryServesOnlyActiveGenerationsLive` serves only a
non-tombstone identity fact of a scope's active generation whose generation row
is `active`.

Observability Evidence: `eshu_dp_identity_cache_reload_total` counts loads
started. `eshu_dp_identity_cache_passthrough_total` gained a `reason` label
(`epoch_moved`, `cap_exceeded`, `size_unknown`, `probe_error`) and records every
discarded load. The new `eshu_dp_identity_cache_flight_waiter_total` has an
`outcome` label (`shared`, `shared_error`, `stale_epoch`, `leader_canceled`, `torn_set`) and
counts callers that joined or queued behind a load. The new
`eshu_dp_identity_cache_load_retry_total` counts loads repeated inside a flight.
`eshu_dp_identity_cache_reload_duration_seconds` now records every load, not
only cached ones. At 3 AM: many `shared` per `reload_total` is healthy; a high
`passthrough_total{reason="epoch_moved"}` or `stale_epoch` count means the
active set moves faster than a load.

Claim fairness (`reducer_queue_batch_query.go`, the `locked` CTE ordering) is
not changed by this work. NOT_CHECKED: no RED/GREEN for a one-line ordering
change that gives held `deployable_unit_correlation` rows a slot.
