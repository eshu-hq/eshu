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
| `TestDeadCodeWeakGrantedEdgeBesideAnUngrantedOneReadsHiddenOnBothBackends` (2) | graph sub-test only: `ambiguity_reasons = []string{"weak_incoming_edge:repo_unique_name"}, want "permission_hidden_consumer"`; the SQL sub-test was already green | `ok internal/query 1.074s` |
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
| `TestDeadCodeGraphProbeKeepsASameMethodUngrantedSource` | `HiddenConsumer:false` for a candidate whose out-of-grant source shares its resolution method with a granted one, exit `1` | `ok internal/query 4.192s`, exit `0` |
| `TestDeadCodeGraphProbeRunsOneTraversalPerPage` | `statement count = 2, want 1`, exit `1` | `ok internal/query 4.192s`, exit `0` |
| `TestDeadCodeGraphProbeReadsEachSourceClass` (4) | new coverage of the four source classes, green on the old shape too and kept as the no-regression guard | `ok internal/query 4.192s`, exit `0` |
| `TestLiveNornicDBDeadCodeIncoming*` (3, live, run by hand) | new coverage on the replaced statement, no prior red; the withdrawn pair's collapse and every row of the `RETURN DISTINCT` pitfall table are recorded as assertions rather than prose | `ok internal/query 1.206s`, exit `0` |
| `TestDeadCodeIncomingProbeMaxResultsMatchesTheManifest`, `*HasNoLimitOfItsOwn` | new coverage tying the queryplan ledger's `max_results` to the probe's grouping key, no prior red | `ok internal/query 1.048s`, exit `0` |
| `TestCrossRepoDeadCodeConsumerSelectorSurvivesABusyGrantedRepository` | `consumer_evidence_truncated` in the `unknown` bucket for a symbol the requested consumer proves live, exit `1` | `ok internal/query 4.270s`, exit `0` |
| `TestCrossRepoDeadCodeConsumerReadPlan` (6) | new coverage of the six read shapes, no prior red | `ok internal/query 4.270s`, exit `0` |
| `TestDeadCodeIncomingEntityIDsHiddenOnlyEntryStillRunsTheLegacyProbe` | `legacy incoming calls = 0, want 1`, exit `1` | `ok internal/query 1.316s`, exit `0` |
| `scripts/verify-tagged-builds.sh`, one `go vet` per build constraint | `vet: code_complexity_grant_nornicdb_live_test.go:56:49: undefined: ptrToCodeGrantAuthContext`, exit `1` on `live_nornicdb_complexity_grant` | 12 configurations in `internal/query`, 29 module-wide, exit `0` |
| `scripts/test-verify-tagged-builds.sh` | new coverage for the gate itself, no prior red | `test-verify-tagged-builds: OK`, exit `0` |
| `TestLiveNornicDBComplexityList*` (2, live, run by hand) | did not compile on this branch, so it could not run at all | `ok internal/query 1.228s`, exit `0` |

### The tagged-test sweep

A `//go:build live_*` file is invisible to `go build`, `go vet` and `go test`
under the default tag set, so a helper deleted somewhere else in the package
breaks it silently and stays broken through any number of green CI runs. That
is what happened here: `let the call-graph routes keep one edge query for every
caller` removed `ptrToCodeGrantAuthContext` along with the test that used it,
and `code_complexity_grant_nornicdb_live_test.go` — the live NornicDB proof
behind BITES row 11 — stopped compiling. Nothing reported it.

That was first answered with a paragraph telling the next person to run a loop,
which is the same shape as the defect: true only while someone remembers. It is
now a gate. `scripts/verify-tagged-builds.sh` runs one `go vet` per distinct
build constraint, reading the constraints out of the files rather than from a
list, and `scripts/dev/pre-pr.sh`'s "tagged build sweep" step runs it with
`--all` whenever the diff touches Go. `scripts/test-verify-tagged-builds.sh` is
its test mirror and BITES proof.

```text
$ bash scripts/verify-tagged-builds.sh                          exit 0
tagged-builds: vetted 12 build configuration(s), skipped 0, across 1 package path(s)

$ bash scripts/verify-tagged-builds.sh --all                    exit 0
tagged-builds: vetted 29 build configuration(s), skipped 16, across 12 package path(s)

$ bash scripts/test-verify-tagged-builds.sh                     exit 0
PASS a package whose tagged files all compile
PASS a break behind a single-tag constraint
PASS a break behind a compound && constraint
PASS an || alternation vetted one alternative at a time
PASS a three-way alternation vetted once per alternative
PASS a constraint that does not parse
PASS a GOOS-gated constraint is skipped, not forced
PASS a package with no build constraints
PASS a nonexistent package path is refused

$ bash scripts/verify-ci-gates-registry.sh                      exit 0
PASS: ci-gates registry integrity + drift check
```

