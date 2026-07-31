# #5854 — outcome-independent container-image identity cutover

## Result

`reducer_container_image_identity` now has one durable logical identity per
`(scope_id, generation_id, image_ref)`. Outcome remains payload, so a
reclassification collides on the same `fact_id`; an authoritative demotion
writes a tombstone at that key. Both paths carry the evidence-read fencing
token introduced by #5847. A late stale worker therefore cannot overwrite a
fresher classification or resurrect a fresher tombstone.

The change also retires exact outcome-keyed rows written before #5854. It does
not use the rejected generation-wide DELETE. The planner derives only the
legacy ID that the old writer could have produced for each evaluated
reference, and publication plus exact cleanup commits atomically.

Collector-declared incompleteness is fail-closed:

- `tag_list_truncated` holds affected tag references.
- `config_blob_unavailable` holds manifest digests mapped from the warning. If
  the active manifest set cannot map the config digest, retirement holds that
  warning's repository and continues for unrelated repositories.
- `missing_manifest_digest` holds the named repository conservatively.
- malformed, unreadable, or unavailable warning-loader state stops a
  destructive pass before the writer runs.

An all-canonical pass skips the warning read because it cannot demote a
canonical publication.

## Rolling-upgrade fence

The new logical-key token cannot conflict with an old binary that derives a
different outcome-keyed `fact_id`. Migration 088 therefore adds a temporary
format-compatibility fence:

1. New rows carry `payload.identity_format = image_ref_v2`.
2. The first new writer creates one durable
   `container_image_identity_cutovers` marker for its scope generation in the
   same transaction as publication and cleanup. V2 publication enters this
   path even when every eligible legacy row is held and the exact cleanup list
   is empty; cleanup eligibility never controls whether the compatibility
   fence is installed.
3. A capable claim atomically advances the claim epoch and durably latches
   `container_image_identity_v2_required` with
   `status='claimed'` and
   `container_image_identity_v2_authorized_status='claimed'`. This capability
   handoff survives a later first-cutover transaction rollback.
4. The marker trigger locks that exact latched claim and transitions it to
   `running/running` before publication. Marker, v2 publication, and exact
   cleanup remain one atomic transaction.
5. A statement-level transition-table trigger rejects or suppresses legacy
   outcome-keyed fact writes after the marker. It serializes only writers for
   the same scope generation; unrelated keys remain concurrent.
6. The queue claim trigger is defined as `BEFORE UPDATE OF last_attempt_at,
   container_image_identity_claim_epoch` with a domain predicate. Including the
   legacy claim timestamp lets the trigger advance the epoch for an old
   same-owner re-claim before the capable handoff and reject that old shape
   after the latch is durable. Every capable target claim increments the epoch
   explicitly and authorizes `claimed`; unrelated claims do not execute the
   trigger body.
7. ACK, retry, and failure SQL bind the exact claim epoch and write the matching
   authorized terminal status. The row constraint requires
   `status = container_image_identity_v2_authorized_status` whenever the v2
   requirement is set. Old SQL cannot update that authorization and therefore
   cannot certify a post-cutover transition.
8. Replay and recovery write matching authorization state. An old callback
   carrying a stale epoch cannot ACK, retry, or fail a reclaimed item, including
   when the literal lease owner is reused.

The marker is durable, so later writers do not reacquire the first-cutover
compatibility lock. A partial index proves whether any active legacy row still
exists for the scope generation:

```sql
CREATE INDEX fact_records_container_image_identity_legacy_cleanup_idx
ON fact_records (scope_id, generation_id, fact_id)
WHERE fact_kind = 'reducer_container_image_identity'
  AND is_tombstone = FALSE
  AND COALESCE(payload->>'identity_format', '') <> 'image_ref_v2';
```

The lookup orders by `fact_id` with `LIMIT 1`, forcing a bounded index-only
probe in the zero-legacy steady state. When the marker and zero-legacy proof
both hold, a single-chunk pass uses the exact-claim publication-only statement;
a multi-chunk pass locks the exact claim once and retains one transaction across
bounded publication chunks. If any held legacy row remains, exact cleanup stays
enabled until that row becomes eligible.

## Prove-the-theory-first evidence

All fixtures use public synthetic values such as
`registry.example.com/performance/team-api` and placeholder SHA-256 digests.

Performance Evidence: against the old outcome-keyed writer on the same
PostgreSQL 18 backend and 99,500-reference input, the exact-head v2 writer
preserved the logical checksum and terminal 99,500-row count while improving
median from 6.281 seconds to 4.580 seconds and p95 from 6.470 seconds to 4.744
seconds. The
live golden gate terminated with zero residual work items, zero dead letters,
517 passing assertions, zero required failures, and one advisory in 109 seconds
(exit 0).

