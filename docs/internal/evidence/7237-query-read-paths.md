# #7237 query read-path evidence

Scope: relationship edge pages, code-search metadata enrichment and content
substring fallback, and semantic code-hint pages. This record separates live
read-only diagnosis, local database proof, and endpoint verification.

## Live read-only diagnosis

The graph and PostgreSQL in the QA environment were probed read-only. No
index, statistics, server setting, or deployment was changed there. The existing sweep found relationship
edge pages around 1.1 seconds, code/search calls at 45–48 seconds or timeout,
and semantic code-hint cold calls at 9–23 seconds. These are the starting
observations, not before/after endpoint comparisons.

For the exact CALLS edge page argument, `PROFILE MATCH
(s:Function)-[r:CALLS]->(t) ... ORDER BY s.uid,
coalesce(t.id,t.uid) LIMIT 11` took 1,007 ms and 1,719,156 db hits. Adding
`s.uid IS NOT NULL` took 20 ms and 106 db hits in the first probe. Both
returned the same ordered 11 rows. Five later read-only indexed samples took
24, 2, 1, 1, and 1 ms, with 139 db hits and 11 rows each. The first sample is
not a controlled cold-cache measurement. Missing-anchor counts were zero for
CALLS Function uid and IMPORTS File path in that corpus at the observation.

A read-only PostgreSQL plan for semantic code hints used the general
`fact_records_scope_generation_idx` and 8,109 index searches even for an empty
page. The previous sweep's cold latency therefore cannot be attributed to a
post-migration result until deployment and a matching sweep.

## Relationship edge correctness

The source anchor is indexed but not required by a graph existence constraint.
The first draft of the fast query would have silently dropped a source with an
alternate ID and no anchor. The final handler probes `limit+1` indexed rows only for the unscoped,
unfiltered CALLS page and uses the original complete query when that probe
returns at most the requested limit. It discards probe rows on fallback, so the
response comes from one complete read. Other verbs, scoped pages, and
source-tool filters keep their original single-read query. The fallback error
propagates. The fast path is valid because ascending nulls sort last on both
checked backends: an isolated Neo4j 2026 Community fixture and a read-only
NornicDB probe. This is a claim about the checked versions.

The Neo4j fixture included two anchored sources, two alternate-ID sources with
no uid, a denied null-anchor source, a duplicate CALLS edge, and a source-tool
filter. The complete ordered scoped/filter result contained four rows,
including the allowed null-anchor row and duplicate, while the indexed query
contained the first three. Unit RED/GREEN cases exercise indexed counts
`limit-1`, `limit`, and `limit+1`, an all-null page, truncation, fallback error,
and single-read dispatch for scoped and filtered pages.

A read-only empty filtered CALLS probe found that the original query took
570 ms and 3,050,807 db hits, while an added indexed probe alone took
1,012 ms and the same db hits. Running both would regress this empty page.
The final dispatch therefore uses the unchanged original query for filtered
pages. Five further samples of that single query were 555, 637, 564, 537,
and 552 ms (nearest-rank p95 637 ms). Scoped pages and other verbs likewise
retain the original query.

The read-only QA graph had 335,224 Function-sourced CALLS edges. At the
maximum allowed page limit of 200, the exact indexed query returned 201 rows
in 42 ms and 3,565 db hits, so this snapshot does not take the fallback for
any allowed limit. A future sparse corpus can. On an isolated Neo4j 2026
Community fixture with 100,004 Function nodes and five CALLS edges (three
anchored, two null-anchor), the complete LIMIT 11 query returned five rows
at 66 db hits; the indexed `WHERE s.uid IS NOT NULL` probe returned three at
44 db hits, then the handler used the complete five-row result. Five paired
warm sequential PROFILE pairs were 3+4, 4+3, 4+3, 5+5, and 16+12 ms
(probe+complete; nearest-rank p95 28 ms for both reads versus 12 ms for
the complete read).
The first cold/compilation-affected samples were 249 and 532 ms, separately;
their 781 ms sum is not a comparable cold endpoint measurement. NornicDB
sparse-page latency at this scale remains NOT_CHECKED. The patched endpoint's
cold/warm p95 also remains NOT_CHECKED pending deployment.

## Local PostgreSQL theory and finished migration proof

Disposable PostgreSQL 18 on a private local port; no production data was
copied. The content fixture had 340,000 entities across two repositories,
including 300,000 earlier-sorting rows outside the requested repository.
`SearchEntityContent` keeps its existing `ILIKE` and limit, and adds
`entity_id` as the last sort key so tied path/start-line rows have stable page
boundaries. The candidate `(repo_id, relative_path, start_line,
entity_id)` btree changed the common `a` plan from a global path walk to a repository-bounded ordered scan. The exact
migration SQL was then applied, and the ordered first-15 ID hash matched
before and after (`9ede6f44739180746314ec750c4abcc9`). The new index was
valid and ready. A second 30-row tied-path fixture crossed the LIMIT 15
boundary. The explicit path/start-line/entity-ID order produced the same
first-15 hash (`6ac1592c3fd5f8f7f225359723ad3b03`) before and after the
final index. Its pre-index global path walk took 69.301 ms and 5,186
buffers; the final repository-first candidate took 0.039 ms and 4 buffers.
This is a plan probe, not a five-sample p95. `decode` still used the trigram
GIN index under a custom plan. A forced generic `decode` plan used the new
repository-bounded btree.

