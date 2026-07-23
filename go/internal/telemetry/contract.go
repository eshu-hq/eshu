// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package telemetry defines the frozen Go data-plane observability contract.
package telemetry //nolint:filelength // 685-line contract doc: SpanNames/LogKeys/MetricDimensionKeys tables are referenced as a single frozen table by the X2 verifier and dashboards. Splitting breaks the table → row parity guarantee.

import (
	"errors"
	"maps"
	"strings"
)

const (
	// DefaultServiceNamespace is the stable namespace shared by Go data-plane
	// runtimes when they publish telemetry resources.
	DefaultServiceNamespace = "eshu"
	// DefaultSignalName is the shared OTEL instrumentation name for the data
	// plane bootstrap substrate.
	DefaultSignalName = "eshu/go/data-plane"

	// InstrumentationScopeName is the stable package-level scope name used by
	// the telemetry bootstrap contract itself.
	InstrumentationScopeName = "eshu/go/internal/telemetry"
)

// Metric dimension keys define the stable labels used by the Go data-plane
// telemetry contract.
const (
	MetricDimensionScopeKind     = "scope_kind"
	MetricDimensionSource        = "source"
	MetricDimensionSourceSystem  = "source_system"
	MetricDimensionCollectorKind = "collector_kind"
	MetricDimensionDomain        = "domain"
	MetricDimensionPartitionKey  = "partition_key"
	// MetricDimensionPartitionID labels per-(domain, partition) shared-projection
	// histograms with the numeric partition slot (0-based, bounded by
	// ESHU_SHARED_PROJECTION_PARTITION_COUNT). It is a small closed set (≤64
	// partitions by operator choice), never a raw intent id or scope id.
	MetricDimensionPartitionID  = "partition_id"
	MetricDimensionRunner       = "runner"
	MetricDimensionLookupResult = "lookup_result"
	MetricDimensionErrorType    = "error_type"
	MetricDimensionRepoSizeTier = "repo_size_tier"
	MetricDimensionSkipReason   = "skip_reason"
	MetricDimensionNodeType     = "node_type"
	// MetricDimensionNodeLabel labels graph maintenance metrics with a closed
	// Cypher label such as Repository, Platform, or EvidenceArtifact. Producers
	// must never use raw node ids, names, paths, or provider locators here.
	MetricDimensionNodeLabel = "node_label"
	// MetricDimensionAgeBucket labels active-generation gauges with a closed
	// activation-age bucket (fresh, aging, stuck). Producers must never use raw
	// scope ids, generation ids, or timestamps here.
	MetricDimensionAgeBucket  = "age_bucket"
	MetricDimensionEdgeType   = "edge_type"
	MetricDimensionWritePhase = "write_phase"
	MetricDimensionOutcome    = "outcome"
	// MetricDimensionGuardrail labels counters for bounded guardrail classes
	// that are not admission or correlation outcomes.
	MetricDimensionGuardrail = "guardrail"
	// MetricDimensionPolicyID labels bounded policy counters, such as search
	// decay scoring decisions, with a configured low-cardinality policy token.
	MetricDimensionPolicyID = "policy_id"
	// MetricDimensionEvidenceClass labels search and evaluation counters with a
	// closed evidence family such as ci_run, deployment_event, or
	// relationship_candidate.
	MetricDimensionEvidenceClass = "evidence_class"
	MetricDimensionBackendKind   = "backend_kind"
	// MetricDimensionConfidence labels reducer read-model metrics with a closed
	// confidence enum such as exact, partial, or unknown.
	MetricDimensionConfidence = "confidence"
	// MetricDimensionRiskType labels posture-observation metrics with bounded
	// risk classes, never raw principal, role, path, or policy identifiers.
	MetricDimensionRiskType = "risk_type"
	// MetricDimensionSeverity labels posture-observation metrics with bounded
	// operator severity classes.
	MetricDimensionSeverity = "severity"
	MetricDimensionResult   = "result"
	MetricDimensionReason   = "reason"
	MetricDimensionKind     = "kind"
	MetricDimensionAction   = "action"
	// MetricDimensionMCPMethod labels eshu_dp_mcp_transport_auth_denied_total
	// with the bounded JSON-RPC method a denied MCP transport request named
	// (initialize, tools/list, tools/call, ping, notifications/initialized,
	// "sse", "other", or "unknown"), never a raw unbounded string parsed from
	// an unauthenticated request body.
	MetricDimensionMCPMethod = "mcp_method"
	// MetricDimensionAuthPath labels every eshu_dp_gcp_freshness_events_total
	// series with a bounded four-value enum: "shared_token" or "oidc" (the
	// webhook listener's accepted auth path that authenticated the inbound
	// push), "none" (the webhook listener rejected the request — neither path
	// matched), or "n/a" (the coordinator's downstream handoff loop, which has
	// no request to authenticate). Every producer of this counter MUST set
	// this label so all series share one label set; producers must never use
	// a raw header, token, or claim value here.
	MetricDimensionAuthPath  = "auth_path"
	MetricDimensionProvider  = "provider"
	MetricDimensionEventKind = "event_kind"
	MetricDimensionDecision  = "decision"
	MetricDimensionStatus    = "status"
	MetricDimensionOperation = "operation"
	// MetricDimensionGate labels graph-write backpressure metrics with the
	// permit-pool class a write drew from: "canonical" (canonical,
	// handler-edge, shared-projection, secrets/IAM, orphan-sweep, and
	// materializer writes), "semantic" (the semantic entity write path), or
	// "aggregate" (the legacy-only-mode outer pool that bounds the combined
	// canonical+semantic total to the legacy ceiling; only emits while neither
	// per-class env is set). Splitting the pool by gate is issue #4448: before
	// the split, a slow semantic write could starve canonical writes (and vice
	// versa) because both drew from one shared permit pool (head-of-line
	// blocking). The value space is this closed three-member set plus
	// "unknown" as the coercion target for any out-of-vocabulary value a
	// future call-site mistake might pass (see
	// graphbackpressure.IsValidGateName); it must never carry a raw operation
	// or statement name.
	MetricDimensionGate           = "gate"
	MetricDimensionService        = "service"
	MetricDimensionAccount        = "account"
	MetricDimensionRegion         = "region"
	MetricDimensionMediaFamily    = "media_family"
	MetricDimensionArtifactFamily = "artifact_family"
	MetricDimensionEcosystem      = "ecosystem"
	MetricDimensionStatusClass    = "status_class"
	// MetricDimensionRoute labels per-endpoint API/MCP request metrics with the
	// matched route pattern (e.g. "GET /api/v0/iac/resources"). The value space
	// is the fixed set of registered routes, so it stays low-cardinality; raw
	// request paths with identifiers are never used.
	MetricDimensionRoute        = "route"
	MetricDimensionFailureClass = "failure_class"
	MetricDimensionFactKind     = "fact_kind"
	// MetricDimensionSourceFileKind labels content-entity emission counters with
	// a bounded classification of the originating source file:
	//   "code"             — ordinary source file parsed by the language engine
	//   "package_manifest" — dependency manifest or lockfile (go.mod, package-lock.json, Cargo.lock, etc.)
	//   "config"           — infra / config artifact (Dockerfile, Terraform, Helm, etc.)
	//   "other"            — any other artifact type returned by the parser
	//
	// This dimension lets operators distinguish a content_entity explosion caused
	// by a lockfile parser from normal code-entity growth without manual SQL.
	// Cardinality is bounded: producers MUST use the SourceFileKind* constants.
	MetricDimensionSourceFileKind = "source_file_kind"
	// MetricDimensionBootstrapPhase labels bootstrap pipeline phase duration
	// histograms with a bounded phase name from the BootstrapPhase* set.
	// Producers MUST use those constants; raw stage strings are not allowed.
	MetricDimensionBootstrapPhase = "bootstrap_phase"
	// MetricDimensionResourceScope labels Kubernetes live collection metrics
	// with a bounded resource scope such as namespaces, pods, deployments, or
	// services. It is a closed enum of resource families, never namespace or
	// object names, so it stays low-cardinality.
	MetricDimensionResourceScope = "resource_scope"
	MetricDimensionDocumentType  = "document_type"
	// MetricDimensionSafeLocatorHash labels Terraform-state metrics with the
	// scope-level safe locator hash so operators can group counters per state
	// without exposing bucket names, S3 keys, or local paths.
	MetricDimensionSafeLocatorHash = "safe_locator_hash"
	// MetricDimensionWarningKind labels Terraform-state warning metrics with
	// the warning category (state_in_vcs, state_too_large, output_value_dropped,
	// etc.) emitted by the streaming parser.
	MetricDimensionWarningKind = "warning_kind"
	// MetricDimensionPack labels correlation metrics with the rule-pack
	// identifier (e.g. "terraform_config_state_drift"). The value space is the
	// fixed set of pack names registered through FirstPartyRulePacks().
	MetricDimensionPack = "pack"
	// MetricDimensionRule labels correlation metrics with one rule name from
	// the rule pack (e.g. "match-config-against-state"). Cardinality is
	// bounded by the rule count in each pack.
	MetricDimensionRule = "rule"
	// MetricDimensionDriftKind labels drift-detection metrics with the closed
	// enum of drift kinds: added_in_state, added_in_config, attribute_drift,
	// removed_from_state, removed_from_config.
	MetricDimensionDriftKind = "drift_kind"
	// MetricDimensionDriftUnresolvedModuleReason labels
	// eshu_dp_drift_unresolved_module_calls_total with the closed enum of
	// reasons the drift loader could not resolve a Terraform module call's
	// source to a local directory under the same repo snapshot:
	// external_registry, external_git, external_archive, cross_repo_local,
	// cycle_detected, depth_exceeded, module_renamed. The string value is
	// "reason" — same short label as MetricDimensionReason but the constant
	// exists so the drift counter's semantic dimension is anchored to this contract and
	// future contributors can find every counter that uses it via
	// grep-by-constant. Referenced from
	// go/internal/storage/postgres/tfstate_drift_evidence_module_prefix.go.
	MetricDimensionDriftUnresolvedModuleReason = "reason"
	// MetricDimensionResourceType labels
	// eshu_dp_drift_schema_unknown_composite_total with the Terraform resource
	// type (e.g. "aws_s3_bucket") whose composite attribute the streaming
	// nested walker dropped or refused before capture. Cardinality is bounded
	// by the schema bundle; the high-cardinality attribute_key stays in summary
	// warning facts and structured logs per the observability rules in
	// CLAUDE.md.
	MetricDimensionResourceType = "resource_type"
	// MetricDimensionCompositeSkipReason labels
	// eshu_dp_drift_schema_unknown_composite_total with a closed enum that
	// disambiguates why the streaming nested walker dropped a composite. The
	// cases carry different operator signals:
	//   - schema_unknown: the resolver does not recognize the (resource_type,
	//     attribute_key) pair; refresh the provider-schema bundle.
	//   - shape_mismatch: the resolver recognizes the pair, but the state
	//     JSON shape disagreed with the schema and the walker bailed mid-walk;
	//     investigate the state file and the walker error.
	//   - known_sensitive_key: the redaction policy classified the top-level
	//     composite source path as sensitive before the walker started.
	//   - unknown_redaction_ruleset or unknown_field_kind: redaction policy
	//     setup was incomplete or unsafe, so the parser failed closed.
	// Cardinality is bounded by the closed enum. The closed-enum values live
	// in terraformstate.CompositeCaptureSkipReason* and producers MUST use
	// those constants.
	//
	// The wire key intentionally shares the "reason" string with
	// MetricDimensionReason and MetricDimensionDriftUnresolvedModuleReason
	// because the metricDimensionKeys() registry deduplicates on the wire
	// label; the constant exists so the composite counter's semantic
	// dimension is anchored to this contract and grep-by-constant locates
	// every counter that uses it.
	MetricDimensionCompositeSkipReason = "reason"
	// MetricDimensionRelationshipType labels the AWS relationship edge
	// projection counter (eshu_dp_aws_relationship_edges_total) with the AWS
	// relationship type (e.g. "USES_KMS_KEY", "ATTACHED_TO_VPC"). Cardinality is
	// bounded by the closed set of relationship types the scanner fleet emits.
	MetricDimensionRelationshipType = "relationship_type"
	// MetricDimensionJoinMode labels the AWS relationship edge projection counter
	// with the closed enum of target resolution modes the in-memory join index
	// uses: arn, bare_id, correlation_anchor, or unresolved when no endpoint was
	// found. It lets an operator answer "which join mode is losing edges, and is
	// it because the target service was not scanned in this scope?" at 3 AM.
	MetricDimensionJoinMode = "join_mode"
	// MetricDimensionOwnershipFamily labels the #5007 cross-scope ownership
	// contention counter (eshu_dp_cross_scope_ownership_contended_rows_total)
	// with the closed enum of graphowner node-writer families: cloud_resource,
	// ec2_instance, kubernetes_workload. It lets an operator answer "which node
	// family is losing cross-scope contention, and how often?" at 3 AM.
	MetricDimensionOwnershipFamily = "family"
	// MetricDimensionCoverageSignal labels the observability coverage correlation
	// counter (eshu_dp_observability_coverage_correlations_total) with the closed
	// enum of coverage signal classes: alarm, composite_alarm, dashboard,
	// datasource, folder, alert_rule, log_group, trace_sampling, scrape_target,
	// rule, metric_route, log_route, trace_route, log_signal, trace_signal, and
	// unsupported. Cardinality is bounded by that closed set so an operator can
	// answer "which observability signal class is losing coverage?" at 3 AM.
	MetricDimensionCoverageSignal = "coverage_signal"
	// MetricDimensionResolutionMode labels the observability coverage COVERS edge
	// projection counter (eshu_dp_observability_coverage_edges_total) with the
	// closed enum of target resolution modes the coverage index uses: arn,
	// bare_id, or correlation_anchor. It lets an operator answer "which identity
	// path proved the covered edges, and is exact coverage shrinking?" at 3 AM.
	MetricDimensionResolutionMode = "resolution_mode"
	// MetricDimensionEndpointKind labels the security-group endpoint node
	// materialization counter (eshu_dp_security_group_endpoint_nodes_total) with
	// the closed enum of network-reachability endpoint node kinds the reducer
	// materializes: cidr_block or prefix_list. Cardinality is bounded by that
	// closed set so an operator can answer "are CIDR or prefix-list endpoint nodes
	// landing, and did a generation produce zero?" at 3 AM.
	MetricDimensionEndpointKind = "endpoint_kind"
	// MetricDimensionPrincipalKind labels the IAM CAN_ASSUME edge projection
	// counter (eshu_dp_iam_can_assume_edges_total) with the closed enum of
	// resolved assuming-principal node kinds: role or user. Cardinality is
	// bounded by that closed set so an operator can answer "are role- or
	// user-assumed trust edges landing, and did a generation produce zero?" at
	// 3 AM.
	MetricDimensionPrincipalKind = "principal_kind"
	// MetricDimensionCloudFormationSection labels
	// CloudFormationPositionFallbacks (issue #5328) with the closed
	// CloudFormation section name the fallback occurred in: Parameters,
	// Conditions, Resources, or Outputs. Never a raw entity or export name.
	MetricDimensionCloudFormationSection = "cloudformation_section"
)

