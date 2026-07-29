# Reducer Gotchas — Correlation, Queue, And Graph-Security Domains

Split from `README.md` (issue #5786). Service-catalog and
observability correlation, Kubernetes correlation, queue/generation
invariants, code-call edge rules, and the security-sensitive
SecurityGroup/IAM graph projections live here; keep the top
invariants and a pointer in `README.md`.

- **Service catalog correlation is repository-evidence gated** —
  `ServiceCatalogCorrelationHandler` writes
  `reducer_service_catalog_correlation` facts for explicit repository-id or
  repository-URL links, and for repo-local catalog descriptors emitted from a
  `git-repository-scope:<repo_id>` source scope when that repository is active.
  Repo-local service descriptors may expose the catalog entity ref as service
  evidence, but workload identity still requires separate workload proof.
  Name-only links are rejected, tombstoned matches are stale, and multiple
  active repository matches are ambiguous. Catalog names, component names, and
  ownership labels never create repository, service, or workload truth by
  themselves.

  No-Regression Evidence: `go test ./internal/reducer -run
  'TestBuildServiceCatalogCorrelationDecisions|TestServiceCatalogCorrelation'
  -count=1` proves explicit URL/id links, provider scoping, name-only rejection,
  ambiguous URL matches, repo-local source-scope admission, tombstoned
  source-scope handling, and unscoped missing-evidence behavior.

  No-Observability-Change: the existing
  `eshu_dp_service_catalog_correlations_total` metric remains labeled by domain
  and outcome; this change only reclassifies source-scope evidence into the
  existing exact/stale/unresolved outcomes.
- **Observability coverage correlation is stable-identity gated** —
  `ObservabilityCoverageCorrelationHandler` writes
  `reducer_observability_coverage_correlation` facts for exact, derived,
  ambiguous, unresolved, stale, rejected, drifted, and permission-hidden outcomes.
  A coverage edge is canonical truth (`exact`, not provenance-only) only when an
  AWS observability object resolves to a monitored CloudResource by a stable
  identity (an alarm whose AWS system dimension value, e.g. `InstanceId`, matches
  a scanned resource id). X-Ray service-name matches stay
  `derived`/provenance-only; metric-name-only alarms (e.g. `AWS/Billing`) are
  `rejected`; non-unique dimension matches are `ambiguous`; tombstoned-only
  matches are `stale`; monitored resources with no resolving coverage emit
  `unresolved` gap findings (bounded to classes with a covered peer in scope).
  Grafana, Prometheus, Mimir, Loki, and Tempo metadata facts are source-class
  correlated as declared/applied/observed evidence only; drifted,
  permission-hidden, unsupported, stale, and ambiguous provider metadata never
  creates a graph edge. The handler reads AWS facts through the existing bounded
  in-memory index, reads observability source fact families through metadata
  identity grouping, redacted dimension values never resolve a target, and the
  correlation domain itself writes no graph edge.
- **Observability `COVERS` edges are exact-only and readiness-gated** —
  `ObservabilityCoverageMaterializationHandler` (the separate
  `DomainObservabilityCoverageMaterialization` domain, issue #391 PR3) re-runs the
  same pure classifier and projects only `exact` coverage decisions that resolved
  a target `CloudResource.uid` into one
  `(:CloudResource)-[:AWS_COVERS_<signal>]->(:CloudResource)` edge per
  `(observability uid, coverage_signal, target uid)`. It gates on the
  `GraphProjectionPhaseCanonicalNodesCommitted` readiness phase (handler
  `ReadinessLookup` + the durable Postgres claim/batch/blockage gate) so edges
  never resolve against uncommitted nodes, retracts prior-generation edges scoped
  to `rel.evidence_source = 'reducer/observability-coverage'`, and fabricates no
  node (two `MATCH`es precede the `MERGE`). Derived X-Ray service coverage
  resolves a service name, not a node, so it is counted skipped and produces no
  edge; ambiguous, unresolved, stale, and rejected coverage never materialize.
  The write is idempotent under retry and partitioned by the scope-generation
  conflict fence, never single-threaded.
- **Live Kubernetes correlation is digest-first and selector-ambiguity-safe** —
  `KubernetesCorrelationHandler` writes `reducer_kubernetes_correlation` facts
  for all six outcomes plus a `drift_kind`. A live image reference joins
  digest-first (`exact`/`in_sync`), then repository+tag (`derived`); a tag that
  resolves to multiple source digests is `ambiguous`/`unknown`; a live image with
  no source is `unresolved`/`missing_source`; a tombstoned-only source is
  `stale`/`stale_source`; an unparseable ref is `rejected`. For workload
  identity, only a Kubernetes `owner_reference` edge is `exact`; a label-selector
  edge that cannot prove exact ownership stays `ambiguous` with an explicit
  `non_promotion` and is **never** promoted to `exact`. The handler reads the
  three settled `kubernetes_live.*` fact kinds for the cluster scope generation
  and joins the cross-scope active deployment-source image facts through a
  bounded in-memory index (no per-edge graph round trip). PR1 writes no graph
  edge.
- **Projection must be idempotent** — queue retries, duplicate claims, and
  partial graph writes must converge on the same truth.
- **Generation supersession** — `Runtime.execute` calls `GenerationCheck`
  before dispatching to a handler; stale intents return
  `ResultStatusSuperseded` without touching the graph.
- **`deployment_mapping` requires post-Phase-3 reopen** — the domain
  cannot produce `resolved_relationships` until after
  ReopenDeploymentMappingWorkItems runs in the bootstrap pipeline
  (bootstrap-index/main.go:273).
- **Phase publications and graph writes are not atomic** — if a graph write
  commits but the subsequent `PublishGraphProjectionPhases` call fails, the
  `GraphProjectionPhaseRepairQueue` captures the publication for retry by
  `GraphProjectionPhaseRepairer`. Do not remove the repair queue without
  understanding this failure mode.
- **Edge domain readiness gates** — shared projection domains
  `code_calls`, `sql_relationships`, and `inheritance_edges` gate on
  their domain-specific readiness phase before writing edges
  (`shared_projection.go:91–99`). `code_calls` and `sql_relationships` require
  `canonical_nodes_committed`; `inheritance_edges` requires
  `semantic_nodes_committed`.
- **Code-call chunks must not retract each other** — a code-call accepted
  unit can exceed `DefaultCodeCallAcceptanceScanLimit`. The runner processes a
  capped slice, marks it complete, and then continues with later slices from
  the same source run. `CodeCallProjectionCurrentRunHistoryLookup` is the guard
  that skips retraction after the first current-run chunk.
- **Bare code-call names are scoped before they are broadened** — same-file
  resolution wins first. Go then allows a same-directory match before the
  repository-unique fallback; if another package has the same bare name, do
  not create a repo-wide edge.
- **Go package and return-chain evidence must stay bounded** — imported
  package calls resolve only through parser import rows and repository
  directory matches. Method-return chains resolve only when one method name has
  one return type inside the same repository.
- **JavaScript-family top-level calls need file-root evidence** — only
  package entrypoint, package bin, and package export files can use the
  repo-scoped `File.uid` caller for top-level calls. Do not promote arbitrary
  module-body calls to roots.
- **`BuildSharedProjectionIntent` produces a stable SHA256 ID** —
  changing any of the identity fields breaks idempotency for in-flight
  intents (`shared_projection.go:59–66`).

No-Regression Evidence: `go test ./internal/reducer ./internal/storage/postgres ./cmd/reducer -run 'TestPlatformMaterializationHandlerLocksInfrastructurePlatformIDs|TestNewDefaultRegistryWiresPlatformGraphLocker|TestPlatformGraphLocker|TestPlatformGraphLockerForReducer|TestBuildReducerServiceWiresDefaultRuntimeAndQueue' -count=1` proves deployment_mapping platform writes acquire per-Platform.id locks without lowering worker concurrency and skip lock wiring when transactions are unavailable.
Observability Evidence: existing reducer queue conflict fields, fact-work retry counters, deployment_mapping completion logs, graph-write retry WARNs, and Postgres query errors expose blocked, retrying, failed, and completed platform materialization work; no new metric label was needed.

No-Regression Evidence: `go test ./internal/reducer -run 'TestSupplyChainCVEGroupRepresentative(UsesSourcePriority|SelectsByPriorityAndSkipsWithdrawn)|TestAdvisorySourcePriorityDoesNotAllocate' -count=1` failed on baseline `4b9128d` because the legacy product representative returned envelope order and `advisorySourcePriority` allocated four times per run; the same command passed after the change on Go 1.26.3. `go test ./internal/reducer -count=1` also passed with pure in-memory fixture envelopes, no graph backend, no reducer queue rows, and the existing provenance tests still producing one consolidated finding per CVE/package anchor.
No-Observability-Change: the change only makes in-memory advisory-source selection deterministic and allocation-free inside existing reducer admission. It does not add a queue, graph write, Postgres query, runtime knob, or metric label; existing `SpanReducerRun`, `reducer_run_duration_seconds`, `reducer_executions_total`, reducer completion logs, and durable finding payloads remain the operator-visible signals for this path.

No-Regression Evidence: `go test ./internal/reducer -run TestSupplyChainImpactHandlerUsesManifestDependencyBeforeRegistryCorrelation -count=1` failed before the impact handler loaded active manifest dependencies, then passed after the reducer consumed advisory-bounded npm lockfile evidence directly. `go test ./internal/reducer ./internal/storage/postgres ./internal/coordinator ./internal/workflow ./cmd/workflow-coordinator ./cmd/collector-package-registry ./cmd/collector-vulnerability-intelligence -count=1` also passed, proving the manifest-dependency read path remains bounded by the existing active dependency index and does not require package-registry completion before exact repository impact.
No-Observability-Change: this uses the existing `ListActivePackageManifestDependencyFacts` Postgres read, reducer run span, reducer duration metric, reducer execution counter, durable evidence path, finding payload, and package-registry/vulnerability collector status rows. No new metric label or graph write path was added.

Performance Evidence: `go test ./internal/reducer -run '^$' -bench BenchmarkAddManifestDependencySupplyChainConsumption -benchmem -count=3` on darwin/arm64 dropped manifest dependency matching from about `52.2 MB/op` and `922k allocs/op` to about `610 KB/op` and `7.3k allocs/op` after affected package match keys were flattened once per reducer execution and dependency keys were built once per manifest dependency.

No-Regression Evidence: `go test ./internal/reducer -run 'TestBuildSecurityAlertReconciliations(FailsClosedForAmbiguousProviderRepositoryScope|ResolvesProviderAlertRepositoryScope)|TestBuildSupplyChainImpactFindings(SkipsAmbiguousProviderRepositoryScope|ResolvesProviderAlertRepositoryScope)' -count=1` failed before provider-alert repository scopes were resolved against canonical owned dependency evidence, then passed after raw provider repository IDs were preserved separately and ambiguous repository-name matches failed closed.
No-Observability-Change: this is an in-memory reducer admission and payload-shape correction over facts already loaded through `ListActiveSupplyChainImpactFacts`; existing reducer run spans, reducer duration metrics, reducer execution counters, durable `reducer_security_alert_reconciliation` payloads, and `reducer_supply_chain_impact_finding` evidence paths remain the operator-visible signals.

No-Regression Evidence: `go test ./internal/reducer -run 'TestBuildSecurityAlertReconciliationsUsesSupportedNpmLockfileEvidence|TestSecurityAlertReconciliationHandlerDefersPackageTriggeredLockfileEvidence' -count=1` failed before security alert reconciliation consumed supported npm lockfile `content_entity` dependency evidence, then passed after the handler loaded active manifest dependency facts and the classifier treated matching lockfile rows as owned dependency evidence. Provider-only rows without local dependency evidence still stay provider-only with a missing-evidence reason, and package-triggered rows defer while matching impact evidence is still pending.
No-Observability-Change: this changes reducer reconciliation over facts loaded through the existing active manifest-dependency read path. It adds no graph write, route, queue domain, worker, metric label, or runtime knob; operators still diagnose the path through existing reducer run spans, reducer execution counters, durable `reducer_security_alert_reconciliation` payload reasons and evidence IDs, fact-load Postgres timings, and API/MCP reconciliation read spans.

No-Regression Evidence: `go test ./internal/reducer -run 'TestSecurityAlertReconciliationHandler(UsesRepositoryFactsForLockfileScope|DefersPackageTriggeredLockfileEvidence)|TestBuildSecurityAlertReconciliationsUsesSupportedNpmLockfileEvidence' -count=1` failed before provider-scoped alerts could match lockfile dependency facts whose repository was stored as a canonical Eshu repository id, then passed after the handler loaded active repository facts before extracting manifest dependency evidence. Provider alerts still fail closed when repository proof is missing or ambiguous because the classifier requires repository id equality or a proven repository-name bridge before using package evidence.
No-Observability-Change: this reuses the existing active repository fact read, active manifest-dependency read, reducer run span, reducer duration metric, reducer execution counter, durable `reducer_security_alert_reconciliation` payload reasons/evidence IDs, fact-load Postgres timings, and API/MCP reconciliation read spans. It adds no queue domain, graph write, route, runtime knob, metric instrument, or metric label.

No-Regression Evidence: `go test ./internal/reducer -run 'TestBuildSupplyChainImpactFindingsUsesProviderAlertManifestDependencyEvidence|TestSupplyChainImpactHandlerUsesRepositoryScopedSecurityAlertLockfileEvidence|TestSupplyChainImpactHandlerUsesManifestDependencyBeforeRegistryCorrelation|TestSecurityAlertReconciliationHandlerUsesRepositoryFactsForLockfileScope' -count=1` failed before provider-scoped security-alert impact could keep lockfile evidence through the scope filter and write a `reducer_supply_chain_impact_finding`, then passed after the impact handler loaded active repository facts before active manifest dependencies and the scope filter allowed manifest facts by already-proven canonical repository id. Provider alerts still fail closed when no repository bridge exists because package evidence is only used after repository id or repository-name proof.
No-Observability-Change: this keeps the existing supply-chain impact reducer domain, writer, queue item shape, API routes, MCP routes, metrics, and runtime knobs. Operators diagnose the path through existing reducer execution counters, reducer run spans, fact-load Postgres timings, durable `reducer_supply_chain_impact_finding` payload evidence paths/evidence IDs, durable security-alert reconciliation payload reasons, and API/MCP impact/reconciliation read spans.

No-Regression Evidence: `go test ./internal/reducer -run 'TestBuildSupplyChainImpactFindingsConnectsProviderAlertRuntimeContext|TestSupplyChainImpactHandlerUsesRepositoryScopedSecurityAlertLockfileEvidence|TestBuildSupplyChainImpactFindingsUsesProviderAlertWithOwnedLockfileEvidence|TestBuildSupplyChainImpactFindingsResolvesProviderAlertRepositoryScope|TestBuildSupplyChainImpactFindingsKeepsRepositoryOnlyRuntimeHopsMissing' -count=1` failed before provider-alert-seeded impact findings ran through the runtime-context finalizer, then passed after provider alert rows attached already-loaded workload identity and service-catalog evidence while repository-only rows still kept missing workload/service evidence explicit.
No-Observability-Change: this reuses the existing supply-chain impact active fact expansion, in-memory runtime-context finalizer, reducer writer, durable `reducer_supply_chain_impact_finding` payload fields, query readiness envelope, API/MCP impact read spans, reducer run spans, and reducer execution counters. It adds no graph write, queue domain, route, runtime knob, metric instrument, or metric label.

No-Regression Evidence: `go test ./internal/reducer -run 'TestBuildSupplyChainImpactFindingsConnectsProviderAlertRuntimeContextFrom(RepositoryScope|RelatedRepositoryScope)' -count=1` failed before supply-chain impact normalized repository-scoped workload identity facts, then passed. The regressions use the emitted reducer workload shape where `scope_id` or `related_scope_ids` carries `git-repository-scope:repository:<id>` and `entity_keys` carries the workload id, so provider-alert impact rows attach real workload evidence instead of leaving `workload evidence missing`.
No-Observability-Change: this only normalizes reducer workload fact scope ids while building supply-chain impact filters and runtime context. It adds no route, graph query, queue, worker, runtime knob, metric instrument, or metric label; operators still diagnose the path through existing reducer run spans, reducer execution counters, durable `reducer_supply_chain_impact_finding` payloads, API/MCP query handler spans, and Postgres query duration metrics.

No-Regression Evidence: `go test ./internal/reducer -run 'TestSupplyChainImpactStableFactKeyIgnoresSourceScopeGeneration|TestSupplyChainImpactStableFactKeyIncludesRepository|TestPostgresSupplyChainImpactWriterPersistsSignalsWithoutPriorityCollapse' -count=1` proves supply-chain impact row storage remains source-scoped while the stable fact key and public finding identity are canonical to the logical vulnerability tuple. Repeated vulnerability-intelligence, package-registry, or mixed-source reducer cycles can preserve source-specific rows without changing the user-facing finding identity.
No-Observability-Change: this changes reducer fact identity fields only. Existing reducer run spans, reducer duration metrics, reducer execution counters, durable `reducer_supply_chain_impact_finding` payloads, query readiness envelopes, and API/MCP read spans remain the operator-visible signals; no queue, graph write, metric label, or runtime knob was added.

No-Regression Evidence: `go test ./internal/reducer -run 'Swift|PackageConsumptionDecisionsMatchesSwift' -count=1` proves exact Swift Package Manager lockfile evidence joins to package identity and OSV `SwiftURL` advisory ranges without admitting branch-only, revision-only, local, or path pins as precise impact.
No-Observability-Change: Swift impact reuses existing supply-chain impact reducer facts, match reasons, missing-evidence fields, query readiness envelopes, API/MCP read spans, and reducer run metrics. It adds no graph write, queue domain, worker, runtime knob, metric instrument, or metric label.

No-Regression Evidence: `go test ./internal/reducer -run 'TestSecurityAlertReconciliationHandlerDefersProviderTriggeredPendingImpactEvidence|TestSecurityAlertReconciliationDefersPackageTriggeredUnmatchedEvidence|TestSecurityAlertReconciliationHandlerUsesRepositoryFactsForLockfileScope|TestSupplyChainImpactHandlerUsesRepositoryScopedSecurityAlertLockfileEvidence' -count=1` failed before provider-triggered reconciliation waited for pending impact evidence, then passed after the same bounded retry guard used for package-triggered repairs covered provider-alert triggers. Provider-only alerts still write provider-only when no owned dependency evidence exists, and retries stop at the existing max-attempt boundary.
No-Regression Evidence: `go test ./internal/reducer -run TestServiceRunLogsClassifiedExecutionFailure -count=1` failed before the service log used the retryable error's `FailureClass()`, then passed after reducer execution logs preserved `failure_class=security_alert_reconciliation_waiting_for_impact`.
No-Observability-Change: this changes the reducer retry/log classification for premature reconciliation only. Existing reducer queue retry status, `failure_class=security_alert_reconciliation_waiting_for_impact`, retry delay, max attempts, reducer run spans, reducer execution counters, durable reconciliation payloads, and API/MCP read spans remain the operator-visible signals; no metric label, queue domain, graph write, route, or runtime knob was added.

No-Regression Evidence: `go test ./internal/reducer -run 'ObservabilityCoverage' -count=1` and `go test ./internal/telemetry -run 'TestMetricDimensionKeys|TestNewInstruments' -count=1` pass for the `observability_coverage_correlation` domain. `BuildObservabilityCoverageDecisions` is a pure in-memory function over the scope generation's AWS and observability source fact envelopes: it builds one bounded AWS coverage index (O(R) over target resources by ARN / bare resource id / correlation anchor — the #805 §5.1 join shape), resolves each AWS observability relationship by O(1) map lookup (O(E)), and groups Grafana-stack metadata by bounded provider/signal/object identity. Durable writes go through the existing `canonicalReducerFactInsertQuery` path. There is no graph write, no new table or schema migration, no per-edge graph round trip, and no N+1; metadata-only outcomes remain provenance-only reducer facts.
Observability Evidence: the `eshu_dp_observability_coverage_correlations_total` counter is dimensioned by `domain`, `outcome` (exact, derived, ambiguous, unresolved, stale, rejected, drifted, permission-hidden), and bounded `coverage_signal`, so an operator can answer at 3 AM which observability signal class is losing coverage and whether the cause is a gap, drift, hidden data, an ambiguous match, stale evidence, or a rejected weak signal. The `EvidenceSummary` and `CanonicalWrites` on the reducer `Result`, plus the existing reducer run spans, execution counters, fact-load Postgres timings, and the durable `reducer_observability_coverage_correlation` payload (`outcome`, `coverage_status`, `source_class`, `source_classes`, `resource_class`, `freshness_state`, `resolution_mode`, `candidate_target_uids`, `evidence_fact_ids`) expose per-intent progress; no graph-write metric is added for provenance-only metadata because the correlation domain performs no graph write.

## SecurityGroup reachability graph (#1135 PR2b, Option D)

No-Regression Evidence: focused Eshu-owned write-path benchmark on Apple M4 Pro, Go test no-op group executor (isolates statement construction/batching from graph round trips). `go test ./internal/storage/cypher -run '^$' -bench BenchmarkSecurityGroupReachabilityWriter -benchmem -benchtime=50x` writes all three reachability surfaces (5000 `:SecurityGroupRule` nodes + 5000 SG→rule edges + 5000 rule→endpoint `TO` edges = 15000 rows) at 4.26ms/op, 7.76MB/op, 75367 allocs/op — ~283ns/row, in the same shape class as the proven baselines on the same machine (`BenchmarkObservabilityCoverageEdgeWriter` 1.72ms/op for 5000 rows ≈ 344ns/row; `BenchmarkKubernetesCorrelationEdgeWriter` 1.32ms/op for 5000 rows ≈ 264ns/row). The writer reuses the identical batched `UNWIND` + `MATCH-MATCH-MERGE`-on-uid shape with static-token / validated-label grouping, so it inherits the COVERS/RUNS_IMAGE no-N+1 profile; the higher absolute op time is the three write surfaces, not a per-row regression. Port and protocol live in the rule NODE key, keeping the relationship MERGE on a static token so NornicDB uses its relationship hot path (avoiding the 20s property-keyed MERGE timeout, #805 §5.3). Rule/endpoint/SG MATCHes anchor on uid (backed by the new `SecurityGroupRule` uid constraint + NornicDB uid index, plus the existing CidrBlock/PrefixList/CloudResource uid indexes), so no MATCH falls back to a label scan. `go test ./internal/reducer ./internal/storage/cypher ./internal/storage/postgres ./internal/projector ./internal/graph ./internal/telemetry ./cmd/reducer -count=1` passes, including the triple-readiness gate (`TestReducerQueueClaimWaitsForSecurityGroupReachabilityTripleReadiness`), idempotent reprojection, per-port distinctness, and graceful-degrade skip cases.

Observability Evidence: the new `eshu_dp_security_group_reachability_rule_nodes_total` (rule nodes committed), `eshu_dp_security_group_reachability_edges_total` (`edge_type` = `sg_rule` / `rule_endpoint`), and `eshu_dp_security_group_reachability_skipped_total` (`skip_reason` = `unresolved_anchor` / `unresolved_endpoint` / `unknown_source`) counters, the `reducer.security_group_reachability_materialization` span, and a structured completion log carrying per-stage durations (load / extract / retract / write) plus the skip tally let an operator answer at 3 AM "are reachability edges landing, and if a generation produced rule nodes but zero `TO` edges, which endpoint family was unscanned?". The durable status-blockage query surfaces which of the three gate keyspaces (`security_group_rule_uid` / `security_group_endpoint_uid` / `cloud_resource_uid`) is the one still uncommitted.

## IAM privilege-escalation graph (#1134 PR3, security-sensitive)

`DomainIAMEscalationMaterialization` projects the merged `aws_iam_permission` facts into conservative `CAN_ESCALATE_TO` edges between IAM principal and target `CloudResource` nodes. `ExtractIAMEscalationEdges` evaluates each principal's escalation primitives against the curated catalog in `iam_escalation_catalog.go` (documented in `docs/internal/design/1134-iam-privilege-escalation-catalog.md`): a primitive arms only when every required action is present in the principal's trusted-Allow set (Allow, unconditioned, no `NotAction`/`NotResource`, not Deny-blocked), and an edge is written only when the target resolves to EXACTLY ONE scanned IAM node. Wildcard/many-resource targets, Deny, condition-gated, `NotAction`, unresolved, and cross-account-unscanned targets materialize no edge and are counted, never dropped silently; `sts:AssumeRole` is deferred to the separate `CAN_ASSUME` trust edge (#1134 PR2). It is EDGE-ONLY on the existing `cloud_resource_uid` keyspace and gates on the `cloud_resource_uid` / `canonical_nodes_committed` readiness phase.

Performance Evidence: focused Eshu-owned write-path benchmark on Apple M4 Pro, no-op group executor (isolates statement construction/batching from graph round trips). `go test ./internal/storage/cypher -run '^$' -bench BenchmarkIAMEscalationEdgeWriter -benchmem` writes 5,000 `CAN_ESCALATE_TO` edges at batch 500 in ~1.33ms/op (~1.97MB/op, ~25,068 allocs/op) — same shape class as the proven `BenchmarkKubernetesCorrelationEdgeWriter` (1.32ms/op for 5,000 rows). The writer reuses the identical batched `UNWIND` + `MATCH-MATCH-MERGE`-on-uid shape with a static `CAN_ESCALATE_TO` relationship type; the escalation primitive set lives in an edge property, never in the MERGE key, keeping the relationship MERGE on a static token so NornicDB uses its relationship hot path (avoiding the 20s property-keyed MERGE timeout, #805 §5.3). Both endpoint MATCHes anchor on the existing CloudResource uid index, so no MATCH falls back to a label scan. Target resolution is bounded in-memory (the #805 §5.1 ARN join index), O(1) per exact match with no per-edge graph round trip and no N+1. `go test ./internal/reducer ./internal/storage/cypher ./internal/telemetry ./cmd/reducer -count=1` passes, including the readiness gate, idempotent reprojection, the conservative skip cases (wildcard/many/Deny/conditioned/NotAction/unresolved), the `sts:AssumeRole` deferral, the self-loop drop, and the multi-primitive merge-into-one-edge case.

Observability Evidence: the new `eshu_dp_iam_escalation_edges_total` (escalation edges committed) and `eshu_dp_iam_escalation_skipped_total` (`skip_reason` = `skipped_ambiguous` / `skipped_unresolved` / `skipped_deny` / `skipped_conditioned` / `skipped_not_action_resource` / `skipped_incomplete` / `deferred_can_assume`) counters, the `reducer.iam_escalation_materialization` span, and a structured completion log carrying per-stage durations (load / extract / retract / write) plus the skip tally let an operator answer at 3 AM "are escalation edges landing, and if a generation produced zero edges, is it because policies use wildcard resources (`skipped_ambiguous`) or because IAM targets were not scanned (`skipped_unresolved`)?". The `cloud_resource_uid` readiness gate is the same durable status-blockage surface the #805 / #1135 edge slices use.

## IAM CAN_PERFORM effective-permission graph (#1134 PR4a/PR4b/PR4c, security-sensitive)

`DomainIAMCanPerformMaterialization` projects the merged `aws_iam_permission`,
`aws_iam_permission_boundary`, and `aws_resource_policy_permission` facts into
conservative `CAN_PERFORM` edges between an IAM principal `CloudResource` node
and the resource `CloudResource` node a catalogued sensitive action applies to.
`ExtractIAMCanPerformEdges` evaluates each scanned principal's trusted-Allow
identity statements, intersects them with attached permissions-boundary managed
policy statements when present, and evaluates exact resource-policy grantees
against the CLOSED catalog in
`iam_can_perform_catalog.go` (documented in
`docs/internal/design/1134-iam-can-perform.md`). An edge is written only when a
catalogued action is granted (Allow, unconditioned, no `NotAction`/`NotResource`,
not Deny-blocked) AND the target resolves to EXACTLY ONE scanned `CloudResource`
node of the catalog-expected resource type. A permissions boundary is a ceiling,
not a grant source: boundary statements must allow the candidate
action/resource, while boundary Deny, missing docs, conditions, `NotAction`, and
`NotResource` suppress the identity edge conservatively. Resource-policy grants
additionally require `principal_arns[]` to resolve to scanned IAM role/user nodes
and the statement Resource patterns to apply to the attached resource. The
granted action set is written as a sorted/deduped edge property `rel.actions`
(never in the MERGE key), alongside `rel.action_count`, `rel.grant_sources`, and
`rel.evaluation_scope` (`identity_policy_only`,
`identity_policy_with_permission_boundary`, `resource_policy_only`,
`identity_and_resource_policy`, or
`identity_and_resource_policy_with_permission_boundary`). Uncatalogued actions,
wildcard/many-resource targets, `*`, public or unscanned principals, Deny,
condition-gated, `NotAction`/`NotResource`, unresolved/cross-account/wrong-type,
self-loops, and boundary-missing-allow cases materialize no edge and are counted,
never dropped silently. It is EDGE-ONLY on the existing `cloud_resource_uid`
keyspace and gates on the `cloud_resource_uid` / `canonical_nodes_committed`
readiness phase.

The honesty boundary is explicit and load-bearing: a PRESENT edge means an
identity policy, resource policy, or both grant the action on the resolved
resource, and when `evaluation_scope` includes `permission_boundary` the attached
permissions boundary was intersected with that identity grant. Conditioned
statements are source provenance only: the scanner stores condition key/operator
names but not values or request context, so no conditioned statement is promoted
to an exact CAN_PERFORM edge. SCPs and session policies are still ignored. A
MISSING edge does NOT mean "cannot perform" — it can mean the action is
uncatalogued, the resource or principal was not scanned or was ambiguous, a
boundary did not allow it, a condition made the grant provenance-only, or a later
evaluator slice has not been implemented. This is why the slice is conservative
and skip-counted rather than claiming a complete effective-permission lattice.

Benchmark Evidence: focused Eshu-owned write-path benchmark, no-op group executor (isolates statement construction/batching from graph round trips). `go test ./internal/storage/cypher -run '^$' -bench 'BenchmarkIAMCanPerformEdgeWriter|BenchmarkIAMEscalationEdgeWriter' -benchmem -count=3` writes 5,000 `CAN_PERFORM` edges at batch 500 in `1.25 ms/op`, `1.24 ms/op`, and `1.24 ms/op` (`~1.97 MB/op`, `25,068 allocs/op`) versus the shipped `BenchmarkIAMEscalationEdgeWriter` baseline at `1.21 ms/op`, `1.21 ms/op`, and `1.20 ms/op` on the identical row shape, staying under the 10% stop threshold. The writer reuses the identical batched `UNWIND` + `MATCH-MATCH-MERGE`-on-uid shape with a static `CAN_PERFORM` relationship type; the granted action set lives in an edge property, never in the MERGE key, keeping the relationship MERGE on a static token so NornicDB uses its relationship hot path. Both endpoint MATCHes anchor on the existing CloudResource uid index, so no MATCH falls back to a label scan. Target resolution is bounded in-memory (the #805 §5.1 ARN join index), O(1) per exact match with no per-edge graph round trip and no N+1. `go test ./internal/reducer ./internal/storage/cypher ./internal/telemetry ./internal/projector ./internal/storage/postgres ./cmd/reducer -count=1` passes, including the readiness gate, idempotent reprojection, the conservative skip cases (uncatalogued / wildcard / many / Deny / conditioned / NotAction / unresolved / self-loop), and the multi-action merge-into-one-edge case.

Observability Evidence: the new `eshu_dp_iam_can_perform_edges_total` (`resolution_mode` = `exact_arn` / `single_glob`), `eshu_dp_iam_can_perform_skipped_total` (`skip_reason` = `skipped_uncatalogued_action` / `skipped_ambiguous` / `skipped_unresolved` / `skipped_deny` / `skipped_conditioned` / `skipped_not_action_resource` / `skipped_self_loop` / `skipped_permission_boundary`), and `eshu_dp_iam_can_perform_conditioned_total` (`confidence` = `provenance_only`) counters, the `reducer.iam_can_perform_materialization` span, and a structured completion log carrying per-stage durations (load / extract / retract / write), `permission_boundary_fact_count`, `conditioned_provenance_only`, and the full skip tally let an operator answer at 3 AM "are CAN_PERFORM edges landing, and if a generation produced zero edges, is it because policies use wildcard resources (`skipped_ambiguous`), the resources were not scanned (`skipped_unresolved`), actions are outside the closed catalog (`skipped_uncatalogued_action`), a permissions boundary did not allow the identity grant (`skipped_permission_boundary`), or conditioned grants were withheld as provenance-only (`conditioned_provenance_only`)?". The `cloud_resource_uid` readiness gate is the same durable status-blockage surface the #805 / #1135 edge slices use.

No-Regression Evidence: PR4c intersects identity-policy CAN_PERFORM grants with
permissions-boundary policy evidence without changing the writer MERGE identity,
queue gate, graph schema, or row shape. `go test ./internal/reducer -run
'PermissionBoundary|NoPermissionBoundary|TestIAMCanPerformHandlerLoadsPermissionBoundaryFacts'
-count=1` covers boundary allow, missing allow, explicit Deny, missing document,
conditioned allow, NotResource, duplicate evidence, boundary statement without
attachment, no-boundary identity scope, and fact-kind loading. `go test
./internal/collector/awscloud/services/iam/awssdk -run
'PermissionBoundary|BoundedManagedPolicyStatements' -count=1` covers one-document
boundary fetch plus the existing attached-policy cap. The after measurement is
focused reducer/collector proof because the change is in-memory extraction and
SDK fact normalization only.

No-Regression Evidence: PR4b reducer follow-up adds `aws_resource_policy_permission`
fact loading and exact resource-policy grant extraction without changing the
relationship MERGE identity. `go test ./internal/reducer -run 'IAMCanPerform'
-count=1` proves identity-only, resource-only, both-source merge, public/unscanned
principal skips, conditioned/NotResource/Deny skips, wrong-resource-pattern
refusal, readiness, and idempotent reprojection behavior. `go test
./internal/storage/cypher -run 'IAMCanPerformEdgeWriter' -count=1` proves
`grant_sources` is a SET property and not part of the `CAN_PERFORM` MERGE key.
The same writer benchmark remains the performance guard because row shape changed
only by one bounded list property: `go test ./internal/storage/cypher -run '^$'
-bench 'BenchmarkIAMCanPerformEdgeWriter|BenchmarkIAMEscalationEdgeWriter'
-benchmem -count=3` produced CAN_PERFORM `1.25/1.24/1.24 ms/op` versus
escalation `1.21/1.21/1.20 ms/op` for 5,000 rows at batch 500 on Apple M4 Pro.

No-Regression Evidence: the review fix moves `skipped_uncatalogued_action`
accounting into `buildIAMCanPerformGrant`, so uncatalogued Allow actions never
arm the grant before the catalog loop. The target-resolution and graph-write
baseline remains the CAN_PERFORM benchmark above: 5,000 edge rows at batch 500,
static `CAN_PERFORM` relationship MERGE, CloudResource uid endpoint MATCHes, and
bounded in-memory ARN lookup. The after measurement for this fix is focused
unit proof, not a new graph benchmark, because no writer, queue, Cypher, batch
size, readiness gate, or graph row shape changed: `go test ./internal/reducer -run 'TestBuildIAMCanPerformGrantCountsUncataloguedActions|TestIAMCanPerformUncatalogued' -count=1` and `go test ./internal/reducer -count=1` pass. The new regression
proves an uncatalogued `cloudwatch:getmetricdata` action is refused and counted,
and the mixed `s3:listbucket` plus `s3:getobject` fixture still writes only the
catalogued action while counting the uncatalogued one.

No-Observability-Change: the fix uses the existing
`eshu_dp_iam_can_perform_skipped_total{skip_reason="skipped_uncatalogued_action"}`
counter, span, structured completion log, and durable readiness blockage path.
It adds no metric instrument, metric label, span name, log field, runtime flag,
queue state, graph label, or API/MCP surface.

No-Regression Evidence: PR4d adds redaction-safe condition operator summaries
to IAM and resource-policy source facts and classifies every conditioned
CAN_PERFORM candidate as provenance-only. Focused tests cover IAM policy
normalization, `aws_iam_permission`, `aws_iam_permission_policy`,
`aws_resource_policy_permission`, S3/KMS resource-policy adapters, conditioned
identity/resource-policy graph refusal, unknown operators, and
`eshu_dp_iam_can_perform_conditioned_total{confidence="provenance_only"}`. The
change is metadata normalization plus in-memory tallying only: no graph schema,
Cypher, queue, readiness gate, MERGE key, or writer row-shape change.

No-Regression Evidence: PR4e expands the reviewed CAN_PERFORM catalog from 9 to
21 actions: `s3:listbucket`, `kms:generatedatakey`,
`secretsmanager:putsecretvalue`, `ssm:getparameters`, `dynamodb:query`,
`dynamodb:scan`, `dynamodb:putitem`, `dynamodb:updateitem`,
`dynamodb:deleteitem`, `ec2:stopinstances`, `rds:stopdbinstance`, and
`lambda:invokefunction`. `go test ./internal/reducer -run
'IAMCanPerform(Catalog|PR4e|ResourceTypeOfARN|UncataloguedAction)' -count=1`
failed before the new catalog entries and Lambda base-function classifier, then
passed. `go test ./internal/reducer -run 'IAMCanPerform' -count=1` keeps the
broader skip taxonomy, duplicate merge, resource-policy, permission-boundary,
conditioned provenance-only, and handler coverage green.

Performance Evidence: `go test ./internal/storage/cypher -run '^$' -bench
'BenchmarkIAMCanPerformEdgeWriter|BenchmarkIAMEscalationEdgeWriter' -benchmem
-benchtime=100x -count=3` keeps the unchanged CAN_PERFORM writer at about
`1.25-1.33 ms/op`, `1.97 MB/op`, and `25,068 allocs/op` for 5,000 rows.

No-Observability-Change: the expansion adds only closed catalog entries and one
base Lambda function ARN classifier. Existing edge, skipped, and conditioned
counters, the `reducer.iam_can_perform_materialization` span, readiness retry
errors, and the completion log still expose projected edge count, bounded skip
reasons, conditioned provenance-only count, fact counts, and stage durations. No
metric label, graph writer shape, Cypher, queue gate, graph property, or API/MCP
surface changes.
