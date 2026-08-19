# Backend conformance evidence notes

## Value-flow cloud sink conformance pair

The production query `valueFlowCloudSinkTargetsCypher`
(`go/internal/reducer/value_flow_cloud_sink_loader.go`) returns zero rows on
NornicDB and the correct row on Neo4j 5.x community. It resolves which cloud
resources a function's cloud action can reach, and its failure is silent — no
error, just a graph missing that category of edge. Nothing in the repository
detected it.

Three independent backend divergences empty it, each measured against Neo4j 5.x
community with the same fixture on `eshu-nornicdb-pr290:3722b483c02c` and on
upstream `main`:

| Shape | Neo4j 5 | NornicDB |
| --- | --- | --- |
| `collect()` after two `MATCH` clauses | one row | no rows |
| `ws[0] AS w2` then `w2.name` | the property value | the literal text `"w2.name"` |
| `x IN relationship.listProperty` | matches | matches nothing |

Controls: two `MATCH` clauses without aggregation, aggregation after one
`MATCH`, `IN` over a *node* list property, and reading the relationship property
directly all behave identically on both backends. Filed upstream as
orneryd/NornicDB#297, #298 and #301.

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
