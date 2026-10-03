# #7547 cross-repo dead-code consumer coverage read

## Scope

`POST /api/v0/code/dead-code/cross-repo` (MCP `find_cross_repo_dead_code`)
classified a producer symbol `dead` whenever no active-generation consumer row
existed in `code_reachability_rows`. `Available` meant only "the store is present
and the grant plan is ok". A consumer repository whose active snapshot is
missing, truncated, from a delta generation, or built from zero roots writes no
rows for the symbols it calls, so every producer symbol it calls read `dead`.

This change makes the reader consult `code_reachability_repository_watermarks`.
The writer side (stamping `truncated`, gating the loader) is a separate change
and is not touched here.

## Behavior

- A repository scope with an active generation is a gap when it has no watermark
  for that generation, or the watermark is `truncated`, or its
  `verdict_schema_epoch` is below `reachabilitystore.CodeReachabilityVerdictSchemaEpoch`
  (bound as a parameter, never copied), AND that generation has a
  `shared_projection_intents` row in the `code_calls` or `inheritance_edges`
  domain (completed or pending), reached through `shared_projection_acceptance`.
  A repository with no such intent cannot be a consumer (docs, IaC) and is
  complete. A zero-root repository with intents is NOT excluded: its edges can sit
  on a chain from a rooted repository to the producer symbol.
- `dead` needs no gap among the consumers the answer is judged against.
  Otherwise a candidate with no strong live consumer evidence is
  `unknown_needs_evidence` with the new reason `consumer_coverage_incomplete`.
  Strong consumer evidence still wins, as it does over a hidden consumer.
- Which consumers: the request's named `consumer_repo_ids` (a named repository
  with no active repository scope is a gap), else the caller's grant, else every
  repository scope with an active generation.
- A content store that cannot answer, or a request that cannot be planned, keeps
  `cross_repo_evidence_unavailable`. A coverage read error fails the request.
- The response gains `consumer_coverage {complete, incomplete_repo_ids,
  incomplete_truncated}`. It carries no count of repositories checked: that
  would force a full scan, and the statements stop at the first gaps.

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

Merge order with the writer change is free. The predicate reads the constant as
compiled, so it flags nothing extra while every watermark is at the current
epoch. When the writer change raises the constant, every older watermark of a
repository with code intents reads as a gap until the loader rebuilds it, which
the loader already does for any watermark below the current epoch. Pre-upgrade
watermarks (epoch default 0) are gaps for the same reason until rebuilt.

Both scope sides read `ingestion_scopes_active_generation_idx`; the watermark side
is a primary-key probe or a hash of the small watermark table. Neither statement
reads `code_reachability_rows`.

Performance Evidence: fixture-scale only, as above. The QA-replica `EXPLAIN` is
UNMEASURED (SSO expired); the figures are not QA timings and do not support a
QA-scale claim. At QA scale confirm: the scope side keeps using the partial index,
the named statement stays a single pass for a large grant, and the intent probe
stays a primary-key-prefix probe. A repository with many retained historical
intents and no intent on its active generation scans those intents once per
request before it is found complete; retention bounds it.

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
`consumer_coverage.incomplete_repo_ids` and the span.

## Known gaps

- **Unscoped requests are judged against every repository that can be a
  consumer.** One repository with `code_calls` or `inheritance_edges` work and no
  watermark, or a truncated one, makes every symbol of an unscoped, unnamed request unknown.
  Repositories with no such work (docs, IaC) no longer do. Name
  `consumer_repo_ids` or use a grant to narrow a request.
- The check detects only a missing, truncated or older-epoch watermark on a repository with
  code edges. A stale or partly drained snapshot with `truncated = false`, even
  beside a pending intent, reads complete until the reducer rebuilds it, and a
  zero-root repository is flagged only after the writer stamps `truncated`.
- "Complete" is bounded by root emission. A consumer whose watermark is not
  truncated, in a language with no public-API root kind, can still hide an
  exported-but-unrooted caller of the producer symbol.
- An UNGRANTED consumer repository with a partial snapshot is not covered for a
  scoped caller who named no consumer: the check runs over the grant, and the
  ungranted-consumer probe that finds ungranted consumers needs rows to exist.
- This reader trusts the watermark. Until the writer side stamps `truncated`
  for zero roots and depth cutoffs, and for delta generations, a watermark with
  `truncated = false` reads as complete.
- Identity of `repository_id`. The statements join the watermark's
  `repository_id` to the scope's `source_key`. The writer stamps
  `input.RepositoryID`, the loader's `acceptance_unit_id`
  (`CodeReachabilityProjectionRunner.projectInput` ->
  `ReplaceRepositoryRows`), and the git collector writes the scope's
  `source_key` as the repository id. Production already treats the two as one
  identity space: `code_reachability_rows.repository_id = ANY(grant)` and
  `scope.source_key = ANY(grant)` bind the same grant list in the evidence page
  and in the changed-since and generation-lifecycle routes. It is not proven end
  to end by a live row here; a QA check that the three counts agree
  (`ingestion_scopes` repository scopes, watermarks, and the join of the two) is
  the confirmation.
