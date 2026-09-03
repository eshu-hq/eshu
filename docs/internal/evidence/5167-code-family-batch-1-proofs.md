# #5167 Code Family, Batch 1 — Proof Ledger

The red/green runs and mutation ledger for
[#5167 Code Family, Batch 1](5167-code-family-batch-1.md). This document holds
the proofs; that one holds the change and its reasoning. They are split only
because together they outgrow the repository's 500-line Markdown cap, and
nothing here stands on its own.

## Red Then Green

Nine of the ten routes carry a response-body two-tenant proof: one granted
repository, one out-of-grant repository, and an assertion that the out-of-grant
id never appears in the body. `code/call-graph/metrics` is the tenth: its
`repo_id` is mandatory and grant-resolved, so its proof is "a granted repo_id
returns only its own functions" plus "an ungranted one is rejected with 400".

The three graph-backed routes are driven by two fakes, and they are not equally
strong. `evaluatingRepositoryGraph`
(`go/internal/query/code_graph_grant_evaluating_fake_test.go`) backs complexity
and quality: it reads the emitted statement far enough to answer whether the
Repository binding is optional and which repository predicates govern it, then
applies Cypher's clause semantics to seeded rows, so it fails on clause
attachment where no substring assertion can.
`TestEvaluatingRepositoryGraphKeepsOptionalMatchRows` feeds it the shape this
change replaced and asserts the out-of-grant row survives with null repository
columns, so a fake that quietly dropped non-matching rows could not pass.
`evaluatingCallGraphEdges` backs call-graph metrics and is weaker: it applies
whatever repository predicates the emitted statement carries — today only the
inline `{repo_id: $repo_id}` anchors — without judging attachment. Nothing turns
on that: this route's binding is its selector, not its query text.

| Test | Red | Green |
| --- | --- | --- |
| `TestCodeTopicInvestigation*` (3) | `AllowedRepositoryIDs = []string(nil), want [...]`; `queried = true, want false` | `ok internal/query 1.789s` |
| `TestCodeTopicFiltersBindTheGrantInTheShippedSQL` | `want a repo_id = ANY($1) grant predicate` | `ok internal/query 1.706s` |
| `TestCodeContentRoutes*` (3 × 4 route cases) | build failure: `AllowedRepositoryIDs undefined` on all three request types | `ok internal/query 1.802s` |
| `TestCodeContentFiltersBindTheGrantInTheShippedSQL` (3) | same build failure | `ok internal/query 1.802s` |
| `TestSymbolNameFallback*` (3) | `SearchEntitiesByName repositories = []string{""}, want ["repo://tenant-a/granted-service"]` | `ok internal/query 1.171s` |
| `TestDeadCodeRoutes*`, `TestCrossRepoDeadCodeProducerScanCarriesTheGrant` | build failure: `undefined: deadCodeCandidateQuery` | `ok internal/query 2.074s` |
| `TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher` | same build failure | `ok internal/query 2.074s` |
| `TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL` (2) | `candidate SQL is missing "AND repo_id = ANY($4)"` | `ok internal/query 1.747s` |
| `TestCrossRepoDeadCodeConsumerEvidence*` (2), `TestCrossRepoDeadCodeKeepsTheHiddenConsumerSignal` | build failure: the reader took no grant argument and returned no signal rows | `ok internal/query 1.074s` |
| `TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector` | `classification: unknown_needs_evidence`, `hidden_consumer_evidence_count: 1` for a symbol the requested consumer proves live | `ok internal/query 1.291s` |
| `TestCrossRepoDeadCodeSignalReadRepeatsTheUngrantedStatement`, `*SignalTruncationKeepsCandidatesUnknown` | new coverage on the replaced statement pair, no prior red | `ok internal/query 1.291s` |
| `TestCallGraphMetricsCypherIsTheSameForEveryCaller`, `TestGraphSummaryHotEntitiesEdgePassIsUnchanged` | `a scoped caller runs a different edge shape than the one the plan fixture pins` | `ok internal/query 1.226s` |
| `TestCodeRoutesEmptyGrantAnswersWithArraysNotNull` (9 routes) | `"results" = <nil>, want an empty JSON array` on structural inventory, both kinds | `ok internal/query 1.078s` |
| `TestCallGraphMetricsEmptyGrantSkipsTheEdgeScan` (2) | `read` sub-test reached the graph | `ok internal/query 1.826s` |
| `TestCallGraphMetricsBodyCarriesOnlyGrantedFunctions`, `TestUngrantedRepositorySelectorIsRejectedWith400` | new coverage, no prior red | `ok internal/query 1.225s` |
| `TestCodeQualityAndComplexityBuildersBindTheGrant` (4) | all four builders `missing "(repo.id IN $allowed_repository_ids OR ...)"` | `ok internal/query 1.799s` |
| `TestCodeQualityAndComplexityEmptyGrantSkipTheGraphRead` (4) | all four reached the graph | `ok internal/query 1.799s` |
| `TestComplexityByEntityIDHonoursASuppliedRepoID` | `entity_id lookup ignores the supplied repo_id` | `ok internal/query 1.799s` |
| `TestComplexityListDoesNotLeakUngrantedFunctions` | `scoped complexity list leaked "UngrantedComplexityProbe"` and `"OrphanComplexityProbe"`, both with `"repo_id":""` | `ok internal/query 1.295s` |
| `TestComplexityListUnscopedRepoIDSelectorFiltersToThatRepository` | `a supplied repo_id sits on an optional Repository binding, so it filters nothing`, exit `1` | `ok internal/query 1.163s`, exit `0` |
| `TestLiveNornicDBComplexityListFiltersUngrantedFunctions` (live) | `scoped complexity list leaked "LiveUngrantedComplexityProbe"`, exit `1` | `ok internal/query 1.112s`, exit `0` |

Unscoped counterparts pin the other direction — a shared-key caller that names
no repository keeps its query text and row set:
`TestCodeTopicInvestigationSharedKeyReadIsUnchanged`,
`TestCodeContentRoutesSharedKeyReadIsUnchanged`,
`TestSymbolNameFallbackSharedKeySearchIsUnchanged`,
`TestDeadCodeRoutesSharedKeyScanIsUnchanged`,
`TestCallGraphMetricsUnscopedCypherIsUnchanged`,
`TestCodeQualityAndComplexityUnscopedCypherCarriesNoGrant`,
`TestComplexityListUnscopedAnswerIsUnchanged`,
`TestCodeQualityInspectUnscopedAnswerIsUnchanged`, and
`TestLiveNornicDBComplexityListKeepsTheUnscopedAnswer`.

## BITES — Each Choke Point Proved To Bite

Each row breaks one production binding, runs the guard, restores the file, and
records the exit code directly (`cmd; echo $?`, never after a pipe). Every
mutation was restored and its guard rerun at exit `0`.

| # | Mutation | Guard run | Exit |
| --- | --- | --- | --- |
| 1 | `appendRepositoryGrantFilter` emits `true /* $n */` instead of `repo_id = ANY($n)` | `go test ./internal/query -run BindTheGrantInTheShippedSQL -count=1` | `1` (4 failures: topic, secrets, symbol_search, structural_inventory) |
| 2 | `codeContentGrantScope` returns `blocked=false` on `access.Empty()` | `go test ./internal/query -run 'EmptyGrant' -count=1` | `1` (topic, secrets, symbols, structure ×2, dead-code ×2) |
| 3 | `buildDeadCodeGraphCypherForLabel` drops `access.GraphCondition("r")` | `go test ./internal/query -run TestDeadCodeGraphCandidateScanBindsTheGrantInTheBuiltCypher -count=1` | `1` |
| 4 | `ContentReader.DeadCodeCandidateRows` emits `AND true /* $n */` | `go test ./internal/query -run TestDeadCodeCandidateRowsBindTheGrantInTheShippedSQL -count=1` | `1` |
| 5 | `buildCodeQualityCypher` and all three complexity builders drop their grant | `go test ./internal/query -run TestCodeQualityAndComplexityBuildersBindTheGrant -count=1` | `1` (4 failures) |
| 6 | `callGraphMetricsEdgesCypher` takes the caller's grant again and appends it to both `CALLS` endpoints | `go test ./internal/query -run TestCallGraphMetricsCypherIsTheSameForEveryCaller -count=1` | `1` |
| 7 | complexity and quality drop their `access.Empty()` refusal | `go test ./internal/query -run TestCodeQualityAndComplexityEmptyGrantSkipTheGraphRead -count=1` | `1` (4 failures) |
| 8 | `symbolNameFallbackEntities` always takes the single-lookup branch (`if true`), so it asks for repository `""` | `go test ./internal/query -run TestSymbolNameFallback -count=1` | `1` (`repositories = []string{""}`) |
| 9 | `complexityListAnchor` returns the `OPTIONAL MATCH` form for every caller (`if false`) | `go test ./internal/query -run TestComplexityListDoesNotLeakUngrantedFunctions -count=1` | `1` |
| 10 | `crossRepoDeadCodeGrantFilter` emits `AND true /* $n */` instead of `AND row.repository_id = ANY($n)` | `go test ./internal/query -run TestCrossRepoDeadCode -count=1` | `1` (3 failures) |
| 11 | the same mutation as #9, run against the live backend instead of the fake | `ESHU_NEO4J_URI=bolt://localhost:17787 go test ./internal/query -tags live_nornicdb_complexity_grant -run TestLiveNornicDBComplexityListFiltersUngrantedFunctions -count=1` | `1` (leaked `LiveUngrantedComplexityProbe` and `LiveOrphanComplexityProbe`) |
| 12 | the same mutation as #6, judged by the graph-summary route's own guard | `go test ./internal/query -run TestGraphSummaryHotEntitiesEdgePassIsUnchanged -count=1` | `1` (scoped edge pass diverged from the shared-key text) |
| 13 | `applyRepositorySelectorForCapability` rejects an ungranted selector with `404` instead of `400` | `go test ./internal/query -run TestUngrantedRepositorySelectorIsRejectedWith400 -count=1` | `1` (`status = 404, want 400`) |
| 14 | `complexityListAnchor` keys only on `access.Scoped()`, ignoring the supplied `repoID` | `go test ./internal/query -run TestComplexityListUnscopedRepoIDSelectorFiltersToThatRepository -count=1` | `1` |
| 15 | `bucketCrossRepoDeadCodeResults` counts the signal rows without the request's consumer selector | `go test ./internal/query -run TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector -count=1` | `1` (a consumer outside the requested set was counted as hidden) |
| 16 | `markCrossRepoDeadCodeConsumerEvidenceTruncated` skips any entity that already has page rows, the shape before this pass | `go test ./internal/query -run 'TestCrossRepoDeadCodeSignalTruncationMarksEntitiesTheSignalNeverReached\|TestContentReaderCrossRepoDeadCodeEvidenceMarksMissingEntitiesUnknownWhenTruncated' -count=1` | `1` (2 failures: the entity the signal read never reached, and the one it stopped inside) |
| 17 | `complexityIDLookupIsRepositoryBound` answers `false` for every caller, so the name fallback runs again | `go test ./internal/query -run 'TestHandleComplexityRepoAnchoredEntityIDDoesNotFallBackToName\|TestHandleComplexityScopedEntityIDDoesNotFallBackToName' -count=1` | `1` (2 failures: the repo_id anchor and the grant anchor) |
| 18 | `RepositoryAccessFilter.WithCanonicalScopeRepositories` returns the filter unchanged (`if true \|\|`), the shape before this pass | `go test ./internal/query ./internal/query/querycontract -run 'ScopeOnlyGrant\|WithCanonicalScopeRepositories' -count=1` | `1` (11 failures across the content, dead-code, complexity, quality, and call-graph-metrics routes) |
| 19 | `CodeReachabilityIncomingEntityIDs` ignores `consumer_in_grant` (`if false && !inGrant`), so an out-of-grant consumer reads as evidence | `go test ./internal/query -run TestCodeReachabilityIncomingEntityIDsBindsTheConsumerGrant -count=1` | `1` (edge came back `MaxConfidence:0.9, HiddenConsumer:false`) |
| 20 | the graph incoming probe runs the unrestricted text for its evidence pass too | `go test ./internal/query -run TestDeadCodeGraphProbeTreatsAnUngrantedSourceAsUnknown -count=1` | `1` (the ungranted source counted as a 0.9 edge) |
| 21 | `applyDeadCodeIncomingEdges` skips the hidden-consumer branch (`if false &&`) | `go test ./internal/query -run 'TestDeadCodeKeepsACandidateWhoseOnlyConsumerIsOutsideTheGrant\|TestDeadCodeInvestigateReportsThePermissionHiddenConsumerReason' -count=1` | `1` (2 failures; the investigation reason fell back to `weak_incoming_edge:repo_unique_name`) |
| 22 | `crossRepoDeadCodeConsumerRows` ignores the sentinel's entity id at the boundary | `go test ./internal/query -run TestCrossRepoDeadCodeCompletesTheEntityTheSentinelMovedPast -count=1` | `1` (a full 1,000-row page marked `consumer_evidence_truncated`) |

An earlier attempt at #1 deleted the whole helper body and failed as an unused
import rather than an assertion, which proves nothing; the mutations above keep
the package compiling so the failure is the assertion's.

Rows 18 through 22 are the round-7 pass. Row 18 is the scope-versus-canonical
identity, rows 19 through 21 the three places an out-of-grant incoming edge has
to stop being evidence, and row 22 the sentinel boundary. Each was restored and
its guard rerun at exit `0`.

Rows 6 and 12 are one mutation judged by two guards: row 6 is the call-graph
route's text guard, row 12 the graph-summary route that shares the builder. The
same mutation also reddens `go test ./internal/queryplan`, exit `1`, because the
builder's `source_sha256` moves off the manifest. Row 13 is the status code the
ten OpenAPI operations and eleven MCP tool descriptions now name. Rows 9, 11 and
14 all mutate `complexityListAnchor`: row 9 is the credential-free scoped guard
CI runs, row 14 the unscoped-with-`repo_id` guard, and row 11 the live NornicDB
one, the only row that settles clause attachment against a real backend. A second engineer
reran both directions of row 11 on a fresh container from the same pinned digest
(self-reporting 1.2.2, bolt on port 17787): mutated exit `1` with the leak body
quoted above, restored exit `0`.
