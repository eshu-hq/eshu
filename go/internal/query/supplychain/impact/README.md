# Impact query read models

## Purpose

Reads the supply-chain vulnerability-impact surface from active reducer
facts in Postgres: the impact findings list and explanation
(`GET /api/v0/supply-chain/impact/*`, anchored to a CVE, advisory,
package, repository, digest, image, workload, or service), the impact
aggregates (counts and inventory rollups), the evidence-readiness
snapshots (per-family counts and freshness), the read-time
runtime-context and runtime-environment evidence joins, and the
scanner-filter and suppression-mutation adapters. Rows are
reducer-owned impact truth: what is affected, how urgently, and what
evidence is still missing.

## Ownership boundary

This package owns the read models: the findings/aggregates/readiness
store ports, their Postgres implementations, the bounded SQL texts, the
finding/explanation/aggregation builders, the finding-row and
remediation decoders, the family-local typed-decode seam for the SBOM,
package-registry, and service-catalog source-fact kinds, the
readiness families and normalization, the runtime filter SQL, the
suppression split SQL, the version-resolution and source-state models,
and the profile, limit, and vocabulary constants.

It does not own auth, the HTTP handlers, the response envelope,
capability registration, or the runtime probes that attach read-time
evidence to rows — those stay in root package `query` until the hub
PR3 (see below).

Root package `query` keeps the handlers
(`supply_chain_impact_*_handler.go`, `supply_chain_security_alerts.go`,
the investigation-packet files), the capability matrix rows
(`contract_supply_chain.go`), the `SupplyChainHandler` struct, the
probes (`*_probe.go`, `supply_chain_impact_scope.go`), and the
compatibility alias file (`supply_chain_impact_alias.go`) with the
store types, constructors, and read-model types `cmd/api`,
`cmd/mcp-server`, `internal/serviceintelhttp`, and `internal/cli`
still call as `query.NewPostgres*` / `query.SupplyChainImpact*`.
Root performs capability registration deliberately: root owns the
router and always links into the production binary.

## Exported surface

The store ports (`SupplyChainImpactFindingStore`,
`SupplyChainImpactAggregateStore`, `SupplyChainImpactExplanationStore`,
`SupplyChainImpactReadinessStore`,
`VulnerabilitySuppressionMutationStore`) and the Postgres
implementations with their constructors
(`NewPostgresSupplyChainImpactFindingStore` with its
`WithReadModel` variant, `NewPostgresSupplyChainImpactAggregateStore`,
`NewPostgresSupplyChainImpactReadinessStore`,
`NewPostgresVulnerabilitySuppressionMutationStore`) and queryer ports
(`SupplyChainImpactFindingQueryer`,
`SupplyChainImpactAggregateQueryer`,
`SupplyChainImpactReadinessQueryer`), the values crossing those ports
(filters, rows, results, envelopes, path hops, remediation,
provenance), the builder entry points
(`BuildSupplyChainImpactExplanation` and its no-evidence/ambiguous
variants, `BuildSupplyChainImpactReadiness` and its unavailable
variant, `BuildSupplyChainImpactFindingResult`), the SQL texts
(`ListSupplyChainImpactFindingsQuery` and its winners variant,
`ExplainSupplyChainImpactFindingQuery`, the aggregates/readiness/
runtime/suppression texts), the request helpers the staying handlers
call (`RequestedSupplyChainImpactProfile`, `FilterProfile`,
`RequiredSupplyChainImpactFindingLimit`,
`ParseSupplyChainImpactIncludeSuppressed`,
`IsSupportedSupplyChainSuppressionState`,
`ParseSupplyChainScannerSeverity`, the scanner-filter sets and
`RejectUnsupportedVulnerabilityScannerFilters`,
`FirstNonEmptyQueryParam`,
`TrimSupplyChainImpactExplanationFilter`,
`SupplyChainImpactExplanationAmbiguousCandidateCount`,
`FindingReadinessScope`, `SupplyChainImpactPriorityFilter` with its
bucket/score helpers, `HasScope`/`HasBoundedScope`/`ReadinessScope`),
the decode entry points (`DecodeSupplyChainImpactFindingRow`,
`DecodeSupplyChainImpactRemediation`,
`NormalizeSupplyChainImpactSort`, `ReadinessMissingContains`,
`AddSupplyChainRuntimeContextFact`,
`RecordSupplyChainRuntimeEnvironmentEvidence`), the profile, limit,
fact-kind, vocabulary, readiness-state, evidence-family, and
missing-evidence constants, and the relocated runtime-evidence types
(`SupplyChainRuntimeContext`, `SupplyChainRuntimeContextResult`,
`SupplyChainRuntimeEnvironmentEvidenceProbe`,
`SupplyChainRuntimeEnvironmentCandidate`,
`KubernetesRuntimeWorkloadRef`, `KubernetesRuntimeProbeMetadata`,
`VulnerabilitySuppressionMutationResult`).

Every export names a staying root caller, an external `query.X`
caller, or a carried pre-existing export; see `AGENTS.md` for the
per-symbol list. See `doc.go` for the godoc-rendered contract.

## Dependencies

Internal packages, all of them leaves that never import root package `query`:

