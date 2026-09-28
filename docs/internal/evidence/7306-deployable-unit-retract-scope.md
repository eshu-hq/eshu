# #7306: deployable_unit_correlation retract scope audit

## Question

#7304 fixed a `workload_materialization` retract that built its keep-list from
the entity-filtered projection while its repository set covered the whole
scope. A foreign-keyed intent then deleted edges a sibling intent had written.
#7306 asks whether `DeployableUnitCorrelationHandler`'s zero-result retract,
which builds its rows from the intent's entity keys, has either failure:

- (a) a foreign-keyed or unmatched-key intent retracts deployable-unit edges a
  sibling intent in the same scope and generation legitimately wrote;
- (b) a stale deployable-unit edge is never retracted because no intent carries
  the matching key.

## Verdict

- **(a): no defect.** The retract does not share #7304's shape, and no producer
  sends a foreign key to this domain.
- **(b): no retract-shape defect.** The only reachable gap is a collector
  key-contract mismatch in dependency mode. There the handler neither writes
  nor retracts for the repository. That was tracked as #7316 and is closed there,
  not a defect in this retract.

No production code changed. This change adds pinning tests and a doc comment
on `deployableUnitRetractRowsFromFacts`.

## Evidence

Paths are relative to `go/` at base `origin/main` `7e844df0be`; citations name the function or constant.

### Why the retract is order-independent

- The zero-result path retracts the rows from `deployableUnitRetractRowsFromFacts`
  (`DeployableUnitCorrelationHandler.Handle`, `len(evaluation.Results) == 0` branch). There is one row
  for each repository fact whose `graph_id`/`repo_id` or `name` matches a key,
  directly or through the last-colon-segment alias
  (`deployableUnitIntentMatchesRepository` in
  `internal/reducer/deployable_unit_correlation_edges.go`).
- The non-zero path retracts the evaluated candidates' repositories, then
  writes their admitted rows (`materializeDeployableUnitEdges`, `retractDeployableUnitEdges`).
- The retract statement deletes every `CORRELATES_DEPLOYABLE_UNIT` edge from the
  source repository for the evidence source
  (`RetractDeployableUnitCorrelationEdgesCypher` in
  `internal/storage/cypher/canonical_deployable_unit_edges.go`). It is a
  whole-repository delete, not a keep-list diff.
- The candidate filter (`filterDeployableUnitCandidates`,
  `candidateIdentityKeys`) matches the
  same identities from the same repository fact
  (pass 1 of `ExtractWorkloadCandidates` in `internal/reducer/candidate_loader.go`).
- So for each repository, an intent does one of two things. If its keys miss,
  it issues no statement for that repository. If they match, it retracts and
  rewrites the repository's whole truth. Two matching intents do identical
  work, so the final graph does not depend on the order intents run in.

### Who sends intents to this domain

- **The only production producer** is the git collector's shared follow-up. Its
  key is `repo:` + `filepath.Base(repoPath)`
  (`deployableUnitCorrelationFactEnvelope` in
  `internal/collector/repo/git/followup_facts.go`). It is emitted for
  every repository on every full generation from `fact_builder.go`, after the
  `snapshot.Delta` early return.
- **Ingestion reopen** replays existing succeeded rows, which carry the same key
  (`crossScopeCorrelationReopenDomains` in
  `internal/storage/postgres/ingestion_reopen_correlation.go`).
- **The foreign-key producers named in the issue** only reach
  `workload_materialization`. `repoDependencyReplayEntityKey` and the
  deployment_mapping replay (`workloadMaterializationReplayScopes` in
  `internal/reducer/platformfam/platform_materialization.go`)
  both call `ReplayWorkloadMaterialization`, and that function hard-codes
  `Domain: DomainWorkloadMaterialization`
  (`ReducerQueue.ReplayWorkloadMaterialization` in
  `internal/storage/postgres/reducer_queue_replay.go`).
- **Each git scope holds one repository**: `git-repository-scope:<repo.ID>`
  (the scope builder in `internal/collector/repo/git/source_processing.go`). Ref scopes share
  `repo.ID`, but the projector emits no reducer intents for
  `KindRepositoryRef` (`Runtime.Project` and `buildProjection` in
  `internal/projector/runtime/projection.go`).

