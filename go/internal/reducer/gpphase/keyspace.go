// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gpphase

// Keyspace identifies the concrete conflict domain for graph projection
// coordination.
type Keyspace string

const (
	// KeyspaceCodeEntitiesUID represents the Neo4j uniqueness domain keyed by
	// code entity uid values.
	KeyspaceCodeEntitiesUID Keyspace = "code_entities_uid"
	// KeyspaceServiceUID represents the canonical workload/service identity
	// domain.
	KeyspaceServiceUID Keyspace = "service_uid"
	// KeyspaceDeployableUnitUID represents the canonical deployable-unit
	// identity domain.
	KeyspaceDeployableUnitUID Keyspace = "deployable_unit_uid"
	// KeyspaceTerraformResourceUID represents the canonical Terraform resource
	// identity domain.
	KeyspaceTerraformResourceUID Keyspace = "terraform_resource_uid"
	// KeyspaceTerraformModuleUID represents the canonical Terraform module
	// identity domain.
	KeyspaceTerraformModuleUID Keyspace = "terraform_module_uid"
	// KeyspaceCloudResourceUID represents the canonical cloud resource
	// identity domain.
	KeyspaceCloudResourceUID Keyspace = "cloud_resource_uid"
	// KeyspaceKubernetesWorkloadUID represents the canonical live Kubernetes
	// workload identity domain. The live-workload edge slice (#388 PR3) gates
	// its RUNS/DRIFTS_FROM edge projection on this keyspace's
	// canonical-nodes-committed phase exactly as the AWS relationship edge
	// gates on KeyspaceCloudResourceUID (#805).
	KeyspaceKubernetesWorkloadUID Keyspace = "kubernetes_workload_uid"
	// KeyspaceSecurityGroupEndpointUID represents the canonical
	// security-group network-reachability endpoint identity domain: the
	// CidrBlock and PrefixList nodes a security_group_rule fact's source
	// endpoint materializes (issue #1135 PR2a). The ALLOWS_INGRESS/EGRESS
	// edge slice (#1135 PR2b) gates its edge projection on this keyspace's
	// canonical-nodes-committed phase exactly as the AWS relationship edge
	// gates on KeyspaceCloudResourceUID (#805), so edges never resolve
	// against endpoint nodes that have not committed.
	KeyspaceSecurityGroupEndpointUID Keyspace = "security_group_endpoint_uid"
	// KeyspaceSecurityGroupRuleUID represents the canonical security-group
	// reachability rule identity domain: the :SecurityGroupRule nodes a
	// security_group_rule fact materializes (issue #1135 PR2b, Option D). The
	// ALLOWS_INGRESS/EGRESS and TO edge slice gates its edge projection on
	// THREE canonical-nodes-committed phases — this rule-node keyspace, the
	// endpoint keyspace (KeyspaceSecurityGroupEndpointUID, the
	// CidrBlock/PrefixList nodes), and the cloud-resource keyspace
	// (KeyspaceCloudResourceUID, the SG nodes) — so an edge never resolves
	// against any endpoint node that has not committed.
	KeyspaceSecurityGroupRuleUID Keyspace = "security_group_rule_uid"
	// KeyspaceWebhookEventUID represents the canonical webhook event identity
	// domain.
	KeyspaceWebhookEventUID Keyspace = "webhook_event_uid"
	// KeyspaceCrossRepoEvidence represents the reducer readiness domain for
	// deferred backward relationship evidence during bootstrap.
	KeyspaceCrossRepoEvidence Keyspace = "cross_repo_evidence"
	// KeyspaceAPIEndpointRepoPath represents the property-keyed presence
	// domain for materialized :Endpoint nodes, keyed by (repo_id, path)
	// rather than the workload-scoped uid. The handles_route
	// shared-projection domain carries the repo_id and path it MATCHes on but
	// not the per-workload uid, so its presence gate (#2809) keys on this
	// domain. It reuses the uid-exact EndpointPresence primitive (#1380) with
	// a synthesized repo_id\x00path uid, so it never resolves a
	// Function-[:HANDLES_ROUTE]->Endpoint edge against an Endpoint that has
	// not committed.
	KeyspaceAPIEndpointRepoPath Keyspace = "api_endpoint_repo_path"
	// KeyspaceRepoWorkloadPresence represents the property-keyed presence
	// domain for committed :Workload nodes, keyed by repo_id. The runs_in
	// shared-projection domain binds a handler Function to every Workload its
	// Repository DEFINES; it carries the repo_id it MATCHes on but not the
	// per-workload uid, so its presence gate (#2855) keys on this domain. It
	// reuses the uid-exact EndpointPresence primitive (#1380) with the
	// repo_id as the synthesized uid, so it never resolves a
	// Function-[:RUNS_IN]->Workload edge against a repo whose Workloads have
	// not committed.
	KeyspaceRepoWorkloadPresence Keyspace = "repo_workload"
)
