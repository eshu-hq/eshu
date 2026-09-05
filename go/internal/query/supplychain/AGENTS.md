# Agent instructions: supplychain hub

Read `doc.go` and `README.md` first.

## Invariants

- MUST NOT import root package `query`. Root's
  `supply_chain_hub_alias.go` already imports this package for its
  compatibility aliases, so the reverse import cycles. If a change needs
  something only root exposes, either a leaf equivalent already exists
  (`querycontract`, `queryauth`, `queryselector`, `queryspan`) or it does
  not belong in this family; ask before adding one.
- Capabilities are registered in ROOT (`contract_supply_chain.go`), not
  here — root owns the router and always links into production. This
  package only declares the constant values.
- `queryHandlerTracer` MUST stay a package-local var seeded from
  `queryspan.HandlerTracer`. The moved probe tests swap it; a second
  tracer var, or seeding from anywhere else, breaks their isolation or
  changes emitted spans.
- `attachCollectorListReadiness` / `collectorListReadiness` are
  family-local copies of root's helpers (root's
  `collector_list_readiness.go` prescribes this split). They MUST stay
  behavior-identical to their root sources. Do not extend them with
  family-specific semantics; add a new helper instead.
- `supplyChainGraphConfigured` is a family-local copy of root's
  `languageQueryGraphConfigured` (#5761 F1). Same rule: mirror, don't
  extend.
- `UniqueSortedNonEmpty` and `SecurityAlertRepositoryScopeIDs` are shared
  with staying root files through root's forwards. Changing their output
  contract changes cicd evidence, sbom stores, and security-alert stores
  too — treat them as shared, not family-local.
- Every list route MUST keep its scope gate: an anchorless read over a
  whole fact corpus is rejected before any store runs. Widening a gate
  enables unbounded reads.
- The probes MUST keep eligibility inside the bound
  (docs/internal/evidence/5789-per-digest-bound.md): bounding candidates
  first and authorizing after returns wrong answers on crowded pages.
- The packet route MUST compose through `SupplyChainImpactPacketResponder`
  and MUST NOT name lane-B packet types. If lane-B moves the envelope to
  a leaf, collapse this seam to direct calls and delete the responder.
- Files must stay under 500 lines. Watch
  `supply_chain_impact_kubernetes_runtime_probe.go` and the aggregate
  handlers; split by concern rather than growing them.

## Exported symbols and why each is exported

Every export below names a staying root caller — no speculative API. Do
not export a new symbol without adding its caller to this list.

- `SupplyChainHandler` — root `handler.go` field, `cmd/api` and
  `cmd/mcp-server` wiring, staying root tests (via the root alias).
- `ContainerImageIdentityResult`, `ContainerImageIdentitySourceBridge` —
  the hub list handler and the staying source-bridge test (via the root
  alias and the `BuildContainerImageIdentitySourceBridge` forward).
- `SBOMAttestationAttachmentResult` — the hub list handler and result
  builder, plus staying SBOM tests (via the root alias).
- `VulnerabilitySuppressionMutationStore`,
  `VulnerabilitySuppressionMutationRequest`,
  `VulnerabilitySuppressionMutationResponse` — `cmd/api` wiring and
  staying root tests (via the root alias).
- `KubernetesRuntimeCandidate`, `KubernetesRuntimeWorkloadMatch` —
  staying `kubernetes_runtime_workload_store.go` and the MCP dispatch
  test (via the root alias).
- `KubernetesWorkloadCurrentInventoryFilter` — staying
  `kubernetes_runtime_workload_store.go` assertion and `cmd/*` wiring
  (via the root alias).
- Container-image / SBOM / security-alert store ports and their
  filter/row/page/count values — the staying Postgres implementations,
  `entity.go`, the incident-context stores, and `cmd/*` wiring (via the
  root aliases). The three filter `HasScope` methods are exported because
  the staying implementations call them across the boundary (advisory
  precedent: `AdvisoryEvidenceFilter.HasScope`).
- `CloudResourceCurrentInventoryFilter`,
  `CloudResourceRuntimeDigestResolver`, `CloudResourceRuntimeDigestMatch` —
  staying `cloud_resource_list_store.go` and its tests (via the root
  alias).
- Capability constants (six list + four aggregate) — staying
  `contract_supply_chain.go` registration and staying handler tests (via
  root const-forwards). The suppression capability is hub-internal only
  (no matrix row) and stays unexported.
- `LightweightExactSupport`, `AuthoritativeExactSupport` — the support
  constructors root's `contract_supply_chain.go` init and this package's
  `TestMain` both call, so production and hub tests gate on one
  declaration (semanticsearch precedent).
- Limit constants (`...MaxLimit`, probe budgets, Cypher text) — the
  staying stores and staying tests that bound through them (via root
  const-forwards).
- `UniqueSortedNonEmpty` — staying `ci_cd_evidence_summary.go` and
  `sbom_attestation_attachments.go` (via root forward).
- `SecurityAlertRepositoryScopeIDs` — staying
  `security_alert_reconciliation*.go` (via root forward).
- `SupplyChainCloudRuntimeProbePerDigestLimit` — staying
  `cloud_resource_list_store.go` (via root forward).
- `BoundedSBOMWarningSummaries`,
  `SBOMAttestationWarningSummaryPreviewMaxCount` — staying
  `sbom_attestation_attachment_rows.go` decode wrappers (via root
  forwards).
- `SecurityAlertReconciliationAnchorRequiredMessage` — the staying
  security-alert store error (via root forward).
- Aggregate pagination offsets (`Next*AggregateOffset`) and
  `SBOMAttestationAttachmentAggregateScope` — the staying aggregate
  tests, which pin them directly (via root forwards).
- `PlanSupplyChainRuntimeEnvironmentCandidates`,
  `SupplyChainRuntimeEnvironmentPlan`,
  `MaxSupplyChainRuntimeEnvironmentCandidates` — the staying
  runtime-context tests (via root forwards).
- `SupplyChainKubernetesRuntimeEvidenceSource`,
  `SupplyChainKubernetesRuntimeResolutionMode` — staying
  `queryplan_profile_params_test.go` (via root forwards).
- `BuildContainerImageIdentitySourceBridge` — the staying source-bridge
  test (via root forward).
- `SupplyChainImpactPacketResponder` — root's lane-B packet responder
  implementation and `cmd/*` wiring, which inject it.

## Where the tests live

These suites moved with the handlers into this package because every
reference they make resolves here (hub symbols, leaf packages, stdlib,
or suite-local doubles):

- the probe suites: cloud probe, Kubernetes probe + fair + bench
  (apply side) + perf live, findings freshness + winners-read;
- the runtime-context suites: context probe, digest bound, environment
  evidence (with two minimal local doubles), the runtime-filter live
  cluster (filter, plan, normalization, precedence, scope, args,
  plan-helpers) and the suppression-authority live cluster;
- `main_test.go` (`TestMain`), which registers the ten hub capabilities
  so these tests gate exactly like production.

These stay in root package `query` and reach the hub through the alias:

- the cross-cutting sweeps (`auth_scoped_routes_*`,
  `graph_read_error_*`) — they sweep every family and never move (the
  supply-chain selector sweep drives the hub through `Mount`, since
  direct unexported-method calls cannot cross the package boundary);
- the packet parity tests (`investigation_packet_api_test.go`, the
  cloud-runtime parity test) — they pin the lane-B builder output
  against the route and share lane-B helpers and fakes;
- handler-driving tests that share root helpers or fakes (advisory
  catalog/evidence, findings/explain/aggregates/scopes, suppression,
  scanner contract, vulnerability detail, SBOM/container/security
  lists) — moving any one forks a fake a staying test needs;
- staying-store and budget tests (`cloud_resource_list_store_*`,
  `cloud_resource_runtime_digest_*`,
  `kubernetes_runtime_workload_store_*` + its SQL bench,
  `container_image_identities_source_bridge_test.go`,
  `queryplan_*`, `supply_chain_impact_runtime_digest_route_live_test.go`)
  — they reach hub symbols through root's forwards.

Do not "reunite" a staying test here until every one of its references
resolves here: shared fakes (`recording*`, `snapshot`-style doubles) and
direct unexported-method calls are the usual blockers, and forking a
double to force a move is worse than leaving the test in root.

## Shared test fixtures

`querytestutil` holds the fixtures both this package's tests and root
need. Put a new shared fixture there rather than copying it. Reuse its
doubles; never redeclare them. Small handler fakes needed on both sides
stay duplicated per side with a comment naming the twin — do not widen
either fake's contract to reunite them.

Deliberate twins (each names its authoritative copy; keep them
behavior-identical): the cloud and Kubernetes probe doubles and the
runtime-context finding store twinned into root's parity test; the
`osPackageFindingRowForRuntimeContext` twin in root's evidence test;
the `evidenceExplanationStore`/`evidenceReadinessStore` and
`findingsOnlyStore` locals here. Opaque cross-lane fixture strings
(`cloudRuntimeProbeTestCICDFactKind`,
`runtimeFilterLiveServiceCatalogFactKind`) mirror lane-owned kind
constants with an `rg both names` comment — the hub never reads those
values, so a rename only touches fixtures, but update both sides
together.

Cross-cutting live tests stay in root. The `integration`-tagged suites
that wire hub planner internals to the production Postgres store and a
real graph reader (`kubernetes_runtime_workload_store_fairness_live_test.go`,
the k8s probe performance pair) live in root, not here: this package
cannot name root production types, and root cannot name hub
unexported planner symbols. They reach the planner through the
`integration`-only seam in
`supply_chain_impact_kubernetes_runtime_probe_fair_live.go` (type
aliases, planner forwards, fanout accessors, one handler-method
forward) — compiled out of the default build, so default lint never
sees it. Do not add unconditional hub exports for live tests, and do
not import the parent query package from hub test files (import cycle):
extend the seam instead.

## Common changes

- New supply-chain route: add the method, register it in `Mount`, add
  the capability constant here and the matrix row in root
  `contract_supply_chain.go` (via the `*Support()` constructors, never
  an inline struct), register the capability in `main_test.go`'s
  `TestMain`, and extend the root alias file. All five, or the route is
  unreachable, unregistered, unwired, or 501 in this package's tests.
- New store port method: extend the port here, the staying Postgres
  implementation in root, and the fakes on both sides. The root
  compile-time assertion in `supply_chain_hub_alias.go` pins the
  implementation to the port.
- Probe budget change: the budget constants are pinned by staying live
  tests through root's forwards — update both sides' expectations and
  cite the measurement.
- Cypher or fair-planner change: `handler-hot-cypher.yaml` and
  `query-source-coverage.yaml` pin the exact declaration bytes
  (`source_sha256`). Recompute with the AST replicator (same algorithm
  as `queryplan.manifestSymbolSource`), update file/symbol paths, and
  re-run `go test ./internal/queryplan/` — the binding test fails red
  otherwise.
- Capability support change: edit the `*Support()` constructor in
  `capabilities.go` once; root's init and `TestMain` both follow. Never
  copy the row fields into either caller.
