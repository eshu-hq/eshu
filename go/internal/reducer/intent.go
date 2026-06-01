// Package reducer defines the durable cross-source and cross-scope reducer
// substrate used by the Go data plane.
package reducer

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Domain identifies a canonical shared-truth reducer domain.
type Domain string

const (
	// DomainWorkloadIdentity resolves canonical workload identity.
	DomainWorkloadIdentity Domain = "workload_identity"
	// DomainDeployableUnitCorrelation correlates cross-source deployable-unit
	// evidence before workload admission and materialization.
	DomainDeployableUnitCorrelation Domain = "deployable_unit_correlation"
	// DomainCloudAssetResolution resolves canonical cloud asset identity.
	DomainCloudAssetResolution Domain = "cloud_asset_resolution"
	// DomainDeploymentMapping resolves deployment relationships.
	DomainDeploymentMapping Domain = "deployment_mapping"
	// DomainDataLineage resolves lineage across sources and scopes.
	DomainDataLineage Domain = "data_lineage"
	// DomainOwnership resolves ownership and responsibility records.
	DomainOwnership Domain = "ownership"
	// DomainGovernance resolves governance and policy attribution.
	DomainGovernance Domain = "governance"
	// DomainWorkloadMaterialization materializes canonical workload graph nodes.
	DomainWorkloadMaterialization Domain = "workload_materialization"
	// DomainCodeCallMaterialization materializes canonical code call edges.
	DomainCodeCallMaterialization Domain = "code_call_materialization"
	// DomainSemanticEntityMaterialization materializes Annotation, Typedef,
	// TypeAlias, and Component semantic nodes.
	DomainSemanticEntityMaterialization Domain = "semantic_entity_materialization"
	// DomainSQLRelationshipMaterialization materializes canonical SQL
	// relationship edges (REFERENCES_TABLE, HAS_COLUMN, TRIGGERS).
	DomainSQLRelationshipMaterialization Domain = "sql_relationship_materialization"
	// DomainInheritanceMaterialization materializes canonical inheritance,
	// override, and alias edges from parser entity bases and trait adaptation
	// metadata.
	DomainInheritanceMaterialization Domain = "inheritance_materialization"
	// DomainConfigStateDrift correlates Terraform config (parsed HCL) against
	// Terraform state to detect five drift kinds. Cross-source, cross-scope,
	// non-canonical-write — counters and structured logs are the v1 surface.
	// Current proof gates are documented in docs/public/reference/local-testing.md
	// under "Terraform Config-vs-State Drift Compose Proofs".
	DomainConfigStateDrift Domain = "config_state_drift"
	// DomainPackageSourceCorrelation classifies package-registry source hints
	// against active repository remotes without promoting package ownership.
	DomainPackageSourceCorrelation Domain = "package_source_correlation"
	// DomainContainerImageIdentity joins Git, registry, and runtime image
	// evidence into digest-keyed container image identity decisions.
	DomainContainerImageIdentity Domain = "container_image_identity"
	// DomainCICDRunCorrelation correlates provider CI/CD runs, artifacts, and
	// environment observations with reducer-owned artifact identity evidence.
	DomainCICDRunCorrelation Domain = "ci_cd_run_correlation"
	// DomainServiceCatalogCorrelation correlates service-catalog entity
	// declarations with repository and ownership evidence without letting
	// catalog names create workloads.
	DomainServiceCatalogCorrelation Domain = "service_catalog_correlation"
	// DomainSBOMAttestationAttachment attaches SBOM and attestation evidence to
	// image digests only when subject evidence is explicit.
	DomainSBOMAttestationAttachment Domain = "sbom_attestation_attachment"
	// DomainSupplyChainImpact publishes reducer-owned vulnerability impact
	// findings only when vulnerability, package, SBOM, image, or repository
	// evidence forms an explicit path.
	DomainSupplyChainImpact Domain = "supply_chain_impact"
	// DomainSecurityAlertReconciliation compares provider-reported repository
	// security alerts against Eshu-owned dependency and impact evidence without
	// promoting provider alerts into impact truth.
	DomainSecurityAlertReconciliation Domain = "security_alert_reconciliation"
	// DomainAWSCloudRuntimeDrift publishes admitted AWS runtime-vs-IaC drift
	// findings as canonical reducer facts. The domain stays graph-neutral until
	// the drift node and query shape are frozen.
	DomainAWSCloudRuntimeDrift Domain = "aws_cloud_runtime_drift"
	// DomainAWSResourceMaterialization materializes aws_resource facts into
	// canonical CloudResource graph nodes. It is the node substrate the AWS
	// relationship edge projection (issue #805) joins against; see
	// docs/internal/aws-relationship-edge-materialization-design.md.
	DomainAWSResourceMaterialization Domain = "aws_resource_materialization"
	// DomainAWSRelationshipMaterialization projects aws_relationship facts into
	// canonical AWS relationship edges between the CloudResource nodes that
	// DomainAWSResourceMaterialization committed. It gates on the
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase so edges never
	// resolve against nodes that have not committed (issue #805 PR 2); see
	// docs/internal/aws-relationship-edge-materialization-design.md §5–§8.
	DomainAWSRelationshipMaterialization Domain = "aws_relationship_materialization"
	// DomainObservabilityCoverageCorrelation correlates which monitored
	// CloudResource nodes have observability coverage (CloudWatch alarms,
	// dashboards, log groups, X-Ray) versus which are uncovered, emitting durable
	// provenance-only reducer facts with the six-outcome contract. It is
	// cross-source (observability object vs. the resource it covers) and
	// cross-scope (a resource in one scan scope may be covered by an alarm
	// discovered in another). PR1 writes facts only; the optional COVERS graph
	// edge is a later gated PR. See issue #391 for the design.
	DomainObservabilityCoverageCorrelation Domain = "observability_coverage_correlation"
	// DomainObservabilityCoverageMaterialization projects the exact-outcome
	// observability coverage decisions into canonical COVERS edges between the
	// CloudResource nodes that DomainAWSResourceMaterialization committed: an
	// observability object (alarm/dashboard/log group/X-Ray) covering a monitored
	// resource. It gates on the GraphProjectionPhaseCanonicalNodesCommitted
	// readiness phase so edges never resolve against nodes that have not committed
	// (issue #391 PR3), exactly like DomainAWSRelationshipMaterialization. Only
	// exact coverage with a resolved target uid materializes an edge; derived,
	// ambiguous, unresolved, stale, and rejected coverage stays provenance-only in
	// the PR1 read model and fabricates no edge. See issue #391 for the design.
	DomainObservabilityCoverageMaterialization Domain = "observability_coverage_materialization"
	// DomainKubernetesCorrelation correlates live Kubernetes workload evidence
	// (kubernetes_live.* facts) against deployment-source image and identity
	// evidence, emitting durable provenance-only reducer facts with the
	// six-outcome contract plus a drift classification. Live image refs join
	// digest-first then repository+tag; a label-selector edge that cannot prove
	// exact ownership stays ambiguous and is never promoted to exact. It is
	// cross-source (live cluster vs. registry/Git/IaC source) and cross-scope
	// (live facts live in a cluster scope, source facts in repo/cloud scopes).
	// PR1 writes facts only; the gated canonical graph edge is a later PR. See
	// issue #388 for the design.
	DomainKubernetesCorrelation Domain = "kubernetes_correlation"
	// DomainKubernetesWorkloadMaterialization materializes
	// kubernetes_live.pod_template facts into canonical KubernetesWorkload graph
	// nodes keyed by the collector-emitted object_id. It is the live-workload node
	// substrate that the #388 live-workload edge projection (PR3) joins against;
	// the edge resolves a workload's deployment-source identity to these nodes in a
	// separate, gated stage. After the node write succeeds it publishes the
	// GraphProjectionKeyspaceKubernetesWorkloadUID /
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase so the later edge
	// slice gates exactly like DomainAWSRelationshipMaterialization (#805). See
	// issue #388 and docs/internal/design/388-kubernetes-correlation-readmodel.md.
	DomainKubernetesWorkloadMaterialization Domain = "kubernetes_workload_materialization"
	// DomainKubernetesCorrelationMaterialization projects the exact-outcome live
	// Kubernetes correlation decisions into canonical RUNS_IMAGE edges between a
	// KubernetesWorkload node (committed by DomainKubernetesWorkloadMaterialization)
	// and the digest-addressed OCI source node a live workload was observed running.
	// It gates on the GraphProjectionKeyspaceKubernetesWorkloadUID /
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase so edges never
	// resolve against workload nodes that have not committed (issue #388 PR3),
	// exactly like DomainAWSRelationshipMaterialization (#805) and
	// DomainObservabilityCoverageMaterialization (#391 PR3). Only an exact image
	// digest match whose source digest resolves a canonical OCI node uid
	// materializes an edge; derived, ambiguous, unresolved, stale, and rejected
	// outcomes — and the structural owner_reference identity decision, which is a
	// workload->workload edge rather than a workload->image edge — stay
	// provenance-only and fabricate no edge. See issue #388 and
	// docs/internal/design/388-kubernetes-correlation-readmodel.md.
	DomainKubernetesCorrelationMaterialization Domain = "kubernetes_correlation_materialization"
	// DomainSecurityGroupCidrMaterialization materializes the CIDR and managed
	// prefix-list source endpoints of aws_security_group_rule facts into canonical
	// CidrBlock and PrefixList graph nodes. CidrBlock nodes are keyed by a
	// deterministic hash of the canonicalized (masked, lowercased) CIDR plus its
	// address family, with an is_internet property for 0.0.0.0/0 and ::/0;
	// PrefixList nodes are keyed by the prefix-list id scoped to its account and
	// region. It is the endpoint-node substrate the #1135 network-reachability edge
	// projection (PR2b) joins against; referenced-security-group endpoints already
	// have CloudResource nodes and are not re-materialized here. After the node
	// write succeeds it publishes the GraphProjectionKeyspaceSecurityGroupEndpointUID
	// / GraphProjectionPhaseCanonicalNodesCommitted readiness phase so the later
	// ALLOWS_INGRESS/EGRESS edge slice gates exactly like
	// DomainAWSRelationshipMaterialization (#805). See issue #1135.
	DomainSecurityGroupCidrMaterialization Domain = "security_group_cidr_materialization"
	// DomainSecurityGroupRuleMaterialization materializes aws_security_group_rule
	// facts into canonical port-precise :SecurityGroupRule graph nodes (issue #1135
	// PR2b, Option D). Each live rule whose SecurityGroup anchor resolved to a
	// committed CloudResource node becomes one node keyed by a deterministic hash of
	// the SG anchor uid, direction, protocol, normalized port range, and source —
	// so port and protocol live in the NODE key (two ports key two nodes) rather
	// than in a relationship-property MERGE that times out at 20s on NornicDB. It is
	// the rule-node substrate the reachability edge projection joins against; after
	// the node write succeeds it publishes the
	// GraphProjectionKeyspaceSecurityGroupRuleUID /
	// GraphProjectionPhaseCanonicalNodesCommitted readiness phase so the edge slice
	// gates exactly like DomainAWSRelationshipMaterialization (#805). See issue #1135.
	DomainSecurityGroupRuleMaterialization Domain = "security_group_rule_materialization"
	// DomainSecurityGroupReachabilityMaterialization projects aws_security_group_rule
	// facts into the Option D network-reachability graph: each live rule becomes a
	// port-precise :SecurityGroupRule node, with a SecurityGroup -> rule
	// ALLOWS_INGRESS/EGRESS edge and a rule -[:TO]-> endpoint edge whose endpoint is
	// a CidrBlock, PrefixList, or referenced SecurityGroup CloudResource node. Port
	// and protocol live in the rule NODE key (keyed in its uid), never in a
	// relationship-property MERGE that times out at 20s on NornicDB. It gates on
	// THREE GraphProjectionPhaseCanonicalNodesCommitted phases —
	// GraphProjectionKeyspaceSecurityGroupRuleUID (the rule nodes this domain itself
	// commits before edges), GraphProjectionKeyspaceSecurityGroupEndpointUID (the
	// CidrBlock/PrefixList endpoints, #1135 PR2a), and
	// GraphProjectionKeyspaceCloudResourceUID (the SG nodes, #805) — so an edge
	// never resolves against any endpoint that has not committed. Unresolved SG
	// anchors or endpoints, unknown sources, and tombstoned rules materialize no
	// node and no edge and are counted, never dropped silently. See issue #1135.
	DomainSecurityGroupReachabilityMaterialization Domain = "security_group_reachability_materialization"
)

