# #7547 cross-repo dead-code consumer coverage read

## Scope

`POST /api/v0/code/dead-code/cross-repo` (MCP `find_cross_repo_dead_code`)
classified a producer symbol `dead` whenever no active-generation consumer row
existed in `code_reachability_rows`. `Available` meant only "the store is present
and the grant plan is ok". A consumer repository whose active snapshot is
missing, truncated, from a delta generation, or built from zero roots writes no
rows for the symbols it calls, so every producer symbol it calls read `dead`.

This change makes the reader consult `code_reachability_repository_watermarks`.
The writer side (stamping `truncated` for zero roots and depth cutoffs,
#7570), the epoch-4 bump (#7576) and the loader's complete-run gate (#7579) are
separate changes, all merged on the base of this branch; none is touched here.
`CodeReachabilityVerdictSchemaEpoch` is 4 in this tree.

## Behavior

- A repository scope with an active generation is a gap when it has no watermark
  for that generation, or the watermark is `truncated`, or its
  `verdict_schema_epoch` is below `reachabilitystore.CodeReachabilityVerdictSchemaEpoch`
  (bound as a parameter, never copied), AND that generation has a
  `shared_projection_intents` row in the `code_calls` or `inheritance_edges`
  domain (completed or pending), reached through `shared_projection_acceptance`.
  A repository with no such intent cannot be a consumer (docs, IaC) and is
  complete. A zero-root repository with edge intents is NOT excluded: its edges
  can sit on a chain from a rooted repository to the producer symbol. On a full
  generation a refresh intent is not an edge and does not count; on a delta
  generation any such intent counts (see "Refresh intents (#7591)").
- `dead` needs no gap among the consumers the answer is judged against.
  Otherwise a candidate with no strong live consumer evidence is
  `unknown_needs_evidence` with the new reason `consumer_coverage_incomplete`.
  Strong consumer evidence still wins, as it does over a hidden consumer.
- Which consumers: the request's named `consumer_repo_ids` (a named repository
  with no active repository scope is a gap), else the caller's grant, else every
  repository scope with an active generation.
- A content store that cannot answer, or a request that cannot be planned, keeps
  `cross_repo_evidence_unavailable`. A coverage read error fails the request.
- The response gains `consumer_coverage {complete, retryable, incomplete,
  incomplete_repo_ids, incomplete_truncated}`. It carries no count of repositories
  checked: that would force a full scan, and the statements stop at the first
  gaps. `incomplete` says per repository why it is a gap (see Per-repository
  state); `incomplete_repo_ids` is the same list, kept for callers that read it
  first.

## Per-repository state

Three situations used to collapse into one answer, so a caller could not tell
whether a snapshot is expected. Each gap now carries `state`, `generation_id` and
`retryable`. `retryable` is a hint, not a promise: it means a snapshot is expected
to appear or refresh without action, and it can stay true for a long time when
the active generation is a delta generation or a full generation whose reducer
work did not complete (see the drain section below). It never changes the
classification, which stays `unknown_needs_evidence`:

| `state` | Condition | `retryable` |
| --- | --- | --- |
| `no_snapshot_yet` | no watermark for the active generation | true |
| `truncated` | watermark present, `truncated` | false |
| `older_epoch` | watermark present, not truncated, epoch below the current one | true |
| `no_active_scope` | a named repository with no active repository scope | false |

Precedence inside one watermark: missing, then truncated, then older epoch. A
watermark that is both truncated and older-epoch reports `truncated`. The
alternative, older epoch first, would report `retryable: true` and hint at a
rebuild that may land on `truncated` again; the cost of the chosen order is that
a stale truncated bit written under old semantics reads as final until a rebuild
re-stamps it. Both are named so the order can be revisited.

`generation_id` is the scope's `active_generation_id`, the snapshot expected;
`no_active_scope` has none and the key is omitted. A repository with several
gap scopes yields one entry: a truncated scope first (so `retryable` never
hints at a refresh another scope defeats), then the lowest generation id. The named
statement does this with `DISTINCT ON (repository_id) ... ORDER BY repository_id,
(state = 'truncated') DESC, generation_id`; the all-repositories statement has no
`ORDER BY` (its `LIMIT` must keep stopping the scan early), so
`ContentReader.CrossRepoDeadCodeConsumerCoverage` applies the same rule to its
rows. The top-level `retryable` is true only when every listed gap is retryable
and `incomplete_truncated` is false: a cut list hides gaps that may not be
retryable.