Writing the gate found two defects in the loop the paragraph had published, and
both are why a script beat a snippet here:

- The snippet's `'^//go:build \S+'` captured only the first token of a
  constraint, so a `//go:build a && b` file contributed `a`, `go vet -tags a`
  excluded the file, and the sweep reported success on a file it never
  compiled. Every constraint in `internal/query` is a single tag today, so this
  was latent — and it is exactly the case the sweep exists to catch, since a
  compound constraint added later is the one that slips through.
- Enabling every term of an `||` alternation at once is not what the constraint
  means. `cmd/reducer`'s `perf5854_head || perf5854_main` files each declare the
  same symbol, so a single run with both tags fails on a redeclaration — a
  break of the sweep's own making. The gate runs one pass per alternative.

Sixteen of the module-wide configurations are reported `SKIP`, with the count
printed so a sweep covering less than it looks like is visible. Ten are
GOOS-gated: `-tags windows` on macOS does not compile the Windows file, it
fails inside `internal/goos` with `GOOS redeclared`, so those files are left to
the ordinary build on their own platform. Six are negated constraints
(`!ifafaultinjection`), which reduce to no selectable tag because the default
build already compiles them.

The gate runs in two places, and reaches both through one registration.
`make pre-pr` runs it with `--all` whenever the diff touches Go, selected by
`run-selected-gates.sh` from its tier and category rather than from a hardcoded
step, so a tagged compile break fails before a push. In CI it is the same
`tagged-builds` entry in `specs/ci-gates.v1.yaml`
and the `Verify tagged-builds gate` row of `static-contract-gates.yml`, wired
the way every other `test-verify-*.sh` mirror is: the matrix job runs the test
mirror and then the gate. Its trigger is `go/**`, deliberately broad — the
change that breaks a tagged file is almost never a change to that file, it is a
helper deleted elsewhere in its package, so narrowing the trigger to the tagged
files would reproduce the defect.