// IntentStatus captures the durable reducer intent lifecycle state.
type IntentStatus string

const (
	// IntentStatusPending means the intent is ready to be claimed.
	IntentStatusPending IntentStatus = "pending"
	// IntentStatusClaimed means the intent has been leased for execution.
	IntentStatusClaimed IntentStatus = "claimed"
	// IntentStatusRunning means the reducer is actively processing the intent.
	IntentStatusRunning IntentStatus = "running"
	// IntentStatusSucceeded means the intent finished successfully.
	IntentStatusSucceeded IntentStatus = "succeeded"
	// IntentStatusFailed means the intent is terminally failed.
	IntentStatusFailed IntentStatus = "failed"
)

// ResultStatus captures the terminal outcome of one reducer execution.
type ResultStatus string

const (
	// ResultStatusSucceeded means the execution completed successfully.
	ResultStatusSucceeded ResultStatus = "succeeded"
	// ResultStatusFailed means the execution failed.
	ResultStatusFailed ResultStatus = "failed"
	// ResultStatusSuperseded means the intent was skipped because a newer
	// generation is already active for the scope.
	ResultStatusSuperseded ResultStatus = "superseded"
)

// FailureRecord captures the durable reducer failure classification.
type FailureRecord struct {
	FailureClass string
	Message      string
	Details      string
}