### The remaining (b) gap: dependency-mode name mismatch (closed by #7316)

- The repository fact's `name` is `SelectedRepository.DisplayName` when set,
  else the path basename (`repositoryName` in `source_processing.go`).
- `DisplayName` is set only from `ESHU_BOOTSTRAP_PACKAGE_NAME` in dependency mode
  (`DependencyName` in `selection_config.go`, copied into
  `SelectedRepository.DisplayName` in `selection_native.go`), and it
  applies to every repository in that run.
- Before #7316 the collector built every follow-up key from the path basename.
  When the name differed, the key matched nothing, so the handler never wrote
  and never retracted for that repository.
- #7316 closes it: every git-collector follow-up key is now
  `<prefix>:<repository fact name>` through one helper (`followupEntityKey` in
  `internal/collector/repo/git/followup_facts.go`). See
  `docs/internal/evidence/7316-followup-key-repository-name.md` for the proof,
  the full-generation heal rule for existing dependency-mode repositories, and
  the one open gap (a name ending in a colon).

### Delta generations (existing policy, not key-related)

Delta generations emit no `deployable_unit_correlation` intent. A deployment
evidence change that lands in a delta is picked up at the next full
generation. `workload_materialization` has the same policy.

## Tests

- `internal/reducer/deployable_unit_correlation_retract_scope_test.go` (hermetic,
  real `Handle` path, stateful in-memory graph with the production retract and
  MERGE semantics). It uses #7304's key table: collector `repo:<name>`,
  `repo:<graph id>`, foreign `repo:<other graph id>`, `repo:<scope id>`, and
  no keys.
  - `TestDeployableUnitRetractScopeFinalGraphIsIntentOrderIndependent` runs the
    matching intent and each keyed intent in both orders. Every order ends at
    `[repo->deploy]`, with the stale edge retracted.
  - `TestDeployableUnitRetractScopeKeyedIntentAloneTouchesOnlyMatchedRepository`
    runs each intent alone, with and without deployment evidence. A matching
    key rewrites or retracts. A non-matching key issues 0 graph statements.
  - `TestDeployableUnitRetractScopeNoKeysFailsBeforeAnyGraphStatement` checks
    that a keyless intent is rejected before any statement.
  - `TestDeployableUnitRetractMatcherAgreesWithCandidateFilter` checks that the
    retract matcher and the candidate filter select the same repositories over
    13 key shapes and 3 repository identities.
- `internal/reducer/deployable_unit_retract_scope_live_test.go`
  (`live_nornicdb_answer_truth`, real handler and real `EdgeWriter`):
  - `TestLiveDeployableUnitRetractScopeIsIntentOrderIndependent` runs a
    matching intent and a foreign-keyed intent in both orders, plus 10 racing
    trials. Each ends with only the admitted edge.
  - In generation 2, the foreign-keyed intent changes nothing and the matching
    intent retracts the edge whose evidence disappeared.

### Seeded mutations (RED), then restored (GREEN)

| Mutation | Result |
| --- | --- |
| M1: zero-result retract ignores keys and covers every repository fact (the #7304 class) | Hermetic RED: `...FinalGraphIsIntentOrderIndependent/foreign_repo:<other_graph_id>/matching_then_keyed`, `...KeyedIntentAlone.../foreign...` (both), and the existing `TestDeployableUnitRetractRowsStayWithinIntentRepository`. Live RED on Neo4j: `matching then foreign: final correlation targets [], want [...-deploy]` |
| M2: the retract matcher drops its alias branch (matcher drift) | Hermetic RED: `TestDeployableUnitRetractMatcherAgreesWithCandidateFilter`, `...KeyedIntentAlone.../matching_repo:<graph_id>/deployable=false`, `.../repo:<scope_id>/deployable=false` |

On the unmutated tree, all tests pass. The live test passes on
neo4j:2026-community in 1.11 s warm and 19.16 s with a cold schema apply.
NornicDB was not run, by the owner's rule; the live-backend CI job runs this
test on both backends.

No-Observability-Change: this change adds tests and a comment only. No runtime
path, metric, span or log changed.
No-Regression Evidence: no production statement or code path changed. The
existing `deployable_unit_correlation` suites stay green.