// Span names define the stable data-plane tracing contract.
const (
	SpanCollectorObserve     = "collector.observe"
	SpanCollectorStream      = "collector.stream"
	SpanScopeAssign          = "scope.assign"
	SpanFactEmit             = "fact.emit"
	SpanProjectorRun         = "projector.run"
	SpanReducerIntentEnqueue = "reducer_intent.enqueue"
	SpanReducerRun           = "reducer.run"
	SpanReducerBatchClaim    = "reducer.batch_claim"
	// SpanReducerEshuSearchIndexWrite wraps the reducer-owned persisted search
	// index write path for curated EshuSearchDocument projection. It covers
	// document/term retire, document/term upsert, and stats refresh work so a
	// trace can distinguish index refresh latency from the broader reducer run.
	SpanReducerEshuSearchIndexWrite = "reducer.eshu_search_index_write"
	// SpanReducerDriftEvidenceLoad wraps the PostgresDriftEvidenceLoader call
	// for one config_state_drift intent. Children spans on the InstrumentedDB
	// surface each component query (config-side terraform_resources, active
	// state-snapshot lookup, current and optional prior state-resources, and
	// the prior-config-addresses walk that activates removed_from_config).
	// Operators reading a trace can tell which of those is slow without
	// instrumenting each call site individually.
	SpanReducerDriftEvidenceLoad = "reducer.drift_evidence_load"
	// SpanReducerAWSRuntimeDriftEvidenceLoad wraps the Postgres AWS runtime
	// drift loader. Child Postgres query spans expose the AWS-resource scan,
	// bounded active-state ARN join, and config-owner lookup per state scope.
	SpanReducerAWSRuntimeDriftEvidenceLoad = "reducer.aws_runtime_drift_evidence_load"
	// SpanReducerMultiCloudRuntimeDriftEvidenceLoad wraps the Postgres
	// multi-cloud runtime drift loader (issues #1997, #1998). Child Postgres
	// query spans expose the observed provider-inventory scan, the bounded
	// active-state identity join, and the config-owner lookup per state scope so
	// a trace shows which sub-scan is slow without instrumenting each call site.
	SpanReducerMultiCloudRuntimeDriftEvidenceLoad = "reducer.multi_cloud_runtime_drift_evidence_load"
	// SpanReducerAWSRelationshipMaterialization wraps the AWS relationship edge
	// projection (issue #805): fact load, in-memory join-index build, target
	// resolution across the three join modes, and the batched MATCH-MATCH-MERGE
	// edge write. The span carries materialized vs unresolved edge counts so a
	// trace shows whether forward-looking targets degraded gracefully.
	SpanReducerAWSRelationshipMaterialization = "reducer.aws_relationship_materialization"
	// SpanReducerGCPRelationshipMaterialization wraps the GCP relationship edge
	// projection (issue #2348): fact load, in-memory join-index build keyed by
	// full resource name, support_state-aware target resolution, and the batched
	// MATCH-MATCH-MERGE GCP_<TYPE> edge write. The span carries materialized vs
	// skipped edge counts so a trace shows whether GCP relationship targets
	// degraded gracefully.
	SpanReducerGCPRelationshipMaterialization = "reducer.gcp_relationship_materialization"
	// SpanReducerObservabilityCoverageMaterialization wraps the observability
	// coverage COVERS edge projection (issue #391 PR3): fact load, classifier
	// re-run, exact-coverage edge-row extraction, and the batched
	// MATCH-MATCH-MERGE COVERS edge write. The span carries materialized vs
	// skipped edge counts so a trace shows whether derived/provenance-only
	// coverage degraded gracefully without fabricating edges.
	SpanReducerObservabilityCoverageMaterialization = "reducer.observability_coverage_materialization"
	// SpanReducerIAMCanAssumeMaterialization wraps the IAM CAN_ASSUME trust-graph
	// edge projection (issue #1134 PR2): fact load, in-memory role/user join-index
	// build, assume-principal resolution, retract, and the batched
	// MATCH-MATCH-MERGE CAN_ASSUME edge write. The span carries materialized vs
	// skipped edge counts so a trace shows whether external/service/wildcard/
	// unscanned principals degraded gracefully without fabricating edges.
	SpanReducerIAMCanAssumeMaterialization = "reducer.iam_can_assume_materialization"
	// SpanReducerKubernetesCorrelationMaterialization wraps the live-workload
	// RUNS_IMAGE edge projection (issue #388 PR3): fact load, classifier re-run plus
	// digest->uid source resolution, exact-image edge-row extraction, and the batched
	// MATCH-MATCH-MERGE RUNS_IMAGE edge write grouped by source-node label. The span
	// carries materialized vs skipped edge counts so a trace shows whether
	// provenance-only or digest-unresolvable correlation degraded gracefully without
	// fabricating or dangling edges.
	SpanReducerKubernetesCorrelationMaterialization = "reducer.kubernetes_correlation_materialization"
	// SpanReducerCrossplaneSatisfiedByMaterialization wraps the Crossplane
	// Claim -> XRD SATISFIED_BY edge projection (issue #5347): own-scope
	// content-entity fact load, cross-scope active CrossplaneXRD fact load,
	// (group, kind) resolution against exactly one XRD, retract, and the
	// batched MATCH-MATCH-MERGE SATISFIED_BY edge write. The span carries
	// materialized vs ambiguous-skipped edge counts so a trace shows whether
	// a 2+ XRD match degraded gracefully without fabricating a representative
	// edge.
	SpanReducerCrossplaneSatisfiedByMaterialization = "reducer.crossplane_satisfied_by_materialization"
	// SpanReducerS3LogsToMaterialization wraps the S3 LOGS_TO server-access-log
	// edge projection (issue #1144 PR2): fact load, in-memory S3 bucket-name
	// join-index build, source/target bucket resolution, retract, and the batched
	// MATCH-MATCH-MERGE LOGS_TO edge write. The span carries materialized vs
	// skipped edge counts so a trace shows whether cross-account or unscanned log
	// targets degraded gracefully without fabricating edges.
	SpanReducerS3LogsToMaterialization = "reducer.s3_logs_to_materialization"
	// SpanReducerRDSPostureMaterialization wraps the RDS posture node-property
	// projection (issue #1233): fact load, the cloud_resource_uid canonical-nodes
	// readiness gate, source RDS CloudResource resolution, scoped property retract,
	// and the batched MATCH+SET posture write. It lets traces distinguish
	// source-unresolved posture facts from graph backend latency without adding
	// high-cardinality metric labels.
	SpanReducerRDSPostureMaterialization = "reducer.rds_posture_materialization"
	// SpanReducerEC2UsesProfileMaterialization wraps the EC2 USES_PROFILE
	// instance-profile edge projection (issue #1146 PR-B): fact load, the dual-key
	// canonical-nodes readiness gate (the EC2 instance node phase plus the IAM
	// instance-profile node phase, published under different entity keys), in-memory
	// instance-profile ARN join-index build, source/target resolution, retract, and
	// the batched MATCH-MATCH-MERGE USES_PROFILE edge write. The span carries
	// materialized vs skipped edge counts so a trace shows whether cross-account or
	// unscanned profiles degraded gracefully without fabricating edges.
	SpanReducerEC2UsesProfileMaterialization = "reducer.ec2_uses_profile_materialization"
	// SpanReducerIAMInstanceProfileRoleMaterialization wraps the IAM
	// instance-profile HAS_ROLE edge projection (issue #1299): fact load, the
	// cloud_resource_uid canonical-nodes readiness gate, in-memory role ARN
	// join-index build, profile->role resolution, retract, and the batched
	// MATCH-MATCH-MERGE HAS_ROLE edge write. The span carries materialized vs
	// skipped edge counts so a trace shows whether cross-account or unscanned roles
	// degraded gracefully without fabricating edges.
	SpanReducerIAMInstanceProfileRoleMaterialization = "reducer.iam_instance_profile_role_materialization"
	// SpanReducerEC2InternetExposureMaterialization wraps EC2 internet-exposure
	// node-property projection (issue #1301): fact load, EC2 canonical-node
	// readiness, ENI/security-group/rule joins, conservative exposed/not_exposed/
	// unknown derivation, scoped retract, and the batched MATCH-only CloudResource
	// property write. The span makes missing reachability evidence visible without
	// treating absent evidence as safe.
	SpanReducerEC2InternetExposureMaterialization = "reducer.ec2_internet_exposure_materialization"
	// SpanReducerEC2BlockDeviceKMSPostureMaterialization wraps EC2 block-device
	// KMS posture node-property projection (issue #1304): fact load, the dual-key
	// canonical-nodes readiness gate (EC2 instance nodes plus EBS/KMS CloudResource
	// nodes), in-memory volume/KMS join-index build, conservative encrypted/
	// not_encrypted/mixed/unknown derivation, scoped retract, and the batched
	// MATCH-only EC2 CloudResource property write. The span makes missing volume
	// facts, missing KMS key facts, AWS-managed/default keys, and detached volumes
	// visible without treating absent evidence as safe.
	SpanReducerEC2BlockDeviceKMSPostureMaterialization = "reducer.ec2_block_device_kms_posture_materialization"
	// SpanReducerS3InternetExposureMaterialization wraps S3 internet-exposure
	// node-property projection (issue #1232): fact load, in-memory S3 bucket-name
	// source resolution, conservative exposed/not_exposed/unknown derivation,
	// scoped retract, and the batched MATCH-only CloudResource property write. The
	// span makes unknown/partial posture visible without treating absent evidence
	// as safe.
	SpanReducerS3InternetExposureMaterialization = "reducer.s3_internet_exposure_materialization"
	// SpanReducerSecurityGroupReachabilityMaterialization wraps the security-group
	// network-reachability edge projection (issue #1135 PR2b, Option D): fact load,
	// the triple canonical-nodes readiness gate, in-memory join-index build, SG
	// anchor + endpoint resolution, port-precise rule-node extraction, and the
	// batched MERGE of :SecurityGroupRule nodes plus the ALLOWS_INGRESS/EGRESS and
	// TO edges. The span carries materialized rule/edge counts and the skipped
	// tally so a trace shows whether rules degraded gracefully (unscanned SG or
	// endpoint) without fabricating or dangling edges.
	SpanReducerSecurityGroupReachabilityMaterialization = "reducer.security_group_reachability_materialization"
	// SpanReducerIAMEscalationMaterialization wraps the IAM privilege-escalation
	// CAN_ESCALATE_TO edge projection (issue #1134 PR3): fact load, the
	// cloud_resource_uid canonical-nodes readiness gate, in-memory ARN join-index
	// build, per-principal primitive evaluation against the curated escalation
	// catalog, conservative single-target resolution, and the batched MERGE of
	// CAN_ESCALATE_TO edges. The span carries materialized edge counts and the
	// skipped/deferred tally so a trace shows why escalation edges degraded
	// gracefully (wildcard/many target, Deny, condition, unscanned) without
	// fabricating or dangling edges.
	SpanReducerIAMEscalationMaterialization = "reducer.iam_escalation_materialization"
	// SpanReducerIAMCanPerformMaterialization wraps the IAM CAN_PERFORM
	// effective-permission edge projection (issue #1134 PR4a/PR4b reducer): fact load, the
	// cloud_resource_uid canonical-nodes readiness gate, in-memory ARN join-index
	// build, per-principal evaluation of the closed sensitive-action catalog against
	// the principal's trusted-Allow identity statements and exact resource-policy
	// grantees, conservative exact/single-glob single-target resolution requiring
	// the catalog-expected resource type, and the batched static-token MERGE of
	// CAN_PERFORM edges. The span carries materialized edge counts and the skipped
	// tally so a trace shows why CAN_PERFORM edges degraded gracefully
	// (uncatalogued, wildcard/many target, public or unscanned principal, Deny,
	// condition, NotAction/NotResource, self-loop) without fabricating or dangling
	// edges.
	SpanReducerIAMCanPerformMaterialization = "reducer.iam_can_perform_materialization"
	// SpanReducerSecretsIAMGraphProjection wraps the secrets/IAM graph projection
	// (exact read-model rows into SecretsIAM* nodes and SECRETS_IAM_* edges).
	SpanReducerSecretsIAMGraphProjection = "reducer.secrets_iam_graph_projection"
	SpanCanonicalWrite                   = "canonical.write"
	SpanCanonicalProjection              = "canonical.projection"
	SpanCanonicalRetract                 = "canonical.retract"

	SpanEvidenceDiscovery                 = "ingestion.evidence_discovery"
	SpanIaCReachabilityMaterialization    = "iac_reachability.materialize"
	SpanSQLRelationshipMaterialization    = "reducer.sql_relationship_materialization"
	SpanInheritanceMaterialization        = "reducer.inheritance_materialization"
	SpanCrossRepoResolution               = "reducer.cross_repo_resolution"
	SpanCodeImportRepoEdge                = "reducer.code_import_repo_edge"
	SpanSharedAcceptanceLookup            = "shared_acceptance.lookup"
	SpanSharedAcceptanceUpsert            = "shared_acceptance.upsert"
	SpanQueryRelationshipEvidence         = "query.relationship_evidence"
	SpanQueryEvidenceCitationPacket       = "query.evidence_citation_packet"
	SpanQueryDocumentationFindings        = "query.documentation_findings"
	SpanQueryDocumentationFacts           = "query.documentation_facts"
	SpanQueryDocumentationEvidencePacket  = "query.documentation_evidence_packet"
	SpanQueryDocumentationPacketFreshness = "query.documentation_packet_freshness"
	// SpanQuerySemanticEvidence wraps opt-in semantic documentation observation
	// and code-hint fact reads. It is separate from deterministic documentation,
	// code, and graph-truth routes so API/MCP callers can distinguish semantic
	// provenance from canonical truth in traces.
	SpanQuerySemanticEvidence = "query.semantic_evidence"
	// SpanQuerySemanticSearch wraps bounded curated search-document retrieval
	// exposed through the HTTP API and MCP transport.
	SpanQuerySemanticSearch = "query.semantic_search"
	// SpanQueryDocumentationAggregate wraps cheap-summary count and inventory
	// aggregates over reducer-owned documentation findings. Replaces the
	// page-and-iterate caller pattern for ecosystem-level questions like
	// "how many findings per status?".
	SpanQueryDocumentationAggregate   = "query.documentation_aggregate"
	SpanQueryDeadIaC                  = "query.dead_iac"
	SpanQueryIaCUnmanagedResources    = "query.iac_unmanaged_resources"
	SpanQueryIaCManagementStatus      = "query.iac_management_status"
	SpanQueryIaCManagementExplanation = "query.iac_management_explanation"
	SpanQueryIaCTerraformImportPlan   = "query.iac_terraform_import_plan"
	SpanQueryAWSRuntimeDriftFindings  = "query.aws_runtime_drift_findings"
	// SpanQueryTerraformConfigStateDriftFindings wraps the bounded active
	// Terraform config-vs-state drift finding read
	// (POST /api/v0/terraform/config-state-drift/findings, issue #5442). The
	// span carries only stable route and capability attributes; state-snapshot
	// scope, resource address, and evidence identifiers remain in the
	// authorized response and out of telemetry.
	SpanQueryTerraformConfigStateDriftFindings = "query.terraform_config_state_drift_findings"
	// SpanQueryReplatformingSelectors wraps the bounded active AWS collector
	// scope inventory read (GET /api/v0/replatforming/selectors). The span carries
	// only stable route and capability attributes; account, region, service, and
	// scope identifiers remain in the authorized response and out of telemetry.
	SpanQueryReplatformingSelectors = "query.replatforming_selectors"
	// SpanQueryReplatformingRollups wraps the bounded replatforming drift and
	// readiness rollup read (POST /api/v0/replatforming/rollups). It aggregates
	// active AWS runtime drift and IaC findings by account, environment, and
	// service over the provider-neutral source-state taxonomy. The span carries
	// only the stable http.route and eshu.capability attributes; per-resource
	// identities stay out of span and metric labels.
	SpanQueryReplatformingRollups = "query.replatforming_rollups"
	// SpanQueryReplatformingPlan wraps the service-scoped replatforming plan
	// composition (POST /api/v0/replatforming/plans). It composes a
	// provider-neutral ReplatformingPlan over reducer-owned IaC management and
	// runtime-drift evidence and carries the stable http.route and
	// eshu.capability attributes so the plan compose read is distinguishable from
	// the underlying unmanaged-resource and import-plan reads.
	SpanQueryReplatformingPlan = "query.replatforming_plan"
	// SpanQueryReplatformingOwnership wraps the bounded unmanaged-resource
	// ownership packet read (POST /api/v0/replatforming/ownership-packets). It
	// composes owner, repository, module, service, and environment candidates for
	// active AWS drift findings from reducer-owned fields. The span carries only
	// the stable http.route and eshu.capability attributes; per-resource
	// identities and candidate values stay out of span and metric labels.
	SpanQueryReplatformingOwnership = "query.replatforming_ownership"
	// SpanQueryIaCResources wraps the bounded Terraform/IaC resource list read
	// over the authoritative graph (GET /api/v0/iac/resources). It carries the
	// stable http.route and eshu.capability attributes so the IaC inventory
	// browse read is distinguishable from dead-IaC and management reads.
	SpanQueryIaCResources        = "query.iac_resources"
	SpanQueryInfraResourceSearch = "query.infra_resource_search"
	// SpanQueryInfraRelationships wraps the bounded per-entity relationship read
	// over the authoritative graph (POST /api/v0/infra/relationships, the MCP
	// analyze_infra_relationships tool). It carries the stable http.route and
	// eshu.capability attributes plus the resolved relationship_filter so an
	// operator can confirm whether a relationship_type argument narrowed the read.
	SpanQueryInfraRelationships = "query.infra_relationships"
	// SpanQueryInfraResourceAggregate wraps cheap-summary count and inventory
	// aggregates over the authoritative infrastructure graph. Replaces the
	// page-and-iterate caller pattern for ecosystem-level questions like
	// "how many resources per provider?".
	SpanQueryInfraResourceAggregate = "query.infra_resource_aggregate"
	// SpanQueryContainerImageList wraps the bounded container-image (OCI) list
	// read over the authoritative (:ContainerImage) graph that backs the
	// console Images browse surface.
	SpanQueryContainerImageList = "query.container_image_list"
	// SpanQueryContainerImageTagHistory wraps the bounded, ordered read of one
	// image_ref's captured ContainerImageTagObservation history (issue #5459):
	// what digest a tag was first observed as, and the order its digests
	// changed.
	SpanQueryContainerImageTagHistory = "query.container_image_tag_history"
	// SpanQueryCloudResourceList wraps the bounded keyset-paged list of cloud
	// provider resources (CloudResource nodes) served by GET
	// /api/v0/cloud/resources. Distinct from the aggregate span so operators can
	// separate per-row inventory browsing from rollup counts.
	SpanQueryCloudResourceList = "query.cloud_resource_list"
	// SpanQueryCloudInventoryReadback wraps the bounded, paginated readback of
	// canonical multi-cloud resource identities (reducer_cloud_resource_identity
	// rows) served by GET /api/v0/cloud/inventory. It is distinct from the
	// graph-backed cloud_resource_list span because this read resolves
	// reducer-owned canonical identity facts from Postgres rather than the
	// CloudResource graph projection. The span carries only the stable http.route
	// and eshu.capability attributes; cloud_resource_uid, raw identities,
	// account/project/subscription scopes, and provider locators stay out of span
	// and metric labels.
	SpanQueryCloudInventoryReadback = "query.cloud_inventory_readback"
	// SpanQueryCloudRuntimeDriftFindings wraps the bounded, paginated readback of
	// provider-neutral runtime drift findings (reducer_multi_cloud_runtime_drift_finding
	// rows) served by POST /api/v0/cloud/runtime-drift/findings. It is distinct
	// from the AWS-specific aws_runtime_drift_findings span because this read
	// resolves the provider-neutral drift fact keyed on canonical
	// cloud_resource_uid so AWS, GCP, and Azure findings share one surface. The
	// span carries only the stable http.route and eshu.capability attributes;
	// cloud_resource_uid, raw identities, scope ids, and provider locators stay
	// out of span and metric labels.
	SpanQueryCloudRuntimeDriftFindings   = "query.cloud_runtime_drift_findings"
	SpanQueryCodeStructuralInventory     = "query.code_structural_inventory"
	SpanQueryCodeTopicInvestigation      = "query.code_topic_investigation"
	SpanQueryDeadCodeInvestigation       = "query.dead_code_investigation"
	SpanQueryChangeSurfaceInvestigation  = "query.change_surface_investigation"
	SpanQueryEntityMap                   = "query.entity_map"
	SpanQueryResourceInvestigation       = "query.resource_investigation"
	SpanQueryPackageRegistryPackages     = "query.package_registry_packages"
	SpanQueryPackageRegistryVersions     = "query.package_registry_versions"
	SpanQueryPackageRegistryDependencies = "query.package_registry_dependencies"
	SpanQueryDependencies                = "query.dependencies"
	SpanQueryCodeownersOwnership         = "query.codeowners_ownership"
	SpanTerraformStateClaimProcess       = "tfstate.collector.claim.process"
	SpanTerraformStateDiscoveryResolve   = "tfstate.discovery.resolve"
	SpanTerraformStateSourceOpen         = "tfstate.source.open"
	SpanTerraformStateParserStream       = "tfstate.parser.stream"
	SpanTerraformStateFactEmitBatch      = "tfstate.fact.emit_batch"
	SpanTerraformStateCoordinatorDone    = "tfstate.coordinator.complete"
	SpanWebhookHandle                    = "webhook.handle"
	SpanWebhookStore                     = "webhook.store"
	SpanOCIRegistryScan                  = "oci_registry.scan"
	SpanOCIRegistryAPICall               = "oci_registry.api_call"
	SpanKubernetesLiveSnapshot           = "kubernetes_live.snapshot"
	SpanKubernetesLiveAPICall            = "kubernetes_live.api_call"
	SpanVaultLiveSnapshot                = "vault_live.snapshot"
	SpanPackageRegistryObserve           = "package_registry.observe"
	SpanPackageRegistryFetch             = "package_registry.fetch"
	SpanAWSCollectorClaimProcess         = "aws.collector.claim.process"
	SpanAWSCredentialsAssumeRole         = "aws.credentials.assume_role"
	SpanAWSServiceScan                   = "aws.service.scan"
	SpanAWSServicePaginationPage         = "aws.service.pagination.page"

	// Dependency service spans — track external call performance.
	SpanPostgresExec  = "postgres.exec"
	SpanPostgresQuery = "postgres.query"
	SpanNeo4jExecute  = "neo4j.execute"
)

