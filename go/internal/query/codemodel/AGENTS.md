# AGENTS.md — `go/internal/query/codemodel`

Scope: the code-model query leaf, split out of root package `query`
(#6060 lane A L1). Agents working here MUST read the package
[README](README.md) and [doc.go](doc.go) first.

## Ownership

- This leaf owns read-model builders, row decoders, response shapers,
  and Cypher/postgres constructors for the code family. It MUST NOT
  register routes or capabilities, widen scopes, or perform unbounded
  reads.
- Root `family_code_shim.go` owns the compatibility aliases/forwarders
  the staying family still uses. When the next lane moves the staying
  handler, delete the corresponding shim entry — never extend the shim
  for new callers.
- `querycontract` owns envelopes, capabilities, access filter, and row
  decoders; `queryauth` owns request auth bounds; `queryspan` owns span
  plumbing. This leaf MUST NOT re-declare those — qualify to them.

## Import discipline

Allowed imports: stdlib, `querycontract`/`queryauth`/`queryspan`, and
the already-present internal leaves (`searchbench`, `searchdocs`,
`searchembed`, `searchhybrid`, `searchretrieval`, `facts`,
`codeprovenance`). NEVER import root package `query` (cycle).
`gofmt -l` MUST be clean; keep the stdlib/eshu import grouping.

## Export discipline (export-minimal)

Every export below exists because a staying root caller (handler,
reader, executor, test, or the content-hybrid lane) names it through
the shim, or because Go forces methods to live with a moved type. Do
NOT export anything else; do NOT unexport anything on this list
without moving its staying callers first.

- Request/row/filter contracts (moved from staying method files with
  their value-receiver methods; `*CodeHandler` route methods stay in
  root): `CallGraphMetricsRequest` (+`Validate`,
  `EffectiveMetricType`), `ImportDependencyRequest` (+`Validate`,
  `EffectiveQueryType`, `NormalizedLanguage`, `QueryLimit`, `Access`
  with `json:"-"`), `RelationshipsRequest`,
  `RelationshipStoryRequest` (+`Validate`, `NormalizedQueryType`,
  `IsRepoScopedOverrideStory`, `EffectiveTarget`, `NormalizedLimit`,
  `NormalizedDirection`, `NormalizedRelationshipType(s)`,
  `NormalizedTokenBudget`, `GraphAnchorProperty{,Resolved}` with
  `json:"-"`), `RelationshipStoryResolution`,
  `RelationshipStoryEvidenceInputs`/`RelationshipStoryEvidenceState`
  (+ reason/truncation vocabularies), `CodeFlow{Kind,Filter,ReadModel,
  Function,TaintPath}` (+ page-limit bounds and kind values),
  `DeadCodePolicyStats`, `DeadCodeGoPolicyContext` (+ its fields),
  `DeadCodeDowngradedRoots` (+`IsDowngraded`),
  `ComplexityAmbiguousError`.
- Builder/shaper/validator surface called from staying code or tests:
  the `CallGraphMetrics*`, `Direct/Package/SourceModule/TargetModule/
  FileImportCycle/CrossModuleCall*Import*Cypher`,
  `BuildFileImportCycleRows`, `FilterCrossModuleCallRows`,
  `FilterImportDependencyScopeRows`, `UniquePackageImportRows`,
  `StripImportDependencyInternalPaths`,
  `ImportDependencyScanBoundError`, `PageImportDependencyRows`,
  `ImportDependencyResponse`, `ImportDependencyUniqueModules`,
  `ResolveRelationshipsNameTarget`,
  `AmbiguousRelationshipsResponse`,
  `SortRelationshipStoryCandidates`,
  `RelationshipStoryCandidateMaps`, `RelationshipGraphRowCypher`,
  `BuildTransitiveRelationship*`, `NormalizeGraphRelationships`,
  `GraphEntityIDPredicate`, `RelationshipStoryProvenance`,
  `RelationshipStoryRowsAboveConfidenceFloor`,
  `ClassifyRelationshipStoryEvidence`,
  `BuildDeadCodeAnalysis{,ForLanguage}`, `ClassifyDeadCodeResults`,
  `DeadCodeResultClassification`, `DeadCodeResultHasHiddenConsumer`,
  `DeadCodeWeakIncomingAmbiguityReason`,
  `DeadCodeLanguage{Supported,MaturityReport,
  ExactnessBlockerReport}`, `DeadCodeIs*` (all language roots,
  `GeneratedCode`, `TestFile`), `NewDeadCodeGoPolicyContext`,
  `FilterResultsByDecoratorExclusions`,
  `ResultMatchesDecoratorExclusion`, `NormalizeDecoratorName`,
  `BuildSearchGraphEntitiesQuery`, `CodeSearchPagePayload`,
  `CodeSearchProbeLimit`, `ComplexityCandidateMaps`,
  `NormalizeComplexityListLimit`, `TrimComplexityResults`,
  `WriteComplexityAmbiguousError`, `ValidateReadOnlyCypher`,
  `NewCodeHybridRanker`, `CodeHybridRanker` (+`LocalEmbedder`),
  `CodeResultReranker`, `EntityIDFromDocument`,
  `CodeFlowFunctionFromPayload`, `CallGraphMetricIdentity`.
- Leaf-owned constants/variables staying code binds through:
  `CallGraphMetricsMaxOffset`, `CodeFlowDefaultLimit`,
  `CodeFlowMaxLimit`, `CodeFlowKindFactKinds`,
  `ListActiveCodeFlowFactsSQL`, `ComplexityNameCandidateLimit`,
  `ReadOnlyCypherCapability`,
  `VisualizationGraphQueryCapability`,
  `DeadCodeClassification{Ambiguous,Excluded,Unused}`,
  `DeadCodeHiddenConsumer{ResultKey,Reason}`,
  `DeadCodeLanguageMaturity`,
  `Elixir/PHPDeadCodeMetadataRootKinds`,
  `RubyRailsControllerActionRootKind`,
  `ErrImportDependencyScopeTooBroad`.

## No-Cypher-text rule

The queryplan-pinned builders relocate only. If you touch one, run
`go test ./internal/queryplan/` and the in-package binding tests; a
`source_sha256` change means your edit altered declaration bytes —
revert unless the rename was forced, and never let `cypher_sha256`
change. `file:` paths in
`go/internal/queryplan/testdata/{handler-hot-cypher,hot-cypher}.yaml`
MUST track the builder's real location.

## Family-local copies (keep byte-identical, change in pairs)

`graphSemanticMetadataProjection`, `cloneQueryAnyMap`,
`deadCodeEntityLanguage`, `deadCodeEntityPath`,
`normalizeDeadCodeLanguage`, `primaryEntityLabel`,
`isCypherIdentifierChar`, `cypherMaxQueryLength` (moved, not copied),
`normalizeCodeFlowLanguage`, `floatVal`, `firstNonEmpty`,
`mapRelationships`, `filterNullRelationships`,
`dropNilOrEmptyRowKey`, the exact-entity resolution chain
(`resolveExactGraphEntityCandidates`,
`selectExactGraphEntityCandidate`, `exactEntityNameMatches`,
`nonTestEntityMatches`, `isTestEntityPath`,
`formatAmbiguousEntityMatches`, `graphEntityResolutionLimit`), the
`deadCodeWeakIncoming*` keys. Each carries a provenance comment naming
its root source.

## Verification (paste all)

```bash
cd go && go build ./internal/query/...
cd go && go vet ./internal/query/ ./internal/query/codemodel/
cd go && go test ./internal/query/ ./internal/query/codemodel/ -count=1
cd go && go test ./internal/queryplan/ -count=1
bash scripts/verify-route-coverage.sh
bash scripts/verify-dirgate.sh --digest internal/query
git diff --check
gofmt -l <touched files, repo-relative>
```