Five warm `EXPLAIN (ANALYZE, BUFFERS)` samples per arm (nearest-rank p95 is the
maximum of five, in milliseconds):

| Pattern and plan | Before samples | After samples | After p95 |
| --- | --- | --- | ---: |
| `a`, custom | 74.291, 75.289, 75.054, 77.015, 74.038 | 0.061, 0.053, 0.207, 0.049, 0.078 | 0.207 |
| `decode`, custom | 0.119, 0.120, 0.124, 0.123, 0.130 | 0.138, 0.125, 0.109, 0.128, 0.131 | 0.138 |
| `a`, forced generic | NOT_CHECKED | 0.052, 0.052, 0.055, 0.051, 0.052 | 0.055 |
| `decode`, forced generic | NOT_CHECKED | 10.615, 11.516, 12.905, 10.426, 12.972 | 12.972 |

For metadata enrichment, the handler now fetches graph result IDs in one
repository-scoped `ListRepoEntitiesByIDs` call, avoiding a second substring
search that could select unrelated entities with the same name/path. A
15-ID lookup used the primary-key index; five local samples were 0.500,
0.102, 0.117, 0.112, 0.112 ms. The fixture has no metadata column, so this
measures the lookup path and not wide-row decode or full API time. A RED/GREEN
unit test proves exact-ID attachment, no substring call, and preservation of
existing metadata. On the read-only QA environment snapshot, 20 of 20
sampled exact `decode` graph `(entity_id, repo_id)` pairs resolved to the same pair in
`content_entities`; no identifiers were copied into this record.

The fact fixture had 400,000 records across 8,000 scope prefixes. Before the
kind-selective index, an empty code-hint page used a parallel scan, 20.681 ms
and 3,744 shared buffers. After the exact partial ordered index migration, the
same shape used one index search, one buffer, and 0.031 ms. Rebuilding the
index kept two populated matching rows in the same order and excluded a
wrong-repository row and a tombstone. Five warm empty-page samples after the
migration were 0.051, 0.065, 0.034, 0.034, 0.087 ms (p95 0.087 ms). The
migration re-applied without a duplicate and remained valid/ready after a
local database restart. The measured semantic page is empty; populated
per-repository pages at scale remain NOT_CHECKED.

The tracked production bootstrap runner applied the exact embedded
migrations 131 and 132 to disposable PostgreSQL 18 and recorded one `full`
receipt for each current checksum. Both indexes were valid and ready.
Reapplying the full bootstrap under a held reader lock passed. A focused
integration test applied both exact migrations through the tracked runner
while a writer transaction was open, observed each concurrent build in
`pg_stat_progress_create_index`, committed a second writer during the build,
and verified both receipts and index validity (0.58 s test body). Existing
live lock-wait and invalid-index recovery tests passed (15.11 s and 0.05 s).
These tests establish the local migration lifecycle, not the cost of index
maintenance under production ingest.

## Performance and observability boundary

Performance Evidence: The read-only QA environment CALLS baseline was 1,007 ms,
1,719,156 db hits, and 11 rows; the indexed shim was 20 ms, 106 db hits,
and the same 11 rows. Five further indexed samples were 24, 2, 1, 1, and
1 ms. On local PostgreSQL 18 fixtures, the content search and semantic
code-hint before/after timings and row counts appear above. These are
backend measurements, not patched endpoint p95.

Observability Evidence: The graph adapter and `ContentReader` retain their
existing spans, query fingerprints, durations, and error recording. A sparse
unscoped CALLS fallback emits one span per read; the complete read's error
propagates. The query fields and response truth envelope are unchanged.

The indexes add write and storage work; this fixture does not measure that
cost against production ingest. Existing graph-read and PostgreSQL spans still
cover every issued read. A sparse unscoped CALLS fallback page issues two graph reads, visible
as two spans, and surfaces an error if its complete query fails. Filtered and
scoped pages issue their original single read. No runtime
telemetry fields or wire contracts changed.

**Endpoint cold/warm p95 is NOT_CHECKED** on the patched build. The QA environment is
read-only and awaits the owner's deployment. The local SQL and Cypher plans
support the candidate; they do not establish a deployed API latency claim.
A matching post-deploy sweep on the recorded arguments is required before
closing #7237 or claiming the <1-second endpoint budget.
