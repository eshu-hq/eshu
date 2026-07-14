# Evidence: source-owned repo-dependency concurrency (#5122)

## Decision

Proceed with the Odù, the grouped-write retry prerequisite, and the
default-off actual-runner proof. Do not enable multiple production workers or
claim an under-20-minute path yet.

The retained 896-repository run shows that the globally serial
`repo_dependency` lane is large enough to matter. After subtracting the
already-removed code-import work from #5210, the inferred serial residual is
`1919.233596s` (`31m59.234s`). Even a conservative fixed two-shard schedule
has a no-contention ceiling of `600.972567s` (`10m00.973s`) recoverable; four
fixed shards have a ceiling of `1104.554718s` (`18m24.555s`). Both exceed the
`158s` worthwhile-work threshold.

Against #5122's `587s` under-20 stretch gap, the conservative fixed-two ceiling
clears the stable-drain screen by only `13.972567s`; fixed four clears it by
`517.554718s`. The `103.19s` primary-exit overlap still fails to prove the
primary target under either schedule.

That arithmetic proves that this lane is worth developing. It does not prove
the primary bootstrap-exit contribution or authorize a production default.
Only about `103.19s` of the measured lane overlapped the primary process-exit
boundary. Dynamic acceptance-unit leases and a same-data built-binary run are
still required before worker count can be raised or the under-20 target can be
claimed.

## Retained data and boundary

The proof uses the terminal Postgres state from the retained 896-repository
run. Connection details and operator-local locations are intentionally absent.

| Data surface | Cardinality |
| --- | ---: |
| Source repositories / acceptance units | 896 |
| Completed `repo_dependency` intents | 2,414 |
| `projection/code-imports` intents | 838 across 838 repositories |
| `resolver/cross-repo` intents | 1,576 across 896 repositories |
| Measured inter-cycle gaps | 895 |

One duration weight was assigned to each source repository. For all but the
last source, the weight is the gap from its completed-intent timestamp to the
next source's completed-intent timestamp. The last weight is the gap to the
terminal `fact_work_items.updated_at`. This is a stable full-drain scheduler
proxy, not a per-statement attribution.

| Serial measurement | Seconds | Human duration |
| --- | ---: | ---: |
| 895 inter-cycle gaps | 2276.106793 | 37m56.107s |
| Final-job terminal proxy | 6.066803 | 6.067s |
| Old serial total | **2282.173596** | **38m02.174s** |
| #5210 code-import work already removed | -362.940000 | -6m02.940s |
| Inferred current serial residual | **1919.233596** | **31m59.234s** |

The distribution is tail-heavy: 60 gaps of at least 30 seconds sum to
`1916.747298s`, or 83.99% of the old total. The largest gap is `42.989717s`;
the gap p50/p90/p95/p99 values are `0.023248s`, `0.218023s`, `30.251308s`,
and `34.577188s`.

## Scheduler proof

The ideal lower bound is total work divided by worker count. LPT sorts all 896
weights from largest to smallest and assigns each next weight to the least
loaded worker. It is a dynamic-scheduling approximation. Fixed FNV32a assigns
each `acceptance_unit_id` to one stable shard, matching the default-off proof
coordinator's hash shape.

### Old retained workload

| Workers | Ideal lower bound | LPT makespan | LPT worker loads | Saving vs serial |
| ---: | ---: | ---: | --- | ---: |
| 1 | 2282.173596s | 2282.173596s | 2282.173596s | 0s |
| 2 | 1141.086798s | 1141.086823s | 1141.086773 / 1141.086823s | 1141.086773s |
| 4 | 570.543399s | 570.543530s | 570.543530 / 570.543363 / 570.543376 / 570.543327s | 1711.630066s |

### Fixed FNV32a shards

