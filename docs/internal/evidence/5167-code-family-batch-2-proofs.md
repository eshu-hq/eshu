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
| `TestImportDependenciesFilterByRepositoryGrant` (6 query types) | `scoped <query_type> query leaked "ungranted_module"` on four, `leaked "repo://tenant-b/other-service"` on the cycle case | `ok internal/query 1.242s` |
| `TestImportDependenciesEmptyGrantReachesNoBackend` (6) | `a grantless scoped caller reached the graph: [MATCH (repo:Repository)…]` on all six | `ok internal/query 1.242s` |
| `TestImportDependenciesResolveAScopeOnlyGrantToItsRepository` (6) | same leak, scope-only grant | `ok internal/query 1.242s` |
| `TestCrossModuleCallsBindTargetRepositoryIndependently` | `cross-module call query does not bind target_repo to the grant` | `ok internal/query 1.242s` |
| `TestImportDependencyScanBoundIsSpentOnGrantedRowsOnly` | `status = 422, want 200; an out-of-grant repository spent the scan budget` | `ok internal/query 1.242s` |
| `TestImportDependencyBuildersBindTheGrantInTheShippedCypher` (8) | new coverage on the changed builders, no prior red | `ok internal/query 1.257s` |
| `TestImportDependencyParamsBindTheGrantArrays` | new coverage, no prior red | `ok internal/query 1.257s` |

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

## Verification

Every command below was run after the last edit, from
`/Users/linuxdynasty/repos/eshu-wt-5167-code2` with a worktree-local `GOCACHE`.
Exit codes were captured directly.

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
