# #6786: Eshu exposure to NornicDB v1.3.3 defects (orneryd/NornicDB#400–#409 and eight related shapes)

This records which production Cypher paths hit the NornicDB v1.3.3 defects in
orneryd/NornicDB#400–#409, plus eight more shapes (X1–X8) found during the
audit. It covers what each classification rests on and what changed. Tracking:
#6786 (epic #6788). Background: [#6689/#6690 answer truth](6689-6690-nornicdb-v133-answer-truth.md).

## Pins and method

| Item | Value |
| --- | --- |
| NornicDB | `timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f` |
| Neo4j (oracle) | `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` (2026.08.1) |
| Driver | `github.com/neo4j/neo4j-go-driver/v5` v5.28.4, the version `go/go.mod` pins |
| Schema | Eshu's full NornicDB schema (337 statements from `graph.EnsureSchemaWithBackend`) applied before every NornicDB case |
| Isolation | Both containers recreated for every case; no reuse between cases |
| Base | `origin/main` at `bb6c8fb70` |

Each case runs the same seed and probe on both backends and compares the
rows. Only rows count as evidence. NornicDB's Bolt summary counters report
`properties_set: 0` for writes that did set properties, so counters are not
used.

Three setup effects produced false signals during the audit. The
classifications below account for all of them:

- **No schema.** Without Eshu's schema, the canonical File update-existing
  statement fails with `UNWIND mutation failed: start node variable 'd' not in
  context`, and typed relationship `count(r)` drifts after `DETACH DELETE`.
  Neither happens once the schema is applied, and production always applies
  it at bootstrap.
- **Container reuse.** Wiping a reused container with
  `MATCH (n) DETACH DELETE n` leaves NornicDB's relationship counters negative
  (-4, -15 and -19 were observed).
- **One transaction for seed and probe.** In a single managed transaction,
  NornicDB does not see nodes created earlier in the same transaction through
  an indexed `MATCH`. Production commits each writer phase separately.

The static audit covered every non-test Go file under `go/` that contains
Cypher (347 files), split across four auditors, with fragment-built statements
rendered as sent on the NornicDB route. A second, independent audit covered
the same ground; findings were merged and every hit re-proven with the method
above.

## Defect shapes

| Id | Shape | NornicDB v1.3.3 behaviour |
| --- | --- | --- |
| #400 | function call in `RETURN` after `MATCH … WITH … MATCH` | 0 rows, or one all-null row |
| #401 | `WITH … WHERE` using `IN` / `STARTS WITH` / `ENDS WITH` / `CONTAINS` | filter not applied |
| #402 | `UNWIND … MATCH … WHERE [NOT] EXISTS {…}` | predicate not evaluated |
| #403 | inline property map or identity inequality inside `EXISTS {…}` | ignored |
| #404 | `n.id` on a node-only `MATCH` when `id` is absent | internal node id instead of null |
| #405 | string `+` with an `UNWIND` variable | expression text returned or stored |
| #406 | subscript on an `UNWIND` variable | expression text or a list |
| #407 | list literal wrapping a variable | expression text stored |
| #408 | `UNWIND … AS v WITH v …` | rest of the statement ignored |
| #409 | `MATCH … WHERE … STARTS WITH/ENDS WITH … CREATE` | nothing written |
| X1 | `UNWIND` over a relationship list built by `collect(rel)` after `OPTIONAL MATCH`, or by a list comprehension over collected relationships | 0 rows; `DELETE` removes nothing |
| X2 | one `CREATE` of a multi-hop path through an inline middle node | duplicate middle node (4 nodes vs 3) |
| X3 | untyped global `MATCH ()-[r]->() RETURN count(r)` after a `DETACH DELETE` that removes both endpoints of an edge | 0 where Neo4j returns 2 (typed and endpoint-bound counts are correct) |
| X4 | `AND` or `OR` in a `WHERE` directly preceded by a newline or a tab instead of a space (for example gofmt-indented `\n\t\tAND`) | whole `WHERE` mis-evaluated: `MATCH (n:Workload) WHERE n.id = $x\n\tAND n.repo_id = $r` returns 0 rows where Neo4j returns the row; with a pattern disjunct the first condition can be dropped instead. `\n AND` and `\n\t AND` are correct |
| X5 | `EXISTS {…}` over a backward (n-last) pattern with an inner `WHERE` (`=` or `IN`) | inner predicate ignored (the one-hop outbound form is correct) |
| X6 | `MATCH (n) RETURN labels(n) AS l, count(*) AS c` on an empty graph | spurious `{c: 0, l: null}` row |
| X7 | `EXISTS {…}` used as a `RETURN` column | always `false` |
| X8 | `UNWIND … MERGE (n:L {k: v})` as the statement's final clause | `k` stored as null (a following `SET`, or `CREATE`, is correct) |
| X9 | an `UNWIND` variable name that equals a `RETURN` alias, with a `MATCH` in between | the first `RETURN` column comes back named after the first `UNWIND` value (e.g. the literal key `'r1'`) instead of its declared alias; Neo4j returns the declared alias |

## Exposure