| Workers | Old shard loads | Old makespan | Proportional residual makespan | Conservative saving vs `1919.233596s` |
| ---: | --- | ---: | ---: | ---: |
| 2 | 963.912567 / 1318.261029s | 1318.261029s | 1108.614550s | **600.972567s** |
| 4 | 495.777255 / 503.582151 / 468.135312 / 814.678878s | 814.678878s | 685.118378s | **1104.554718s** |

The inferred post-#5210 ideal lower bounds are `959.616798s` for two workers
and `479.808399s` for four. Fixed hashing leaves a real four-shard imbalance:
`814.678878s` versus the `570.543530s` LPT makespan on the old weights.
Dynamic acceptance-unit claims should use capacity better, but that design
must be proven before it replaces the safe default of one worker.

These are no-contention scheduling ceilings. They do not include extra
Postgres claims, NornicDB contention, retries, heartbeats, or multi-process
ownership. They are appropriate for the cheap go/no-go screen only.

## Real-NornicDB Odù proof

The matrix ran against image `eshu-nornicdb-pr261:149245885258`, labeled with
source commit `1492458852588c884c32f70d27ea2ee07086769c`. Before every matrix,
`graph.EnsureSchemaWithBackendStrict` applied and verified the 289-statement
NornicDB schema. The conflict-bearing identities were protected by
`repository_id` (`Repository.id`), `evidence_artifact_id`
(`EvidenceArtifact.id`), and `environment_name` (`Environment.name`) UNIQUE
constraints; their NornicDB lookup indexes were also present.

The 29-fact Odù exercises eight source-owned acceptance units with fan-in to a
shared target, reciprocal edges, disjoint edges, a shared fixture-unique
`Environment`, duplicate evidence, stale-edge cleanup, a later retract, and
replay of an old generation. Hostile inputs also name the source repository
itself and a non-existent prefix-collision alias; neither may emit an edge. It
runs the actual repo-dependency runner and production graph writer with an
injected 15ms write delay so overlap is observable.

Ten complete 1/2/4-worker matrices passed against ten independently recreated
disposable real NornicDB containers. The timings below are fixture
safety/overlap measurements, not an end-to-end speed prediction.

| Workers | Median base time | Range across 10 runs | Max overlapping writes | Base rows | Final rows | Serial diff |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 166.741ms | 155.362-241.190ms | 1 | 52 | 47 | 0/0 |
| 2 | 140.059ms | 106.184-293.777ms | 2 | 52 | 47 | 0/0 |
| 4 | 114.597ms | 99.524-154.021ms | 4 | 52 | 47 | 0/0 |

| Exactness / lifecycle invariant | Result |
| --- | ---: |
| Serial-to-2-worker full-property graph diff | 0/0 in 10/10 matrices |
| Serial-to-4-worker full-property graph diff | 0/0 in 10/10 matrices |
| Duplicate relationship counts | 0 |
| Stale edges after source-scoped retract | 0 |
| Old-generation replay reopened intents | 0 |
| Active relationships before/after one source retract | 8 / 7 |
| Full owned graph rows before/after retract | 52 / 47 |
| Owned nodes before/after retract | 20 / 19 |
| Owned relationships before/after retract | 32 / 28 |
| Same-source overlap | 1 maximum by acceptance-unit ownership |
| Distinct-source overlap | 2 with two workers; 4 with four workers |
| Fixture-owned repositories/artifacts/environment after each cleanup | 0 / 0 / 0 |
| Same-generation unrelated cleanup sentinel preserved | 1 in 10/10 runs |
| Pre-existing repository/environment/artifact collision | rejected before cleanup or seeding in 10/10 runs |
| Hostile colliding nodes/relationships after rejection | 3 / 2 preserved in 10/10 runs |
| Clean HTTP endpoint paired with dirty Bolt endpoint | rejected; 3 nodes / 2 relationships preserved; proof locks 0 |
| Lock committed but response lost | rejected; owner-scoped cleanup leaves proof locks 0 |

