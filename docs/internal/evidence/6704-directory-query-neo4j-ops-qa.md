# 6704 / 6541: the directory language query on Neo4j at ops-qa scale

This record measures `buildDirectoryCypher` (`go/internal/query/language/cypher.go`)
and its handler, `Handler.directoryRowsByLanguage`
(`go/internal/query/language/directory.go`), on the backend ops-qa runs. It
answers the open question in both issues. #6704 predicted that the unscoped
Directory read fans out `len(repoIDs) * limit` rows and misses the 10 s
deadline at reference scale. #6541 asked for a corpus-scale timing with
cardinality.

Every earlier measurement of this statement
([6541 corpus timing](6541-directory-query-s2-corpus-timing.md),
[6541 S2](6541-directory-query-s2.md)) ran on pinned NornicDB against a
synthetic 50-repository corpus. This run uses Neo4j and the real ops-qa graph.

## Result

The #6704 fan-out does not occur on Neo4j. There, `ORDER BY ... LIMIT` after
the aggregation is one global `Top` operator, so the statement returns at most
`limit` rows however many repository ids it unwinds. At 804 repositories the
unscoped read answers in under 1 s cold and at warm p95 on every measured cell,
through both the HTTP route and the MCP tool. It returns the same rows as an
independent oracle.

The per-unwound-id row bound that #6704 measured is a NornicDB behavior. That
backend is no longer the one the deployment runs. `sortAndTruncateDirectoryRows`
still cuts the NornicDB superset to `limit`, and on Neo4j it is a no-op. The
Go-side bound stays as it is; this record changes no code.

## Identity

- Cluster: ops-qa, namespace `eshu`. Identity checked with
  `aws sts get-caller-identity --profile ops-qa` before the run. Account and
  repository identifiers are omitted.
- API and MCP image: `ghcr.io/eshu-hq/eshu:sha-5583d45@sha256:e4014933cfd00bdf0da38b11d01153a32936988210a4826f18b22c7cbc5e9b27`.
  `git diff 5583d45 origin/main` over `language/cypher.go`, `language/directory.go`
  and `language/handler.go` is empty at `049be7161`, so the deployed statement
  and handler are the ones on `main`.
- Graph: `neo4j:2026.08.1-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`,
  Cypher 25, slotted runtime.
- Corpus: 804 Repository, 43,281 Directory, 144,010 File nodes. The largest
  languages by File count are php 45,415, hcl 23,772, javascript 22,815 and
  typescript 11,983. The `directory_repo_id` RANGE index on `Directory(repo_id)`
  is ONLINE.
- Access: all reads were read-only. Cypher went through `cypher-shell
  --access-mode read` on the Neo4j pod. HTTP and MCP calls were the read-only
  `POST /api/v0/code/language-query` and `tools/call execute_language_query`,
  made with the unscoped admin key over `kubectl port-forward`.
