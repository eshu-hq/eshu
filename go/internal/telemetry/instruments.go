// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// Package telemetry provides pre-registered OTEL metric instruments for the
// Go data plane.
package telemetry //nolint:filelength // data registry; ~4400 lines of frozen eshu_dp_* instrument definitions. Tracked in audit § T11 and issue eshu-hq/eshu#3761. Splitting is a separate, non-trivial work item because the contract is reviewed as a single table.

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/sourcetool"
)

// QueueObserver provides queue depth and age readings for observable gauges.
type QueueObserver interface {
	// QueueDepths returns the current depth of each queue by status.
	// Keys: queue name -> status (pending, in_flight, retrying) -> count.
	QueueDepths(ctx context.Context) (map[string]map[string]int64, error)

	// QueueOldestAge returns the age in seconds of the oldest item per queue.
	QueueOldestAge(ctx context.Context) (map[string]float64, error)
}

// SourceQueueObserver provides queue pressure grouped by bounded source system.
// Keys for SourceQueueDepths are queue name -> source_system -> status -> count.
// Keys for SourceQueueOldestAge are queue name -> source_system -> age seconds.
type SourceQueueObserver interface {
	SourceQueueDepths(ctx context.Context) (map[string]map[string]map[string]int64, error)
	SourceQueueOldestAge(ctx context.Context) (map[string]map[string]float64, error)
}

// WorkflowFamilyQueueDepthObserver provides outstanding claim-aware collector
// queue depth grouped by collector family and status, backing the per-family
// queue-depth gauge. Keys: collector_kind -> source_system -> status -> count.
// It is intentionally separate from QueueObserver so existing implementers are
// not forced to provide it.
type WorkflowFamilyQueueDepthObserver interface {
	WorkflowFamilyQueueDepths(ctx context.Context) (map[string]map[string]map[string]int64, error)
}

// WorkerObserver provides active worker counts for observable gauges.
type WorkerObserver interface {
	// ActiveWorkers returns the current active count per worker pool.
	ActiveWorkers(ctx context.Context) (map[string]int64, error)
}

// AcceptanceObserver provides shared acceptance row counts for observable
// gauges.
type AcceptanceObserver interface {
	// AcceptanceRowCount returns the number of durable shared acceptance rows.
	AcceptanceRowCount(ctx context.Context) (int64, error)
}

// GraphOrphanObserver provides bounded graph orphan counts by node label.
type GraphOrphanObserver interface {
	// GraphOrphanNodeCounts returns current zero-relationship node counts keyed
	// by a closed graph label such as Repository, Platform, or EvidenceArtifact.
	GraphOrphanNodeCounts(ctx context.Context) (map[string]int64, error)
}

// ActiveGenerationAgeObserver provides bounded active-generation counts keyed by
// a closed activation-age bucket (fresh, aging, stuck). The stuck bucket is the
// operator alarm signal that generations are wedging.
type ActiveGenerationAgeObserver interface {
	// ActiveGenerationsByAge returns current active generation counts keyed by a
	// closed age bucket. Implementations must never key by raw scope or
	// generation identifiers.
	ActiveGenerationsByAge(ctx context.Context) (map[string]int64, error)
}

// PoisonLivenessObserver provides the current dead-letter/poison class size:
// fact_work_items rows whose status is 'dead_letter' with no strictly-newer
// scope_generations row for the same scope (#4740). This is the class the
// generation-liveness ActiveGenerationAgeObserver above does not reach — a
// poison scope's newest generation is not 'active' (it is 'failed'), so it
// never appears in the fresh/aging/stuck buckets at all.
type PoisonLivenessObserver interface {
	// PoisonDeadLetterCounts returns the current poison-class scope count, item
	// count, and the oldest item's age in seconds. Implementations must never
	// key or label by raw scope, generation, or work-item identifiers.
	PoisonDeadLetterCounts(ctx context.Context) (scopes int64, items int64, oldestAgeSeconds float64, err error)
}

// AWSClaimConcurrencyObserver provides active AWS claim counts by account.
type AWSClaimConcurrencyObserver interface {
	// AWSClaimConcurrency returns active claim counts keyed by AWS account ID.
	AWSClaimConcurrency(ctx context.Context) (map[string]int64, error)
}

// EdgesBySourceToolObserver provides bounded edge counts keyed by source_tool.
// Implementations must never include raw property values that are not members
// of the sourcetool canonical vocabulary; values outside the set are coerced
// to "unknown" before the label reaches the metric.
type EdgesBySourceToolObserver interface {
	// EdgesBySourceTool returns current graph edge counts keyed by the
	// bounded source_tool label. Only edges where source_tool IS NOT NULL
	// are counted. The returned map must have a cardinality bounded by the
	// closed sourcetool.Canonical set plus "unknown".
	EdgesBySourceTool(ctx context.Context) (map[string]int64, error)
}

// FilesByLanguageObserver provides bounded File node counts keyed by language.
// Implementations must only include non-empty language values; the language
// property is written by the parser registry, which is already bounded.
type FilesByLanguageObserver interface {
	// FilesByLanguage returns current File node counts keyed by language.
	// Only File nodes where language IS NOT NULL are counted.
	FilesByLanguage(ctx context.Context) (map[string]int64, error)
}