| Path | Shape | Classification | Evidence |
| --- | --- | --- | --- |
| Value-flow cloud sink loader (`reducer/code/value/cloud_sink_*`) | #400 | Fixed by #6761 | #6689/#6690 evidence |
| `graph.ResetRepositorySubtreeInGraph` | #408 (step 1), X1 (step 2) | Affected; test-only callers. Deleted with the rest of `graph/mutations.go` | NornicDB keeps 4 owned nodes and 3 relationships that Neo4j removes |
| `graph.DeleteRepositoryFromGraph`, `graph.DeleteFileFromGraph` | #408 (claimed earlier) | Not affected; test-only callers. Deleted | Same nodes and edges as Neo4j after the delete |
| Canonical File update-existing (`canonicalNodeFileUpdateExistingCypher`, root variant) | #408 (`MATCH`-seeded `WITH`) | Not affected with the schema applied | Two-generation writes match Neo4j row for row |
| Canonical File create-missing (`canonicalNodeFileCreateMissingCypher`, root variant) | #402, #403 | Affected, latent. The `NOT EXISTS` guard is a no-op with or without the schema, but the graph stays correct because update-existing runs first with the same rows | Read-only twin returns the existing file on NornicDB (2 rows vs 1) |
| Two read paths | X4, X5 | Affected; fixed in a separate change | Recorded in that change |
| Repository list `is_dependency` (`querycontract.RepositoryDependencyMarkerProjection`) | X7, plus invalid scoped Cypher | Affected. Unscoped: always false on NornicDB. Scoped: a syntax error on Neo4j | Fixed in #6786; RED/GREEN in [6786-repository-dependency-marker-and-relationship-repo-anchor.md](6786-repository-dependency-marker-and-relationship-repo-anchor.md) |
| Code relationship lookup by name and `repo_id` (`codequery/relationship_handlers.go`) | X5 | Affected: other repositories' entities are returned | Fixed in #6786; RED/GREEN in [6786-repository-dependency-marker-and-relationship-repo-anchor.md](6786-repository-dependency-marker-and-relationship-repo-anchor.md) |
| Workload-instance retraction lookup (`cmd/reducer/workload_instance_retraction_lookup.go`, `neo4jWorkloadInstanceRetractionLookup.ListWorkloadInstances`) | X9 | Affected: `ListWorkloadInstances` returns nothing on NornicDB, so `reducer.ReconcileWorkloadInstanceRetraction` never retracts a stale `WorkloadInstance` node superseded by #5473's environment-alias canonicalization | Fixed in #6786 (renamed the `UNWIND` binding to `requested_repo_id`); live RED/GREEN in `cmd/reducer/nornicdb_workload_instance_retraction_lookup_live_test.go` |
| Infra grant predicate `DEPLOYMENT_SOURCE` disjunct (`infraResourceScopeCoreDisjuncts`) | X5 candidate | Not affected (one-hop outbound) | Rows match on one line and multi-line |
| Entity resolve scoped `EXISTS` (`entity.BuildResolveEntityGraphQuery`) | X5 | Unreachable: the branch never renders | Function returns early when not repository-anchored |
| Taint backfill `CountTaintFlowsToEdges`, relationships catalog tiles, golden-corpus `CountEdges`/`CountCorrelation` | X3 | Not affected: typed counts are correct with the schema | Counts match Neo4j after a both-endpoint `DETACH DELETE` |
| Reducer gauge `eshu_dp_edges_by_source_tool` (`storage/cypher/provenance_counts.go`) | X3 | Not affected: typed count grouped by `r.source_tool` | Rows match after a both-endpoint `DETACH DELETE` |
| Deployment evidence incoming read (`repository/deployment_evidence.go`) | #400 (literal in `RETURN`) | Not affected | Rows match |
| Language query Directory branch (`language/cypher.go`) | #408, #404 | Not affected | Rows match (`entity_id` null on both) |
| Orphan sweep Module composite read (`orphan_sweep_queries.go`) | #408 (`MATCH`-seeded `WITH`) | Not affected | Rows match |
| Every Cypher literal with `AND`/`OR` directly after a newline or tab | X4 | Only the two read paths above and the unreachable resolve branch; the four other scan hits are Postgres SQL | Static scan of all production Go Cypher literals |
| Every other production statement | #401, #405, #406, #407, #409, X1, X2, X6, X8 | No production statement has the shape | Static audit; X8 scan found only one statement ending in a relationship `MERGE`, which is correct |
| #404 | `.id` on node-only `MATCH` | No reachable exposure | Labels that can lack `id`: File, Directory, Module, Environment, CodeownerTeam, Rationale, DocumentationSection, KustomizeOverlay, ShellCommand, Parameter |

## Upstream

X1–X9 are outside orneryd/NornicDB#400–#409. Upstream reports for them are
drafted but not yet filed; #6787 tracks upstream fixes and the re-proof on the
next pin.

## X9 fix: workload-instance retraction lookup

No-Regression Evidence: the fix renames the `UNWIND` binding
(`repo_id` -> `requested_repo_id`) and updates the one `MATCH` that reads it;
the `RETURN` clause, its aliases, the `WHERE` filter, and the bound
parameters (`$repo_ids`, `$evidence_source`) are byte-identical to before.
A bound-variable rename does not change the query plan (same labels, same
property lookups, same `DISTINCT`), so this is a correctness fix with no
throughput or latency claim to make; the live test asserts row content, not
timing.

Observability Evidence: no new signal is needed. The lookup itself carries
no telemetry of its own (it is a plain graph read behind
`reducer.ReconcileWorkloadInstanceRetraction`); the caller,
`WorkloadMaterializationHandler.Handle`
(internal/reducer/workload_materialization_handler.go), already times and
counts the whole instance-retraction stage (`timing.instanceRetract`,
`instanceRetractRows`) regardless of how many rows the lookup returns, so a
previously-silent zero-row read now correctly shows up as nonzero
`instanceRetractRows` there once stale instances exist to retract, with no
handler code change required.
