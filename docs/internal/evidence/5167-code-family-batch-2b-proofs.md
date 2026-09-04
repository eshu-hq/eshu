# #5167 Code Family, Batch 2b — Proof Ledger

The red/green runs and mutation ledger for
[#5167 Code Family, Batch 2b](5167-code-family-batch-2b.md). That document holds
the change and its reasoning; this one holds the proofs. They are split only
because together they outgrow the repository's 500-line Markdown cap.

## The Red Came First, And It Came From A Real Backend

This batch is unusual in where its red lives. The defect was clause attachment,
and a text-capture test cannot see clause attachment — the predicate string is
present either way. So the reds below were produced against a standalone pinned
NornicDB before any production line changed, in the prove-the-theory-first phase
recorded in the batch document, and the same files are the green now.

Image: `timothyswt/nornicdb-cpu-bge:v1.2.3@sha256:4dfa887d…`, Bolt on host port
17987, `NORNICDB_EMBEDDING_ENABLED=false`. Fixture: two repositories, the caller
granted one; the anchor calls one in-grant callee, one out-of-grant callee, and
one callee the graph attributes to no repository at all — the row an
`OPTIONAL MATCH`-attached predicate keeps.

| Live test | Red, before the change | Green, after |
| --- | --- | --- |
| `TestLiveNornicDBRelationshipStoryMustNotLeakUngrantedRows/outgoing` | `the scoped outgoing read returned the out-of-grant row "LiveClauseUngrantedCallee"` — 3 rows | PASS |
| `…/incoming` | `the scoped incoming read returned the out-of-grant row "LiveClauseUngrantedCaller"` — 2 rows | PASS |
| `TestLiveNornicDBRelationshipStoryCompatBuilderMustNotLeakUngrantedRows` | 5 rows: every `CALLS` edge in the graph, the anchor predicate inert too | PASS |
| `TestLiveNornicDBRelationshipStoryClassMethodsHaveNoRepositoryBinding` | `returned the out-of-grant method … with no grant in the statement` | PASS |
| `TestLiveNornicDBRelationshipStoryInheritanceDepthHasNoRepositoryBinding` | `the inheritance walk crossed into the out-of-grant repository` | PASS |
| `TestLiveNornicDBCallChainOneHopMustNotLeakUngrantedTargets` | `the bounded one-hop traversal returned the out-of-bound target …` — 3 rows | PASS |

Both live suites: `exit 0`.

```text
cd go && go test ./internal/query -tags live_nornicdb_relationship_story -run TestLiveNornicDB -count=1   # 0
cd go && go test ./internal/query -tags live_nornicdb_call_chain          -run TestLiveNornicDB -count=1   # 0
```

Three live tests are measurement pins rather than wanted behaviour, and say so
in their own doc comments: `TestLiveNornicDBPathListPredicateBehaviour` (every
way of writing a path-wide set bound, with the measured row count for each),
`TestLiveNornicDBRepositoryBindingLadder` (which clause positions bind a
Repository at all), and
`TestLiveNornicDBCallChainShippedNornicDBBuilderDoesNotParse` (the pinned build
rejects a shape the pitfalls page called safe). Pinning the measured value means
a later NornicDB build that changes it is seen rather than silently absorbed.

One correction belongs in this record. The first seeded fixture omitted the
`REPO_CONTAINS` edges, and the binding ladder caught it immediately — every
Repository lookup returned zero rows. Every number reported anywhere in this
batch comes from the corrected seed.

## Unit And Route Coverage

The fake-based tests were written alongside the production change and have no
prior behavioural red; every one of them is instead proven mutation-sensitive by
the BITES table below. They exist because the live suites need a backend and CI
does not have one.

`storyClauseGraph` (`go/internal/query/code_story_grant_clause_fake_test.go`) is
what makes them worth having. It reads the emitted statement far enough to
separate predicates attached to the anchoring `MATCH` from predicates attached
after an `OPTIONAL MATCH`, applies the first list as a row filter, and applies
the second only by nulling that optional pattern's columns — which is what the
backend does. `TestStoryClauseGraphKeepsOptionalMatchRows` proves the fake can
still fail: handed a grant stranded on an `OPTIONAL MATCH`, it returns both rows
with the optional column nulled, exactly as measured. It is a sibling of batch
1's `evaluatingRepositoryGraph`, not a replacement: that fake reads the statement
against a `<alias>:Repository` binding, and these statements bind their grant on
the driving row's own `repo_id`.

| Test | Covers |
| --- | --- |
| `TestRelationshipStoryFiltersByRepositoryGrant` (2 backends) | out-of-grant neighbour absent from the response body |
| `TestRelationshipStorySharedKeyReadIsUnchanged` (2 backends) | unscoped row set and statement text unchanged |
| `TestRelationshipStoryEmptyGrantReachesNoBackend` | no graph read, no content read, route's own `not_found` |
| `TestRelationshipStoryResolvesAScopeOnlyGrantToItsRepository` | `git-repository-scope:` grant resolves to the repository it owns |
| `TestRelationshipStoryAmbiguousCandidatesStayInGrant` | candidate list bound; the corpus-wide any-repo search is not used |
| `TestRelationshipStoryClassHierarchyStaysInGrant` | class methods and inheritance depth bound end to end |
| `TestRelationshipStoryOverrideRowsStayInGrant` | the `OVERRIDES` target that could leave the anchored repository |
| `TestRelationshipStoryBuildersBindTheGrantInTheAnchoringMatch` (10 statements) | clause position, per statement |
| `TestRelationshipStoryBuildersCarryNoGrantForAnUnscopedCaller` (7) | no grant array rendered for a shared-key caller |
| `TestCallChainFiltersByRepositoryGrant` | out-of-grant callee absent from the response body |
| `TestCallChainBoundsEveryFrontierHop` | a chain that exists only through an out-of-grant hop is absent, not hidden |
| `TestCallChainEmptyGrantReachesNoBackend` | no graph read, route's own `"chains": []` |
| `TestCallChainSharedKeyReadIsUnchanged` | unscoped row set and statement text unchanged |
| `TestCallChainResolvesAScopeOnlyGrantToItsRepository` | scope-only grant |
| `TestCallChainOneHopBindsTheGrantInTheAnchoringMatch` | clause position for both predicates |
| `TestRelationshipMetadataAnchorBindsTheGrant` | the shared anchor's grant and its absence for an unscoped caller |
| `TestExactGraphEntityCandidatesRefuseAnUngrantedRepository` | the defense-in-depth SQL check |

Two existing shipped-text pins were rewritten rather than deleted, because they
pinned the inert text:
`TestRelationshipStoryGraphCypherCrossRepoScopesAnchorAndRelatedRepositories`
now asserts the predicates are on the entity nodes and fails if
`sourceRepo.id IN` or `targetRepo.id IN` comes back, and
`TestRelationshipStoryGraphCypherRepoScopedKeepsBothEndpointsInRepository` now
asserts `source.repo_id = $repo_id` plus no grant array for an unscoped caller.

## BITES — Each Binding Proved To Bite

Each row breaks one production binding, runs the guard, restores the file from
`HEAD`, and records the exit code directly (`cmd; echo $?`, never after a pipe).
Every mutation was restored and the tree verified clean afterwards. The driver
is a scratch script, not committed.

| # | Mutation | Guard run | Exit |
| --- | --- | --- | --- |
| 1 | `relationshipStoryGrantPredicates` writes on the `sourceRepo`/`targetRepo` aliases again | `-run 'TestRelationshipStoryFiltersByRepositoryGrant\|TestRelationshipStoryBuildersBindTheGrantInTheAnchoringMatch'` | `1` |
| 2 | `relationshipStoryGrantPredicates` returns nil for every caller | `-run 'TestRelationshipStoryFiltersByRepositoryGrant\|TestRelationshipStoryClassHierarchyStaysInGrant\|TestRelationshipStoryOverrideRowsStayInGrant'` | `1` |
| 3 | the compat story builder puts its `WHERE` back after both `OPTIONAL MATCH` clauses | `-run 'TestRelationshipStoryFiltersByRepositoryGrant\|TestRelationshipStoryBuildersBindTheGrantInTheAnchoringMatch'` | `1` |
| 4 | `relationshipStoryGrantBlocked` stops reporting blocked | `-run TestRelationshipStoryEmptyGrantReachesNoBackend` | `1` |
| 5 | `relationshipStoryGrantedCandidates` falls back to the corpus-wide any-repo search | `-run TestRelationshipStoryAmbiguousCandidatesStayInGrant` | `1` |
| 6 | `nornicDBCallChainOneHopRows` puts both predicates back after the `OPTIONAL MATCH` clauses | `-run 'TestCallChainFiltersByRepositoryGrant\|TestCallChainBoundsEveryFrontierHop\|TestCallChainOneHopBindsTheGrantInTheAnchoringMatch'` | `1` |
| 7 | `nornicDBRelationshipMetadataPredicate` drops the grant condition | `-run TestRelationshipMetadataAnchorBindsTheGrant` | `1` |
| 8 | `handleCallChain` drops the empty-grant gate | `-run TestCallChainEmptyGrantReachesNoBackend` | `1` |
| 9 | `resolveExactGraphEntityCandidates` drops its grant check | `-run TestExactGraphEntityCandidatesRefuseAnUngrantedRepository` | `1` |
| 10 | `codeGrantAccessFilter` drops `WithCanonicalScopeRepositories` | `-run 'TestRelationshipStoryResolvesAScopeOnlyGrantToItsRepository\|TestCallChainResolvesAScopeOnlyGrantToItsRepository'` | `1` |

Rows 4 and 7 are the two worth reading the history of, because both passed at
`0` on the first attempt and that was a finding about the tests, not a pass.

Row 4's guard asserted that a grantless caller reached no backend but only
watched the two search methods, and the `entity_id` branch's refusal happens
after `GetEntityContent` — a content read the top-level gate is what prevents.
The fake now records entity-id lookups too, and the guard fails at `1`.

Row 7 had no guard at all. Breaking the shared metadata anchor's grant changed
no call-chain response, because the one-hop read already drops an out-of-grant
hop, so every route-level assertion still passed. The predicate is defense in
depth on this route and the only repository binding on the statement that
resolves an endpoint by name — and it is shared with
`POST /api/v0/code/relationships`. A route-level assertion cannot judge it, so
`TestRelationshipMetadataAnchorBindsTheGrant` judges the statement instead, and
the mutation now fails at `1`.

Neither was papered over by widening a `-run` pattern until something red. Both
were treated as missing coverage, which is what a BITES row passing at `0`
means.

## Verification

Run after the last edit, exit codes captured directly:

```text
cd go && go test ./internal/query ./internal/mcp/... ./internal/queryplan -count=1   # 0
cd go && go vet ./internal/query ./internal/mcp ./internal/queryplan                 # 0
cd go && go test ./internal/query -tags live_nornicdb_relationship_story \
  -run TestLiveNornicDB -count=1                                                     # 0
cd go && go test ./internal/query -tags live_nornicdb_call_chain \
  -run TestLiveNornicDB -count=1                                                     # 0
mkdocs build --strict --clean --config-file docs/mkdocs.yml                           # 0
git diff --check                                                                      # 0
```

No-Regression Evidence: see the batch document's marker; this file adds no
separate performance claim. The proofs here are row-set and clause-position
proofs, not timing ones, and no benchmark was run.

No-Observability-Change: this file records tests and mutations only. No metric,
span, log event, queue stage, worker knob, or schema phase is touched by
anything it describes.