One more silent-PASS path came out of review. The alternation split was
`sed 's/||/\n/g'`, and `\n` in a sed replacement is a GNU extension: on a sed
that inserts a literal `n`, `perf5854_head || perf5854_main` collapses to the
single token `perf5854_headnperf5854_main`, which satisfies the identifier
check, vets trivially, and reports PASS having compiled nothing at all. Both
platforms this runs on happen to emit a real newline, so it was latent — but a
latent false green in the gate written to remove false greens is worth
removing. The split is pure bash now, and the count is checked: an alternation
that yields fewer alternatives than its `||` promises is an `ERROR`, which
fails the run, as is a term that is not a legal tag identifier. Simulating the
collapse turns the module's two `||` constraints from PASS into
`alternation split produced 1 alternative(s)`; collapsing every separator
reddens a third, the `&&` conjunction, through the identifier check. Mutation
rows 30 and 31.

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
| 16 | `markCrossRepoDeadCodeConsumerEvidenceTruncated` skips any entity that already has page rows, the shape before this pass | `go test ./internal/query -run 'TestCrossRepoDeadCodeSignalTruncationMarksEntitiesTheSignalNeverReached\|TestContentReaderCrossRepoDeadCodeEvidenceMarksMissingEntitiesUnknownWhenTruncated' -count=1` | `1` (2 failures: the entity the signal read never reached, and the one it stopped inside). Round 9 replaced the row-returning signal read with a probe that cannot leave an entity unreached, so the first of the two tests this row names no longer exists — rerunning this command today judges the mutation by the second one alone |
| 17 | `complexityIDLookupIsRepositoryBound` answers `false` for every caller, so the name fallback runs again | `go test ./internal/query -run 'TestHandleComplexityRepoAnchoredEntityIDDoesNotFallBackToName\|TestHandleComplexityScopedEntityIDDoesNotFallBackToName' -count=1` | `1` (2 failures: the repo_id anchor and the grant anchor) |
| 18 | `RepositoryAccessFilter.WithCanonicalScopeRepositories` returns the filter unchanged (`if true \|\|`), the shape before this pass | `go test ./internal/query ./internal/query/querycontract -run 'ScopeOnlyGrant\|WithCanonicalScopeRepositories' -count=1` | `1` (11 failures across the content, dead-code, complexity, quality, and call-graph-metrics routes) |
| 19 | `CodeReachabilityIncomingEntityIDs` ignores `consumer_in_grant` (`if false && !inGrant`), so an out-of-grant consumer reads as evidence | `go test ./internal/query -run TestCodeReachabilityIncomingEntityIDsBindsTheConsumerGrant -count=1` | `1` (edge came back `MaxConfidence:0.9, HiddenConsumer:false`) |
| 20 | the graph incoming probe runs the unrestricted text for its evidence pass too | `go test ./internal/query -run TestDeadCodeGraphProbeTreatsAnUngrantedSourceAsUnknown -count=1` | `1` (the ungranted source counted as a 0.9 edge) |
| 21 | `applyDeadCodeIncomingEdges` skips the hidden-consumer branch (`if false &&`) | `go test ./internal/query -run 'TestDeadCodeKeepsACandidateWhoseOnlyConsumerIsOutsideTheGrant\|TestDeadCodeInvestigateReportsThePermissionHiddenConsumerReason' -count=1` | `1` (2 failures; the investigation reason fell back to `weak_incoming_edge:repo_unique_name`) |
| 22 | `crossRepoDeadCodeConsumerRows` ignores the sentinel's entity id at the boundary | `go test ./internal/query -run TestCrossRepoDeadCodeCompletesTheEntityTheSentinelMovedPast -count=1` | `1` (a full 1,000-row page marked `consumer_evidence_truncated`) |
| 23 | `deadCodeResultsWithGraphIncomingEdges` diffs the signal probe against the grant-bound one entity by entity instead of row by row, the shape before this pass | `go test ./internal/query -run TestDeadCodeWeakGrantedEdgeBesideAnUngrantedOneReadsHiddenOnBothBackends -count=1` | `1` (graph sub-test only; the granted weak edge hid the ungranted one and the reason fell back to `weak_incoming_edge:repo_unique_name`) |
| 24 | `deadCodeResultsWithGraphIncomingEdges` ignores the projected grant (`if false && access.Scoped() && !BoolVal(row, "in_grant")`), so an out-of-grant source reads as evidence | `go test ./internal/query -run 'TestDeadCodeGraphProbe\|TestDeadCodeKeepsACandidate\|TestDeadCodeInvestigateReports\|TestDeadCodeWeakGranted' -count=1` | `1` (8 failures) |
| 25 | the scoped incoming probe groups with `RETURN DISTINCT` instead of `count(*)`, run against the live backend | `ESHU_NEO4J_URI=bolt://localhost:17987 go test ./internal/query -tags live_nornicdb_dead_code_incoming -run TestLiveNornicDBDeadCodeIncomingProbeSeparatesSameMethodSources -count=1` | `1` (3 ungrouped rows, `incoming_entity_id` came back as the literal `"DISTINCT coalesce(e.uid, e.id)"`) |
| 26 | `missingDeadCodeIncomingEntityIDs` counts a hidden-only entry as coverage, the shape before this pass | `go test ./internal/query -run TestDeadCodeIncomingEntityIDsHiddenOnlyEntryStillRunsTheLegacyProbe -count=1` | `1` (`legacy incoming calls = 0, want 1`) |
| 27 | `crossRepoDeadCodeConsumerReadPlan` binds the grant for a request that named consumers, the shape before this pass | `go test ./internal/query -run TestCrossRepoDeadCodeConsumerSelectorSurvivesABusyGrantedRepository -count=1` | `1` (the requested consumer answered `unknown_needs_evidence` with `consumer_evidence_truncated`) |
| 28 | `deadCodeIncomingProbeMaxResults` keeps the carried-forward 2,500 instead of the re-derived 5,000 | `go test ./internal/query -run TestDeadCodeIncomingProbeMaxResultsMatchesTheManifest -count=1` | `1` (`derived row bound = 5000, want 2500`) |
| 29 | a helper call that does not exist added to `code_dead_code_incoming_probe_nornicdb_live_test.go`, the exact shape `53239cb5e` left behind | `bash scripts/verify-tagged-builds.sh --all` | `1` (`FAIL ./internal/query [live_nornicdb_dead_code_incoming]`) — and `go test ./internal/query -count=1` on the same tree still exits `0`, which is the false green the gate exists for |
| 30 | `split_on` returns its input unsplit for every separator | `bash scripts/verify-tagged-builds.sh --all` and `bash scripts/test-verify-tagged-builds.sh` | `1` both. Three `ERROR`s: the module's two `\|\|` constraints report `alternation split produced 1 alternative(s)`, and `perf5854_ack && perf5740_completion` reports `unrecognized term` because a collapsed conjunction hits the identifier check instead of the alternation count. 26 vetted, 8 skipped. Five self-test cases fail |
| 31 | only the `\|\|` split collapses, which is exactly what a sed that writes a literal `n` would do | the same two commands | `1` both. Two `ERROR`s, both `alternation split produced 1 alternative(s)`, on `perf5854_head \|\| perf5854_main` and `aix \|\| … \|\| solaris` — the module has exactly two `\|\|` constraints. 27 vetted, 8 skipped. The self-test's two alternation cases fail |
| 32 | restore the pre-8k parenthesis flattening, then point the gate at a fixture whose only tagged file is `!(tag_a \|\| tag_b)` and does not compile | `TAGGED_BUILDS_REPO_ROOT=<fixture> bash scripts/verify-tagged-builds.sh` | `0` — `SKIP … no selectable tags` plus `PASS … tags=tag_b`, neither of which compiled the file. With the fail-closed check: `1`, one `ERROR … uses a parenthesised group` |
| 33 | the same, with `tag_a && (tag_b \|\| tag_c)` | the same command | `0` — a lone `SKIP … mixed && and \|\| is not expanded`, a green run over an uncompiled file on a blocking gate. With the check: `1`, one `ERROR`. The no-parentheses spelling `tag_a && tag_b \|\| tag_c` takes the other arm and reports `mixes && and \|\|` |
| 34 | the probe's interior-gap range inverts its bounds (`> gap.hi AND < gap.lo`), so a consumer between two granted ids is never found | `ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 ESHU_POSTGRES_DSN=… go test ./internal/query -run TestCrossRepoDeadCodeUngrantedConsumerProbeLive -count=1` | `1` (2 sub-test failures; `hidden = []string{}, want []string{"ent-busy", "ent-middle", "ent-spread"}`) |
| 35 | the probe orders the grant `DESC` in its `gap` CTE — what a grant sorted in the wrong collation looks like | the same command | `1` (2 sub-test failures, same body: the ranges stop being the complement of the grant) |
| 36 | the above-the-largest range drops its `CROSS JOIN LATERAL … LIMIT 1` and becomes a plain correlated `EXISTS` | the same command, and `go test ./internal/query -run TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe -count=1` | `1` both. The live guard reports `probe fell back to a sequential scan over code_reachability_rows` with `Seq Scan on code_reachability_rows row_1 (cost=0.00..5714.40 rows=27779)` — the hashed subplan drops the per-entity equality; the unit guard reports the missing bound |
| 37 | `bucketCrossRepoDeadCodeResults` ignores the probe's answer (`if false && consumers.HiddenConsumers.has(entityID)`) | `go test ./internal/query -run 'TestCrossRepoDeadCodeKeepsTheHiddenConsumerSignal\|TestCrossRepoDeadCodeHiddenConsumerOutranksStrongGrantedEvidence' -count=1` | `1` (2 failures: the candidate whose only consumer is out of grant came back `"classification":"dead"`, and the mixed one lost its `unknown` bucket entry) |
| 38 | `crossRepoDeadCodeUngrantedConsumers` drops its empty-grant refusal | `go test ./internal/query -run TestCrossRepoDeadCodeProbeRefusesAnEmptyGrant -count=1` | `1` (`hidden = …{"entity-1":struct {}{}}, want empty`) |
| 39 | `crossRepoDeadCodeConsumerReadPlan` sets `SignalGrant` for a request that named consumers | `go test ./internal/query -run 'TestCrossRepoDeadCodeConsumerReadPlan\|TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector\|TestCrossRepoDeadCodeConsumerSelectorSurvivesABusyGrantedRepository' -count=1` | `1` (3 failures; the read plan leaks the grant into the probe and the selector route sends a second statement) |
| 40 | the walk drops its stop condition (`AND EXISTS (… granted …)` becomes `AND TRUE`), so it enumerates every distinct consumer repository instead of stopping at the first ungranted one | `ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 ESHU_POSTGRES_DSN=… go test ./internal/query -run TestCrossRepoDeadCodeUngrantedConsumerProbeLive -count=1` | `1` (4 sub-test failures: both guards fail under BOTH plan modes -- `the recursive walk produced 215 rows, want at most 60` under a custom plan and again under `plan_cache_mode = force_generic_plan`, and the plan guard loses the per-step seek in both). Every entity's verdict is unchanged, which is the point of the row-count guard |
| 41 | the walk's step seeks `>=` instead of `>`, so it never advances past the repository it just found | the same command | `1` (14 sub-test failures, all `cross-repo dead code ungranted consumer probe: context deadline exceeded`) |
| 42 | the final filter selects the granted repositories instead of the ungranted ones (`NOT EXISTS` becomes `EXISTS`) | the same command | `1` (12 sub-test failures; e.g. `hidden = []string{"ent-busy", "ent-middle", "ent-spread"}, want []string(nil)` for a grant that hides nothing) |
| 43 | not a mutation — the gate's first catch on the branch that added it. `code_dead_code_cross_repo_ungranted_probe_live_test.go` carries no build tag, so its `quoteLiteral` joined the `integration` build alongside the one in `cloud_resource_runtime_digest_starvation_live_test.go` | `bash scripts/verify-tagged-builds.sh --all`, then `go vet -tags integration ./internal/query` | `1` and `1` (`quoteLiteral redeclared in this block`), while `go build ./...`, `go vet ./...` and `go test ./internal/query` on the same tree all stayed `0`. Renamed to `crossRepoDeadCodeProbeQuoteLiteral`; both back to `0` |

