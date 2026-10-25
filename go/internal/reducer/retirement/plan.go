// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package retirement

import (
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	sharedworker "github.com/eshu-hq/eshu/go/internal/reducer/intents/shared/worker"
)

// RetireDomainClass names how step 2c retracts one reducer domain's graph
// state for a retired repository.
type RetireDomainClass string

const (
	// RetireDomainRetractByRepo marks a domain the runner retracts through
	// the domain's own repo-keyed (or tombstone-scope-keyed) retract path:
	// an EdgeWriter RetractEdges branch, a dedicated runner retract, or a
	// scope-keyed evidence retract over the tombstone's captured scope ids.
	RetireDomainRetractByRepo RetireDomainClass = "retract_by_repo"
	// RetireDomainRemovedByNodeRetract marks a domain whose graph edges are
	// only incident to nodes the G5/G5w node retracts delete: File,
	// Directory, code-entity, Workload, and WorkloadInstance nodes of the
	// retired repository. Their DETACH DELETEs remove these edges with the
	// nodes, so the runner issues no per-domain retract for them.
	RetireDomainRemovedByNodeRetract RetireDomainClass = "removed_by_node_retract"
	// RetireDomainNotRepositoryScoped marks a domain with no repo-keyed
	// graph state to retract. The reason names the identity its graph (if
	// any) is keyed by instead.
	RetireDomainNotRepositoryScoped RetireDomainClass = "not_repository_scoped"
)

// RetireDomainPlan is one domain's 2c classification.
type RetireDomainPlan struct {
	// Class is the retraction mechanism.
	Class RetireDomainClass
	// Reason cites the retract path or the non-repo identity. It is
	// required on every row: the test pins presence, and review pins
	// truth against the cited source.
	Reason string
}

