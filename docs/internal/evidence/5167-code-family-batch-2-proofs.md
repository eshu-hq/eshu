# #5167 Code Family, Batch 2a — Proof Ledger

The red/green runs and mutation ledger for
[#5167 Code Family, Batch 2a](5167-code-family-batch-2.md). This document holds
the proofs; that one holds the change and its reasoning. They are split only
because together they outgrow the repository's 500-line Markdown cap, and
nothing here stands on its own.

## Red Then Green

Both routes carry a response-body two-tenant proof: one granted repository, one
out-of-grant repository, and an assertion that the out-of-grant identifier never
appears in the serialized body. Not the query text — the bytes the caller
receives.

The graph reads are driven by two fakes. `evaluatingRepositoryGraph`
(`go/internal/query/code_graph_grant_evaluating_fake_test.go`) backs
language-query: it reads the emitted statement far enough to answer whether the
Repository binding is optional and which repository predicates govern it, then
applies Cypher's clause semantics to seeded rows, so it fails on clause
attachment where no substring assertion can. Batch 1's
`TestEvaluatingRepositoryGraphKeepsOptionalMatchRows` proves that fake can still
fail. `evaluatingImportDependencyGraph`
(`go/internal/query/auth_scoped_import_dependencies_grant_test.go`) backs
imports/investigate and applies whatever repository predicates the emitted
statement carries — the inline `{id: $repo_id}` anchor and the grant condition —
per Repository alias, which is what the two-anchor cross-module case needs.

Every red below is a behavioural failure on the pre-change code, not a build
error.

| Test | Red | Green |
| --- | --- | --- |
| `TestLanguageQueryFiltersByRepositoryGrant` (4 branches) | `scoped language query leaked "UngrantedLanguageProbe"` on guard, graph-backed, graph-first-content and content-backed | `ok internal/query 1.058s` |
| `TestLanguageQueryEmptyGrantReachesNoBackend` (4) | `content store was queried with []string{""}` on all four | `ok internal/query 1.058s` |
| `TestLanguageQueryEmptyGrantAnswersWithArraysNotNull` (4) | `results = [...two tenants' rows...], want no rows for a grantless caller` | `ok internal/query 1.058s` |
| `TestLanguageQueryResolvesAScopeOnlyGrantToItsRepository` (4) | `scope-only language query leaked "UngrantedLanguageProbe"` | `ok internal/query 1.058s` |
| `TestLanguageQueryGraphlessProfileBindsTheContentFallback` | `content fallback repositories = []string{""}, want ["repo://tenant-a/granted-service"]` | `ok internal/query 1.058s` |
| `TestLanguageQueryMetadataEnrichmentCannotWidenTheAnswer` | `the metadata enrichment read asked for repository ""` | `ok internal/query 1.058s` |
| `TestLanguageQueryUngrantedRepositorySelectorIsRejected` | `status = 200, want 400 for an ungranted repository selector` | `ok internal/query 1.058s` |
| `TestLanguageTypeEntityFiltersBindTheGrantInTheShippedSQL` | new coverage on the changed builder, no prior red | `ok internal/query 1.092s` |
| `TestLanguageQueryBuildersBindTheGrantInTheShippedCypher` (4) | new coverage, no prior red | `ok internal/query 1.092s` |
| `TestLanguageQueryGrantBoundStoreTakesOneRead` | new coverage, no prior red | `ok internal/query 1.092s` |
| `TestLanguageQuerySharedKeyRepoIDGoesThroughTheSelector` (3) | with `applyRepositorySelectorForAccess` bypassed: `content read repositories = []string{"granted-service"}, want ["repo://tenant-a/granted-service"]` and `status = 200, want 400 for a repo_id that resolves to nothing` | `ok internal/query` (3 sub-cases pass) |
| `TestImportDependenciesFilterByRepositoryGrant` (6 query types) | `scoped <query_type> query leaked "ungranted_module"` on four, `leaked "repo://tenant-b/other-service"` on the cycle case | `ok internal/query 1.242s` |
| `TestImportDependenciesEmptyGrantReachesNoBackend` (6) | `a grantless scoped caller reached the graph: [MATCH (repo:Repository)…]` on all six | `ok internal/query 1.242s` |
| `TestImportDependenciesResolveAScopeOnlyGrantToItsRepository` (6) | same leak, scope-only grant | `ok internal/query 1.242s` |
| `TestCrossModuleCallsBindTargetRepositoryIndependently` | `cross-module call query does not bind target_repo to the grant` | `ok internal/query 1.242s` |
| `TestImportDependencyScanBoundIsSpentOnGrantedRowsOnly` | `status = 422, want 200; an out-of-grant repository spent the scan budget` | `ok internal/query 1.242s` |
| `TestImportDependencyBuildersBindTheGrantInTheShippedCypher` (8) | new coverage on the changed builders, no prior red | `ok internal/query 1.257s` |
| `TestImportDependencyParamsBindTheGrantArrays` | new coverage, no prior red | `ok internal/query 1.257s` |

`TestLanguageQuerySharedKeyRepoIDGoesThroughTheSelector` is the unscoped half of
the selector change, added in review round 1 because nothing covered it. Its
three sub-cases do not all red the same way, and the difference is the point:
`canonical_id_anchors_the_read` passes with or without the selector, because a
canonical id passes through `ResolveExactForAccess` untouched — it pins that
this stays true. The other two red without it, as the table records.

Unscoped counterparts pin the other direction — a shared-key caller that names
no repository keeps its query text and its row set:
`TestLanguageQuerySharedKeyReadIsUnchanged`,
`TestImportDependenciesSharedKeyReadIsUnchanged`, and the two
`…CarryNoGrantForAnUnscopedCaller` builder assertions. The queryplan manifests
pin the same thing from the other side: no `cypher_sha256` and no `plan` block
moved.

`TestImportDependenciesSharedKeyReadIsUnchanged` earned its keep during the red
pass. Its first two fixtures were wrong — the cross-module rows named a file the
request did not anchor on, and the cycle edges carried full paths where
`pythonSourceModule` expects a base name — so the unscoped case returned nothing
and the test failed for a fixture reason rather than a production one. Both were
fixed before the production change was written, so the reds above are the
production behaviour and not a broken fixture.

## BITES — Each Binding Proved To Bite

Each row breaks one production binding, runs the guard, restores the file, and
records the exit code directly (`cmd; echo $?`, never after a pipe). Every
mutation was restored and its guard rerun at exit `0`. The driver is a scratch
script, not committed; the working tree was verified clean afterwards.

| # | Mutation | Guard run | Exit |
| --- | --- | --- | --- |
| 1 | `buildLanguageTypeEntityFilters` drops its `appendRepositoryGrantFilter` branch | `-run 'TestLanguageTypeEntityFiltersBindTheGrantInTheShippedSQL\|TestLanguageQueryFiltersByRepositoryGrant'` | `1` |
| 2 | the four language-query builders emit `""` instead of `access.GraphPredicate("r")` | `-run 'TestLanguageQueryBuildersBindTheGrantInTheShippedCypher\|TestLanguageQueryFiltersByRepositoryGrant'` | `1` |
| 3 | `languageQueryGrantFor` stops reporting `blocked` | `-run 'TestLanguageQueryEmptyGrantReachesNoBackend\|TestLanguageQueryEmptyGrantAnswersWithArraysNotNull'` | `1` |
| 4 | `handleLanguageQuery` skips `applyRepositorySelectorForAccess` | `-run TestLanguageQueryUngrantedRepositorySelectorIsRejected` | `1` |
| 5 | `enrichLanguageResultsWithContentMetadata` drops `AllowedRepositoryIDs` from its search | `-run TestLanguageQueryMetadataEnrichmentCannotWidenTheAnswer` | `1` |
| 6 | `searchLanguageEntities` asks for repository `""` instead of iterating the grant | `-run TestLanguageQueryGraphlessProfileBindsTheContentFallback` | `1` |
| 7 | `importDependencyGrantPredicates` returns nil for every caller | `-run 'TestImportDependencyBuildersBindTheGrantInTheShippedCypher\|TestImportDependenciesFilterByRepositoryGrant'` | `1` |
| 8 | `crossModuleCallRowsCypher` binds `source_repo` only | `-run TestCrossModuleCallsBindTargetRepositoryIndependently` | `1` |
| 9 | `handleImportDependencyInvestigation` drops the empty-grant gate | `-run TestImportDependenciesEmptyGrantReachesNoBackend` | `1` |
| 10 | `importDependencyParams` stops merging `GraphParams` | `-run 'TestImportDependencyParamsBindTheGrantArrays\|TestImportDependenciesFilterByRepositoryGrant'` | `1` |
| 11 | `codeGrantAccessFilter` drops `WithCanonicalScopeRepositories` | `-run 'TestLanguageQueryResolvesAScopeOnlyGrantToItsRepository\|TestImportDependenciesResolveAScopeOnlyGrantToItsRepository'` | `1` |
| 12 | `sourceModuleFilesCypher` and `targetModuleFilesCypher` compute the grant and discard it | `-run TestImportDependencyBuildersBindTheGrantInTheShippedCypher` | `1` |
| 13 | `acceptLanguageQueryEntityType` admits every entity type | `-run TestLanguageQueryRejectsUnsupportedEntityTypeForEveryCaller` | `1` |
| 14 | `languageResultRepositoryMatchKey` drops its `repoID` component | `-run 'TestLanguageQueryMetadata\|TestLanguageResultRepositoryMatchKey'` | `1` |
| 15 | the four language-query builders emit `""` instead of `access.GraphPredicate("r")`, against the live backend | `-tags live_nornicdb_language_imports_grant -run TestLiveNornicDBLanguageQueryGrantBindsEveryBuilder` | `1` |
| 16 | `importDependencyGrantPredicates` returns nil for every caller, against the live backend | `-tags live_nornicdb_language_imports_grant -run TestLiveNornicDBImportDependencyGrantBindsEveryBuilder` | `1` |
| 17 | `buildDirectoryCypher` reverted to its two-MATCH shape, against the live backend | `-tags live_nornicdb_language_imports_grant -run 'TestLiveNornicDBLanguageQueryGrantBindsEveryBuilder\|TestLiveNornicDBLanguageQueryDirectoryBuilderReturnsNothing'` | `1` |
| 18 | `buildDirectoryCypher` rewritten forward from Repository instead of anchored at File | `-tags live_nornicdb_language_imports_grant -run TestLiveNornicDBLanguageQueryDirectoryBuilderReturnsNothing` | `1` |

Rows 17 and 18 are the two ways the directory rewrite can be got wrong, and
they fail differently on purpose. Row 17 reverts it and the statement returns
nothing at all. Row 18 keeps one clause but runs the join forward from
`Repository`, which returns rows — so a presence-only assertion would have
passed it — and the guard catches it on the counts instead:
`counted 2 file(s) in "z-src-0", want 1`, with the nested directory missing from
the answer entirely.

Row 13 reds only its `scoped caller with no repository grants` sub-case, and
that is the correct shape: neutering the request-time gate leaves the dispatch
tail's backstop answering the other two callers exactly as before. The sub-case
that reds is the one the fix exists for. Rows 15 and 16 red every sub-case they
cover — four language shapes and ten import shapes — so no live shape rests on
a statement the grant is absent from.

Rows 4 and 11 are the two worth reading the output of. Breaking the selector
(row 4) does not produce a leak — `codeContentGrantScope`'s defense-in-depth
check still refuses an out-of-grant `repo_id` and the caller gets an empty page
— so what the guard actually catches is the wrong status code, `200` where the
contract now says `400`. That is the layering working: two independent gates,
and the test names which one it is judging. Row 11 fails in the opposite
direction: a scope-only grant stops resolving to the repository it owns and the
caller reads nothing at all, the #5052 shape.

Row 3's first attempt was rejected as evidence. Deleting the `blocked` branch
outright produced `no new variables on left side of :=` — a compile error, not a
behavioural red, which proves nothing about the guard. It was redone as
`if blocked && false`, which compiles, and the guard then failed on the
behaviour: `content store was queried with []string{""}` and
`a grantless scoped caller reached the graph`.

## Fixture Faithfulness

Both fakes mirror the production contract they stand in for, which is what makes
the assertions mutation-sensitive rather than decorative.

`languageQueryGrantEntities` applies the same three-way rule the shipped SQL
does: an explicit `repo_id` anchors the scan, a non-empty grant list restricts
it, and an empty grant list restricts nothing. That last clause is the one that
matters — it is why removing the empty-grant short-circuit makes the grantless
caller read the whole corpus rather than silently reading zero rows.

`importGrantRowAdmitted` reads the emitted statement for each Repository alias
the seeded row names, applying the inline `{id: $repo_id}` anchor and the grant
condition separately. Binding only `source_repo` therefore still admits a row
whose `target_repo` is out of grant, which is what row 8 above catches.

Two fixture shapes are deliberately not what a reader would first reach for, and
both are forced by production Go passes that run after the read:

- The cycle case gives both tenants the same file names, because
  `buildFileImportCycleRows` reconstructs cycles from reciprocal edges and
  `importCycleRowMatches` then filters on the request's `target_file`. Distinct
  file names per tenant would have made the request's own anchor do the
  filtering rather than the grant. Tenants are distinguished by repository id
  instead.
- The cross-module case anchors on `src/api.py`, the path its seeded rows carry,
  because `crossModuleCallRowMatches` drops any row whose `source_file` differs
  from the request's.

## Live NornicDB Run

| Field | Value |
| --- | --- |
| Image | `timothyswt/nornicdb-cpu-bge` |
| Digest | `sha256:4dfa887d990bf0b536693830830e34351c036716b0fe6dc957e1a3680e9f3c74` |
| Self-reported version | `1.2.2` (the digest `deploy/helm/eshu/values.yaml` pins as `v1.2.3`) |
| Environment | `NORNICDB_EMBEDDING_ENABLED=false`, `NORNICDB_NO_AUTH=true` |
| Bolt | a non-default host port, so no shared local stack is touched |
| Store | a container started clean for the run and removed after it |
| Build tag | `live_nornicdb_language_imports_grant` |
| Shapes proved | 15 (5 language-query, 10 import-dependency) |
| Statements executed | 30 shipped runs (15 shapes scoped and unscoped) and 12 backend probes (9 directory bisection, 3 plan) |
| Wall time | every statement under 4ms; the whole tagged package run 1.4s |

The seed is two repositories: `repo://live-zeta/granted-service`, the one the
caller is granted, with a single file plus one more in a directory a level
further down, and `repo://live-alpha/other-service`, which the caller is not
granted, with six. The nested directory is there so the rewritten
`buildDirectoryCypher` is judged on the depth-N `CONTAINS` chain the projector
actually writes, and its case asserts each directory's `file_count` rather than
mere presence. Every out-of-grant node carries
`live-alpha` in its id, name, path, relative path, entity name and module name,
so a leak is visible in any column any builder happens to project. Repository
ids and directory prefixes are chosen so the out-of-grant rows sort FIRST under
every builder's `ORDER BY`, and each shape runs with its page or scan bound set
to 2 — below the six rows the out-of-grant repository can supply.

`assertLiveGrantSqueezed` is the assertion the argument rests on: EVERY row of
the unscoped control must be out-of-grant. A page holding one row of each would
survive a filter applied after the bound, and the scoped result would then prove
nothing about when the predicate ran. It held for all fourteen shapes.

| Shape | Query type | Scoped rows | Unscoped rows |
| --- | --- | ---: | ---: |
| `buildRepositoryCypher` | `repository` | 1 | 1 |
| `buildDirectoryCypher` | `directory` | 2 | 3 |
| `buildFileCypher` | `file` | 1 | 2 |
| `buildEntityCypherWithSemanticFilter` | `function` | 2 | 2 |
| `buildEntityCypherWithSemanticFilter` + `semantic_kind` | `guard` | 2 | 2 |
| `directImportRowsCypher` | `imports_by_file` | 2 | 2 |
| `directImportRowsCypher` | `importers` | 1 | 2 |
| `packageImportRowsCypher` (`DISTINCT`) | `package_imports` | 2 | 2 |
| `packageImportRowsCypher` (scan-bounded) | `package_imports` | 2 | 2 |
| `sourceModuleFilesCypher` | `module_dependencies` | 1 | 2 |
| `targetModuleFilesCypher` | `cross_module_calls` | 1 | 2 |
| `sourceModuleImportRowsCypher` | `module_dependencies` | 2 | 2 |
| `fileImportCycleEdgeRowsCypher` | `file_import_cycles` | 2 | 2 |
| `crossModuleCallRowsCypher` | `cross_module_calls` | 1 | 2 |
| `crossModuleCallRowsCypher` + module scopes | `cross_module_calls` | 1 | 2 |

Every scoped row above named only the granted repository. Every unscoped row
named only the out-of-grant one.

The builders that take `$source_paths` and `$target_paths` were given BOTH
repositories' file paths, so the path predicate cannot be what drops the
out-of-grant rows — only the grant can.

Two results are about the backend rather than the grant, and both are recorded
in the change note. `EXPLAIN` and `PROFILE` are accepted, return zero rows, and
leave the driver summary without `Plan()` or `Profile()`, so plan shape is not
reportable on this build and the squeeze control above stands in for it.
`buildDirectoryCypher` returned nothing with or without a grant, because a
statement with two `MATCH` clauses and a `WITH` aggregation answers no rows on
this build once the `RETURN` carries a function call or a list construction.
That one is fixed in this change rather than recorded and left; the nine-probe
bisection stays committed inside
`TestLiveNornicDBLanguageQueryDirectoryBuilderReturnsNothing` as the control
that fails when the backend behaviour moves.

## Verification

Every command below was run after the last edit, from the batch-2 worktree with
a worktree-local `GOCACHE`. Exit codes were captured directly.

```text
cd go && go test ./internal/query ./internal/mcp ./internal/queryplan -count=1
```

The promotion gates specifically: `TestScopedTokenAllowlistCompleteness`,
`TestScopedRouteClassLedgerAgreesWithPredicate`,
`TestPolicyGatedRoutesDeclareForbiddenResponse` and
`TestScopedTokenAdvertisedRoutesReachHandlerThroughRealAuthMiddleware` in
`internal/query`, and `TestEveryMCPReachableRouteIsScopedOrAnnotated` in
`internal/mcp`.

The heavier promotion preflight (`make pre-pr`), the docs build, and the live
gates are the orchestrator's to run; this branch's proof is the package-scoped
set above plus the mutation ledger.
