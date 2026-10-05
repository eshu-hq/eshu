# Agent instructions: supplychain hub

Read `doc.go` and `README.md` first.

## Invariants

- MUST NOT import root package `query`. Root's
  `compat_supply_chain.go` already imports this package for its
  compatibility aliases, so the reverse import cycles. If a change needs
  something only root exposes, either a leaf equivalent already exists
  (`querycontract`, `auth`, `selector`, `tracing`) or it does
  not belong in this family; ask before adding one.
- Capabilities are registered in ROOT (`contract_supply_chain.go`), not
  here — root owns the router and always links into production. This
  package only declares the constant values.
- `queryHandlerTracer` MUST stay a package-local var seeded from
  `tracing.HandlerTracer`. The moved probe tests swap it; a second
  tracer var, or seeding from anywhere else, breaks their isolation or
  changes emitted spans.
- `attachCollectorListReadiness` / `collectorListReadiness` are
  family-local copies of root's helpers (root's
  `collector_list_readiness.go` prescribes this split). They MUST stay
  behavior-identical to their root sources. Do not extend them with
  family-specific semantics; add a new helper instead.
- Graph-configured checks go through `querycontract.GraphConfigured`
  (single home since #6542 review retired the family-local copy). Do not
  reintroduce a local predicate.
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
- The packet route MUST compose through `ImpactPacketResponder`
  and MUST NOT name lane-B packet types. If lane-B moves the envelope to
  a leaf, collapse this seam to direct calls and delete the responder.
- Every handler-owned 500 store-read branch on every sibling route MUST call
  `querycontract.WriteGraphReadError` first and return before `failStage`
  when it reports true (#7548 for the findings route, #7549 for all nineteen
  sibling branches): a stale or timed-out guarded PostgreSQL reader
  is a retryable 503 with `Retry-After`, not a 500, and the mapped verdict is
  deliberately not logged as `stage_failed`. The readiness read is the
  exception: its error serves a `readiness_unavailable` envelope. The
  security-alert repository selector's reads (`repository_selector.go`,
  #7567) follow the same order as stages `repository_catalog_match` and
  `provider_repository_scope_lookup`, reporting against the calling route's
  span and operation through `securityAlertSelectorRoute`. Keep the
  capability a plain string parameter: root's
  `TestWriteGraphReadErrorCapabilitiesExistInMatrix` resolves a parameter
  through its callers but not a struct field. Their `repo_id`
  MUST NOT be the raw selector (unbounded caller input): the catalog match
  logs `""`, the scope lookup the resolved canonical id.
- Files must stay under 500 lines. Watch
  `kubernetes_runtime_probe.go` and the aggregate
  handlers; split by concern rather than growing them.

## Exported symbols and why each is exported

Every export below names a staying root caller — no speculative API. Do
not export a new symbol without adding its caller to this list.

- `Handler` — root `handler.go` field, `cmd/api` and
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
  `internal/query/kubernetes/runtime_workload_store.go` (direct import)
  and the MCP dispatch test (via the root alias).
- `KubernetesWorkloadCurrentInventoryFilter` — root's
  `compat_supply_chain.go` port assertion and `cmd/*` wiring (via the
  root alias).
- Container-image / SBOM / security-alert store ports and their
  filter/row/page/count values — the staying Postgres implementations,
  `entity/handler.go`, the incident-context stores, and `cmd/*` wiring (via the
  root aliases). The three filter `HasScope` methods are exported because
  the staying implementations call them across the boundary (advisory
  precedent: `advisory.EvidenceFilter.HasScope`).
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
- `SecurityAlertRepositoryScopeIDs` — the moved
  `alerts/store.go` and `alerts/aggregates.go` (direct import, #6642; no
  longer a root forward, since those files left root).
- `CloudRuntimeProbePerDigestLimit` — staying
  `cloud_resource_list_store.go` (via root forward).
- `BoundedSBOMWarningSummaries`,
  `SBOMAttestationWarningSummaryPreviewMaxCount` — staying
  `sbom_attestation_attachment_rows.go` decode wrappers (via root
  forwards).
- `SecurityAlertReconciliationAnchorRequiredMessage` — this package's own
  handler (`security_alerts.go`) and the moved
  `alerts/store.go` (direct import, #6642).
- Aggregate pagination offsets (`Next*AggregateOffset`) and
  `SBOMAttestationAttachmentAggregateScope` — the staying aggregate
  tests, which pin them directly (via root forwards).
- `PlanRuntimeEnvironmentCandidates`,
  `RuntimeEnvironmentPlan`,
  `MaxRuntimeEnvironmentCandidates` — the staying
  runtime-context tests (via root forwards).
- `KubernetesRuntimeEvidenceSource`,
  `KubernetesRuntimeResolutionMode` — staying
  `queryplan_profile_params_test.go` (via root forwards).
- `BuildContainerImageIdentitySourceBridge` — the staying source-bridge
  test (via root forward).
- `ImpactPacketResponder` — root's lane-B packet responder
  implementation and `cmd/*` wiring, which inject it.

## Where the tests live

These suites moved with the handlers into this package because every
reference they make resolves here (hub symbols, leaf packages, stdlib,
or suite-local doubles):

- the probe suites: cloud probe, Kubernetes probe + fair + bench
  (apply side) + perf live, findings freshness + winners-read;
- the sibling error-path suites (`sibling_stage_failed_test.go`,
  `sibling_reader_retryable_test.go`): every sibling store-read branch
  answers a handler-owned 500 with exactly one `stage_failed` record, and
  fence verdicts answer retryable 503s with `Retry-After` and no record
  (#7549). Their advisory branches register the advisory capabilities
  through the file-local `ensureSiblingAdvisoryCapabilities` helper, and
  their packet branch carries a no-op `PacketResponder` so the read (not
  the nil-responder guard) is what fails;
- the selector suite (`repository_selector_retryable_test.go`): the
  security-alert selector's catalog match and provider scope lookup answer
  retryable 503s with no record on fence verdicts, and otherwise a 500 with
  exactly one `stage_failed` record, an Error handler span, and no raw
  selector in any log attribute (#7567);
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
  `container_image_identities_source_bridge_test.go`,
  `queryplan_*`, `supply_chain_impact_runtime_digest_route_live_test.go`)
  — they reach hub symbols through root's forwards.

Do not "reunite" a staying test here until every one of its references
resolves here: shared fakes (`recording*`, `snapshot`-style doubles) and
direct unexported-method calls are the usual blockers, and forking a
double to force a move is worse than leaving the test in root.

## Shared test fixtures

`testutil` holds the fixtures both this package's tests and root
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
`kubernetes_runtime_probe_fair_live.go` (type
aliases, planner forwards, fanout accessors, one handler-method
forward) — compiled out of the default build, so default lint never
sees it. Do not add unconditional hub exports for live tests, and do
not import the parent query package from hub test files (import cycle):
extend the seam instead.

## Common changes

- Reader stage attributes on `impact_findings_query` (#7545): only the findings
  read is wrapped in `db.WithStageTimings`, so the sums cover that read and not
  the later probes. Keep the four attributes together and only when
  `Recorded()`; never import `runtime/postgres` here (the accumulator lives in
  `storage/postgres/db` for that reason).

- New supply-chain route: add the method, register it in `Mount`, add
  the capability constant here and the matrix row in root
  `contract_supply_chain.go` (via the `*Support()` constructors, never
  an inline struct), register the capability in `main_test.go`'s
  `TestMain`, and extend the root alias file. All five, or the route is
  unreachable, unregistered, unwired, or 501 in this package's tests.
- New store port method: extend the port here, the staying Postgres
  implementation in root, and the fakes on both sides. The root
  compile-time assertion in `compat_supply_chain.go` pins the
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
