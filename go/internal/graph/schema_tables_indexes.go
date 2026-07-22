// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

// This file holds the secondary-index and fulltext-index DDL tables split out
// of schema_tables.go to stay under the repo's 500-line file cap. Consumed by
// schema.go the same way schema_tables.go's constraint tables are; add new
// performance, NornicDB-only merge-lookup, or fulltext indexes here.

// schemaPerformanceIndexes lists secondary indexes that improve query
// performance for common access patterns.
var schemaPerformanceIndexes = []string{
	"CREATE INDEX function_lang IF NOT EXISTS FOR (f:Function) ON (f.lang)",
	// Function and inheritance edge retractions anchor cleanup by repo_id or
	// changed file path. Keep those cleanup passes index-backed on NornicDB
	// instead of scanning every code-entity label in large corpora.
	"CREATE INDEX function_repo_id IF NOT EXISTS FOR (f:Function) ON (f.repo_id)",
	"CREATE INDEX function_path IF NOT EXISTS FOR (f:Function) ON (f.path)",
	"CREATE INDEX shell_command_repo_id IF NOT EXISTS FOR (s:ShellCommand) ON (s.repo_id)",
	"CREATE INDEX shell_command_path IF NOT EXISTS FOR (s:ShellCommand) ON (s.path)",
	"CREATE INDEX class_repo_id IF NOT EXISTS FOR (c:Class) ON (c.repo_id)",
	"CREATE INDEX class_path IF NOT EXISTS FOR (c:Class) ON (c.path)",
	"CREATE INDEX interface_repo_id IF NOT EXISTS FOR (i:Interface) ON (i.repo_id)",
	"CREATE INDEX interface_path IF NOT EXISTS FOR (i:Interface) ON (i.path)",
	"CREATE INDEX trait_repo_id IF NOT EXISTS FOR (t:Trait) ON (t.repo_id)",
	"CREATE INDEX trait_path IF NOT EXISTS FOR (t:Trait) ON (t.path)",
	"CREATE INDEX struct_repo_id IF NOT EXISTS FOR (s:Struct) ON (s.repo_id)",
	"CREATE INDEX struct_path IF NOT EXISTS FOR (s:Struct) ON (s.path)",
	"CREATE INDEX enum_repo_id IF NOT EXISTS FOR (e:Enum) ON (e.repo_id)",
	"CREATE INDEX enum_path IF NOT EXISTS FOR (e:Enum) ON (e.path)",
	"CREATE INDEX protocol_repo_id IF NOT EXISTS FOR (p:Protocol) ON (p.repo_id)",
	"CREATE INDEX protocol_path IF NOT EXISTS FOR (p:Protocol) ON (p.path)",
	"CREATE INDEX class_lang IF NOT EXISTS FOR (c:Class) ON (c.lang)",
	"CREATE INDEX annotation_lang IF NOT EXISTS FOR (a:Annotation) ON (a.lang)",
	"CREATE INDEX k8s_kind IF NOT EXISTS FOR (k:K8sResource) ON (k.kind)",
	"CREATE INDEX k8s_namespace IF NOT EXISTS FOR (k:K8sResource) ON (k.namespace)",
	// KustomizeOverlay repo_id index backs the #5445 EXTENDS_BASE resolver's
	// full-repo read (go/internal/storage/cypher, "list every KustomizeOverlay
	// in this repo"). The only existing constraint on this label
	// (kustomize_unique, ko.path IS UNIQUE) is not repo-scoped, so without this
	// index a repo-scoped WHERE ko.repo_id = $repo_id predicate falls back to a
	// full KustomizeOverlay label scan -- measured directly against a pinned
	// eshu-nornicdb-pr261:149245885258 instance seeded with 100,000
	// KustomizeOverlay nodes across 5,000 repos (20 per repo): warm median
	// query time for the target repo's 20 rows dropped from 280.7us to
	// 241.7us (p90 385.3us to 311.5us) after this index reached ONLINE (SHOW
	// INDEXES). The pinned NornicDB Bolt transport returns no PROFILE/EXPLAIN
	// plan metadata (reproduced: both return zero rows and a nil plan),
	// matching the documented limitation in
	// docs/internal/evidence/5410-sql-relationships-performance.md, so
	// wall-clock timing on this discriminating shape is the available proof,
	// not a plan-tree db-hit count.
	"CREATE INDEX kustomize_overlay_repo_id IF NOT EXISTS FOR (ko:KustomizeOverlay) ON (ko.repo_id)",
	// KubernetesWorkload lookup indexes back graph-backed reads of the live
	// workload node (the #388 PR3 RUNS edge anchors on the uid, which the
	// generated uid uniqueness constraint already indexes; cluster_id and
	// namespace back scoped fan-out reads). Without these, a per-cluster or
	// per-namespace read falls back to a KubernetesWorkload label scan.
	"CREATE INDEX kubernetes_workload_cluster_id IF NOT EXISTS FOR (w:KubernetesWorkload) ON (w.cluster_id)",
	"CREATE INDEX kubernetes_workload_namespace IF NOT EXISTS FOR (w:KubernetesWorkload) ON (w.namespace)",
	// CloudResource lookup indexes back the AWS relationship edge join
	// (issue #805). The edge projection resolves both endpoints to a
	// CloudResource.uid using an in-memory index built from aws_resource facts,
	// but graph-backed reads (impact, compare, entity-map) anchor on arn,
	// resource_id, and resource_type. Without these, those reads fall back to a
	// CloudResource label scan.
	"CREATE INDEX cloud_resource_arn IF NOT EXISTS FOR (r:CloudResource) ON (r.arn)",
	"CREATE INDEX cloud_resource_resource_id IF NOT EXISTS FOR (r:CloudResource) ON (r.resource_id)",
	"CREATE INDEX cloud_resource_type IF NOT EXISTS FOR (r:CloudResource) ON (r.resource_type)",
	// Secrets/IAM graph-projection node scope lookups back the scoped
	// retract-before-reproject of reducer-owned SecretsIAM* nodes (ADR #1314 §8).
	"CREATE INDEX secrets_iam_service_account_scope_id IF NOT EXISTS FOR (s:SecretsIAMServiceAccount) ON (s.scope_id)",
	"CREATE INDEX secrets_iam_vault_auth_role_scope_id IF NOT EXISTS FOR (v:SecretsIAMVaultAuthRole) ON (v.scope_id)",
	"CREATE INDEX secrets_iam_vault_policy_scope_id IF NOT EXISTS FOR (p:SecretsIAMVaultPolicy) ON (p.scope_id)",
	"CREATE INDEX secrets_iam_secret_metadata_path_scope_id IF NOT EXISTS FOR (s:SecretsIAMSecretMetadataPath) ON (s.scope_id)",
	// CidrBlock and PrefixList lookup indexes back the security-group
	// network-reachability edge join (issue #1135 PR2b) and the internet-exposure
	// read. The edge projection resolves a rule's CIDR/prefix endpoint to a
	// canonical uid (already indexed by the generated uid uniqueness constraint),
	// but graph-backed reads anchor on the human-readable cidr / prefix_list_id and
	// the is_internet flag. Without these, those reads fall back to a label scan.
	"CREATE INDEX cidr_block_cidr IF NOT EXISTS FOR (c:CidrBlock) ON (c.cidr)",
	"CREATE INDEX cidr_block_is_internet IF NOT EXISTS FOR (c:CidrBlock) ON (c.is_internet)",
	"CREATE INDEX prefix_list_prefix_list_id IF NOT EXISTS FOR (p:PrefixList) ON (p.prefix_list_id)",
	// SecurityGroupRule lookup indexes back the network-reachability edge
	// projection (issue #1135 PR2b) and the internet-exposure read. The
	// ALLOWS_INGRESS/EGRESS and TO edges anchor on the rule uid (already indexed
	// by the generated uid uniqueness constraint), but graph-backed reads filter
	// reachability by direction and surface internet-open rules by is_internet.
	// Without these, those reads fall back to a SecurityGroupRule label scan.
	"CREATE INDEX security_group_rule_direction IF NOT EXISTS FOR (r:SecurityGroupRule) ON (r.direction)",
	"CREATE INDEX security_group_rule_is_internet IF NOT EXISTS FOR (r:SecurityGroupRule) ON (r.is_internet)",
	"CREATE INDEX tf_resource_type IF NOT EXISTS FOR (r:TerraformResource) ON (r.resource_type)",
	// Indexes that back the infrastructure resource aggregate (#690)
	// grouped-count hot path on the TerraformResource label (the largest
	// infra label in typical deployments). Aggregates filtered by
	// `category=terraform` plus any of `provider` / `environment` /
	// `resource_service` / `resource_category` are eligible for these
	// indexes. The only matching indexed property on the other infra
	// labels today is `k8s_kind` on K8sResource (declared above), so
	// `category=k8s` + `kind=<value>` is the other supported hot path.
	// Aggregates over Argo CD, Crossplane, Helm, or CloudFormation
	// labels currently fall back to a label-set scan; matching indexes
	// can ship in follow-ups as their volume warrants.
	"CREATE INDEX tf_resource_provider IF NOT EXISTS FOR (r:TerraformResource) ON (r.provider)",
	"CREATE INDEX tf_resource_environment IF NOT EXISTS FOR (r:TerraformResource) ON (r.environment)",
	"CREATE INDEX tf_resource_service IF NOT EXISTS FOR (r:TerraformResource) ON (r.resource_service)",
	"CREATE INDEX tf_resource_category IF NOT EXISTS FOR (r:TerraformResource) ON (r.resource_category)",
	// Backs the #5443 MATCHES_STATE edge write: the graph writer anchors on
	// `{repo_id, name}` where name is the config-declared bare address (e.g.
	// "aws_instance.web") -- the most selective property available (an
	// address is typically unique within a repo), so this index, not
	// tf_resource_unique's (name, path, line_number) composite, is the
	// intended lookup path.
	"CREATE INDEX tf_resource_name IF NOT EXISTS FOR (r:TerraformResource) ON (r.name)",
	// TerraformStateResource property indexes (#5443 P1 review finding).
	// TerraformStateResource joined every query path TerraformResource's six
	// property indexes above back (infra_resource_aggregates.go's per-label
	// aggregate branches and entity_map_resolver.go's terraform candidate
	// lookups both iterate every label in infraCategoryLabels["terraform"] /
	// entityMapResolverQueries' terraform case with the SAME filter clauses
	// regardless of label), so without a matching index those become
	// unindexed TerraformStateResource label scans. Only three of
	// TerraformResource's six sibling properties are ever actually written
	// onto a TerraformStateResource node
	// (canonicalTerraformStateResourceUpsertCypher, tfstate_canonical_writer.go):
	// resource_type, name, and address (name and address both hold the state
	// address; address is the field entity_map_resolver.go actually filters
	// on). provider, environment, resource_service, and resource_category
	// are config-only concepts with no TerraformStateResource equivalent
	// field, so no state-side node ever carries them -- an index there would
	// back a predicate that can never match a single row, so it is
	// deliberately not added.
	//   - tf_state_resource_type backs infra_resource_aggregates.go's
	//     ResourceType/Kind filter clauses
	//     ("(n.resource_type = $resource_type OR ...)",
	//     "(n.kind = $kind OR n.resource_type = $kind OR ...)"), which run
	//     against the TerraformStateResource branch whenever
	//     filter.Category is "terraform" or unset.
	//   - tf_state_resource_name backs entity_map_resolver.go's
	//     `MATCH (n:TerraformStateResource {name: $from})` candidate lookup
	//     (entityMapResolverQueries' "terraform"/"tf"/"terraform_resource"
	//     case and entityMapGenericResolverQueries' fallback), a
	//     bound-property MATCH that falls back to a full label scan without
	//     a backing index on the pinned NornicDB executor.
	//   - tf_state_resource_address backs entity_map_resolver.go's
	//     `MATCH (n:TerraformStateResource {address: $from})` candidate
	//     lookup, which both call sites try BEFORE the name lookup above
	//     (rank 3 vs. rank 5 in the terraform/tf case; the only
	//     TerraformStateResource property lookup besides uid in
	//     entityMapGenericResolverQueries' fallback) -- the higher-priority,
	//     more commonly hit branch, and the one the prior fix missed.
	"CREATE INDEX tf_state_resource_type IF NOT EXISTS FOR (r:TerraformStateResource) ON (r.resource_type)",
	"CREATE INDEX tf_state_resource_name IF NOT EXISTS FOR (r:TerraformStateResource) ON (r.name)",
	"CREATE INDEX tf_state_resource_address IF NOT EXISTS FOR (r:TerraformStateResource) ON (r.address)",
	"CREATE INDEX workload_name IF NOT EXISTS FOR (w:Workload) ON (w.name)",
	"CREATE INDEX workload_repo_id IF NOT EXISTS FOR (w:Workload) ON (w.repo_id)",
	"CREATE INDEX workload_instance_environment IF NOT EXISTS FOR (i:WorkloadInstance) ON (i.environment)",
	"CREATE INDEX workload_instance_workload_id IF NOT EXISTS FOR (i:WorkloadInstance) ON (i.workload_id)",
	"CREATE INDEX workload_instance_repo_id IF NOT EXISTS FOR (i:WorkloadInstance) ON (i.repo_id)",
	"CREATE INDEX container_image_digest IF NOT EXISTS FOR (i:ContainerImage) ON (i.digest)",
	"CREATE INDEX container_image_index_digest IF NOT EXISTS FOR (i:ContainerImageIndex) ON (i.digest)",
	"CREATE INDEX container_image_descriptor_digest IF NOT EXISTS FOR (d:ContainerImageDescriptor) ON (d.digest)",
	"CREATE INDEX container_image_tag_observation_ref IF NOT EXISTS FOR (t:ContainerImageTagObservation) ON (t.image_ref)",
	"CREATE INDEX package_ecosystem IF NOT EXISTS FOR (p:Package) ON (p.ecosystem)",
	"CREATE INDEX package_normalized_name IF NOT EXISTS FOR (p:Package) ON (p.normalized_name)",
	// Indexes that back the package-registry aggregate (#689) grouped count
	// hot path. Without these, `MATCH (p:Package) WHERE p.<prop> = $v` falls
	// back to a label scan and the cookbook Area-5 hot path is forfeited.
	"CREATE INDEX package_registry IF NOT EXISTS FOR (p:Package) ON (p.registry)",
	"CREATE INDEX package_namespace IF NOT EXISTS FOR (p:Package) ON (p.namespace)",
	"CREATE INDEX package_package_manager IF NOT EXISTS FOR (p:Package) ON (p.package_manager)",
	"CREATE INDEX package_visibility IF NOT EXISTS FOR (p:Package) ON (p.visibility)",
	"CREATE INDEX package_version_package_id IF NOT EXISTS FOR (v:PackageVersion) ON (v.package_id)",
	"CREATE INDEX package_dependency_package_id IF NOT EXISTS FOR (d:PackageDependency) ON (d.package_id)",
	"CREATE INDEX package_dependency_version_id IF NOT EXISTS FOR (d:PackageDependency) ON (d.version_id)",
	"CREATE INDEX function_name IF NOT EXISTS FOR (f:Function) ON (f.name)",
	"CREATE INDEX class_name IF NOT EXISTS FOR (c:Class) ON (c.name)",
}