The differential includes every fixture-owned Repository, EvidenceArtifact,
Environment, and outgoing relationship property map, not only expected joined
paths or aggregate counts. It rejects orphan artifacts, unexpected relationship
types, wrong target mappings, duplicates, and extra evidence links. The fixture
also proves self-exclusion, exact prefix matching, deterministic intent
de-duplication, exact source ownership, fan-in, reciprocal edges, and ordered
replay. The runner keeps each source's complete retract-then-write cycle
together and ordered.

Cleanup deletes exact computed fixture artifact IDs and marker-owned nodes; it
never uses a general repo-dependency generation prefix. Before cleanup or
seeding, the harness checks both independently configured HTTP and Bolt views
for pre-existing repositories, environments, and every exact artifact identity
the fixture can touch without mutating the database. Both views must be clean
before the exclusive proof identity can be claimed; the harness repeats the
dual-view identity check after the lock to close the acquisition race. A
hostile mismatch test points HTTP at a separate clean database while Bolt holds
colliding identities, then proves Bolt truth rejects the harness, preserves the
three nodes and two relationships, and creates no proof lock. Another hostile
live test seeds colliding repository,
environment, and artifact identities plus their two relationships, then proves
the harness rejects the database before mutation, creates no proof lock, and
preserves all five records. A second hostile test creates an unrelated artifact with the same
generation and path as the fixture, then proves the fixture artifact is removed
while the unrelated sentinel survives in 10/10 runs.

The live executor shares a driver bookmark manager across write and read
sessions and requires two consecutive clean HTTP reads before starting the next
cell. Pinned NornicDB does not safely support unbounded delete/recreate cycles
for the same uniquely indexed identities: a single long-lived-database
`-count=10` control eventually exposed stale artifact visibility and a false
`Environment.name` uniqueness conflict. Therefore each accepted repeated
matrix uses a newly recreated disposable container. The fail-closed identity
guard prevents accidental reuse; the retained evidence stack is never a valid
target for this fixture harness.

## Conflict and retry proof

Raw concurrent graph groups are not safe by themselves. In one observed
hostile-control run, one worker completed the 32-row canonical baseline, while
two raw workers reproduced a commit-time UNIQUE conflict on the shared
`Environment`. This observation motivates the deterministic conflict gate
below; it is not itself presented as a repeatable gate or a passing production
design.

The existing bounded `RetryingExecutor.ExecuteGroup` owns the complete
MERGE-shaped group. With that layer, both two- and four-worker matrices passed
10/10 with serial graph diff `0/0`. The positive matrix recorded no stale
edges, duplicates, retry exhaustion, deadlocks, or timeouts. A deterministic
unit proof separately drives two writers into the same commit-time conflict:
the bare group loses exactly one contribution, while the retrying group
creates one canonical node and retains both contributions.

| Write shape | Workers | Repetitions | Outcome |
| --- | ---: | ---: | --- |
| Observed raw graph group | 1 | 1 | pass; 32 canonical rows |
| Observed raw graph group | 2 | 1 | commit-time UNIQUE conflict |
| Grouped bounded retry | 2 | 10 | 10/10 pass; diff 0/0 |
| Grouped bounded retry | 4 | 10 | 10/10 pass; diff 0/0 |

The retry proof preserves concurrency; it does not reduce worker count or
serialize distinct acceptance units. Retry remains bounded and
operator-visible through bounded `reason` values (`connectivity_error`,
`transient_error`, `write_conflict`, and `commit_unique_conflict`). A driver
`ConnectivityError` wrapping `CommitFailedDeadError` is not retried in place by
`RetryingExecutor` because the transaction outcome is unknown. The still-pending
repo-dependency acceptance unit may be replayed later after backoff; its
source-scoped retract and deterministic MERGE upserts are idempotent. A runner
state-model test covers both direct upsert and retract-then-rewrite after a
commit lands but its response is lost, proving one final relationship, one
deterministic artifact, and one completion. A malformed nil-cause connectivity
error becomes a safe terminal error without panicking.

