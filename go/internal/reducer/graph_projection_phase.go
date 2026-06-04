package reducer

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// GraphProjectionKeyspace identifies the concrete conflict domain for graph
// projection coordination.
type GraphProjectionKeyspace string

const (
	// GraphProjectionKeyspaceCodeEntitiesUID represents the Neo4j uniqueness
	// domain keyed by code entity uid values.
	GraphProjectionKeyspaceCodeEntitiesUID GraphProjectionKeyspace = "code_entities_uid"
	// GraphProjectionKeyspaceServiceUID represents the canonical workload/service
	// identity domain.
	GraphProjectionKeyspaceServiceUID GraphProjectionKeyspace = "service_uid"
	// GraphProjectionKeyspaceDeployableUnitUID represents the canonical
	// deployable-unit identity domain.
	GraphProjectionKeyspaceDeployableUnitUID GraphProjectionKeyspace = "deployable_unit_uid"
	// GraphProjectionKeyspaceTerraformResourceUID represents the canonical
	// Terraform resource identity domain.
	GraphProjectionKeyspaceTerraformResourceUID GraphProjectionKeyspace = "terraform_resource_uid"
	// GraphProjectionKeyspaceTerraformModuleUID represents the canonical
	// Terraform module identity domain.
	GraphProjectionKeyspaceTerraformModuleUID GraphProjectionKeyspace = "terraform_module_uid"
	// GraphProjectionKeyspaceCloudResourceUID represents the canonical cloud
	// resource identity domain.
	GraphProjectionKeyspaceCloudResourceUID GraphProjectionKeyspace = "cloud_resource_uid"
	// GraphProjectionKeyspaceKubernetesWorkloadUID represents the canonical live
	// Kubernetes workload identity domain. The live-workload edge slice (#388 PR3)
	// gates its RUNS/DRIFTS_FROM edge projection on this keyspace's
	// canonical-nodes-committed phase exactly as the AWS relationship edge gates on
	// GraphProjectionKeyspaceCloudResourceUID (#805).
	GraphProjectionKeyspaceKubernetesWorkloadUID GraphProjectionKeyspace = "kubernetes_workload_uid"
	// GraphProjectionKeyspaceSecurityGroupEndpointUID represents the canonical
	// security-group network-reachability endpoint identity domain: the CidrBlock
	// and PrefixList nodes a security_group_rule fact's source endpoint
	// materializes (issue #1135 PR2a). The ALLOWS_INGRESS/EGRESS edge slice
	// (#1135 PR2b) gates its edge projection on this keyspace's
	// canonical-nodes-committed phase exactly as the AWS relationship edge gates on
	// GraphProjectionKeyspaceCloudResourceUID (#805), so edges never resolve
	// against endpoint nodes that have not committed.
	GraphProjectionKeyspaceSecurityGroupEndpointUID GraphProjectionKeyspace = "security_group_endpoint_uid"
	// GraphProjectionKeyspaceSecurityGroupRuleUID represents the canonical
	// security-group reachability rule identity domain: the :SecurityGroupRule
	// nodes a security_group_rule fact materializes (issue #1135 PR2b, Option D).
	// The ALLOWS_INGRESS/EGRESS and TO edge slice gates its edge projection on
	// THREE canonical-nodes-committed phases — this rule-node keyspace, the
	// endpoint keyspace (GraphProjectionKeyspaceSecurityGroupEndpointUID, the
	// CidrBlock/PrefixList nodes), and the cloud-resource keyspace
	// (GraphProjectionKeyspaceCloudResourceUID, the SG nodes) — so an edge never
	// resolves against any endpoint node that has not committed.
	GraphProjectionKeyspaceSecurityGroupRuleUID GraphProjectionKeyspace = "security_group_rule_uid"
	// GraphProjectionKeyspaceWebhookEventUID represents the canonical webhook
	// event identity domain.
	GraphProjectionKeyspaceWebhookEventUID GraphProjectionKeyspace = "webhook_event_uid"
	// GraphProjectionKeyspaceCrossRepoEvidence represents the reducer readiness
	// domain for deferred backward relationship evidence during bootstrap.
	GraphProjectionKeyspaceCrossRepoEvidence GraphProjectionKeyspace = "cross_repo_evidence"
)

// GraphProjectionPhase identifies one durable readiness milestone for a graph
// projection keyspace.
type GraphProjectionPhase string

