# NornicDB v1.3.3 answer truth and value-flow cloud sinks (#6689, #6690)

## Pins

| Artifact | Value |
| --- | --- |
| Eshu base | `f89b05014` (`origin/main`, 2026-09-18) |
| NornicDB image (Compose, Helm `values.yaml`, `scripts/verify-replay-tier.sh`) | `timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f` |
| NornicDB runtime | linux/amd64, `CALL dbms.components()` → `NornicDB 1.3.3`, fresh container, Compose environment (`NORNICDB_ASYNC_WRITES_ENABLED=false`, embeddings and search off), database `nornic` |
| Neo4j positive control | `neo4j:2026-community` (`sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f`, Neo4j 2026.08.1), database `neo4j` |

## #6689: every production statement returns the exact rows

Each row in the issue was re-run through the production function or HTTP
handler against a seed with ground truth known by construction. The tag-gated
live tests are committed:

```bash
cd go && ESHU_NEO4J_URI=bolt://127.0.0.1:27687 go test -p 1 -tags live_nornicdb_answer_truth \
  ./internal/query ./internal/query/repository ./internal/query/entity ./internal/query/codequery \
  -run 'AnswerTruth' -count=1 -v
```

`-p 1` matters: the packages share one database, and the unscoped A5 count
reads every `Platform` node, so running the packages in parallel lets one
package's seed change another's answer. Each package also uses its own id
prefix (`answer-truth-query:`, `-repo:`, `-entity:`, `-code:`), so one package's
cleanup never deletes another's seed.

| Row | Production path | Returned on v1.3.3 | Expected |
| --- | --- | --- | --- |
| A1 | `graphSummaryRepoEcosystemCounts` platform_count | 1 | 1 (3 raw paths) |
| A2 | `graphSummaryRepoEcosystemCounts` dependency_count | 1 | 1 (2 edges) |
| A3 | `queryRepositoryPlatformCount` | 1 | 1 |
| A4 | `queryRepositoryDependencyCount` | 1 | 1 |
| A5 | `runEcosystemOverviewCounts` platform_count | unscoped 1; grant repo-a 1; grant repo-b 0 | same |
| A6 | `GET /api/v0/entities/{id}/context` | `file_path a.go`, real `repo_id`/`repo_name`, 3 CALLS with real ids | same |
| A7 | `POST /api/v0/infra/relationships` | incoming CONTAINS + 2 CALLS with real `source_id`; outgoing 1 CALLS | same |
| A8 | `listMostComplexFunctions`, unscoped, no repo_id | Main 7 a.go repo-b; Helper1 3 b.go repo-b; Orphan 2 with empty repo | same |
| A9 | `catalogWorkloadEnrichments` | instance_count 3, environments [prod staging] | same |
| A10 | `queryRepositoryGraphCoverageStats` | file_count 2, entity_count 3 | same |

A10 was still wrong on the earlier upstream `main` build `d50e0427`; it is
correct on the v1.3.3 release image. No #6689 statement changes.

## #6690: the loader returned nothing; now it returns the exact sinks

Root-Cause Evidence: on v1.3.3, cutting `CloudSinkTargetsCypher` clause by
clause on a seed with one single-workload function, one two-workload function
and one function whose action is not allowed: the aggregation, the
`size(workloads) = 1` filter, the `workloads[0]` subscript and the two-hop
`MATCH` each returned the right rows. Adding
`WHERE action.action IN sinkRel.actions` after the subscript-bound workload
returned 0 rows. The same predicate after a `WITH` passed the disallowed
function too; `any(x IN sinkRel.actions WHERE x = action.action)` was correct
with a narrow `RETURN` and returned 0 rows with the production `RETURN` items;
replacing the subscript with `UNWIND workloads AS workload` also returned 0 rows.
The same `IN` predicate without the subscript binding (plain `WITH`, or
`collect` then `UNWIND` with no filter) was correct. The single-statement form is
therefore not safe to build on, and the loader now runs two statements with the
single-workload check in Go.

Live proof through the real loader (`TestLiveCloudSinkLoader`,
`go/internal/reducer/code/value/cloud_sink_loader_live_test.go`):

| Build | Before (production `f89b05014`) | After |
| --- | --- | --- |
| NornicDB v1.3.3 | `targets: []` (FAIL) | `[{FunctionID:repo-apkgone Kind:iam_privileged_action Label:IAM effective privileged action}]` |
| Neo4j 2026.08.1 | — | the same single target |