Two limits of the multi-scope pick, neither changed. `ORDER BY` in the named
statement uses the database collation, while `Outranks` and the final Go sort
compare bytes, so "lowest generation" and the sort order can differ under a
non-C collation between the named and all-repositories paths; each is
deterministic. And when the all-repositories list is cut at its `LIMIT`, one
entry's state and `retryable` for a multi-scope repository can be approximate
(the other scope's row may not have been read), and a repository returned twice
for two scopes can reach the row limit and set `incomplete_truncated` with
nothing cut. The top-level `retryable` stays false in both.

Both statements return the two extra columns from the join and CASE they already
had (`scope.active_generation_id`, and a `CASE` over the same watermark row). No
index, table or parameter was added; the gating `CASE` that keeps the intent
probe correlated is unchanged.

Proof: `TestCrossRepoDeadCodeConsumerCoverageLive` seeds `r-multi2` (its `-a`
scope has the lower generation id and no watermark, its `-b` scope is truncated)
and `r-trunc-old` (one watermark both truncated and one epoch behind) and asserts
repository, state, generation id and retryable for the named, grant and
all-repositories modes. Three seeded mutations each went RED on the live test and
the shipped statements pass: testing `older_epoch` before `truncated` (fails
`r-trunc-old` in all three modes); dropping the `(state = 'truncated') DESC` term
(fails `r-multi2` in the named and grant modes); and picking by lowest generation
only in Go (fails `r-multi2` in the all-repositories mode).

### Plan, old statement against new

Fixture-scale, a throwaway `postgres:18-alpine` container, hot cache, NOT the QA
replica: 3,000 repository scopes (three generations each, the last active),
20,000 other scopes, 396,000 intents (2,400 repositories with code intents on
every generation, noise for all), 2,850 watermarks at epoch 4 of which every 19th
is truncated. Custom plans, three runs per case, `EXPLAIN (ANALYZE, BUFFERS,
TIMING OFF)`, execution times in ms (old, then new, same fixture):

| Statement | Case | Old | New |
| --- | --- | --- | --- |
| named | 20 ids | 0.64, 0.38, 0.36 | 0.67, 0.43, 0.41 |
| named | 500 ids | 2.9, 2.9, 2.8 | 3.1, 2.7, 2.7 |
| named | 3,000 ids (grant), all probes pass at epoch 4 | 7.9, 7.0, 6.8 | 7.8, 7.2, 7.2 |
| named | 3,000 ids, epoch bound to 5 (every watermark behind) | 36.6, 34.4, 35.1 | 35.7, 34.2, 34.1 |
| all | epoch 4, full pass | 2.3, 2.1, 2.0 | 2.2, 2.0, 1.9 |
| all | epoch 5, stops at LIMIT 26 | 2.0, 1.9, 1.8 | 1.9, 1.9, 1.8 |
| all | epoch 5, cap 100,000 (every gap probed) | 30.4, 30.7, 30.4 | 30.0, 30.7, 32.1 |

The timings are within noise. The plan class is the same: the scope scan, the
watermark primary-key probe and the acceptance and intents lookup are unchanged.
The one difference is the named statement's last node: `HashAggregate` then
`Sort` became `Sort` (repository, truncated first, generation) then `Unique`,
over the same 256 estimated rows and a handful of real ones; the sort key is
three narrow columns. The all statement's plan is identical apart from two more
output columns.

QA replica, read-only, PostgreSQL 18.3, `EXPLAIN` without `ANALYZE` (nothing was
executed), `statement_timeout` 10 s, read-only transaction mode, replay lag
before 0.062 s and after 0.078 s: the old and new named plans both show the
sequential scan of `ingestion_scopes`, a primary-key probe of
`scope_generations`, the watermark primary key and the acceptance primary key
plus `shared_projection_intents_acceptance_lookup_idx`; the new named plan swaps
`Unique` over `Sort` on `source_key` for `Unique` over `Sort` on the three-column
key, with a cost of 10.54 to 10.59 against 10.29 to 10.34. The all plans are the
same shape. No `ANALYZE` was run on the replica for this change, so execution
timings there are NOT_CHECKED; the figures above are fixture-scale.

## Statement shape

One statement per request, never per candidate; skipped when there is no
candidate to classify.

| Request | Statement | Parameters |
| --- | --- | --- |
| named consumers, or the caller's grant | `deadcode.CrossRepoDeadCodeNamedConsumerCoverageQuery` | ids `text[]`, require-active-scope, verdict epoch, row cap + 1 |
| unscoped, none named | `deadcode.CrossRepoDeadCodeAllConsumerCoverageQuery` | verdict epoch, row cap + 1 |

The intent probe is a scalar `(SELECT true ... LIMIT 1)` inside a `CASE` that
runs only for a scope that already failed the watermark test.

## Proof

Theory proved before the work, on a throwaway PostgreSQL 18 container
(`postgres:18-alpine`, local, hot cache, generated fixture; NOT the QA
replica, so every timing below is fixture-scale):

1. Semantics, on an 11-scope fixture and again on the full bootstrap schema
   (`TestCrossRepoDeadCodeConsumerCoverageLive`): a repository with a missing
   watermark and completed intents is a gap; a truncated watermark with intents
   (zero-root) is a gap; a pending-only repository with no watermark is a gap; a
   repository with two scopes is a gap when either is; a repository with no
   intent, only intents in another domain, or intents only on a superseded
   generation is complete; a named repository with no active scope is a gap only
   when require-active-scope is set. Mutating the statement's domain list made
   the live test fail (seeded RED), and the unmutated statement passes.
2. Plan, found by `EXPLAIN (ANALYZE, BUFFERS)` and fixed before this commit:
   - the probe written as `EXISTS` was hoisted into a hashed subplan over every
     `code_calls`/`inheritance_edges` intent in the database; the scalar `LIMIT 1`
     form is a correlated subplan using the `shared_projection_acceptance` primary
     key prefix and `shared_projection_intents_acceptance_lookup_idx`;
   - joining the requested list to the scopes row by row planned as a nested loop
     over list x scopes: 73.2 ms for 500 ids and 519.6 ms for 3,000 (1.5 M and 9 M
     join-filter comparisons). The shipped shape finds the scopes in one pass with
     a hashed `source_key = ANY($1)` test.

Fixture: 3,010 repository scopes (5 generations each, last active), 20,000
non-repository scopes, 15,000 acceptance rows, 780,008 intents (2,400 repositories
with `code_calls`/`inheritance_edges` intents on every generation, noise intents in
other domains for all), 2,854 watermarks.

| Statement | Case | Execution |
| --- | --- | --- |
| all | fewer than 26 gaps (4), so a full pass over 3,010 scopes (158 probes) | 5.0 ms |
| all | gaps present, stops at LIMIT 26 | 3.3 ms |
| named | 500 ids, require-active-scope, 40 gaps | 3.6 ms |
| named | 3,000 ids (grant), 40 gaps | 6.1 ms |
| named | 3,000 ids, all probes pass | 7.5 ms |
| named | 1 id | 0.4 ms |

Epoch predicate (`watermark.verdict_schema_epoch < $epoch` inside the same CASE as
the missing and truncated tests, so the intent probe still gates it), re-measured
on a fresh fixture of 3,000 repository scopes, 20,000 other scopes, 2,850
watermarks at epoch 3 and the same intents, with the epoch bound to 3:

| Statement | Case | Execution |
| --- | --- | --- |
| all | no gaps, full pass over 3,000 scopes | 4.4 ms |
| all | epoch bound to 4, every watermark older, stops at LIMIT 26 | 2.0 ms |
| named | 500 ids | 2.5 ms |
| named | 3,000 ids | 6.1 ms |
| named | 1 id | 0.4 ms |
| named | 3,000 ids, epoch bound to 4 (2,400 gaps, each with a probe) | 33.6 ms |

The plan class is unchanged; the epoch test is a filter on the watermark row the
primary-key probe already reads. Semantics: `TestCrossRepoDeadCodeConsumerCoverageLive`
adds `r-oldepoch` (intents, `truncated = false`, epoch one below the constant: a
gap) and `r-oldepoch-docs` (same watermark, no intents: complete); replacing the
epoch test with a never-true condition made the live test fail on `r-oldepoch`
(seeded RED), the shipped statement passes. The epoch is imported from
`reachabilitystore.CodeReachabilityVerdictSchemaEpoch` and bound as a parameter
(`$3` named, `$1` all); the reader test asserts the bound value is that constant.

The predicate reads the constant as compiled, so it flags nothing extra while
every watermark is at the current epoch. The epoch-4 bump has merged: every
watermark stamped below 4 on a repository with code intents reads as a gap
(`older_epoch`, retryable) until the loader rebuilds it, which the loader does
for any watermark below the current epoch. Pre-upgrade watermarks (epoch default
0) are gaps for the same reason until rebuilt.

When `older_epoch` is gone on ops-qa: the drain is done when the split census in
`7547-reachability-loader-gate.md` (section "Interaction with the epoch-4
drain") reports `residual` equal to `gated_out`. A residual above `gated_out`
is a drain still running, and those repositories answer `older_epoch` with
`retryable: true`.

`retryable` is a hint, and the merged loader gate (#7579) is why. A run the gate
rejects never gets a watermark for its active generation until a later full
generation: a delta generation activated after deploy, and a full generation
whose materialization work item failed or is pending. The coverage join keys on
the active generation, so such a repository answers `no_snapshot_yet` with
`retryable: true` for as long as that generation stays active. `older_epoch` has
the same limit when a gated-out run left an older watermark and its generation is
still active. In both cases the answer stays `unknown_needs_evidence`, and
`retryable` cannot see the loader's gate: it follows the watermark.

On the QA replica the scope side is a cheap sequential scan of the 819-row `ingestion_scopes` table on the custom plan (the generic plan, from the sixth prepared execution on, used the partial `ingestion_scopes_active_generation_idx`; both are fast, and 799 of the 819 scopes qualify, so the index buys little); the earlier fixture-scale expectation that the partial index serves the scope side was wrong at this table size. The watermark side is a primary-key probe, and the intent probe is a correlated prefix probe. Neither statement reads `code_reachability_rows`.

Performance Evidence: QA replica, read-only, PostgreSQL 18.3, one data copy, three runs per case in one session, measured by the #7547 measurement agent (verified, not fixture). These timings are for the statements as first shipped (`SELECT DISTINCT repository_id`, no state `CASE`, no `DISTINCT ON`); the per-repository-state statements were only `EXPLAIN`ed on QA, without `ANALYZE` (see "Plan, old statement against new"). Execution times: named statement, 20 ids, 0.4 to 0.9 ms at epoch 3 and 0.95 to 1.9 ms with every watermark one epoch behind; named, 3,000 ids (799 real keys plus 2,201 absent), 23 to 42 ms at epoch 3 and 43 to 61 ms one epoch behind; all-repositories statement, 7.3 to 7.8 ms for a full pass with no gaps, 1.2 to 1.4 ms when it stops at its limit with every watermark behind, and 37 ms when every scope runs the intent probe. Shared-buffer hits stay in the tens of thousands at most (22,388 for the 3,000-id case one epoch behind, zero reads once warm). Plan facts: the named statement is one pass (a single scan of `ingestion_scopes` with `source_key = ANY($1)` and a hashed NOT IN, no list-by-scopes nested loop); the intent probe uses `shared_projection_acceptance_pkey` and then `shared_projection_intents_acceptance_lookup_idx`, and shows as never executed when there are no gaps. Identity check: 799 active repository scopes; 796 of the 799 active repository scopes have a watermark for their active generation, matched on `repository_id = source_key` (one query, not two independent counts); the 3 scopes without a watermark have no `code_calls` or `inheritance_edges` intents, so they are not gaps; all 796 watermarks are at epoch 3 with `truncated = false`, so at epoch 3 the coverage check finds 0 gaps. The all-repositories one-epoch-behind full-probe cases ran on a generic plan (seventh to ninth executions of a prepared statement); the driver may choose differently. Fixture-scale figures earlier in this note stay labeled fixture-scale. A repository with many retained historical intents and no intent on its active generation scans those intents once per request before it is found complete; retention bounds it.

No-Regression Evidence: the change adds one bounded statement per classified
request and touches no existing statement, index, or writer.
`TestCrossRepoDeadCodeConsumerCoverageStatementShape` and
`TestCrossRepoDeadCodeConsumerCoverageUniversePredicate` pin the statement shapes
above, including that the two statements carry the same probe; the one-call-per-
request rule is pinned by
`TestCrossRepoDeadCodeIncompleteNamedConsumerCoverageIsNotDead`.

Observability Evidence: the read runs under a `postgres.query` span with
`db.operation=cross_repo_dead_code_consumer_coverage`,
`db.sql.table=code_reachability_repository_watermarks`,
`db.coverage.all_repositories`, `db.coverage.requested_repositories`,
`db.rows.incomplete_consumer_repositories` and `db.coverage.truncated`. An
operator finding a surprising `unknown_needs_evidence` reads the response's
`consumer_coverage.incomplete` (state, generation id, retryable) and the span,
which also carries `db.coverage.retryable`.

## Refresh intents (#7591)

A repository's reducer run writes one refresh intent
(`{"action":"refresh","intent_type":"repo_refresh"}`, the stored generated column
`shared_projection_intents.is_refresh_intent`) beside its per-edge intents, in
one `UpsertIntents` call. A refresh intent has no `caller_entity_id` or
`child_entity_id`, and the reachability loader's edge read
(`listCodeReachabilityEdgesSQL`) never loads it. A repository whose only
`code_calls` or `inheritance_edges` intents are refreshes therefore has no
edges and cannot be a consumer, yet the first probe counted it, so it answered a
`truncated` gap forever (zero roots stamps `truncated`).

Both statements' intent probe now adds
`AND (generation.is_delta OR NOT intent.is_refresh_intent)` (the named statement
carries `is_delta` through its `matched` CTE):

- Full generation: only per-edge intents (`action` other than `refresh`) make
  the repository a consumer. A legacy row with no `action` key has
  `is_refresh_intent = false` and counts as an edge.
- Delta generation: the intents describe only the changed files, the loader's
  run gate never writes a watermark for the generation, and consumer rows are
  read only for the active generation, so any code intent keeps the gap. A bare
  `NOT is_refresh_intent` would hide a refresh-only delta repository that is a
  real `no_snapshot_yet`; the delta case is pinned for that reason.
- A repository with no per-edge intent on a full generation is complete without
  a watermark, the same as a docs or IaC repository. A generation with no
  intent yet was already complete (queue-time window below). A repository whose
  only intents are `inheritance_edges` refreshes is complete on a full
  generation.
- Queue-time window (pre-existing, widened by #7591). From generation activation
  (projector Ack) until the reducer writes the repository's per-edge intents,
  both statements read the repository complete, and a cross-repo answer can say
  `dead` for a symbol it calls. Before #7591 the window ended at the first
  `code_calls` or `inheritance_edges` intent of either handler; now it ends at
  the first per-edge write. The two work items (`code_call_materialization`,
  `inheritance_materialization`) are independent and unordered, so an
  inheritance refresh-only write while the code-call item is still queued is
  inside the window. `UpsertIntents` writes 2,000-row statements with no
  enclosing transaction, so a refresh can also sit one round trip or one retry
  interval ahead of its edges. It is not closed here; see #7602. Net effect on
  accuracy: the 278 repositories that were permanent false gaps on ops-qa leave
  the gap list.

`CodeReachabilityVerdictSchemaEpoch` stays 4; no schema, response shape or
index changes.

Proof: `TestCrossRepoDeadCodeConsumerCoverageRefreshIntentLive` runs both
statements on a disposable PostgreSQL 18 with the full bootstrap schema:
(a) full generation, truncated epoch-4 watermark, refresh only: complete (failed
before the change: it returned six gaps, three of them refresh-only repositories that are complete);
(b) the same plus one `upsert` edge: gap `truncated`; (c) delta generation, no
watermark, refresh only: gap `no_snapshot_yet`; (d) a legacy row with no
`action` key: gap `truncated`; plus a no-watermark refresh-only repository and an
`inheritance_edges` refresh-only repository, both complete. Replacing the
predicate with a bare `NOT intent.is_refresh_intent` failed exactly case (c) in
both statements (seeded RED); the shipped predicate passes.
`TestCrossRepoDeadCodeConsumerCoverageUniversePredicate` pins the predicate in
both probes and still forbids `completed_at` and `EXISTS`.

Performance Evidence: QA replica, read-only, PostgreSQL 18.3, 2026-10-04,
`statement_timeout` 10 s, replay lag 0.1 to 2.0 s before and 0.86 s after,
`EXPLAIN` before `EXPLAIN (ANALYZE, BUFFERS)`, a unique nonce comment per timed
run, three runs per case, parameters epoch 4 and row cap 26 (named: the 799
active repository keys as `$1`, require-active-scope true). Measured. Plan class
unchanged: `shared_projection_acceptance_pkey` then
`shared_projection_intents_acceptance_lookup_idx`, with the new predicate as a
residual `Filter` on the intent row (no new index, no hoisted subplan).
Execution, old to new: named 28.9 to 53.3 ms before and 36.1 to 54.3 ms after
(runs 1 to 3: 53.3, 30.9, 28.9 old; 54.3, 36.1, 39.0 new; shared hit 619 to
628); all-repositories 3.5 to 3.7 ms before and 8.7 to 9.1 ms after (3.67,
3.52, 3.56 old; 8.67, 8.80, 9.11 new; shared hit 690 to 699). The all-repositories
rise is the early stop, not a costlier probe: it ends at the first 26 gaps, and
the 278 refresh-only repositories are no longer gaps, so it scanned 140 scopes
with 71 probes instead of 50 scopes with 27 probes; per-probe time is unchanged
(0.07 ms old, 0.086 ms new). A fully covered corpus already read every scope, so
the worst case is unchanged. Census over the 799 active repository scopes (795
with a watermark for the active generation, 4 delta generations with none and no
code intent): gap repositories 384 before, 106 after, all `truncated`, none
`no_snapshot_yet` or `older_epoch`; 278 refresh-only repositories leave the gap
set.

Observability Evidence: no change. The read keeps its `postgres.query` span
(`db.operation=cross_repo_dead_code_consumer_coverage`) and the response's
`consumer_coverage.incomplete` list; the removed gaps simply stop appearing.

## Known gaps

- **Unscoped requests are judged against every repository that can be a
  consumer.** One repository with `code_calls` or `inheritance_edges` work and no
  watermark, or a truncated one, makes every symbol of an unscoped, unnamed request unknown.
  Repositories with no such work (docs, IaC) no longer do. Name
  `consumer_repo_ids` or use a grant to narrow a request.
- The queue-time window above is not closed by #7591 (#7602).
- The check detects only a missing, truncated or older-epoch watermark on a repository with
  code edges (on a full generation, per-edge intents; see "Refresh intents (#7591)"). A stale or partly drained snapshot with `truncated = false`, even
  beside a pending intent, reads complete until the reducer rebuilds it. A
  zero-root repository is now stamped `truncated` by the writer (#7570), so it
  reads `truncated` once its watermark is rewritten at the current epoch.
- "Complete" is bounded by root emission. A consumer whose watermark is not
  truncated, in a language with no public-API root kind, can still hide an
  exported-but-unrooted caller of the producer symbol.
- An UNGRANTED consumer repository with a partial snapshot is not covered for a
  scoped caller who named no consumer: the check runs over the grant, and the
  ungranted-consumer probe that finds ungranted consumers needs rows to exist.
- This reader trusts the watermark. A watermark written by a loader older than
  #7570 with `truncated = false` reads as complete only while its epoch is
  current; the epoch-4 bump makes those older-epoch gaps.
- Identity of `repository_id`. The statements join the watermark's
  `repository_id` to the scope's `source_key`. The writer stamps
  `input.RepositoryID`, the loader's `acceptance_unit_id`
  (`CodeReachabilityProjectionRunner.projectInput` ->
  `ReplaceRepositoryRows`), and the git collector writes the scope's
  `source_key` as the repository id. Production already treats the two as one
  identity space: `code_reachability_rows.repository_id = ANY(grant)` and
  `scope.source_key = ANY(grant)` bind the same grant list in the evidence page
  and in the changed-since and generation-lifecycle routes. The QA check that
  the three counts agree (799 repository scopes, 796 watermarks, 796 in the join;
  the 3 scopes without a watermark have no code intents) is recorded above.
