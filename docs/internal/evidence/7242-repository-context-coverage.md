# #7242 repository context coverage and entry-point read

## Root-Cause Evidence

The 2026-09-26 ops-qa sweep on the deployed `sha-518f9dd` image measured
`GET /api/v0/repositories/{repo_id}/context` at 12.49 s cold, 0.54 s warm
p50, and 7.15 s warm p95. MCP `get_repo_context` measured 1.98 s cold and
0.98 s warm p95. The largest argument has 12,403 indexed files. The issue's
other observed calls were 7.2 s for 7,097 files and 1.3 s for 147 files.
These are baseline measurements, not post-change endpoint results.

The context handler called `RepositoryCoverage`, which computes entity counts,
entity type groups, and maximum index time even though context reads only file
count and language groups. Read-only `EXPLAIN (ANALYZE, BUFFERS)` on the same
ops-qa repository found the `content_entities` aggregate scanned 241,726
rows: 7,301 ms on its first observed call, then 6,645/311/191 ms. Its first
plan used 88,332 shared buffers, including 83,369 reads. A file-only language
group query returned the same 12,403-file count and seven language groups in
1,871 ms first and 1,506/6.8/6.4 ms on later calls. The completed API stage
log on a cold probe showed a 10.7 s gap between `repository_lookup` and
`summary_counts`, where this uninstrumented full coverage call ran. That
observation supports the unnecessary entity scan as a major cause.

The existing entry-point query found 59 rows through a global name-trigram
bitmap path: 23,925 bitmap candidates, 7,980 rechecks, and 2,778 repository
or type filters on the ops-qa profile. It took 2,217 ms first and
196/125/125 ms later. An isolated 300,000-entity PostgreSQL fixture showed
that the exact-predicate partial index in migration 133 changes this query to
an index-only scan. The fixture returned the same 59 rows in the same order.

The context handler also loaded only the first 5,000 files for its derived
infrastructure and deployment-artifact overviews, without telling callers
that later files were omitted. A 5,001st sentinel read now discloses that
limit. A failed content summary or file read also has a stable partial reason.

A read-only API/MCP crossover on the same 12,403-file argument returned HTTP
200 and 59 rows from both surfaces. API then MCP measured 14.203 s then
1.118 s; reversing the order measured MCP 0.635 s then API 0.595 s. This
small sample supports an execution-order/cache explanation for the reported
API/MCP gap. It does not establish steady-state p95 or prove that the routes
have identical total cost.

## Performance Evidence:

On ops-qa, the file-only language query's exact plan was an index-only scan
using `content_files_language_repo_idx`, but visibility checks fetched 8,486
heap rows. The first measured call read 5,222 buffers and took 2,499 ms; a
repeat hit 6,300 buffers and took 8.6 ms. A local synthetic candidate index
on `(repo_id, language)` improved a 12,403-file fixture's warm median only
from 0.838 to 0.771 ms, so no new language index is included. The measured
cold heap-read cost remains an endpoint latency risk.

A disposable PostgreSQL 18 live test applied all 154 migrations, seeded
12,403 files and 241,726 entities by setting
`ESHU_TEST_CONTEXT_COVERAGE_ENTITY_ROWS=241726`, and called both production
Go methods in one process. `RepositoryCoverage` took 59.601 ms and the new
`RepositoryContextCoverage` took 1.766 ms in that sequence. File count and
ordered language groups matched exactly; an empty repository matched too.
This is a local, warm fixture measurement. It is not comparable to the
ops-qa cold total or a deployed handler p95.

A disposable Neo4j Community 2026.09 graph with 12,403 synthetic files
confirmed the fallback count returned 12,403 and its language query returned
six groups of 1,772 files (1,771 files had null language). Cold `PROFILE`
showed a repository index seek followed by expansion of all 12,403 file
edges: 24,810 DB hits and 243 ms for count, 35,442 DB hits and 160 ms for
language grouping. These server timings exclude `cypher-shell` startup and
are not endpoint measurements. The handler bypasses these graph reads when
content coverage is available; they do not explain the observed cold
Postgres coverage gap.

On an isolated 300,000-entity fixture, five warm entry-point samples had a
9.632 ms median without the partial index and 0.062 ms with it. The read
changed from GIN bitmap filtering to an index-only scan, with 0 before-only
rows, 0 after-only rows, and 0 position mismatches across 59 rows. A separate
100,000-row fixture measured the index write cost: inserting 59 matching
rows took median 1.072 ms before and 1.690 ms after; changing their included
language took 1.660 ms before and 1.829 ms after. A concurrent build completed
in 706 ms while a separate writer completed 40 commits of 500 nonmatching
inserts with a 200 ms lock timeout. These fixture measurements do not prove
production write throughput.

A deliberately failed concurrent unique-index build left an invalid index;
`IF NOT EXISTS` alone skipped it. The existing bootstrap invalid-index cleanup
path drops such an index before retrying. Migration 133 is one standalone
`CREATE INDEX CONCURRENTLY` statement, and the local bootstrap applied it.

**Target status:** a changed API/MCP binary has not been deployed to ops-qa.
Cold and warm endpoint p95 under 1 s are NOT_CHECKED and must not be claimed
from the local SQL improvements. In particular, the measured cold language
read and file-derived overview may still exceed the cold target. #7242 stays
open for the owner's deployment and same-argument sweep, or for a further
bounded read redesign if that sweep misses the target.

## Observability Evidence:

The handler now logs a timed `content_coverage` stage with `available`,
`error`, and `failure_class` when the content summary fails. The existing
`content_infrastructure_overview` stage records file count, error, truncation,
and a failure class on read failure. The new Postgres read has a
`postgres.query` span with operation `repository_context_coverage` and table
`content_files`. The response exposes stable `partial_reasons` for content
coverage failure, file-read failure, and the 5,000-file limit, so operators
and callers can distinguish a complete answer from a fallback or lower bound.

## No-Regression Evidence

- Focused repository handler regressions were RED before the narrow read and
  file-limit disclosure, then GREEN after the implementation. The content
  failure disclosure also had its own RED/GREEN test.
- A fake-store response comparison kept the complete context JSON identical
  between full and narrow content coverage on equal file/language inputs.
- The live PostgreSQL differential above covered populated and empty repos,
  including a null-language bucket.
- The index fixture compared ordered rows and measured matching-row write
  cost. The migration schema test checks its exact predicate and isolated
  concurrent statement; the migration checksum and bootstrap digest tests
  pin its new bytes without changing any shipped migration.