The seed also carries a function in two workloads, a disallowed action, a
workload with no instance, and a duplicate `RUNS_IN` edge; none adds a target.
On Neo4j, the old single statement and the new pair return the same final row
(`vf-one, CAN_PERFORM, [CloudResource, S3Bucket], false`) on the same seed.

Backend conformance: `TestLiveBackendConformance` passed on both backends with
the value-flow statements and the #6689 shapes in the default corpora as exact
rows, and cleanup left no fixture nodes on either backend. The
`value-flow-conformance-expectation` gate is flipped from its inverted
expectation to a positive check (both lanes pass and log both value-flow cases);
`scripts/verify-value-flow-conformance-expectation.sh` passed on both live lanes,
and its test mirror fails when the marker check is removed. The gate stays
because `required-gates-complete` awaits it through the default branch's
registry.

The two statements are separate autocommit reads, so `RUNS_IN` can change
between them (PR #6761 review). The second statement therefore re-matches the
function, its action and the claimed workload, and returns every workload the
function runs in now on each row; the loader drops a pair unless all of them are
the claimed workload. A `NOT EXISTS { (fn)-[:RUNS_IN]->(other) WHERE other <>
workload }` form of the same check was correct on Neo4j and returned no rows at
all on NornicDB v1.3.3, so the check is carried in rows instead. The live loader
test changes `RUNS_IN` between the two reads (fn-one gains a second workload):
without the revalidation it returned the fn-one target on v1.3.3 (FAIL), with it
it returns none on v1.3.3 and on Neo4j.

## Performance and observability

No-Regression Evidence: `CloudSinkWorkloadRowsCypher` anchors on
`Function.uid IN $function_uids` (`function_uid_unique` constraint, NornicDB
`nornicdb_function_uid_lookup` index) and returns one row per
(function, action, workload); `CloudSinkTargetsByPairCypher` starts from
`UNWIND $pairs` and anchors on `Workload {id}` (`workload_id` unique constraint,
`nornicdb_workload_id_lookup` index), with every hop a single-hop `MATCH`. Both
run in batches of 500. Measured on NornicDB v1.3.3 over HTTP, one full batch of
500 functions (50 of them in two workloads), with those indexes present, 15
repetitions after one warm-up, on a fresh container with the revalidating
second statement: the old single statement took median 1.3 ms (min 1.1,
max 1.5) and returned 0 rows, so its timing is not a comparable answer; the new
path (statement 1, Go grouping, statement 2, Go revalidation) took median 8.5 ms
(min 6.6, max 10.4) and returned 550 workload rows, 450 pairs and 450 sink rows,
the exact expected result. Statement 1 alone took median 2.3 ms. Before the
revalidation columns were added the same path measured 6.2 ms median; the extra
`MATCH (fn)-[:RUNS_IN]->(current)` hop adds one row per current workload. The
loader runs once per value-flow fixpoint load, so the cost is two bounded,
index-anchored round trips per 500-function batch.

Observability Evidence: the loader logs one `value-flow cloud sink targets
loaded` line per load with `function_count`, `workload_row_count`,
`single_workload_pair_count`, `multi_workload_pair_dropped_count`,
`unresolved_pair_dropped_count`, `revalidation_pair_dropped_count`,
`sink_row_count` and
`cloud_sink_target_count`, wired through `cmd/reducer/value_flow_wiring.go`.
Graph read errors are wrapped and fail the reducer pass, as before.

## Other NornicDB v1.3.3 write defects seen while seeding

None of these shapes appears in an Eshu production writer (checked with `rg`
over non-test Go under `go/internal` and `go/cmd`); they are recorded for the
upstream report and so seeds avoid them:

- a single-quoted literal concatenated inside an inline `CREATE` property map
  (`CREATE (:V {id: 'v:' + v})`) stores the mangled text `v:' + 'x`; a
  double-quoted literal, and `SET n.k = "p:" + row.id`, are correct;
- a list subscript over an `UNWIND` row inside a `CREATE` map
  (`{id: row[0]}`) stores a list, not the element;
- `SET r.actions = [row.act]` stores the literal `["row.act"]`; the production
  shape `SET rel.actions = row.actions` stores the real list;
- `UNWIND $rows AS row WITH row WHERE … CREATE …` writes nothing;
- `MATCH … WHERE x.id STARTS WITH … CREATE (…)-[:R]->(…)` wrote no edges in a
  two-node probe while the same statement with `IN` or `MERGE` wrote both.