// nornicDBMergeLookupIndexes are explicit property indexes required for
// NornicDB's schema-backed MERGE lookup path. Neo4j uniqueness constraints
// already create backing indexes for these schemas; keep this NornicDB-only to
// avoid duplicate-index warnings on Neo4j.
//
// The SourceLocalRecord.scope_id and Parameter.path entries cover labels whose
// composite UNIQUE constraints declared in schemaConstraints are silently
// dropped by nornicDBSchemaConstraint because NornicDB rejects composite
// constraint syntax. Without a backing single-property index, MERGE on those
// labels in the source-local projection writer and the canonical
// HAS_PARAMETER edge falls through to a full label scan, which the comment
// above the source_local_record_unique constraint calls out as O(n²) for
// large-repo projection.
var nornicDBMergeLookupIndexes = []string{
	"CREATE INDEX nornicdb_repository_id_lookup IF NOT EXISTS FOR (r:Repository) ON (r.id)",
	"CREATE INDEX nornicdb_function_legacy_id_lookup IF NOT EXISTS FOR (n:Function) ON (n.id)",
	"CREATE INDEX nornicdb_directory_path_lookup IF NOT EXISTS FOR (d:Directory) ON (d.path)",
	"CREATE INDEX nornicdb_file_path_lookup IF NOT EXISTS FOR (f:File) ON (f.path)",
	"CREATE INDEX nornicdb_workload_id_lookup IF NOT EXISTS FOR (w:Workload) ON (w.id)",
	"CREATE INDEX nornicdb_workload_instance_id_lookup IF NOT EXISTS FOR (i:WorkloadInstance) ON (i.id)",
	"CREATE INDEX nornicdb_platform_id_lookup IF NOT EXISTS FOR (p:Platform) ON (p.id)",
	"CREATE INDEX nornicdb_endpoint_id_lookup IF NOT EXISTS FOR (e:Endpoint) ON (e.id)",
	"CREATE INDEX nornicdb_cloud_action_id_lookup IF NOT EXISTS FOR (a:CloudAction) ON (a.id)",
	"CREATE INDEX nornicdb_codeowner_team_ref_lookup IF NOT EXISTS FOR (t:CodeownerTeam) ON (t.ref)",
	"CREATE INDEX nornicdb_evidence_artifact_id_lookup IF NOT EXISTS FOR (a:EvidenceArtifact) ON (a.id)",
	// KubernetesWorkload has a cluster_id/namespace index pair above but no
	// .id-property index, unlike the other by-id-anchored labels this handler
	// serves. The kubernetes_workload_node_writer sets w.id = row.uid (the live
	// object identity), and analyze_infra_relationships/getRelationships anchors
	// its MATCH on n.id; without this index that anchor falls back to a
	// KubernetesWorkload label scan (#5436).
	"CREATE INDEX nornicdb_kubernetes_workload_id_lookup IF NOT EXISTS FOR (w:KubernetesWorkload) ON (w.id)",
	"CREATE INDEX nornicdb_environment_name_lookup IF NOT EXISTS FOR (e:Environment) ON (e.name)",
	"CREATE INDEX nornicdb_source_local_record_scope_lookup IF NOT EXISTS FOR (n:SourceLocalRecord) ON (n.scope_id)",
	"CREATE INDEX nornicdb_parameter_path_lookup IF NOT EXISTS FOR (n:Parameter) ON (n.path)",
}