const (
	// GraphProjectionPhaseCanonicalNodesCommitted is published after canonical
	// projector node writes commit successfully.
	GraphProjectionPhaseCanonicalNodesCommitted GraphProjectionPhase = "canonical_nodes_committed"
	// GraphProjectionPhaseDeployableUnitCorrelation is published after the
	// deployable-unit correlation pass finishes one bounded slice, including
	// slices that intentionally admit zero candidates.
	GraphProjectionPhaseDeployableUnitCorrelation GraphProjectionPhase = "deployable_unit_correlation"
	// GraphProjectionPhaseSemanticNodesCommitted is published after semantic
	// entity reducer node writes commit successfully.
	GraphProjectionPhaseSemanticNodesCommitted GraphProjectionPhase = "semantic_nodes_committed"
	// GraphProjectionPhaseBackwardEvidenceCommitted is published after deferred
	// backward relationship evidence is durably committed for one
	// scope-generation slice.
	GraphProjectionPhaseBackwardEvidenceCommitted GraphProjectionPhase = "backward_evidence_committed"
	// GraphProjectionPhaseDeploymentMapping is published after the
	// deployment_mapping reducer domain finishes one bounded slice.
	GraphProjectionPhaseDeploymentMapping GraphProjectionPhase = "deployment_mapping"
	// GraphProjectionPhaseWorkloadMaterialization is published after the
	// workload_materialization reducer domain finishes one bounded slice.
	GraphProjectionPhaseWorkloadMaterialization GraphProjectionPhase = "workload_materialization"
	// GraphProjectionPhaseCrossSourceAnchorReady is reserved for the future DSL
	// evaluator publication that proves cross-source joins are fully converged.
	GraphProjectionPhaseCrossSourceAnchorReady GraphProjectionPhase = "cross_source_anchor_ready"
)

// GraphProjectionPhaseKey identifies one bounded graph-write readiness slice.
type GraphProjectionPhaseKey struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
	GenerationID     string
	Keyspace         GraphProjectionKeyspace
}

// GraphProjectionPhaseState captures one durable readiness publication.
type GraphProjectionPhaseState struct {
	Key         GraphProjectionPhaseKey
	Phase       GraphProjectionPhase
	CommittedAt time.Time
	UpdatedAt   time.Time
}

// Validate checks the bounded readiness identity contract.
func (k GraphProjectionPhaseKey) Validate() error {
	if strings.TrimSpace(k.ScopeID) == "" {
		return fmt.Errorf("scope_id must not be blank")
	}
	if strings.TrimSpace(k.AcceptanceUnitID) == "" {
		return fmt.Errorf("acceptance_unit_id must not be blank")
	}
	if strings.TrimSpace(k.SourceRunID) == "" {
		return fmt.Errorf("source_run_id must not be blank")
	}
	if strings.TrimSpace(k.GenerationID) == "" {
		return fmt.Errorf("generation_id must not be blank")
	}
	if strings.TrimSpace(string(k.Keyspace)) == "" {
		return fmt.Errorf("keyspace must not be blank")
	}
	return nil
}

// GraphProjectionReadinessLookup reports whether a bounded readiness slice has
// reached the requested phase. It returns (ready, found).
type GraphProjectionReadinessLookup func(key GraphProjectionPhaseKey, phase GraphProjectionPhase) (bool, bool)

// GraphProjectionReadinessPrefetch resolves readiness for a bounded set of keys
// and returns an in-memory lookup closure for the current cycle.
type GraphProjectionReadinessPrefetch func(ctx context.Context, keys []GraphProjectionPhaseKey, phase GraphProjectionPhase) (GraphProjectionReadinessLookup, error)

// GraphProjectionPhasePublisher persists graph-readiness publications.
type GraphProjectionPhasePublisher interface {
	PublishGraphProjectionPhases(context.Context, []GraphProjectionPhaseState) error
}

// EndpointPresenceRow records that one endpoint node uid is committed in the
// canonical graph, keyed by its bounded keyspace. It is the uid-exact,
// cross-scope readiness primitive (issue #1380, ADR #1314 §6/§8): a presence row
// proves the specific node X is committed, which the same-scope/same-generation
// graph_projection_phase_state gate cannot express. CommittedAt is the node
// materializer's commit instant; an empty value defers to the store's clock.
type EndpointPresenceRow struct {
	Keyspace    GraphProjectionKeyspace
	UID         string
	ScopeID     string
	CommittedAt time.Time
}

// EndpointPresenceWriter records and retracts endpoint-node presence. The
// CloudResource and KubernetesWorkload node materializers call Upsert with one
// row per committed node uid (idempotent: re-upserting the same (keyspace, uid)
// converges on one row), and RetractScope removes a scope's presence rows so a
// node retract removes its presence. Implementations MUST be safe under
// concurrent materializer workers (the upsert is ON CONFLICT idempotent); the
// contract forbids reducing workers or batch size to dodge a race.
type EndpointPresenceWriter interface {
	Upsert(ctx context.Context, rows []EndpointPresenceRow) error
	RetractScope(ctx context.Context, scopeIDs []string) error
}

// EndpointPresenceLookup answers the uid-exact cross-scope readiness question
// for the secrets/IAM graph projection gate (issue #1380). MissingUIDs returns
// the subset of uids that have no presence row for the keyspace, computed with
// ONE bounded query (WHERE keyspace=$1 AND uid = ANY($2)) and an in-memory
// set-difference — never an N+1 per-uid probe, which the §performance contract
// forbids. An empty input yields an empty result and no query.
type EndpointPresenceLookup interface {
	MissingUIDs(ctx context.Context, keyspace GraphProjectionKeyspace, uids []string) ([]string, error)
}
