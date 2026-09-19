# #6786 X11: NornicDB v1.3.3 label predicates depend on clause position

X11 extends the #6786 defect catalogue (shapes X1–X10 in
`docs/internal/evidence/6786-nornicdb-400-409-exposure.md` on the #6786
exposure branch). On NornicDB v1.3.3, whether a label predicate is evaluated
depends on the clause it sits in. In the `WHERE` of a `MATCH` that has a
relationship pattern, a label test is ignored. So is an `any()` over
`labels()`. A `NOT` label test drops every row. Neo4j evaluates all of them.
This page covers the minimal repro, a map of when each form works, the
exposure audit of every production statement, and the fixes. Tracking: #6786
(epic #6788).

The shape was first seen while measuring the repository-dependency catalog
(`6786-repository-dependency-marker-and-relationship-repo-anchor.md`, same
branch). There, a `DEPENDS_ON` read with the endpoint labels tested in `WHERE`
returned 2,317 rows on NornicDB against a true answer of 300.

Both cited files are pending: when this page was written they existed only on
the unmerged `fix/6786-nornicdb-defect-exposure` branch, not on `main`. They
are named by file, not linked, so this page does not point at paths that do
not exist yet. Link them here when that branch merges. Nothing below depends
on them: every X11 claim on this page has its own probe or live test.

## Pins and method

| Item | Value |
| --- | --- |
| NornicDB | `timothyswt/nornicdb-cpu-bge:v1.3.3@sha256:81cedbf48898f4c37d05c325fee76b6d797b43e290e3a8a4e9eea936f0ec827f` |
| Neo4j (oracle) | `neo4j:2026-community@sha256:eabfbb042bdaca2fd5e1950db1329b22c794eee80f0eacc4e7a729d44b2e863f` |
| Driver | `github.com/neo4j/neo4j-go-driver/v5` v5.28.4 (the `go/go.mod` pin) |
| Schema | `graph.EnsureSchemaWithBackend` for the backend under test, applied before every case |
| Isolation | Both containers recreated for every case (`docker rm -f` + `run`) |
| Seeds | `CREATE` or `MATCH … MATCH … CREATE`, each committed as its own statement |
| Reads | auto-commit `session.Run` in a read session, the mode the production `Neo4jReader` uses |
| Base | `origin/main` at `c4ff6e447` |

Only returned rows count as evidence. NornicDB summary counters are not used.

**The transaction mode matters, and it was checked.** In a managed
transaction (`neo4j.ExecuteQuery`), NornicDB v1.3.3 rejects every
`CALL { … }` subquery with `SyntaxError (unknown procedure: CALL {`, even
`CALL { MATCH (n:Workload) RETURN n.id AS id } RETURN id`. In auto-commit mode
all 15 of those CALL probes are correct, and production reads are auto-commit.
An early managed-transaction run made the scoped change-surface read look
broken. All 106 probes were then re-run in both modes on fresh containers.
90 of the 91 label-predicate probes returned identical verdicts in the two
modes: 50 diverged in both, and 41 matched in both. The exception is
X4-style newline-tab-`OR` probe E07 (`MATCH (n) WHERE n:R` with the `OR`
led by a newline and tab), whose verdict depended on the mode; its
mode-specific row counts were not tabulated, so it sits in neither the 50
nor the 41. All 15 CALL probes are correct in auto-commit mode, and
production reads are auto-commit. The live tests below read
in auto-commit mode only.

## Minimal repro

Seed: `Repository` r1, r2, r3; `Workload` w1, w2; `Function` f1. The
`DEPENDS_ON` edges are r1→r2, r2→r3, w1→r2, r1→w1, f1→w1 and w2→w1.

```cypher
MATCH (s)-[:DEPENDS_ON]->(t) WHERE s:Repository AND t:Repository RETURN s.id, t.id
-- Neo4j:    2 rows  r1->r2 r2->r3
-- NornicDB: 6 rows  every DEPENDS_ON edge
MATCH (s:Repository)-[:DEPENDS_ON]->(t:Repository) RETURN s.id, t.id
-- both:     2 rows  r1->r2 r2->r3
```

## When a label predicate is evaluated (auto-commit, v1.3.3)

Counts are rows returned. "ignored" means NornicDB returned the rows the
predicate should have removed.

| Probe | Shape | Neo4j | NornicDB |
| --- | --- | --- | --- |
| A02 | `(s)-[:DEPENDS_ON]->(t) WHERE s:Repository AND t:Repository` | 2 | 6 (ignored) |
| A04 | `… WHERE t:Repository` | 3 | 6 (ignored) |
| A06 | untyped `(s)-[r]->(t) WHERE s:R AND t:R` | 2 | 18 (ignored) |
| B02 | labelled source `(s:Repository)-[:DEPENDS_ON]->(t) WHERE t:Workload` | 1 | 3 (ignored) |
| B03 | both labelled, contradicting `WHERE t:Workload` | 0 | 2 (ignored) |
| C02 | `WHERE s:Function OR s:Workload` | 3 | 6 (ignored) |
| C05 | anchored `(s:Repository {id: $r})-[:CONTAINS]->(t) WHERE t:K8sResource OR t:TerraformModule` | 2 | 3 (ignored) |
| D01 | `WHERE s:Repository AND s.id = 'r1'` | 2 | 2 |
| D02 | `WHERE s.id = 'w1' AND t:Repository` | 1 | 1 |
| D03 | `WHERE t:Repository AND s.id <> 'r2'` | 2 | 5 (ignored) |
| J05 | `WHERE t:Repository AND s.id IN [...]` | 2 | 4 (ignored) |
| D04 | `WHERE NOT t:Workload` | 3 | **0** |
| H02 | `MATCH (s:Repository {id:'r1'}) MATCH (s)-[:DEPENDS_ON]->(t) WHERE t:Repository` | 1 | **0** |
| H03 | var-length `-[:DEPENDS_ON*1..2]->(t) WHERE t:Repository` | 2 | 3 (ignored) |
| H05 | `OPTIONAL MATCH (s)-[:DEPENDS_ON]->(t) WHERE t:Repository` | r1→r2 | r1→null |
| I01 | two-hop `…-[:CONTAINS]->(infra) WHERE infra:K8sResource OR …` | 2 | 5 (ignored) |
| G02 | `WHERE any(l IN labels(t) WHERE l IN $ls)` | 3 | 6 (ignored) |
| K05 | `WHERE any(l IN $ls WHERE l IN labels(t))` | 3 | 6 (ignored) |
| K06 | `WHERE size([l IN labels(t) WHERE l IN $ls]) > 0` | 3 | 6 (ignored) |
| I11 | var-length, `id <> $r AND any(label IN labels(impacted) WHERE …)` | 4 | 13 (ignored) |
| F03 | `WHERE 'Repository' IN labels(s) AND 'Repository' IN labels(t)` | 2 | 2 |
| F04 | `WHERE $type IN labels(t)` | 3 | 3 |
| I03 | two-hop `WHERE e.name = $name AND $type IN labels(e)` | 1 | 1 |
| J09 | var-length `id <> 'r1' AND ('Repository' IN labels(i) OR 'Workload' IN labels(i))` | 8 | 8 |
| K09 | var-length `id <> $r AND (repo_id IN $a OR id IN $a) AND (… IN labels(i) OR …)` | 6 | 6 |
| L04 | same, with `(impacted:Workload OR impacted:TerraformModule)` | 3 | 10 (ignored) |
| E01/E02 | single node `MATCH (n) WHERE n:R` / `n:R OR n:W` | 3 / 6 | 3 / 6 |
| I08/I17 | single node `MATCH (n:ArgoCDApplicationSet) WHERE NOT n:ArgoCDApplication` | 1 | 1 |
| I14/I15 | single node `WHERE (n.id = … OR (n:L AND n.name = …))` | 1 | 1 |
| G01/J12 | single node `WHERE any(l IN labels(n) WHERE l IN …)` | 6 / 3 | **0 / 0** |
| H01/J06 | `(s)-[:DEPENDS_ON]->(t) WITH s, t WHERE s:R AND t:R` | 2 | 2 |
| J11/L03 | var-length `WITH path, impacted WHERE impacted:Workload OR …` | 4 / 3 | 4 / 3 |
| J07 | `WITH s, t WHERE NOT t:Workload` | 3 | 6 (ignored) |
| J03/L07 | `WITH … WHERE 'Repository' IN labels(s)` | 2 / 3 | 6 / 17 (ignored) |
| J08 | `WITH impacted WHERE any(label IN labels(impacted) …)` | 8 | 19 (ignored) |
| K01–K08 | label disjunction in the pattern, `(t:A|B)` | 2–8 | **0** (known pitfall) |

The rules that follow from the table:

- In the `WHERE` of a `MATCH` that has a relationship pattern, `x:Label` is
  ignored whether the pattern nodes are labelled or not. That covers `OR`,
  `AND` with `<>` or `IN`, and one-hop, two-hop and variable-length
  patterns. `NOT x:Label` drops every row, and so does a label test after a
  second `MATCH`. `AND` with an `=` property equality happened to be correct
  (D01, D02, D05), but the rule does not depend on that.
- `'Label' IN labels(x)` and `$p IN labels(x)` are correct in that position
  (F03, F04, I03–I06, J01, J09, K09).
- After `WITH`, it is the other way round. A positive `x:Label` is correct
  (H01, J04, J06, J11, L01, L03, L06). `IN labels(x)` and `NOT x:Label` are
  ignored (J03, J07, L07). J03 is the shape behind the catalog's 2,317-row
  reading: `WITH startNode(r) AS s … WHERE 'Repository' IN labels(s)`.
- `any|all|none|single` over `labels()`, or a list comprehension over it, is
  wrong everywhere. After a pattern it is ignored. On a single-node `MATCH` it
  returns zero rows (G01, J12, J13).
- A single-node `MATCH (n) WHERE n:A OR n:B` or `NOT n:A` is correct.
- Newline-led `OR` (X4) does not make X11 worse: C06 and I01 fail the same
  way with or without it.
- `(n:A|B)` in the pattern still matches zero rows (K01–K08), as
  `docs/public/reference/nornicdb-query-pitfalls.md` already documents.

## Exposure audit

Candidates came from `rg` over non-test Go under `go/internal` and `go/cmd`,
plus a scan of every Cypher string literal and literal concatenation (see
[Guard](#guard)). That scan found exactly the three affected statements. For
each candidate the production dispatch was traced, and the real function was
driven on both backends.

| Statement | Dispatch | Classification | Evidence (rows) |
| --- | --- | --- | --- |
| `repository/infrastructure.go` `QueryRepoInfrastructureFromGraph`: two-hop `WHERE infra:K8sResource OR …` (20 labels) | both backends; the `repository` story, context and `entity/workload_context.go` fallback when the content read model is missing | **affected** | The Go `isRepositoryInfrastructureType` gate hid it for small repos. With 5,001 code entities in the file, NornicDB returned `[]`, `truncated=true`, while Neo4j returned all 4 infra rows. The `ORDER BY type LIMIT 5001` ran over unfiltered `Function` rows. |
| `impact/change_surface_legacy.go` unscoped traversal: `any(label IN labels(impacted) …)` | both backends; `FindChangeSurfaceImpactRows` and the investigate path through `changeSurfaceTraversalRows` | **affected** | Repo with 3 `File`s named ahead of 1 `Workload`, limit 2. NornicDB returned impacted `[]`, `truncated=true`. Neo4j returned `[workload]`. |
| `codequery/relationships/story/class.go` `OverrideRowsCypher`: pair of `any(label IN labels(x) …)` | both backends; `relationshipStoryOverrideRows` | **affected shape; not observable today** | A seeded out-of-contract `OVERRIDES` to a `Variable` leaked on NornicDB (2 rows, Neo4j 1). The canonical writer only writes `OVERRIDES` between the seven override labels (`canonical_inheritance_edges.go`), so production data cannot reach the leak. |
| `impact/change_surface_traversal.go:39` scoped traversal: `WITH path, impacted WHERE impacted:Workload OR …` | both backends; scoped callers | not affected | Live scoped subtest: `[workload]` on both. The WITH-attached positive label test is evaluated on v1.3.3. |
| `entity/handler.go` `BuildResolveGraphQuery`: `AND $type IN labels(e)` after a two-hop MATCH | both backends | not affected | Live: `Function` + `Class` both named `dup` → `[x11-na:fn]` on both. |
| `infra_argocd_search.go` `AND NOT n:ArgoCDApplication` on single-node `MATCH (n:ArgoCDApplicationSet)` | both backends | not affected | Live: dual-labelled node returned once; `[app, dual, set]` on both. |
| `infra_resource_aggregates.go:323` / `infra_search_predicates.go:20` `(n.provider = $provider OR (n:CloudResource AND n.source_system = $provider))` on per-label single-node MATCH | both backends | not affected | Live `CountInfraResources{Provider: aws}`: total 2, `{aws: 2}` on both. The seed has a `TerraformResource` with `source_system: 'aws'` that an ignored label test would count. |
| `infra_resource_aggregates.go:418` `CASE … WHEN ('CloudResource' IN labels(n))` in the projection | both backends | not affected | Same live drive: the CloudResource was bucketed by `source_system` (`aws: 2`). |
| `storage/cypher/canonical_check.go:31` `MATCH (n) WHERE n:Function OR n:Class OR n:File` | none: `NewCanonicalNodeChecker` has no production call site (`rg`, and the note in `cmd/reducer/neo4j_wiring.go`) | unreachable | Raw shape correct anyway (I09, I10). |
| `impact/resource_investigation_selector.go` `resourceInvestigationLabelPredicate` | test-only helper (`rg`: called only from `_test.go`) | unreachable | The production statement anchors `MATCH (n:%s)` per label. |

Classification rests on the auto-commit live drives. The scoped
change-surface row reflects the fixed harness; the managed-transaction run
had shown a `CALL {` error that production never hits.

## Fixes

- **Infrastructure read.** The 20-label test now sits in a `WHERE` attached
  to `WITH f, infra`. v1.3.3 evaluates that position, and Neo4j keeps its
  cheap label check. The live test checks every projected column on both
  backends: `labels(infra)[0]`, `coalesce(resource_type, data_type)`,
  `kind`, `source`, `terraform_source`, `config_path`, `provider`, and the
  service and category fields. That rules out the multi-clause projection
  pitfall for this shape.
- **Change-surface legacy traversal.** The whitelist is now an OR of
  `'Label' IN labels(impacted)` terms in the `MATCH`'s `WHERE`. A
  WITH-attached test would be faster on both backends, but it would break the
  single-clause contract `TestChangeSurfaceTraversalQueriesAreNornicDBSafe`
  (#5287) holds this read to. The Go whitelist stays as defence in depth.
- **Overrides story read.** Both endpoint filters are now OR-chains of
  `'Label' IN labels(x)` built from `OverrideNodeLabels()`. The unused
  `$override_labels` parameter is gone.

Unit guards: `TestRepoInfrastructureGraphLabelFilterMatchesEntityTypes`,
`TestChangeSurfaceImpactedLabelsMatchTheLegacyCypher` (rewritten), and
`TestOverrideRowsCypherUsesEvaluatedLabelFilter`. Each asserts the X11 and X4
guards on the rendered statement and keeps the label list equal to the Go
whitelist. Mutation checks were run by hand: reintroducing `infra:HelmValues`
before the WITH, or `impacted:DataAsset`, fails the guard, and dropping or
adding a label fails the drift check. The queryplan hashes were refreshed for
`QueryRepoInfrastructureFromGraph` (`query-source-coverage.yaml`) and
`QP-IMPACT-CHANGE-SURFACE` (`hot-cypher.yaml`).

## Guard

`querytestutil.AssertCypherHasNoIgnoredLabelPredicate` (and
`IgnoredLabelPredicate`) flags:

- a label test in a `WHERE` whose governing clause is a relationship `MATCH`
  or `OPTIONAL MATCH`;
- `IN labels()` or `NOT x:Label` in a `WHERE` attached to `WITH`;
- any quantifier or list comprehension over `labels()`.

It handles `CALL` and `EXISTS` subquery frames, pattern predicates and string
literals. `TestProductionCypherHasNoIgnoredLabelPredicate` scans every
non-test Go file under `go/internal` and `go/cmd`.

- RED: before the fixes it reported exactly `story/class.go:261`,
  `change_surface_legacy.go:126` and `infrastructure.go:209`.
- GREEN: after the fixes it reports nothing.
- Seeded pair: `TestAssertCypherHasNoIgnoredLabelPredicateSeededViolations`
  (15 RED, 12 GREEN) and `TestProductionCypherScanFindsSeededViolation`, a
  temp-dir file in RED and fixed form.
- Cross-check against all 106 live probe verdicts. Every probe the guard
  passes but NornicDB gets wrong is one of: another documented defect
  (`(n:A|B)` disjunction, X4), a `DISTINCT` or `CALL` probe outside X11, or a
  harness artefact. The guard also flags six probes that happen to be correct
  (an `OR` covering every target, `AND` with `=`), which is deliberately
  conservative.

## Live tests

Build tag `live_nornicdb_label_predicates`; env `ESHU_NEO4J_URI` and
`ESHU_LIVE_GRAPH_BACKEND` (`nornicdb` or `neo4j`). Fresh containers per
package run.

| Test | Before fix: NornicDB / Neo4j | After fix: NornicDB / Neo4j |
| --- | --- | --- |
| `repository` `TestLiveRepositoryInfrastructureLabelPredicate/code-heavy` | FAIL `[]` truncated / PASS | PASS / PASS |
| `impact` `TestLiveChangeSurfaceLabelPredicate/unscoped` | FAIL `[]` truncated / PASS | PASS / PASS |
| `impact` `TestLiveChangeSurfaceLabelPredicate/scoped` | PASS / PASS | PASS / PASS |
| `codequery` `TestLiveRelationshipStoryOverrideLabelPredicate` | FAIL (Variable target leaked) / PASS | PASS / PASS |
| `query` `TestLiveLabelPredicateExposureControls` (3 subtests) | n/a (not affected) | PASS / PASS |

Log lines from the rerun on this branch (fresh containers per package and
backend, pins above). "Before fix" swaps in the three pre-fix production files
from `d4e600239^` and runs the same tests:

```text
before fix, NornicDB:
--- FAIL: TestLiveRepositoryInfrastructureLabelPredicate/code-heavy
    infrastructure rows = [] truncated=true, want [K8sResource api-deployment ...]
--- FAIL: TestLiveChangeSurfaceLabelPredicate/unscoped
    impacted = [] truncated=true, want [x11-cs:workload] truncated=false
--- PASS: TestLiveChangeSurfaceLabelPredicate/scoped
--- FAIL: TestLiveRelationshipStoryOverrideLabelPredicate
    override rows = [x11-ov:method->x11-ov:base x11-ov:method->x11-ov:var], want [x11-ov:method->x11-ov:base]
before fix, Neo4j: all three PASS
after fix, NornicDB and Neo4j (each):
--- PASS: TestLiveRepositoryInfrastructureLabelPredicate (small, code-heavy)
--- PASS: TestLiveChangeSurfaceLabelPredicate (unscoped, scoped)
--- PASS: TestLiveRelationshipStoryOverrideLabelPredicate
--- PASS: TestLiveLabelPredicateExposureControls (infra_aggregate_provider_filter,
          argocd_category_NOT_label, entity_resolve_type_filter)
```

The infrastructure test compares rows in the order the server returns them,
and seeds two `TerraformResource`s in reverse name order, so `ORDER BY type,
name` after the new `WITH` is asserted. Changing it to `ORDER BY type, name
DESC` fails on both backends.

## Performance

Performance Evidence: the before and after statements were timed
interleaved in one process per backend. Seed: one repository and one `File`
that `CONTAINS` 20,000 `Function`s, one node of each of the 20 infrastructure
labels, one `DEFINES` `Workload` and two `OVERRIDES`. Schema was applied.
Each run had one warm-up, then 7 rounds with a nonce write before every
timed read, auto-commit reads, and medians reported. The "before" statement
is the old production text with whitespace normalised. Row digests were
checked for every shape.

| Read | NornicDB before → after (median) | Neo4j before → after (median) |
| --- | --- | --- |
| infrastructure (`LIMIT 5001`) | 1.0695s, **wrong**: 5,001 rows, no infra after the Go gate → 2.2454s, 20 correct rows | 0.0052s → 0.0059s |
| infrastructure, rejected `IN labels()` alternative | 3.1045s | 0.0984s (19× the before) |
| change-surface legacy (depth 2, `LIMIT 51`) | 1.0990s, **wrong** → 1.1007s | 0.0201s → 0.0522s |
| change-surface, rejected WITH alternative | 0.7233s | 0.0293s |
| overrides story | 0.3926s → 0.4088s | 0.0030s → 0.0025s |

The harness is `go/internal/query/label_predicate_timing_live_test.go`,
build tag `live_nornicdb_label_predicate_timing`; its header has the run
command. It holds frozen copies of the timed statements, including the
pre-fix texts. A rerun on this branch, on fresh containers on a busier host,
gave these medians. The absolute numbers moved and NornicDB's infrastructure
ratio was 2.7× rather than 2.1×, but the ordering of shapes and the
conclusions below held:

| Read | NornicDB before → after | Neo4j before → after |
| --- | --- | --- |
| infrastructure | 0.8786s (5,001 rows, wrong) → 2.3655s (20 rows); `IN labels()` 2.5444s | 0.0052s → 0.0045s; `IN labels()` 0.1364s |
| change-surface legacy | 1.5650s (51 rows, wrong) → 0.9889s (2 rows); WITH 0.6734s | 0.0333s → 0.0599s; WITH 0.0338s |
| overrides story | 0.4200s → 0.4359s | 0.0032s → 0.0043s |

What the numbers mean:

- NornicDB's infrastructure time roughly doubles, from a wrong answer to a
  correct one. The old read did no per-row label check and sorted 20,020
  unfiltered rows; the correct read has to check labels on every `CONTAINS`
  child. The WITH shape was the fastest correct option on both backends.
- The change-surface fix costs nothing on NornicDB, the default backend. It
  costs about 32ms on Neo4j at 20,000 reachable nodes, the price of keeping
  the #5287 single-clause shape.
- Overrides are neutral on both backends.

No other hot path changed.

## Observability

No-Observability-Change: only the Cypher text of three existing reads
changed. They run through the existing `Neo4jReader` spans (`neo4j.query`)
and graph-read duration and outcome telemetry, and through the handlers'
existing `truncated` and `limitations` signals. Before the fix those signals
reported truncation for pages the backend had failed to filter. After the
fix they report real truncation only. No new metric, span or log is needed
to operate these reads.

## Not in scope, recorded for follow-up

- Label disjunction in a pattern, `(n:A|B)`, still returns zero rows on
  v1.3.3 (K01–K08). About 35 production statements use it, mostly
  `UNWIND … MATCH (x:A|B {uid: row.x})` writers such as
  `canonical_inheritance_edges.go`. Those were not re-audited here; the
  inheritance writer prefers a label-scoped statement when both labels are
  known.
- In a managed transaction, v1.3.3 rejects `CALL { … }`. Production reads are
  auto-commit, but any managed-transaction reader or writer that sends a
  `CALL` subquery would fail on this build.
