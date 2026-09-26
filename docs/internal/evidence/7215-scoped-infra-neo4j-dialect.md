# #7215 Scoped Infra Search And Relationships On Neo4j

Scoped-token `POST /api/v0/infra/resources/search` and
`POST /api/v0/infra/relationships` blew the 10 s bounded-read budget on Neo4j.
The SHAPE-A grant predicate renders O(grant) inline pattern terms, and the
search copied it into all 27 `CALL { UNION }` branches (relationships: 3
aliases on each of up to 15 anchor statements). The fix splits by
`InfraHandler.GraphBackend`: Neo4j gets one hoisted list-`EXISTS` predicate
plus an unscoped per-label anchor probe; NornicDB keeps SHAPE-A byte for byte.
Design: [Scoped Grant Predicates](../../public/reference/cypher-scoped-grant-predicates.md).

## Setup

- Image: `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`
  (the `docker-compose.neo4j.yml` pin), private container `perf7215impl-neo4j`,
  heap 512m/512m, pagecache 512m, `db.transaction.timeout=300s`. linux/amd64
  under emulation on an Apple M4 Pro: absolute seconds are inflated, the same
  container serves every row.
- Schema: 137 of 139 Eshu DDL statements (the 2 multi-line fulltext rows fail
  to extract; the searched predicates do not use them).
- Graph: the #7215 proof's `seed.py` (32,584 nodes, 1,864 edges; 30 repos, 27
  infra labels x 40 nodes per repo, orphan and unmatched negatives).
- Grants: g5 = `repo-00..04` + `git-repository-scope:00..04`; cap = 64 + 64 =
  128 scalars. Search body `{"query":"api"}`; relationships on
  `TerraformStateResource:0:0` (hit on the 6th anchor label) and
  `does-not-exist` (full miss).
- Driver: `TestLiveInfraScopeNeo4jColdWarmBudget`
  (`go/internal/query/infra_scope_neo4j_timing_live_test.go`). After = the real
  handler on the real `Neo4jReader` (10 s budget enforced), cold =
  `CALL db.clearQueryCaches()` first, warm = the same request again, 3 runs.
  Before = the SHAPE-A statements today's handler sends, run outside the
  handler with a unique `/* nonce */` prefix after clearing the query cache,
  bounded at 120 s per statement, 1 run.

## Results

Performance Evidence: scoped infra search and relationships on Neo4j,
before (SHAPE-A) and after (Neo4j list-EXISTS dialect), wall seconds.

| Read | Grant | Before cold | Before warm | After cold (3 runs) | After warm (3 runs) |
| --- | --- | --- | --- | --- | --- |
| search `api` | g5 | 22.46 | 2.58 | 2.61 / 2.89 / 2.49 | 0.26 / 0.28 / 0.23 |
| search `api` | cap | DNF: heap OOM at 90.0 s | DNF: heap OOM at 124.6 s | 2.85 / 2.54 / 2.65 | 0.23 / 0.23 / 0.24 |
| relationships hit | g5 | 10.52 | 0.12 | 0.55 / 1.59 / 0.35 | 0.02 / 0.11 / 0.01 |
| relationships hit | cap | DNF: heap OOM at 51.0 s | DNF: heap OOM at 9.6 s | 0.89 / 0.24 / 0.28 | 0.02 / 0.01 / 0.02 |
| relationships miss | g5 | 25.21 | 0.17 | 0.40 / 0.35 / 0.33 | 0.03 / 0.03 / 0.03 |
| relationships miss | cap | DNF: heap OOM at 36.7 s | DNF: heap OOM at 9.5 s | 0.40 / 1.65 / 0.48 | 0.05 / 0.16 / 0.04 |

At the cap every SHAPE-A statement failed with
`Neo.TransientError.General.OutOfMemoryError (Java heap space)` on the compose
512m heap, on its first statement, before the 120 s bound; the relationships
"warm" rows follow an OOM and only show the heap had not recovered. Every after
run answered inside the 10 s budget (the test fails a 504 or any
run of 10 s or more). The before numbers agree with the proof's round-robin
medians (search g5 33.65 s cold / 2.56 s warm; relationships g5 miss
35.9-50.0 s cold) within the host-load spread. The Neo4j statement text is
identical for every grant size, so one plan-cache entry serves every token.

## Equivalence and authorization

`go/internal/query/infra_scope_neo4j_equivalence_live_test.go` seeds its own
nonce-prefixed fixture on the same container and drives the real handler:

- search row sets equal SHAPE-A and an oracle computed in Go from the fixture:
  g1 10 rows, g5 25, cap (140 scalars, past the 128 cap) 32. At the cap the
  SHAPE-A reference is its predicate hoisted after the `CALL`, because the
  per-branch SHAPE-A statement does not plan on Neo4j at that size;
- no ungranted-used or orphan `CloudResource`, no unmatched or
  ungranted-matched `TerraformStateResource`, no ungranted-owned `K8sResource`
  or `TerraformResource` in any result;
- relationships on 8 ungranted anchors (labeled: `CloudResource`,
  `TerraformStateResource`, `TerraformResource`; unlabeled fallback:
  `K8sResource`, `HelmChart`) return a 404 byte-identical to a nonexistent id,
  at every grant; 4 granted anchors return 200 with every neighbour granted,
  and match the SHAPE-A replay for g1 and g5; the planted ungranted neighbours
  on `tf-repo-00` (outgoing and incoming) never appear.

## Observability

Observability Evidence: both request spans carry `eshu.infra_scope_dialect`
(`unscoped`, `shape_a`, `neo4j_list_exists`); the Neo4j relationships loop adds
`eshu.entity_anchor_probes` and `eshu.entity_anchor_scoped_reads` next to the
existing `eshu.entity_anchor_labels_tried`
(`TestInfraScopeDialectSpanAttributes`). The #5408 cap counter
`eshu_dp_query_scope_grant_inline_capped_total` and its warning still fire once
per read, unchanged. No metric is added.

## Limits

- Emulated amd64 on arm64; host 1-minute load 3.9-14 during the runs.
- `seed.py` is small (32.6k nodes); execution cost is the same free-text label
  scan for every variant, so the claim is planning cost, not scan cost.
- NornicDB was not re-measured: its statements are unchanged (digest-pinned).
