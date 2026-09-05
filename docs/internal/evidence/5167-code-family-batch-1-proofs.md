# #5167 Code Family, Batch 1 — Proof Ledger

The red/green runs, the mutation ledger, the query-plan manifest re-audit and
the read-path cost record for
[#5167 Code Family, Batch 1](5167-code-family-batch-1.md). That note holds the
change and its reasoning, and
[#5167 cross-repo hidden-consumer walk](5167-cross-repo-hidden-consumer-walk.md)
holds the measurement record for the cross-repo hidden-consumer read. The three
are split only because together they outgrow the repository's 500-line file cap,
and nothing here stands on its own.

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
| 29 | a helper call that does not exist added to `code_dead_code_incoming_probe_nornicdb_live_test.go`, the exact shape the commit titled "let the call-graph routes keep one edge query for every caller" left behind | `bash scripts/verify-tagged-builds.sh --all` | `1` (`FAIL ./internal/query [live_nornicdb_dead_code_incoming]`) — and `go test ./internal/query -count=1` on the same tree still exits `0`, which is the false green the gate exists for |
| 30 | `split_on` returns its input unsplit for every separator | `bash scripts/verify-tagged-builds.sh --all` and `bash scripts/test-verify-tagged-builds.sh` | `1` both. Three `ERROR`s: the module's two `\|\|` constraints report `alternation split produced 1 alternative(s)`, and `perf5854_ack && perf5740_completion` reports `unrecognized term` because a collapsed conjunction hits the identifier check instead of the alternation count. 26 vetted, 8 skipped. Five self-test cases fail |
| 31 | only the `\|\|` split collapses, which is exactly what a sed that writes a literal `n` would do | the same two commands | `1` both. Two `ERROR`s, both `alternation split produced 1 alternative(s)`, on `perf5854_head \|\| perf5854_main` and `aix \|\| … \|\| solaris` — the module has exactly two `\|\|` constraints. 27 vetted, 8 skipped. The self-test's two alternation cases fail |
| 32 | restore the pre-8k parenthesis flattening, then point the gate at a fixture whose only tagged file is `!(tag_a \|\| tag_b)` and does not compile | `TAGGED_BUILDS_REPO_ROOT=<fixture> bash scripts/verify-tagged-builds.sh` | `0` — `SKIP … no selectable tags` plus `PASS … tags=tag_b`, neither of which compiled the file. With the fail-closed check: `1`, one `ERROR … uses a parenthesised group` |
| 33 | the same, with `tag_a && (tag_b \|\| tag_c)` | the same command | `0` — a lone `SKIP … mixed && and \|\| is not expanded`, a green run over an uncompiled file on a blocking gate. With the check: `1`, one `ERROR`. The no-parentheses spelling `tag_a && tag_b \|\| tag_c` takes the other arm and reports `mixes && and \|\|` |
| 34 | the probe's interior-gap range inverts its bounds (`> gap.hi AND < gap.lo`), so a consumer between two granted ids is never found | `ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 ESHU_POSTGRES_DSN=… go test ./internal/query -run TestCrossRepoDeadCodeUngrantedConsumerProbeLive -count=1` | `1` (2 sub-test failures; `hidden = []string{}, want []string{"ent-busy", "ent-middle", "ent-spread"}`) |
| 35 | the probe orders the grant `DESC` in its `gap` CTE — what a grant sorted in the wrong collation looks like | the same command | `1` (2 sub-test failures, same body: the ranges stop being the complement of the grant) |
| 36 | the above-the-largest range drops its `CROSS JOIN LATERAL … LIMIT 1` and becomes a plain correlated `EXISTS` | the same command, and `go test ./internal/query -run TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe -count=1` | `1` both. The live guard reports `probe fell back to a sequential scan over code_reachability_rows` with `Seq Scan on code_reachability_rows row_1 (cost=0.00..5714.40 rows=27779)` — the hashed subplan drops the per-entity equality; the unit guard reports the missing bound |
| 37 | `bucketCrossRepoDeadCodeResults` ignores the probe's answer (`if false && consumers.HiddenConsumers.has(entityID)`) | `go test ./internal/query -run 'TestCrossRepoDeadCodeKeepsTheHiddenConsumerSignal\|TestCrossRepoDeadCodeStrongGrantedEvidenceOutranksHiddenConsumer' -count=1` | `1` (2 failures: the candidate whose only consumer is out of grant came back `"classification":"dead"`, and the mixed one kept its `live_by_consumer` answer but lost the count that goes with it — `hidden_consumer_evidence_count = <nil>, want 1`) |
| 38 | the hidden count is not outranked by strong granted evidence (`if false && strongLiveEvidence` around `unknownHiddenCount = 0`), the order this route had before the two dead-code routes were reconciled | `go test ./internal/query -run TestCrossRepoDeadCodeStrongGrantedEvidenceOutranksHiddenConsumer -count=1` | `1` (`candidate_buckets[live_by_consumer] missing entity "producer-strong-plus-hidden"` — a strong granted consumer answered `unknown_needs_evidence`) |
| 39 | `crossRepoDeadCodeUngrantedConsumers` drops its empty-grant refusal | `go test ./internal/query -run TestCrossRepoDeadCodeProbeRefusesAnEmptyGrant -count=1` | `1` (`hidden = …{"entity-1":struct {}{}}, want empty`) |
| 40 | `crossRepoDeadCodeConsumerReadPlan` sets `SignalGrant` for a request that named consumers | `go test ./internal/query -run 'TestCrossRepoDeadCodeConsumerReadPlan\|TestCrossRepoDeadCodeHiddenCountHonoursTheConsumerSelector\|TestCrossRepoDeadCodeConsumerSelectorSurvivesABusyGrantedRepository' -count=1` | `1` (3 failures; the read plan leaks the grant into the probe and the selector route sends a second statement) |
| 41 | the walk drops its stop condition (`AND EXISTS (… granted …)` becomes `AND TRUE`), so it enumerates every distinct consumer pair instead of stopping at the first hidden one | `ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 ESHU_POSTGRES_DSN=… go test ./internal/query -run TestCrossRepoDeadCodeUngrantedConsumerProbeLive -count=1` | `1` (4 sub-test failures: both guards fail under BOTH plan modes -- `the recursive walk produced 215 rows, want at most 60` under a custom plan and again under `plan_cache_mode = force_generic_plan`, and the plan guard loses the per-step seek in both). Every entity's verdict is unchanged, which is the point of the row-count guard |
| 42 | the walk's step seeks `>=` instead of `>`, so it never advances past the repository it just found | the same command | `1` (14 sub-test failures, all `cross-repo dead code ungranted consumer probe: context deadline exceeded`) |
| 43 | the final filter selects the granted repositories instead of the ungranted ones (`NOT EXISTS` becomes `EXISTS`) | the same command | `1` (12 sub-test failures; e.g. `hidden = []string{"ent-busy", "ent-middle", "ent-spread"}, want []string(nil)` for a grant that hides nothing) |
| 44 | not a mutation — the gate's first catch on the branch that added it. `code_dead_code_cross_repo_ungranted_probe_live_test.go` carries no build tag, so its `quoteLiteral` joined the `integration` build alongside the one in `cloud_resource_runtime_digest_starvation_live_test.go` | `bash scripts/verify-tagged-builds.sh --all`, then `go vet -tags integration ./internal/query` | `1` and `1` (`quoteLiteral redeclared in this block`), while `go build ./...`, `go vet ./...` and `go test ./internal/query` on the same tree all stayed `0`. Renamed to `crossRepoDeadCodeProbeQuoteLiteral`; both back to `0` |
| 45 | `platform_term`'s GOARCH arm carries the `mips*` prefix it shipped with, against a fixture whose only tagged file is `//go:build mipsmock` and does not compile | `TAGGED_BUILDS_REPO_ROOT=<fixture> bash scripts/verify-tagged-builds.sh` | `0` — `SKIP … platform-gated (mipsmock)`, "vetted 0 build configuration(s)", over a package that does not build. With the six exact `mips` GOARCH names: `1`, `FAIL … tags=mipsmock` naming the undefined helper |
| 46 | the probe statement reverts to the walk migration 100 shipped -- stop at a consumer repository's first row instead of seeking its active one | `ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 ESHU_POSTGRES_DSN=… go test ./internal/query -run TestCrossRepoDeadCodeUngrantedConsumerProbeLive -count=1`, and `go test ./internal/query -run TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe -count=1` | `1` both (6 sub-test failures, both guards under both plan modes: `the walk touched 5946 buffers for one entity, want at most 200`, and `no plan node carries the walk's per-step seek`). Every entity's answer is unchanged, including `ent-retained`'s, which is why the guard measures buffers |
| 47 | the liveness seek drops `AND live_row.scope_id = pair.scope_id`, so its index condition stops at the pair and the generation becomes a filter over the pair's retained rows | the same live command, and `go test ./internal/query -run TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe -count=1` | `1` both (2 sub-test failures, both plan modes: `no index condition carries the full (entity_id, repository_id, scope_id, generation_id) liveness seek`; the unit guard reports the missing equality). The answer is unchanged again, and so are the buffers -- the mutated seek filters inside the index rather than fetching heap rows, which is why the plan guard and not the buffer budget is what catches this one |
| 48 | migration 101 builds `(entity_id, repository_id)` instead of the four-column key | the same live command | `1` (2 sub-test failures, both plan modes, same liveness-seek message: with the last two key columns gone the seek cannot be an index condition at all) |
| 49 | migration 102 stops dropping migration 100's index (`DROP INDEX …` becomes `SELECT 1`) | `go test ./internal/storage/postgres -run TestCodeReachabilityWalkIndexIsCreatedOnceAndNeverDropped -count=1` | `1` (`code_reachability_entity_repository_idx has 0 creates and 0 drops, want 0 and 1`) -- a build that keeps both indexes answers every question correctly and makes every reachability write maintain a redundant btree. The guard moved: this row was first captured against the live probe at the commit titled "measure what a retained generation costs the hidden-consumer walk", when the proof schema still applied migration 100 and `assertCrossRepoDeadCodeProbeIndexExists` could see the two-column index survive. Migration 100 was deleted and `crossRepoDeadCodeProbeIndexMigrations` now lists only 101 and 102, so nothing builds that index in the live fixture and its count reads 0 whether or not 102 drops anything -- the old pointer would have passed on this mutation. Re-captured against the hermetic guard, which asserts the drop exists rather than the index's absence |
| 50 | migration 100 is put back, so a create of the two-column index sits ahead of the drop again | `go test ./internal/storage/postgres -run 'TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay|TestCodeReachabilityWalkIndexIsCreatedOnceAndNeverDropped' -count=1`, and `ESHU_POSTGRES_TEST_DSN=… go test -tags integration ./internal/storage/postgres -run TestCodeReachabilityIndexMigrationsReapplyWithoutRebuildLive -count=1` | `1` both. Hermetic: `index code_reachability_entity_repository_idx is created by [… /100_…sql] and dropped by [… /102_…sql], so every bootstrap rebuilds it`. Live: the second pass reports `"CREATE INDEX CONCURRENTLY IF NOT EXISTS code_reachability_entity_repository_idx" built index …` and `"DROP INDEX CONCURRENTLY IF EXISTS …" dropped index …`. The live half did NOT bite on its first run — the proof selected the definitions it applied from a fixed list of names, so restoring the migration left the applied set unchanged; it now discovers every definition naming the index family, and the row above is the re-run |
| 51 | the granted skip is removed, so every step seeks the next PAIR again | `ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 ESHU_POSTGRES_DSN=… go test ./internal/query -run TestCrossRepoDeadCodeUngrantedConsumerProbeLive -count=1` | `1` (4 sub-test failures, both plan modes: `the walk produced 51 rows for one entity, want at most 8; a granted repository is being walked scope by scope`, and `no index condition carries the granted repository skip`). Every entity's answer is identical, which is why the guard counts rows |
| 52 | the skip is applied unconditionally, so an ungranted repository's remaining scopes are never walked | the same live command | `1` (3 sub-test failures). The answer guard is the one that matters here: `hidden = []string{"ent-scopes-granted"}, want []string{"ent-scopes-granted", "ent-scopes-ungranted"}` — the live row in the fifty-first scope of an ungranted repository is missed, which is the whole reason the skip is conditional |
| 53 | `replayRebuiltIndexNames` is emptied, so the known create/drop pair would be silently allowed rather than asserted | `go test ./internal/storage/postgres -run TestBootstrapDefinitionsDoNotRebuildIndexesOnEveryReplay -count=1` | `1` (`index fact_records_identity_epoch_idx is created by [… /069_…sql … /077_…sql] and dropped by [… /076_…sql]`). The list is compared for equality, not membership, so a second offender fails and fixing this pre-existing one fails until the entry goes with it |
| 54 | the liveness generation arrives as a join to `ingestion_scopes` on the outer row instead of the scalar subquery -- the shape that shipped before the commit titled "seek the active row instead of letting the planner reorder for it" | `go test ./internal/query -run TestCrossRepoDeadCodeSignalReadIsTheBoundedUngrantedProbe -count=1` | `1` (`probe is missing "AND live_row.generation_id = (", so a step scans a pair's retained generations instead of seeking its active row`). The paired absence assertion needs its own mutation, because the missing-subquery check `t.Fatalf`s first: reintroducing `JOIN code_reachability_rows AS live_row` BESIDE a surviving subquery -- the likelier drift -- gives `1` (`probe joins the liveness row on the outer pair; the planner may then reorder the generation out of the Index Cond`). No answer changes under either mutation, and no plan assertion catches them at unit-test scale, which is why the pin is on statement text |
| 55 | migration 103's `CREATE INDEX` becomes `SELECT 1`, so the consumer-evidence page has no index in its own `ORDER BY` | `go test ./internal/storage/postgres -run TestCodeReachabilityPageRankIndexIsCreatedOnceAndNeverDropped -count=1`, `ESHU_POSTGRES_TEST_DSN=… go test -tags integration ./internal/storage/postgres -run TestCodeReachabilityIndexMigrationsReapplyWithoutRebuildLive -count=1`, and `ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 ESHU_POSTGRES_DSN=… go test ./internal/query -run TestCrossRepoDeadCodeConsumerEvidencePageBoundLive -count=1` | `1` all three. Hermetic: `code_reachability_entity_confidence_rank_idx has 0 creates and 0 drops, want 1 and 0`. Live replay: `found 2 definition(s) naming [code_reachability_entity_repository code_reachability_entity_confidence], want at least the walk index's create and drop plus the page-rank index's create`. Page proof: `the page read scanned 30015 code_reachability_rows rows, want at most 1200` under BOTH plan modes, while the answer guard stayed green — which is the whole point of measuring the work, since the rows the page returns are identical either way |
| 56 | migration 103 builds the same five columns in another order, `(entity_id, depth, confidence DESC, repository_id, root_entity_id)` | the page proof command from row 55 | `1` (3 sub-test failures). The index exists and is still useless: `… is defined as "… USING btree (entity_id, depth, confidence DESC, …)", want its key to be (entity_id, confidence DESC, depth, repository_id, root_entity_id)`, and the work guard reports the same `scanned 30015 … want at most 1200` in both plan modes. The answer guard stayed green again |
| 57 | the page statement orders `row.entity_id DESC`, so the busy entity is read first and spends the whole cap | the page proof command from row 55 | `1` (2 sub-test failures). This is the mutation the ANSWER guard exists for and the index guard cannot see: `ent-001 carries 1 truncation markers, want 0`, `ent-001 has 1 consumer rows, want 3`, with the index guard still passing. Restored, all three guards back to `0` |
| 58 | the page statement orders `(entity_id, depth, confidence DESC, repository_id, root_entity_id)`, swapping the ranking column with its first tie-break, while migration 103 is untouched | `go test ./internal/query -run TestCrossRepoDeadCodeConsumerPageOrderMatchesItsIndexKey -count=1` | `1` (`the evidence page orders by (entity_id, depth, confidence DESC, …) and migration 103's index key is (entity_id, confidence DESC, depth, …); they have to be the same columns in the same order`). Unit lane, no Postgres: the answer is identical under this mutation, so nothing behavioural moves and only a pin that reads both files can see it |
| 59 | the other side of the same pin — migration 103 builds `(entity_id, confidence DESC, repository_id, depth, root_entity_id)` while the statement is untouched | the same command | `1`, the same message with the two lists the other way round. Both directions matter: the drift can start in either file, and a pin that restated the columns instead of reading them would agree with itself while the deployment built something else |
| 60 | migration 103's build is neutered, judged by the live plan guard's sort assertion rather than by its row budget | `ESHU_CROSS_REPO_DEAD_CODE_PROBE_LIVE=1 ESHU_POSTGRES_DSN=… go test ./internal/query -run TestCrossRepoDeadCodeConsumerEvidencePageBoundLive -count=1` | `1` (`the page read plans [Incremental Sort] under its Limit; the index is meant to supply the order so nothing has to sort`, both plan modes, beside row 55's row-budget failure). The row count says the `LIMIT` held; this says why, and it is the assertion that survives a future planner finding some other way to over-read |

| 61 | the page statement drops the tiebreak and orders by five columns while migration 103 keeps seven | `go test ./internal/query -run TestCrossRepoDeadCodeConsumerPageOrderMatchesItsIndexKey -count=1`, then the live page proof | `1` (`read the page's ORDER BY as (…root_entity_id), want seven columns including confidence DESC`) and `0`. The live proof staying GREEN is the point of this row: the index still has seven columns, so its key guard passes and its plan is unchanged — a statement that quietly stops asking for the tiebreak is invisible to every live assertion, and only a pin that reads the statement itself catches it |
| 62 | migration 103 drops the tiebreak from the key while the statement keeps it | the same two commands | `1` and `1` (5 failing sub-tests). The pin reports `migration 103's index key is (entity_id, confidence DESC, depth, repository_id, root_entity_id)` beside the statement's seven, and the live index guard reports the same mismatch against `pg_indexes` |
| 63 | the live fixture's `crossRepoDeadCodeConsumerPageRetainedGenerations` is set to 0, so the retention arm retains nothing | the live page proof | `1` (`the retention arm scanned 1001 rows, no more than the no-retention budget of 1200; the fixture is not carrying retained generations and this guard is vacuous`). The ceiling would have passed — 1,001 is comfortably under the retention budget — which is why the arm carries a floor as well |

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
anything. Row 44 is not a mutation either: it is the gate finding a real break
on the branch that introduced it, which is the strongest evidence in this table
that it earns its place. Row 45 is the third of that shape and the sharpest:
the platform list matched `mips*` as a prefix, so a project tag named
`mipsmock` was classified a GOARCH, skipped, and never compiled — the gate
answering "vetted 0", exit 0, over a package that does not build. Every GOOS,
GOARCH and meta-tag name is spelled out now, copied from `internal/syslist`,
and none of those patterns may become a glob again. Both are now `ERROR`. The module has no parenthesised and no mixed
constraint, so the sweep's own output is unchanged at 29 vetted / 16 skipped /
exit 0, which is the behaviour-preservation check for a change that only
narrows what the gate will accept.

Rows 41 through 43 are the round-10 pass, which replaced the grant-complement
ranges of rows 34 through 36 with a loose index scan over each producer
entity's distinct consumer repositories. Rows 42 and 43 are ordinary
correctness mutations. Row 41 is not, and it is the one worth reading: dropping
the walk's stop condition leaves every entity's verdict identical and turns a
bounded walk into a full enumeration, so no assertion on the answer can see it.
The guard that catches it reads the recursive term's measured row count out of
`EXPLAIN ANALYZE`. Its budget is measured rather than chosen -- 15 rows shipped,
215 mutated, budget 60 -- and the first budget written for it, 900, sat above
both and passed the mutation it existed to catch. That guard was a false green
until row 41 was run against it, which is the argument for running these at all.

Rows 34 through 37, 39 and 40 are the round-9 pass, the one that replaced the unrestricted
signal read with the bounded ungranted-consumer probe. Rows 34 and 35 are the
two ways the grant-complement ranges can stop being the complement — an
inverted interior range and a mis-ordered bound list — and both are judged
against real Postgres, because a fake driver cannot evaluate a range. Row 36 is
the plan property the whole rewrite rests on, and it is the only row here whose
behavioural answer stays correct: the mutated probe returns the right entities
and reads the whole table to do it, which is why the live guard asserts the plan
and not only the result. Rows 37, 39 and 40 are the three Go-side bindings: the
handler consuming the answer, the read refusing an empty grant, and the read
plan keeping the probe away from a request that named consumers. Row 39 was
rewritten before it bit — the first version drove the refusal through
`CrossRepoDeadCodeConsumerEvidence`, whose own guard shadows it, and passed
against the mutation; the guard now calls the read directly. Each was restored
and its guard rerun at exit `0`.

Row 38 sits inside that run of numbers without belonging to it. It is the later
pass that reconciled the two dead-code routes' order for mixed evidence, and it
restores the order this route had before that: a hidden consumer outranking a
strong granted one. Its guard is
`TestCrossRepoDeadCodeStrongGrantedEvidenceOutranksHiddenConsumer`.

Rows 46 through 49 are the round-11 pass, the one that made a walk step
independent of how many superseded generations the retention runner still
keeps. Row 46 is the whole previous shape put back, and it is the row the
buffer budget exists for: 5,946 buffers against 24 for the shipped walk, with
every entity's answer identical either way. Rows 46 and 47 take the seek apart
from the two sides it can break from -- the statement dropping a key column
from the condition, and the migration dropping it from the index -- and neither
moves the buffer count, because a mutated seek filters inside the index instead
of fetching heap rows. That is why the plan guard asserts all four key columns
rather than trusting the budget to notice. Row 49 is the index the migrations
must NOT leave behind. Each was restored and its guard rerun at exit `0`.

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

## Query-Plan Source Coverage

`go test ./internal/queryplan` was red on this branch before this pass, and no
earlier verification list ran that package. Six callsites failed
`TestHotCypherManifestCoversEveryProductionQueryCall`, because adding a grant
predicate changes the enclosing symbol's `source_sha256` and the manifest
freezes it:

```text
code_call_graph_metrics.go:(*CodeHandler).callGraphMetricsData: hot callsite source_sha256 does not match production symbol
code_complexity_queries.go:(*CodeHandler).listMostComplexFunctions: grandfathered source_sha256 does not match production symbol
```

The other four — `lookupComplexityRowByName`, `deadCodeCandidateRows`,
`inspectCodeQuality`, `graphSummaryHotEntities` — printed the same
grandfathered-digest line. That is the gate working as designed: a changed
digest forces the owning callsite through a typed non-hot audit rather than
letting a prose `non_hot_reason` carry forward. The five grandfathered prose
entries become typed dispositions carrying the bound each read already enforces,
and leave `grandfatheredNonHotSourceDigests`. Later passes move three digests
again — `listMostComplexFunctions` for the anchor fix, `callGraphMetricsData`
for its grantless-caller refusal, `graphSummaryHotEntities` for a corrected
comment — each re-recorded against the production symbol with its disposition
and bound re-audited unchanged. `handler-hot-cypher.yaml` ends this branch
untouched: `callGraphMetricsEdgesCypher` carries no grant, so its
`source_sha256` and the `cypher_sha256` for `QP-CALL-GRAPH-HUBS` and
`QP-CALL-GRAPH-RECURSIVE` are the values already committed:

| Callsite | Class | Bound |
| --- | --- | --- |
| `listMostComplexFunctions` | `label_inventory` | `Function`, 101 rows (`complexityMaxListLimit` + 1) |
| `lookupComplexityRowByName` | `keyed_support` | single key `$entity_name`, 3 rows (`complexityNameCandidateLimit` + 1) |
| `deadCodeCandidateRows` | `label_inventory` | one candidate label per page from the closed `deadCodeCandidateLabels` set, 250 rows (`deadCodeCandidateQueryMax`) |
| `inspectCodeQuality` | `label_inventory` | `Function`, 101 rows (`codeQualityMaxLimit` + 1) |
| `graphSummaryHotEntities` | `keyed_support` | single key `$repo_id`, 50001 rows (`callGraphMetricsEdgeScanLimit` + 1) |
| `deadCodeResultsWithGraphIncomingEdges` | `keyed_support` | bounded key batch of one candidate page, 250 keys (`deadCodeCandidateQueryMax`), 2500 rows (one per key per resolution method) |

The sixth entry is the incoming-edge probe. It kept its prose disposition until
this pass; moving it to `code_dead_code_candidate_entity.go` and giving it a
second statement forced the same audit, and a new callsite may not use
`non_hot_reason` at all, so it left the grandfather ledger for the typed row
above.

## Why The internal/query Files Were Split

On origin/main `code_dead_code.go` was 496 lines and `code_dead_code_scan.go`
was 468; this change pushed both over the 500-line cap, and
`code_dead_code_cross_repo.go` followed later. The candidate-page request type,
the scan budget helpers, the candidate-label predicate, the cross-repo
consumer-evidence filter and, in the round-7 pass, the whole incoming-edge probe
family moved to sibling files that already own those families rather than to new
ones, because `internal/query`'s non-test file set is pinned by the dirgate
grandfather ledger.

## What The Change Costs On The Read Path

No-Regression Evidence: every predicate this change adds is an indexed equality
or an `ANY()`/`IN` membership test against the caller's grant, on a node or
column the query already matched, and it lands ahead of the existing
`SKIP`/`LIMIT` (Cypher) or `LIMIT`/`OFFSET` (SQL), so a scoped page is drawn
from the granted set instead of a cross-tenant-polluted one. A scoped caller
reads no more rows than before, save the one widened `DISTINCT` key declared
below, and on the routes that were corpus-wide it reads fewer. On the SQL side
the grant column is `content_entities.repo_id` / `content_files.repo_id`, plus
`code_reachability_rows.repository_id` — the same columns those queries'
single-repository branches already filter on.

Two shapes do change, and both are declared. `listMostComplexFunctions` swaps
its `OPTIONAL MATCH` for a required `MATCH` over the same
`CONTAINS`/`REPO_CONTAINS` path for a scoped caller or a supplied `repo_id`,
which removes a clause between the anchor and the `RETURN` rather than adding
one. The cross-repo consumer read runs one extra statement per scoped request,
on a route that already issues a paged candidate scan plus per-entity probes.
That statement is a bounded per-entity existence probe backed by a new index,
measured rather than asserted — see [#5167 cross-repo hidden-consumer
walk](5167-cross-repo-hidden-consumer-walk.md) for its plan, its numbers, and
the three shapes withdrawn on measurements.

The incoming-edge probe is the third shape, and it is measured. A scoped caller
runs one graph statement, as before:
`buildDeadCodeScopedIncomingBatchProbeCypher` expands the candidate's incoming
edges once, optionally matches the source's repository, and projects the grant
per row as `in_grant`, grouping on `(entity, method, in_grant)` with `count(*)`
rather than `RETURN DISTINCT`. It replaced a pair — a grant-bound probe plus the
unrestricted one, diffed row by row — which both cost more and could not see an
out-of-grant source whose resolution method a granted source also carried. On
one entity with 5,000 incoming edges split across two repositories, four
interleaved runs of 15 iterations against the pinned NornicDB v1.2.3: median
274–303 µs against 497–583 µs for the withdrawn pair, and 2–14% above what a
single probe costs alone. The full table and the mistake in the first
measurement are under "One Probe, Because Two Could Not See A Same-Method
Source" in [#5167 code family batch 1](5167-code-family-batch-1.md).

The grouping key does widen. It carries `in_grant` as a third column, so an
entity and method reachable from both a granted and an ungranted consumer
returns two rows where it returned one — at most 2x over one candidate page, and
the bound that follows from it is re-derived in
`go/internal/queryplan/testdata/query-source-coverage.yaml`. The SQL half adds
no predicate and no scan: the grant is a projected boolean over
`code_reachability_rows.repository_id`, a column of the table the read already
scans, not one the statement returned before. Every one of these costs falls
only on scoped callers, who could not reach these routes before this PR. Nothing
here puts a filter in a `WITH`-attached `WHERE` (not evaluated as a filter on
NornicDB) or guards a disjunct with `$param <> ''` (poisons the enclosing `OR`
on NornicDB) — see
[NornicDB Query-Shape Pitfalls](../../public/reference/nornicdb-query-pitfalls.md).

For an unscoped shared, admin, or local caller every grant predicate renders
empty and every grant parameter is unbound, so the query text those callers
execute is byte-identical to before — with two deliberate exceptions, both on
`POST /api/v0/code/complexity`, and both about a `repo_id` the caller supplied
and the query then ignored. `lookupComplexityRowByID` now emits
`WHERE repo.id = $repo_id` whenever `repo_id` is supplied, so
`{"entity_id":"X","repo_id":"A"}` used to return X's row from repository B and
now returns not-found, and a `function_name` sent with it no longer softens
that: the name fallback runs only for an id lookup bound to no repository, the
one case where an empty result proves the id stale rather than held elsewhere
(`complexityIDLookupIsRepositoryBound`). `listMostComplexFunctions` takes the
required Repository
anchor on the same condition, so `{"repo_id":"A"}` ranks A's functions instead
of the whole corpus with other repositories' rows nulled. Both are user-visible
row-set fixes, documented in the route's OpenAPI description and in
[HTTP API — Code](../../public/reference/http-api/code.md), and pinned by
`TestComplexityByEntityIDHonoursASuppliedRepoID` and
`TestComplexityListUnscopedRepoIDSelectorFiltersToThatRepository`.

Byte-identity is pinned for the one hot read carrying committed plan evidence.
`callGraphMetricsEdgesCypher` is untouched, so its whole manifest entry
(`go/internal/queryplan/testdata/handler-hot-cypher.yaml`) holds the digests it
already held, and its accepted plan block (`NodeIndexSeek`, `Expand`; forbidden
`AllNodesScan`, `CartesianProduct`, `UnboundedExpand`) describes what every
caller emits rather than only an unscoped one.
`TestCallGraphMetricsCypherIsTheSameForEveryCaller` and
`TestCallGraphMetricsUnscopedCypherIsUnchanged` keep it that way.

No-Observability-Change: no metric instrument, metric label, span, log event,
route, worker, queue, lease, or runtime knob is added or renamed. The cross-repo
consumer read's existing `postgres.query` span gains one attribute,
`db.rows.consumer_signal_entities`, which counts the producer entities the
ungranted-consumer probe flagged. Operators keep diagnosing these ten routes
through the governance-audit read-authorization events in
`go/internal/query/auth_audit.go` — `DecisionAllowed` / `scoped_read_allowed`
(`recordScopedReadAuthorized`) and `DecisionDenied` with the route's reason code
(`recordScopedRouteAuthorizationDeniedWithReason`), both stamped with tenant,
workspace, actor hash, and correlation id — plus the existing per-capability
handler spans (`SpanQueryCodeTopicInvestigation`,
`SpanQueryDeadCodeInvestigation`, `SpanQueryCallGraphMetrics`,
`SpanQueryCodeStructuralInventory`, `SpanQueryHardcodedSecretInvestigation`) and
the `eshu_dp_postgres_query_duration_seconds` /
`eshu_dp_neo4j_query_duration_seconds` histograms. A caller that now reads fewer
rows shows up as a smaller `count`/`truncated` in the same response envelope.