// RetryableError marks reducer failures that should re-enter the durable
// queue instead of becoming terminal on the first failure.
type RetryableError interface {
	error
	Retryable() bool
}

// IsRetryable reports whether the supplied error explicitly opts into bounded
// retry behavior.
func IsRetryable(err error) bool {
	var retryable RetryableError
	if !errors.As(err, &retryable) {
		return false
	}

	return retryable.Retryable()
}

// Intent describes one durable reducer follow-up action keyed by scope
// generation.
type Intent struct {
	IntentID        string
	ScopeID         string
	GenerationID    string
	SourceSystem    string
	Domain          Domain
	Cause           string
	Priority        int
	AttemptCount    int
	EntityKeys      []string
	RelatedScopeIDs []string
	Status          IntentStatus
	EnqueuedAt      time.Time
	AvailableAt     time.Time
	ClaimedAt       *time.Time
	CompletedAt     *time.Time
	Failure         *FailureRecord
}

// ScopeGenerationKey returns the durable scope-generation boundary for the intent.
func (i Intent) ScopeGenerationKey() string {
	return fmt.Sprintf("%s:%s", i.ScopeID, i.GenerationID)
}

// Validate checks the durable intent contract.
func (i Intent) Validate() error {
	if strings.TrimSpace(i.IntentID) == "" {
		return errors.New("intent_id must not be blank")
	}
	if strings.TrimSpace(i.ScopeID) == "" {
		return errors.New("scope_id must not be blank")
	}
	if strings.TrimSpace(i.GenerationID) == "" {
		return errors.New("generation_id must not be blank")
	}
	if strings.TrimSpace(i.SourceSystem) == "" {
		return errors.New("source_system must not be blank")
	}
	if err := i.Domain.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(i.Cause) == "" {
		return errors.New("cause must not be blank")
	}
	if i.EnqueuedAt.IsZero() {
		return errors.New("enqueued_at must not be zero")
	}
	if i.AvailableAt.IsZero() {
		return errors.New("available_at must not be zero")
	}
	if len(i.RelatedScopeIDs) == 0 {
		return errors.New("related_scope_ids must not be empty")
	}
	if err := i.Status.Validate(); err != nil {
		return err
	}

	for _, key := range i.EntityKeys {
		if strings.TrimSpace(key) == "" {
			return errors.New("entity_keys must not contain blank values")
		}
	}
	var seenRelatedScopes map[string]struct{}
	for _, scopeID := range i.RelatedScopeIDs {
		normalizedScopeID := strings.TrimSpace(scopeID)
		if normalizedScopeID == "" {
			return errors.New("related_scope_ids must not contain blank values")
		}
		if seenRelatedScopes == nil {
			seenRelatedScopes = make(map[string]struct{}, len(i.RelatedScopeIDs))
		}
		if _, exists := seenRelatedScopes[normalizedScopeID]; exists {
			return errors.New("related_scope_ids must not contain duplicate values")
		}
		seenRelatedScopes[normalizedScopeID] = struct{}{}
	}

	return nil
}