// retirementDomainPlans is the closed per-domain table the 2c runner reads.
// Coverage is enforced by TestRetirementRetractPlanClassifiesEveryDomain,
// which iterates the reducer domain registry in both directions: a domain
// added later fails the test until someone classifies it here.
var retirementDomainPlans = map[reducercontract.Domain]RetireDomainPlan{
	// Shared-projection edge domains: every one has an EdgeWriter write
	// and retract branch (storage/cypher/edge/writer/writer.go and
	// retract.go), driven repo-keyed through shared intents (2b deletes
	// them) and the dedicated code_calls and repo_dependency runners.
	reducercontract.DomainRepoDependency:           {Class: RetireDomainRetractByRepo, Reason: "repo-dependency projection runner retract and write, repo-keyed (repo_dependency_projection_runner.go), G1/G4"},
	reducercontract.DomainWorkloadDependency:       {Class: RetireDomainRetractByRepo, Reason: "EdgeWriter workload-dependency retract branch (edge/writer/retract.go), G1"},
	reducercontract.DomainCodeCalls:                {Class: RetireDomainRetractByRepo, Reason: "code-call projection runner retract and write, repo-keyed (code/call/projection/runner.go), G1"},
	reducercontract.DomainSQLRelationships:         {Class: RetireDomainRetractByRepo, Reason: "repo-keyed delta scope (sqlrelationship/sql_relationship_delta_scope.go RepositoryIDs), G1"},
	reducercontract.DomainShellExec:                {Class: RetireDomainRetractByRepo, Reason: "repo-keyed whole-scope retract (code/shell/intents.go), G1"},
	reducercontract.DomainInheritanceEdges:         {Class: RetireDomainRetractByRepo, Reason: "repo-keyed delta scope (inheritance/delta_scope.go RepositoryIDs), G1"},
	reducercontract.DomainDocumentationEdges:       {Class: RetireDomainRetractByRepo, Reason: "scope-keyed DOCUMENTS retract over the tombstone scopes (documentation_edge_delta_scope.go buildDocumentationScopeRetractRows), G3"},
	reducercontract.DomainRationaleEdges:           {Class: RetireDomainRetractByRepo, Reason: "repo-keyed rationale retract (rationale_delta_scope.go buildRationaleRepoRetractRows), G2"},
	reducercontract.DomainDeployableUnitEdges:      {Class: RetireDomainRetractByRepo, Reason: "repo-keyed deployable-unit edge retract (deployable_unit_correlation_edges.go retractDeployableUnitEdges), G1"},
	reducercontract.DomainHandlesRoute:             {Class: RetireDomainRetractByRepo, Reason: "EdgeWriter handles-route retract branch (edge/writer/retract.go), G1"},
	reducercontract.DomainRunsIn:                   {Class: RetireDomainRetractByRepo, Reason: "EdgeWriter runs-in retract branch (edge/writer/retract.go), G1"},
	reducercontract.DomainInvokesCloudAction:       {Class: RetireDomainRetractByRepo, Reason: "EdgeWriter invokes-cloud-action retract branch (edge/writer/retract.go), G1"},
	reducercontract.DomainCodeownersOwnershipEdges: {Class: RetireDomainRetractByRepo, Reason: "EdgeWriter codeowners retract branch (edge/writer/retract.go), G1"},
	reducercontract.DomainSubmodulePinEdges:        {Class: RetireDomainRetractByRepo, Reason: "EdgeWriter submodule-pin retract branch (edge/writer/retract.go), G1"},
	// Materialization halves: each drives its repo-keyed retract and write
	// through its edges domain above.
	reducercontract.DomainCodeownersOwnership:            {Class: RetireDomainRetractByRepo, Reason: "repo-keyed retract rows through codeowners_ownership_edges (code/owners/handler.go buildRetractRows)"},
	reducercontract.DomainSubmodulePin:                   {Class: RetireDomainRetractByRepo, Reason: "repo-keyed retract rows through submodule_pin_edges (submodule_pin_materialization.go submodulePinRepositoryIDs)"},
	reducercontract.DomainSQLRelationshipMaterialization: {Class: RetireDomainRetractByRepo, Reason: "repo-keyed delta scope through sql_relationships (sqlrelationship/sql_relationship_delta_scope.go RepositoryIDs)"},
	reducercontract.DomainShellExecMaterialization:       {Class: RetireDomainRetractByRepo, Reason: "repo-keyed whole-scope retract through shell_exec (code/shell/intents.go)"},
	reducercontract.DomainInheritanceMaterialization:     {Class: RetireDomainRetractByRepo, Reason: "repo-keyed delta scope through inheritance_edges (inheritance/delta_scope.go RepositoryIDs)"},
	reducercontract.DomainDocumentationMaterialization:   {Class: RetireDomainRetractByRepo, Reason: "scope-keyed DOCUMENTS retract through documentation_edges over the tombstone scopes, G3"},
	reducercontract.DomainRationaleMaterialization:       {Class: RetireDomainRetractByRepo, Reason: "repo-keyed rationale retract through rationale_edges (rationale_delta_scope.go), G2"},
	// Dedicated repo-keyed writers outside the shared worker.
	reducercontract.DomainCodeImportRepoEdge:             {Class: RetireDomainRetractByRepo, Reason: "repo-keyed projection/code-imports DEPENDS_ON edges per consumer repo, retracted through the repo-dependency lane refresh-first reconstruction (code_import_repo_edge_retract.go)"},
	reducercontract.DomainWorkloadMaterialization:        {Class: RetireDomainRetractByRepo, Reason: "repo-anchored Workload/Instance/Endpoint node and DEFINES edge retracts (workload/retract/retract.go, workload_materializer_retract_instances.go), G5w"},
	reducercontract.DomainSemanticEntityMaterialization:  {Class: RetireDomainRetractByRepo, Reason: "repo-scoped entity retract and write through the semantic EntityWriter (code/semantic/materialization.go ExtractEntityRowsForRepo)"},
	reducercontract.DomainCodeTaintEvidence:              {Class: RetireDomainRetractByRepo, Reason: "scope-keyed taint-node retract over the tombstone scopes, uids enumerated from the 2e ledger (code/taint/evidence_materialization.go RetractCodeTaintEvidenceByUIDs); nodes carry reducer/code-taint, so G5 misses them"},
	reducercontract.DomainRuntimeDeclarationProjection:   {Class: RetireDomainRetractByRepo, Reason: "repo-anchored RuntimeDeclaration DETACH DELETE (storage/cypher/edge/writer/runtime_declaration.go RetractRuntimeDeclarations, #8816); nodes carry reducer/runtime_declaration, so G5 misses them"},
	reducercontract.DomainIncidentRoutingMaterialization: {Class: RetireDomainRetractByRepo, Reason: "scope-keyed incident-routing evidence retract (incident/routing_materialization.go RetractIncidentRoutingEvidence)"},
	reducercontract.DomainCICDRunCorrelation:             {Class: RetireDomainRetractByRepo, Reason: "repo-anchored BUILT_FROM provenance edges with a scope-keyed retract (contract/provenance_edges.go RetractBuiltFromEdges); correlation rows themselves are Postgres"},
	reducercontract.DomainContainerImageIdentity:         {Class: RetireDomainRetractByRepo, Reason: "repo-anchored BUILT_FROM provenance edges with a scope-keyed retract (contract/provenance_edges.go RetractBuiltFromEdges); image nodes stay digest-keyed"},
	reducercontract.DomainPlatformInfraMaterialization:   {Class: RetireDomainRetractByRepo, Reason: "repo-keyed PROVISIONS_PLATFORM edge retract (infrastructure_platform_materializer.go RetractStale); Platform nodes stay id-keyed for other repos"},
	// Edges only incident to G5/G5w nodes: the node DETACH DELETEs remove
	// them, so 2c issues no per-domain retract.
	reducercontract.DomainCodeInterprocEvidence:                    {Class: RetireDomainRemovedByNodeRetract, Reason: "TAINT_FLOWS_TO edges run only between Function nodes; G5 DETACH DELETE removes them with the endpoints"},
	reducercontract.DomainCodeValueFlowRefresh:                     {Class: RetireDomainRemovedByNodeRetract, Reason: "global dataflow edges run only between code-entity nodes (Function, Variable); G5 DETACH DELETE removes them with the endpoints"},
	reducercontract.DomainWorkloadCloudRelationshipMaterialization: {Class: RetireDomainRemovedByNodeRetract, Reason: "USES edges run only from WorkloadInstances; G5w DETACH DELETE removes them with the instances (writer never fabricates endpoints)"},
	// No repo-keyed graph state. Cloud and infra nodes are keyed by cloud
	// or cluster identity and shared cross-scope; findings, decisions, and
	// index rows live in Postgres under the purge and census.
	reducercontract.DomainAWSCloudImageMaterialization:             {Class: RetireDomainNotRepositoryScoped, Reason: "image-relationship edges between ARN-keyed CloudResource nodes (MERGE on uid); cross-scope, no repo key"},
	reducercontract.DomainAWSCloudRuntimeDrift:                     {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres drift finding rows only (reducer_aws_cloud_runtime_drift_finding); no graph writes"},
	reducercontract.DomainAWSRelationshipMaterialization:           {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed CloudResource nodes, idempotent by (source uid, type, target uid) (aws_relationship_materialization.go CloudResourceEdgeWriter); cross-scope"},
	reducercontract.DomainAWSResourceMaterialization:               {Class: RetireDomainNotRepositoryScoped, Reason: "ARN-keyed CloudResource nodes (MERGE on uid, cloud/resource/node_writer.go); cross-scope"},
	reducercontract.DomainAzureRelationshipMaterialization:         {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed CloudResource nodes (shared CloudResourceEdgeWriter contract); cross-scope"},
	reducercontract.DomainAzureResourceMaterialization:             {Class: RetireDomainNotRepositoryScoped, Reason: "ARN-keyed CloudResource nodes (MERGE on uid); cross-scope"},
	reducercontract.DomainCloudAssetResolution:                     {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres resolution rows only (PostgresCloudAssetResolutionWriter); no graph writes"},
	reducercontract.DomainCloudInventoryAdmission:                  {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres admission rows only (PostgresCloudInventoryAdmissionWriter); no graph writes"},
	reducercontract.DomainCodeCallMaterialization:                  {Class: RetireDomainNotRepositoryScoped, Reason: "call-materialization decisions feed the code_calls projection domain; graph edges are written and retracted under code_calls"},
	reducercontract.DomainCodeDrifted:                              {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres drifted-pair findings only (PostgresCodeDriftedWriter); no graph writes"},
	reducercontract.DomainCodeFunctionSummary:                      {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres summary/source/graph-id rows only (function_summaries, function_sources, function_graph_ids); 2e deletes them by repo"},
	reducercontract.DomainConfigStateDrift:                         {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres drift finding rows only (PostgresTerraformConfigStateDriftWriter); no graph writes"},
	reducercontract.DomainCrossplaneSatisfiedByMaterialization:     {Class: RetireDomainNotRepositoryScoped, Reason: "SATISFIED_BY edges between k8s-keyed Claim and XRD nodes (crossplane/satisfied_by_materialization.go); no repo keying"},
	reducercontract.DomainDataLineage:                              {Class: RetireDomainNotRepositoryScoped, Reason: "reserved identifier; no writer reduces it (no handler outside the registry)"},
	reducercontract.DomainDeployableUnitCorrelation:                {Class: RetireDomainNotRepositoryScoped, Reason: "graph edges are written and retracted under deployable_unit_edges; the correlation domain itself writes Postgres rows and repo-keyed intents (2b)"},
	reducercontract.DomainDeploymentMapping:                        {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres platform rows only (PostgresPlatformMaterializationWriter); no graph writes"},
	reducercontract.DomainEC2BlockDeviceKMSPostureMaterialization:  {Class: RetireDomainNotRepositoryScoped, Reason: "ARN-keyed CloudResource posture; cross-scope, no repo key"},
	reducercontract.DomainEC2InstanceIdentityMaterialization:       {Class: RetireDomainNotRepositoryScoped, Reason: "ARN-keyed CloudResource nodes (MERGE on uid); cross-scope"},
	reducercontract.DomainEC2InstanceNodeMaterialization:           {Class: RetireDomainNotRepositoryScoped, Reason: "ARN-keyed CloudResource nodes (MERGE on uid); cross-scope"},
	reducercontract.DomainEC2InternetExposureMaterialization:       {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed CloudResource nodes; cross-scope"},
	reducercontract.DomainEC2UsesProfileMaterialization:            {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed CloudResource nodes; cross-scope"},
	reducercontract.DomainEshuSearchDocument:                       {Class: RetireDomainNotRepositoryScoped, Reason: "search-document projection rows only (SearchDocumentWriter); no graph writes"},
	reducercontract.DomainGCPRelationshipMaterialization:           {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed CloudResource nodes (shared CloudResourceEdgeWriter contract); cross-scope"},
	reducercontract.DomainGCPResourceMaterialization:               {Class: RetireDomainNotRepositoryScoped, Reason: "ARN-keyed CloudResource nodes (MERGE on uid); cross-scope"},
	reducercontract.DomainGovernance:                               {Class: RetireDomainNotRepositoryScoped, Reason: "reserved identifier; no writer reduces it (no handler outside the registry)"},
	reducercontract.DomainIAMCanAssumeMaterialization:              {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed role nodes; cross-scope, no repo key"},
	reducercontract.DomainIAMCanPerformMaterialization:             {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed role nodes; cross-scope, no repo key"},
	reducercontract.DomainIAMEscalationMaterialization:             {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed role nodes; cross-scope, no repo key"},
	reducercontract.DomainIAMInstanceProfileRoleMaterialization:    {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed role nodes; cross-scope, no repo key"},
	reducercontract.DomainIncidentRepositoryCorrelation:            {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres correlation fact rows only (PostgresIncidentRepositoryCorrelationWriter); no graph writes"},
	reducercontract.DomainKubernetesCorrelation:                    {Class: RetireDomainNotRepositoryScoped, Reason: "cluster-keyed Kubernetes nodes; cross-scope, no repo key"},
	reducercontract.DomainKubernetesCorrelationMaterialization:     {Class: RetireDomainNotRepositoryScoped, Reason: "cluster-keyed Kubernetes nodes and edges; cross-scope, no repo key"},
	reducercontract.DomainKubernetesNamespaceMaterialization:       {Class: RetireDomainNotRepositoryScoped, Reason: "(cluster_id, namespace)-keyed MERGE nodes; cross-scope, no repo key"},
	reducercontract.DomainKubernetesWorkloadMaterialization:        {Class: RetireDomainNotRepositoryScoped, Reason: "uid-keyed KubernetesWorkload nodes (w.id = row.uid); cross-scope, no repo key"},
	reducercontract.DomainMultiCloudRuntimeDrift:                   {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres drift finding rows only; no graph writes"},
	reducercontract.DomainObservabilityCoverageCorrelation:         {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres coverage rows only (PostgresObservabilityCoverageCorrelationWriter); no graph writes"},
	reducercontract.DomainObservabilityCoverageMaterialization:     {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres coverage rows only; no graph writes"},
	reducercontract.DomainOwnership:                                {Class: RetireDomainNotRepositoryScoped, Reason: "reserved identifier; no writer reduces it (no handler outside the registry)"},
	reducercontract.DomainPackageSourceCorrelation:                 {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres admission-decision rows only; no graph writes"},
	reducercontract.DomainRDSPostureMaterialization:                {Class: RetireDomainNotRepositoryScoped, Reason: "ARN-keyed CloudResource posture; cross-scope, no repo key"},
	reducercontract.DomainS3ExternalPrincipalGrantMaterialization:  {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed nodes; cross-scope, no repo key"},
	reducercontract.DomainS3InternetExposureMaterialization:        {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed nodes; cross-scope, no repo key"},
	reducercontract.DomainS3LogsToMaterialization:                  {Class: RetireDomainNotRepositoryScoped, Reason: "edges between ARN-keyed nodes; cross-scope, no repo key"},
	reducercontract.DomainSBOMAttestationAttachment:                {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres attachment facts only (PostgresSBOMAttestationAttachmentWriter); no graph writes"},
	reducercontract.DomainSearchVectorBuild:                        {Class: RetireDomainNotRepositoryScoped, Reason: "search-vector build rows only (searchvector runner); no graph writes"},
	reducercontract.DomainSecretsIAMGraphProjection:                {Class: RetireDomainNotRepositoryScoped, Reason: "uid-only SecretsIAM* node identity with cross-scope matched endpoints, never fabricated (secretsiam/secrets_iam_graph_projection.go); no repo key"},
	reducercontract.DomainSecretsIAMTrustChain:                     {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres trust-chain state rows only; graph projection (if wired) is under secrets_iam_graph_projection"},
	reducercontract.DomainSecurityAlertReconciliation:              {Class: RetireDomainNotRepositoryScoped, Reason: "alert-correlation decision rows only; no repo-keyed graph writes"},
	reducercontract.DomainSecurityGroupCidrMaterialization:         {Class: RetireDomainNotRepositoryScoped, Reason: "security-group endpoint nodes keyed by cloud identity; cross-scope, no repo key"},
	reducercontract.DomainSecurityGroupReachabilityMaterialization: {Class: RetireDomainNotRepositoryScoped, Reason: "edges between cloud-keyed security-group rule nodes (SecurityGroupReachabilityWriter); cross-scope, no repo key"},
	reducercontract.DomainSecurityGroupRuleMaterialization:         {Class: RetireDomainNotRepositoryScoped, Reason: "security-group rule nodes keyed by cloud identity; cross-scope, no repo key"},
	reducercontract.DomainServiceCatalogCorrelation:                {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres correlation rows only (PostgresServiceCatalogCorrelationWriter); no graph writes"},
	reducercontract.DomainSupplyChainImpact:                        {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres impact findings and winner rows only (PostgresSupplyChainImpactWriter); no graph writes"},
	reducercontract.DomainWorkloadIdentity:                         {Class: RetireDomainNotRepositoryScoped, Reason: "Postgres workload-identity facts only (PostgresWorkloadIdentityWriter); graph nodes are written under workload_materialization"},
}

// RetirementDomainPlanFor returns the 2c classification for one domain, or
// false when the domain is not classified (a bug the completeness test
// turns into a build-time failure, never a silent skip at runtime).
func RetirementDomainPlanFor(domain reducercontract.Domain) (RetireDomainPlan, bool) {
	plan, ok := retirementDomainPlans[domain]
	return plan, ok
}

// RetirementLeaseClass names why a partition-lease domain appears in the
// 2b' wait: either its writer holds the lease while writing repo-keyed
// graph edges, or it holds a lease for maintenance that never writes this
// repository's edges.
type RetirementLeaseClass string

const (
	// RetirementLeaseRepoGraphWriter marks a domain whose writer holds a
	// shared_projection_partition_leases row from before it reads intents
	// until after its last graph write (concurrency contract I1). A live
	// pre-barrier lease in one of these domains holds 2b'.
	RetirementLeaseRepoGraphWriter RetirementLeaseClass = "repo_graph_writer"
	// RetirementLeaseMaintenance marks a lease that never writes this
	// repository's edges (orphan sweep, evidence summary, value-flow
	// cleanup, winners rebuild). 2b' skips these domains so a maintenance
	// lease that never touches the repository cannot hold the barrier.
	RetirementLeaseMaintenance RetirementLeaseClass = "maintenance_lease"
)

// RetirementLeaseDomain is one classified partition-lease domain.
type RetirementLeaseDomain struct {
	// Class is the wait behavior.
	Class RetirementLeaseClass
	// Reason cites the claim site (and, for maintenance, why it cannot
	// write this repository's edges).
	Reason string
}

// retirementLeaseDomains classifies every projection_domain value a
// ClaimPartitionLease caller can pass. The shared worker's domain travels
// in cfg.Domain, so the worker range is derived from worker.Domains()
// instead of listed here; the test pins that every one of those is a
// repo_graph_writer below. A new lease writer fails
// TestRetirementLeaseDomainsMatchLeaseWriters until someone classifies it.
var retirementLeaseDomains = map[string]RetirementLeaseDomain{
	// Dedicated repo-keyed graph writers with their own lease claim.
	string(reducercontract.DomainCodeCalls):      {Class: RetirementLeaseRepoGraphWriter, Reason: "code-call projection runner claim and heartbeat (code/call/projection/runner.go, lease.go)"},
	string(reducercontract.DomainRepoDependency): {Class: RetirementLeaseRepoGraphWriter, Reason: "repo-dependency projection runner claim and heartbeat (repo_dependency_projection_runner.go, repo_dependency_projection_telemetry.go)"},
	// Maintenance leases: named so 2b' can skip them.
	"graph_orphan_sweep":            {Class: RetirementLeaseMaintenance, Reason: "orphan sweep maintenance runner (maintenance/orphan/runner.go); sweeps disconnected nodes, never writes repo edges"},
	"collector_evidence_summary":    {Class: RetirementLeaseMaintenance, Reason: "collector evidence summary maintainer (maintenance/evidence/maintainer.go); reaps summary rows, never writes repo edges"},
	"code_value_flow_stale_cleanup": {Class: RetirementLeaseMaintenance, Reason: "value-flow stale cleanup runner (code/value/cleanup/runner.go); deletes stale rows, never writes repo edges"},
	"supply_chain_impact_winners":   {Class: RetirementLeaseMaintenance, Reason: "supply-chain winners maintainer (supplychain/core/winners_maintainer.go); reconciles winner rows, never writes repo edges"},
}

// RetirementLeaseDomainFor returns the 2b' classification for one
// projection_domain lease value, or false when it is not classified.
func RetirementLeaseDomainFor(domain string) (RetirementLeaseDomain, bool) {
	entry, ok := retirementLeaseDomains[domain]
	return entry, ok
}

// RetirementSharedWorkerLeaseDomains returns the lease domains the shared
// projection worker can hold: exactly the domains its partition worker
// drains (intents/shared/worker/runner.go Domains). Every one is a
// repo_graph_writer: the worker claims the lease before it reads intents
// and holds it through the edge write and MarkIntentsCompleted
// (worker/process.go claim, write, mark, release; concurrency contract
// I1), so a live pre-barrier lease in any of them holds 2b'.
func RetirementSharedWorkerLeaseDomains() []string {
	return sharedworker.Domains()
}