The production reducer composition was also exercised against the pinned live
backend under `ifafaultinjection`: the exact
`neo4jSessionRunner -> reducerNeo4jExecutor -> InstrumentedExecutor ->
FaultingExecutor` chain deterministically fired one group fault below the
persistent retry seam. Across 10/10 runs, the fault-fired assertion passed,
the retry counter was exactly `1`, and both MERGE statements landed exactly
once. This is the non-vacuity proof that grouped writes no longer bypass the
production retry seam; the 1/2/4 Odù matrix separately proves worker overlap.

## Primary-exit contribution caveat

The measured repo-dependency tail began at `19:53:37.903985` and the primary
bootstrap process exited at `19:55:21.095`, an overlap of about `103.19s`.
Most of the 38-minute stable-drain lane therefore occurred outside that
primary exit boundary.

Consequently:

- the lane clearly passes the `158s` stable-drain worthwhile-work threshold;
- the current evidence does **not** show a `158s` reduction in primary
  bootstrap exit;
- it does **not** establish the remaining under-20-minute path;
- a same-state built-binary run must measure both primary exit and full drain.

## Why task 777 / #5088 is not this proof

Task 777 is retained evidence for the PostgreSQL relationship-backfill query:
`332.27s`, 16,361 facts, and the non-repository alias `LIKE` arm. It has no
acceptance-unit mapping and is not the `repo_dependency` graph writer lane.

#5088 remains a closed no-go: the accepted relationship-backfill contribution
was `113.755s`, below the then-current `158s` gap, and the narrowed candidate
was not both exact and faster. Those numbers must not be added to the scheduler
savings above or presented as support for this concurrency candidate.

## Commands

Operator-local endpoints, credentials, and retained-run locations are replaced
with environment variables.

