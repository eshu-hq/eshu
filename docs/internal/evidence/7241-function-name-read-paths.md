# #7241 function-name read paths

## Scope and cause

The recorded sweep timed out on short or common function names across entity
resolution, complexity, relationships, content entity search, and MCP reads.
The graph-backed complexity, relationships, and `find_code` paths now use the
repository-and-ID content hydration from #7237. Entity resolution still called
`SearchEntityContent`, which searches `source_cache ILIKE` and limits the page
before matching graph rows by path, type, name, and start line. On the read-only
QA corpus, the `decode` lookup took 6,568.467 ms for limit 13; `a` took
3,917.985 ms for limit 31. The predicates already included `repo_id`; the
observed plan walked a broad source trigram or path index and discarded many
rows outside the requested repository. This is backend statement time, not
endpoint latency.

The resolver now reads its bounded graph-result IDs in one repository-scoped
batch. A graph ID may differ from its content ID, so unmatched IDs use one
batch of exact `(repo_id, relative_path, entity_type, entity_name, start_line)`
lookups. Each exact key reads at most two rows; an ambiguous key returns an
error rather than attaching arbitrary metadata. Existing graph metadata wins,
and an ID hit with empty metadata does not borrow metadata from another row.
The helper also serves entity context. ID hits, key hits, misses, and ambiguity
are recorded as count-only trace event attributes. The Postgres reads retain
query spans and error recording.

## Theory proof on read-only QA

The recorded resolver candidates were checked against content by exact key and
repository plus ID. Both methods returned the same selected content payload
and metadata for the sampled entities. Batches of 5 and 13 actual graph
candidates returned all corresponding rows. The ID batches took 6.514 and
12.245 ms; exact-key batches took 10.112 and 15.275 ms. A complete available
common-name graph set had 20 candidates: the repository-and-ID batch took
14.370 ms and the exact-key `JOIN LATERAL` batch with a two-row bound per key
took 77.046 ms, returning 20 rows with no missing or ambiguous key. A
101-key repetition proxy using those same graph tuples took 30.509 ms when
all matched and 15.633 ms when none matched. The proxy repeats keys, has warm
cache effects, and is not a real 101-distinct-result request. No QA writes,
DDL, settings, or deployment were performed.

## Local proof and response boundary

Focused Go regressions went red on the old substring hydration and green on
ID and exact-key hydration, including legacy mismatched IDs, contradictory
ID/tuple rows, empty ID-hit metadata, and ambiguous exact keys. A local
PostgreSQL 18 scratch query parsed the final `JOIN LATERAL` shape and checked
repository scoping, key order, and the two-row-per-key bound. The query was
rolled back. The named test commands and final results belong in the PR body.

The direct content entity search keeps substring semantics and explicit
`truncated` paging. Its SQL page now sorts tied paths and start lines by
`entity_id`, matching the repository-first index added in #7237. On the
340,030-row synthetic PostgreSQL fixture with that index, old and new pages
had the same ordered-ID hashes for both recorded patterns. Five warm
`EXPLAIN (ANALYZE, BUFFERS)` samples used the same 13-row or 31-row page:

| Pattern | Old execution ms | New execution ms | Ordered-ID hash equal |
| --- | --- | --- | --- |
| `a` | 0.089, 0.114, 0.110, 0.108, 0.090 | 0.079, 0.102, 0.084, 0.084, 0.146 | yes |
| `decode` | 0.258, 0.207, 0.205, 0.270, 0.202 | 0.185, 0.187, 0.241, 0.258, 0.282 | yes |

The fixture has only the index and narrow query columns, so this is a page
ordering and access-path check, not a wide-row decode or API timing result.
Metadata may become present on a graph row that the old limited substring page
missed; graph row IDs, order, and count stay unchanged. Byte-identical complete
resolver responses are not claimed. Complexity, relationships, `find_code`, and
MCP content-search responses have not been retimed on a deployed patched build.

**Endpoint cold/warm p95 is NOT_CHECKED.** The owner controls deployment. The
same recorded-argument sweep must run after #7237 and #7241 are deployed
before a sub-second endpoint claim or issue closure. After its PR merges,
#7241 remains open awaiting deployment and that sweep.
