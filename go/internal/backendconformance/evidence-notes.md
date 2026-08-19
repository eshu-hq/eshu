# Backend conformance evidence notes

## Value-flow cloud sink conformance pair

The production query `valueFlowCloudSinkTargetsCypher`
(`go/internal/reducer/value_flow_cloud_sink_loader.go`) returns zero rows on
NornicDB and the correct row on Neo4j 5.x community. It resolves which cloud
resources a function's cloud action can reach, and its failure is silent — no
error, just a graph missing that category of edge. Nothing in the repository
detected it.

Four independent backend divergences empty it, each measured against Neo4j 5.x
community with the same fixture on `eshu-nornicdb-pr290:3722b483c02c` and on
upstream `main`:

| Shape | Neo4j 5 | NornicDB |
| --- | --- | --- |
| `collect()` after two `MATCH` clauses | one row | no rows |
| `ws[0] AS w2` then `w2.name` | the property value | the literal text `"w2.name"` |
| `x IN relationship.listProperty` | matches | matches nothing |
| multi-hop `MATCH` whose anchor was bound by an earlier clause | one row | no rows |

Controls: two `MATCH` clauses without aggregation, aggregation after one
`MATCH`, `IN` over a *node* list property, reading the relationship property
directly, the same multi-hop pattern as one clause with a fresh anchor, and the
same bound anchor split into single-hop clauses all behave identically on both
backends. Filed upstream as orneryd/NornicDB#297, #298, #301 and #302.

**The list is not exhaustive, and its history says why.** It grew from two to
three to four as each review looked further along the statement, and every
addition was found by testing past where the previous round stopped. So this
case asserts that the production statement returns a row on a conforming
backend; it does not assert that these four are all the reasons it might not.
Fixing all four upstream is necessary for the query to work and has not been
shown to be sufficient.

The fourth has a workaround needing no upstream change: decomposing the compound
multi-hop `MATCH` into chained single-hop clauses works on both backends. That
is available to the production query today, independently of #297/#298/#301.

**A full rewrite avoiding all of them has been attempted and does not exist
yet.** Replacing `collect()`+subscript with a correlated
`WHERE NOT EXISTS { ... }` — the natural way to assert "exactly one workload"
without aggregation — runs straight into a fifth defect: correlated subqueries
ignore their inner predicate, so `EXISTS` admits every row and `NOT EXISTS`
admits none (orneryd/NornicDB#303, reproduced here with an uncorrelated control
and both polarities). Every alternative reached for so far has avoided one
defect by hitting another. Upstream is the path; a workaround-only rewrite would
need its own live-backend proof pass and that proof does not exist.

The conformance pair runs the complete production statement rather than stopping
at the first divergence, because a case truncated at the first bug goes green as
soon as that bug is fixed while the query stays broken on a later clause.

No-Regression Evidence: the cases are data appended to `DefaultReadCorpus` and
`DefaultWriteCorpus`. They are evaluated only under the existing
`ESHU_BACKEND_CONFORMANCE_LIVE` opt-in, so the default test path does no
additional work and its runtime is unchanged; `go test
./internal/backendconformance -count=1` passes. Against a live backend the pair
adds one seeded chain of six nodes and five edges plus one read, which is the
same order as every other case in the corpus.

Observability Evidence: failure is reported by the existing live-conformance
runner, which names the failing case and the row shortfall — `read case
"value-flow cloud sink aggregation and subscript projection" returned 0 rows,
want at least 1`. No new metric, span, log field or environment variable is
introduced.

Verified both directions on live backends: `ESHU_GRAPH_BACKEND=neo4j` passes
`TestLiveBackendConformance` with exit 0; `ESHU_GRAPH_BACKEND=nornicdb` fails
with exit 1 naming this case.