An earlier attempt at #1 deleted the whole helper body and failed as an unused
import rather than an assertion, which proves nothing; the mutations above keep
the package compiling so the failure is the assertion's.

Rows 18 through 22 are the round-7 pass. Row 18 is the scope-versus-canonical
identity, rows 19 through 21 the three places an out-of-grant incoming edge has
to stop being evidence, and row 22 the sentinel boundary. Each was restored and
its guard rerun at exit `0`.

Rows 24 through 27 are the round-8 pass, and rows 24 and 25 are one change
judged from both sides. Row 24 neuters the projected grant and is judged by the
credential-free guards CI runs; row 25 neuters the grouping clause instead and
is judged against the pinned backend, which is the only place the
`RETURN DISTINCT` corruption is visible at all. Row 26 is the hidden-only
reachability entry and row 27 the consumer selector's place in the page read.
Each was restored and its guard rerun at exit `0`. Row 28 is the round-8 review
follow-up: the ledger bound the merged probe outgrew. Row 29 is the round-8c
one, and it is the only mutation in this table that the default `go test` lane
cannot see at all — which is the whole argument for the gate beside it. Rows 30
and 31 are the round-8d parser guard, run as two mutations because they are two
different defects: 31 is the sed behaviour that was actually there, and 30 is
the broader "the splitter is broken" case, which reddens one constraint more.
Rows 32 and 33 are round-8k, and they are the reverse shape: not a mutation of
the gate but two constraints the shipped gate answered green without compiling
anything. Row 34 is not a mutation either: it is the gate finding a real break
on the branch that introduced it, which is the strongest evidence in this table
that it earns its place. Both are now `ERROR`. The module has no parenthesised and no mixed
constraint, so the sweep's own output is unchanged at 29 vetted / 16 skipped /
exit 0, which is the behaviour-preservation check for a change that only
narrows what the gate will accept.

