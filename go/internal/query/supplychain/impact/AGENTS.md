# Agent instructions: impact

Read `doc.go` and `README.md` first.

## Invariants

- MUST NOT import root package `query`. Root's
  `supply_chain_impact_alias.go` already imports this package for its
  compatibility aliases, so the reverse import cycles. If a change needs
  something only root exposes, either a leaf equivalent already exists
  (`querycontract`, `querydecode`) or it does not belong in this family;
  ask before adding one.
- The capabilities (`supplyChainImpactFindingsCapability`,
  `supplyChainImpactExplanationCapability`) are registered in ROOT
  (`contract_supply_chain.go`), not here — root owns the router and always
  links into production. This package only declares the read models.
- The copied decode wrappers MUST return `*querydecode.Error` via
  `querydecode.New`, never root's `newQueryDecodeError`. That constructor
  forwards to `querydecode.New` (root's `queryDecodeError` is an alias),
  so the values are identical — but the next family copying this seam
  copies the constructor call too, so keep it on the leaf.
- `supplyChainDefaultSchemaMajorVersion` MUST stay `"1.0.0"` and MUST stay
  a family-local copy (advisory precedent). It mirrors root's
  `queryDefaultSchemaMajorVersion`; if the schema major ever moves, both
  change together — grep for both names.
- `supplyChainImpactFindingMaxLimit` (200),
  `maxSupplyChainRuntimeEnvironmentCandidates`
  (= the finding limit), `serviceCatalogCorrelationFactKind`,
  `cicdRunCorrelationFactKind`, `compactStrings`,
  `firstNonEmptyString`, `appendUniqueString`, `stringMapVal`,
  `derefString`, `supplyChainFactDecodeInput`,
  `supplyChainSchemaEnvelope` and the six source-fact wrappers plus the
  component-evidence seam are family-local copies of trivial root values.
  They MUST stay behavior-identical to their root sources (named in each
  provenance comment). Do not extend them with family-specific semantics;
  add a new helper instead.
- `SupplyChainImpactFindingFilter` MUST carry an anchor: `HasScope`
  gates the store, and the staying root handlers gate before it.
  Widening either gate enables unscoped reads over the whole impact
  corpus.
- The findings SQL MUST keep its bounded placeholder-bound shape: the
  root placeholder-binding test parses
  `supplychain/impact/supply_chain_impact_findings.go` and pins the
  production `QueryContext` argument list against both list-query
  variants. Adding a placeholder without updating the production call
  (or vice versa) fails that test by design.
- Files must stay under 500 lines. `supply_chain_impact_readiness.go`
  (489), `supply_chain_impact_findings.go` (458), and
  `supply_chain_impact_readiness_postgres_query.go` (438) are the ones
  to watch; split by concern (decode, grouping, SQL legs) rather than
  growing them.

## Exported symbols and why each is exported

Every export below names a staying root caller, an external `query.X`
caller, a method call site, or a carried pre-existing export — no
speculative API. Do not export a new symbol without adding its caller
to this list.

- Store constructors `NewPostgresSupplyChainImpactFindingStore` (plus
  the `WithReadModel` variant), `NewPostgresSupplyChainImpactAggregateStore`,
  `NewPostgresSupplyChainImpactReadinessStore`,
  `NewPostgresVulnerabilitySuppressionMutationStore` — `cmd/api` and
  `cmd/mcp-server` wiring via the root aliases, plus the staying root
  store tests. The queryer ports (`SupplyChainImpactFindingQueryer`,
  `SupplyChainImpactAggregateQueryer`,
  `SupplyChainImpactReadinessQueryer`) are the constructor parameters
  those call sites name.
- Store ports and structs (`SupplyChainImpactFindingStore`,
  `SupplyChainImpactAggregateStore`, `SupplyChainImpactExplanationStore`,
  `SupplyChainImpactReadinessStore`,
  `VulnerabilitySuppressionMutationStore`, the four Postgres structs) —
  the `SupplyChainHandler` fields, the staying probes, and `cmd/*`
  wiring via the root aliases.
- Row/filter/result values (`SupplyChainImpactFindingFilter`,
  `SupplyChainImpactFindingRow`, `SupplyChainImpactFindingResult`,
  `SupplyChainImpactEvidenceFact` and its summary,
  `SupplyChainImpactExplanationFilter/Row/Result/Anchors/Freshness`,
  `SupplyChainImpactAggregateFilter/Count`,
  `SupplyChainImpactInventoryRow/Dimension`,
  `SupplyChainImpactPathHop`, `SupplyChainImpactRemediation`,
  `SupplyChainImpactProvenance`, `SupplyChainImpactTargetScope`,
  `SupplyChainImpactReadinessEnvelope/Query/Snapshot/State`) — the
  staying handlers, probes, investigation packets, `serviceintelhttp`,
  `internal/cli`, and the staying root tests.
- Builders (`BuildSupplyChainImpactExplanation` and its no-evidence /
  ambiguous variants, `BuildSupplyChainImpactReadiness` and its
  unavailable variant, `BuildSupplyChainImpactFindingResult`) — the
  staying explain/findings handlers, the investigation packets, and
  the probe tests.
- Request helpers (`RequestedSupplyChainImpactProfile`,
  `FilterProfile`, `RequiredSupplyChainImpactFindingLimit`,
  `ParseSupplyChainImpactIncludeSuppressed`,
  `IsSupportedSupplyChainSuppressionState`,
  `ParseSupplyChainScannerSeverity`, `ImpactFindingsScannerFilters`,
  `ImpactExplanationScannerFilters`,
  `RejectUnsupportedVulnerabilityScannerFilters`,
  `SecurityAlertScannerFilters`, `FirstNonEmptyQueryParam`,
  `TrimSupplyChainImpactExplanationFilter`,
  `SupplyChainImpactExplanationAmbiguousCandidateCount`,
  `FindingReadinessScope`, `SupplyChainImpactPriorityFilter` with
  `ValidSupplyChainImpactPriorityBucket` and
  `OptionalSupplyChainImpactMinPriorityScore`) — the staying
  findings/aggregates/explain handlers, the security-alert handlers,
  the investigation packets, and `supply_chain_sbom_attachments.go`.
- Decode/normalize entry points (`DecodeSupplyChainImpactFindingRow`,
  `DecodeSupplyChainImpactRemediation`,
  `NormalizeSupplyChainImpactSort`, `ReadinessMissingContains`,
  `AddSupplyChainRuntimeContextFact`,
  `RecordSupplyChainRuntimeEnvironmentEvidence`) — the staying root
  unit tests pinning decode and grouping behavior.
- SQL texts and kind/query consts (`ListSupplyChainImpactFindingsQuery`
  and its winners variant, `ExplainSupplyChainImpactFindingQuery` and
  its public-ID variant, the aggregates/readiness/runtime/suppression
  texts, `SupplyChainImpactFindingFactKind`,
  `SupplyChainImpactRuntimeContextFactKinds`,
  `WorkloadIdentityFactKindQuery`,
  `PlatformMaterializationFactKindQuery`,
  `SupplyChainImpactSuppressionReadAt`,
  `SupplyChainImpactAggregateMaxLimit`, the inventory-dimension and
  canonical-key consts) — the staying root SQL-shape tests and the
  lockstep schema test.
- Vocabulary consts (profile precise/comprehensive, readiness states,
  evidence families, missing-evidence reasons, unsupported-target
  kinds, freshness labels, `ServiceCatalogAnchorMissingReason`,
  `ServiceCatalogCorrelationMissingReason`, the environment-evidence
  deploy_event/declared vocabulary, `ReadinessStateReadyWithFindings`
  for compparity, `SupplyChainImpactWinnersReadEnv` and
  `SupplyChainImpactWinnersReadEnabled`) — the staying handlers,
  tests, and external callers that name the exact wire values.
- Relocated runtime-evidence types (`SupplyChainRuntimeContext`,
  `SupplyChainRuntimeContextResult`,
  `SupplyChainRuntimeEnvironmentEvidenceProbe`,
  `SupplyChainRuntimeEnvironmentCandidate`,
  `KubernetesRuntimeWorkloadRef`, `KubernetesRuntimeProbeMetadata`,
  `VulnerabilitySuppressionMutationResult`) — the moved rows that
  name them, via `type X = impact.X` root aliases for the staying
  probes. The declarations moved here because this package cannot
  import root; the behavior is byte-identical.
- Store/query methods (`ListSupplyChainImpactFindings`,
  `CountSupplyChainImpactFindings`, `SupplyChainImpactInventory`,
  `ExplainSupplyChainImpact`, `ReadSupplyChainImpactReadiness`,
  `ListSupplyChainImpactRuntimeContext`,
  `ListSupplyChainImpactRuntimeEnvironmentEvidence`,
  `UpsertVulnerabilitySuppression`,
  `SupplyChainImpactWinnersWatermark`, `HasScope`,
  `HasBoundedScope`, `ReadinessScope`) — called through interfaces
  and receivers, never package-qualified; the staying doubles
  implement them.
- Carried pre-existing exports (row/response struct types,
  `ExplainSupplyChainImpactFindingByPublicIDQuery` companions,
  closed-vocabulary members with no current outside caller such as
  `EvidenceFamilySBOMAttestation`,
  `EvidenceFamilyScannerWorkerAnalysis`,
  `EvidenceFamilyVulnerabilityOSPackage`,
  `MissingEvidencePackageRegistryMetadata`,
  `ListSupplyChainImpactReadinessQueryCore`,
  `SupplyChainImpactSourceStateWindow`,
  `SupplyChainVersionResolutionCorroboration`,
  `SupplyChainProviderAlertAnchor`) — exported before the move and
  kept exported; do not unexport without checking both suites.

## Where the tests live

Seventeen paired unit-test files moved here with the read models
(`*_test.go` beside their subjects): deployment-tier, nuget
readiness, package-metadata freshness, container-identity live,
scan-tier (unit + explain-live), result performance, runtime-context
store, runtime-environment store live, runtime filter (unit), runtime
malformed IDs, source state, version resolution, runtime-repository
SQL, suppression expiry query, and the suppression paths-performance
and plan-proof live tests.

Everything else stays in root package `query` for this lane — do not
"reunite" it here:

- Handler/probe/scope-driving tests (aggregates, explain, findings,
  readiness-handler, scope, probes, scanner contract/filters,
  suppression mutation, remediation, reachability, priority,
  profile): they drive staying root handlers and reach moved symbols
  as `impact.X`.
- Tests sharing doubles or corpus with staying files
  (`explanationFact` and the path asserts in `explain_test`,
  `snapshot` in the kubernetes probe test, `recording*` stores in
  the handler tests, `recordingImpactQueryer` shared by the
  winners-read/freshness pair, `openScopeQueryerTestDB`,
  `explanationFact`-adjacent remediation/operational-anchor/catalog
  tests): splitting them now would fork the helpers. The hub PR3
  moves the handlers and re-homes the suite.
- The live-filter and live-authority cluster (`runtime_filter_live`,
  `runtime_filter_plan_live`, `runtime_filter_args`,
  `runtime_filter_plan_helpers`, `runtime_normalization_live`,
  `suppression_authority_*_live`, `runtime_context_scope_live`,
  `runtime_digest_route_live`, `runtime_repository_precedence_live`):
  bound to the shared live seeders (`seedSupplyChainRuntimeFilterLiveFacts`,
  `insertSupplyChainRuntimeFilterFact`), corpus consts
  (`runtimeFilterLive*`, `suppressionAuthorityLive*`), and the
  scope-live assert helper that all live in staying handler-adjacent
  test files.
- The SQL-shape, decode, and placeholder tests pin moved texts from
  root as `impact.X` (including the placeholder-binding test, which
  parses `supplychain/impact/supply_chain_impact_findings.go` by its
  new path).

## Shared test fixtures

`querytestutil` holds the fixtures both this package's tests and root
need. Put a new shared fixture there rather than copying it. Reuse
its doubles; never redeclare them.

## Common changes

- New impact filter dimension: add the predicate to the list query
  legs AND the winners variant, extend the `FilterProfile`/sort
  normalization, and extend the root placeholder-binding test and the
  SQL-shape tests. All of them, or the dimension silently drifts.
- New readiness evidence family: add the family const, the SQL leg,
  the normalization branch, and the readiness tests on both sides
  (moved unit + staying handler tests).
- New source-fact kind on the explain path: add the typed wrapper in
  `supply_chain_impact_decode_helpers.go`, the accumulator branch in
  `decodeSupplyChainComponentEvidence`, and extend the decode tests.
  If the sdk struct does not declare a field the response reads, the
  read stays raw with a struct-gap comment (advisory precedent).
- Touching a family-local copy: change the root source and the copy
  together and grep for both names — they are duplicated by
  necessity, not by choice.