// Instruments holds all pre-registered OTEL metric instruments for the Go
// data plane. All instruments use the eshu_dp_ prefix to differentiate from
// Python eshu_ metrics.
type Instruments struct {
	// Counters track cumulative totals
	FactsEmitted              metric.Int64Counter
	FactsCommitted            metric.Int64Counter
	ProjectionsCompleted      metric.Int64Counter
	ReducerIntentsEnqueued    metric.Int64Counter
	ReducerAdmissionDeferrals metric.Int64Counter
	ReducerExecutions         metric.Int64Counter
	// ReducerHeartbeatMissed counts reducer lease heartbeat failures, whether
	// from the immediate pre-heartbeat emitted at claim time (#4447) or a
	// later periodic tick. Labeled by domain only (a bounded closed set). A
	// non-zero rate means a worker's lease heartbeat could not reach the
	// lease store and the intent's lease may be reclaimed and re-executed by
	// another worker.
	ReducerHeartbeatMissed metric.Int64Counter
	// ProjectorRetrySurge counts every projector work-item retry scheduled
	// via ProjectorQueue.Fail's retry path (#4450), labeled by failure_class
	// only (a bounded closed set from queueFailureMetadata/deadLetterTriage
	// classification — never by scope_id or generation_id). Retries now
	// carry exponential backoff plus jitter instead of a fixed delay, so a
	// burst of same-instant failures no longer reconverges on one
	// visible_at and starves new work. An operator can graph this
	// counter's rate by failure_class to spot a genuine retry surge (many
	// items failing the same way) independently of whether the spread
	// mitigation is working — the spread itself is not directly observable
	// from a counter and is proven by the deterministic backoff/jitter unit
	// tests instead.
	ProjectorRetrySurge metric.Int64Counter
	// ReducerRetrySurge counts every reducer intent retry scheduled via
	// ReducerQueue.failIntent's retry path (#4450), labeled by failure_class
	// only (a bounded closed set — the same self-classified or fallback
	// reducer_retryable class recorded on the row, never scope_id or
	// generation_id). Mirrors ProjectorRetrySurge: reducer retries now carry
	// exponential backoff plus jitter instead of a fixed delay, so a burst
	// of same-instant failures no longer reconverges on one visible_at.
	ReducerRetrySurge metric.Int64Counter
	// WorkflowRunTerminalDeadLetterBlocks counts workflow run completeness
	// reconciliations that terminated a run (blocked/failed, not left
	// wedged in reducer_converging) because a required graph-projection
	// phase's owning reducer domain had a genuinely terminal
	// fact_work_items dead-letter (#4459). Labeled by collector_kind and
	// domain only — both bounded closed sets — never by run_id, scope_id,
	// or generation_id. An operator alerting on this counter's rate can
	// distinguish "the pipeline is draining slowly" from "this run can
	// never converge on its own."
	WorkflowRunTerminalDeadLetterBlocks metric.Int64Counter
	SearchIndexMutations                metric.Int64Counter
	SearchIndexErrors                   metric.Int64Counter
	CanonicalWrites                     metric.Int64Counter
	SharedProjectionCycles              metric.Int64Counter
	// SharedProjectionIntentsCompleted counts shared-projection intents marked
	// completed per domain. Labeled by domain only (bounded: the
	// domain set is the fixed sharedProjectionDomains list). Never keyed by
	// intent_id, scope_id, or generation_id. Lets an operator derive per-domain
	// drain rate (completed/s) and — combined with an intent-emit counter —
	// pending depth per domain without a per-scrape table scan.
	SharedProjectionIntentsCompleted metric.Int64Counter
	SharedAcceptanceUpserts          metric.Int64Counter
	SharedAcceptanceLookupErrors     metric.Int64Counter
	SharedProjectionStaleIntents     metric.Int64Counter
	// SharedProjectionPartitionHeartbeatMissed counts shared-projection
	// partition lease heartbeat failures (#4449). Labeled by domain only (a
	// bounded closed set). A non-zero rate means a slow partition cycle's
	// lease heartbeat could not reach the lease store, risking reclaim and
	// double-write by another worker while the original holder is still
	// processing.
	SharedProjectionPartitionHeartbeatMissed metric.Int64Counter
	// SharedProjectionLeaseQuarantines counts fail-closed shard pauses after
	// an ambiguous, canceled, or lease-uncertain repo-dependency cycle. Labels
	// are domain and a bounded reason set; never repository or lease owner.
	SharedProjectionLeaseQuarantines metric.Int64Counter
	GenerationRetentionPruned        metric.Int64Counter
	GenerationRetentionRowsPruned    metric.Int64Counter
	GenerationRetentionFailures      metric.Int64Counter
	GenerationRetentionSkipped       metric.Int64Counter
	GenerationLivenessRecovered      metric.Int64Counter
	GenerationLivenessSuperseded     metric.Int64Counter
	GenerationLivenessFailures       metric.Int64Counter
	// PoisonLivenessRecovered counts dead-letter/poison-class fact_work_items
	// rows re-enqueued to pending by the bounded poison-recovery sweep (#4740).
	// Only increments when the sweep's bounded auto-retry is enabled; the
	// stuck-gauge (PoisonDeadLetterScopes/Items) reports the class
	// independently of whether auto-retry is on.
	PoisonLivenessRecovered metric.Int64Counter
	// PoisonLivenessFailures counts poison-recovery sweep failures by bounded
	// reason (#4740).
	PoisonLivenessFailures                    metric.Int64Counter
	DeltaBaselineFallbacks                    metric.Int64Counter
	ReconciliationFullSnapshots               metric.Int64Counter
	ReconciliationDriftRetractions            metric.Int64Counter
	ReconciliationConvergence                 metric.Int64Counter
	DocumentationEntityMentions               metric.Int64Counter
	DocumentationClaimCandidates              metric.Int64Counter
	DocumentationClaimsSuppressed             metric.Int64Counter
	DocumentationDriftFindings                metric.Int64Counter
	TerraformStateSnapshotsObserved           metric.Int64Counter
	TerraformStateResourcesEmitted            metric.Int64Counter
	TerraformStateOutputsEmitted              metric.Int64Counter
	TerraformStateModulesEmitted              metric.Int64Counter
	TerraformStateWarningsEmitted             metric.Int64Counter
	TerraformStateRedactionsApplied           metric.Int64Counter
	TerraformStateS3ConditionalGetNotModified metric.Int64Counter
	TerraformStateDiscoveryCandidates         metric.Int64Counter
	OCIRegistryAPICalls                       metric.Int64Counter
	OCIRegistryTagsObserved                   metric.Int64Counter
	OCIRegistryManifestsObserved              metric.Int64Counter
	OCIRegistryReferrersObserved              metric.Int64Counter
	KubernetesLiveAPICalls                    metric.Int64Counter
	KubernetesLiveResourcesListed             metric.Int64Counter
	KubernetesLiveFactsEmitted                metric.Int64Counter
	KubernetesLiveWarnings                    metric.Int64Counter
	SecretsIAMSourceAPICalls                  metric.Int64Counter
	// VaultRequestTotal counts Vault API requests by operation and result.
	// result is a bounded enum: success, timeout, auth_error, not_found,
	// transport_error, or fallback. It carries no path, token, address, or
	// mount name.
	VaultRequestTotal                     metric.Int64Counter
	SecretsIAMSourceFactsEmitted          metric.Int64Counter
	SecretsIAMSourcePartialScope          metric.Int64Counter
	SecretsIAMSourceRedactions            metric.Int64Counter
	SecretsIAMSourceScopeFreshness        metric.Float64Gauge
	SecretsIAMGraphNodesWritten           metric.Int64Counter
	SecretsIAMGraphEdgesWritten           metric.Int64Counter
	SecretsIAMGraphSkipped                metric.Int64Counter
	PackageRegistryRequests               metric.Int64Counter
	PackageRegistryFactsEmitted           metric.Int64Counter
	PackageRegistryRateLimited            metric.Int64Counter
	PackageRegistryParseFailures          metric.Int64Counter
	VulnerabilityIntelligenceObservations metric.Int64Counter
	VulnerabilityIntelligenceFactsEmitted metric.Int64Counter
	VulnerabilityIntelligenceRateLimited  metric.Int64Counter
	SecurityAlertProviderRequests         metric.Int64Counter
	SecurityAlertFactsEmitted             metric.Int64Counter
	SecurityAlertRateLimited              metric.Int64Counter
	CICDRunProviderRequests               metric.Int64Counter
	CICDRunFactsEmitted                   metric.Int64Counter
	CICDRunRateLimited                    metric.Int64Counter
	CICDRunPartialGenerations             metric.Int64Counter
	PagerDutyProviderRequests             metric.Int64Counter
	PagerDutyFactsEmitted                 metric.Int64Counter
	PagerDutyRateLimited                  metric.Int64Counter
	PagerDutyConfigResourcesObserved      metric.Int64Counter
	PagerDutyConfigDriftCandidates        metric.Int64Counter
	PagerDutyConfigPartialFailures        metric.Int64Counter
	PagerDutyConfigRedactions             metric.Int64Counter
	JiraProviderRequests                  metric.Int64Counter
	JiraFactsEmitted                      metric.Int64Counter
	JiraRateLimited                       metric.Int64Counter
	GrafanaProviderRequests               metric.Int64Counter
	GrafanaFactsEmitted                   metric.Int64Counter
	GrafanaRateLimited                    metric.Int64Counter
	GrafanaRetries                        metric.Int64Counter
	GrafanaRedactions                     metric.Int64Counter
	PrometheusMimirProviderRequests       metric.Int64Counter
	PrometheusMimirFactsEmitted           metric.Int64Counter
	PrometheusMimirRateLimited            metric.Int64Counter
	PrometheusMimirRetries                metric.Int64Counter
	PrometheusMimirRedactions             metric.Int64Counter
	PrometheusMimirStale                  metric.Int64Counter
	LokiProviderRequests                  metric.Int64Counter
	LokiFactsEmitted                      metric.Int64Counter
	LokiRateLimited                       metric.Int64Counter
	LokiRetries                           metric.Int64Counter
	LokiRedactions                        metric.Int64Counter
	LokiHighCardinalityRejected           metric.Int64Counter
	LokiStale                             metric.Int64Counter
	TempoProviderRequests                 metric.Int64Counter
	TempoFactsEmitted                     metric.Int64Counter
	TempoRateLimited                      metric.Int64Counter
	TempoRetries                          metric.Int64Counter
	TempoRedactions                       metric.Int64Counter
	TempoHighCardinalityRejected          metric.Int64Counter
	TempoStale                            metric.Int64Counter
	ScannerWorkerClaims                   metric.Int64Counter
	ScannerWorkerRetries                  metric.Int64Counter
	ScannerWorkerDeadLetters              metric.Int64Counter
	ScannerWorkerFactsEmitted             metric.Int64Counter
	PackageSourceCorrelations             metric.Int64Counter
	// PackageConsumptionRepoEdges counts repo-to-repo DEPENDS_ON edge intents
	// derived from package consumption-to-owner correlation joins, labeled by
	// reducer domain and outcome (projected, skipped reason). It lets an
	// operator confirm the package-consumption projection lane is producing
	// edges and see why candidates were dropped (issue #3579).
	PackageConsumptionRepoEdges metric.Int64Counter
	// CodeImportRepoEdges counts repo-to-repo DEPENDS_ON edge outcomes derived
	// from per-file external import sources correlated to package-registry
	// ownership, labeled by reducer domain and outcome. The outcome label values
	// are exactly the strings the handler emits: "considered", "written",
	// "skipped_relative", "skipped_unresolved", "skipped_ambiguous",
	// "skipped_no_owner", "skipped_self", and "skipped_malformed_file". It lets
	// an operator confirm the code-import projection lane is producing edges and
	// see why candidate imports were dropped (issues #3642, #4749).
	CodeImportRepoEdges             metric.Int64Counter
	ContainerImageIdentityDecisions metric.Int64Counter
	// ProvenanceEdges counts canonical PUBLISHES and BUILT_FROM graph
	// provenance edges materialized (or skipped) from package-ownership,
	// package-publication, and container-image-identity correlation
	// decisions, labeled by the producing evidence_source domain and outcome
	// (materialized/skipped). See
	// docs/internal/design/5472-graph-projection-policy.md and issue #5457.
	ProvenanceEdges            metric.Int64Counter
	CICDRunCorrelations        metric.Int64Counter
	ServiceCatalogCorrelations metric.Int64Counter
	// ServiceCatalogCorrelationGuardrails counts reducer service-catalog
	// admission guardrail events by reducer domain and bounded guardrail name.
	// It stays separate from ServiceCatalogCorrelations so decision outcomes
	// remain limited to the admission decision enum.
	ServiceCatalogCorrelationGuardrails metric.Int64Counter
	// IncidentRepositoryCorrelations counts reducer-owned durable
	// incident-routing-to-repository correlation decisions by reducer domain and
	// outcome (exact, derived, ambiguous, unresolved, rejected). Only exact and
	// derived outcomes carry a durable repository edge; the counter lets an
	// operator see how often a confident tenant-safe link was found versus how
	// often the routing stayed provenance-only. Labels are bounded enums only:
	// domain and outcome. It never carries repo ids, provider service ids, or
	// backend locators.
	IncidentRepositoryCorrelations metric.Int64Counter
	// CloudInventoryAdmissions counts reducer cloud-inventory identity admission
	// records by provider and outcome (admitted, unresolved, ambiguous,
	// unsupported, skipped). Labels are bounded enums only: provider and outcome.
	// It never carries resource ids, names, project ids, subscription ids, or
	// ARNs. It is the reducer phase-count surface for the shared admission path.
	CloudInventoryAdmissions metric.Int64Counter
	// SearchDecayPolicyApplications counts decay-scoring decisions for
	// non-canonical search evidence. Labels: policy_id, evidence_class, and
	// outcome. It is ranking metadata only; it does not count or mutate
	// canonical graph truth.
	SearchDecayPolicyApplications metric.Int64Counter
	// SecretsIAMReducerTrustChains counts reducer-owned secrets/IAM
	// workload-to-identity read-model chains by result and confidence. Labels
	// stay bounded; raw IAM role ARNs, ServiceAccount names, Vault role names,
	// and Vault paths stay out of metrics.
	SecretsIAMReducerTrustChains metric.Int64Counter
	// SecretsIAMPostureObservations counts reducer-owned secrets/IAM posture
	// observations such as broad web-identity trust by bounded risk type and
	// severity. It is the 3 AM signal for why exact chains are not being
	// admitted.
	SecretsIAMPostureObservations metric.Int64Counter
	// ObservabilityCoverageCorrelations counts reducer-owned observability
	// coverage correlation decisions (issue #391). Labels: domain
	// (observability_coverage_correlation), outcome (exact / derived / ambiguous /
	// unresolved / stale / rejected / drifted / permission_hidden), and
	// coverage_signal (alarm / composite_alarm / dashboard / datasource / folder /
	// alert_rule / log_group / trace_sampling / scrape_target / rule /
	// metric_route / log_route / trace_route / log_signal / trace_signal /
	// unsupported). It lets an operator answer "which observability signal class
	// is losing coverage, and is it a gap, drift, hidden data, an ambiguous match,
	// or rejected weak signal?" at 3 AM.
	ObservabilityCoverageCorrelations metric.Int64Counter
	// KubernetesCorrelations counts reducer-owned live Kubernetes correlation
	// decisions (issue #388). Labels: domain (kubernetes_correlation), outcome
	// (exact / derived / ambiguous / unresolved / stale / rejected), and
	// drift_kind (in_sync / image_drift / missing_source / stale_source /
	// unknown). It lets an operator answer "which drift class is growing —
	// missing_source, image_drift, or stale_source — and is it an ambiguous
	// selector or a rejected weak ref?" at 3 AM.
	KubernetesCorrelations metric.Int64Counter
	// KubernetesWorkloadNodes counts canonical KubernetesWorkload graph nodes
	// committed by the live-workload node materialization reducer (issue #388).
	// Label: domain (kubernetes_workload_materialization). It lets an operator see
	// how many live-workload nodes one generation committed — the substrate the
	// later live-workload edge slice resolves against — and spot a generation that
	// produced zero nodes (every pod template lacked an object_id) at 3 AM.
	KubernetesWorkloadNodes metric.Int64Counter
	// EC2InstanceNodes counts canonical EC2 instance CloudResource graph nodes
	// committed by the EC2 instance node materialization reducer (issue #1146
	// PR-A). Label: domain (ec2_instance_node_materialization). It lets an operator
	// see how many instance nodes one generation committed — the substrate the
	// later USES_PROFILE edge slice resolves against — and spot a generation that
	// produced zero nodes (every posture fact lacked an identity) at 3 AM.
	EC2InstanceNodes metric.Int64Counter
	// EC2InstanceNodesSkipped counts ec2_instance_posture facts that produced no
	// node. Label: skip_reason (missing_identity — neither instance id nor arn was
	// present to form a uid; tombstone — a terminated instance no longer running).
	// It is the bounded, honest graceful-degradation surface: a skip is an
	// intentional no-fabrication outcome, not a silently dropped fact.
	EC2InstanceNodesSkipped metric.Int64Counter
	// KubernetesCorrelationEdges counts canonical RUNS_IMAGE edges the live-workload
	// correlation edge projection committed (issue #388 PR3). Label: resolution_mode
	// (digest — the only edge-eligible exact join). It counts only materialized
	// exact edges; provenance-only correlation (derived/ambiguous/unresolved/stale/
	// rejected) and exact decisions whose source digest resolved no canonical OCI
	// node (counted skipped in the completion log) never produce an edge. Lets an
	// operator see live-workload->image edge throughput, and a generation that
	// materialized zero edges, at 3 AM.
	KubernetesCorrelationEdges metric.Int64Counter
	// CrossplaneSatisfiedByEdges counts canonical SATISFIED_BY edges the
	// Crossplane Claim -> XRD correlation edge projection committed (issue
	// #5347). Label: resolution_mode (group_claim_kind — the sole exact join
	// this domain produces today). It counts only materialized edges; a
	// zero-match candidate (an ordinary Kubernetes object) and an ambiguous
	// 2+ XRD match (counted ambiguous_skipped in the completion log, not a
	// metric label, to keep cardinality bounded) never produce an edge. Lets
	// an operator see Claim->XRD edge throughput, and a generation that
	// materialized zero edges, at 3 AM.
	CrossplaneSatisfiedByEdges metric.Int64Counter
	// SecurityGroupEndpointNodes counts canonical CidrBlock and PrefixList graph
	// nodes committed by the security-group endpoint materialization reducer
	// (issue #1135 PR2a). Label: endpoint_kind (cidr_block / prefix_list). It lets
	// an operator see how many network-reachability endpoint nodes one generation
	// committed — the substrate the later ALLOWS_INGRESS/EGRESS edge slice resolves
	// against — and spot a generation that produced zero endpoints (every rule
	// named a referenced group, or had an unparseable CIDR) at 3 AM.
	SecurityGroupEndpointNodes metric.Int64Counter
	// SecurityGroupReachabilityRuleNodes counts canonical :SecurityGroupRule graph
	// nodes committed by the network-reachability edge projection (issue #1135
	// PR2b, Option D). No label: it is the port-precise rule-node throughput for
	// one generation. A zero count is itself a signal (every rule named an
	// unscanned group or carried an unknown source), so the counter is recorded
	// even when no nodes materialized.
	SecurityGroupReachabilityRuleNodes metric.Int64Counter
	// SecurityGroupReachabilityEdges counts canonical reachability edges committed
	// by the network-reachability edge projection. Label: edge_type (sg_rule for
	// the SecurityGroup -> SecurityGroupRule ALLOWS_INGRESS/EGRESS edge,
	// rule_endpoint for the SecurityGroupRule -[:TO]-> endpoint edge). It lets an
	// operator see reachability edge throughput per family and spot a generation
	// that committed rule nodes but zero TO edges (every endpoint unscanned).
	SecurityGroupReachabilityEdges metric.Int64Counter
	// SecurityGroupReachabilitySkipped counts security_group_rule facts that
	// produced no graph truth. Label: skip_reason (unresolved_anchor — the SG was
	// not scanned; unresolved_endpoint — a referenced group / parseable CIDR /
	// prefix list endpoint did not resolve; unknown_source — the rule reported no
	// usable source). It is the bounded, honest diagnostic surface for graceful
	// degradation: a rising unresolved_anchor rate means reachability edges are
	// missing because security groups have not been scanned, not because the
	// reducer silently dropped rules.
	SecurityGroupReachabilitySkipped metric.Int64Counter
	// IAMEscalationEdges counts canonical CAN_ESCALATE_TO privilege-escalation edges
	// committed by the IAM escalation projection (issue #1134 PR3). No label: it is
	// the escalation-edge throughput for one generation. A zero count is itself a
	// signal (no principal held a complete primitive with a single resolved target),
	// so the counter is recorded even when no edges materialized.
	IAMEscalationEdges metric.Int64Counter
	// IAMEscalationSkipped counts escalation-primitive evaluations that produced no
	// edge. Label: skip_reason (skipped_ambiguous — wildcard/many-resource target;
	// skipped_unresolved — principal or target not scanned, incl. cross-account;
	// skipped_deny — a Deny blocked a required action; skipped_conditioned — a
	// condition-gated grant could not be conservatively trusted;
	// skipped_not_action_resource — a NotAction/NotResource grant; skipped_incomplete
	// — a multi-action primitive missing an action; deferred_can_assume —
	// sts:AssumeRole deferred to the CAN_ASSUME edge). It is the bounded, honest
	// graceful-degradation surface: a rising skipped_ambiguous rate means escalation
	// edges are missing because policies use wildcard resources, not a reducer bug.
	IAMEscalationSkipped metric.Int64Counter
	// IAMCanPerformEdges counts canonical CAN_PERFORM effective-permission edges
	// committed by the IAM CAN_PERFORM projection (issue #1134 PR4a/PR4b reducer).
	// Label: resolution_mode (exact_arn — the resource ARN matched exactly one
	// scanned node; single_glob — a glob/prefix matched exactly one scanned node of
	// the catalog-expected type). Edge properties carry grant_sources so operators
	// can distinguish identity-policy, resource-policy, and both-source grants. A
	// zero count is itself a signal (no principal held a catalogued action with a
	// single resolved resource), so the counter is recorded for both modes even when
	// no edges materialized.
	IAMCanPerformEdges metric.Int64Counter
	// IAMCanPerformSkipped counts CAN_PERFORM catalog-action evaluations that
	// produced no edge. Label: skip_reason (skipped_uncatalogued_action — action not
	// in the closed catalog; skipped_ambiguous — wildcard/many-resource target;
	// skipped_unresolved — principal or resource not scanned, incl. cross-account or
	// wrong-type ARN; skipped_deny — a Deny blocked the action; skipped_conditioned
	// — a condition-gated grant could not be conservatively trusted;
	// skipped_not_action_resource — a NotAction/NotResource grant; skipped_self_loop
	// — the resolved resource is the principal's own node;
	// skipped_permission_boundary — an attached permission boundary did not allow the
	// identity-policy grant). It is the bounded, honest graceful-degradation surface:
	// a rising skipped_ambiguous rate means CAN_PERFORM edges are missing because
	// policies use wildcard resources, not a reducer bug.
	IAMCanPerformSkipped metric.Int64Counter
	// IAMCanPerformConditioned counts condition-gated CAN_PERFORM evidence by
	// bounded confidence. Label: confidence (provenance_only). The reducer never
	// promotes conditioned evidence into exact CAN_PERFORM edges because scanner
	// facts omit condition values and request context.
	IAMCanPerformConditioned   metric.Int64Counter
	SBOMAttestationAttachments metric.Int64Counter
	SupplyChainImpactFindings  metric.Int64Counter
	// SupplyChainSuppressionDecisions counts reducer suppression-state
	// outcomes per supply-chain impact finding. Labels: domain
	// (supply_chain_impact) and outcome (one of active, not_affected,
	// accepted_risk, false_positive, ignored, expired, provider_dismissed,
	// scope_mismatch). Lets operators detect drift between VEX/operator
	// suppression intent and finding identity at 3 AM without re-running the
	// reducer.
	SupplyChainSuppressionDecisions metric.Int64Counter
	// SupplyChainRemediationDecisions counts reducer-owned advisory-only
	// safe-upgrade decisions per supply-chain impact finding (issue #595).
	// Labels: domain (supply_chain_impact), outcome (confidence label
	// exact, partial, or unknown), and reason (closed enum:
	// direct_upgrade_allowed, direct_range_blocked,
	// transitive_parent_upgrade_required, no_patched_version,
	// multiple_patched_branches, package_manager_unsupported,
	// manifest_range_missing, manifest_range_malformed,
	// installed_version_missing, installed_version_malformed).
	// Operators read this to see how often Eshu can compute an exact
	// upgrade path versus how many findings need parent-dependency or
	// ecosystem support to graduate from unknown without re-running the
	// reducer.
	SupplyChainRemediationDecisions metric.Int64Counter
	ConfluenceHTTPRequests          metric.Int64Counter
	ConfluencePermissionDeniedPages metric.Int64Counter
	ConfluenceDocumentsObserved     metric.Int64Counter
	ConfluenceSectionsEmitted       metric.Int64Counter
	ConfluenceLinksEmitted          metric.Int64Counter
	ConfluenceSyncFailures          metric.Int64Counter
	AWSAPICalls                     metric.Int64Counter
	AWSThrottles                    metric.Int64Counter
	AWSAssumeRoleFailed             metric.Int64Counter
	AWSBudgetExhausted              metric.Int64Counter
	AWSCheckpointEvents             metric.Int64Counter
	AWSResourcesEmitted             metric.Int64Counter
	AWSRelationshipsEmitted         metric.Int64Counter
	AWSTagObservationsEmitted       metric.Int64Counter
	AWSFreshnessEvents              metric.Int64Counter
	AWSOrgAccessSkipped             metric.Int64Counter
	// AWSRelationshipEdges counts AWS relationship edge projection outcomes
	// (issue #805). Labels: relationship_type, join_mode (arn / bare_id /
	// correlation_anchor / unresolved). The unresolved join_mode is the bounded,
	// honest diagnostic surface for forward-looking targets that did not
	// materialize an edge because their endpoint node was not scanned in this
	// scope; resolved modes count materialized edges.
	AWSRelationshipEdges metric.Int64Counter
	// CrossScopeOwnershipContendedRows counts #5007 owner-ledger node rows a
	// graphowner Gate batch lost to a higher-order-key contributor from
	// another scope (cross-scope same-uid contention, resolved
	// deterministically by the Postgres owner ledger). Labels: family
	// (cloud_resource / ec2_instance / kubernetes_workload). Zero is the
	// common, healthy case; a sustained non-zero rate is the 3 AM signal that
	// two ingestion scopes are racing the same canonical node uid.
	CrossScopeOwnershipContendedRows metric.Int64Counter
	// ReducerInputInvalidFacts counts reducer facts quarantined during typed
	// payload decode because a required identity field was missing or null
	// (issue #4568, input_invalid). Labels: domain (the reducer domain that
	// consumed the fact), fact_kind (the malformed fact kind). Each increment is
	// a per-fact dead-letter: the fact is skipped and NOT projected, while the
	// batch's valid facts still materialize, so one malformed fact never stalls a
	// scope generation's graph. A non-zero rate signals a collector emitting a
	// fact without its emitter-guaranteed identity fields — a genuine collector
	// defect an operator can locate through the paired structured error log
	// (fact_id + field). The count is a rate signal, not exactly-once: an intent
	// retried after a post-decode failure re-quarantines the same facts and may
	// over-count, which is acceptable for an alerting rate.
	ReducerInputInvalidFacts metric.Int64Counter
	// ReducerInputInvalidFactWriteBatchSize records the row count of each
	// batched write to the durable reducer_input_invalid_facts read surface
	// (issue #4630), one observation per intent that quarantined at least one
	// fact (persistQuarantinedFacts, factschema_decode.go). No labels: the
	// batch is always one reducer intent's quarantined facts across every
	// domain, so a domain breakdown would defeat the point of batching one
	// round trip per intent. Compare against
	// ReducerInputInvalidFacts (the per-fact counter) to see the write
	// amplification ratio; a healthy deployment keeps this close to 1
	// (nearly every intent that emits ReducerInputInvalidFacts writes exactly
	// once).
	ReducerInputInvalidFactWriteBatchSize metric.Float64Histogram
	// ReducerInputInvalidFactsCommitted counts rows successfully committed to
	// reducer_input_invalid_facts (issue #4630). No labels. This is the
	// write-success counterpart to ReducerInputInvalidFacts (which counts
	// every quarantine regardless of durable-write outcome); a sustained gap
	// between the two signals a durable-write outage — the fact is still
	// safely quarantined (the batch's valid facts still project) but the
	// operator-facing durable row is missing.
	ReducerInputInvalidFactsCommitted metric.Int64Counter
	// ReducerInputInvalidFactWriteErrors counts failed batched writes to
	// reducer_input_invalid_facts (issue #4630). Labels: reason (a bounded
	// enum, currently only "write_error"). This write is best-effort and
	// never fails the owning reducer intent (persistQuarantinedFacts,
	// factschema_decode.go swallows the error after logging and counting it
	// here) — a non-zero rate is an operator signal that the durable
	// quarantine read surface is degraded, not that facts are being lost from
	// the graph (they remain correctly quarantined either way).
	ReducerInputInvalidFactWriteErrors metric.Int64Counter
	// QueryInputInvalidFactsDuration records the wall-clock duration of the
	// bounded reducer_input_invalid_facts read (issue #4630,
	// POST /api/v0/admin/input-invalid-facts/query), regardless of outcome.
	// No labels: the route is always scoped to one scope/generation, so a
	// per-domain breakdown would not add diagnostic value over the existing
	// structured request log.
	QueryInputInvalidFactsDuration metric.Float64Histogram
	// QueryInputInvalidFactsErrors counts failed reducer_input_invalid_facts
	// reads (issue #4630). Labels: reason (a bounded enum: "timeout" or
	// "store_error").
	QueryInputInvalidFactsErrors metric.Int64Counter
	// QueryK8sSelectCandidateScanTruncated counts k8s SELECTS relationship
	// builds (GET /api/v0/entities/{id}/context on a Service or Deployment
	// K8sResource) whose K8sResource candidate scan hit the
	// repositorySemanticEntityLimit ceiling and was truncated (issue #5343
	// follow-up #5367: the scan is not yet paginated). Labels: direction
	// ("outgoing" for Service->Deployment, "incoming" for Deployment->Service).
	// A non-zero rate is the 3 AM operator signal that a repo outgrew the
	// 5000-row K8sResource ceiling and some SELECTS edges may be missing from
	// the response; the response also carries relationships_complete=false
	// with reason k8s_resource_candidate_scan_truncated_at_5000 for the
	// specific request that hit it.
	QueryK8sSelectCandidateScanTruncated metric.Int64Counter
	// ProjectorInputInvalidFacts counts projector canonical-extractor facts
	// quarantined during typed payload decode because a required identity field
	// was missing or null (input_invalid). Labels: stage (the projector
	// canonical extractor that consumed the fact, e.g.
	// "oci_registry_canonical"), fact_kind (the malformed fact kind). Each
	// increment is a per-fact dead-letter: the fact is skipped and NOT
	// projected, while the batch's valid facts (OCI and non-OCI) still
	// materialize, so one malformed fact never fails the whole repository
	// generation's projection. A non-zero rate signals a collector emitting a
	// fact without its emitter-guaranteed identity fields — a genuine collector
	// defect an operator can locate through the paired structured error log
	// (fact_id + field). The count is a rate signal, not exactly-once: a
	// re-projected generation re-quarantines the same facts and may over-count,
	// which is acceptable for an alerting rate. This is the projector-side
	// counterpart to ReducerInputInvalidFacts; the two are separate instruments
	// so an operator can tell which pipeline stage quarantined a fact.
	ProjectorInputInvalidFacts metric.Int64Counter
	// GCPRelationshipEdges counts GCP relationship edge projection outcomes
	// (issue #2348). Labels: relationship_type, join_mode (full_resource_name /
	// unresolved / partial / unsupported / invalid_type / empty_type /
	// unknown_state). full_resource_name counts materialized edges; every other
	// mode is the bounded, honest diagnostic surface for a relationship that did
	// not materialize an edge.
	GCPRelationshipEdges metric.Int64Counter
	// GCPMaterializationFacts counts GCP materialization input facts by reducer
	// domain and fact kind. Labels are bounded enums: domain and fact_kind. Raw
	// scope ids, generation ids, and provider resource names stay in logs/traces.
	GCPMaterializationFacts metric.Int64Counter
	// GCPMaterializationGraphWrites counts GCP materialization graph writes by
	// reducer domain and write kind (node or edge). Labels are bounded enums:
	// domain and kind.
	GCPMaterializationGraphWrites metric.Int64Counter
	// GCPFreshnessEvents counts GCP Cloud Asset Inventory feed freshness
	// events by bounded kind and action, mirroring AWSFreshnessEvents. Labels
	// are bounded enums: kind (asset_change / asset_deleted / unknown) and
	// action (a closed intake or handoff action). Raw asset names, parent
	// scope ids, and push payload bodies must never appear in these metric
	// labels; the push payload body is dropped entirely and never reaches
	// logs, traces, or metrics.
	GCPFreshnessEvents metric.Int64Counter
	// GCPFreshnessFanOut records the number of configured scopes one GCP
	// freshness trigger resolved to (#4338). A CAI asset-change event carries
	// no content_family signal, so one trigger legitimately fans out to every
	// configured scope sharing (parent_scope_kind, parent_scope_id,
	// asset_type_family, location_bucket) regardless of content_family. This
	// histogram is the fan-out cardinality distribution an operator reads to
	// confirm the coordinator is not systematically over- or under-scanning.
	// No labels; value is the resolved scope count for one trigger.
	GCPFreshnessFanOut metric.Int64Histogram
	// ObservabilityCoverageEdges counts observability COVERS edge projection
	// outcomes (issue #391 PR3). Labels: coverage_signal (alarm / composite_alarm
	// / dashboard / log_group / trace_sampling) and resolution_mode (arn /
	// bare_id / correlation_anchor). It counts only materialized exact-coverage
	// edges; provenance-only coverage (derived/ambiguous/unresolved/stale/
	// rejected) never produces an edge and is surfaced by the
	// ObservabilityCoverageCorrelations counter and the completion log instead.
	// Lets an operator answer "which coverage signal class is materializing
	// COVERS edges, and by which identity path?" at 3 AM.
	ObservabilityCoverageEdges metric.Int64Counter
	// IncidentRoutingEvidence counts PagerDuty incident-routing graph evidence
	// projection outcomes (issue #1168). Labels: domain
	// (incident_routing_materialization), outcome (exact / drifted / ambiguous /
	// unresolved / stale / rejected / permission_hidden / missing), source
	// (declared / applied / observed / provenance), and kind (intended_routing /
	// applied_routing / live_routing / routing). It never labels incident ids,
	// service names, PagerDuty object ids, Jira keys, commits, PRs, or image refs.
	IncidentRoutingEvidence metric.Int64Counter
	// IAMCanAssumeEdges counts IAM CAN_ASSUME trust-graph edge projection
	// outcomes (issue #1134 PR2). Labels: principal_kind (role / user — the
	// resolved assuming-principal node type) and resolution_mode (arn). It counts
	// only materialized edges; external, AWS-service, wildcard, account-root, and
	// unscanned assume-principals never produce an edge and are surfaced by the
	// "iam can-assume materialization completed" completion log's skip tally
	// instead. Lets an operator answer "which assuming-principal kind is
	// materializing CAN_ASSUME edges, and did a generation produce zero?" at
	// 3 AM.
	IAMCanAssumeEdges metric.Int64Counter
	// S3LogsToEdges counts S3 LOGS_TO server-access-log edge projection outcomes
	// (issue #1144 PR2). Label: resolution_mode (name — the only resolution path,
	// bucket-name equality against the in-memory join index). It counts only
	// materialized edges; cross-account, out-of-scope, and unscanned log targets
	// never produce an edge and are surfaced by S3LogsToSkipped and the "s3
	// logs-to materialization completed" completion log instead. Lets an operator
	// answer "are LOGS_TO edges landing, and did a generation produce zero?" at
	// 3 AM.
	S3LogsToEdges metric.Int64Counter
	// S3LogsToSkipped counts s3_bucket_posture facts that named a log target but
	// produced no LOGS_TO edge. Label: skip_reason (source_unresolved — the
	// posture fact's own bucket did not scan as a node; target_unresolved — the
	// named log bucket was not scanned in this scope, e.g. a cross-account central
	// log account). It is the bounded, honest graceful-degradation surface: a
	// rising target_unresolved rate means LOGS_TO edges are missing because the
	// central log bucket has not been scanned, not because the reducer silently
	// dropped facts. A blank logging_target_bucket (logging disabled) is NOT
	// counted here — it is the normal no-edge state.
	S3LogsToSkipped metric.Int64Counter
	// EC2UsesProfileEdges counts EC2 USES_PROFILE instance-profile edge projection
	// outcomes (issue #1146 PR-B). Label: resolution_mode (arn — the only
	// resolution path, exact instance-profile ARN equality against the in-memory
	// join index). It counts only materialized edges; cross-account, out-of-scope,
	// and unscanned profiles never produce an edge and are surfaced by
	// EC2UsesProfileSkipped and the "ec2 uses-profile materialization completed"
	// completion log instead. Lets an operator answer "are USES_PROFILE edges
	// landing, and did a generation produce zero?" at 3 AM.
	EC2UsesProfileEdges metric.Int64Counter
	// EC2UsesProfileSkipped counts ec2_instance_posture facts that named an
	// instance profile but produced no USES_PROFILE edge. Label: skip_reason
	// (source_unresolved — the posture fact carried no instance identity;
	// target_unresolved — the named instance profile was not scanned in this scope,
	// e.g. a cross-account profile). It is the bounded, honest graceful-degradation
	// surface: a rising target_unresolved rate means USES_PROFILE edges are missing
	// because the profile's account has not been scanned, not because the reducer
	// silently dropped facts. A blank instance_profile_arn (no attached profile) is
	// NOT counted here — it is the normal no-edge state.
	EC2UsesProfileSkipped metric.Int64Counter
	// IAMInstanceProfileRoleEdges counts IAM instance-profile HAS_ROLE edge
	// projection outcomes (issue #1299). Label: resolution_mode (arn — the only
	// resolution path, exact role ARN equality against the in-memory join index).
	// It counts only materialized edges; cross-account, out-of-scope, and
	// unscanned roles never produce an edge and are surfaced by
	// IAMInstanceProfileRoleSkipped and the completion log instead.
	IAMInstanceProfileRoleEdges metric.Int64Counter
	// IAMInstanceProfileRoleSkipped counts profile role_arns that produced no
	// HAS_ROLE edge. Label: skip_reason (source_unresolved — the instance-profile
	// fact carried no stable profile identity; target_unresolved — the named role
	// was not scanned in this scope, e.g. a cross-account role). Profiles with no
	// roles are not counted here because they are the normal no-edge state.
	IAMInstanceProfileRoleSkipped metric.Int64Counter
	// EC2InternetExposureDecisions counts EC2 internet-exposure node-property
	// decisions derived from ec2_instance_posture plus ENI/security-group/rule
	// evidence (issue #1301). Labels: outcome (exposed / not_exposed / unknown)
	// and reason. Unknown preserves missing reachability evidence instead of
	// converting it into a safe false.
	EC2InternetExposureDecisions metric.Int64Counter
	// EC2InternetExposureSkipped counts ec2_instance_posture facts that could not
	// produce a stable EC2 CloudResource uid. Label: skip_reason
	// (missing_identity / tombstone). Missing identities are counted and logged,
	// never fabricated.
	EC2InternetExposureSkipped metric.Int64Counter
	// EC2BlockDeviceKMSPostureDecisions counts EC2 block-device KMS posture
	// decisions derived from ec2_instance_posture joined to EBS volume and KMS
	// facts (issue #1304). Labels: outcome (encrypted / not_encrypted / mixed /
	// unknown) and reason. Unknown preserves missing volume facts, missing KMS key
	// facts, AWS-managed/default keys, detached volumes, and absent evidence
	// instead of converting them into a safe encrypted value.
	EC2BlockDeviceKMSPostureDecisions metric.Int64Counter
	// EC2BlockDeviceKMSPostureSkipped counts ec2_instance_posture facts that
	// could not attach to an existing EC2 CloudResource identity for posture
	// projection. Label: skip_reason (source_unresolved / tombstone). Missing EC2
	// identity is counted and logged, never fabricated.
	EC2BlockDeviceKMSPostureSkipped metric.Int64Counter
	// S3InternetExposureDecisions counts S3 internet-exposure node-property
	// decisions derived from s3_bucket_posture (issue #1232). Labels: outcome
	// (exposed / not_exposed / unknown) and reason. Unknown preserves partial or
	// absent evidence instead of converting it into a safe false.
	S3InternetExposureDecisions metric.Int64Counter
	// S3InternetExposureSkipped counts s3_bucket_posture facts that could not
	// attach to an existing S3 CloudResource node. Label: skip_reason
	// (source_unresolved). Missing bucket nodes are counted and logged, never
	// fabricated.
	S3InternetExposureSkipped metric.Int64Counter
	// AWSScanStatusStaleFence counts AWS scan-status rejections caused by a
	// stale fencing token, labeled by service, account, region, and the
	// operation (start, observe, commit) that was rejected. Operators read
	// this to separate the orphaned-row symptom in issue #612 from credential,
	// throttle, and network-class failures. A non-zero rate after the
	// orphan-handoff SQL widening means a stale collector instance is still
	// alive after its lease was reaped and is being correctly fenced out.
	AWSScanStatusStaleFence metric.Int64Counter
	// WorkflowClaimAttemptBudgetExhausted counts retryable workflow claim
	// failures that the bounded retry guard escalated to terminal because the
	// work item AttemptCount reached ClaimedService.MaxAttempts. Labeled by
	// collector_kind and source_system so the operator can attribute runaway
	// loops to the right collector. Pair with workflow_claims state counts to
	// see that runtime backpressure is working and failed_retryable rows are
	// no longer unbounded (issue #612).
	WorkflowClaimAttemptBudgetExhausted metric.Int64Counter
	// WorkflowClaimRetries counts retryable workflow claim failures that were
	// re-queued (not yet terminal), labeled by collector_kind, source_system,
	// and failure_class so an operator can see which collector family is
	// generating retry pressure and why, without high-cardinality labels.
	// failure_class is the bounded classified failure class: a closed set for
	// built-in collectors (collect_failure, commit_failure, registry_rate_limited,
	// ...) and a validated, pattern-bounded value for extension-host collectors
	// (issue #2699).
	WorkflowClaimRetries metric.Int64Counter
	// WorkflowClaimProviderThrottles counts retryable workflow claim failures
	// classified as provider rate-limiting/throttling, labeled by
	// collector_kind, source_system, and outcome (retry_after_honored or
	// poll_backoff). It lets an operator see provider backpressure per family
	// without putting provider targets, accounts, or URLs in labels
	// (issue #2699).
	WorkflowClaimProviderThrottles metric.Int64Counter
	// WorkflowClaimLeaseAge records the observed heartbeat age (seconds since the
	// last successful heartbeat) for an active claim, labeled by collector_kind
	// and source_system. Rising lease age before lease-TTL expiry is the 3 AM
	// signal that a collector family is stalling under load (issue #2699).
	WorkflowClaimLeaseAge metric.Float64Histogram
	// GraphWriteBackpressureEngaged counts graph writes that had to block waiting
	// for an in-flight permit because the write path was at its concurrency
	// ceiling, labeled by operation and gate ("canonical" or "semantic"; issue
	// #4448 split the single shared pool into these two independent classes so
	// one write class cannot starve the other). A non-zero rate is the 3 AM
	// signal that the graph backend is slow enough that the BackpressureExecutor
	// is slowing intake instead of letting concurrent writes time out and flood
	// the dead-letter queue (issue #3560). Sustained engagement means the
	// backend needs headroom or the per-gate ceiling needs tuning; zero means
	// writes on that gate never contended for a permit.
	GraphWriteBackpressureEngaged metric.Int64Counter
	// GraphWriteBackpressureWaitDuration records, in seconds, how long a graph
	// write blocked for an in-flight permit, labeled by operation and gate
	// ("canonical" or "semantic"; issue #4448). Only writes that actually
	// waited are recorded, so the histogram count equals
	// GraphWriteBackpressureEngaged and the distribution shows how hard
	// backpressure is biting per gate. Rising p95 wait on one gate without a
	// corresponding rise on the other is the signal that class needs its own
	// ceiling raised rather than the whole pool.
	GraphWriteBackpressureWaitDuration metric.Float64Histogram
	// CorrelationRuleMatches counts rule-match outcomes recorded by
	// engine.Evaluate.Results[i].MatchCounts, labeled by pack and rule.
	// The engine populates MatchCounts for RuleKindMatch rules only
	// (correlation/engine/engine.go:50-56), keyed by rule name with
	// boundedMatchCount(MaxMatches, len(Evidence)). Handlers emit one
	// counter Add(count) per (rule, admitted candidate) pair, so
	// rate(eshu_dp_correlation_rule_matches_total[5m]) by (rule)
	// reflects match-phase activity per rule, not admission throughput.
	// Used by the drift pack (terraform_config_state_drift) in v1;
	// available for any future pack that needs match-frequency observability.
	CorrelationRuleMatches metric.Int64Counter
	// CorrelationDriftDetected counts admitted drift candidates emitted by
	// the terraform_config_state_drift correlation pack, labeled by
	// pack, rule, and drift_kind (added_in_state, added_in_config,
	// attribute_drift, removed_from_state, removed_from_config).
	//
	// The `rule` label here is always the admission-producing rule
	// (TerraformConfigStateDriftRuleAdmitDriftEvidence) by design; the drift
	// pack's match/derive/explain rules are pre-admission and post-admission
	// bookkeeping stages that do not gate emission. The pairing of the two
	// counters lets operators relate match-phase activity (CorrelationRuleMatches)
	// to admit-phase outcome volume (this counter) per pack.
	CorrelationDriftDetected metric.Int64Counter
	// CorrelationDriftIntentsEnqueued counts config_state_drift reducer intents
	// enqueued by the bootstrap-index Phase 3.5 trigger
	// (IngestionStore.EnqueueConfigStateDriftIntents). The counter advances by
	// the number of state_snapshot:* scopes with active generations at the
	// time the trigger fires — so a single bootstrap run advances it by N for
	// N active state-snapshot scopes (or by 0 when there are none, which is
	// itself a useful "trigger ran but produced zero work" signal).
	//
	// Pairing this with CorrelationDriftDetected lets operators decouple
	// enqueue health (intents reaching the queue) from admission health
	// (classifier admitted them). A drop in CorrelationDriftDetected with
	// flat CorrelationDriftIntentsEnqueued points at the classifier or the
	// loader; a drop in both points at the bootstrap trigger or the upstream
	// fact set.
	//
	// Labels: pack (frozen string "terraform_config_state_drift"), source
	// (currently always "bootstrap_index"; reserved for a future ingester
	// delta-trigger that would emit the same intent domain).
	CorrelationDriftIntentsEnqueued metric.Int64Counter
	// CorrelationOrphanDetected counts admitted AWS cloud-runtime candidates
	// where an observed cloud resource has no Terraform-state backing for the
	// same ARN. Labels: pack, rule.
	CorrelationOrphanDetected metric.Int64Counter
	// CorrelationUnmanagedDetected counts admitted AWS cloud-runtime candidates
	// where AWS and Terraform state agree on an ARN but current Terraform
	// config has no backing declaration. Labels: pack, rule.
	CorrelationUnmanagedDetected metric.Int64Counter
	// DriftUnresolvedModuleCalls counts Terraform module {} calls the drift
	// loader could not resolve to a local-filesystem callee directory under
	// the same repo snapshot. Each increment carries a `reason` label drawn
	// from the closed enum documented at
	// MetricDimensionDriftUnresolvedModuleReason: external_registry,
	// external_git, external_archive, cross_repo_local, cycle_detected,
	// depth_exceeded, module_renamed. State-side resources whose canonical
	// address would have been prefixed by the unresolved call surface as
	// added_in_state (the existing classifier fallback); module_renamed
	// increments when prior-config projection sees a prior generation module
	// prefix differ from the current generation prefix for the same callee
	// path.
	//
	// Cardinality is bounded by the seven closed-enum reasons. Pairing this
	// with CorrelationDriftDetected{drift_kind="added_in_state"} lets
	// operators distinguish "real operator-imported resource" from
	// "callee module out of scope for v1 join."
	//
	// Owned by PostgresDriftEvidenceLoader (issue #169 / ADR
	// 2026-05-11-module-aware-drift-joining). Tolerates a nil Instruments
	// handle through an interface adapter so tests can substitute a
	// stub recorder.
	DriftUnresolvedModuleCalls metric.Int64Counter
	// DriftAmbiguousOwnerWriteFailed counts failed durable writes of an
	// "ambiguous" terraform_config_state_drift finding — the
	// TerraformConfigStateDriftFindingWriter call
	// TerraformConfigStateDriftHandler.writeAmbiguousOwner issues when
	// backend-owner resolution finds more than one candidate config repo.
	// That write failure is deliberately non-fatal (Handle() still returns
	// Status=Succeeded and logs a warning — retrying an ambiguous-owner
	// rejection cannot resolve it), which makes it invisible to an operator
	// who is not grepping logs; this counter is the metric-visible signal.
	//
	// Unlike CorrelationDriftDetected (the per-address admitted-candidate
	// path, which already has metric plus reducer-retry coverage via the
	// non-ambiguous write failure returning a Handle() error), the ambiguous
	// path has no retry to recover through, so this counter is the only
	// durability signal for it. Labels: pack (frozen string
	// "terraform_config_state_drift", matching CorrelationDriftDetected and
	// CorrelationRuleMatches so operators can group all three by pack).
	DriftAmbiguousOwnerWriteFailed metric.Int64Counter
	// WebhookRequests counts public webhook requests by provider, bounded
	// outcome, and reason. Provider is one of github, gitlab, bitbucket, or
	// unknown; reason values are closed enums from the webhook listener.
	WebhookRequests metric.Int64Counter
	// WebhookTriggerDecisions counts normalized provider events that reached
	// durable trigger storage, labeled by provider, event kind, decision,
	// reason, and resulting queue status.
	WebhookTriggerDecisions metric.Int64Counter
	// WebhookStoreOperations counts durable trigger upserts attempted by the
	// webhook listener, labeled by provider, outcome, and stored status.
	WebhookStoreOperations metric.Int64Counter
	// SemanticExtractionQueueEvents counts semantic extraction queue lifecycle
	// events by bounded source/provider/profile/budget/status classes. Provider
	// profile IDs, source IDs, prompts, and provider responses must never be
	// metric labels.
	SemanticExtractionQueueEvents metric.Int64Counter
	// SemanticExtractionBudgetTokens counts semantic extraction token consumption
	// and estimates by bounded source/provider/profile/budget classes.
	SemanticExtractionBudgetTokens metric.Int64Counter
	// SemanticExtractionBudgetCostMicros counts semantic extraction cost micros
	// and estimates by bounded source/provider/profile/budget classes.
	SemanticExtractionBudgetCostMicros metric.Int64Counter

	// DriftSchemaUnknownComposite counts Terraform-state composite attributes
	// the streaming nested walker dropped because the loaded
	// ProviderSchemaResolver did not recognize the (resource_type,
	// attribute_key) pair. Each increment carries a `resource_type` label
	// (bounded by the schema bundle); the high-cardinality attribute_key
	// stays in the structured log per CLAUDE.md observability rules.
	//
	// Operators read this counter to detect provider-schema drift: real
	// state JSON shipped a nested block (or composite-typed attribute) the
	// bundled schema is behind on, and drift detection for that attribute
	// silently regresses until somebody refreshes the bundle. Paired with
	// the slog.Warn line emitted at the same call site, the counter is the
	// operator-visible signal and the log carries the diagnostic detail.
	//
	// Owned by terraformstate.compositeCaptureLoggingRecorder
	// (go/internal/collector/tfstateruntime/composite_capture_recorder.go).
	// Tolerates a nil Instruments handle so fixtures and early-bootstrap
	// paths stay operable.
	DriftSchemaUnknownComposite metric.Int64Counter

	// DependencyListErrors counts failed GET /api/v0/dependencies graph reads,
	// labeled by the bounded `direction` (forward or reverse) so an operator can
	// tell whether forward dependency or reverse dependent traversals are
	// failing. High-cardinality package anchors stay in the span, not the label.
	//
	// Owned by query.DependenciesHandler. Tolerates a nil Instruments handle so
	// the handler stays operable when telemetry is not wired (tests, local
	// lightweight profile).
	DependencyListErrors metric.Int64Counter

	// LiveActivityQueryErrors counts failed executions of the bounded
	// live-activity Postgres query backing GET /api/v0/status/operations
	// (#5137: the live operations board's in-flight work-item read model).
	//
	// Owned by postgres.LiveActivityStore. Nil-tolerant so the store stays
	// operable when telemetry is not wired.
	LiveActivityQueryErrors metric.Int64Counter

	// RepositoryFreshnessQueryErrors counts failed executions of the
	// bounded repository-freshness Postgres composite read backing GET
	// /api/v0/repositories/{id}/freshness (#5143: per-repo commit receipt
	// and build-completeness verdict).
	//
	// Owned by postgres.RepositoryFreshnessStore. Nil-tolerant so the store
	// stays operable when telemetry is not wired.
	RepositoryFreshnessQueryErrors metric.Int64Counter

	// Histograms track distributions
	CollectorObserveDuration        metric.Float64Histogram
	WorkflowClaimWaitDuration       metric.Float64Histogram
	TerraformStateClaimWaitDuration metric.Float64Histogram
	TerraformStateSnapshotBytes     metric.Int64Histogram
	TerraformStateParseDuration     metric.Float64Histogram
	OCIRegistryScanDuration         metric.Float64Histogram
	KubernetesLiveListDuration      metric.Float64Histogram
	// DependencyListDuration measures GET /api/v0/dependencies graph read
	// latency in seconds, labeled by the bounded `direction` (forward or
	// reverse). Owned by query.DependenciesHandler; nil-tolerant.
	DependencyListDuration metric.Float64Histogram
	// LiveActivityQueryDuration measures the bounded live-activity Postgres
	// query duration in seconds for GET /api/v0/status/operations (#5137).
	// Owned by postgres.LiveActivityStore; nil-tolerant.
	LiveActivityQueryDuration metric.Float64Histogram
	// RepositoryFreshnessQueryDuration measures the bounded
	// repository-freshness Postgres composite read duration in seconds for
	// GET /api/v0/repositories/{id}/freshness (#5143). Owned by
	// postgres.RepositoryFreshnessStore; nil-tolerant.
	RepositoryFreshnessQueryDuration       metric.Float64Histogram
	PackageRegistryObserveDuration         metric.Float64Histogram
	PackageRegistryGenerationLag           metric.Float64Histogram
	VulnerabilityIntelligenceFetchDuration metric.Float64Histogram
	SecurityAlertFetchDuration             metric.Float64Histogram
	CICDRunFetchDuration                   metric.Float64Histogram
	PagerDutyFetchDuration                 metric.Float64Histogram
	PagerDutyGenerationLag                 metric.Float64Histogram
	JiraFetchDuration                      metric.Float64Histogram
	GrafanaFetchDuration                   metric.Float64Histogram
	PrometheusMimirFetchDuration           metric.Float64Histogram
	LokiFetchDuration                      metric.Float64Histogram
	TempoFetchDuration                     metric.Float64Histogram
	ScannerWorkerQueueWaitDuration         metric.Float64Histogram
	ScannerWorkerScanDuration              metric.Float64Histogram
	ScannerWorkerTargetCount               metric.Int64Histogram
	ScannerWorkerResultCount               metric.Int64Histogram
	ScannerWorkerCPUSeconds                metric.Float64Histogram
	ScannerWorkerMemoryBytes               metric.Int64Histogram
	ConfluenceFetchDuration                metric.Float64Histogram
	AWSScanDuration                        metric.Float64Histogram
	ScopeAssignDuration                    metric.Float64Histogram
	FactEmitDuration                       metric.Float64Histogram
	ProjectorRunDuration                   metric.Float64Histogram
	ProjectorStageDuration                 metric.Float64Histogram
	ReducerRunDuration                     metric.Float64Histogram
	ReducerQueueWaitDuration               metric.Float64Histogram
	SearchIndexWriteDuration               metric.Float64Histogram
	// GCPMaterializationDuration measures GCP resource and relationship
	// materialization stages by reducer domain and write_phase.
	GCPMaterializationDuration metric.Float64Histogram
	// SearchVectorBuildPhaseDuration splits the search-vector build sweep
	// (#4430) into scheduling_wait, query_load, embed_build, and
	// write_upsert phases via write_phase, so the reducer-tail sweep's
	// dominant cost slice is isolable without recomputing it from logs.
	SearchVectorBuildPhaseDuration       metric.Float64Histogram
	GenerationRetentionDuration          metric.Float64Histogram
	GenerationRetentionBatchSize         metric.Int64Histogram
	GenerationRetentionOldestEligibleAge metric.Float64Histogram
	CanonicalWriteDuration               metric.Float64Histogram
	QueueClaimDuration                   metric.Float64Histogram
	PostgresQueryDuration                metric.Float64Histogram
	Neo4jQueryDuration                   metric.Float64Histogram
	// IaCResourceListDuration measures the GET /api/v0/iac/resources handler
	// duration, and IaCResourceListErrors counts handler failures. They let an
	// operator see IaC inventory read latency and error rate without parsing
	// traces. The query package records both through the global meter using the
	// same instrument names registered here so the frozen contract stays
	// authoritative.
	IaCResourceListDuration      metric.Float64Histogram
	IaCResourceListErrors        metric.Int64Counter
	CloudResourceListDuration    metric.Float64Histogram
	CloudResourceListErrors      metric.Int64Counter
	CloudResourceListScannedRows metric.Int64Histogram
	CloudResourceListPageSize    metric.Int64Histogram
	CloudResourceListTruncations metric.Int64Counter
	// APIRequestDuration measures per-endpoint HTTP handler latency for every
	// query API and MCP read route, labeled by the matched low-cardinality route
	// pattern (which already encodes the method) and response status_class. Its
	// _count series also gives per-endpoint request totals. APIRequestErrors
	// counts server-side (5xx) failures per route. Together they give operators
	// uniform latency and error-rate signal across all read endpoints, not only
	// the handful with bespoke instruments.
	APIRequestDuration metric.Float64Histogram
	APIRequestErrors   metric.Int64Counter
	// RelationshipBreakdownPermitWaitDuration measures time spent waiting for
	// one of the four handler-wide relationship source-tool breakdown permits.
	// RelationshipBreakdownQueued and RelationshipBreakdownInFlight expose the
	// current number of callers waiting for and holding those permits. All three
	// instruments are label-free because the limiter is one fixed conflict
	// domain shared by every relationship-catalog request in the process.
	RelationshipBreakdownPermitWaitDuration metric.Float64Histogram
	RelationshipBreakdownQueued             metric.Int64UpDownCounter
	RelationshipBreakdownInFlight           metric.Int64UpDownCounter
	// SearchHybridDegraded counts POST /api/v0/search/semantic requests served
	// without semantic ranking (hybrid degraded to BM25, or semantic refused) by
	// query_type and reason. Like APIRequestDuration it is recorded from the query
	// package through the global meter (see
	// go/internal/query/semantic_search_telemetry.go); it is declared here so the
	// metric is registered on the API and MCP providers and tracked by the
	// telemetry-coverage contract. Degraded search is expected in no-embedder mode.
	SearchHybridDegraded metric.Int64Counter
	OIDCLoginThrottled   metric.Int64Counter
	// MCPTransportAuthDenied counts MCP transport-level authentication
	// denials by mcp_method and reason (unauthenticated,
	// session_principal_mismatch), so an operator can see catalog-enumeration
	// or session-hijack attempts against initialize/tools/list/tools/call/
	// ping/SSE (issue #5168). Like APIRequestDuration/APIRequestErrors, it is
	// recorded from go/internal/mcp through the global meter (see
	// go/internal/mcp/transport_auth_metrics.go), not through this struct's
	// field; the field exists so the metric is registered and tracked by the
	// telemetry-coverage contract.
	MCPTransportAuthDenied metric.Int64Counter
	// GovernanceAuditAllowedEmitted, GovernanceAuditAllowedDropped, and
	// GovernanceAuditAllowedPersistFailures are the F-9 (#5170) allowed-read
	// governance-audit drop-observability triad. The mcp-server transport auth
	// middleware's resolver-success branch (go/internal/query/auth.go) emits an
	// allowed read_authorization event for every scoped-token/OIDC-bearer MCP
	// read through a governanceauditasync.AsyncAppender so the emission never
	// couples request latency to a Postgres round trip; that appender's worker
	// (go/internal/governanceauditasync/appender.go) records to these three
	// counters, not through this struct's fields directly — they are declared
	// here so the metrics are registered on the mcp-server provider and tracked
	// by the telemetry-coverage contract. Emitted counts events accepted into
	// the bounded buffer; Dropped (the 3am signal) counts events rejected
	// because the buffer was full or the appender was closed; PersistFailures
	// counts events accepted but that a durable-store Append call failed.
	GovernanceAuditAllowedEmitted         metric.Int64Counter
	GovernanceAuditAllowedDropped         metric.Int64Counter
	GovernanceAuditAllowedPersistFailures metric.Int64Counter
	// AuthSecretSealTotal and AuthSecretOpenTotal count go/internal/secretcrypto
	// Keyring.Seal/Open calls made by identity bootstrap-credential seeding
	// (epic #4962/#4963) and, later, provider-secret write/read (#4966), by
	// bounded operation and result. Producers must never attach plaintext,
	// ciphertext, or key material as attributes.
	AuthSecretSealTotal metric.Int64Counter
	AuthSecretOpenTotal metric.Int64Counter
	// AuthBootstrapCredentialGeneratedTotal counts
	// IdentitySubjectStore.GenerateBootstrapCredential outcomes by result
	// ("generated" on a true first insert, "already_provisioned" on an
	// idempotent conflict), so an operator can tell a fresh generation from a
	// restart-before-first-login no-op.
	AuthBootstrapCredentialGeneratedTotal metric.Int64Counter
	// AuthBootstrapSeedTotal counts the startup bootstrap identity seeding
	// stage's outcome by bounded outcome value (sealed_existing, seeded_env,
	// generated, skipped, error).
	AuthBootstrapSeedTotal metric.Int64Counter
	// AuthSetupWizardTotal counts first-run setup wizard (#4965) outcomes by
	// bounded step and result (claim/success, claim/denied, admin/success,
	// admin/denied, mfa/success, mfa/denied, error), so an operator can tell
	// a legitimate completion from repeated wrong-credential guesses at the
	// exposed claim step.
	AuthSetupWizardTotal metric.Int64Counter
	// AuthSignInPolicyGuardrailTotal counts every attempt to enable
	// require_sso on a tenant's sign-in policy (issue #4968, epic #4962), by
	// bounded decision value (allowed, denied_no_provider,
	// denied_no_sso_proof). Lets an operator see guardrail rejections trying
	// to lock a tenant out of local login without a proven SSO path, not just
	// successful policy changes.
	AuthSignInPolicyGuardrailTotal metric.Int64Counter
	// AuthRequireSSOLoginGateTotal counts every local password login attempt
	// evaluated against a tenant's require_sso policy (issue #4968, epic
	// #4962), by bounded decision value (not_required, allowed_admin,
	// denied_non_admin, policy_read_error_admin_allowed,
	// policy_read_error_fail_closed_non_admin). allowed_admin covers both the
	// normal login form (when require_sso is off) and the break-glass path
	// (/login?local=1 — a console-only UI hint with no server meaning): the
	// server applies the identical admin-only rule either way, so this
	// counter cannot distinguish which URL the caller used, only whether the
	// authenticated identity was an admin. The two policy_read_error_*
	// values cover a sign-in-policy read failure: admins still get in
	// (break-glass does not depend on a readable policy), non-admins are
	// denied (a non-admin's session depends on confirming require_sso is
	// off, which an unreadable policy cannot do).
	AuthRequireSSOLoginGateTotal       metric.Int64Counter
	SharedAcceptanceUpsertDuration     metric.Float64Histogram
	SharedAcceptanceLookupDuration     metric.Float64Histogram
	SharedAcceptancePrefetchSize       metric.Int64Histogram
	SharedProjectionIntentWaitDuration metric.Float64Histogram
	SharedProjectionProcessingDuration metric.Float64Histogram
	SharedProjectionStepDuration       metric.Float64Histogram
	// SharedProjectionPartitionProcessingDuration records per-(domain,
	// partition_id) wall time for one ProcessPartitionOnce call (lease claim +
	// selection + retract + write + mark_completed). Bounded dims: domain
	// is the fixed domain set; partition_id is 0-based ≤ ESHU_SHARED_PROJECTION_PARTITION_COUNT.
	// This is the primary long-pole signal for #3624: an operator reads which
	// (domain, partition) pair dominates cycle latency without a full corpus run.
	SharedProjectionPartitionProcessingDuration metric.Float64Histogram
	DocumentationDriftGenerationDuration        metric.Float64Histogram
	WebhookRequestDuration                      metric.Float64Histogram
	WebhookStoreDuration                        metric.Float64Histogram

	// Collector concurrency histograms and counters
	RepoSnapshotDuration           metric.Float64Histogram
	CollectorSnapshotStageDuration metric.Float64Histogram
	FileParseDuration              metric.Float64Histogram
	FilePreScanDuration            metric.Float64Histogram
	ReposSnapshotted               metric.Int64Counter
	FilesParsed                    metric.Int64Counter
	SCIPSnapshotAttempts           metric.Int64Counter
	SCIPProcessWaitDuration        metric.Float64Histogram

	// Streaming fact production metrics
	FactBatchesCommitted metric.Int64Counter
	GenerationFactCount  metric.Float64Histogram
	ContentReReads       metric.Int64Counter
	ContentReReadSkips   metric.Int64Counter

	// Discovery skip counters — per-name breakdown of what discovery prunes
	DiscoveryDirsSkipped  metric.Int64Counter
	DiscoveryFilesSkipped metric.Int64Counter
	// RepositoryBasenameCollision counts discovered repository paths whose
	// basename (the last path segment) matches at least one other discovered
	// path in the same collector cycle. Each colliding path beyond the first
	// increments the counter by one, so the total equals the number of surplus
	// (non-first) occurrences of a given basename across all discovered roots.
	//
	// Basename is a cheap, label-free signal for LIKELY accidental corpus
	// nesting (e.g. repos/repos/… recursive copies — issue #3677); it is NOT a
	// true repository identity. Distinct repositories can share a basename
	// (org-a/utils and org-b/utils, or monorepo common/ directories), so a
	// non-zero value warrants inspecting the logged paths before concluding
	// duplication. Labels are intentionally absent: paths and basenames are
	// unbounded and must never appear as metric labels. Use the accompanying
	// structured warning log (key "repository basename collision detected
	// (possible accidental corpus nesting)") to see the colliding basename, the
	// surplus count, and a bounded sample of paths.
	//
	// Owned by collector.reportRepositoryBasenameCollisions (issue #3677).
	RepositoryBasenameCollision metric.Int64Counter

	// Size-tiered scheduling metrics
	LargeRepoClassifications metric.Int64Counter
	LargeRepoSemaphoreWait   metric.Float64Histogram

	// Reducer batch claim metric
	BatchClaimSize metric.Int64Histogram

	// Neo4j batch write metrics
	Neo4jBatchSize        metric.Float64Histogram
	Neo4jBatchesExecuted  metric.Int64Counter
	SharedEdgeWriteGroups metric.Int64Counter
	// SharedEdgeRunsOnRetractOmissions counts source-capability decisions that
	// omit an impossible shared-edge RUNS_ON retract. Labels are limited to the
	// bounded projection domain and reason enums; evidence source and repository
	// identity stay in the accompanying structured log.
	SharedEdgeRunsOnRetractOmissions   metric.Int64Counter
	SharedEdgeWriteGroupDuration       metric.Float64Histogram
	SharedEdgeWriteGroupStatementCount metric.Int64Histogram
	CodeCallEdgeBatches                metric.Int64Counter
	CodeCallEdgeDuration               metric.Float64Histogram

	// Canonical projection metrics
	CanonicalNodesWritten       metric.Int64Counter
	CanonicalEdgesWritten       metric.Int64Counter
	CanonicalProjectionDuration metric.Float64Histogram
	CanonicalRetractDuration    metric.Float64Histogram
	CanonicalBatchSize          metric.Float64Histogram
	CanonicalPhaseDuration      metric.Float64Histogram

	// Canonical atomic write metrics
	CanonicalAtomicWrites    metric.Int64Counter
	CanonicalAtomicFallbacks metric.Int64Counter

	// Neo4j transient error retry metrics
	Neo4jDeadlockRetries metric.Int64Counter

	// Evidence discovery metrics (during ingestion)
	EvidenceFactsDiscovered metric.Int64Counter

	// Ingestion-TX lock split (issue #4451, § T8)
	IngestionSharedLockHoldDuration metric.Float64Histogram

	// Deferred bootstrap backfill and reopen metrics
	DeferredBackfillDuration               metric.Float64Histogram
	DeferredBackfillBatchDuration          metric.Float64Histogram
	DeferredBackfillBatchesCompleted       metric.Int64Counter
	DeferredBackfillEvidence               metric.Int64Counter
	DeferredBackfillPartitions             metric.Int64Counter
	DeferredBackfillPartitionWorkers       metric.Int64Histogram
	DeferredBackfillPartitionLoadDuration  metric.Float64Histogram
	DeploymentMappingReopened              metric.Int64Counter
	CodeImportRepoEdgeReopened             metric.Int64Counter
	CorrelationReopened                    metric.Int64Counter
	IaCReachabilityMaterializationDuration metric.Float64Histogram
	IaCReachabilityRows                    metric.Int64Counter

	// Deferred backfill partition memo gate metrics (issue #3624 Track 1 / B').
	// DeferredBackfillPartitionsSkipped counts partitions the memo gate skipped
	// (memo hit under an unchanged catalog fingerprint, not ArgoCD-bearing).
	// DeferredBackfillPartitionsLoaded counts partitions that loaded, labeled
	// reason=memo_miss (no memo row, or the catalog fingerprint changed).
	// ArgoCD-bearing partitions are excluded from the memo on the write side, so
	// they surface here as memo_miss reloads rather than a distinct label.
	// Together they let an operator watch the memo gate's skip rate rise once the
	// corpus reaches steady state.
	DeferredBackfillPartitionsSkipped metric.Int64Counter
	DeferredBackfillPartitionsLoaded  metric.Int64Counter

	// ReopenSkippedByPartitionMemo counts succeeded deployment_mapping and
	// code_import_repo_edge reducer work items whose replay was skipped because
	// their (scope_id, generation_id) partition already committed backward
	// evidence under the CURRENT catalog fingerprint (issue #4770 / #3624
	// Track 2). Labeled by domain (deployment_mapping, code_import_repo_edge)
	// and reason=catalog_unchanged. A rising skip count with a stable catalog is
	// the operator-visible steady-state signal that the reopen gate is
	// eliminating redundant reducer replay, mirroring
	// DeferredBackfillPartitionsSkipped for the fact-load side of the same memo.
	ReopenSkippedByPartitionMemo metric.Int64Counter

	// Cross-repo resolution metrics
	CrossRepoResolutionDuration metric.Float64Histogram
	CrossRepoEvidenceLoaded     metric.Int64Counter
	CrossRepoEdgesResolved      metric.Int64Counter
	// CrossRepoActivationFenced counts generations whose publish (generation
	// activation) was withheld because the durable graph-acceptance intents
	// failed to commit, leaving the generation un-published so no stranded
	// denormalized edges are stranded at the source.
	CrossRepoActivationFenced metric.Int64Counter

	// FluxCrossRepoURLResolution counts every Flux GitRepository spec.url
	// discoverStructuredFluxEvidence considered during evidence discovery
	// (go/internal/storage/postgres/ingestion.go), labeled by outcome (linked,
	// unresolved, ambiguous, self; issue #5483 C2). An operator reads a
	// sustained unresolved/ambiguous rate as a signal that cross-repo Flux
	// lineage is under-linking, without inspecting graph state directly.
	FluxCrossRepoURLResolution metric.Int64Counter

	// RepoDependencyGateDecisions counts per-key gate decisions emitted from
	// GateAcceptedGenerationOnActive, labeled by the bounded decision enum
	// bypassed, deferred_inactive, deferred_error, active. It increments once per
	// intent key resolved, never per prefetch batch. Operators read this to
	// distinguish scope-gen bypass (bypassed, healthy) from transient deferral
	// (deferred_inactive, expected until activation), lookup failures
	// (deferred_error, fail-safe), and successful pass-through (active).
	// Sustained deferred_inactive signals a generation that's accepted but not
	// yet active — the alertable wedge signal the B-13 post-mortem identified.
	// Labels are bounded enums only: decision. Never carries generation ids,
	// scope ids, or repository ids. Counter uses context.Background() because
	// AcceptedGenerationLookup closures lack a context parameter.
	RepoDependencyGateDecisions metric.Int64Counter

	// ContentEntityEmitted counts content_entity facts streamed during
	// collection, broken down by source_file_kind (code, package_manifest,
	// config, other). This is the single most valuable counter for surfacing
	// lockfile/minified entity explosions without manual SQL.
	ContentEntityEmitted metric.Int64Counter

	// BootstrapPipelinePhaseDuration records the wall time of each named
	// bootstrap pipeline phase (collection, projection, relationship_backfill,
	// iac_reachability, config_state_drift, content_index_finalization) so
	// operators can see the long pole in a full-corpus run from the metrics port
	// without strace.
	BootstrapPipelinePhaseDuration metric.Float64Histogram

	// WorkflowClaimRunDuration records the wall time of one claimed-service
	// processing cycle (ClaimedService.processClaimed) in seconds, labeled by
	// collector_kind, source_system, and outcome. It is the per-collector
	// long-pole signal: sum by (collector_kind) of run duration / count gives
	// mean run time per family. Outcome values are the bounded
	// CollectorRunOutcome* constants (success, unchanged, released,
	// fail_retryable, fail_terminal) so claim-state tables do not need to be
	// joined to attribute cost. Duration is recorded on every return path
	// (including failures) via defer so no cycle is silently dropped.
	// Concurrency-safe: metric.Float64Histogram.Record is safe for concurrent
	// callers; timing is call-local (time.Now diff on the stack frame).
	WorkflowClaimRunDuration metric.Float64Histogram

	// WorkflowClaimFactsEmitted counts facts committed per claimed-service run,
	// labeled by collector_kind and source_system. It uses
	// CollectedGeneration.FactCount (already populated at the seam) so no extra
	// scan or IO is introduced. Only recorded on success — unchanged, released,
	// and failed runs contribute zero facts but are not counted here (they are
	// visible via the outcome label on WorkflowClaimRunDuration). Join with
	// eshu_dp_bootstrap_pipeline_phase_seconds on collector_kind to attribute
	// volume to a pipeline phase.
	WorkflowClaimFactsEmitted metric.Int64Counter

	// Pipeline overlap metric — how long collector and projector ran concurrently
	PipelineOverlapDuration metric.Float64Histogram

	// Observable gauges for autoscaling signals
	QueueDepth             metric.Int64ObservableGauge
	QueueOldestAge         metric.Float64ObservableGauge
	SourceQueueDepth       metric.Int64ObservableGauge
	SourceQueueOldestAge   metric.Float64ObservableGauge
	WorkerPoolActive       metric.Int64ObservableGauge
	SharedAcceptanceRows   metric.Int64ObservableGauge
	GraphOrphanNodes       metric.Int64ObservableGauge
	AWSClaimConcurrency    metric.Int64ObservableGauge
	ActiveGenerationsByAge metric.Int64ObservableGauge
	// PoisonDeadLetterScopes and PoisonDeadLetterItems report the current
	// dead-letter/poison class size (#4740): fact_work_items rows whose status
	// is 'dead_letter' with no strictly-newer scope_generations row for the same
	// scope. A non-zero, non-draining value is the alarm signal that a scope has
	// permanently wedged and cannot self-heal without an operator or the bounded
	// poison-recovery arm.
	PoisonDeadLetterScopes metric.Int64ObservableGauge
	PoisonDeadLetterItems  metric.Int64ObservableGauge
	// PoisonDeadLetterOldestAgeSeconds reports the age, in seconds, of the
	// oldest poison-class item's updated_at (when it was dead-lettered). Zero
	// when the class is empty.
	PoisonDeadLetterOldestAgeSeconds metric.Float64ObservableGauge
	// WorkflowFamilyQueueDepth reports outstanding claim-aware collector queue
	// depth by collector_kind, source_system, and status (issue #2699/#2857).
	WorkflowFamilyQueueDepth metric.Int64ObservableGauge
	// EdgesBySourceTool reports bounded graph edge counts by source_tool. The
	// only metric label is source_tool; values not in the sourcetool.Canonical
	// set are coerced to "unknown" before the label reaches the metric (#3997).
	EdgesBySourceTool metric.Int64ObservableGauge
	// FilesByLanguage reports bounded File node counts by language. The only
	// metric label is language; values are bounded by the parser registry
	// (#4003).
	FilesByLanguage metric.Int64ObservableGauge
	// APIShutdownDuration records the graceful shutdown duration of the API HTTP
	// server. Recorded from the shutdown goroutine once per process exit. Labeled
	// by result (success, error, timeout) to let operators distinguish clean
	// shutdowns from forced terminations.
	APIShutdownDuration metric.Float64Histogram
	// StatusStageCountsCacheTotal counts status-query stage-counts reads
	// (activeFactWorkItemsCTE via stageCountsQuery, issue #4446) by cache
	// outcome. Labeled by outcome: hit (served from the in-memory TTL cache,
	// no Postgres round trip), miss (cache cold or expired, Postgres query ran
	// and succeeded), or error (Postgres query ran and failed; never cached).
	// Lets an operator read the cache hit-rate for the status stage-counts
	// read at 3 AM.
	StatusStageCountsCacheTotal metric.Int64Counter
	// OIDCBearerValidationTotal counts every IdP bearer-token (Authorization:
	// Bearer <access_token>) validation outcome the internal/oidcbearer
	// resolver reaches (issue #5162, epic #5161), by bounded outcome value:
	// valid, expired, wrong_audience, unknown_issuer, bad_signature,
	// malformed, jwks_fetch_failure, or no_grants. A credential that is not
	// JWT-shaped, or a deployment with zero enabled bearer IdPs, never
	// increments this counter at all — those paths fall through to the rest
	// of the scoped-token resolver chain instead of being "denied" (see
	// oidcbearer.Resolver.ResolveScopedToken's doc comment). Never labeled
	// with the raw token, issuer, or subject — those go to the paired
	// structured log only.
	OIDCBearerValidationTotal metric.Int64Counter
	// CloudFormationPositionFallbacks counts every degraded-position row a
	// CloudFormation adapter records when it cannot attribute a real
	// per-entity line_number/end_line to a Parameters/Conditions/Resources/
	// Outputs entity and falls back to the section header line or the
	// document-root line instead. Both adapters feed it: the YAML
	// gopkg.in/yaml.v3 Node.Line walk (issue #5328) and the JSON ordered-entry
	// walk (issue #5348). Labeled with the bounded cloudformation_section
	// (Parameters, Conditions, Resources, Outputs) and skip_reason the fallback
	// occurred under — YAML: unresolved_section_mapping, entity_position_missing,
	// root_node_unavailable, root_not_mapping; JSON: ordered_walk_failed,
	// section_entry_missing, unresolved_section_object, entity_position_missing.
	// Both adapters measure real per-entity positions on the happy path; a JSON
	// fallback is unreachable in practice because stdjson.Unmarshal already
	// accepted the same bytes the ordered walk re-reads.
	CloudFormationPositionFallbacks metric.Int64Counter
	// IdentityCacheHitTotal counts identity-fact cache hits (#5438).
	IdentityCacheHitTotal metric.Int64Counter
	// IdentityCacheMissTotal counts identity-fact cache misses (epoch changed → reload) (#5438).
	IdentityCacheMissTotal metric.Int64Counter
	// IdentityCacheReloadTotal counts identity-fact cache reloads (singleflight leader) (#5438).
	IdentityCacheReloadTotal metric.Int64Counter
	// IdentityCachePassthroughTotal counts identity-fact passthroughs (cap exceeded or mid-load commit) (#5438).
	IdentityCachePassthroughTotal metric.Int64Counter
	// IdentityCacheReloadDuration records the duration of identity-fact cache reloads (#5438).
	IdentityCacheReloadDuration metric.Float64Histogram
	// IdentityCacheProbeDuration records the duration of identity-fact epoch probe queries (#5438).
	IdentityCacheProbeDuration metric.Float64Histogram
}