- `absolute_target_applicable: true` for ops-qa. It holds 804 repositories,
  against the 896 of the reference profile. Repository count is not the cost
  axis. The cost follows directory and CONTAINS volume (see
  [Width slope](#width-slope)), and ops-qa's volume per repository is not
  known to match the reference profile's. The applicability rests on headroom
  instead: the unscoped statement uses about a third of the 1 s budget, so a
  reference corpus up to about 2.9 times ops-qa's directory and edge volume
  still fits.

## Plan

`PROFILE` of the shipped statement, unwinding all 804 repository ids, with
language php and limit 200:

| operator | rows | db hits |
| --- | ---: | ---: |
| `Unwind $repo_ids AS rid` | 804 | 0 |
| `NodeIndexSeek` `directory_repo_id` (`d.repo_id = rid`) | 43,281 | 44,085 |
| `Expand(All)` `(d)-[:CONTAINS]->(f)` | 180,335 | 267,209 |
| `Filter` `f.repo_id = rid AND f.language IN $languages AND f:File` | 44,750 | 445,678 |
| `EagerAggregation` `d, count(f)` | 7,561 | 0 |
| `Top` `file_count DESC, repo_id ASC, name ASC LIMIT $limit` | 200 | 0 |

The total is 772,706 db hits. The server-side `Time` over three runs was 318,
312 and 311 ms (ledger:6704-opsqa-directory-unscoped-server-ms). At limit 50,
`Top` emits 50 rows over the identical upstream plan
(ledger:6704-opsqa-directory-plan-php-limit50). hcl has the same shape: the
Filter keeps 23,677 rows and the aggregation 6,200
(ledger:6704-opsqa-directory-plan-hcl).

This is the plan the query-plan profile gate already asserts on an isolated
Neo4j. `QP-LANGUAGE-DIRECTORY` in
`go/internal/queryplan/testdata/handler-hot-cypher.yaml` pins the statement's
hash, `required_anchors: Directory.repo_id`, `required_schema:
directory_repo_id` and the `indexed_plan` block. The `language-directory/*`
variants in `handlerQueryplanSafeCypherVariants`
(`go/internal/query/queryplan_production_variants_test.go`) put the statement
under `TestProductionQueryplanProfilesRejectWholeGraphScans`. The second
closure option in #6541 was therefore already met on `main`. What was missing
was the corpus-scale plan, and the table above supplies it: the anchor is an
index seek at 804 repositories, not a Directory label scan.

## Latency through the product surfaces

Each cell is one cold call followed by ten warm calls on the same arguments,
measured client-side. Warm p95 is the nearest-rank percentile. With ten warm
calls it equals the slowest warm call, so read it as the worst of ten, not as
a stable percentile. The two cells closest to the budget were repeated with
thirty warm calls (below). The time covers the whole handler path: `allRepositoryIDs`,
the statement, the Go re-sort and truncate, and the `repo_name` read.

| surface | language | limit | query | rows | cold | warm p50 | warm p95 |
| --- | --- | ---: | --- | ---: | ---: | ---: | ---: |
| HTTP | php | 50 | | 50 | 0.407 s | 0.392 s | 0.408 s |
| HTTP | php | 200 | | 200 | 0.422 s | 0.419 s | 0.440 s |
| HTTP | hcl | 200 | | 200 | 0.439 s | 0.385 s | 0.416 s |
| HTTP | javascript | 200 | | 200 | 0.351 s | 0.381 s | 0.440 s |
| HTTP | typescript | 200 | | 200 | 0.425 s | 0.413 s | 0.451 s |
| HTTP | python | 200 | | 200 | 0.398 s | 0.401 s | 0.433 s |
| HTTP | go | 200 | | 25 | 0.379 s | 0.399 s | 0.530 s |
| HTTP | java | 100 | | 100 | 0.417 s | 0.396 s | 0.433 s |
| HTTP | php | 200 | `src` | 13 | 0.345 s | 0.232 s | 0.382 s |
| HTTP | javascript | 50 | `js` | 50 | 0.434 s | 0.280 s | 0.511 s |
| MCP | php | 200 | | 200 | 0.475 s | 0.684 s | 0.817 s |

The largest HTTP warm p95 is 0.530 s (ledger:6704-opsqa-directory-api-warm-p95-max).
That ledger row covers the eight cells without a `query` filter. The two
`query` cells (`src`, `js`) have their own row
(ledger:6704-opsqa-directory-api-query-cells).
The MCP tool was the slowest surface in this run, at 0.817 s warm p95
(ledger:6704-opsqa-directory-mcp-warm-p95). The MCP figure includes the tool
server's own envelope and resource rendering on top of the same HTTP route.
Three read-only diagnosis probes for other issues were running against
ops-qa's Neo4j during that run.

Repeated with thirty warm calls:

| surface | language | limit | rows | cold | warm p50 | warm p95 | warm max |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: |
| MCP | php | 200 | 200 | 0.424 s | 0.447 s | 0.479 s | 0.490 s |
| HTTP | go | 200 | 25 | 0.390 s | 0.395 s | 0.441 s | 0.535 s |

The HTTP go/200 repeat has its own row
(ledger:6704-opsqa-directory-http-go200-warm-p95-n30). The MCP repeat
(ledger:6704-opsqa-directory-mcp-warm-p95-n30) puts that
surface at 0.479 s warm p95. Across both runs the MCP path is 0.45-0.82 s. It
stays under 1 s in both, but the first run shows it is sensitive to other load
on the shared graph.

Scope. These cells are the Directory branch only (`entity_type: directory`).
The same route and MCP tool also appear as offenders in #7098, with their
slow calls on other entity types (the sweep's `entity_type: function`,
repository-scoped). Those calls, code inventory and the `by-language` and
`language-inventory` routes are tracked in #7247 and are not measured here.
The 1 s figure is the read budget #7098 applies to every endpoint.

The unscoped admin caller is the widest grant the route can serve. A scoped
caller unwinds its own granted ids, which are a subset of these 804, so its
statement reads a subset of the rows above.

## Rows equal to an independent oracle

The shipped statement ran with `limit` at 1,000,000, so no row was cut. Its
output was compared with a structurally different statement: a Directory label
scan, `MATCH (d:Directory)-[:CONTAINS]->(f:File) WHERE f.language IN $languages
AND f.repo_id = d.repo_id AND d.repo_id IS NOT NULL WITH d, count(f)`. The
comparison is the multiset of `(repo_id, name, file_count)`.

- php: 7,561 rows on each side, and the multisets are equal
  (ledger:6704-opsqa-directory-rows-equal-php).
- hcl: 6,200 rows on each side, and the multisets are equal
  (ledger:6704-opsqa-directory-rows-equal-hcl).
- The HTTP php page at limit 200 equals the oracle's first 200 rows under
  `file_count DESC, repo_id ASC, name ASC`, in the same order. `repo_name` is
  filled on every one of the 200 rows.

The oracle requires File ownership to match Directory ownership
(`f.repo_id = d.repo_id`), the same rule #6703 added to the shipped statement.
So the equality also shows that the inline `f:File {repo_id: rid}` form admits
exactly the owned files on Neo4j.

## Width slope

`PROFILE` time for the same statement, unwinding the first N repository ids
(sorted by id), language php. Three runs each:

| repository ids | db hits | server time |
| ---: | ---: | --- |
| 100 | 95,595 | 71, 39, 38 ms |
| 200 | 189,184 | 75, 74, 76 ms |
| 400 | 298,107 | 119, 122, 121 ms |
| 804 | 772,706 | 318, 312, 311 ms |

The cost follows the Directory and CONTAINS volume of the repositories
unwound, not the repository count itself. The ids above 400 hold the heavier
repositories. At about 0.4 µs per db hit, and with about 0.1 s of fixed handler
cost measured over the HTTP route, the unscoped read reaches 1 s near
2.2 million db hits. That is roughly 2.9 times the current corpus's directory
and edge volume. That is the next bottleneck to watch. Past that point, the
materialized per-repository directory count that #6704 lists becomes the
candidate, and it would need its own theory proof.

## What this changes for the issues

- #6704: the predicted fan-out and the missed deadline do not reproduce on
  Neo4j at 804 repositories. The acceptance asked for three correctness
  properties at the reference profile: a correct `file_count` under nesting,
  `within_limit`, and a filled `repo_name`. The oracle comparison and the
  page check above show all three. The three-key total order is unchanged.
- #6541: the corpus-scale timing with cardinality is above, and the statement
  was already under the plan-profile gate on `main`.
- NornicDB: the per-unwound-id bound, and the NornicDB timings in the 6541
  records, remain true for that backend. They were not re-measured, because
  the deployment's backend is Neo4j.

Performance Evidence: `buildDirectoryCypher` as shipped at `049be7161` ran on
ops-qa Neo4j 2026.08.1-community against 804 repositories, 43,281 directories
and 144,010 files, with the `directory_repo_id` index ONLINE. An unscoped php
read at limit 200 made 772,706 db hits in 311-318 ms of server time. Through
HTTP, cold was 0.345-0.439 s and warm p95 0.382-0.530 s over ten cells. Through
MCP, cold was 0.424-0.475 s and warm p95 0.479-0.817 s over two runs. Rows equal an independent
Directory-scan oracle.

No-Observability-Change: this record changes no code. The existing
`language query directory read` debug log (`Handler.logDirectoryRead`, fields
`repositories`, `rows_returned`, `rows_kept`, `repositories_named`) still
separates grant width from backend over-return. The debug log was not read
in this run. Because the plan's global `Top` emits at most `limit` rows, on
Neo4j `rows_returned` should equal `rows_kept`.
