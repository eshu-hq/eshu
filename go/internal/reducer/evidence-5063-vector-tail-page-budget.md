# Search-vector terminal-tail evidence (#5063)

## Performance Evidence:

The pre-change full-corpus proof completed reducer queue work in 1,350 seconds
but did not publish `search_vector_ready` until 2,172.9 seconds. The residual
search-vector tail was therefore 822.9 seconds.

The first candidate raised the bounded sweep budget from 10,000 to 50,000
documents. A clean remote run disproved that as a complete fix. An early sweep
built 49,995 documents across 11 scopes in 25.39 seconds (1.44 seconds query,
0.68 seconds embed, 21.79 seconds write), but one 76,553-document scope then
stalled after 50,000 terminal rows. Its pending selector remained active for
257 seconds while the global document population continued growing and the
vector count stayed fixed at 460,947. The run was stopped rather than spending
the full validation budget on a disproved theory.

The residual root cause was repeated evaluation of the terminal anti-join from
the start of a scope. The representative PostgreSQL 18 fixture contains
1,803,206 documents globally and one 253,206-document scope with a 250,000-row
completed prefix and 3,206 pending rows. On that same fixture:

| Selector shape | Execution time | Result |
| --- | ---: | ---: |
| Cursor-free terminal anti-join | 196-282 ms | 3,206 pending |
| Keyset cursor at `doc-0250000`, JIT enabled | about 123 ms | 3,206 pending |
| Keyset cursor at `doc-0250000`, transaction-local JIT disabled | 7.162 ms | 3,206 pending |
| Keyset cursor at `doc-0200000`, transaction-local JIT disabled | 189.527 ms | 3,206 pending |

The terminal cursor is 27.4x faster than the 196 ms cursor-free observation.
The mid-scope cursor does not regress the cursor-free query. The JIT-enabled
terminal plan spent about 114 ms compiling a query that executed in single-
digit milliseconds, so the production reader disables JIT only for this query
transaction. PostgreSQL's [`SET` documentation](https://www.postgresql.org/docs/current/sql-set.html)
states that `SET LOCAL` lasts only for the current transaction, so the setting
cannot leak to later pooled-connection work.

The finished production store was then run against the same fixture, rather
than only re-running hand-written SQL. It returned all 3,206 rows, ordered from
`doc-0250001` through `doc-0253206`, in 44.221 ms including row decoding and
transaction completion.

The first clean remote cursor run proved the cursor selector itself stayed
bounded (observed active age 0-1 seconds while the corpus grew past 1.3 million
documents), but exposed the next residual before PR: the retired
`ScopeVectorComplete` materialized `EXCEPT` branch remained active for 178
seconds on a fully terminal 76,553-document scope. The scope had
76,553/76,553 terminal rows, so this was readiness proof cost rather than
useful vector work.

On that exact live scope under the same ingestion pressure, the indexed
`LIMIT 1` pending predicate returned the identical "no gap" verdict in 5.562
seconds: 32x faster than the cancelled materialized query. It used the vector
metadata and value primary-key indexes and bounded memory with no materialized
scope sets. The invalidated acceptance run was stopped and its clean volumes
were removed instead of allowing the residual to consume the validation cap.

The finished `ScopeVectorComplete` implementation keeps the cheap count gate
but replaces only its exact branch with the already-proven indexed pending
predicate. On the 1.8-million-document local fixture, the production store
proved a fully terminal 253,206-document scope complete in 1.167 seconds. The
same production call returned incomplete in 1.157 seconds after a content hash
was made stale behind the cursor. On the still-building semantic fixture, the
count gate left the exact probe `never executed` and completed in 0.035 ms.

## Full-Corpus Result:

The finished binary completed a clean, cold 896-repository remote run in
1,814 seconds (30 minutes 14 seconds) measured launch-to-query-readiness. This
missed the under-30-minute target by 14 seconds, but replaced the previous
90-minute-plus stall with a bounded terminal drain. The terminal snapshot was:

- 896/896 `source_local` projections succeeded;
- 11,934/11,934 queued work items succeeded, with zero failed or dead-letter
  items;
- 2,583,358/2,583,358 search documents had vector metadata;
- 831/831 vector scopes were ready, with zero pending scopes;
- `search_vector_ready` was published; and
- the maximum observed active age of the exact readiness query was zero
  seconds at the controller's sampling interval.

Source projection reached 896/896 at about 26 minutes. The remaining roughly
four minutes were the fixed-corpus vector drain; no terminal selector or exact
readiness query accumulated runtime during that drain.

Because launch-to-readiness missed 30 minutes by 14 seconds, the next bounded
phase is the already-tracked source/content-index write-tax work in #4980.
Source projection now dominates the wall clock; another cursor rewrite is not
the highest-leverage path toward the under-20-minute objective.

After the terminal snapshot, an intentional backend-container restart caused
the runtime recovery path to schedule 2,231 correlation and materialization
repairs. Restarting the same resolution-engine binary drained those repairs
and their follow-on work to 13,034/13,034 succeeded with zero outstanding,
failed, or dead-letter items. The recovered API reported `healthy`, 896
repositories, and the terminal queue counts. MCP returned the same repository
and queue truth. One durable relationship-evidence record matched between API
and MCP for lookup basis, generation, relationship type, confidence, evidence
count, source, and target.

## Accuracy Evidence:

The pending predicate is unchanged. The cursor only adds
`document_id > document_cursor`, a deterministic `ORDER BY document_id`, and a
bounded limit. On the representative terminal fixture, the cursor-free and
cursor-bearing pending sets had symmetric differences of `0/0`.

A cursor alone would be incorrect if a document behind it later acquired a
stale content hash. That case was injected transactionally at `doc-0100000`:
the exact pending set became 3,207 rows while the suffix contained 3,206, an
expected one-row gap. The runner therefore keeps `ScopeVectorComplete` as the
exact truth gate. When a short or empty suffix page remains incomplete, a
fence-CAS resets the cursor to the start. The following sweep finds the stale
row behind the former cursor. A full page advances without wrapping.

The exact-gate live matrix agrees with the independent fact-record reference
for still-building, fully complete, ready-without-value, stale-hash,
disabled-without-value, and retired-extra-metadata scopes. This is the same
semantic boundary the cursor wrap relies on; no count-only readiness shortcut
was introduced.

Cursor state is scoped to the existing
scope+generation+provider+source+model+index-version identity. It is preserved
across same-revision retries, reset when a newer document-projection revision
wins, advanced monotonically only after both fenced vector batches succeed,
and rejected when the build fence or active generation is stale. A real-
Postgres live test proves monotonic advance, stale-worker rejection, revision
reset, stale reset rejection, and current-fence reset.

The 50,000-document total sweep budget remains bounded and is divided across
selected scopes. It improves useful work per sweep but is not treated as the
root fix. Two broader write-path candidates were also rejected after
finished-shape proof on 50,000 256-dimensional vectors. Both produced exact
table equivalence (`values=0/0`, `metadata=0/0`) but regressed runtime:

- transactional COPY staging: 7.922 seconds versus 7.305 seconds current;
- 4,000-row statements: 4.824 seconds versus 4.668 seconds current.

## Observability Evidence:

The runner continues to emit
`eshu_dp_search_vector_build_phase_seconds` for `query_load`, `embed_build`,
`write_upsert`, and `scheduling_wait`, plus the structured sweep-completion
log. A new structured `search vector document cursor wrapped` log records the
scope, generation, short-page document count, and page limit whenever the exact
gate forces a wrap. Cursor advance/reset errors and CAS rejections are also
logged with scope and generation identifiers. No public telemetry contract or
operator configuration changes.