// Clone returns a replay-safe copy of the intent.
func (i Intent) Clone() Intent {
	cloned := i
	cloned.EntityKeys = slices.Clone(i.EntityKeys)
	cloned.RelatedScopeIDs = slices.Clone(i.RelatedScopeIDs)
	if i.ClaimedAt != nil {
		claimedAt := *i.ClaimedAt
		cloned.ClaimedAt = &claimedAt
	}
	if i.CompletedAt != nil {
		completedAt := *i.CompletedAt
		cloned.CompletedAt = &completedAt
	}
	if i.Failure != nil {
		failure := *i.Failure
		cloned.Failure = &failure
	}

	return cloned
}

// Validate checks that the lifecycle state is one of the known durable values.
func (status IntentStatus) Validate() error {
	switch status {
	case IntentStatusPending, IntentStatusClaimed, IntentStatusRunning, IntentStatusSucceeded, IntentStatusFailed:
		return nil
	default:
		return fmt.Errorf("unknown intent status %q", status)
	}
}

// Terminal reports whether the status represents a final state.
func (status IntentStatus) Terminal() bool {
	switch status {
	case IntentStatusSucceeded, IntentStatusFailed:
		return true
	default:
		return false
	}
}

// WithStatus returns a clone of the intent with the given status and timestamp.
func (i Intent) WithStatus(status IntentStatus, at time.Time) Intent {
	cloned := i.Clone()
	cloned.Status = status
	switch status {
	case IntentStatusClaimed:
		cloned.ClaimedAt = &at
	case IntentStatusRunning:
		cloned.ClaimedAt = &at
	case IntentStatusSucceeded, IntentStatusFailed:
		cloned.CompletedAt = &at
	}

	return cloned
}
