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

Drain of 130 items, 8 workers, 2 s of handler work per item, same shim, run at
the final code (head of this change, after the retryable-error rule for a torn
leader). Each item retries on a leader error up to 3 attempts, the
`ESHU_REDUCER_MAX_ATTEMPTS` default of 3 in the env registry, then counts as dead-lettered. Raw captures
`after-drain-head-superseded.txt`, `after-drain-head-active.txt`,
`after-drain-head-active-7s.txt`, `aborted-drain-head-active-5s-timeout600s.txt`:

| Churn during the run | Elapsed | Items per hour | Loads / in-flight retries | Cache hits | Shared / torn-set waiters | Leader errors (`identity_epoch_unstable`) | Items needing a retry | Max attempts one item used | Dead-lettered |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Superseded-generation row deleted every 10 s (4 deletes) | 45.4 s | 10,302 | 1 / 0 | 122 | 7 / 0 | 0 | 0 | 1 | 0 |
| Active-generation fact inserted every 10 s (7 inserts, about one per 2 loads) | 70.8 s | 6,611 | 7 / 0 | 74 | 49 / 0 | 0 | 0 | 1 | 0 |
| Active-generation fact inserted every 7 s (57 inserts, 1.5 per load time) | 6 min 41 s | 1,167 | 49 / 22 | 0 | 113 / 64 | 10 | 8 | 3 | 0 |
| Active-generation fact inserted every 5 s (about one per load time) | did not finish in 600 s (stopped) | n/a | n/a | n/a | n/a | n/a | n/a | n/a | n/a |

The older drain files `after-drain-final-*.txt` ran before the retryable-error
rule existed and are kept only as history (pre-F17): they measured 49.5 s and
139.6 s for the first two churn profiles.

Reading the table. Superseded-generation churn costs nothing. At one active-set
change per two loads the cache still serves 74 of 130 items. At 1.5 changes per
load time the cache never serves a hit, every flight is retried, 10 leaders fail
retryably, and 8 items need a second or third attempt, but none dead-letters
(1,167 items per hour, still about 200 times the pre-change rate of one item
per 8 to 12 minutes, 5 to 7 items per hour). At one change per load time (5 s churn against a 4.7 s load) the
drain did not finish: every flight tears, joined waiters re-probe and wait for
the next flight, and only leaders consume attempts. Production churn is far
below that (see the torn-load paragraph below), but the cache then has no
bound on waiter patience other than the caller's context and the lease, a limit
of the caller loop that existed before this change (each flight now makes at
most two loads) and is bounded only by that context.

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
and can tear an in-flight load. A leader item fails retryably with
`identity_epoch_unstable` only when the epoch moves during both of its two load
attempts. At the measured churn that is about 0.3 percent per leader item; if
one activation landed per load, it is about 6 percent. The item is retried under
the normal lease and retry policy. A dead letter needs three consecutive torn
attempts (`ESHU_REDUCER_MAX_ATTEMPTS`, default 3), which is rare at those odds
and impossible unless the epoch keeps moving. Measured production churn today is
one activation in 24 hours (a read-only count on the reader). The elevated
windows are the delta-active repair and maintenance reopens, which activate many
scopes in a short time. A repeatedly torn item is findable: the dead-letter row
and `eshu_dp_queue_dead_letters_total{queue="reducer",failure_class="identity_epoch_unstable"}`
carry the class. No code changes for this in this PR. Follow-up: narrow the
epoch fingerprint to identity-relevant activations only.
Follow-up: #7825

Raw outputs are attached to the PR (no private paths). File names:
`red-f6-f8.txt`, `red-joinable-mutation.txt`, `red-noreprobe-mutation.txt`,
`red-probeerr-mutation.txt`, `red-noclass-mutation.txt`,
`green-unit-r3.txt`, `green-live-r3.txt`, `red-live-epoch.txt`,
`red-oldprobe-live-mutation.txt`, `red-orfalse-live-mutation.txt`,
`red-keyset-live-mutation.txt`, `before-load.txt`, `after-load-final.txt`,
`after-drain-head-superseded.txt`, `after-drain-head-active.txt`,
`after-drain-head-active-7s.txt`, `aborted-drain-head-active-5s-timeout600s.txt`,
`postgres-race-r3.txt`, `gates-r3.txt`.

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