Rows 40 through 42 are the round-10 pass, which replaced the grant-complement
ranges of rows 34 through 36 with a loose index scan over each producer
entity's distinct consumer repositories. Rows 41 and 42 are ordinary
correctness mutations. Row 40 is not, and it is the one worth reading: dropping
the walk's stop condition leaves every entity's verdict identical and turns a
bounded walk into a full enumeration, so no assertion on the answer can see it.
The guard that catches it reads the recursive term's measured row count out of
`EXPLAIN ANALYZE`. Its budget is measured rather than chosen -- 15 rows shipped,
215 mutated, budget 60 -- and the first budget written for it, 900, sat above
both and passed the mutation it existed to catch. That guard was a false green
until row 40 was run against it, which is the argument for running these at all.

Rows 34 through 39 are the round-9 pass, the one that replaced the unrestricted
signal read with the bounded ungranted-consumer probe. Rows 34 and 35 are the
two ways the grant-complement ranges can stop being the complement — an
inverted interior range and a mis-ordered bound list — and both are judged
against real Postgres, because a fake driver cannot evaluate a range. Row 36 is
the plan property the whole rewrite rests on, and it is the only row here whose
behavioural answer stays correct: the mutated probe returns the right entities
and reads the whole table to do it, which is why the live guard asserts the plan
and not only the result. Rows 37 through 39 are the three Go-side bindings: the
handler consuming the answer, the read refusing an empty grant, and the read
plan keeping the probe away from a request that named consumers. Row 38 was
rewritten before it bit — the first version drove the refusal through
`CrossRepoDeadCodeConsumerEvidence`, whose own guard shadows it, and passed
against the mutation; the guard now calls the read directly. Each was restored
and its guard rerun at exit `0`.

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