- `internal/query/querycontract` — `StringVal`, `BoolVal`, `IntVal`,
  `FloatVal`, `StringSliceVal` row-value decoders plus `QueryParam`
  and `WriteError` (root forwards to the same functions, so behavior
  is identical on both sides of the move).
- `internal/query/querydecode` — the classified decode failure the
  copied source-fact wrappers return via `querydecode.New`, the same
  constructor root's `newQueryDecodeError` forwards to (advisory and
  packagereg precedent).
- `internal/storage/postgres/pgarray` — the array scan/build surface
  the stores read through.
- `internal/storage/postgres` — the suppression-mutation storage
  adapter.
- `internal/truth` — truth-vocabulary helpers the result model reads.

Plus `sdk/go/factschema` (typed SBOM, package-registry,
service-catalog, and vulnerability-suppression decode seams) and the
standard library.

The `compactStrings`/`firstNonEmptyString`/`appendUniqueString`/
`stringMapVal` payload helpers, the `derefString` nil-safe deref, the
`supplyChainFactDecodeInput` bundle, the `supplyChainSchemaEnvelope`
adapter (with its `supplyChainDefaultSchemaMajorVersion` literal),
the six source-fact decode wrappers, the
`decodeSupplyChainComponentEvidence` seam, the
`serviceCatalogCorrelationFactKind`/`cicdRunCorrelationFactKind`
literals, and the `supplyChainImpactFindingMaxLimit`/
`maxSupplyChainRuntimeEnvironmentCandidates` bounds are family-local
copies of trivial root values that cannot cross the package boundary;
each carries a provenance comment naming its root source.

## Telemetry

This package emits no metrics, spans, or logs of its own. The impact
routes are traced in the staying root handlers
(`telemetry.SpanQuerySupplyChainImpactFindings`,
`telemetry.SpanQuerySupplyChainImpactExplanation`,
`telemetry.SpanQuerySupplyChainImpactAggregate`, and the
`eshu.query.freshness_state` / `eshu.query.runtime_context_*` span
attributes).

## Move evidence (#6060)

This package was created by moving thirty-five files out of root
package `query` (`git mv`, no logic changes), plus a
runtime-evidence types file for the five read-model declarations the
moved rows name, and seventeen paired unit-test files that test only
moved subjects. The two assertions below are structural rather than
promissory — each names what a reader can check.

No-Regression Evidence: the move is a package relocation, not a
rewrite. `git diff -M --find-renames` pairs each file with its root
predecessor; the only statement-level changes are the `package`
clause, the `impact.` qualification at staying root call sites, the
`querycontract.` qualification of the value decoders and HTTP helpers
(root forwards to the identical functions), the export renames listed
in `AGENTS.md`, the relocated read-model type declarations (root
keeps `type X = impact.X` aliases in
`supply_chain_impact_alias.go`), and the family-local copies of the
decode seam, deref/map helpers, fact-kind literals, and limit bounds,
each documented with its root source and verified
behavior-preserving by the re-qualified root test suite
(`go test ./internal/query/...`) and the moved package suite
(`go test ./internal/query/supplychain/impact`), which pin the
filtering, grouping, paging, normalization, SQL shape, placeholder
binding, and dead-letter behavior. The source-path-asserting
placeholder test was repointed at the new file location and still
parses the production list function.

No-Observability-Change: there is no observability to change — this
package emits none, and the handler/probe spans, attribute names, and
capability strings keep their exact values (the relocated profile
constants and the copied error constructor produce identical values;
only their package qualifier changed).

## Gotchas / invariants

- Do not import root package `query`. Root's
  `supply_chain_impact_alias.go` already imports this package, so the
  reverse import cycles.
- The capability is registered in ROOT (`contract_supply_chain.go`),
  not here — root owns the router and always links into production.
- `SupplyChainImpactFindingFilter` must carry an anchor (`HasScope`);
  the store rejects anchorless reads before running SQL, and the
  handlers reject them before reaching the store. The page limit is
  bounded by `supplyChainImpactFindingMaxLimit` (family-local copy —
  keep byte-identical to root's `supply_chain.go` value).
- A dropped decode contribution is a dead-lettered malformed fact
  (`input_invalid`), not missing data. The typed wrappers drop
  rather than zero-fill; reducer-derived kinds with no sdk struct yet
  stay on the raw path per the #4784 ADR note in
  `supply_chain_impact_decode_helpers.go`.
- `ReadinessState*`, `EvidenceFamily*`, `MissingEvidence*`,
  `UnsupportedTargetKind*`, and `FreshnessLabel*` are closed
  vocabularies pinned by readiness tests on both sides of the move;
  adding a member means updating the normalization, the SQL legs, and
  both suites.
- The winners-read cutover (`ReadFromWinners`,
  `SupplyChainImpactWinnersReadEnv`) keeps output byte-identical
  between the legacy dedup and the maintained read model; the
  placeholder-binding test pins the production argument list.
- The handler-driving tests that share helpers, fakes, or corpus with
  staying files remain in root package `query` (see `AGENTS.md`): they
  drive the hub handlers through `Mount` there. Do not "reunite" them
  here; the probe, scope, and live suites that need no root helpers
  moved with the handlers to the hub package
  (`internal/query/supplychain`).

## Related docs

- [HTTP API Reference](../../../../../docs/public/reference/http-api.md)
- [Telemetry](../../../../../docs/public/reference/telemetry/index.md)
