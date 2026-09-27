# #7249 dead-code reachability read proof

## Scope

Read-only PostgreSQL 18 probes used a 12,403-file repository and the `find_dead_code` argument shape (`repo_id`, `limit=12`, no language). The deployed MCP trace below used a different 7,097-file repository, so its endpoint time is not a paired measurement for these SQL probes. The first Function candidate page had 121 unique entity IDs. Those IDs are before application policy, duplicate filtering, and exclusions, so the SQL probe is representative of the first page rather than a claim about the exact dispatched ID set. No production DDL, data write, `ANALYZE`, or setting change was made. Private repository and entity names were withheld.

## Observation and theory

The shipped `CodeReachabilityIncomingEntityIDs` query joined active scopes before finding candidate reachability rows. Its first observed `EXPLAIN (ANALYZE, BUFFERS)` drove 814 active scopes into 914 primary-key index searches, returned zero active rows for this page, and took 1,738.022 ms with 4,744 shared reads. For the separate 7,097-file deployed MCP request, Tempo reported a 2,095 ms reachability span near a 2.536-second first-observed call with the same argument shape. The spans have separate roots, so their request-level attribution is an inference; the trace and SQL times are not subtracted as endpoint proof.

A read-only row-first materialized CTE fetched reachability rows by the 121 candidate entity IDs before joining active scopes and generations. The first observed plan used 121 entity-leading index searches, 189 shared reads, and 114.162 ms. This avoids the scope-first index search fanout. It retains the cross-repository consumer read; a scoped caller still projects grant membership rather than filtering away hidden consumers.

## Query shapes

The shipped query binds each candidate ID, then joins active scopes and generations before applying `row.entity_id IN ($1, ..., $K)` and `row.depth > 0`. The tested rewrite materializes the entity-ID and depth-filtered reachability rows first, then applies the same active-scope and active-generation joins and outer `DISTINCT`. A scoped request continues to project `(candidate_rows.repository_id = ANY($K+1))` as a result column; it does not put the grant in `WHERE`. The unscoped form projects only the two original result columns.

## Row truth and repeat measurement

A second probe combined the 121 page IDs with 20 distinct IDs known to have active incoming rows. Both SQL shapes returned the same 20 distinct `(entity_id, min_resolution_method)` rows. Bidirectional `EXCEPT ALL` returned zero rows in both directions. With those 141 unique IDs, two observed baseline plans took 257.590 and 259.800 ms; two row-first plans took 4.403 and 3.079 ms. The observed plans were:

| IDs | Shape | Result rows | Index searches | Shared reads | Execution |
| ---: | --- | ---: | ---: | ---: | ---: |
| 121 | shipped, first observation | 0 | 914 | 4,744 | 1,738.022 ms |
| 121 | row-first, first observation | 0 | 121 | 189 | 114.162 ms |
| 141 | shipped, repeat 1 | 20 | 914 | 0 | 257.590 ms |
| 141 | row-first, repeat 1 | 20 | 127 entity searches plus 20 generation lookups | 203 | 4.403 ms |
| 141 | row-first, repeat 2 | 20 | 127 entity searches plus 20 generation lookups | 0 | 3.079 ms |
| 141 | shipped, repeat 2 | 20 | 914 | 0 | 259.800 ms |

The same 141-ID set with a one-repository consumer grant returned 20 rows with 20 hidden-consumer markers from both shapes; bidirectional `EXCEPT ALL` was zero in both directions. The scoped plans took 529.817 ms (shipped) and 15.746 ms (row-first). A second 7,097-file repository supplied another 121-ID Function page: both shapes returned zero active rows with zero bidirectional differences, and the observed plans took 600.785 ms (shipped) versus 3.501 ms (row-first).

Performance Evidence: These are PostgreSQL statement times under the observed cache state, not cold or warm HTTP/MCP p95. The first observations are sequential and do not share identical cache state. The row-first shape changes no table or index and adds no writer cost. The existing entity-leading index served the measured row-first plans, so an additional index is not justified by this evidence.

No-Observability-Change: The existing `postgres.query` span retains `db.operation=code_reachability_incoming_entity_ids`, errors, and duration for this statement. The rewrite changes only SQL text and does not add a worker, queue stage, metric, or status field.

## Remaining proof

A Go regression failed on the shipped scope-first SQL and passes on the row-first SQL. The full `go test ./internal/query -count=1` package suite passed after the change; deliberate removal of the active-generation join and addition of a consumer-repository filter each failed the provenance test. Final diff review, local promotion gates, hosted CI, and a deployed same-argument cold/warm p95 sweep are separate gates. Keep #7249 open until the owner deploys the merged change and replays the original eight real argument sets across all seven dead-code endpoints, including the 7,097-file `find_dead_code(repo_id, limit=12, no language)` timeout case and the largest-repository cold case. Cold and warm p95 must each be below 1 second on the same arguments; the SQL probes here do not establish that endpoint result.