// NewInstruments creates and registers all OTEL metric instruments using the
// provided meter. Returns an error if the meter is nil or if any instrument
// registration fails.
func NewInstruments(meter metric.Meter) (*Instruments, error) {
	if meter == nil {
		return nil, errors.New("meter is required")
	}

	inst := &Instruments{}
	var err error

	// Register counters
	inst.FactsEmitted, err = meter.Int64Counter(
		"eshu_dp_facts_emitted_total",
		metric.WithDescription("Total facts emitted by collector"),
	)
	if err != nil {
		return nil, fmt.Errorf("register FactsEmitted counter: %w", err)
	}

	inst.FactsCommitted, err = meter.Int64Counter(
		"eshu_dp_facts_committed_total",
		metric.WithDescription("Total facts committed to store"),
	)
	if err != nil {
		return nil, fmt.Errorf("register FactsCommitted counter: %w", err)
	}

	inst.ProjectionsCompleted, err = meter.Int64Counter(
		"eshu_dp_projections_completed_total",
		metric.WithDescription("Total projection cycles completed"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ProjectionsCompleted counter: %w", err)
	}

	inst.ReducerIntentsEnqueued, err = meter.Int64Counter(
		"eshu_dp_reducer_intents_enqueued_total",
		metric.WithDescription("Total reducer intents enqueued"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerIntentsEnqueued counter: %w", err)
	}

	inst.ReducerAdmissionDeferrals, err = meter.Int64Counter(
		"eshu_dp_reducer_admission_deferrals_total",
		metric.WithDescription("Total reducer intent enqueue admission deferrals"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerAdmissionDeferrals counter: %w", err)
	}

	inst.ReducerExecutions, err = meter.Int64Counter(
		"eshu_dp_reducer_executions_total",
		metric.WithDescription("Total reducer intent executions"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerExecutions counter: %w", err)
	}

	inst.ReducerHeartbeatMissed, err = meter.Int64Counter(
		"eshu_dp_reducer_heartbeat_missed_total",
		metric.WithDescription("Total reducer lease heartbeat failures, including the immediate pre-heartbeat emitted at claim time"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerHeartbeatMissed counter: %w", err)
	}

	inst.ProjectorRetrySurge, err = meter.Int64Counter(
		"eshu_dp_projector_retry_surge_total",
		metric.WithDescription("Total projector work-item retries scheduled with exponential backoff and jitter, labeled by failure_class (#4450)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ProjectorRetrySurge counter: %w", err)
	}

	inst.ReducerRetrySurge, err = meter.Int64Counter(
		"eshu_dp_reducer_retry_surge_total",
		metric.WithDescription("Total reducer intent retries scheduled with exponential backoff and jitter, labeled by failure_class (#4450)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerRetrySurge counter: %w", err)
	}

	inst.WorkflowRunTerminalDeadLetterBlocks, err = meter.Int64Counter(
		"eshu_dp_workflow_run_terminal_dead_letter_blocks_total",
		metric.WithDescription("Total workflow run completeness reconciliations that terminated a run because a required phase's owning reducer domain had a terminal fact_work_items dead-letter, labeled by collector_kind and domain (#4459)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register WorkflowRunTerminalDeadLetterBlocks counter: %w", err)
	}

	inst.SearchIndexMutations, err = meter.Int64Counter(
		"eshu_dp_search_index_mutations_total",
		metric.WithDescription("Total persisted search index document and term mutations by reducer domain, kind, operation, and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SearchIndexMutations counter: %w", err)
	}

	inst.SearchIndexErrors, err = meter.Int64Counter(
		"eshu_dp_search_index_errors_total",
		metric.WithDescription("Total persisted search index write failures by reducer domain and operation"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SearchIndexErrors counter: %w", err)
	}

	inst.CanonicalWrites, err = meter.Int64Counter(
		"eshu_dp_canonical_writes_total",
		metric.WithDescription("Total canonical graph write batches"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalWrites counter: %w", err)
	}

	inst.SharedProjectionCycles, err = meter.Int64Counter(
		"eshu_dp_shared_projection_cycles_total",
		metric.WithDescription("Total shared projection partition cycles"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedProjectionCycles counter: %w", err)
	}

	inst.SharedProjectionIntentsCompleted, err = meter.Int64Counter(
		"eshu_dp_shared_projection_intents_completed_total",
		metric.WithDescription("Total shared-projection intents marked completed, labeled by domain (bounded domain set only). Combine with intent-emit counters to derive per-domain pending depth without a per-scrape table scan."),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedProjectionIntentsCompleted counter: %w", err)
	}

	inst.SharedAcceptanceUpserts, err = meter.Int64Counter(
		"eshu_dp_shared_acceptance_upserts_total",
		metric.WithDescription("Total shared acceptance upserts"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedAcceptanceUpserts counter: %w", err)
	}

	inst.SharedAcceptanceLookupErrors, err = meter.Int64Counter(
		"eshu_dp_shared_acceptance_lookup_errors_total",
		metric.WithDescription("Total shared acceptance lookup errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedAcceptanceLookupErrors counter: %w", err)
	}

	inst.SharedProjectionStaleIntents, err = meter.Int64Counter(
		"eshu_dp_shared_projection_stale_intents_total",
		metric.WithDescription("Total stale shared projection intents filtered"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedProjectionStaleIntents counter: %w", err)
	}

	inst.SharedProjectionPartitionHeartbeatMissed, err = meter.Int64Counter(
		"eshu_dp_shared_projection_partition_heartbeat_missed_total",
		metric.WithDescription("Total shared projection partition lease heartbeat failures"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedProjectionPartitionHeartbeatMissed counter: %w", err)
	}

	inst.SharedProjectionLeaseQuarantines, err = meter.Int64Counter(
		"eshu_dp_shared_projection_lease_quarantines_total",
		metric.WithDescription("Total fail-closed shared-projection lease quarantines by domain and bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedProjectionLeaseQuarantines counter: %w", err)
	}

	inst.GenerationRetentionPruned, err = meter.Int64Counter(
		"eshu_dp_generation_retention_generations_pruned_total",
		metric.WithDescription("Total superseded scope generations pruned by retention policy"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationRetentionPruned counter: %w", err)
	}

	inst.GenerationRetentionRowsPruned, err = meter.Int64Counter(
		"eshu_dp_generation_retention_rows_pruned_total",
		metric.WithDescription("Total rows pruned by generation retention policy, labeled by bounded table name"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationRetentionRowsPruned counter: %w", err)
	}

	inst.GenerationRetentionFailures, err = meter.Int64Counter(
		"eshu_dp_generation_retention_failures_total",
		metric.WithDescription("Total generation retention pruning failures by bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationRetentionFailures counter: %w", err)
	}

	inst.GenerationRetentionSkipped, err = meter.Int64Counter(
		"eshu_dp_generation_retention_skipped_total",
		metric.WithDescription("Total generation retention candidates skipped by bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationRetentionSkipped counter: %w", err)
	}

	inst.GenerationLivenessRecovered, err = meter.Int64Counter(
		"eshu_dp_generation_liveness_recovered_total",
		metric.WithDescription("Total wedged active generations re-driven through projector re-enqueue by the liveness sweep"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationLivenessRecovered counter: %w", err)
	}

	inst.GenerationLivenessSuperseded, err = meter.Int64Counter(
		"eshu_dp_generation_liveness_superseded_total",
		metric.WithDescription("Total orphaned older active generations superseded by the liveness sweep"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationLivenessSuperseded counter: %w", err)
	}

	inst.GenerationLivenessFailures, err = meter.Int64Counter(
		"eshu_dp_generation_liveness_failures_total",
		metric.WithDescription("Total generation liveness recovery sweep failures by bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationLivenessFailures counter: %w", err)
	}

	inst.PoisonLivenessRecovered, err = meter.Int64Counter(
		"eshu_dp_poison_liveness_recovered_total",
		metric.WithDescription("Total dead-letter/poison-class fact_work_items rows re-enqueued to pending by the bounded poison-recovery sweep"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PoisonLivenessRecovered counter: %w", err)
	}

	inst.PoisonLivenessFailures, err = meter.Int64Counter(
		"eshu_dp_poison_liveness_failures_total",
		metric.WithDescription("Total poison dead-letter recovery sweep failures by bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PoisonLivenessFailures counter: %w", err)
	}

	inst.DeltaBaselineFallbacks, err = meter.Int64Counter(
		"eshu_dp_collector_delta_baseline_fallback_total",
		metric.WithDescription("Total git delta syncs that fell back to a full snapshot, by bounded skip_reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeltaBaselineFallbacks counter: %w", err)
	}

	inst.ReconciliationFullSnapshots, err = meter.Int64Counter(
		"eshu_dp_collector_reconciliation_full_snapshots_total",
		metric.WithDescription("Total git scopes forced to a full reconciliation snapshot to retract delta-path drift"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReconciliationFullSnapshots counter: %w", err)
	}

	inst.ReconciliationDriftRetractions, err = meter.Int64Counter(
		"eshu_dp_reconciliation_drift_retractions_total",
		metric.WithDescription("Total graph nodes and edges retracted by forced reconciliation snapshots, by bounded domain, write phase, and kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReconciliationDriftRetractions counter: %w", err)
	}

	inst.ReconciliationConvergence, err = meter.Int64Counter(
		"eshu_dp_reconciliation_convergence_total",
		metric.WithDescription("Total denormalized graph edges classified by a dual-write reconciliation pass, by domain and drift_kind (in_sync / stale_generation / orphan_resolved_id); non-in_sync values are stranded edges being converged back to Postgres truth"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReconciliationConvergence counter: %w", err)
	}

	inst.DocumentationEntityMentions, err = meter.Int64Counter(
		"eshu_dp_documentation_entity_mentions_extracted_total",
		metric.WithDescription("Total documentation entity mentions extracted by resolution outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DocumentationEntityMentions counter: %w", err)
	}

	inst.DocumentationClaimCandidates, err = meter.Int64Counter(
		"eshu_dp_documentation_claim_candidates_extracted_total",
		metric.WithDescription("Total documentation claim candidates extracted by outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DocumentationClaimCandidates counter: %w", err)
	}

	inst.DocumentationClaimsSuppressed, err = meter.Int64Counter(
		"eshu_dp_documentation_claim_candidates_suppressed_total",
		metric.WithDescription("Total documentation claim candidates suppressed before exact finding emission"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DocumentationClaimsSuppressed counter: %w", err)
	}

	inst.DocumentationDriftFindings, err = meter.Int64Counter(
		"eshu_dp_documentation_drift_findings_total",
		metric.WithDescription("Total documentation drift findings generated by outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DocumentationDriftFindings counter: %w", err)
	}

	inst.TerraformStateSnapshotsObserved, err = meter.Int64Counter(
		"eshu_dp_tfstate_snapshots_observed_total",
		metric.WithDescription("Total Terraform state snapshot observations by backend kind and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateSnapshotsObserved counter: %w", err)
	}

	inst.TerraformStateResourcesEmitted, err = meter.Int64Counter(
		"eshu_dp_tfstate_resources_emitted_total",
		metric.WithDescription("Total Terraform state resource facts emitted"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateResourcesEmitted counter: %w", err)
	}

	inst.TerraformStateOutputsEmitted, err = meter.Int64Counter(
		"eshu_dp_tfstate_outputs_emitted_total",
		metric.WithDescription("Total Terraform state output facts emitted, labeled by safe locator hash"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateOutputsEmitted counter: %w", err)
	}

	inst.TerraformStateModulesEmitted, err = meter.Int64Counter(
		"eshu_dp_tfstate_modules_emitted_total",
		metric.WithDescription("Total Terraform state module observation facts emitted, labeled by safe locator hash"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateModulesEmitted counter: %w", err)
	}

	inst.TerraformStateWarningsEmitted, err = meter.Int64Counter(
		"eshu_dp_tfstate_warnings_emitted_total",
		metric.WithDescription("Total Terraform state warning facts emitted, labeled by warning kind and safe locator hash"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateWarningsEmitted counter: %w", err)
	}

	inst.TerraformStateRedactionsApplied, err = meter.Int64Counter(
		"eshu_dp_tfstate_redactions_applied_total",
		metric.WithDescription("Total Terraform state value redactions by reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateRedactionsApplied counter: %w", err)
	}

	inst.TerraformStateS3ConditionalGetNotModified, err = meter.Int64Counter(
		"eshu_dp_tfstate_s3_conditional_get_not_modified_total",
		metric.WithDescription("Total Terraform state S3 conditional reads that returned not modified"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateS3ConditionalGetNotModified counter: %w", err)
	}

	inst.TerraformStateDiscoveryCandidates, err = meter.Int64Counter(
		"eshu_dp_tfstate_discovery_candidates_total",
		metric.WithDescription("Total Terraform state discovery candidates resolved by source"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateDiscoveryCandidates counter: %w", err)
	}

	inst.OCIRegistryAPICalls, err = meter.Int64Counter(
		"eshu_dp_oci_registry_api_calls_total",
		metric.WithDescription("Total OCI registry API calls by provider, operation, and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register OCIRegistryAPICalls counter: %w", err)
	}

	inst.OCIRegistryTagsObserved, err = meter.Int64Counter(
		"eshu_dp_oci_registry_tags_observed_total",
		metric.WithDescription("Total OCI registry tags observed by provider and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register OCIRegistryTagsObserved counter: %w", err)
	}

	inst.OCIRegistryManifestsObserved, err = meter.Int64Counter(
		"eshu_dp_oci_registry_manifests_observed_total",
		metric.WithDescription("Total OCI registry manifests observed by provider and media family"),
	)
	if err != nil {
		return nil, fmt.Errorf("register OCIRegistryManifestsObserved counter: %w", err)
	}

	inst.OCIRegistryReferrersObserved, err = meter.Int64Counter(
		"eshu_dp_oci_registry_referrers_observed_total",
		metric.WithDescription("Total OCI registry referrers observed by provider and artifact family"),
	)
	if err != nil {
		return nil, fmt.Errorf("register OCIRegistryReferrersObserved counter: %w", err)
	}

	inst.KubernetesLiveAPICalls, err = meter.Int64Counter(
		"eshu_dp_kubernetes_api_calls_total",
		metric.WithDescription("Total Kubernetes live API calls by operation and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register KubernetesLiveAPICalls counter: %w", err)
	}

	inst.KubernetesLiveResourcesListed, err = meter.Int64Counter(
		"eshu_dp_kubernetes_resources_listed_total",
		metric.WithDescription("Total Kubernetes live resources listed by resource scope and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register KubernetesLiveResourcesListed counter: %w", err)
	}

	inst.KubernetesLiveFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_kubernetes_facts_emitted_total",
		metric.WithDescription("Total Kubernetes live facts emitted by fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register KubernetesLiveFactsEmitted counter: %w", err)
	}

	inst.KubernetesLiveWarnings, err = meter.Int64Counter(
		"eshu_dp_kubernetes_warnings_total",
		metric.WithDescription("Total Kubernetes live warnings emitted by reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register KubernetesLiveWarnings counter: %w", err)
	}

	inst.SecretsIAMSourceAPICalls, err = meter.Int64Counter(
		"eshu_dp_secrets_iam_source_api_calls_total",
		metric.WithDescription("Total secrets/IAM source-collector API calls by source, operation, and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMSourceAPICalls counter: %w", err)
	}

	inst.VaultRequestTotal, err = meter.Int64Counter(
		"eshu_dp_vault_request_total",
		metric.WithDescription("Total Vault API requests by operation and bounded result (success, timeout, auth_error, not_found, transport_error, fallback)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register VaultRequestTotal counter: %w", err)
	}

	inst.SecretsIAMSourceFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_secrets_iam_source_facts_emitted_total",
		metric.WithDescription("Total secrets/IAM source facts emitted by source and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMSourceFactsEmitted counter: %w", err)
	}

	inst.SecretsIAMSourcePartialScope, err = meter.Int64Counter(
		"eshu_dp_secrets_iam_partial_scope_total",
		metric.WithDescription("Total secrets/IAM source families with partial coverage by source and reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMSourcePartialScope counter: %w", err)
	}

	inst.SecretsIAMSourceRedactions, err = meter.Int64Counter(
		"eshu_dp_secrets_iam_source_redactions_total",
		metric.WithDescription("Total secrets/IAM source-collector redactions applied by source and bounded field class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMSourceRedactions counter: %w", err)
	}

	inst.SecretsIAMSourceScopeFreshness, err = meter.Float64Gauge(
		"eshu_dp_secrets_iam_source_scope_freshness_seconds",
		metric.WithDescription("Age in seconds of the latest secrets/IAM source generation at finalization, by source and scope kind"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMSourceScopeFreshness gauge: %w", err)
	}

	inst.SecretsIAMGraphNodesWritten, err = meter.Int64Counter(
		"eshu_dp_secrets_iam_graph_nodes_written_total",
		metric.WithDescription("Total secrets/IAM graph projection nodes written by node type"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMGraphNodesWritten counter: %w", err)
	}

	inst.SecretsIAMGraphEdgesWritten, err = meter.Int64Counter(
		"eshu_dp_secrets_iam_graph_edges_written_total",
		metric.WithDescription("Total secrets/IAM graph projection edges written by edge type"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMGraphEdgesWritten counter: %w", err)
	}

	inst.SecretsIAMGraphSkipped, err = meter.Int64Counter(
		"eshu_dp_secrets_iam_graph_skipped_total",
		metric.WithDescription("Total secrets/IAM graph projection rows skipped by skip reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMGraphSkipped counter: %w", err)
	}

	inst.PackageRegistryRequests, err = meter.Int64Counter(
		"eshu_dp_package_registry_requests_total",
		metric.WithDescription("Total package registry metadata requests by ecosystem and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PackageRegistryRequests counter: %w", err)
	}

	inst.PackageRegistryFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_package_registry_facts_emitted_total",
		metric.WithDescription("Total package registry facts emitted by ecosystem and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PackageRegistryFactsEmitted counter: %w", err)
	}

	inst.PackageRegistryRateLimited, err = meter.Int64Counter(
		"eshu_dp_package_registry_rate_limited_total",
		metric.WithDescription("Total package registry metadata requests that were rate limited by ecosystem"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PackageRegistryRateLimited counter: %w", err)
	}

	inst.PackageRegistryParseFailures, err = meter.Int64Counter(
		"eshu_dp_package_registry_parse_failures_total",
		metric.WithDescription("Total package registry metadata parse failures by ecosystem and document type"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PackageRegistryParseFailures counter: %w", err)
	}

	inst.VulnerabilityIntelligenceObservations, err = meter.Int64Counter(
		"eshu_dp_vulnerability_intelligence_observations_total",
		metric.WithDescription("Total vulnerability source target observations by source and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register VulnerabilityIntelligenceObservations counter: %w", err)
	}

	inst.VulnerabilityIntelligenceFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_vulnerability_intelligence_facts_emitted_total",
		metric.WithDescription("Total vulnerability intelligence facts emitted by source and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register VulnerabilityIntelligenceFactsEmitted counter: %w", err)
	}

	inst.VulnerabilityIntelligenceRateLimited, err = meter.Int64Counter(
		"eshu_dp_vulnerability_intelligence_rate_limited_total",
		metric.WithDescription("Total vulnerability source observations that ended rate limited by source"),
	)
	if err != nil {
		return nil, fmt.Errorf("register VulnerabilityIntelligenceRateLimited counter: %w", err)
	}

	inst.SecurityAlertProviderRequests, err = meter.Int64Counter(
		"eshu_dp_security_alert_provider_requests_total",
		metric.WithDescription("Total provider security-alert requests by provider and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecurityAlertProviderRequests counter: %w", err)
	}

	inst.SecurityAlertFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_security_alert_facts_emitted_total",
		metric.WithDescription("Total provider security-alert source facts emitted by provider and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecurityAlertFactsEmitted counter: %w", err)
	}

	inst.SecurityAlertRateLimited, err = meter.Int64Counter(
		"eshu_dp_security_alert_rate_limited_total",
		metric.WithDescription("Total provider security-alert requests that ended rate limited by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecurityAlertRateLimited counter: %w", err)
	}

	inst.CICDRunProviderRequests, err = meter.Int64Counter(
		"eshu_dp_ci_cd_run_provider_requests_total",
		metric.WithDescription("Total CI/CD run provider requests by provider and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CICDRunProviderRequests counter: %w", err)
	}

	inst.CICDRunFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_ci_cd_run_facts_emitted_total",
		metric.WithDescription("Total CI/CD run source facts emitted by provider and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CICDRunFactsEmitted counter: %w", err)
	}

	inst.CICDRunRateLimited, err = meter.Int64Counter(
		"eshu_dp_ci_cd_run_rate_limited_total",
		metric.WithDescription("Total CI/CD run provider requests that ended rate limited by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CICDRunRateLimited counter: %w", err)
	}

	inst.CICDRunPartialGenerations, err = meter.Int64Counter(
		"eshu_dp_ci_cd_run_partial_generations_total",
		metric.WithDescription("Total CI/CD run generations with bounded partial provider evidence by provider and reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CICDRunPartialGenerations counter: %w", err)
	}

	inst.PagerDutyProviderRequests, err = meter.Int64Counter(
		"eshu_dp_pagerduty_provider_requests_total",
		metric.WithDescription("Total PagerDuty provider requests by provider and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PagerDutyProviderRequests counter: %w", err)
	}

	inst.PagerDutyFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_pagerduty_facts_emitted_total",
		metric.WithDescription("Total PagerDuty incident and routing source facts emitted by provider and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PagerDutyFactsEmitted counter: %w", err)
	}

	inst.PagerDutyRateLimited, err = meter.Int64Counter(
		"eshu_dp_pagerduty_rate_limited_total",
		metric.WithDescription("Total PagerDuty provider requests that ended rate limited by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PagerDutyRateLimited counter: %w", err)
	}

	inst.PagerDutyConfigResourcesObserved, err = meter.Int64Counter(
		"eshu_dp_pagerduty_config_resources_observed_total",
		metric.WithDescription("Total live PagerDuty configuration resources observed by provider and resource type"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PagerDutyConfigResourcesObserved counter: %w", err)
	}

	inst.PagerDutyConfigDriftCandidates, err = meter.Int64Counter(
		"eshu_dp_pagerduty_config_drift_candidates_total",
		metric.WithDescription("Total live PagerDuty configuration drift candidates by provider and bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PagerDutyConfigDriftCandidates counter: %w", err)
	}

	inst.PagerDutyConfigPartialFailures, err = meter.Int64Counter(
		"eshu_dp_pagerduty_config_partial_failures_total",
		metric.WithDescription("Total partial live PagerDuty configuration failures by provider and bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PagerDutyConfigPartialFailures counter: %w", err)
	}

	inst.PagerDutyConfigRedactions, err = meter.Int64Counter(
		"eshu_dp_pagerduty_config_redactions_total",
		metric.WithDescription("Total live PagerDuty configuration redactions by provider and bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PagerDutyConfigRedactions counter: %w", err)
	}

	inst.JiraProviderRequests, err = meter.Int64Counter(
		"eshu_dp_jira_provider_requests_total",
		metric.WithDescription("Total Jira provider requests by provider and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register JiraProviderRequests counter: %w", err)
	}

	inst.JiraFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_jira_facts_emitted_total",
		metric.WithDescription("Total Jira work-item source facts emitted by provider and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register JiraFactsEmitted counter: %w", err)
	}

	inst.JiraRateLimited, err = meter.Int64Counter(
		"eshu_dp_jira_rate_limited_total",
		metric.WithDescription("Total Jira provider requests that ended rate limited by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register JiraRateLimited counter: %w", err)
	}

	inst.GrafanaProviderRequests, err = meter.Int64Counter(
		"eshu_dp_grafana_provider_requests_total",
		metric.WithDescription("Total Grafana provider requests by provider and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GrafanaProviderRequests counter: %w", err)
	}

	inst.GrafanaFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_grafana_facts_emitted_total",
		metric.WithDescription("Total live Grafana observability source facts emitted by provider and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GrafanaFactsEmitted counter: %w", err)
	}

	inst.GrafanaRateLimited, err = meter.Int64Counter(
		"eshu_dp_grafana_rate_limited_total",
		metric.WithDescription("Total Grafana provider requests that ended rate limited by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GrafanaRateLimited counter: %w", err)
	}

	inst.GrafanaRetries, err = meter.Int64Counter(
		"eshu_dp_grafana_retries_total",
		metric.WithDescription("Total Grafana provider retry attempts by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GrafanaRetries counter: %w", err)
	}

	inst.GrafanaRedactions, err = meter.Int64Counter(
		"eshu_dp_grafana_redactions_total",
		metric.WithDescription("Total live Grafana metadata redactions by provider and bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GrafanaRedactions counter: %w", err)
	}

	inst.PrometheusMimirProviderRequests, err = meter.Int64Counter(
		"eshu_dp_prometheus_mimir_provider_requests_total",
		metric.WithDescription("Total Prometheus/Mimir provider requests by provider and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PrometheusMimirProviderRequests counter: %w", err)
	}

	inst.PrometheusMimirFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_prometheus_mimir_facts_emitted_total",
		metric.WithDescription("Total live Prometheus/Mimir observability source facts emitted by provider and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PrometheusMimirFactsEmitted counter: %w", err)
	}

	inst.PrometheusMimirRateLimited, err = meter.Int64Counter(
		"eshu_dp_prometheus_mimir_rate_limited_total",
		metric.WithDescription("Total Prometheus/Mimir provider requests that ended rate limited by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PrometheusMimirRateLimited counter: %w", err)
	}

	inst.PrometheusMimirRetries, err = meter.Int64Counter(
		"eshu_dp_prometheus_mimir_retries_total",
		metric.WithDescription("Total Prometheus/Mimir provider retry attempts by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PrometheusMimirRetries counter: %w", err)
	}

	inst.PrometheusMimirRedactions, err = meter.Int64Counter(
		"eshu_dp_prometheus_mimir_redactions_total",
		metric.WithDescription("Total live Prometheus/Mimir metadata redactions by provider and bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PrometheusMimirRedactions counter: %w", err)
	}

	inst.PrometheusMimirStale, err = meter.Int64Counter(
		"eshu_dp_prometheus_mimir_stale_total",
		metric.WithDescription("Total live Prometheus/Mimir stale observations by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PrometheusMimirStale counter: %w", err)
	}

	inst.LokiProviderRequests, err = meter.Int64Counter(
		"eshu_dp_loki_provider_requests_total",
		metric.WithDescription("Total Loki provider requests by provider and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register LokiProviderRequests counter: %w", err)
	}

	inst.LokiFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_loki_facts_emitted_total",
		metric.WithDescription("Total live Loki observability source facts emitted by provider and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register LokiFactsEmitted counter: %w", err)
	}

	inst.LokiRateLimited, err = meter.Int64Counter(
		"eshu_dp_loki_rate_limited_total",
		metric.WithDescription("Total Loki provider requests that ended rate limited by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register LokiRateLimited counter: %w", err)
	}

	inst.LokiRetries, err = meter.Int64Counter(
		"eshu_dp_loki_retries_total",
		metric.WithDescription("Total Loki provider retry attempts by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register LokiRetries counter: %w", err)
	}

	inst.LokiRedactions, err = meter.Int64Counter(
		"eshu_dp_loki_redactions_total",
		metric.WithDescription("Total live Loki metadata redactions by provider and bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register LokiRedactions counter: %w", err)
	}

	inst.LokiHighCardinalityRejected, err = meter.Int64Counter(
		"eshu_dp_loki_high_cardinality_rejected_total",
		metric.WithDescription("Total live Loki label values rejected as high cardinality by provider and bounded reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register LokiHighCardinalityRejected counter: %w", err)
	}

	inst.LokiStale, err = meter.Int64Counter(
		"eshu_dp_loki_stale_total",
		metric.WithDescription("Total live Loki stale observations by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register LokiStale counter: %w", err)
	}

	inst.TempoProviderRequests, err = meter.Int64Counter(
		"eshu_dp_tempo_provider_requests_total",
		metric.WithDescription("Total Tempo provider metadata requests by provider and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TempoProviderRequests counter: %w", err)
	}

	inst.TempoFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_tempo_facts_emitted_total",
		metric.WithDescription("Total Tempo observability source facts emitted by provider and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TempoFactsEmitted counter: %w", err)
	}

	inst.TempoRateLimited, err = meter.Int64Counter(
		"eshu_dp_tempo_rate_limited_total",
		metric.WithDescription("Total Tempo provider metadata requests that ended rate limited by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TempoRateLimited counter: %w", err)
	}

	inst.TempoRetries, err = meter.Int64Counter(
		"eshu_dp_tempo_retries_total",
		metric.WithDescription("Total Tempo provider metadata request retries by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TempoRetries counter: %w", err)
	}

	inst.TempoRedactions, err = meter.Int64Counter(
		"eshu_dp_tempo_redactions_total",
		metric.WithDescription("Total Tempo metadata redactions by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TempoRedactions counter: %w", err)
	}

	inst.TempoHighCardinalityRejected, err = meter.Int64Counter(
		"eshu_dp_tempo_high_cardinality_rejected_total",
		metric.WithDescription("Total Tempo high-cardinality tag-value reads rejected by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TempoHighCardinalityRejected counter: %w", err)
	}

	inst.TempoStale, err = meter.Int64Counter(
		"eshu_dp_tempo_stale_total",
		metric.WithDescription("Total Tempo metadata observations classified stale by provider"),
	)
	if err != nil {
		return nil, fmt.Errorf("register TempoStale counter: %w", err)
	}

	inst.ScannerWorkerClaims, err = meter.Int64Counter(
		"eshu_dp_scanner_worker_claims_total",
		metric.WithDescription("Total scanner-worker workflow claims by analyzer, target kind, and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerClaims counter: %w", err)
	}

	inst.ScannerWorkerRetries, err = meter.Int64Counter(
		"eshu_dp_scanner_worker_retries_total",
		metric.WithDescription("Total scanner-worker retryable failures by analyzer, target kind, and failure class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerRetries counter: %w", err)
	}

	inst.ScannerWorkerDeadLetters, err = meter.Int64Counter(
		"eshu_dp_scanner_worker_dead_letters_total",
		metric.WithDescription("Total scanner-worker terminal failures by analyzer, target kind, and failure class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerDeadLetters counter: %w", err)
	}

	inst.ScannerWorkerFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_scanner_worker_facts_emitted_total",
		metric.WithDescription("Total scanner-worker source facts emitted by analyzer, target kind, and fact kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerFactsEmitted counter: %w", err)
	}

	inst.PackageSourceCorrelations, err = meter.Int64Counter(
		"eshu_dp_package_source_correlations_total",
		metric.WithDescription("Total package source-correlation decisions by reducer domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PackageSourceCorrelations counter: %w", err)
	}

	inst.PackageConsumptionRepoEdges, err = meter.Int64Counter(
		"eshu_dp_package_consumption_repo_edges_total",
		metric.WithDescription("Total repo-to-repo DEPENDS_ON edge intents derived from package consumption-to-owner correlations by reducer domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register PackageConsumptionRepoEdges counter: %w", err)
	}

	inst.CodeImportRepoEdges, err = meter.Int64Counter(
		"eshu_dp_code_import_repo_edges_total",
		metric.WithDescription("Total repo-to-repo DEPENDS_ON edge outcomes derived from per-file external import sources correlated to package-registry ownership by reducer domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CodeImportRepoEdges counter: %w", err)
	}

	inst.ContainerImageIdentityDecisions, err = meter.Int64Counter(
		"eshu_dp_container_image_identity_decisions_total",
		metric.WithDescription("Total container image identity decisions by reducer domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ContainerImageIdentityDecisions counter: %w", err)
	}

	inst.ProvenanceEdges, err = meter.Int64Counter(
		"eshu_dp_provenance_edges_total",
		metric.WithDescription("Total canonical PUBLISHES/BUILT_FROM graph provenance edges materialized by evidence_source domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ProvenanceEdges counter: %w", err)
	}

	inst.CICDRunCorrelations, err = meter.Int64Counter(
		"eshu_dp_ci_cd_run_correlations_total",
		metric.WithDescription("Total CI/CD run correlation decisions by reducer domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CICDRunCorrelations counter: %w", err)
	}

	inst.ServiceCatalogCorrelations, err = meter.Int64Counter(
		"eshu_dp_service_catalog_correlations_total",
		metric.WithDescription("Total service catalog correlation decisions by reducer domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ServiceCatalogCorrelations counter: %w", err)
	}

	inst.ServiceCatalogCorrelationGuardrails, err = meter.Int64Counter(
		"eshu_dp_service_catalog_correlation_guardrails_total",
		metric.WithDescription("Total service catalog correlation guardrail events by reducer domain and guardrail"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ServiceCatalogCorrelationGuardrails counter: %w", err)
	}

	inst.IncidentRepositoryCorrelations, err = meter.Int64Counter(
		"eshu_dp_incident_repository_correlations_total",
		metric.WithDescription("Total durable incident-routing-to-repository correlation decisions by reducer domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IncidentRepositoryCorrelations counter: %w", err)
	}

	inst.CloudInventoryAdmissions, err = meter.Int64Counter(
		"eshu_dp_cloud_inventory_admissions_total",
		metric.WithDescription("Total reducer cloud-inventory identity admission records by provider and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CloudInventoryAdmissions counter: %w", err)
	}

	inst.SearchDecayPolicyApplications, err = meter.Int64Counter(
		"eshu_dp_search_decay_policy_applications_total",
		metric.WithDescription("Total search decay scoring decisions by policy, evidence class, and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SearchDecayPolicyApplications counter: %w", err)
	}

	inst.SecretsIAMReducerTrustChains, err = meter.Int64Counter(
		"eshu_dp_secrets_iam_reducer_trust_chains_total",
		metric.WithDescription("Total secrets/IAM trust-chain read-model decisions by result and confidence"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMReducerTrustChains counter: %w", err)
	}

	inst.SecretsIAMPostureObservations, err = meter.Int64Counter(
		"eshu_dp_secrets_iam_posture_observations_total",
		metric.WithDescription("Total secrets/IAM privilege posture observations by risk type and severity"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecretsIAMPostureObservations counter: %w", err)
	}

	inst.ObservabilityCoverageCorrelations, err = meter.Int64Counter(
		"eshu_dp_observability_coverage_correlations_total",
		metric.WithDescription("Total observability coverage correlation decisions by reducer domain, outcome, and coverage signal"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ObservabilityCoverageCorrelations counter: %w", err)
	}

	inst.KubernetesCorrelations, err = meter.Int64Counter(
		"eshu_dp_kubernetes_correlations_total",
		metric.WithDescription("Total live Kubernetes correlation decisions by reducer domain, outcome, and drift kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register KubernetesCorrelations counter: %w", err)
	}

	inst.KubernetesWorkloadNodes, err = meter.Int64Counter(
		"eshu_dp_kubernetes_workload_nodes_total",
		metric.WithDescription("Total canonical KubernetesWorkload graph nodes committed by reducer domain"),
	)
	if err != nil {
		return nil, fmt.Errorf("register KubernetesWorkloadNodes counter: %w", err)
	}

	inst.EC2InstanceNodes, err = meter.Int64Counter(
		"eshu_dp_ec2_instance_nodes_total",
		metric.WithDescription("Total canonical EC2 instance CloudResource graph nodes committed by reducer domain"),
	)
	if err != nil {
		return nil, fmt.Errorf("register EC2InstanceNodes counter: %w", err)
	}

	inst.EC2InstanceNodesSkipped, err = meter.Int64Counter(
		"eshu_dp_ec2_instance_nodes_skipped_total",
		metric.WithDescription("Total ec2_instance_posture facts that produced no node, by skip_reason (missing_identity/tombstone)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register EC2InstanceNodesSkipped counter: %w", err)
	}

	inst.KubernetesCorrelationEdges, err = meter.Int64Counter(
		"eshu_dp_kubernetes_correlation_edges_total",
		metric.WithDescription("Total canonical RUNS_IMAGE live-workload edges committed by resolution_mode (digest)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register KubernetesCorrelationEdges counter: %w", err)
	}

	inst.CrossplaneSatisfiedByEdges, err = meter.Int64Counter(
		"eshu_dp_crossplane_satisfied_by_edges_total",
		metric.WithDescription("Total canonical SATISFIED_BY Crossplane Claim -> XRD edges committed by resolution_mode"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CrossplaneSatisfiedByEdges counter: %w", err)
	}

	inst.SecurityGroupEndpointNodes, err = meter.Int64Counter(
		"eshu_dp_security_group_endpoint_nodes_total",
		metric.WithDescription("Total canonical CidrBlock and PrefixList graph nodes committed by endpoint kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecurityGroupEndpointNodes counter: %w", err)
	}

	inst.SecurityGroupReachabilityRuleNodes, err = meter.Int64Counter(
		"eshu_dp_security_group_reachability_rule_nodes_total",
		metric.WithDescription("Total canonical SecurityGroupRule graph nodes committed by the network-reachability edge projection"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecurityGroupReachabilityRuleNodes counter: %w", err)
	}

	inst.SecurityGroupReachabilityEdges, err = meter.Int64Counter(
		"eshu_dp_security_group_reachability_edges_total",
		metric.WithDescription("Total canonical security-group reachability edges committed by edge_type (sg_rule/rule_endpoint)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecurityGroupReachabilityEdges counter: %w", err)
	}

	inst.SecurityGroupReachabilitySkipped, err = meter.Int64Counter(
		"eshu_dp_security_group_reachability_skipped_total",
		metric.WithDescription("Total security_group_rule facts skipped by the reachability edge projection by skip_reason (unresolved_anchor/unresolved_endpoint/unknown_source)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecurityGroupReachabilitySkipped counter: %w", err)
	}

	inst.IAMEscalationEdges, err = meter.Int64Counter(
		"eshu_dp_iam_escalation_edges_total",
		metric.WithDescription("Total canonical IAM CAN_ESCALATE_TO privilege-escalation edges committed by the escalation projection"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IAMEscalationEdges counter: %w", err)
	}

	inst.IAMEscalationSkipped, err = meter.Int64Counter(
		"eshu_dp_iam_escalation_skipped_total",
		metric.WithDescription("Total IAM escalation-primitive evaluations skipped by the escalation projection by skip_reason (skipped_ambiguous/skipped_unresolved/skipped_deny/skipped_conditioned/skipped_not_action_resource/skipped_incomplete/deferred_can_assume)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IAMEscalationSkipped counter: %w", err)
	}

	inst.IAMCanPerformEdges, err = meter.Int64Counter(
		"eshu_dp_iam_can_perform_edges_total",
		metric.WithDescription("Total canonical IAM CAN_PERFORM effective-permission edges committed by the CAN_PERFORM projection by resolution_mode (exact_arn/single_glob); edge properties carry identity/resource grant_sources"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IAMCanPerformEdges counter: %w", err)
	}

	inst.IAMCanPerformSkipped, err = meter.Int64Counter(
		"eshu_dp_iam_can_perform_skipped_total",
		metric.WithDescription("Total IAM CAN_PERFORM catalog-action evaluations skipped by the CAN_PERFORM projection by skip_reason (skipped_uncatalogued_action/skipped_ambiguous/skipped_unresolved/skipped_deny/skipped_conditioned/skipped_not_action_resource/skipped_self_loop/skipped_permission_boundary)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IAMCanPerformSkipped counter: %w", err)
	}

	inst.IAMCanPerformConditioned, err = meter.Int64Counter(
		"eshu_dp_iam_can_perform_conditioned_total",
		metric.WithDescription("Total condition-gated IAM CAN_PERFORM evidence classified by bounded confidence (provenance_only)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IAMCanPerformConditioned counter: %w", err)
	}

	inst.SBOMAttestationAttachments, err = meter.Int64Counter(
		"eshu_dp_sbom_attestation_attachments_total",
		metric.WithDescription("Total SBOM and attestation attachment decisions by reducer domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SBOMAttestationAttachments counter: %w", err)
	}

	inst.SupplyChainImpactFindings, err = meter.Int64Counter(
		"eshu_dp_supply_chain_impact_findings_total",
		metric.WithDescription("Total supply-chain impact findings by reducer domain and outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SupplyChainImpactFindings counter: %w", err)
	}

	inst.SupplyChainSuppressionDecisions, err = meter.Int64Counter(
		"eshu_dp_supply_chain_suppression_decisions_total",
		metric.WithDescription("Total VEX/operator-policy suppression decisions per supply-chain impact finding by reducer domain and outcome state"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SupplyChainSuppressionDecisions counter: %w", err)
	}

	inst.SupplyChainRemediationDecisions, err = meter.Int64Counter(
		"eshu_dp_supply_chain_remediation_decisions_total",
		metric.WithDescription("Total advisory-only safe-upgrade decisions per supply-chain impact finding by reducer domain, confidence outcome, and reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SupplyChainRemediationDecisions counter: %w", err)
	}

	inst.ConfluenceHTTPRequests, err = meter.Int64Counter(
		"eshu_dp_confluence_http_requests_total",
		metric.WithDescription("Total Confluence HTTP GET requests by bounded operation, result, and status class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ConfluenceHTTPRequests counter: %w", err)
	}

	inst.ConfluencePermissionDeniedPages, err = meter.Int64Counter(
		"eshu_dp_confluence_permission_denied_pages_total",
		metric.WithDescription("Total Confluence pages skipped because the read-only credential could not view them"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ConfluencePermissionDeniedPages counter: %w", err)
	}

	inst.ConfluenceDocumentsObserved, err = meter.Int64Counter(
		"eshu_dp_confluence_documents_observed_total",
		metric.WithDescription("Total Confluence documentation documents observed by bounded result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ConfluenceDocumentsObserved counter: %w", err)
	}

	inst.ConfluenceSectionsEmitted, err = meter.Int64Counter(
		"eshu_dp_confluence_sections_emitted_total",
		metric.WithDescription("Total Confluence documentation sections emitted by bounded result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ConfluenceSectionsEmitted counter: %w", err)
	}

	inst.ConfluenceLinksEmitted, err = meter.Int64Counter(
		"eshu_dp_confluence_links_emitted_total",
		metric.WithDescription("Total Confluence documentation links emitted by bounded result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ConfluenceLinksEmitted counter: %w", err)
	}

	inst.ConfluenceSyncFailures, err = meter.Int64Counter(
		"eshu_dp_confluence_sync_failures_total",
		metric.WithDescription("Total failed Confluence sync attempts by bounded failure class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ConfluenceSyncFailures counter: %w", err)
	}

	inst.AWSAPICalls, err = meter.Int64Counter(
		"eshu_dp_aws_api_calls_total",
		metric.WithDescription("Total AWS API calls by service, account, region, operation, and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSAPICalls counter: %w", err)
	}

	inst.AWSThrottles, err = meter.Int64Counter(
		"eshu_dp_aws_throttle_total",
		metric.WithDescription("Total AWS API throttle responses by service, account, and region"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSThrottles counter: %w", err)
	}

	inst.AWSAssumeRoleFailed, err = meter.Int64Counter(
		"eshu_dp_aws_assumerole_failed_total",
		metric.WithDescription("Total AWS claim credential acquisition failures by account"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSAssumeRoleFailed counter: %w", err)
	}

	inst.AWSBudgetExhausted, err = meter.Int64Counter(
		"eshu_dp_aws_budget_exhausted_total",
		metric.WithDescription("Total AWS service scans that yielded after exhausting the configured API budget by service, account, and region"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSBudgetExhausted counter: %w", err)
	}

	inst.AWSCheckpointEvents, err = meter.Int64Counter(
		"eshu_dp_aws_pagination_checkpoint_events_total",
		metric.WithDescription("Total AWS pagination checkpoint load, save, resume, expiry, completion, and failure events by service, account, region, operation, event kind, and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSCheckpointEvents counter: %w", err)
	}

	inst.AWSResourcesEmitted, err = meter.Int64Counter(
		"eshu_dp_aws_resources_emitted_total",
		metric.WithDescription("Total AWS resource facts emitted by service, account, region, and resource type"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSResourcesEmitted counter: %w", err)
	}

	inst.AWSRelationshipsEmitted, err = meter.Int64Counter(
		"eshu_dp_aws_relationships_emitted_total",
		metric.WithDescription("Total AWS relationship facts emitted by service, account, and region"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSRelationshipsEmitted counter: %w", err)
	}

	inst.AWSTagObservationsEmitted, err = meter.Int64Counter(
		"eshu_dp_aws_tag_observations_emitted_total",
		metric.WithDescription("Total AWS tag observation facts emitted by service, account, and region"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSTagObservationsEmitted counter: %w", err)
	}

	inst.AWSFreshnessEvents, err = meter.Int64Counter(
		"eshu_dp_aws_freshness_events_total",
		metric.WithDescription("Total AWS freshness events by bounded kind and action"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSFreshnessEvents counter: %w", err)
	}

	inst.AWSOrgAccessSkipped, err = meter.Int64Counter(
		"eshu_dp_aws_org_access_skipped_total",
		metric.WithDescription("Total AWS Organizations scans skipped because credentials were not org-aware, labeled by service, account, region, and reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSOrgAccessSkipped counter: %w", err)
	}

	inst.AWSScanStatusStaleFence, err = meter.Int64Counter(
		"eshu_dp_aws_scan_status_stale_fence_total",
		metric.WithDescription("Total AWS scan-status mutations rejected by stale fence, labeled by service, account, region, and operation (start, observe, commit)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSScanStatusStaleFence counter: %w", err)
	}

	inst.WorkflowClaimAttemptBudgetExhausted, err = meter.Int64Counter(
		"eshu_dp_workflow_claim_attempt_budget_exhausted_total",
		metric.WithDescription("Total workflow claim failures escalated to terminal because the work item AttemptCount reached the configured retry budget, labeled by collector_kind and source_system"),
	)
	if err != nil {
		return nil, fmt.Errorf("register WorkflowClaimAttemptBudgetExhausted counter: %w", err)
	}

	inst.WorkflowClaimRetries, err = meter.Int64Counter(
		"eshu_dp_workflow_claim_retries_total",
		metric.WithDescription("Total retryable workflow claim failures re-queued for another attempt, labeled by collector_kind, source_system, and failure_class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register WorkflowClaimRetries counter: %w", err)
	}

	inst.WorkflowClaimProviderThrottles, err = meter.Int64Counter(
		"eshu_dp_workflow_claim_provider_throttle_total",
		metric.WithDescription("Total retryable workflow claim failures classified as provider throttling/rate-limiting, labeled by collector_kind, source_system, and outcome (retry_after_honored, poll_backoff)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register WorkflowClaimProviderThrottles counter: %w", err)
	}

	inst.CorrelationRuleMatches, err = meter.Int64Counter(
		"eshu_dp_correlation_rule_matches_total",
		metric.WithDescription("Total correlation rule matches by pack and rule"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CorrelationRuleMatches counter: %w", err)
	}

	inst.CorrelationDriftDetected, err = meter.Int64Counter(
		"eshu_dp_correlation_drift_detected_total",
		metric.WithDescription("Total admitted drift candidates by pack, rule, and drift kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CorrelationDriftDetected counter: %w", err)
	}

	inst.CorrelationDriftIntentsEnqueued, err = meter.Int64Counter(
		"eshu_dp_correlation_drift_intents_enqueued_total",
		metric.WithDescription("Total config_state_drift reducer intents enqueued by Phase 3.5 per pack and source"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CorrelationDriftIntentsEnqueued counter: %w", err)
	}

	inst.CorrelationOrphanDetected, err = meter.Int64Counter(
		"eshu_dp_correlation_orphan_detected_total",
		metric.WithDescription("Total admitted cloud-runtime orphan candidates by pack and rule"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CorrelationOrphanDetected counter: %w", err)
	}

	inst.AWSRelationshipEdges, err = meter.Int64Counter(
		"eshu_dp_aws_relationship_edges_total",
		metric.WithDescription("Total AWS relationship edge projection outcomes by relationship_type and join_mode (arn/bare_id/correlation_anchor/unresolved)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSRelationshipEdges counter: %w", err)
	}

	inst.CrossScopeOwnershipContendedRows, err = meter.Int64Counter(
		"eshu_dp_cross_scope_ownership_contended_rows_total",
		metric.WithDescription("Total #5007 owner-ledger node rows a graphowner batch lost to a higher-order-key cross-scope contributor, by family"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CrossScopeOwnershipContendedRows counter: %w", err)
	}

	inst.ReducerInputInvalidFacts, err = meter.Int64Counter(
		"eshu_dp_reducer_input_invalid_facts_total",
		metric.WithDescription("Total reducer facts quarantined during typed payload decode for a missing required identity field (input_invalid), by domain and fact_kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerInputInvalidFacts counter: %w", err)
	}

	reducerInputInvalidFactWriteBatchBuckets := []float64{0, 1, 2, 5, 10, 25, 50, 100, 250, 500}
	inst.ReducerInputInvalidFactWriteBatchSize, err = meter.Float64Histogram(
		"eshu_dp_reducer_input_invalid_fact_write_batch_size",
		metric.WithDescription("Row count of each batched durable write to reducer_input_invalid_facts, one observation per reducer intent that quarantined at least one fact"),
		metric.WithExplicitBucketBoundaries(reducerInputInvalidFactWriteBatchBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerInputInvalidFactWriteBatchSize histogram: %w", err)
	}

	inst.ReducerInputInvalidFactsCommitted, err = meter.Int64Counter(
		"eshu_dp_reducer_input_invalid_facts_committed_total",
		metric.WithDescription("Total rows successfully committed to the durable reducer_input_invalid_facts read surface"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerInputInvalidFactsCommitted counter: %w", err)
	}

	inst.ReducerInputInvalidFactWriteErrors, err = meter.Int64Counter(
		"eshu_dp_reducer_input_invalid_fact_write_errors_total",
		metric.WithDescription("Total failed batched writes to reducer_input_invalid_facts, by reason; the write is best-effort and never fails the owning reducer intent"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerInputInvalidFactWriteErrors counter: %w", err)
	}

	queryDurationBuckets := []float64{0, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	inst.QueryInputInvalidFactsDuration, err = meter.Float64Histogram(
		"eshu_dp_query_input_invalid_facts_duration_seconds",
		metric.WithDescription("Duration of the bounded reducer_input_invalid_facts read (POST /api/v0/admin/input-invalid-facts/query), regardless of outcome"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(queryDurationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register QueryInputInvalidFactsDuration histogram: %w", err)
	}

	inst.QueryInputInvalidFactsErrors, err = meter.Int64Counter(
		"eshu_dp_query_input_invalid_facts_errors_total",
		metric.WithDescription("Total failed reducer_input_invalid_facts reads, by reason (timeout/store_error)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register QueryInputInvalidFactsErrors counter: %w", err)
	}

	inst.QueryK8sSelectCandidateScanTruncated, err = meter.Int64Counter(
		"eshu_dp_query_k8s_select_candidate_scan_truncated_total",
		metric.WithDescription("Total k8s SELECTS relationship builds whose K8sResource candidate scan was truncated at the repository entity limit, by direction (outgoing/incoming)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register QueryK8sSelectCandidateScanTruncated counter: %w", err)
	}

	inst.ProjectorInputInvalidFacts, err = meter.Int64Counter(
		"eshu_dp_projector_input_invalid_facts_total",
		metric.WithDescription("Total projector canonical-extractor facts quarantined during typed payload decode for a missing required identity field (input_invalid), by stage and fact_kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ProjectorInputInvalidFacts counter: %w", err)
	}

	inst.GCPRelationshipEdges, err = meter.Int64Counter(
		"eshu_dp_gcp_relationship_edges_total",
		metric.WithDescription("Total GCP relationship edge projection outcomes by relationship_type and join_mode (full_resource_name/unresolved/partial/unsupported/invalid_type/empty_type/unknown_state)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GCPRelationshipEdges counter: %w", err)
	}

	inst.GCPMaterializationFacts, err = meter.Int64Counter(
		"eshu_dp_gcp_materialization_facts_total",
		metric.WithDescription("Total GCP materialization input facts by reducer domain and fact_kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GCPMaterializationFacts counter: %w", err)
	}

	inst.GCPMaterializationGraphWrites, err = meter.Int64Counter(
		"eshu_dp_gcp_materialization_graph_writes_total",
		metric.WithDescription("Total GCP materialization graph writes by reducer domain and kind (node/edge)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GCPMaterializationGraphWrites counter: %w", err)
	}

	inst.GCPFreshnessEvents, err = meter.Int64Counter(
		"eshu_dp_gcp_freshness_events_total",
		metric.WithDescription("Total GCP Cloud Asset Inventory freshness events by bounded kind and action"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GCPFreshnessEvents counter: %w", err)
	}

	gcpFreshnessFanOutBuckets := []float64{1, 2, 4, 8, 16, 32, 64}
	inst.GCPFreshnessFanOut, err = meter.Int64Histogram(
		"eshu_dp_gcp_freshness_fanout_scope_count",
		metric.WithDescription("Number of configured scopes one GCP freshness trigger resolved to (fan-out cardinality)"),
		metric.WithExplicitBucketBoundaries(gcpFreshnessFanOutBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register GCPFreshnessFanOut histogram: %w", err)
	}

	inst.ObservabilityCoverageEdges, err = meter.Int64Counter(
		"eshu_dp_observability_coverage_edges_total",
		metric.WithDescription("Total observability COVERS edge projection outcomes by coverage_signal and resolution_mode (arn/bare_id/correlation_anchor)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ObservabilityCoverageEdges counter: %w", err)
	}

	inst.IncidentRoutingEvidence, err = meter.Int64Counter(
		"eshu_dp_incident_routing_evidence_total",
		metric.WithDescription("Total PagerDuty incident-routing graph evidence projection outcomes by domain, outcome, source class, and slot kind"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IncidentRoutingEvidence counter: %w", err)
	}

	inst.IAMCanAssumeEdges, err = meter.Int64Counter(
		"eshu_dp_iam_can_assume_edges_total",
		metric.WithDescription("Total IAM CAN_ASSUME trust-graph edge projection outcomes by principal_kind (role/user) and resolution_mode (arn)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IAMCanAssumeEdges counter: %w", err)
	}

	inst.S3LogsToEdges, err = meter.Int64Counter(
		"eshu_dp_s3_logs_to_edges_total",
		metric.WithDescription("Total S3 LOGS_TO server-access-log edge projection outcomes by resolution_mode (name)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register S3LogsToEdges counter: %w", err)
	}

	inst.S3LogsToSkipped, err = meter.Int64Counter(
		"eshu_dp_s3_logs_to_skipped_total",
		metric.WithDescription("Total s3_bucket_posture facts that named a log target but produced no LOGS_TO edge, by skip_reason (source_unresolved/target_unresolved)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register S3LogsToSkipped counter: %w", err)
	}

	inst.EC2UsesProfileEdges, err = meter.Int64Counter(
		"eshu_dp_ec2_uses_profile_edges_total",
		metric.WithDescription("Total EC2 USES_PROFILE instance-profile edge projection outcomes by resolution_mode (arn)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register EC2UsesProfileEdges counter: %w", err)
	}

	inst.EC2UsesProfileSkipped, err = meter.Int64Counter(
		"eshu_dp_ec2_uses_profile_skipped_total",
		metric.WithDescription("Total ec2_instance_posture facts that named an instance profile but produced no USES_PROFILE edge, by skip_reason (source_unresolved/target_unresolved)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register EC2UsesProfileSkipped counter: %w", err)
	}

	inst.IAMInstanceProfileRoleEdges, err = meter.Int64Counter(
		"eshu_dp_iam_instance_profile_role_edges_total",
		metric.WithDescription("Total IAM instance-profile HAS_ROLE edge projection outcomes by resolution_mode (arn)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IAMInstanceProfileRoleEdges counter: %w", err)
	}

	inst.IAMInstanceProfileRoleSkipped, err = meter.Int64Counter(
		"eshu_dp_iam_instance_profile_role_skipped_total",
		metric.WithDescription("Total IAM instance-profile role_arns that produced no HAS_ROLE edge, by skip_reason (source_unresolved/target_unresolved)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IAMInstanceProfileRoleSkipped counter: %w", err)
	}

	inst.EC2InternetExposureDecisions, err = meter.Int64Counter(
		"eshu_dp_ec2_internet_exposure_decisions_total",
		metric.WithDescription("Total EC2 internet-exposure posture decisions by outcome and reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register EC2InternetExposureDecisions counter: %w", err)
	}

	inst.EC2InternetExposureSkipped, err = meter.Int64Counter(
		"eshu_dp_ec2_internet_exposure_skipped_total",
		metric.WithDescription("Total ec2_instance_posture facts skipped by the EC2 internet-exposure projection by skip_reason (missing_identity/tombstone)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register EC2InternetExposureSkipped counter: %w", err)
	}

	inst.EC2BlockDeviceKMSPostureDecisions, err = meter.Int64Counter(
		"eshu_dp_ec2_block_device_kms_posture_decisions_total",
		metric.WithDescription("Total EC2 block-device KMS posture decisions by outcome and reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register EC2BlockDeviceKMSPostureDecisions counter: %w", err)
	}

	inst.EC2BlockDeviceKMSPostureSkipped, err = meter.Int64Counter(
		"eshu_dp_ec2_block_device_kms_posture_skipped_total",
		metric.WithDescription("Total ec2_instance_posture facts skipped by the EC2 block-device KMS posture projection by skip_reason (source_unresolved/tombstone)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register EC2BlockDeviceKMSPostureSkipped counter: %w", err)
	}

	inst.S3InternetExposureDecisions, err = meter.Int64Counter(
		"eshu_dp_s3_internet_exposure_decisions_total",
		metric.WithDescription("Total S3 internet-exposure posture decisions by outcome and reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register S3InternetExposureDecisions counter: %w", err)
	}

	inst.S3InternetExposureSkipped, err = meter.Int64Counter(
		"eshu_dp_s3_internet_exposure_skipped_total",
		metric.WithDescription("Total s3_bucket_posture facts skipped by the S3 internet-exposure projection by skip_reason (source_unresolved)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register S3InternetExposureSkipped counter: %w", err)
	}

	inst.CorrelationUnmanagedDetected, err = meter.Int64Counter(
		"eshu_dp_correlation_unmanaged_detected_total",
		metric.WithDescription("Total admitted cloud-runtime unmanaged candidates by pack and rule"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CorrelationUnmanagedDetected counter: %w", err)
	}

	inst.DriftUnresolvedModuleCalls, err = meter.Int64Counter(
		"eshu_dp_drift_unresolved_module_calls_total",
		metric.WithDescription("Total Terraform module calls the drift loader could not resolve to a local callee, labeled by reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DriftUnresolvedModuleCalls counter: %w", err)
	}

	inst.DriftAmbiguousOwnerWriteFailed, err = meter.Int64Counter(
		"eshu_dp_drift_ambiguous_owner_write_failed_total",
		metric.WithDescription("Total failed durable writes of an ambiguous-owner terraform_config_state_drift finding, labeled by pack"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DriftAmbiguousOwnerWriteFailed counter: %w", err)
	}

	inst.DriftSchemaUnknownComposite, err = meter.Int64Counter(
		"eshu_dp_drift_schema_unknown_composite_total",
		metric.WithDescription("Total Terraform-state composite attributes the parser skipped before or during composite capture, labeled by resource type and reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DriftSchemaUnknownComposite counter: %w", err)
	}

	inst.WebhookRequests, err = meter.Int64Counter(
		"eshu_dp_webhook_requests_total",
		metric.WithDescription("Total webhook listener requests by provider, outcome, and reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register WebhookRequests counter: %w", err)
	}

	inst.WebhookTriggerDecisions, err = meter.Int64Counter(
		"eshu_dp_webhook_trigger_decisions_total",
		metric.WithDescription("Total normalized webhook trigger decisions by provider, event kind, decision, reason, and status"),
	)
	if err != nil {
		return nil, fmt.Errorf("register WebhookTriggerDecisions counter: %w", err)
	}

	inst.WebhookStoreOperations, err = meter.Int64Counter(
		"eshu_dp_webhook_store_operations_total",
		metric.WithDescription("Total webhook trigger store operations by provider, outcome, and status"),
	)
	if err != nil {
		return nil, fmt.Errorf("register WebhookStoreOperations counter: %w", err)
	}

	inst.SemanticExtractionQueueEvents, err = meter.Int64Counter(
		"eshu_dp_semantic_extraction_queue_events_total",
		metric.WithDescription("Total semantic extraction queue lifecycle events by bounded source, provider, provider profile class, status, failure class, budget state, and budget reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SemanticExtractionQueueEvents counter: %w", err)
	}

	inst.SemanticExtractionBudgetTokens, err = meter.Int64Counter(
		"eshu_dp_semantic_extraction_budget_tokens_total",
		metric.WithDescription("Total semantic extraction estimated and actual token budget usage by bounded source, provider, provider profile class, budget state, and budget reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SemanticExtractionBudgetTokens counter: %w", err)
	}

	inst.SemanticExtractionBudgetCostMicros, err = meter.Int64Counter(
		"eshu_dp_semantic_extraction_budget_cost_micros_total",
		metric.WithDescription("Total semantic extraction estimated and actual cost budget usage in micros by bounded source, provider, provider profile class, budget state, and budget reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SemanticExtractionBudgetCostMicros counter: %w", err)
	}

	// Register histograms with explicit bucket boundaries where specified
	collectorBuckets := []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.CollectorObserveDuration, err = meter.Float64Histogram(
		"eshu_dp_collector_observe_duration_seconds",
		metric.WithDescription("Collector observe cycle duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(collectorBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CollectorObserveDuration histogram: %w", err)
	}

	workflowClaimWaitBuckets := []float64{0, 0.1, 0.5, 1, 5, 10, 30, 60, 300, 900, 1800, 3600}
	inst.WorkflowClaimWaitDuration, err = meter.Float64Histogram(
		"eshu_dp_workflow_claim_wait_seconds",
		metric.WithDescription("Workflow work item age when a claim starts"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(workflowClaimWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register WorkflowClaimWaitDuration histogram: %w", err)
	}

	tfstateClaimWaitBuckets := workflowClaimWaitBuckets
	inst.TerraformStateClaimWaitDuration, err = meter.Float64Histogram(
		"eshu_dp_tfstate_claim_wait_seconds",
		metric.WithDescription("Terraform state collector work item age when a claim starts"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(tfstateClaimWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateClaimWaitDuration histogram: %w", err)
	}

	inst.WorkflowClaimLeaseAge, err = meter.Float64Histogram(
		"eshu_dp_workflow_claim_lease_age_seconds",
		metric.WithDescription("Observed heartbeat age in seconds for an active claim at heartbeat time, labeled by collector_kind and source_system"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(workflowClaimWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register WorkflowClaimLeaseAge histogram: %w", err)
	}

	inst.GraphWriteBackpressureEngaged, err = meter.Int64Counter(
		"eshu_dp_graph_write_backpressure_engaged_total",
		metric.WithDescription("Graph writes that blocked for an in-flight permit because the write path hit its concurrency ceiling, labeled by operation and gate (canonical or semantic) (issues #3560, #4448)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GraphWriteBackpressureEngaged counter: %w", err)
	}

	inst.GraphWriteBackpressureWaitDuration, err = meter.Float64Histogram(
		"eshu_dp_graph_write_backpressure_wait_seconds",
		metric.WithDescription("Time a graph write blocked waiting for an in-flight permit, labeled by operation and gate (canonical or semantic) (issues #3560, #4448)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(workflowClaimWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register GraphWriteBackpressureWaitDuration histogram: %w", err)
	}

	tfstateSnapshotByteBuckets := []float64{1024, 10240, 102400, 1048576, 10485760, 52428800, 104857600}
	inst.TerraformStateSnapshotBytes, err = meter.Int64Histogram(
		"eshu_dp_tfstate_snapshot_bytes",
		metric.WithDescription("Terraform state snapshot source size in bytes"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(tfstateSnapshotByteBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateSnapshotBytes histogram: %w", err)
	}

	tfstateParseBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	inst.TerraformStateParseDuration, err = meter.Float64Histogram(
		"eshu_dp_tfstate_parse_duration_seconds",
		metric.WithDescription("Terraform state parser stream duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(tfstateParseBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register TerraformStateParseDuration histogram: %w", err)
	}

	webhookBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	inst.WebhookRequestDuration, err = meter.Float64Histogram(
		"eshu_dp_webhook_request_duration_seconds",
		metric.WithDescription("Webhook listener request duration by provider, outcome, and reason"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(webhookBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register WebhookRequestDuration histogram: %w", err)
	}

	inst.WebhookStoreDuration, err = meter.Float64Histogram(
		"eshu_dp_webhook_store_duration_seconds",
		metric.WithDescription("Webhook trigger store operation duration by provider, outcome, and status"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(webhookBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register WebhookStoreDuration histogram: %w", err)
	}

	ociRegistryScanBuckets := []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}
	inst.OCIRegistryScanDuration, err = meter.Float64Histogram(
		"eshu_dp_oci_registry_scan_duration_seconds",
		metric.WithDescription("OCI registry repository scan duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(ociRegistryScanBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register OCIRegistryScanDuration histogram: %w", err)
	}

	kubernetesLiveListBuckets := []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}
	inst.KubernetesLiveListDuration, err = meter.Float64Histogram(
		"eshu_dp_kubernetes_list_duration_seconds",
		metric.WithDescription("Kubernetes live resource list duration by resource scope"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(kubernetesLiveListBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register KubernetesLiveListDuration histogram: %w", err)
	}

	dependencyListBuckets := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	inst.DependencyListDuration, err = meter.Float64Histogram(
		"eshu_dp_dependency_list_duration_seconds",
		metric.WithDescription("GET /api/v0/dependencies graph read duration by direction (forward or reverse)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(dependencyListBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register DependencyListDuration histogram: %w", err)
	}

	inst.DependencyListErrors, err = meter.Int64Counter(
		"eshu_dp_dependency_list_errors_total",
		metric.WithDescription("Failed GET /api/v0/dependencies graph reads by direction (forward or reverse)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DependencyListErrors counter: %w", err)
	}

	// liveActivityQueryBuckets matches the documented postgres-query-seconds
	// bucket set (docs/public/observability/telemetry-coverage.md); the
	// bounded live-activity query's own proof evidence (6.1ms normal shape,
	// 12.3ms pathological shape) sits comfortably inside it.
	liveActivityQueryBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}
	inst.LiveActivityQueryDuration, err = meter.Float64Histogram(
		"eshu_dp_status_operations_live_activity_query_duration_seconds",
		metric.WithDescription("GET /api/v0/status/operations bounded live-activity Postgres query duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(liveActivityQueryBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register LiveActivityQueryDuration histogram: %w", err)
	}

	inst.LiveActivityQueryErrors, err = meter.Int64Counter(
		"eshu_dp_status_operations_live_activity_query_errors_total",
		metric.WithDescription("Failed GET /api/v0/status/operations bounded live-activity Postgres queries"),
	)
	if err != nil {
		return nil, fmt.Errorf("register LiveActivityQueryErrors counter: %w", err)
	}

	// repositoryFreshnessQueryBuckets matches the documented
	// postgres-query-seconds bucket set (docs/public/observability/telemetry-coverage.md);
	// the #5143 prove-theory-first single-scope composite read measured
	// 2.5ms full shape against a 20k-scope/150k-work-item synthetic corpus,
	// sitting comfortably inside it.
	repositoryFreshnessQueryBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}
	inst.RepositoryFreshnessQueryDuration, err = meter.Float64Histogram(
		"eshu_dp_repository_freshness_query_duration_seconds",
		metric.WithDescription("GET /api/v0/repositories/{id}/freshness bounded composite Postgres read duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(repositoryFreshnessQueryBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register RepositoryFreshnessQueryDuration histogram: %w", err)
	}

	inst.RepositoryFreshnessQueryErrors, err = meter.Int64Counter(
		"eshu_dp_repository_freshness_query_errors_total",
		metric.WithDescription("Failed GET /api/v0/repositories/{id}/freshness bounded composite Postgres reads"),
	)
	if err != nil {
		return nil, fmt.Errorf("register RepositoryFreshnessQueryErrors counter: %w", err)
	}

	packageRegistryBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.PackageRegistryObserveDuration, err = meter.Float64Histogram(
		"eshu_dp_package_registry_observe_duration_seconds",
		metric.WithDescription("Package registry target observation duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(packageRegistryBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register PackageRegistryObserveDuration histogram: %w", err)
	}

	inst.PackageRegistryGenerationLag, err = meter.Float64Histogram(
		"eshu_dp_package_registry_generation_lag_seconds",
		metric.WithDescription("Package registry metadata generation lag from source observation to collector processing"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(collectorBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register PackageRegistryGenerationLag histogram: %w", err)
	}

	vulnerabilityBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.VulnerabilityIntelligenceFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_vulnerability_intelligence_fetch_duration_seconds",
		metric.WithDescription("Vulnerability source fetch and normalization duration by source and result"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(vulnerabilityBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register VulnerabilityIntelligenceFetchDuration histogram: %w", err)
	}

	securityAlertBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.SecurityAlertFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_security_alert_fetch_duration_seconds",
		metric.WithDescription("Hosted provider security-alert fetch duration by provider and status class"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(securityAlertBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SecurityAlertFetchDuration histogram: %w", err)
	}

	cicdRunBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.CICDRunFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_ci_cd_run_fetch_duration_seconds",
		metric.WithDescription("Hosted CI/CD run provider fetch duration by provider and status class"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(cicdRunBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CICDRunFetchDuration histogram: %w", err)
	}

	pagerDutyBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.PagerDutyFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_pagerduty_fetch_duration_seconds",
		metric.WithDescription("PagerDuty incident evidence fetch duration by provider and status class"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(pagerDutyBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register PagerDutyFetchDuration histogram: %w", err)
	}

	inst.PagerDutyGenerationLag, err = meter.Float64Histogram(
		"eshu_dp_pagerduty_generation_lag_seconds",
		metric.WithDescription("PagerDuty incident evidence lag from source observation to collector processing"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(collectorBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register PagerDutyGenerationLag histogram: %w", err)
	}

	jiraBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.JiraFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_jira_fetch_duration_seconds",
		metric.WithDescription("Jira work-item evidence fetch duration by provider and status class"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(jiraBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register JiraFetchDuration histogram: %w", err)
	}

	grafanaBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.GrafanaFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_grafana_fetch_duration_seconds",
		metric.WithDescription("Grafana observed metadata fetch duration by provider and status class"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(grafanaBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register GrafanaFetchDuration histogram: %w", err)
	}

	prometheusMimirBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.PrometheusMimirFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_prometheus_mimir_fetch_duration_seconds",
		metric.WithDescription("Prometheus/Mimir observed metadata fetch duration by provider and status class"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(prometheusMimirBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register PrometheusMimirFetchDuration histogram: %w", err)
	}

	lokiBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.LokiFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_loki_fetch_duration_seconds",
		metric.WithDescription("Loki observed metadata fetch duration by provider and status class"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(lokiBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register LokiFetchDuration histogram: %w", err)
	}

	tempoBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.TempoFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_tempo_fetch_duration_seconds",
		metric.WithDescription("Tempo metadata fetch duration by provider and status class"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(tempoBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register TempoFetchDuration histogram: %w", err)
	}

	scannerWorkerWaitBuckets := []float64{0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900, 1800, 3600, 21600}
	inst.ScannerWorkerQueueWaitDuration, err = meter.Float64Histogram(
		"eshu_dp_scanner_worker_queue_wait_seconds",
		metric.WithDescription("Scanner-worker work item age when claim processing starts"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(scannerWorkerWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerQueueWaitDuration histogram: %w", err)
	}

	scannerWorkerScanBuckets := []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600, 1200}
	inst.ScannerWorkerScanDuration, err = meter.Float64Histogram(
		"eshu_dp_scanner_worker_scan_duration_seconds",
		metric.WithDescription("Scanner-worker analyzer execution duration by analyzer, target kind, and result"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(scannerWorkerScanBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerScanDuration histogram: %w", err)
	}

	scannerWorkerCountBuckets := []float64{1, 10, 100, 1000, 10000, 100000}
	inst.ScannerWorkerTargetCount, err = meter.Int64Histogram(
		"eshu_dp_scanner_worker_target_count",
		metric.WithDescription("Number of bounded targets processed by one scanner-worker claim"),
		metric.WithExplicitBucketBoundaries(scannerWorkerCountBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerTargetCount histogram: %w", err)
	}

	inst.ScannerWorkerResultCount, err = meter.Int64Histogram(
		"eshu_dp_scanner_worker_result_count",
		metric.WithDescription("Number of scanner-worker source results emitted by one claim"),
		metric.WithExplicitBucketBoundaries(scannerWorkerCountBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerResultCount histogram: %w", err)
	}

	scannerWorkerCPUBuckets := []float64{0.01, 0.1, 1, 10, 30, 60, 120, 300, 600, 1800}
	inst.ScannerWorkerCPUSeconds, err = meter.Float64Histogram(
		"eshu_dp_scanner_worker_cpu_seconds",
		metric.WithDescription("Scanner-worker CPU seconds consumed by analyzer, target kind, and result"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(scannerWorkerCPUBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerCPUSeconds histogram: %w", err)
	}

	scannerWorkerMemoryBuckets := []float64{1048576, 16777216, 67108864, 268435456, 1073741824, 2147483648, 4294967296, 8589934592, 17179869184}
	inst.ScannerWorkerMemoryBytes, err = meter.Int64Histogram(
		"eshu_dp_scanner_worker_memory_bytes",
		metric.WithDescription("Scanner-worker peak memory bytes by analyzer, target kind, and result"),
		metric.WithUnit("By"),
		metric.WithExplicitBucketBoundaries(scannerWorkerMemoryBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScannerWorkerMemoryBytes histogram: %w", err)
	}

	confluenceFetchBuckets := []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.ConfluenceFetchDuration, err = meter.Float64Histogram(
		"eshu_dp_confluence_fetch_duration_seconds",
		metric.WithDescription("Confluence HTTP GET duration by bounded operation and result"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(confluenceFetchBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ConfluenceFetchDuration histogram: %w", err)
	}

	awsScanBuckets := []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}
	inst.AWSScanDuration, err = meter.Float64Histogram(
		"eshu_dp_aws_scan_duration_seconds",
		metric.WithDescription("AWS service claim scan duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(awsScanBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register AWSScanDuration histogram: %w", err)
	}

	scopeAssignBuckets := []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	inst.ScopeAssignDuration, err = meter.Float64Histogram(
		"eshu_dp_scope_assign_duration_seconds",
		metric.WithDescription("Scope assignment duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(scopeAssignBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ScopeAssignDuration histogram: %w", err)
	}

	factEmitBuckets := []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}
	inst.FactEmitDuration, err = meter.Float64Histogram(
		"eshu_dp_fact_emit_duration_seconds",
		metric.WithDescription("Fact emission duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(factEmitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register FactEmitDuration histogram: %w", err)
	}

	projectorBuckets := []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120}
	inst.ProjectorRunDuration, err = meter.Float64Histogram(
		"eshu_dp_projector_run_duration_seconds",
		metric.WithDescription("Projector run cycle duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(projectorBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ProjectorRunDuration histogram: %w", err)
	}

	projectorStageBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120}
	inst.ProjectorStageDuration, err = meter.Float64Histogram(
		"eshu_dp_projector_stage_duration_seconds",
		metric.WithDescription("Projector stage duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(projectorStageBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ProjectorStageDuration histogram: %w", err)
	}

	reducerRunBuckets := []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 900}
	inst.ReducerRunDuration, err = meter.Float64Histogram(
		"eshu_dp_reducer_run_duration_seconds",
		metric.WithDescription("Reducer intent execution duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(reducerRunBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerRunDuration histogram: %w", err)
	}

	reducerWaitBuckets := []float64{0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900, 1800, 3600, 21600}
	inst.ReducerQueueWaitDuration, err = meter.Float64Histogram(
		"eshu_dp_reducer_queue_wait_seconds",
		metric.WithDescription("Reducer work item time from queue visibility to handler start"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(reducerWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReducerQueueWaitDuration histogram: %w", err)
	}

	inst.SearchIndexWriteDuration, err = meter.Float64Histogram(
		"eshu_dp_search_index_write_duration_seconds",
		metric.WithDescription("Persisted search index write duration by reducer domain, operation, and result"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(reducerWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SearchIndexWriteDuration histogram: %w", err)
	}

	inst.GCPMaterializationDuration, err = meter.Float64Histogram(
		"eshu_dp_gcp_materialization_duration_seconds",
		metric.WithDescription("GCP materialization stage duration by reducer domain and write_phase"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(reducerWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register GCPMaterializationDuration histogram: %w", err)
	}

	inst.SearchVectorBuildPhaseDuration, err = meter.Float64Histogram(
		"eshu_dp_search_vector_build_phase_seconds",
		metric.WithDescription("Search vector build sweep phase duration by domain and write_phase (scheduling_wait, query_load, embed_build, write_upsert)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(reducerWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SearchVectorBuildPhaseDuration histogram: %w", err)
	}

	retentionDurationBuckets := []float64{0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900}
	inst.GenerationRetentionDuration, err = meter.Float64Histogram(
		"eshu_dp_generation_retention_duration_seconds",
		metric.WithDescription("Generation retention pruning transaction duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(retentionDurationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationRetentionDuration histogram: %w", err)
	}

	retentionBatchBuckets := []float64{1, 2, 4, 8, 16, 32, 64, 100}
	inst.GenerationRetentionBatchSize, err = meter.Int64Histogram(
		"eshu_dp_generation_retention_batch_size",
		metric.WithDescription("Superseded generation count selected by one retention pruning batch"),
		metric.WithExplicitBucketBoundaries(retentionBatchBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationRetentionBatchSize histogram: %w", err)
	}

	retentionAgeBuckets := []float64{3600, 21600, 43200, 86400, 259200, 604800, 1209600, 2592000, 7776000}
	inst.GenerationRetentionOldestEligibleAge, err = meter.Float64Histogram(
		"eshu_dp_generation_retention_oldest_eligible_age_seconds",
		metric.WithDescription("Age of the oldest superseded generation selected by one retention pruning batch"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(retentionAgeBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationRetentionOldestEligibleAge histogram: %w", err)
	}

	canonicalWriteBuckets := []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.CanonicalWriteDuration, err = meter.Float64Histogram(
		"eshu_dp_canonical_write_duration_seconds",
		metric.WithDescription("Canonical graph write duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(canonicalWriteBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalWriteDuration histogram: %w", err)
	}

	inst.QueueClaimDuration, err = meter.Float64Histogram(
		"eshu_dp_queue_claim_duration_seconds",
		metric.WithDescription("Queue work item claim duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("register QueueClaimDuration histogram: %w", err)
	}

	postgresBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}
	inst.PostgresQueryDuration, err = meter.Float64Histogram(
		"eshu_dp_postgres_query_duration_seconds",
		metric.WithDescription("Postgres query duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(postgresBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register PostgresQueryDuration histogram: %w", err)
	}

	neo4jQueryBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}
	inst.Neo4jQueryDuration, err = meter.Float64Histogram(
		"eshu_dp_neo4j_query_duration_seconds",
		metric.WithDescription("Neo4j query duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(neo4jQueryBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register Neo4jQueryDuration histogram: %w", err)
	}

	iacResourceListBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
	inst.IaCResourceListDuration, err = meter.Float64Histogram(
		"eshu_dp_iac_resource_list_duration_seconds",
		metric.WithDescription("Bounded IaC resource list (GET /api/v0/iac/resources) handler duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(iacResourceListBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register IaCResourceListDuration histogram: %w", err)
	}

	inst.IaCResourceListErrors, err = meter.Int64Counter(
		"eshu_dp_iac_resource_list_errors_total",
		metric.WithDescription("Bounded IaC resource list (GET /api/v0/iac/resources) handler errors"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IaCResourceListErrors counter: %w", err)
	}

	inst.CloudResourceListDuration, err = meter.Float64Histogram(
		"eshu_dp_cloud_resource_list_duration_seconds",
		metric.WithDescription("Cloud resource list query duration for GET /api/v0/cloud/resources"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(neo4jQueryBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CloudResourceListDuration histogram: %w", err)
	}

	inst.CloudResourceListErrors, err = meter.Int64Counter(
		"eshu_dp_cloud_resource_list_errors_total",
		metric.WithDescription("Cloud resource list query errors for GET /api/v0/cloud/resources"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CloudResourceListErrors counter: %w", err)
	}

	cloudResourceListRowBuckets := []float64{0, 1, 2, 5, 10, 25, 50, 100, 201}
	inst.CloudResourceListScannedRows, err = meter.Int64Histogram(
		"eshu_dp_cloud_resource_list_scanned_rows",
		metric.WithDescription("Owner-ledger candidate rows returned by the bounded cloud resource page selection"),
		metric.WithExplicitBucketBoundaries(cloudResourceListRowBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CloudResourceListScannedRows histogram: %w", err)
	}

	inst.CloudResourceListPageSize, err = meter.Int64Histogram(
		"eshu_dp_cloud_resource_list_page_size",
		metric.WithDescription("Cloud resources returned by one GET /api/v0/cloud/resources page"),
		metric.WithExplicitBucketBoundaries(cloudResourceListRowBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CloudResourceListPageSize histogram: %w", err)
	}

	inst.CloudResourceListTruncations, err = meter.Int64Counter(
		"eshu_dp_cloud_resource_list_truncations_total",
		metric.WithDescription("Cloud resource list pages with another page available"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CloudResourceListTruncations counter: %w", err)
	}

	inst.APIRequestDuration, err = meter.Float64Histogram(
		"eshu_dp_api_request_duration_seconds",
		metric.WithDescription("Per-endpoint query API/MCP read handler duration, labeled by route and status_class"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(neo4jQueryBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register APIRequestDuration histogram: %w", err)
	}

	inst.APIRequestErrors, err = meter.Int64Counter(
		"eshu_dp_api_request_errors_total",
		metric.WithDescription("Per-endpoint query API/MCP read handler server errors (5xx), labeled by route and status_class"),
	)
	if err != nil {
		return nil, fmt.Errorf("register APIRequestErrors counter: %w", err)
	}

	relationshipBreakdownWaitBuckets := []float64{0, 0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	inst.RelationshipBreakdownPermitWaitDuration, err = meter.Float64Histogram(
		"eshu_dp_relationship_breakdown_permit_wait_seconds",
		metric.WithDescription("Time relationship catalog source-tool breakdown reads wait for a handler-wide graph-read permit"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(relationshipBreakdownWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register RelationshipBreakdownPermitWaitDuration histogram: %w", err)
	}

	inst.RelationshipBreakdownQueued, err = meter.Int64UpDownCounter(
		"eshu_dp_relationship_breakdown_queued",
		metric.WithDescription("Current relationship catalog source-tool breakdown reads waiting for a graph-read permit"),
		metric.WithUnit("{read}"),
	)
	if err != nil {
		return nil, fmt.Errorf("register RelationshipBreakdownQueued up/down counter: %w", err)
	}

	inst.RelationshipBreakdownInFlight, err = meter.Int64UpDownCounter(
		"eshu_dp_relationship_breakdown_in_flight",
		metric.WithDescription("Current relationship catalog source-tool breakdown reads holding a graph-read permit"),
		metric.WithUnit("{read}"),
	)
	if err != nil {
		return nil, fmt.Errorf("register RelationshipBreakdownInFlight up/down counter: %w", err)
	}

	inst.SearchHybridDegraded, err = meter.Int64Counter(
		"eshu_dp_search_hybrid_degraded_total",
		metric.WithDescription("Semantic/hybrid search requests served without semantic ranking (degraded to BM25, or refused) by query_type and reason. Expected, not an error, in no-embedder mode."),
	)
	if err != nil {
		return nil, fmt.Errorf("register SearchHybridDegraded counter: %w", err)
	}

	inst.OIDCLoginThrottled, err = meter.Int64Counter(
		"eshu_dp_oidc_login_throttled_total",
		metric.WithDescription("OIDC login requests rejected by per-IP or per-user rate limiter"),
	)
	if err != nil {
		return nil, fmt.Errorf("register OIDCLoginThrottled counter: %w", err)
	}

	inst.MCPTransportAuthDenied, err = meter.Int64Counter(
		"eshu_dp_mcp_transport_auth_denied_total",
		metric.WithDescription("MCP transport-level authentication denials by mcp_method and reason, so an operator can see catalog-enumeration or session-hijack attempts against initialize/tools/list/tools/call/ping/SSE"),
	)
	if err != nil {
		return nil, fmt.Errorf("register MCPTransportAuthDenied counter: %w", err)
	}

	inst.GovernanceAuditAllowedEmitted, err = meter.Int64Counter(
		"eshu_dp_governance_audit_allowed_emitted_total",
		metric.WithDescription("F-9 (#5170) allowed-read governance-audit events accepted into the async appender's bounded buffer"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GovernanceAuditAllowedEmitted counter: %w", err)
	}

	inst.GovernanceAuditAllowedDropped, err = meter.Int64Counter(
		"eshu_dp_governance_audit_allowed_dropped_total",
		metric.WithDescription("F-9 (#5170) allowed-read governance-audit events dropped because the async appender's buffer was full or the appender was closed; non-zero means governance data loss is happening"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GovernanceAuditAllowedDropped counter: %w", err)
	}

	inst.GovernanceAuditAllowedPersistFailures, err = meter.Int64Counter(
		"eshu_dp_governance_audit_allowed_persist_failures_total",
		metric.WithDescription("F-9 (#5170) allowed-read governance-audit events accepted into the async appender's buffer but that a durable-store Append call failed to persist"),
	)
	if err != nil {
		return nil, fmt.Errorf("register GovernanceAuditAllowedPersistFailures counter: %w", err)
	}

	inst.AuthSecretSealTotal, err = meter.Int64Counter(
		"eshu_dp_auth_secret_seal_total",
		metric.WithDescription("go/internal/secretcrypto Keyring.Seal calls by operation and result (success, error). Never carries plaintext, ciphertext, or key material."),
	)
	if err != nil {
		return nil, fmt.Errorf("register AuthSecretSealTotal counter: %w", err)
	}

	inst.AuthSecretOpenTotal, err = meter.Int64Counter(
		"eshu_dp_auth_secret_open_total",
		metric.WithDescription("go/internal/secretcrypto Keyring.Open calls by operation and result (success, error). Never carries plaintext, ciphertext, or key material."),
	)
	if err != nil {
		return nil, fmt.Errorf("register AuthSecretOpenTotal counter: %w", err)
	}

	inst.AuthBootstrapCredentialGeneratedTotal, err = meter.Int64Counter(
		"eshu_dp_auth_bootstrap_credential_generated_total",
		metric.WithDescription("IdentitySubjectStore.GenerateBootstrapCredential outcomes by result (generated on a true first insert, already_provisioned on an idempotent conflict)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AuthBootstrapCredentialGeneratedTotal counter: %w", err)
	}

	inst.AuthBootstrapSeedTotal, err = meter.Int64Counter(
		"eshu_dp_auth_bootstrap_seed_total",
		metric.WithDescription("Startup bootstrap identity seeding stage outcome by bounded outcome value (sealed_existing, seeded_env, generated, skipped_sso-only, skipped_disabled, error)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AuthBootstrapSeedTotal counter: %w", err)
	}

	inst.AuthSetupWizardTotal, err = meter.Int64Counter(
		"eshu_dp_auth_setup_wizard_total",
		metric.WithDescription("First-run setup wizard (#4965) outcomes by bounded step_result value (claim_allowed, claim_denied, admin_allowed, admin_denied, mfa_allowed, mfa_denied, error)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AuthSetupWizardTotal counter: %w", err)
	}

	inst.AuthSignInPolicyGuardrailTotal, err = meter.Int64Counter(
		"eshu_dp_auth_sign_in_policy_guardrail_total",
		metric.WithDescription("Tenant sign-in policy require_sso enable attempts by bounded decision value (allowed, denied_no_provider, denied_no_sso_proof)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AuthSignInPolicyGuardrailTotal counter: %w", err)
	}

	inst.AuthRequireSSOLoginGateTotal, err = meter.Int64Counter(
		"eshu_dp_auth_require_sso_login_gate_total",
		metric.WithDescription("Local password login attempts evaluated against tenant require_sso policy by bounded decision value (not_required, allowed_admin, denied_non_admin, policy_read_error_admin_allowed, policy_read_error_fail_closed_non_admin)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register AuthRequireSSOLoginGateTotal counter: %w", err)
	}

	inst.SharedAcceptanceUpsertDuration, err = meter.Float64Histogram(
		"eshu_dp_shared_acceptance_upsert_duration_seconds",
		metric.WithDescription("Shared acceptance upsert duration"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedAcceptanceUpsertDuration histogram: %w", err)
	}

	acceptanceLookupBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	inst.SharedAcceptanceLookupDuration, err = meter.Float64Histogram(
		"eshu_dp_shared_acceptance_lookup_duration_seconds",
		metric.WithDescription("Shared acceptance lookup duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(acceptanceLookupBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedAcceptanceLookupDuration histogram: %w", err)
	}

	acceptancePrefetchBuckets := []float64{1, 2, 4, 8, 16, 32, 64, 128, 256, 512}
	inst.SharedAcceptancePrefetchSize, err = meter.Int64Histogram(
		"eshu_dp_shared_acceptance_prefetch_size",
		metric.WithDescription("Shared acceptance bounded-unit prefetch size"),
		metric.WithExplicitBucketBoundaries(acceptancePrefetchBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedAcceptancePrefetchSize histogram: %w", err)
	}

	sharedProjectionWaitBuckets := []float64{0.001, 0.01, 0.1, 1, 5, 10, 30, 60, 300, 900, 1800, 3600, 21600}
	inst.SharedProjectionIntentWaitDuration, err = meter.Float64Histogram(
		"eshu_dp_shared_projection_intent_wait_seconds",
		metric.WithDescription("Shared projection intent age when a partition processes or blocks it"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(sharedProjectionWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedProjectionIntentWaitDuration histogram: %w", err)
	}

	sharedProjectionProcessingBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.SharedProjectionProcessingDuration, err = meter.Float64Histogram(
		"eshu_dp_shared_projection_processing_seconds",
		metric.WithDescription("Shared projection graph-write and completion duration after partition selection"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(sharedProjectionProcessingBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedProjectionProcessingDuration histogram: %w", err)
	}

	inst.SharedProjectionStepDuration, err = meter.Float64Histogram(
		"eshu_dp_shared_projection_step_seconds",
		metric.WithDescription("Shared projection substep duration by write phase"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(sharedProjectionProcessingBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedProjectionStepDuration histogram: %w", err)
	}

	inst.SharedProjectionPartitionProcessingDuration, err = meter.Float64Histogram(
		"eshu_dp_shared_projection_partition_processing_seconds",
		metric.WithDescription("Per-(domain, partition_id) wall time for one ProcessPartitionOnce call "+
			"(lease claim + selection + retract + write + mark_completed). "+
			"Bounded dims: domain is the fixed domain set; partition_id is 0-based ≤ ESHU_SHARED_PROJECTION_PARTITION_COUNT. "+
			"Primary long-pole signal for #3624: identifies which (domain, partition) pair dominates cycle latency."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(sharedProjectionProcessingBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedProjectionPartitionProcessingDuration histogram: %w", err)
	}

	inst.DocumentationDriftGenerationDuration, err = meter.Float64Histogram(
		"eshu_dp_documentation_drift_generation_duration_seconds",
		metric.WithDescription("Duration of documentation drift finding generation"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DocumentationDriftGenerationDuration histogram: %w", err)
	}

	// Collector concurrency instruments
	repoSnapshotBuckets := []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}
	inst.RepoSnapshotDuration, err = meter.Float64Histogram(
		"eshu_dp_repo_snapshot_duration_seconds",
		metric.WithDescription("Per-repository snapshot duration including discovery, parsing, and materialization"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(repoSnapshotBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register RepoSnapshotDuration histogram: %w", err)
	}

	// Per-stage snapshot buckets reach finer-grained low end than the
	// whole-repo histogram because individual stages (discovery, pre-scan,
	// materialize) routinely complete in well under a second, while parse,
	// SCIP, and value-flow evidence can dominate a slow repository.
	collectorStageBuckets := []float64{0.005, 0.025, 0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120}
	inst.CollectorSnapshotStageDuration, err = meter.Float64Histogram(
		"eshu_dp_collector_snapshot_stage_duration_seconds",
		metric.WithDescription("Per-stage git-collector snapshot duration, labeled by collector_kind and stage"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(collectorStageBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CollectorSnapshotStageDuration histogram: %w", err)
	}

	fileParseBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}
	inst.FileParseDuration, err = meter.Float64Histogram(
		"eshu_dp_file_parse_duration_seconds",
		metric.WithDescription("Per-file parse duration"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(fileParseBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register FileParseDuration histogram: %w", err)
	}

	// Same bucket shape as FileParseDuration: pre_scan and parse both run one
	// tree-sitter pass per file, so their per-file cost distributions land in
	// the same range. Sharing the histogram's "language" attribute naming
	// (not the instrument itself) lets an operator pivot language cost across
	// both stages with the same PromQL shape (#4767).
	inst.FilePreScanDuration, err = meter.Float64Histogram(
		"eshu_dp_file_prescan_duration_seconds",
		metric.WithDescription("Per-file pre_scan duration, labeled by language"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(fileParseBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register FilePreScanDuration histogram: %w", err)
	}

	inst.ReposSnapshotted, err = meter.Int64Counter(
		"eshu_dp_repos_snapshotted_total",
		metric.WithDescription("Total repositories snapshotted by status"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReposSnapshotted counter: %w", err)
	}

	inst.FilesParsed, err = meter.Int64Counter(
		"eshu_dp_files_parsed_total",
		metric.WithDescription("Total files parsed by status"),
	)
	if err != nil {
		return nil, fmt.Errorf("register FilesParsed counter: %w", err)
	}

	inst.SCIPSnapshotAttempts, err = meter.Int64Counter(
		"eshu_dp_scip_snapshot_attempts_total",
		metric.WithDescription("Total SCIP snapshot attempts by selected language and result"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SCIPSnapshotAttempts counter: %w", err)
	}

	scipProcessWaitBuckets := []float64{0, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30, 60}
	inst.SCIPProcessWaitDuration, err = meter.Float64Histogram(
		"eshu_dp_scip_process_wait_seconds",
		metric.WithDescription("Time spent waiting for a SCIP process slot before launching an external indexer"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(scipProcessWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SCIPProcessWaitDuration histogram: %w", err)
	}

	inst.FactBatchesCommitted, err = meter.Int64Counter(
		"eshu_dp_fact_batches_committed_total",
		metric.WithDescription("Total fact batches committed to Postgres during streaming ingestion"),
	)
	if err != nil {
		return nil, fmt.Errorf("register FactBatchesCommitted counter: %w", err)
	}

	// Use wide buckets for fact counts — repos range from 5 to 295k facts
	generationFactBuckets := []float64{10, 50, 100, 500, 1000, 5000, 10000, 50000, 100000, 300000}
	inst.GenerationFactCount, err = meter.Float64Histogram(
		"eshu_dp_generation_fact_count",
		metric.WithDescription("Fact count per scope generation, for identifying outlier repos"),
		metric.WithExplicitBucketBoundaries(generationFactBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register GenerationFactCount histogram: %w", err)
	}

	inst.ContentReReads, err = meter.Int64Counter(
		"eshu_dp_content_rereads_total",
		metric.WithDescription("Total content file re-reads from disk during two-phase streaming"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ContentReReads counter: %w", err)
	}

	inst.ContentReReadSkips, err = meter.Int64Counter(
		"eshu_dp_content_reread_skips_total",
		metric.WithDescription("Content re-reads skipped due to missing file or read error"),
	)
	if err != nil {
		return nil, fmt.Errorf("register ContentReReadSkips counter: %w", err)
	}

	inst.DiscoveryDirsSkipped, err = meter.Int64Counter(
		"eshu_dp_discovery_dirs_skipped_total",
		metric.WithDescription("Directories pruned during file discovery, labeled by ignored directory name"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DiscoveryDirsSkipped counter: %w", err)
	}

	inst.DiscoveryFilesSkipped, err = meter.Int64Counter(
		"eshu_dp_discovery_files_skipped_total",
		metric.WithDescription("Files skipped during file discovery, labeled by skip reason (extension or hidden)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DiscoveryFilesSkipped counter: %w", err)
	}

	inst.RepositoryBasenameCollision, err = meter.Int64Counter(
		"eshu_dp_repository_basename_collision_total",
		metric.WithDescription("Number of discovered repository paths whose basename collides with another discovered path in the same collector cycle; a heuristic — non-zero is a likely signal of accidental corpus nesting, not proof of duplication"),
	)
	if err != nil {
		return nil, fmt.Errorf("register RepositoryBasenameCollision counter: %w", err)
	}

	inst.LargeRepoClassifications, err = meter.Int64Counter(
		"eshu_dp_large_repo_classifications_total",
		metric.WithDescription("Repositories classified by size tier (small or large)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register LargeRepoClassifications counter: %w", err)
	}

	semWaitBuckets := []float64{0, 0.1, 0.5, 1, 5, 10, 30, 60, 120, 300}
	inst.LargeRepoSemaphoreWait, err = meter.Float64Histogram(
		"eshu_dp_large_repo_semaphore_wait_seconds",
		metric.WithDescription("Time spent waiting for the large-repo semaphore before snapshotting"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(semWaitBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register LargeRepoSemaphoreWait histogram: %w", err)
	}

	batchClaimBuckets := []float64{1, 4, 8, 16, 32, 64, 128}
	inst.BatchClaimSize, err = meter.Int64Histogram(
		"eshu_dp_reducer_batch_claim_size",
		metric.WithDescription("Number of work items claimed per batch claim call"),
		metric.WithExplicitBucketBoundaries(batchClaimBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register BatchClaimSize histogram: %w", err)
	}

	neo4jBatchBuckets := []float64{1, 10, 50, 100, 250, 500, 1000}
	inst.Neo4jBatchSize, err = meter.Float64Histogram(
		"eshu_dp_neo4j_batch_size",
		metric.WithDescription("Number of rows per Neo4j UNWIND batch execution"),
		metric.WithExplicitBucketBoundaries(neo4jBatchBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register Neo4jBatchSize histogram: %w", err)
	}

	inst.Neo4jBatchesExecuted, err = meter.Int64Counter(
		"eshu_dp_neo4j_batches_executed_total",
		metric.WithDescription("Total Neo4j UNWIND batch executions"),
	)
	if err != nil {
		return nil, fmt.Errorf("register Neo4jBatchesExecuted counter: %w", err)
	}

	inst.SharedEdgeWriteGroups, err = meter.Int64Counter(
		"eshu_dp_shared_edge_write_groups_total",
		metric.WithDescription("Total grouped shared-edge write transactions by domain"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedEdgeWriteGroups counter: %w", err)
	}

	inst.SharedEdgeRunsOnRetractOmissions, err = meter.Int64Counter(
		"eshu_dp_shared_edge_runs_on_retract_omissions_total",
		metric.WithDescription("Total impossible shared-edge RUNS_ON retracts omitted by bounded source-capability reason and domain"),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedEdgeRunsOnRetractOmissions counter: %w", err)
	}

	sharedEdgeWriteGroupDurationBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.SharedEdgeWriteGroupDuration, err = meter.Float64Histogram(
		"eshu_dp_shared_edge_write_group_duration_seconds",
		metric.WithDescription("Duration of each grouped shared-edge write transaction by domain"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(sharedEdgeWriteGroupDurationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedEdgeWriteGroupDuration histogram: %w", err)
	}

	sharedEdgeWriteGroupStatementBuckets := []float64{1, 2, 4, 8, 16, 32, 64, 128}
	inst.SharedEdgeWriteGroupStatementCount, err = meter.Int64Histogram(
		"eshu_dp_shared_edge_write_group_statement_count",
		metric.WithDescription("Number of statements executed in each grouped shared-edge write transaction by domain"),
		metric.WithExplicitBucketBoundaries(sharedEdgeWriteGroupStatementBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register SharedEdgeWriteGroupStatementCount histogram: %w", err)
	}

	inst.CodeCallEdgeBatches, err = meter.Int64Counter(
		"eshu_dp_code_call_edge_batches_total",
		metric.WithDescription("Total isolated code-call edge batch executions"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CodeCallEdgeBatches counter: %w", err)
	}

	codeCallBatchBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
	inst.CodeCallEdgeDuration, err = meter.Float64Histogram(
		"eshu_dp_code_call_edge_batch_duration_seconds",
		metric.WithDescription("Duration of each isolated code-call edge batch transaction"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(codeCallBatchBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CodeCallEdgeDuration histogram: %w", err)
	}

	inst.CanonicalAtomicWrites, err = meter.Int64Counter(
		"eshu_dp_canonical_atomic_writes_total",
		metric.WithDescription("Total canonical writes dispatched as a single atomic transaction"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalAtomicWrites counter: %w", err)
	}

	inst.CanonicalAtomicFallbacks, err = meter.Int64Counter(
		"eshu_dp_canonical_atomic_fallbacks_total",
		metric.WithDescription("Total canonical writes falling back to sequential execution"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalAtomicFallbacks counter: %w", err)
	}

	inst.Neo4jDeadlockRetries, err = meter.Int64Counter(
		"eshu_dp_neo4j_deadlock_retries_total",
		metric.WithDescription("Total graph-write retries by write phase and bounded retry reason"),
	)
	if err != nil {
		return nil, fmt.Errorf("register Neo4jDeadlockRetries counter: %w", err)
	}

	// Canonical projection instruments
	inst.CanonicalNodesWritten, err = meter.Int64Counter(
		"eshu_dp_canonical_nodes_written_total",
		metric.WithDescription("Total canonical nodes written to Neo4j, labeled by node type"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalNodesWritten counter: %w", err)
	}

	inst.CanonicalEdgesWritten, err = meter.Int64Counter(
		"eshu_dp_canonical_edges_written_total",
		metric.WithDescription("Total canonical edges written to Neo4j, labeled by edge type"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalEdgesWritten counter: %w", err)
	}

	canonicalProjectionBuckets := []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}
	inst.CanonicalProjectionDuration, err = meter.Float64Histogram(
		"eshu_dp_canonical_projection_duration_seconds",
		metric.WithDescription("Total canonical projection duration per repository"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(canonicalProjectionBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalProjectionDuration histogram: %w", err)
	}

	canonicalRetractBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}
	inst.CanonicalRetractDuration, err = meter.Float64Histogram(
		"eshu_dp_canonical_retract_duration_seconds",
		metric.WithDescription("Duration of canonical node retraction per repository"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(canonicalRetractBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalRetractDuration histogram: %w", err)
	}

	canonicalBatchBuckets := []float64{1, 10, 50, 100, 250, 500, 1000}
	inst.CanonicalBatchSize, err = meter.Float64Histogram(
		"eshu_dp_canonical_batch_size",
		metric.WithDescription("Rows per canonical UNWIND batch execution"),
		metric.WithExplicitBucketBoundaries(canonicalBatchBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalBatchSize histogram: %w", err)
	}

	canonicalPhaseBuckets := []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}
	inst.CanonicalPhaseDuration, err = meter.Float64Histogram(
		"eshu_dp_canonical_phase_duration_seconds",
		metric.WithDescription("Duration of each canonical write phase (repository, directories, files, entities, etc.)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(canonicalPhaseBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CanonicalPhaseDuration histogram: %w", err)
	}

	// Evidence discovery instruments (during ingestion)
	inst.EvidenceFactsDiscovered, err = meter.Int64Counter(
		"eshu_dp_evidence_facts_discovered_total",
		metric.WithDescription("Total evidence facts discovered from IaC content during ingestion"),
	)
	if err != nil {
		return nil, fmt.Errorf("register EvidenceFactsDiscovered counter: %w", err)
	}

	// Ingestion-TX lock split instrument (issue #4451, § T8): how long
	// CommitScopeGeneration holds the deferred-maintenance shared advisory
	// barrier for one atomic commit. Low values confirm the per-commit
	// relationship backfill runs after release, not inside the held window.
	inst.IngestionSharedLockHoldDuration, err = meter.Float64Histogram(
		"eshu_dp_ingestion_shared_lock_hold_duration_seconds",
		metric.WithDescription("Duration the ingestion commit holds the deferred-maintenance shared advisory barrier for one atomic scope/generation/fact commit"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5),
	)
	if err != nil {
		return nil, fmt.Errorf("register IngestionSharedLockHoldDuration histogram: %w", err)
	}

	// Deferred bootstrap backfill and reopen instruments
	backfillBuckets := []float64{0.1, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300}
	inst.DeferredBackfillDuration, err = meter.Float64Histogram(
		"eshu_dp_deferred_backfill_duration_seconds",
		metric.WithDescription("Duration of corpus-wide deferred backward evidence backfill"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(backfillBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeferredBackfillDuration histogram: %w", err)
	}

	inst.DeferredBackfillBatchDuration, err = meter.Float64Histogram(
		"eshu_dp_deferred_backfill_batch_duration_seconds",
		metric.WithDescription("Wall time of each per-repository-batch transaction inside the deferred backward evidence backfill. Lets an operator watch batch-by-batch backfill progress instead of waiting for the whole pass to return."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(backfillBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeferredBackfillBatchDuration histogram: %w", err)
	}

	inst.DeferredBackfillBatchesCompleted, err = meter.Int64Counter(
		"eshu_dp_deferred_backfill_batches_completed_total",
		metric.WithDescription("Total committed per-repository batches in the deferred backward evidence backfill. Rising during a pass is the operator-visible progress signal for the backfill long pole."),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeferredBackfillBatchesCompleted counter: %w", err)
	}

	inst.DeferredBackfillEvidence, err = meter.Int64Counter(
		"eshu_dp_deferred_backfill_evidence_total",
		metric.WithDescription("Total evidence facts discovered during deferred bootstrap backfill"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeferredBackfillEvidence counter: %w", err)
	}

	inst.DeferredBackfillPartitions, err = meter.Int64Counter(
		"eshu_dp_deferred_backfill_partitions_total",
		metric.WithDescription("Total (scope_id, generation_id) partitions the deferred backfill's per-scope fact load fanned out over. The per-pass increment sizes the fan-out width."),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeferredBackfillPartitions counter: %w", err)
	}

	inst.DeferredBackfillPartitionWorkers, err = meter.Int64Histogram(
		"eshu_dp_deferred_backfill_partition_workers",
		metric.WithDescription("Worker-pool saturation (concurrent worker count) used for the deferred backfill's per-scope fact-load fan-out. Lets an operator confirm the load ran concurrently and was not throttled to one."),
		metric.WithExplicitBucketBoundaries(1, 2, 4, 8, 16, 32),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeferredBackfillPartitionWorkers histogram: %w", err)
	}

	inst.DeferredBackfillPartitionLoadDuration, err = meter.Float64Histogram(
		"eshu_dp_deferred_backfill_partition_load_duration_seconds",
		metric.WithDescription("Wall time of each per-scope (scope_id, generation_id) deferred fact-load query. A long tail isolates the scope whose fact load dominates the pass."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(backfillBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeferredBackfillPartitionLoadDuration histogram: %w", err)
	}

	inst.DeferredBackfillPartitionsSkipped, err = meter.Int64Counter(
		"eshu_dp_deferred_backfill_partitions_skipped_total",
		metric.WithDescription("Total deferred backfill partitions skipped by the partition memo gate: a matching-fingerprint memo row exists and the partition is not ArgoCD-bearing (issue #3624 Track 1 / B'). A rising skip count with a stable catalog is the operator-visible steady-state signal."),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeferredBackfillPartitionsSkipped counter: %w", err)
	}

	inst.DeferredBackfillPartitionsLoaded, err = meter.Int64Counter(
		"eshu_dp_deferred_backfill_partitions_loaded_total",
		metric.WithDescription("Total deferred backfill partitions loaded despite a partition memo lookup, labeled by reason=memo_miss (no memo row, or the catalog fingerprint changed). ArgoCD-bearing partitions are excluded from the memo on the write side, so they surface here as memo_miss reloads (issue #3624 Track 1 / B')."),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeferredBackfillPartitionsLoaded counter: %w", err)
	}

	inst.DeploymentMappingReopened, err = meter.Int64Counter(
		"eshu_dp_deployment_mapping_reopened_total",
		metric.WithDescription("Total deployment_mapping work items reopened after deferred backfill"),
	)
	if err != nil {
		return nil, fmt.Errorf("register DeploymentMappingReopened counter: %w", err)
	}

	inst.CodeImportRepoEdgeReopened, err = meter.Int64Counter(
		"eshu_dp_code_import_repo_edge_reopened_total",
		metric.WithDescription("Total code_import_repo_edge work items reopened after deferred backfill"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CodeImportRepoEdgeReopened counter: %w", err)
	}

	inst.ReopenSkippedByPartitionMemo, err = meter.Int64Counter(
		"eshu_dp_reopen_skipped_by_partition_memo_total",
		metric.WithDescription("Total succeeded deployment_mapping/code_import_repo_edge reducer work items whose reopen was skipped because their partition's backward evidence is already committed under the current catalog fingerprint (issue #4770), labeled by domain and reason=catalog_unchanged."),
	)
	if err != nil {
		return nil, fmt.Errorf("register ReopenSkippedByPartitionMemo counter: %w", err)
	}

	inst.CorrelationReopened, err = meter.Int64Counter(
		"eshu_dp_correlation_reopened_total",
		metric.WithDescription("Total additive-correlation reducer work items reopened after deferred maintenance, keyed by domain"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CorrelationReopened counter: %w", err)
	}

	inst.IaCReachabilityMaterializationDuration, err = meter.Float64Histogram(
		"eshu_dp_iac_reachability_materialization_duration_seconds",
		metric.WithDescription("Duration of corpus-wide IaC reachability materialization"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(backfillBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register IaCReachabilityMaterializationDuration histogram: %w", err)
	}

	inst.IaCReachabilityRows, err = meter.Int64Counter(
		"eshu_dp_iac_reachability_rows_total",
		metric.WithDescription("Total IaC reachability rows materialized by reachability outcome"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IaCReachabilityRows counter: %w", err)
	}

	// Cross-repo resolution instruments
	crossRepoBuckets := []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30}
	inst.CrossRepoResolutionDuration, err = meter.Float64Histogram(
		"eshu_dp_cross_repo_resolution_duration_seconds",
		metric.WithDescription("Duration of cross-repo relationship resolution per generation"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(crossRepoBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register CrossRepoResolutionDuration histogram: %w", err)
	}

	inst.CrossRepoEvidenceLoaded, err = meter.Int64Counter(
		"eshu_dp_cross_repo_evidence_loaded_total",
		metric.WithDescription("Total evidence facts loaded for cross-repo resolution"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CrossRepoEvidenceLoaded counter: %w", err)
	}

	inst.CrossRepoEdgesResolved, err = meter.Int64Counter(
		"eshu_dp_cross_repo_edges_resolved_total",
		metric.WithDescription("Total dependency edges resolved from cross-repo evidence"),
	)
	if err != nil {
		return nil, fmt.Errorf("register CrossRepoEdgesResolved counter: %w", err)
	}

	inst.CrossRepoActivationFenced, err = meter.Int64Counter(
		"eshu_dp_cross_repo_activation_fenced_total",
		metric.WithDescription(
			"Total generations whose activation was fenced because durable graph-acceptance intents failed to commit",
		),
	)
	if err != nil {
		return nil, fmt.Errorf("register CrossRepoActivationFenced counter: %w", err)
	}

	inst.FluxCrossRepoURLResolution, err = meter.Int64Counter(
		"eshu_dp_flux_cross_repo_url_resolution_total",
		metric.WithDescription(
			"Total Flux GitRepository spec.url cross-repo resolution attempts by outcome (linked, unresolved, ambiguous, self)",
		),
	)
	if err != nil {
		return nil, fmt.Errorf("register FluxCrossRepoURLResolution counter: %w", err)
	}

	inst.RepoDependencyGateDecisions, err = meter.Int64Counter(
		"eshu_dp_repo_dependency_gate_decisions_total",
		metric.WithDescription(
			"Total repo-dependency activation gate decisions labeled by bounded decision (bypassed, deferred_inactive, deferred_error). Increments per key resolved, never per prefetch batch.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf("register RepoDependencyGateDecisions counter: %w", err)
	}

	inst.ContentEntityEmitted, err = meter.Int64Counter(
		"eshu_dp_content_entity_emitted_total",
		metric.WithDescription("Total content_entity facts streamed during collection, broken down by source_file_kind (code, package_manifest, config, other). Use to detect lockfile or config entity explosions without manual SQL."),
	)
	if err != nil {
		return nil, fmt.Errorf("register ContentEntityEmitted counter: %w", err)
	}

	bootstrapPhaseBuckets := []float64{1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800, 3600}
	inst.BootstrapPipelinePhaseDuration, err = meter.Float64Histogram(
		"eshu_dp_bootstrap_pipeline_phase_seconds",
		metric.WithDescription("Wall time of each named bootstrap pipeline phase (collection, projection, relationship_backfill, iac_reachability, config_state_drift, content_index_finalization). Use to find the long pole in a full-corpus run."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(bootstrapPhaseBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register BootstrapPipelinePhaseDuration histogram: %w", err)
	}

	// Per-collector claimed-service run duration: wide enough for sub-second
	// lightweight collectors and 30-minute heavyweight git snapshots, with fine
	// resolution at the low end so fast no-op cycles are visible.
	claimRunBuckets := []float64{0.1, 0.5, 1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800}
	inst.WorkflowClaimRunDuration, err = meter.Float64Histogram(
		"eshu_dp_workflow_claim_run_duration_seconds",
		metric.WithDescription("Wall time of one claimed-service processing cycle labeled by collector_kind, source_system, and outcome. Use to find the per-collector long pole in a full-corpus run."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(claimRunBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register WorkflowClaimRunDuration histogram: %w", err)
	}

	inst.WorkflowClaimFactsEmitted, err = meter.Int64Counter(
		"eshu_dp_workflow_claim_facts_emitted_total",
		metric.WithDescription("Total facts emitted per claimed-service run, labeled by collector_kind and source_system. Recorded from CollectedGeneration.FactCount on successful commit only."),
	)
	if err != nil {
		return nil, fmt.Errorf("register WorkflowClaimFactsEmitted counter: %w", err)
	}

	pipelineOverlapBuckets := []float64{1, 5, 10, 30, 60, 120, 300, 600, 1800}
	inst.PipelineOverlapDuration, err = meter.Float64Histogram(
		"eshu_dp_pipeline_overlap_seconds",
		metric.WithDescription("Time both collector and projector ran concurrently during bootstrap"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(pipelineOverlapBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register PipelineOverlapDuration histogram: %w", err)
	}

	shutdownDurationBuckets := []float64{0.5, 1, 2.5, 5, 10, 30, 60}
	inst.APIShutdownDuration, err = meter.Float64Histogram(
		"eshu_dp_shutdown_duration_seconds",
		metric.WithDescription("API HTTP server graceful shutdown duration, from signal received to process exit"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(shutdownDurationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("register APIShutdownDuration histogram: %w", err)
	}

	inst.StatusStageCountsCacheTotal, err = meter.Int64Counter(
		"eshu_dp_status_stage_counts_cache_total",
		metric.WithDescription(
			"Status-query stage-counts reads (activeFactWorkItemsCTE via "+
				"stageCountsQuery) by cache outcome: hit (served from the "+
				"in-memory TTL cache, no Postgres round trip), miss (cache cold "+
				"or expired, Postgres query ran and succeeded), or error "+
				"(Postgres query ran and failed; never cached).",
		),
	)
	if err != nil {
		return nil, fmt.Errorf("register StatusStageCountsCacheTotal counter: %w", err)
	}

	inst.OIDCBearerValidationTotal, err = meter.Int64Counter(
		"eshu_dp_oidc_bearer_validation_total",
		metric.WithDescription(
			"IdP bearer-token (Authorization: Bearer <access_token>) validation "+
				"outcomes by bounded outcome: valid, expired, wrong_audience, "+
				"unknown_issuer, bad_signature, malformed, jwks_fetch_failure, "+
				"no_grants.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf("register OIDCBearerValidationTotal counter: %w", err)
	}

	inst.CloudFormationPositionFallbacks, err = meter.Int64Counter(
		"eshu_dp_cloudformation_position_fallback_total",
		metric.WithDescription(
			"Total CloudFormation entities either format adapter's per-entity "+
				"position walk could not resolve — the YAML yaml.v3 Node.Line "+
				"walk (#5328) or the JSON ordered-entry walk (#5348) — by bounded "+
				"cloudformation_section and skip_reason.",
		),
	)
	if err != nil {
		return nil, fmt.Errorf("register CloudFormationPositionFallbacks counter: %w", err)
	}

	inst.IdentityCacheHitTotal, err = meter.Int64Counter(
		"eshu_dp_identity_cache_hit_total",
		metric.WithDescription("Total identity-fact cache hits"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IdentityCacheHitTotal counter: %w", err)
	}

	inst.IdentityCacheMissTotal, err = meter.Int64Counter(
		"eshu_dp_identity_cache_miss_total",
		metric.WithDescription("Total identity-fact cache misses (epoch changed → reload)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IdentityCacheMissTotal counter: %w", err)
	}

	inst.IdentityCacheReloadTotal, err = meter.Int64Counter(
		"eshu_dp_identity_cache_reload_total",
		metric.WithDescription("Total identity-fact cache reloads (singleflight leader)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IdentityCacheReloadTotal counter: %w", err)
	}

	inst.IdentityCachePassthroughTotal, err = meter.Int64Counter(
		"eshu_dp_identity_cache_passthrough_total",
		metric.WithDescription("Total identity-fact cache passthroughs (cap exceeded or mid-load commit)"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IdentityCachePassthroughTotal counter: %w", err)
	}

	inst.IdentityCacheReloadDuration, err = meter.Float64Histogram(
		"eshu_dp_identity_cache_reload_duration_seconds",
		metric.WithDescription("Duration of identity-fact cache reloads"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IdentityCacheReloadDuration histogram: %w", err)
	}

	inst.IdentityCacheProbeDuration, err = meter.Float64Histogram(
		"eshu_dp_identity_cache_probe_duration_seconds",
		metric.WithDescription("Duration of identity-fact epoch probe queries"),
	)
	if err != nil {
		return nil, fmt.Errorf("register IdentityCacheProbeDuration histogram: %w", err)
	}

	return inst, nil
}

// RegisterObservableGauges registers observable gauge instruments with their
// callback functions. This is separate from NewInstruments because the observer
// implementations may not be available at instrument creation time.
func RegisterObservableGauges(
	inst *Instruments,
	meter metric.Meter,
	queueObs QueueObserver,
	workerObs WorkerObserver,
) error {
	if inst == nil {
		return errors.New("instruments must not be nil")
	}
	if meter == nil {
		return errors.New("meter is required for observable gauges")
	}

	var err error

	if queueObs != nil {
		inst.QueueDepth, err = meter.Int64ObservableGauge(
			"eshu_dp_queue_depth",
			metric.WithDescription("Current queue depth by queue and status"),
			metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
				depths, err := queueObs.QueueDepths(ctx)
				if err != nil {
					return err
				}
				for queue, statuses := range depths {
					for status, count := range statuses {
						o.Observe(
							count,
							metric.WithAttributes(
								attribute.String("queue", queue),
								attribute.String("status", status),
							),
						)
					}
				}
				return nil
			}),
		)
		if err != nil {
			return fmt.Errorf("register QueueDepth gauge: %w", err)
		}

		inst.QueueOldestAge, err = meter.Float64ObservableGauge(
			"eshu_dp_queue_oldest_age_seconds",
			metric.WithDescription("Age of oldest queue item in seconds"),
			metric.WithUnit("s"),
			metric.WithFloat64Callback(func(ctx context.Context, o metric.Float64Observer) error {
				ages, err := queueObs.QueueOldestAge(ctx)
				if err != nil {
					return err
				}
				for queue, age := range ages {
					o.Observe(
						age,
						metric.WithAttributes(
							attribute.String("queue", queue),
						),
					)
				}
				return nil
			}),
		)
		if err != nil {
			return fmt.Errorf("register QueueOldestAge gauge: %w", err)
		}

		if sourceObs, ok := queueObs.(SourceQueueObserver); ok {
			inst.SourceQueueDepth, err = meter.Int64ObservableGauge(
				"eshu_dp_queue_source_depth",
				metric.WithDescription("Current queue depth by queue, source system, and status"),
				metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
					depths, err := sourceObs.SourceQueueDepths(ctx)
					if err != nil {
						return err
					}
					for queue, sources := range depths {
						for sourceSystem, statuses := range sources {
							for status, count := range statuses {
								o.Observe(
									count,
									metric.WithAttributes(
										attribute.String("queue", queue),
										attribute.String(MetricDimensionSourceSystem, sourceSystem),
										attribute.String(MetricDimensionStatus, status),
									),
								)
							}
						}
					}
					return nil
				}),
			)
			if err != nil {
				return fmt.Errorf("register SourceQueueDepth gauge: %w", err)
			}

			inst.SourceQueueOldestAge, err = meter.Float64ObservableGauge(
				"eshu_dp_queue_source_oldest_age_seconds",
				metric.WithDescription("Age of oldest queue item in seconds by queue and source system"),
				metric.WithUnit("s"),
				metric.WithFloat64Callback(func(ctx context.Context, o metric.Float64Observer) error {
					ages, err := sourceObs.SourceQueueOldestAge(ctx)
					if err != nil {
						return err
					}
					for queue, sources := range ages {
						for sourceSystem, age := range sources {
							o.Observe(
								age,
								metric.WithAttributes(
									attribute.String("queue", queue),
									attribute.String(MetricDimensionSourceSystem, sourceSystem),
								),
							)
						}
					}
					return nil
				}),
			)
			if err != nil {
				return fmt.Errorf("register SourceQueueOldestAge gauge: %w", err)
			}
		}
	}

	if workerObs != nil {
		inst.WorkerPoolActive, err = meter.Int64ObservableGauge(
			"eshu_dp_worker_pool_active",
			metric.WithDescription("Current active worker count per pool"),
			metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
				counts, err := workerObs.ActiveWorkers(ctx)
				if err != nil {
					return err
				}
				for pool, count := range counts {
					o.Observe(
						count,
						metric.WithAttributes(
							attribute.String("pool", pool),
						),
					)
				}
				return nil
			}),
		)
		if err != nil {
			return fmt.Errorf("register WorkerPoolActive gauge: %w", err)
		}
	}

	return nil
}

// RegisterAcceptanceObservableGauges registers acceptance-specific observable
// gauges backed by the supplied observer.
func RegisterAcceptanceObservableGauges(inst *Instruments, meter metric.Meter, acceptanceObs AcceptanceObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if acceptanceObs == nil {
		return nil
	}

	var err error
	inst.SharedAcceptanceRows, err = meter.Int64ObservableGauge(
		"eshu_dp_shared_acceptance_rows",
		metric.WithDescription("Current durable shared acceptance row count"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			rows, err := acceptanceObs.AcceptanceRowCount(ctx)
			if err != nil {
				return err
			}
			o.Observe(rows)
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register SharedAcceptanceRows gauge: %w", err)
	}

	return nil
}

// RegisterGraphOrphanObservableGauge registers the graph orphan count gauge.
func RegisterGraphOrphanObservableGauge(inst *Instruments, meter metric.Meter, observer GraphOrphanObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if observer == nil {
		return nil
	}

	var err error
	inst.GraphOrphanNodes, err = meter.Int64ObservableGauge(
		"eshu_dp_graph_orphan_nodes",
		metric.WithDescription("Current bounded zero-relationship graph node count by closed node label"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			counts, err := observer.GraphOrphanNodeCounts(ctx)
			if err != nil {
				return err
			}
			for label, count := range counts {
				o.Observe(count, metric.WithAttributes(
					attribute.String(MetricDimensionNodeLabel, label),
				))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register GraphOrphanNodes gauge: %w", err)
	}
	return nil
}

// RegisterActiveGenerationAgeObservableGauge registers the active-generation
// age-bucket gauge. The callback runs read-only on the meter collection
// goroutine and is a no-op when observer is nil so binaries without a liveness
// store skip it. The "stuck" bucket doubles as the wedged-generation alarm
// signal: a non-zero value means active generations are eligible for recovery.
func RegisterActiveGenerationAgeObservableGauge(inst *Instruments, meter metric.Meter, observer ActiveGenerationAgeObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if observer == nil {
		return nil
	}

	var err error
	inst.ActiveGenerationsByAge, err = meter.Int64ObservableGauge(
		"eshu_dp_active_generations",
		metric.WithDescription("Current active scope generation count by closed activation-age bucket (fresh, aging, stuck)"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			counts, err := observer.ActiveGenerationsByAge(ctx)
			if err != nil {
				return err
			}
			for bucket, count := range counts {
				o.Observe(count, metric.WithAttributes(
					attribute.String(MetricDimensionAgeBucket, bucket),
				))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register ActiveGenerationsByAge gauge: %w", err)
	}
	return nil
}

// RegisterPoisonLivenessObservableGauges registers the dead-letter/poison-class
// gauges (#4740). The callbacks run read-only on the meter collection goroutine
// and are a no-op when observer is nil so binaries without a poison-liveness
// store skip them. A non-zero PoisonDeadLetterScopes/Items is the alarm signal
// that a scope has permanently wedged: dead_letter is terminal and unclaimable,
// and the generation-liveness sweep does not reach it because the scope has no
// ACTIVE generation at all.
func RegisterPoisonLivenessObservableGauges(inst *Instruments, meter metric.Meter, observer PoisonLivenessObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if observer == nil {
		return nil
	}

	var err error
	inst.PoisonDeadLetterScopes, err = meter.Int64ObservableGauge(
		"eshu_dp_poison_dead_letter_scopes",
		metric.WithDescription("Current count of distinct scopes stuck in the dead-letter/poison class (dead_letter with no newer generation)"),
	)
	if err != nil {
		return fmt.Errorf("register PoisonDeadLetterScopes gauge: %w", err)
	}

	inst.PoisonDeadLetterItems, err = meter.Int64ObservableGauge(
		"eshu_dp_poison_dead_letter_items",
		metric.WithDescription("Current count of fact_work_items rows stuck in the dead-letter/poison class (dead_letter with no newer generation)"),
	)
	if err != nil {
		return fmt.Errorf("register PoisonDeadLetterItems gauge: %w", err)
	}

	inst.PoisonDeadLetterOldestAgeSeconds, err = meter.Float64ObservableGauge(
		"eshu_dp_poison_dead_letter_oldest_age_seconds",
		metric.WithDescription("Age in seconds of the oldest dead-letter/poison-class item, zero when the class is empty"),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("register PoisonDeadLetterOldestAgeSeconds gauge: %w", err)
	}

	// Single callback across all three gauges so one meter scrape runs the
	// poison-count query once (not once per gauge), matching the "one query per
	// snapshot" idiom of the sibling active-generation gauge.
	if _, err = meter.RegisterCallback(func(ctx context.Context, o metric.Observer) error {
		scopes, items, oldestAgeSeconds, err := observer.PoisonDeadLetterCounts(ctx)
		if err != nil {
			return err
		}
		o.ObserveInt64(inst.PoisonDeadLetterScopes, scopes)
		o.ObserveInt64(inst.PoisonDeadLetterItems, items)
		o.ObserveFloat64(inst.PoisonDeadLetterOldestAgeSeconds, oldestAgeSeconds)
		return nil
	}, inst.PoisonDeadLetterScopes, inst.PoisonDeadLetterItems, inst.PoisonDeadLetterOldestAgeSeconds); err != nil {
		return fmt.Errorf("register poison liveness gauge callback: %w", err)
	}
	return nil
}

// RegisterWorkflowFamilyQueueDepthObservableGauge registers the per-family
// claim-aware collector queue-depth gauge. The callback runs read-only on the
// meter collection goroutine. It is a no-op when observer is nil so binaries
// without a workflow control store skip it.
func RegisterWorkflowFamilyQueueDepthObservableGauge(inst *Instruments, meter metric.Meter, observer WorkflowFamilyQueueDepthObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if observer == nil {
		return nil
	}

	var err error
	inst.WorkflowFamilyQueueDepth, err = meter.Int64ObservableGauge(
		"eshu_dp_workflow_family_queue_depth",
		metric.WithDescription("Outstanding claim-aware collector work-item count by collector_kind, source_system, and status"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			depths, err := observer.WorkflowFamilyQueueDepths(ctx)
			if err != nil {
				return err
			}
			for collectorKind, sources := range depths {
				for sourceSystem, statuses := range sources {
					for status, count := range statuses {
						o.Observe(count, metric.WithAttributes(
							AttrCollectorKind(collectorKind),
							AttrSourceSystem(sourceSystem),
							AttrStatus(status),
						))
					}
				}
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register WorkflowFamilyQueueDepth gauge: %w", err)
	}
	return nil
}

// RegisterAWSClaimConcurrencyGauge registers the AWS active-claim gauge.
func RegisterAWSClaimConcurrencyGauge(
	inst *Instruments,
	meter metric.Meter,
	observer AWSClaimConcurrencyObserver,
) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if observer == nil {
		return nil
	}

	var err error
	inst.AWSClaimConcurrency, err = meter.Int64ObservableGauge(
		"eshu_dp_aws_claim_concurrency",
		metric.WithDescription("Current active AWS collector claims by account"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			counts, err := observer.AWSClaimConcurrency(ctx)
			if err != nil {
				return err
			}
			for account, count := range counts {
				o.Observe(count, metric.WithAttributes(AttrAccount(account)))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register AWSClaimConcurrency gauge: %w", err)
	}
	return nil
}

// AttrScopeID returns a scope_id attribute for metric recording.
func AttrScopeID(v string) attribute.KeyValue {
	return attribute.String(LogKeyScopeID, v)
}

// AttrScopeKind returns a scope_kind attribute for metric recording.
func AttrScopeKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionScopeKind, v)
}

// AttrSource returns a source attribute for metric recording.
func AttrSource(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSource, v)
}

// AttrSourceClass returns a source_class attribute for metric recording.
func AttrSourceClass(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSourceClass, v)
}

// AttrSourceSystem returns a source_system attribute for metric recording.
func AttrSourceSystem(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSourceSystem, v)
}

// AttrFieldClass returns a field_class attribute for redaction metrics. Callers
// MUST pass a bounded FieldClass* value naming the redacted field shape, never
// the redacted content.
func AttrFieldClass(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionFieldClass, v)
}

// AttrGenerationID returns a generation_id attribute for span or structured log
// recording. generation_id is NOT a metric dimension label (removed per #3943);
// use bounded alternatives like scope_kind for metric labels.
func AttrGenerationID(v string) attribute.KeyValue {
	return attribute.String(LogKeyGenerationID, v)
}

// AttrCollectorKind returns a collector_kind attribute for metric recording.
func AttrCollectorKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionCollectorKind, v)
}

// AttrAnalyzer returns an analyzer attribute for scanner-worker metrics.
func AttrAnalyzer(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionAnalyzer, v)
}

// AttrTargetKind returns a target_kind attribute for scanner-worker metrics.
func AttrTargetKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionTargetKind, v)
}

// AttrLimitKind returns a limit_kind attribute for scanner-worker metrics.
func AttrLimitKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionLimitKind, v)
}

// AttrDomain returns a domain attribute for metric recording.
func AttrDomain(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionDomain, v)
}

// AttrPartitionKey returns a partition_key attribute for metric recording.
func AttrPartitionKey(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionPartitionKey, v)
}

// AttrPartitionID returns a partition_id attribute for per-(domain, partition)
// shared-projection histograms. The value is the 0-based partition slot, which
// is bounded by ESHU_SHARED_PROJECTION_PARTITION_COUNT (operator-configured,
// ≤64 by convention). Never use raw intent, scope, or generation identifiers
// here.
func AttrPartitionID(v int) attribute.KeyValue {
	return attribute.Int(MetricDimensionPartitionID, v)
}

// AttrRunner returns a runner attribute for metric recording.
func AttrRunner(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionRunner, v)
}

// AttrLookupResult returns a lookup_result attribute for metric recording.
func AttrLookupResult(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionLookupResult, v)
}

// AttrErrorType returns an error_type attribute for metric recording.
func AttrErrorType(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionErrorType, v)
}

// AttrRepoSizeTier returns a repo_size_tier attribute for metric recording.
func AttrRepoSizeTier(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionRepoSizeTier, v)
}

// AttrSkipReason returns a skip_reason attribute for discovery skip metrics.
func AttrSkipReason(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSkipReason, v)
}

// AttrNodeType returns a node_type attribute for canonical write metrics.
func AttrNodeType(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionNodeType, v)
}

// AttrEdgeType returns an edge_type attribute for canonical write metrics.
func AttrEdgeType(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionEdgeType, v)
}

// AttrRelationshipType returns a relationship_type attribute for the AWS
// relationship edge projection counter.
func AttrRelationshipType(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionRelationshipType, v)
}

// AttrJoinMode returns a join_mode attribute for the AWS relationship edge
// projection counter (arn / bare_id / correlation_anchor / unresolved).
func AttrJoinMode(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionJoinMode, v)
}

// AttrOwnershipFamily returns a family attribute for the #5007 cross-scope
// ownership contention counter (cloud_resource / ec2_instance /
// kubernetes_workload).
func AttrOwnershipFamily(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionOwnershipFamily, v)
}

// AttrCoverageSignal returns a coverage_signal attribute for the observability
// coverage correlation counter (alarm / composite_alarm / dashboard / log_group
// / trace_sampling).
func AttrCoverageSignal(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionCoverageSignal, v)
}

// AttrResolutionMode returns a resolution_mode attribute for the observability
// coverage COVERS edge projection counter (arn / bare_id / correlation_anchor).
func AttrResolutionMode(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionResolutionMode, v)
}

// AttrPrincipalKind returns a principal_kind attribute for the IAM CAN_ASSUME
// edge projection counter (role / user).
func AttrPrincipalKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionPrincipalKind, v)
}

// AttrEndpointKind returns an endpoint_kind attribute for the security-group
// endpoint node materialization counter (cidr_block / prefix_list).
func AttrEndpointKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionEndpointKind, v)
}

// AttrDriftKind returns a drift_kind attribute for drift-classification
// counters such as the live Kubernetes correlation counter (in_sync /
// image_drift / missing_source / stale_source / unknown).
func AttrDriftKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionDriftKind, v)
}

// AttrWritePhase returns a write_phase attribute for canonical phase metrics.
func AttrWritePhase(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionWritePhase, v)
}

// AttrOutcome returns an outcome attribute for metric recording.
func AttrOutcome(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionOutcome, v)
}

// AttrGuardrail returns a guardrail attribute for metric recording.
func AttrGuardrail(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionGuardrail, v)
}

// AttrPolicyID returns a policy_id attribute for bounded policy counters.
func AttrPolicyID(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionPolicyID, v)
}

// AttrEvidenceClass returns an evidence_class attribute for search and
// evaluation metrics. The value must be a bounded evidence family.
func AttrEvidenceClass(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionEvidenceClass, v)
}

// AttrBackendKind returns a backend_kind attribute for metric recording.
func AttrBackendKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionBackendKind, v)
}

// AttrConfidence returns a confidence attribute for reducer read-model metrics.
func AttrConfidence(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionConfidence, v)
}

// AttrRiskType returns a risk_type attribute for posture-observation metrics.
func AttrRiskType(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionRiskType, v)
}

// AttrSeverity returns a severity attribute for posture-observation metrics.
func AttrSeverity(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSeverity, v)
}

// AttrResult returns a result attribute for metric recording.
func AttrResult(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionResult, v)
}

// AttrReason returns a reason attribute for metric recording.
func AttrReason(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionReason, v)
}

// AttrMCPMethod returns an mcp_method attribute for
// eshu_dp_mcp_transport_auth_denied_total. v must be a bounded JSON-RPC
// method name or one of "sse", "other", "unknown".
func AttrMCPMethod(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionMCPMethod, v)
}

// AttrKind returns a kind attribute for metric recording.
func AttrKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionKind, v)
}

// AttrAction returns an action attribute for metric recording.
func AttrAction(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionAction, v)
}

// AttrAuthPath returns an auth_path attribute for eshu_dp_gcp_freshness_events_total
// producers. v must be one of "shared_token", "oidc", "none", or "n/a" —
// every producer of that counter must set this attribute so all series share
// one label set.
func AttrAuthPath(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionAuthPath, v)
}

// AttrProvider returns a provider attribute for metric recording.
func AttrProvider(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionProvider, v)
}

// AttrProviderKind returns a provider_kind attribute for metric recording.
func AttrProviderKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionProviderKind, v)
}

// AttrProviderProfileClass returns a provider_profile_class attribute for
// semantic extraction metric recording.
func AttrProviderProfileClass(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionProviderProfileClass, v)
}

// AttrEventKind returns an event_kind attribute for webhook listener metrics.
func AttrEventKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionEventKind, v)
}

// AttrDecision returns a bounded-decision attribute for metric recording
// (webhook listener, repo-dependency activation gate, and other
// decision-classification call sites).
func AttrDecision(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionDecision, v)
}

// AttrStatus returns a status attribute for metric recording.
func AttrStatus(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionStatus, v)
}

// AttrFailureClass returns a failure_class attribute for metric recording.
func AttrFailureClass(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionFailureClass, v)
}

// AttrOperation returns an operation attribute for metric recording.
func AttrOperation(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionOperation, v)
}

// AttrGate returns a gate attribute for graph-write backpressure metric
// recording. v must be one of the closed gate-class values ("canonical" or
// "semantic"; see graphbackpressure.CanonicalGateName and
// graphbackpressure.SemanticGateName), never a raw operation or statement name
// (issue #4448).
func AttrGate(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionGate, v)
}

// AttrResourceScope returns a resource_scope attribute for Kubernetes live
// metrics. The value must be a bounded resource family, never namespace or
// object names.
func AttrResourceScope(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionResourceScope, v)
}

// AttrFactKind returns a fact_kind attribute for metric recording.
func AttrFactKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionFactKind, v)
}

// Bounded source_file_kind values for ContentEntityEmitted. Producers MUST
// use exactly these constants; the metric must stay low-cardinality.
const (
	// SourceFileKindCode represents an ordinary source file parsed by the
	// language engine (no artifact_type set by the parser and no
	// dependency-manifest metadata).
	SourceFileKindCode = "code"
	// SourceFileKindPackageManifest represents a dependency manifest or
	// lockfile entity (go.mod, package-lock.json, Cargo.lock, requirements.txt,
	// pom.xml, *.csproj, etc.). The git parser does NOT set artifact_type for
	// these files; the manifest signal lives in entity metadata instead. A
	// manifest dependency entity is emitted with entity_type "Variable" and
	// metadata config_kind "dependency" (the exact pair the reducer's
	// extractPackageManifestDependencies admits in
	// internal/reducer/package_consumption_correlation.go). This is the bucket
	// that surfaces a lockfile content_entity explosion (issue #3676).
	SourceFileKindPackageManifest = "package_manifest"
	// SourceFileKindConfig represents an infra or config artifact. Classified
	// from the artifact_type tokens the git parser actually emits via
	// inferArtifactType / persistedArtifactType (internal/parser:
	// templated_detection.go): Dockerfile, docker-compose, Terraform HCL and
	// templates, Helm/Go/Jinja templated YAML, GitHub Actions workflows,
	// Ansible files, and nginx/apache/generic config families.
	SourceFileKindConfig = "config"
	// SourceFileKindOther represents any other artifact_type value returned by
	// the parser that does not map to code, manifest, or config.
	SourceFileKindOther = "other"
)

// SourceFileKinds returns the bounded, ordered set of source_file_kind label
// values. Producers MUST iterate this set (rather than dynamic map keys) when
// emitting per-kind metrics or log fields so the dimension space stays
// statically bounded and a stray classification can never leak a new label.
func SourceFileKinds() []string {
	return []string{
		SourceFileKindCode,
		SourceFileKindPackageManifest,
		SourceFileKindConfig,
		SourceFileKindOther,
	}
}

// ConfigKindDependency is the entity-metadata config_kind value the git
// dependency parsers set on a package-manifest dependency entity. It is the
// exact value the reducer's extractPackageManifestDependencies admits, so the
// telemetry classifier keys on the same signal as supply-chain truth.
const ConfigKindDependency = "dependency"

// EntityTypeVariable is the content-entity type the git dependency parsers emit
// for manifest dependency rows (they land in the parser "variables" bucket,
// labeled "Variable"). The reducer requires entity_type == "Variable" before
// admitting a package-manifest dependency, so the classifier requires it too.
const EntityTypeVariable = "Variable"

// ContentEntitySourceFileKind classifies a content entity into one of the
// bounded SourceFileKind* constants so eshu_dp_content_entity_emitted_total
// stays low-cardinality. It mirrors the real parser/reducer data path:
//
//   - package_manifest: entity_type "Variable" AND config_kind "dependency"
//     (the dependency-manifest signal the git parsers set in entity metadata;
//     artifact_type is empty for these files, so it can NOT be used). This is
//     the exact pair the reducer admits as a package-manifest dependency.
//   - config: a non-empty artifact_type emitted by inferArtifactType /
//     persistedArtifactType for IaC/config/templated files.
//   - code: no artifact_type and no manifest metadata (ordinary source).
//   - other: a non-empty artifact_type that is not a known config token.
//
// Callers MUST use this function rather than deriving the label themselves.
func ContentEntitySourceFileKind(entityType, artifactType, configKind string) string {
	// Manifest detection comes first: dependency entities carry no
	// artifact_type, so the metadata signal is the only reliable one.
	if entityType == EntityTypeVariable && configKind == ConfigKindDependency {
		return SourceFileKindPackageManifest
	}
	if artifactType == "" {
		return SourceFileKindCode
	}
	if isConfigArtifactType(artifactType) {
		return SourceFileKindConfig
	}
	return SourceFileKindOther
}

// isConfigArtifactType reports whether artifactType is one of the config/infra
// tokens the git parser actually emits. The set is aligned with the switch arms
// of inferArtifactType and persistedArtifactType in
// internal/parser/templated_detection.go; tokens those functions never produce
// (terraform, helm_chart, argocd, kustomize, cloudformation, bare "ansible")
// are intentionally absent because they would be dead labels.
func isConfigArtifactType(artifactType string) bool {
	switch artifactType {
	// Container / orchestration config.
	case "dockerfile", "docker_compose", "github_actions_workflow":
		return true
	// Terraform HCL and Terraform templates.
	case "terraform_hcl", "terraform_template_text":
		return true
	// Persisted templated-YAML buckets (persistedArtifactType). A plain
	// (untemplated) YAML document persists with an empty artifact_type and is
	// intentionally classified as code, not config.
	case "helm_helper_tpl", "go_template_yaml", "jinja_yaml":
		return true
	// Jinja/text/YAML templates from inferArtifactType.
	case "yaml_template", "jinja_text_template", "text_template":
		return true
	// Web-server and generic config families (inferArtifactType).
	case "nginx_config", "nginx_config_template",
		"apache_config", "apache_config_template",
		"generic_config", "generic_config_template":
		return true
	// Ansible family (ansibleArtifactType).
	case "ansible_inventory", "ansible_vars", "ansible_playbook",
		"ansible_role", "ansible_task_entrypoint":
		return true
	default:
		return false
	}
}

// AttrSourceFileKind returns a source_file_kind attribute for metric
// recording. The value MUST be a SourceFileKind* constant.
func AttrSourceFileKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSourceFileKind, v)
}

// Bounded bootstrap_phase values for BootstrapPipelinePhaseDuration. Producers
// MUST use exactly these constants.
const (
	// BootstrapPhaseCollection is the concurrent collector + fact-emission
	// phase (drainCollector).
	BootstrapPhaseCollection = "collection"
	// BootstrapPhaseProjection is the source-local projector drain phase
	// (drainProjectorPipelined).
	BootstrapPhaseProjection = "projection"
	// BootstrapPhaseRelationshipBackfill is the deferred relationship
	// evidence backfill phase (BackfillAllRelationshipEvidence).
	BootstrapPhaseRelationshipBackfill = "relationship_backfill"
	// BootstrapPhaseIaCReachability is the IaC reachability materialization
	// phase (MaterializeIaCReachability).
	BootstrapPhaseIaCReachability = "iac_reachability"
	// BootstrapPhaseDeploymentReopen is the deployment_mapping work-item reopen
	// phase (ReopenDeploymentMappingWorkItems). It runs after IaC reachability
	// and before config-state drift; without its own phase it would be an
	// unaccounted gap that could not be flagged as a long pole.
	BootstrapPhaseDeploymentReopen = "deployment_reopen"
	// BootstrapPhaseCodeImportReopen is the code_import_repo_edge work-item reopen
	// phase (ReopenCodeImportRepoEdgeWorkItems). It runs alongside the
	// deployment_mapping reopen so a code-import projection that resolved no owner
	// before the cross-scope package-registry facts landed is replayed once they
	// exist; without its own phase its duration would be an unaccounted gap.
	BootstrapPhaseCodeImportReopen = "code_import_repo_edge_reopen"
	// BootstrapPhaseConfigStateDrift is the config-state drift intent
	// enqueue phase (EnqueueConfigStateDriftIntents).
	BootstrapPhaseConfigStateDrift = "config_state_drift"
	// BootstrapPhaseContentIndexFinalization is the post-drain exact content
	// substring index build, validation, and ANALYZE phase.
	BootstrapPhaseContentIndexFinalization = "content_index_finalization"
)

// AttrBootstrapPhase returns a bootstrap_phase attribute for metric recording.
// The value MUST be a BootstrapPhase* constant.
func AttrBootstrapPhase(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionBootstrapPhase, v)
}

// AttrStatusClass returns a status_class attribute for metric recording.
func AttrStatusClass(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionStatusClass, v)
}

// AttrRoute returns a route attribute for metric recording. The value is the
// matched low-cardinality route pattern (e.g. "GET /api/v0/iac/resources"),
// never a concrete request path with identifiers.
func AttrRoute(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionRoute, v)
}

// AttrService returns a service attribute for cloud-provider metrics.
func AttrService(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionService, v)
}

// AttrAccount returns an account attribute for cloud-provider metrics.
func AttrAccount(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionAccount, v)
}

// AttrRegion returns a region attribute for cloud-provider metrics.
func AttrRegion(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionRegion, v)
}

// AttrMediaFamily returns a media_family attribute for metric recording.
func AttrMediaFamily(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionMediaFamily, v)
}

// AttrArtifactFamily returns an artifact_family attribute for metric recording.
func AttrArtifactFamily(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionArtifactFamily, v)
}

// AttrSafeLocatorHash returns a safe_locator_hash attribute for Terraform-state
// metrics. The value is the scope-level hash; raw locators must never be used.
func AttrSafeLocatorHash(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionSafeLocatorHash, v)
}

// AttrWarningKind returns a warning_kind attribute for Terraform-state warning
// metrics.
func AttrWarningKind(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionWarningKind, v)
}

// AttrResourceType returns a resource_type attribute for Terraform-resource
// counters such as eshu_dp_drift_schema_unknown_composite_total. The label is
// bounded by the schema bundle, so cardinality stays under the operator-
// visible cap; high-cardinality companions (attribute_key, source path) stay
// in the structured log.
func AttrResourceType(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionResourceType, v)
}

// AttrBudgetState returns a budget_state attribute for semantic extraction
// metrics.
func AttrBudgetState(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionBudgetState, v)
}

// AttrBudgetReason returns a budget_reason attribute for semantic extraction
// metrics.
func AttrBudgetReason(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionBudgetReason, v)
}

// AttrCompositeSkipReason returns a reason attribute for
// eshu_dp_drift_schema_unknown_composite_total. The value must come from the
// closed enum in terraformstate (CompositeCaptureSkipReason* constants) so
// cardinality stays bounded.
func AttrCompositeSkipReason(v string) attribute.KeyValue {
	return attribute.String(MetricDimensionCompositeSkipReason, v)
}

// RegisterTfstateSchemaResolverEntries registers the
// eshu_dp_tfstate_schema_resolver_entries observable gauge. The supplied
// counter is invoked on every scrape and must report the number of Terraform
// resource types currently held in memory by the collector's provider-schema
// resolver. Operators read this gauge to size the collector pod for the
// startup-loaded schema footprint; the resolver is loaded once at startup and
// held for the process lifetime, so the value is stable per process.
//
// A nil meter or nil counter returns nil without registering anything; the
// caller is responsible for skipping the call when the runtime resolver does
// not implement SchemaResolverEntryCounter.
func RegisterTfstateSchemaResolverEntries(meter metric.Meter, counter func() int) error {
	if meter == nil || counter == nil {
		return nil
	}
	_, err := meter.Int64ObservableGauge(
		"eshu_dp_tfstate_schema_resolver_entries",
		metric.WithDescription("Number of Terraform resource types covered by the loaded provider-schema resolver in the collector process"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(counter()))
			return nil
		}),
	)
	return err
}

// RecordGOMEMLIMIT registers and records the applied GOMEMLIMIT as a gauge.
// Call once at startup after instruments and memlimit are configured.
func RecordGOMEMLIMIT(meter metric.Meter, limitBytes int64) error {
	if meter == nil {
		return nil
	}
	_, err := meter.Int64ObservableGauge(
		"eshu_dp_gomemlimit_bytes",
		metric.WithDescription("Configured GOMEMLIMIT in bytes"),
		metric.WithUnit("By"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(limitBytes)
			return nil
		}),
	)
	return err
}

// RegisterEdgesBySourceToolObservableGauge registers the extraction-provenance
// edge count gauge. The callback runs read-only on the meter collection
// goroutine and is a no-op when observer is nil. It is a no-op when observer
// is nil so binaries without a graph read port skip it. The source_tool label
// is bounded by sourcetool.Canonical; any value not in that set is coerced to
// "unknown" so the time series set stays closed even if the graph holds a
// stale or unrecognised token.
func RegisterEdgesBySourceToolObservableGauge(inst *Instruments, meter metric.Meter, observer EdgesBySourceToolObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if observer == nil {
		return nil
	}

	var err error
	inst.EdgesBySourceTool, err = meter.Int64ObservableGauge(
		"eshu_dp_edges_by_source_tool",
		metric.WithDescription("Current exact graph edge count by closed source_tool label, summed across the Tier-2 relationship types that carry source_tool (relationship-type-index answered, no row sampling)"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			counts, err := observer.EdgesBySourceTool(ctx)
			if err != nil {
				return err
			}
			// Coalesce before observing: multiple out-of-vocabulary tokens (or a
			// real "unknown" plus a stale token) all coerce to source_tool="unknown",
			// and an observable gauge must emit exactly one observation per distinct
			// label set per callback — duplicate sets corrupt the series.
			coalesced := make(map[string]int64, len(counts))
			for tool, count := range counts {
				label := tool
				if !sourcetool.IsValid(tool) {
					label = "unknown"
				}
				coalesced[label] += count
			}
			for label, count := range coalesced {
				o.Observe(count, metric.WithAttributes(AttrSourceTool(label)))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register EdgesBySourceTool gauge: %w", err)
	}
	return nil
}

// RegisterFilesByLanguageObservableGauge registers the extraction-provenance
// file count gauge. The callback runs read-only on the meter collection
// goroutine and is a no-op when observer is nil so binaries without a graph
// read port skip it. The language label is bounded by the parser registry
// (set at ingest time); empty strings are skipped.
func RegisterFilesByLanguageObservableGauge(inst *Instruments, meter metric.Meter, observer FilesByLanguageObserver) error {
	if inst == nil {
		return errors.New("instruments are required")
	}
	if meter == nil {
		return errors.New("meter is required")
	}
	if observer == nil {
		return nil
	}

	var err error
	inst.FilesByLanguage, err = meter.Int64ObservableGauge(
		"eshu_dp_files_by_language",
		metric.WithDescription("Current exact File node count by language (File-label-anchored group; ESHU_GRAPH_COUNT_LIMIT bounds the returned language groups, not the rows counted)"),
		metric.WithInt64Callback(func(ctx context.Context, o metric.Int64Observer) error {
			counts, err := observer.FilesByLanguage(ctx)
			if err != nil {
				return err
			}
			for lang, count := range counts {
				if lang == "" {
					continue
				}
				o.Observe(count, metric.WithAttributes(AttrLanguage(lang)))
			}
			return nil
		}),
	)
	if err != nil {
		return fmt.Errorf("register FilesByLanguage gauge: %w", err)
	}
	return nil
}
