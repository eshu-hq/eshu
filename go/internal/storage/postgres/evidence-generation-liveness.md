# Generation Liveness Shared-Resolver Ownership Evidence

This note covers the #4207/#5122 predicate that decides whether an aged active
generation has a source-local wedge worth replaying. Exact cross-repository
`repo_dependency` work is owned by the shared resolver. Reopening
`source_local` cannot advance that queue, even after backward evidence commits.

## Retained full-corpus failure

A retained 896-repository run preserved the production concurrency profile:
parse 16, snapshot 16, projection 8, reducer 16, shared 4, graph in-flight 8,
and source-local projector 2. Initial bootstrap and graph projection completed
without uniqueness conflicts, graph-write timeouts, restarts, OOMs, or failed
generations.

The controller declared terminal at 1,588 seconds because it checked only
`fact_work_items`. The 30-minute generation-liveness sweep later reopened 790
succeeded `source_local` rows. All 790 had matching
`backward_evidence_committed`; their remaining shared work was only exact
`repo_dependency:<scope>` intents. Each sampled replay timed out in
`canonical_retract` near 120 seconds, with zero successful recoveries.

The shared resolver then reached 2,414 completed and zero outstanding intents
and stayed there for three samples. At that earlier sample, all 790 reopened
source-local rows still remained open: 761 pending, 27 retrying, and 2 in
flight. This separates the obsolete source-local replay from the shared
resolver's own completion path.

At a fixed retained-run cutoff, 126 recovery attempts had completed with zero
successes: 63 per projector worker. Their durations totaled 15,163.667
worker-seconds (mean 120.347 seconds, median 120.120 seconds, p95 120.311
seconds, maximum 130.320 seconds). Another 662 rows were untouched pending,
one was claimed, and one was running.

| Contribution estimate | Recoverable wall time at 2 workers |
|---|---:|
| Hard observed floor: 126 completed failures | 7,581.833 s (2h 6m 21.833s) |
| Realistic first-pass estimate: 790 rows at measured median-to-mean | 47,447.372-47,536.891 s (about 13h 11m-13h 12m) |
| Count-bounded two-attempt configured timeout budget | 94,800 s (26h 20m) |
| Two-attempt estimate at measured mean | 95,073.782 s (26h 24m 33.782s) |
| Current target gap | 158 s (2m 38s) |

The hard observed floor alone exceeds the 158-second gap by 7,423.833 seconds
(2h 3m 43.833s). The two-attempt budget is count-bounded: the retained rows
started at attempt one, the source retry ceiling is three, and each can consume
at most two recovery claims. Applying the single observed maximum duration to
every claim gives a 102,952.474-second empirical high envelope, not a formal
runtime ceiling.

These replays began after the harness's false 1,588-second terminal. Preventing
them cannot be credited as reducing that already-reported milestone or the
#5088 relationship-backfill stage. It removes hours from the corrected stable
terminal lifecycle and is necessary before the next comparable full-corpus run
can state an accurate terminal time. The rejected #5088 relationship-query
candidate remains a separate no-go.

## Old and new predicate

The shipped pending-intent arm allowed exact `repo_dependency[:scope]` work to
trigger source-local recovery after backward evidence:

```sql
AND (
    intent.projection_domain <> 'repo_dependency'
    OR (
        intent.source_run_id <> 'repo_dependency'
        AND intent.source_run_id NOT LIKE 'repo_dependency:%'
    )
    OR EXISTS (matching backward_evidence_committed phase)
)
```

The replacement excludes only the exact shared-resolver family:

```sql
AND NOT (
    intent.projection_domain = 'repo_dependency'
    AND (
        intent.source_run_id = 'repo_dependency'
        OR starts_with(intent.source_run_id, 'repo_dependency:')
    )
)
```

Other domains and lookalike source runs such as
`code_import_repo_dependency:<scope>` remain actionable. Recovery and
`CountActiveGenerationsByAge` use the same shared-resolver predicate so their
ownership classification does not diverge.

## Local PostgreSQL proof

The cheapest proof ran the shipped, rejected unescaped-`LIKE`, and final
queries against PostgreSQL 16 with ten aged active generations, ten pending
intents, ten source-local rows, one backward-evidence phase, and one live
shared partition lease. PostgreSQL `LIKE` treats `_` as a one-character
wildcard, so the intermediate `LIKE 'repo_dependency:%'` candidate was wrong:
it also matched both `repoXdependency:<scope>` and
`repo-dependency:<scope>`.

| Check | Shipped | Rejected `LIKE` | Final `starts_with` |
|---|---:|---:|---:|
| Selected rows | 5 | 4 | 6 |
| Exact shared-resolver progress row | selected | excluded | excluded |
| `repoXdependency:` collision | excluded | excluded | selected |
| `repo-dependency:` collision | excluded | excluded | selected |
| Blocked non-repo domain | selected | selected | selected |
| Expired source-local lease | selected | selected | selected |
| Live source-local lease | excluded | excluded | excluded |
| Unready exact repo dependency | excluded | excluded | excluded |
| Pending recovery | excluded | excluded | excluded |

