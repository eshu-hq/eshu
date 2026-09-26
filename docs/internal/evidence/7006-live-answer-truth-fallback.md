# #7006 Wave 5 — Answer-Truth Regressions Found By live-backend CI

Follow-up to [7006-code-quality-context-anchor-fix.md](7006-code-quality-context-anchor-fix.md).

CI `live-backend (nornicdb and neo4j)` on `c1df74858b` failed three live
answer-truth tests; each reproduced locally on the pinned NornicDB
(`fix-500-e022384c`) and Neo4j (`2026-community`) images.

1. `TestLiveNornicDBEntityContextAnswerTruth` (both backends): `repo_id` and
   `repo_name` empty. Wave 3's `coalesce(e.repo_id, f.repo_id)` needs a node
   property the fixture (and any graph written without it) lacks, and no
   content store backfills `repo_name`.
2. `TestLiveScopedEntityContextGrant` (both): `fn-in` 404 because its empty
   `repo_id` failed `access.AllowsRepositoryID`; `wli-in` 404 because
   `WorkloadInstance` was not in `EntityContextAnchorLabels`.
3. `TestLiveNornicDBAnswerTruth/A7`: a `Function` id sent to
   `infra/relationships` 404'd; `Function` is not an impact anchor label.

Root cause of 2b and 3: the pre-fix `MATCH (e) WHERE e.id = $id` matched an id
on any label. The schema alone names over a hundred id- or uid-keyed labels
(`codemodel.neo4jEntityUIDAnchorLabels` plus `neo4jEntityIDAnchorLabels`,
pinned to the DDL by `TestNeo4jEntityIDAnchorLabelsMatchSchema`), and fixtures
write labels outside it. A label list cannot be complete, and looping over a
hundred labels would make a miss slower than the scan it replaces.

Fix: both routes keep the per-label fast path and end with the pre-fix
unlabeled read, under the same shared deadline. `GetEntityContext` projects
the pre-fix enrichment again (`(e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)`,
scoped grant on `r`, `r.id`/`r.name`); `WorkloadInstance` joined its fast path.
A fast-path hit never pays the scan; any other id, and a genuine miss, pay
the fast-path reads plus the pre-fix statement. Unit tests
`TestGetEntityContextFallsBackToUnlabeledAnchorAfterLabelMisses`,
`TestGetEntityContextRepoIdentityComesFromRepositoryHop`,
`TestInfraRelationshipsFallsBackToUnlabeledAnchorAfterLabelMisses` failed
before the fix and pass after; the live files above pass on both backends.

No-Regression Evidence: not re-measured at scale. The ops-qa Neo4j numbers in the
parent doc cover the Wave 3 entity-context shape and the infra miss path, neither
of which ships now; the infra first-hit statement is unchanged. The restored
two-hop enrichment is the statement main shipped.

Observability Evidence: no new signal. The request span's
`eshu.entity_anchor_labels_tried` (`infra/relationships`) and the
`labels_tried`/`labels_total` log fields (`GetEntityContext`) count the
fallback as one more anchor read, so a request that fell through to the scan
reads `len(labels)+1`; `graph_query_name` and the shared-deadline `deadline`
classification are unchanged.