// Log keys define the structured logging contract for terminal failures and
// retryable failure classification.
const (
	LogKeyScopeID                = "scope_id"
	LogKeyScopeKind              = "scope_kind"
	LogKeySourceSystem           = "source_system"
	LogKeyGenerationID           = "generation_id"
	LogKeyCollectorKind          = "collector_kind"
	LogKeyDomain                 = "domain"
	LogKeyPartitionKey           = "partition_key"
	LogKeyRequestID              = "request_id"
	LogKeyFailureClass           = "failure_class"
	LogKeyRefreshSkipped         = "refresh_skipped"
	LogKeyPipelinePhase          = "pipeline_phase"
	LogKeyAcceptanceScopeID      = "acceptance.scope_id"
	LogKeyAcceptanceUnitID       = "acceptance.unit_id"
	LogKeyAcceptanceSourceRunID  = "acceptance.source_run_id"
	LogKeyAcceptanceGenerationID = "acceptance.generation_id"
	LogKeyAcceptanceStaleCount   = "acceptance.stale_count"
	// LogKeyResourceFingerprint carries a deterministic hash of a raw cloud
	// or infrastructure resource identifier. Use it when an operator needs to
	// correlate logs without exposing ARNs, state addresses, or secret-shaped
	// resource names.
	LogKeyResourceFingerprint = "resource.fingerprint"
	// LogKeyResourceIdentityKind tells operators which identity shape produced
	// LogKeyResourceFingerprint. Closed values are emitted by
	// SafeResourceLogIdentity.
	LogKeyResourceIdentityKind = "resource.identity_kind"
	// LogKeyResourceType is a bounded resource family derived from a safe
	// prefix such as an AWS ARN resource prefix or Terraform resource type.
	LogKeyResourceType = "resource.type"

	// LogKeyDriftPriorConfigDepth is the effective prior-config-walk depth
	// bound applied when scanning prior repo-snapshot generations for
	// removed_from_config evidence. Emitted by the drift evidence loader so
	// operators can confirm the ESHU_DRIFT_PRIOR_CONFIG_DEPTH knob took effect.
	LogKeyDriftPriorConfigDepth = "depth"
	// LogKeyDriftPriorConfigAddresses is the count of unique resource addresses
	// found across all prior-config-snapshot generations within the depth window.
	// A lower-than-expected count here is the first signal that the window is
	// too narrow to catch a removal.
	LogKeyDriftPriorConfigAddresses = "prior_config_addresses"
	// LogKeyDriftStateOnlyAddresses is the count of addresses present in state
	// but absent from the current config snapshot. Includes both
	// removed_from_config candidates (promoted) and plain added_in_state
	// addresses (outside the depth window).
	LogKeyDriftStateOnlyAddresses = "state_only_addresses"
	// LogKeyDriftAddressesPromoted is the count of state-only addresses that
	// were found in the prior-config set and promoted to
	// PreviouslyDeclaredInConfig=true, enabling the classifier to emit
	// removed_from_config. The wire key intentionally matches the classifier
	// kind label so log lines and metric labels share the same terminology.
	LogKeyDriftAddressesPromoted = "addresses_promoted_to_removed_from_config"
	// LogKeyDriftMultiElementPrefix is the dot-path prefix at which a
	// multi-element repeated nested block was truncated to its first element by
	// either the state-loader flatten step
	// (storage/postgres/tfstate_drift_evidence_state_row.go) or the parser's
	// seenBlockTypes guard (parser/hcl/terraform_resource_attributes.go).
	// Operators read this to identify which allowlist entry would silently lose
	// drift signal once a multi-element entry lands. High-cardinality identifier;
	// stays in log attrs per CLAUDE.md observability rules.
	LogKeyDriftMultiElementPrefix = "multi_element.prefix"
	// LogKeyDriftMultiElementCount is the number of elements present in a
	// truncated repeated block on the state-flatten side. Always >= 2 by
	// construction; singleton repeated blocks do not truncate. A higher value
	// means more drift signal was discarded by the first-wins policy. Only the
	// state-flatten emission carries this attr — the parser walker sees
	// duplicates one-at-a-time during recursion and cannot precount cheaply.
	LogKeyDriftMultiElementCount = "multi_element.count"
	// LogKeyDriftMultiElementSource identifies which truncation site emitted the
	// log. Closed enum: "parser_walk" (HCL walkBlockAttributes seenBlockTypes
	// guard) or "state_flatten" (Postgres flattenStateAttributes first-element
	// recursion). The two sources have different attr shapes (state side has
	// count, parser side does not); the source field disambiguates them.
	LogKeyDriftMultiElementSource = "multi_element.source"
	// LogKeyDriftCompositeResourceType is the Terraform resource type carried
	// alongside eshu_dp_drift_schema_unknown_composite_total log lines so an
	// operator reading either signal sees the same dimension key. Duplicates
	// the metric label's resource_type by intent — log lines must carry
	// enough context for a triage operator to pivot without re-reading the
	// counter export.
	LogKeyDriftCompositeResourceType = "resource_type"
	// LogKeyDriftCompositeAttributeKey is the high-cardinality attribute key
	// the streaming nested walker dropped. Stays in log attrs (never metric
	// labels) because attribute keys are unbounded across provider versions;
	// operators investigating a counter spike read this key to learn which
	// nested block disagrees with the bundle.
	LogKeyDriftCompositeAttributeKey = "attribute_key"
	// LogKeyDriftCompositePath is the source-prefixed walker path
	// (resources.*.attributes.<key>) where the composite-capture skip
	// happened. Anchors the log line to the parser surface that emitted it
	// so a future second emitter (e.g., reducer-side composite reasoning)
	// can carry the same key with a different prefix.
	LogKeyDriftCompositePath = "path"
	// LogKeyDriftCompositeError carries the walker's diagnostic error string
	// (errCompositeSchemaUnknown for "bundle behind reality" or a
	// walker-internal parse error for "state shape disagreed with schema").
	// Closed enum at the parser boundary; future emitters may add new
	// classes.
	LogKeyDriftCompositeError = "error"
	// LogKeyDriftCompositeReason is the closed-enum reason emitted on the
	// same log line as the eshu_dp_drift_schema_unknown_composite_total
	// counter's `reason` label so operators reading either signal see the
	// same value. The closed-enum values live in
	// terraformstate.CompositeCaptureSkipReason*.
	LogKeyDriftCompositeReason = "reason"
)