```bash
# Public issue state.
gh issue view 5122 --repo eshu-hq/eshu \
  --json number,title,state,body,comments,updatedAt,url
gh issue view 5088 --repo eshu-hq/eshu \
  --json number,title,state,body,comments,updatedAt,url

# Retained cardinality.
psql "$POSTGRES_DSN" -c "
SELECT payload->>'evidence_source', count(*),
       count(DISTINCT repository_id),
       count(*) FILTER (WHERE completed_at IS NOT NULL)
FROM shared_projection_intents
WHERE projection_domain='repo_dependency'
GROUP BY 1 ORDER BY 1;"

# Stable-drain source weights.
psql "$POSTGRES_DSN" -c "
WITH cycles AS (
  SELECT repository_id, max(completed_at) AS started_at
  FROM shared_projection_intents
  WHERE projection_domain='repo_dependency'
    AND completed_at IS NOT NULL
  GROUP BY repository_id
), ordered AS (
  SELECT *, lead(started_at) OVER
    (ORDER BY started_at, repository_id) AS next_started_at
  FROM cycles
), terminal AS (
  SELECT max(updated_at) AS terminal_at FROM fact_work_items
)
SELECT repository_id,
       extract(epoch FROM coalesce(next_started_at, terminal_at)-started_at)
         AS weight_seconds
FROM ordered CROSS JOIN terminal
ORDER BY repository_id;"

# Scheduling calculation: sort weights descending for LPT. Fixed shards use
# FNV32a(acceptance_unit_id) modulo worker count.
ruby "$SCHEDULER_PROOF" "$RETAINED_WEIGHTS"

# Deterministic conflict/no-retry control and grouped-retry convergence.
cd go
go test -race ./internal/storage/cypher -run \
  '^(TestRetryingExecutorConvergesUnderConcurrentMergeConflict|TestBareGroupExecutorLosesConcurrentMergeWriteWithoutRetry)$' \
  -count=10

# Default-off runner seam and production-resolver Odù extraction.
go test -tags ifarepodependencyproof -race ./internal/reducer -run \
  '^(TestIfaRepoDependencyProofWorkersOverlapDistinctAcceptanceUnits|TestIfaRepoDependencyProofWorkersKeepWholeAcceptanceUnitTogether|TestRepoDependencyConcurrencyOduProducesHostileProductionIntents)$' \
  -count=10

# Deterministic Odù catalog, payload, correlation, and edge shape.
go test -race ./internal/ifa -run \
  '^TestRepoDependencyConcurrencyOduProductionEvidence$' -count=10

# Real NornicDB isolation guard, cleanup boundary, and Odù matrix. Run this
# command ten times, recreating the disposable container before every
# invocation. Do not use -count=10 against one long-lived NornicDB database.
ESHU_REPLAY_TIER_LIVE=1 \
ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE=1 \
ESHU_REPO_DEPENDENCY_PROOF_HTTP_URL="$NORNICDB_HTTP_BASE_URL" \
ESHU_REPO_DEPENDENCY_PROOF_MISMATCH_HTTP_URL="$SEPARATE_CLEAN_NORNICDB_HTTP_BASE_URL" \
ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_DATABASE="$NORNICDB_DATABASE" \
NEO4J_URI="$NORNICDB_BOLT_DSN" \
NEO4J_USERNAME="$NORNICDB_USERNAME" \
NEO4J_PASSWORD="$NORNICDB_PASSWORD" \
go test -tags ifarepodependencyproof ./internal/replay/offlinetier \
  -run 'TestRepoDependencyIfa(ProofRejectsNonDisposableDatabase|ProofRejectsMismatchedHTTPBackend|ProofCleansAmbiguousCommittedLock|CleanupPreservesUnrelatedArtifact|ConcurrencyLive)$' \
  -count=1 -v

# Production grouped-retry seam, deterministic retry-fired proof on the same
# pinned live backend.
ESHU_REPO_DEPENDENCY_CONCURRENCY_PROVE_LIVE=1 \
ESHU_GRAPH_BACKEND=nornicdb \
ESHU_NEO4J_DATABASE="$NORNICDB_DATABASE" \
NEO4J_URI="$NORNICDB_BOLT_DSN" \
NEO4J_USERNAME="$NORNICDB_USERNAME" \
NEO4J_PASSWORD="$NORNICDB_PASSWORD" \
go test -tags ifafaultinjection ./cmd/reducer \
  -run '^TestReducerGroupedRetrySeamLiveNornicDB$' -count=10 -v
```

The FNV32a calculation used the standard offset and prime:

```ruby
hash = 2_166_136_261
acceptance_unit_id.each_byte do |byte|
  hash ^= byte
  hash = (hash * 16_777_619) & 0xffff_ffff
end
shard = hash % shard_count
```

## Recommendation and next gate

Merge the Odù, grouped-write retry proof, and default-off runner proof as one
accuracy-and-concurrency foundation. Do not enable production workers in this
change and do not describe the fixture speedup or retained scheduler ceiling
as an achieved end-to-end reduction.

The production route should use dynamic, database-backed leases keyed by
`(projection_domain, acceptance_unit_id)`, with unique process/worker owners,
lease epochs, heartbeats, and fenced completion. Fixed hashes are acceptable
for the proof coordinator but leave material straggler imbalance and do not
provide multi-process ownership.

Before raising the default above one, prove on identical retained data and
storage state:

1. the candidate-discovery SQL and index with `EXPLAIN (ANALYZE, BUFFERS)`;
2. one winner per acceptance unit, parallel claims for distinct units,
   expiry takeover, heartbeat loss, and stale-epoch completion rejection;
3. built binaries with real Postgres and pinned NornicDB at 1/2/4 workers;
4. graph/API truth, queues drained, same-AU overlap 1, and graph diff 0/0;
5. primary-exit and full-drain before/after seconds from the same boundaries.

Only that built-binary result can justify a production worker default and an
under-20-minute claim.