Observability Evidence: the bounded
`eshu_dp_container_image_identity_decisions_total` and
`eshu_dp_container_image_identity_retirements_total` counters expose
classification, holds, attempts, and cleanup; existing Postgres query-duration
and reducer execution/run-duration signals expose write and claim failures.
The exact-head live golden gate also records terminal queue counts and
per-phase timings, so the faster path is not accepted on latency alone.

### Correctness and concurrency theories

- A live token 9 row, token 10 tombstone, stale token 5 live write, and fresh
  token 11 live write proved the conflict fence: the stale write affected zero
  rows and the fresh write revived the tombstone.
- A cutover held for one scope generation blocked same-key old and new writers
  while an unrelated scope committed. After cutover commit, the old writer
  inserted zero rows and the fresher logical-key writer won.
- An all-held mixed pass published its canonical v2 row and marker atomically,
  preserved the held legacy row, rejected a later old-format write, and
  rejected a stale claim epoch. A fully held demotion with no publication
  performed no database work.
- Injecting a later-chunk failure rolled back all new rows, exact cleanup, and
  the marker. A retry then converged to the complete v2 set.
- An existing two-row legacy upsert in reverse fact-ID order reproduced the old
  fact-row-to-advisory versus advisory-to-fact-row lock inversion. The current
  writer uses an exact `FOR UPDATE NOWAIT` prelock after marker acquisition:
  20/20 live contention trials returned classified retryable lock-busy errors,
  rolled back marker/publication/cleanup, allowed the old writer to finish, and
  then converged on retry with no eligible legacy row left.
- A production `Service` plus `ReducerQueue` proof forced the capable claim to
  fail with the classified lock-busy error while a same-owner legacy ACK raced
  the failure path. The claim-time latch survived marker rollback, rejected the
  legacy ACK and legacy reclaim, committed `retrying/retrying`, advanced the
  capable retry to epoch 3, and let the marker transition that exact claim to
  `running/running`. Both the writer contention proof and this queue lifecycle
  proof passed 20/20 repetitions.
- A failed migration under a held fact-table lock left no partial table,
  column, function, trigger, or constraint. Retry through `ApplyBootstrap`
  preserved rows and remained idempotent.
- The cleanup probe used the partial index in both the zero-legacy and
  one-legacy-row cases, touching only a few buffers.

### Queue and claim performance

Fixed-schema paired tests compare the old queue table with migration 088
applied. Six trials, 100 warmups, and 1,000 paired iterations per operation
cover unrelated claim, ACK, failure, target scalar ACK, and target batch ACK.
The unrelated paths remain inside the repository's 5% median, 10% p95, and
25-microsecond absolute-p95 policies. Batch sizes 1, 16, and 64 preserve exact
row selection. The live claim-trigger catalog proof requires exactly one
user-defined `fact_work_items` trigger, the `UPDATE OF last_attempt_at,
container_image_identity_claim_epoch` column list, and the target-domain
predicate. Two competing claimers produce one successful claim and one epoch
increment.

No-Regression Evidence: the final claim-fence rerun kept all measured shapes
inside those budgets. The pre-cutover target-domain claim moved from 1,206.875
to 1,262.792 microseconds at median (+4.63%) and from 2,181.458 to 2,229.959
microseconds at p95 (+2.22%). A separate unrelated-domain claim rerun moved
from 1,847.834 to 1,860.375 microseconds at median (+0.68%) and from 2,385.417
to 2,436.792 microseconds at p95 (+2.15%); this is the `ownership` row for
which the target-only epoch `CASE` must retain epoch zero. The single-row
target ACK moved from 729.979 to 735.375 microseconds at median (+0.74%) and
from 1,196.000 to 1,168.708 microseconds at p95 (-2.28%). The legacy
pre-cutover batch-16 ACK moved from 2,042.958 to 1,986.459 microseconds at
median (-2.77%) and from 3,524.875 to 3,595.125 microseconds at p95 (+1.99%).
The target batch-64 ACK moved from 1,821.333 to 1,853.146 microseconds at
median (+1.75%) and from 3,097.063 to 3,102.229 microseconds at p95 (+0.17%).
The old-shape derivation guard also proves the performance baseline is distinct
from the current query and contains neither the claim epoch nor authorization
columns; it cannot silently measure the current query on both sides.

The claim-time capability latch is also measured against the exact prior-head
claim SQL on the same migrated schema. Six trials with 100 warmups and 1,000
paired single claims moved median from 201.417 to 201.292 microseconds (-0.06%)
and p95 from 231.375 to 231.958 microseconds (+0.25%). Production batch-query
trials remained inside the same budgets: batch 1 moved +0.54% median/+1.80%
p95, batch 16 +0.18%/-0.49%, and batch 64 +1.45%/-1.05%. The older
pre-migration-to-prior-head comparison also remained green at +3.73% median
and +3.47% p95. The latch adds no query round trip and does not serialize
unrelated work.

No-Observability-Change: claim attempts, failures, and Postgres duration remain
visible through the existing reducer execution/run-duration and Postgres
query-duration signals. The fence adds no worker, retry, queue, metric label,
or runtime knob; SQLSTATE `55000` distinguishes rejected legacy or invalid
claim epochs in the existing error path.

