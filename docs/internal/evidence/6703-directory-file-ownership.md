# 6703: File-owned directory language counts

The `directory` branch of `POST /api/v0/code/language-query` seeks each
granted repository's `Directory` nodes through `directory_repo_id`, expands
`CONTAINS` to Files, aggregates by Directory, and returns a page. The
Directory's `repo_id` can change in a committed projection phase before old
File edges are pruned. The old statement counted those Files under the new
owner even when each File's own `repo_id` named another repository.

## Theory and correctness

The read-only shim used the full production statement, including two
`UNWIND` repository ids, language filtering, aggregation, projection, order,
and limit. Each isolated NornicDB store had two repositories, 2,000
Directories, 4,000 Files, 4,000 `CONTAINS` edges, and an `ONLINE`
`directory_repo_id` index. Each Directory had one owned and one stale
cross-repository File edge. The returned page had 100 rows because these
builds apply the 50-row bound per unwound id.

| NornicDB image | Old count | `AND f.repo_id = rid` | `AND f.repo_id = d.repo_id` | File owner in pattern |
| --- | ---: | ---: | ---: | ---: |
| v1.2.1, `eshu-nornicdb-pr290:3722b483c02c` | 2 | no rows | no rows | 1 |
| v1.3.1, digest `sha256:ac524899…` | 2 | no rows | 1 | 1 |
| v1.3.3, digest `sha256:74a8ed7b…` | 2 | no rows | 1 | 1 |

All three builds returned 100 rows for the selected pattern. The ordinary
trailing equality is rejected: it silently drops the whole result on every
build. Equality against `d.repo_id` is also rejected because v1.2.1 drops
the result. The compatible query is
`MATCH (d:Directory {repo_id: rid})-[:CONTAINS]->(f:File {repo_id: rid})`.
On v1.3.1, ownerless Files raised the old count from 2 to 3 but did not
change the selected pattern's count of 1. A Directory with no owned File is
absent, not returned with a zero count.

Before the backend-priority change, a temporary NornicDB production-path live
regression first failed on pinned v1.3.3 with
`file_count = 3` instead of 1: one owned File, one stale cross-repository
File, and one ownerless File. It passed after the File-pattern edit. The
existing five Directory live cases and the new regression passed on each of
v1.2.1, v1.3.1, and v1.3.3. This proves the behavior through the handler's
build, graph read, sort/truncate, and repository-name lookup, not by comparing
only a copied query string. The live proof uses only disposable local stores.
After a later base rebase, the six-case suite passed again on each pinned build.
The new fixture uses a per-run nonce, removes only its tagged nodes before
driver close, and checks for residue; its test passed twice in one isolated
v1.3.3 run. That new NornicDB-only test was removed from the final diff;
the retained recurring regression is the Neo4j-only CI test below. The
historical NornicDB correctness and timing observations remain evidence,
not a claim of continuing NornicDB-specific test coverage.

On a disposable Neo4j 2026 Community store, a torn-edge fixture with one
owned, one cross-repository, and one ownerless File returned `file_count=3`
under the old production match and `file_count=1` under the File-owned match.
Both statements were run with `PROFILE`. The separate
`TestLiveNeo4jDirectoryLanguageQueryCountsOnlyOwnedFiles` regression drives
the production handler on that Neo4j backend: restoring the old match made
it fail with `file_count=3` (expected 1), then restoring the File-owned match
passed with `-count=2`;
it deletes only its nonce-tagged nodes and asserts no fixture residue. The
live-test ledger registers this fixture as a Neo4j-only CI row under the
runner's existing shared `live_nornicdb_answer_truth` build tag, so the
blocking live-backend workflow executes it on a fresh Neo4j store.
This local correctness fixture is separate from the read-only ops-qa timing
corpus below.

## Before and after

Performance Evidence: five interleaved same-store/same-corpus query-shim
measurements on NornicDB v1.3.1, v1.3.3, and ops-qa Neo4j. The NornicDB figures are HTTP query
response medians on the synthetic fixture above, not full-handler timings or
planner output. Its `EXPLAIN` and `PROFILE` endpoints returned no usable plan
or timing. Both tested releases improved on this fixture; no NornicDB
latency regression was observed. The v1.2.1 check proves correctness, but
does not make a latency claim.

| Backend and read shape | Before median | After median | Result |
| --- | ---: | ---: | --- |
| NornicDB v1.3.3, two repositories, limit 50 | 110 ms | 63 ms | 100 rows, count 2 → 1 |
| NornicDB v1.3.1, two repositories, limit 50 | 137 ms | 99 ms | 100 rows, count 2 → 1 |
| ops-qa Neo4j 2026.08.1, one PHP repository, limit 50 | 16 ms | 21 ms | 50 rows, unchanged on live data |

The Neo4j measurements used read-only `PROFILE` on the same populated graph
with `directory_repo_id` `ONLINE`: 9,260 matching File edges in the selected
repository, all 9,260 owned, zero mismatched. The five interleaved old/new
times in milliseconds were `24/29`, `16/21`, `16/21`, `16/22`, and `17/20`.
Both plans retained `NodeIndexSeek` on `Directory(repo_id)` using
`directory_repo_id` (2,956 seek rows, 2,957 seek hits), with no
`NodeByLabelScan`. The new predicate increased total DB hits from 54,734
to 66,779 (+12,045, +22%) and median statement time by 5 ms. Every run
was below the requested 1 s Neo4j budget (worst 29 ms). This is a
correctness win with a measured read cost, not a speedup claim. The live
scope had no cross-repository edges, so its unchanged row set is a
performance control; the synthetic torn-edge fixture proves the intended
accuracy delta. Statement PROFILE does not establish the full HTTP/MCP
endpoint's latency.

The broad local `make pre-pr` gate passed on head `1bd36385` before later
unrelated base rebases. Its B-7 corpus reported 570 required passes, zero
dead letters, and 363 s total against a 1,800 s ceiling. The first-drain
and maintenance phase times raised advisory warnings (131 s and 202 s),
but those phases run before the API starts and do not exercise this read.
Their Apple Silicon baseline is not comparable to this Linux x86-64 host;
the cause of the phase variation is unknown. Final-head CI B-7 remains a
merge gate, not a latency claim for this query.

No-Observability-Change: the route keeps `SpanQueryLanguageQuery` with
`http.route` and `eshu.capability`, the graph-reader query-duration metric,
and the `language query directory read` debug log with repository count,
rows returned/kept, and named-repository count. No graph write, retry, or
transaction boundary changed. The operator can still distinguish a wide
grant from excess backend rows and inspect the graph query-duration signal.

The local NornicDB source checkout at `orneryd/NornicDB` commit `81542d88`
shows top-level `UNWIND` routing in `pkg/cypher/unwind_routing.go` and
traversal predicate evaluation in `pkg/cypher/traversal.go`; it is not
proven to be the source of every tested image, so image behavior above is
authoritative. The ops-qa Neo4j `PROFILE` comparison was read-only. The local
disposable Neo4j regression seeded and removed only its nonce-tagged nodes.
Disposable test containers and the temporary ops-qa port-forward were removed
after proof.
