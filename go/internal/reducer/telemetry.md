# Reducer Telemetry

Split from `README.md` (issue #5786). Full span and metric detail
lives here; `README.md` keeps a short pointer.

## Telemetry

Spans emitted:

- `SpanReducerRun` — wraps each `executeWithTelemetry` call
  (`service.go:308`).
- `SpanCanonicalWrite` — wraps each `processPartitionWithTelemetry`
  call in `SharedProjectionRunner` (`shared_projection_runner.go:284`).

Key metrics (all prefixed `eshu_dp_`):

- `reducer_run_duration_seconds` — per-intent execution duration, labeled by domain.
- `reducer_queue_wait_duration_seconds` — time from `AvailableAt` to claim start.
- `reducer_executions_total` — intent executions, labeled by domain, queue, status.
- `queue_claim_duration_seconds` — time to acquire one claim from Postgres.
- `shared_projection_cycles_total` — completed shared projection cycles per domain.
- `canonical_write_duration_seconds` — duration of one canonical write cycle.
- `shared_projection_intent_wait_duration_seconds` — per-domain intent queue age.
- `shared_projection_processing_duration_seconds` — per-domain partition processing.
- `shared_projection_step_duration_seconds` — per phase (retract, write, mark_completed).
- `canonical_writes_total` — includes graph-projection repair writes.
- `package_source_correlations_total` — package source-correlation decisions by
  bounded outcome (`exact`, `derived`, `ambiguous`, `unresolved`, `stale`,
  `rejected`) and reducer domain. Durable package correlation facts store
  source-hint ownership candidates and package-version publication evidence as
  `provenance_only=true`, while manifest-backed consumption decisions are
  canonical package consumption truth.
- `package_consumption_repo_edges_total` — repo-to-repo `DEPENDS_ON` edge
  intents derived from package consumption-to-owner correlations, dimensioned by
  reducer `domain` (`repo_dependency`) and `outcome`. `projected` counts emitted
  consumer-repo to owner-repo edges; `skipped_no_owner` counts consumers that
  declared package dependencies but resolved no owner this generation and so
  emit a refresh/retraction instead, removing any stale package-consumption edge
  the consumer held in a prior generation. The acceptance source-run id is a
  stable function of the package-registry scope only (not the generation), so
  re-projection across generations targets the same edges idempotently.
- `service_catalog_correlations_total` — service-catalog correlation decisions
  by bounded outcome (`exact`, `derived`, `ambiguous`, `unresolved`, `stale`,
  `rejected`) and reducer domain. Durable service-catalog correlation facts
  store catalog entity, owner, repository, service/workload IDs when explicitly
  supplied by evidence, drift status, candidate repository IDs, and evidence
  fact IDs for API/MCP freshness checks.
- `service_catalog_correlation_guardrails_total` — service-catalog admission
  guardrail events by reducer domain and bounded guardrail
  (`candidate_fanout`, `dropped_ambiguous_candidate`,
  `missing_anchor_entity`). It stays separate from the decision counter so
  correlation outcomes remain closed to admission decisions.
- `observability_coverage_correlations_total` — observability coverage
  correlation decisions by bounded outcome (`exact`, `derived`, `ambiguous`,
  `unresolved`, `stale`, `rejected`, `drifted`, `permission_hidden`), reducer
  domain, and `coverage_signal` (`alarm`, `composite_alarm`, `dashboard`,
  `datasource`, `folder`, `alert_rule`, `log_group`, `trace_sampling`,
  `scrape_target`, `rule`, `metric_route`, `log_route`, `trace_route`,
  `log_signal`, `trace_signal`, `unsupported`).
  Durable `reducer_observability_coverage_correlation` facts record
  which monitored CloudResource nodes have AWS-native coverage versus uncovered
  gaps, and which Grafana-stack declared, applied, or observed metadata identities
  are exact, drifted, stale, permission-hidden, rejected, ambiguous, or unresolved.
  The `DomainObservabilityCoverageCorrelation` domain is fact-only and is **not**
  gated on graph readiness, so the read model populates before any edge lands.
- `observability_coverage_edges_total` — observability `COVERS` graph-edge
  projection outcomes by `coverage_signal` and `resolution_mode` (`arn`,
  `bare_id`, `correlation_anchor`). The separate, gated
  `DomainObservabilityCoverageMaterialization` domain (issue #391 PR3) projects
  only `exact` coverage decisions that resolved a target `CloudResource.uid` into
  one `(:CloudResource)-[:AWS_COVERS_<signal>]->(:CloudResource)` edge per
  `(observability uid, coverage_signal, target uid)`, gated on the
  `GraphProjectionPhaseCanonicalNodesCommitted` readiness phase on the
  `cloud_resource_uid` keyspace (the same gate `aws_relationship_materialization`
  uses) so edges never resolve against uncommitted nodes. Derived, ambiguous,
  unresolved, stale, and rejected coverage stays provenance-only in the read
  model and fabricates no edge. See issue #391 and
  `docs/internal/design/391-observability-coverage-correlation.md` §6/§12.
- `incident_routing_evidence_total` — PagerDuty incident-routing graph evidence
  outcomes by reducer domain, bounded outcome, source class, and slot kind.
  The `DomainIncidentRoutingMaterialization` domain writes exact
  `IncidentRoutingEvidence` graph rows only for full declared/applied/live
  convergence or exact live-only no-IaC evidence; unsafe outcomes stay
  provenance-only and are counted with `source=provenance`.
- `kubernetes_correlations_total` — live Kubernetes correlation decisions by
  bounded outcome (`exact`, `derived`, `ambiguous`, `unresolved`, `stale`,
  `rejected`), reducer domain, and `drift_kind` (`in_sync`, `image_drift`,
  `missing_source`, `stale_source`, `unknown`). Durable
  `reducer_kubernetes_correlation` facts record how each live workload's image
  references and identity edges join to deployment-source image evidence. PR1 is
  fact-only: no graph edge is written; the gated canonical edge and the query/MCP
  read surface are follow-up PRs. See issue #388.
- `ec2_instance_nodes_total` — canonical EC2 instance `CloudResource` graph nodes
  committed by `DomainEC2InstanceNodeMaterialization`, dimensioned by `domain`. The
  handler also publishes the `cloud_resource_uid` / `canonical_nodes_committed`
  readiness phase under the distinct `ec2_instance_node_materialization:<scope>`
  entity key only after the node write succeeds (or is a legitimate no-op for an
  empty generation), emits `ec2_instance_nodes_skipped_total` (dimension
  `skip_reason`: `missing_identity` / `tombstone`), and logs an `ec2 instance node
  materialization completed` structured log with per-stage durations, node count,
  and the skip tally. See issue #1146 PR-A and
  `docs/internal/design/1146-ec2-instance-node.md`.
- `kubernetes_workload_nodes_total` — canonical `KubernetesWorkload` graph nodes
  committed by `DomainKubernetesWorkloadMaterialization`, dimensioned by `domain`.
  The handler also publishes the `kubernetes_workload_uid` /
  `canonical_nodes_committed` readiness phase only after the node write succeeds
  (or is a legitimate no-op for an empty generation), and emits a `kubernetes
  workload materialization completed` structured log with per-stage durations and
  the node count. See issue #388 and
  `docs/internal/design/388-kubernetes-workload-node.md`.
- `DomainKubernetesNamespaceMaterialization` emits NO new metric instrument
  (issue #5434). A malformed `kubernetes_live.namespace` fact still increments
  the existing `instruments.ReducerInputInvalidFacts` counter (new
  `domain`/`fact_kind` attribute values on the existing closed label set), and
  completion is logged via a `kubernetes namespace materialization completed`
  structured log (fact count, node count, environment-bound count, per-stage
  durations) mirroring the workload handler's log shape. No-Observability-Change:
  reusing `kubernetes_workload_nodes_total` for this domain's node count would
  corrupt that counter's KubernetesWorkload-specific meaning, so this domain
  deliberately adds neither a new instrument nor a reused one; a
  `kubernetes_namespace_nodes_total` counter (dimensioned by
  `environment_bound`) is a documented follow-up, not required for #5434.
- `kubernetes_correlation_edges_total` — canonical `RUNS_IMAGE` live-workload
  edges committed by `DomainKubernetesCorrelationMaterialization`, dimensioned by
  `resolution_mode` (`digest`). It counts only materialized exact edges;
  provenance-only correlation and exact decisions whose source digest resolved no
  canonical OCI node (counted in the completion log's
  `skipped_unresolvable_source`) never produce an edge. See issue #388 PR3.

  Benchmark Evidence: `go test ./internal/reducer/... -run '^$' -bench
  'BenchmarkExtractKubernetesWorkloadNodeRows|BenchmarkBuildSourceImageDigestJoinIndex'
  -benchmem -benchtime=200x` (issue #6061 moved `BenchmarkBuildSourceImageDigestJoinIndex`
  into `go/internal/reducer/kubernetescorrelation`, so the bare `./internal/reducer`
  package no longer selects it; the `...` tree form reaches both packages) on
  darwin/arm64 (Apple M3 Pro): node-row extraction
  ran `5.13 ms/op`, `9.34 MB/op`, `150,024 allocs/op` for `5,000` pod-template
  facts (bounded O(W)); the source-side digest→uid join index built in
  `0.93 ms/op`, `1.80 MB/op`, `10,065 allocs/op` for `5,000` manifest facts
  (bounded O(M) build, O(1) resolution, no per-edge round trip and no N+1). The
  cypher write path is benchmarked separately as
  `BenchmarkKubernetesWorkloadNodeWriter` in `go/internal/storage/cypher`.
  No-Regression Evidence: the node-write path reuses the proven CloudResource
  UNWIND-batched MERGE-on-uid writer and the bounded-join shape from #805 §5.1; it
  adds no graph round trip per node or per digest. `go test ./internal/reducer/...
  -run 'KubernetesWorkload|SourceImageDigestJoinIndex' -count=1` (same #6061
  package-tree caveat as the benchmark above) proves the
  object_id node identity, idempotent dedup, tombstone suppression, empty/no-op
  handling, the readiness-phase-after-write-success invariant (and that a failed
  write publishes no phase), and digest→uid resolution including the unresolvable
  and tombstone-only cases. Observability Evidence: the
  `eshu_dp_kubernetes_workload_nodes_total` counter, the `kubernetes workload
  materialization completed` log, and the InstrumentedExecutor's
  `eshu_dp_neo4j_query_duration_seconds` / `eshu_dp_neo4j_batch_size` on each
  `phase=kubernetes_workload` statement let an operator see node throughput and
  graph-write cost at 3 AM.
- `correlation_rule_matches_total`, `correlation_orphan_detected_total`, and
  `correlation_unmanaged_detected_total` — AWS runtime drift rule execution and
  admitted orphan/unmanaged findings. Unknown and ambiguous findings are exposed
  in reducer summaries, admitted-finding logs, and durable fact evidence.
  Admitted-finding logs use `resource.fingerprint`,
  `resource.identity_kind`, and `resource.type` rather than raw ARNs; exact
  ARNs stay in durable fact evidence and controlled read paths, not metric
  labels.

Log phase attributes: `telemetry.PhaseReduction` (main loop),
`telemetry.PhaseShared` (shared projection and repair runner).