### Production writer performance

The production-handler benchmark retains three distinct lanes:

- uncached full handler, representing cache-disabled or cap-exceeded fallback;
- cache-warm full handler, representing the default reducer wiring;
- writer-only, isolating publication from the common cross-scope evidence
  load.

The final-head 99,500-reference writer-only confirmation on the same Postgres
instance produced the identical logical checksum and terminal row count:

| variant | median | p95 | write statements |
| --- | ---: | ---: | ---: |
| old outcome-keyed writer | 6.281 s | 6.470 s | 199 |
| final v2 writer | 4.580 s | 4.744 s | 100 |

The final v2 writer is 27.1% faster at median and 26.7% faster at p95. The
exact first-marker
operation on 100,000 historical rows measured 192.167 microseconds median and
294.750 microseconds p95 against its 1,301.150-microsecond contribution budget.

The lock-order fix adds work only to a first-cutover transaction with an exact
legacy cleanup set. `EXPLAIN (ANALYZE, BUFFERS, WAL)` measured its conservative
prelock contribution at 0.150 milliseconds for zero rows, 0.130 milliseconds
for one row, 0.582 milliseconds for 500 rows, and 57.696 milliseconds plus
5.373 MB WAL for 99,500 existing rows. At worst cardinality that is about 1.3%
of final writer latency while preventing a transaction-aborting deadlock.
Completed-cutover writes do not run the prelock. The global WAL observation is
reported below without attributing it to this transaction.

The cache-warm production handler also returned the same checksum:

| variant | median | p95 | queries/op |
| --- | ---: | ---: | ---: |
| old outcome-keyed writer | 6.825 s | 6.995 s | 5 |
| final v2 writer | 5.308 s | 5.329 s | 8 |

The final path was 22.2% faster at median and 23.8% faster at p95. The extra
three queries are the exact cutover marker, zero-legacy probe, and claim check.
The final-head run kept the same 99,500-row checksum, exactly one epoch probe
per measured call, and zero paginated identity loads.

The uncached lane performs 204 unchanged paginated evidence reads at this
cardinality for both variants. One paired run measured 11.530 s main versus
11.901 s head at median, while the head write statements themselves were about
one second faster per operation. Query-call time for the shared evidence reads
varied by more than two seconds per operation, so this lane is retained as a
fallback guard and not used to attribute writer cost.

The harness reads server-global `pg_current_wal_lsn()`, not transaction-local
or schema-local WAL. Exact-head observations ranged from 384.6 to 493.9 MB/op
as checkpoint/full-page-write state changed, so no WAL improvement claim is
made for either the cache-warm or writer-only lane.

## Live acceptance proof

Focused live tests cover:

- mixed eligible and warning-held legacy rows, then warning-clear retirement;
- an all-held mixed pass that publishes v2, creates the marker, preserves the
  held legacy row, rejects a later old writer, and rejects a stale claim epoch;
- a missing warning-loader capability that stops before any writer call, plus
  a fully held demotion that performs no database work;
- marker-present seeded legacy rows;
- first-cutover rollback atomicity;
- old INSERT and UPDATE before, during, and after marker commit;
- stale exact-claim rejection for single- and multi-chunk writes;
- same-owner reclaim and replay;
- later-chunk rollback and retry;
- exact batch sizes 1, 16, and 64;
- cleanup-probe index plans;
- migration failure, retry, backfill, and repeated bootstrap.

Primary commands:

```bash
cd go
ESHU_POSTGRES_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu' \
  go test ./internal/reducer -run '^TestPostgresContainerImageIdentity' -count=1
ESHU_POSTGRES_TEST_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu' \
  go test ./internal/storage/postgres -run '^TestContainerImageIdentity' -count=1
ESHU_POSTGRES_TEST_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu' \
  go test -tags perf5854_ack ./internal/storage/postgres \
  -run '^TestContainerImageIdentity.*Performance.*Live$' -count=1
ESHU_5854_PERF_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu' \
ESHU_5854_PERF_VARIANT=head \
  go test -tags perf5854_head ./cmd/reducer \
  -run '^TestContainerImageIdentity.*PerformanceLive$' -count=1
```

The final promotion sequence also requires the live golden-corpus gate after
the last edit, preliminary and final `eshu-code-review` verdicts with
P0=P1=P2=0, and `make pre-pr` between those reviews.

## Observability

`eshu_dp_container_image_identity_retirements_total` uses bounded `domain` and
`outcome` labels. Outcomes are `retirement_attempted`, `legacy_deleted`, and
`held_<reason>`. `retirement_attempted` is intentionally not named
`tombstoned`: a fresher row can reject an attempted publication at the conflict
fence. Existing Postgres query-duration metrics, reducer execution metrics,
status, and traces expose cutover or claim failures without high-cardinality
labels.