The final-versus-shipped intended delta removes the one exact
`repo_dependency:<scope>` false replay and restores the two same-domain
wildcard-collision rows. After subtracting those three intended corrections,
the unexpected bidirectional diff is `0/0`. The rejected unescaped candidate
has a final-only diff of two collision rows and is not acceptable. A
`code_import_repo_dependency:<scope>` source run and an exact-looking source
run in another projection domain also remain actionable.

A warmed three-way `EXPLAIN (ANALYZE, BUFFERS)` comparison on the same ten-row
fixture measured 0.130 ms and 32 top-level shared hits for shipped, 0.085 ms
and 38 hits for the rejected `LIKE`, and 0.069 ms and 50 hits for final
`starts_with`. Treat these sub-millisecond differences as noise: the value is
avoiding wrong 120-second replays, not making the sweep itself faster. The
final shape has the largest hit count because it correctly retains both
collision rows.

The real-Postgres TDD proof first failed on the exact shared-resolver row, then
failed again when hostile review added the underscore-wildcard collision. It
passed with the exact `starts_with` predicate and both one-character wildcard
collisions. Ten repeated integration runs prove exact shared-resolver exclusion,
lookalike and other-domain preservation, live/expired lease behavior, and
shared-resolver stuck-age classification parity. The concurrent heartbeat test
also passed repeatedly: a committed lease renewal remained intact and the
sweep recovered zero rows.

## Combined local runtime proof

The final branch code was built with the merged JSON payload correction and
run against the exact-key NornicDB commit-lock candidate in a fresh isolated
Compose project. The retained literal-escape fixture used the production
concurrency profile: parse 16, snapshot 16, projection 8, reducer 16, shared 4,
graph in-flight 8, and source-local projector 2. No worker or writer lane was
serialized.

| Surface | Result |
|---|---:|
| Bootstrap collection | 0.172 s |
| Relationship backfill | 0.011 s |
| Bootstrap pipeline | 1.029 s |
| Projector/reducer queue | 15/15 succeeded; 0 outstanding |
| Shared intents | 159 completed; 0 pending |
| Fact identity | 864 rows; 864 distinct fact IDs |
| Active file payloads | 5; 0 empty objects |
| Preserved literal `\\u0000` tokens | 10 across all 5 expected files |
| Graph | 122 nodes; 280 edges |
| UID identity | 109 UID nodes; 109 distinct UIDs |
| Function UID identity | 89 functions; 89 distinct UIDs; 0 null UIDs |
| API readback | healthy; 1 repository; queue 15/15 succeeded |
| MCP readback | 158 tools; `get_index_status` healthy; same queue counts |
| Runtime failures | 0 restarts; 0 OOM kills; 0 uniqueness, graph-timeout, panic, fatal, or dead-letter signals |

The last fact-queue item was the scheduled search-document sweep, which
completed about 30.27 seconds after bootstrap start; vector finalization
completed on the next scheduled sweep at about 60.05 seconds. The shared
`repo_dependency` cycle itself completed about 1.48 seconds after start. These
one-repository timings prove the combined runtime path and expose the scheduled
sweep floor, but they are not a substitute for a comparable 896-repository
performance rerun.

The exact final-code Compose project remains live for inspection. After the
60-second scheduled-sweep floor, all 15 fact-work rows were succeeded, all 159
shared intents were complete, and zero work rows were open. A preceding local
stack containing the same exact `repo_dependency:<scope>` family remained
stable through 39 minutes: its succeeded source-local projector row was not
reopened after the 30-minute liveness window. The direct PostgreSQL integration
proof above executes the final corrected predicate and its concurrent lease
race, including both `LIKE` wildcard collisions that the preceding stack did
not contain.

Commands:

```bash
cd go
ESHU_GENERATION_LIVENESS_PROOF_DSN='postgres://.../proof?sslmode=disable' \
  go test ./internal/storage/postgres \
  -run '^(TestGenerationLivenessIntegration|TestGenerationLivenessRepoDependencyOwnership|TestRecoverWedgedActiveGenerationsQueryDoesNotClobberConcurrentlyRenewedLease)$' \
  -count=10
ESHU_GENERATION_LIVENESS_PROOF_DSN='postgres://.../proof?sslmode=disable' \
  go test -race ./internal/storage/postgres \
  -run '^(TestGenerationLivenessIntegration|TestGenerationLivenessRepoDependencyOwnership|TestRecoverWedgedActiveGenerationsQueryDoesNotClobberConcurrentlyRenewedLease)$' \
  -count=3
go test ./internal/storage/postgres \
  -run 'Liveness|RecoverWedgedActiveGenerations|CountActiveGenerationsByAge' \
  -count=1
```

No worker count, partition count, batch size, timeout, lease duration, queue
domain, transaction boundary, or lock behavior changes. Existing liveness logs,
stuck/aging gauges, source-local queue counts, shared-intent backlog, phase
state, and failure-class counts remain the operator surfaces.
