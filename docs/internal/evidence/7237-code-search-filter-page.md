# #7237 repository code-search fallback page proof

Scope: `POST /api/v0/code/search` when a selected repository has no graph
results and the request includes `language` or `exact=true`. This is a
follow-up to the merged #7237 read-path work in
`7237-query-read-paths.md`; the deployed endpoint target remains separate.

`repo-B` in this note is a stable placeholder for the measured repository id;
the mapping is held outside the repository.

## Failure and local fix proof

The old handler fetched `limit+1` name and source rows from Postgres, then
removed rows with the wrong language or a nonexact name in Go. With a public
limit of 1, two earlier nonmatching rows filled the probe and hid a later
matching row. A source-only term had the same failure.

`GOCACHE=<worktree>/.gocache go -C go test ./internal/query/codequery
-run '^TestCodeSearchContentFiltersBeforeBoundedPage$' -count=1` failed before
the production change in all three subtests: `language`, `source language`,
and `exact name`; each returned an empty result instead of entity `wanted`.
After the change, the same command passed. The reader tests also exercise the
production SQL builder and pgx bind arguments: repository and language or
exact-name predicates precede the ordered `LIMIT`, and repository-scoped
reads use a custom plan rather than a cached generic plan.

## Theory and representative plan checks

A disposable synthetic PostgreSQL 18 fixture contained 20,003 entities:
20,000 unrelated rows, two early Python `decode*` rows, and one later Go
`decode` row. The old name and source statements each returned the two Python
rows at `LIMIT 2`; applying `language='go'` before the limit returned the
later target. The exact-name predicate likewise found `decode` after two
substring near misses. Candidate and unbounded-oracle result differences were
zero. Warm alternating `EXPLAIN ANALYZE` samples were about 0.05–0.06 ms for
both source shapes; this small fixture is a correctness proof, not a latency
estimate for the deployed corpus. The owned fixture was removed.

Read-only QA `EXPLAIN (ANALYZE, BUFFERS)` checked the recorded
`showImage`/JavaScript request in `repository:repo-B` with SQL probe limit
11 and a 3-second statement timeout. The warm name read was 6.37 ms baseline
versus 6.46 ms with language in SQL, both at 828 buffer hits and eight rows.
The warm source read was 81.77 versus 81.79 ms, both at 2,988 hits and 11
rows. Both retained trigram bitmap plans; bidirectional row-set differences
were zero. The exact-name variant returned the same eight rows in 7.21 ms.
This recorded request had no row loss, so it is a same-data plan check rather
than another reproduction. No QA data, index, or setting was changed.

The exact parameterized candidate SQL was also prepared in a read-only QA
transaction with `repo_id`, pattern, `text[]` language variants, and probe
limit bound separately. For `showImage`/JavaScript/limit 11, the warm forced
custom name plan took 3.31 ms (466 buffers); its forced generic plan took
33.89 ms (2,406 buffers). The forced custom source plan took 84.99 ms (1,791
buffers); generic took 33.37 ms (3,180 buffers). The old and new generic
statements had the same plan, rows, and buffer counts for this argument; the
custom source tradeoff adds about 52 ms to this individual read. `pgx` 5.9.2
`QueryExecModeExec` sends the new statements without a named prepared cache,
matching the forced custom-plan arm rather than the generic arm.

The recorded set 7 uses `query='a'`, language JavaScript,
public limit 30, and probe limit 31. The candidate's forced custom name plan
returned 31 rows in 286.56 ms with 414 cache hits and 772 reads; custom
source returned 31 rows in 52.94 ms with 45 hits and 1,108 reads. Both used
the ordered repository path index. Forced generic name hit a 3-second statement
timeout. Forced generic source exceeded the 10-second client bound; its exact
read-only backend remained active until `pg_cancel_backend` took effect. The
backend was verified gone; no termination was invoked and no QA data changed.
Those timeouts provide no completed generic plan or latency value. Earlier
same-argument diagnosis recorded a severe generic source tail, but it was not
a fresh paired baseline for this candidate.

Performance Evidence: the read-only `EXPLAIN (ANALYZE, BUFFERS)` checks above
compare the recorded `showImage` argument on the same QA corpus, including the
84.99 ms custom source versus 33.37 ms generic source cost. The short `a`
argument completed in 286.56/52.94 ms with custom name/source plans; generic
name timed out at 3 seconds and generic source exceeded the 10-second client
bound. These are SQL read measurements, not deployed endpoint p95.

Observability Evidence: both filtered reads retain `postgres.query` spans with
`db.operation=search_code_entity_names` and
`db.operation=search_code_entity_content`. Exact-name requests omit the source
read and its span; query errors are recorded on the read's span.

Automatic plan selection, other corpus arguments, and the patched API/MCP
cold/warm p95 are **NOT_CHECKED**. The long-pattern source plan tradeoff is
accepted here only to avoid the measured short-pattern generic timeout; it is
not a claim of universal speedup. #7237 stays open until owner deployment and
the exact recorded-argument sweep meets the issue's latency target.