// schemaFulltextIndexes lists Neo4j full-text index creation statements.
// The primary form uses the procedure-based API; the fallback uses modern
// CREATE FULLTEXT INDEX syntax for newer Neo4j versions.
var schemaFulltextIndexes = []fulltextIndex{
	{
		primary: "CALL db.index.fulltext.createNodeIndex('code_search_index', " +
			"['Function', 'Class', 'Variable'], ['name', 'source', 'docstring'])",
		fallback: "CREATE FULLTEXT INDEX code_search_index IF NOT EXISTS " +
			"FOR (n:Function|Class|Variable) ON EACH [n.name, n.source, n.docstring]",
	},
	{
		primary: "CALL db.index.fulltext.createNodeIndex('infra_search_index', " +
			"['K8sResource', 'TerraformResource', 'TerraformStateResource', " +
			"'ArgoCDApplication', " +
			"'ArgoCDApplicationSet', 'AtlantisProject', 'AtlantisWorkflow', 'GitlabPipeline', 'GitlabJob', 'CrossplaneXRD', 'CrossplaneComposition', " +
			"'CrossplaneClaim', 'KustomizeOverlay', 'HelmChart', 'HelmValues', " +
			"'TerraformVariable', 'TerraformOutput', 'TerraformModule', " +
			"'TerraformDataSource', 'TerraformProvider', 'TerraformLocal', " +
			"'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock', " +
			"'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', " +
			"'TerragruntConfig', 'CloudFormationResource', " +
			"'CloudFormationParameter', 'CloudFormationOutput'], " +
			"['name', 'kind', 'resource_type'])",
		// TerraformStateResource joins the infra_search_index label set
		// (#5443 P1 review finding): it is already in allInfraLabels
		// (internal/query/infra.go) and infraCategoryLabels["terraform"], but
		// this fulltext label set previously listed every other Terraform*
		// label and omitted it, so a fulltext infra search silently never
		// surfaced state-observed Terraform resources. It carries the same
		// `name` and `resource_type` properties every other label in this
		// index carries (canonicalTerraformStateResourceUpsertCypher,
		// tfstate_canonical_writer.go), so no property-list change is needed.
		fallback: "CREATE FULLTEXT INDEX infra_search_index IF NOT EXISTS " +
			"FOR (n:K8sResource|TerraformResource|TerraformStateResource|" +
			"ArgoCDApplication|" +
			"ArgoCDApplicationSet|AtlantisProject|AtlantisWorkflow|GitlabPipeline|GitlabJob|CrossplaneXRD|CrossplaneComposition|" +
			"CrossplaneClaim|KustomizeOverlay|HelmChart|HelmValues|" +
			"TerraformVariable|TerraformOutput|TerraformModule|" +
			"TerraformDataSource|TerraformProvider|TerraformLocal|" +
			"TerraformBackend|TerraformImport|TerraformMovedBlock|" +
			"TerraformRemovedBlock|TerraformCheck|TerraformLockProvider|" +
			"TerragruntConfig|CloudFormationResource|" +
			"CloudFormationParameter|CloudFormationOutput) " +
			"ON EACH [n.name, n.kind, n.resource_type]",
	},
}