// Bootstrap captures the minimum OpenTelemetry-first runtime settings needed
// by the Go data-plane bootstrap substrate.
type Bootstrap struct {
	ServiceName      string
	ServiceNamespace string
	MeterName        string
	TracerName       string
	LoggerName       string
}

// NewBootstrap constructs the stable telemetry bootstrap configuration for a
// service name.
func NewBootstrap(serviceName string) (Bootstrap, error) {
	serviceName = strings.TrimSpace(serviceName)
	if serviceName == "" {
		return Bootstrap{}, errors.New("service name is required")
	}

	return Bootstrap{
		ServiceName:      serviceName,
		ServiceNamespace: DefaultServiceNamespace,
		MeterName:        DefaultSignalName,
		TracerName:       DefaultSignalName,
		LoggerName:       DefaultSignalName,
	}, nil
}

// Validate checks that the bootstrap contract is fully populated.
func (b Bootstrap) Validate() error {
	if strings.TrimSpace(b.ServiceName) == "" {
		return errors.New("service name is required")
	}
	if strings.TrimSpace(b.ServiceNamespace) == "" {
		return errors.New("service namespace is required")
	}
	if strings.TrimSpace(b.MeterName) == "" {
		return errors.New("meter name is required")
	}
	if strings.TrimSpace(b.TracerName) == "" {
		return errors.New("tracer name is required")
	}
	if strings.TrimSpace(b.LoggerName) == "" {
		return errors.New("logger name is required")
	}

	return nil
}

// ResourceAttributes returns the stable resource labels for the service.
func (b Bootstrap) ResourceAttributes() map[string]string {
	return maps.Clone(map[string]string{
		"service.name":      b.ServiceName,
		"service.namespace": b.ServiceNamespace,
	})
}

// InstrumentationScopeName returns the frozen scope name for the telemetry
// package bootstrap contract.
func (b Bootstrap) InstrumentationScopeName() string {
	return InstrumentationScopeName
}
