// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package graph

// This file holds the flat, checked-in schema DDL tables consumed by schema.go.
// Keeping the data literals here keeps schema.go focused on the EnsureSchema
// orchestration and backend-dialect logic. Add new graph entity labels, indexes,
// and constraints to the appropriate table below.

// schemaConstraints lists uniqueness and node-key constraints that must exist
// before any graph writes occur. The order stays stable so schema diffs remain
// easy to audit across releases.
var schemaConstraints = []string{
	// Repository identity
	"CREATE CONSTRAINT repository_id IF NOT EXISTS FOR (r:Repository) REQUIRE r.id IS UNIQUE",
	"CREATE CONSTRAINT repository_path IF NOT EXISTS FOR (r:Repository) REQUIRE r.path IS UNIQUE",

	// File identity
	"CREATE CONSTRAINT path IF NOT EXISTS FOR (f:File) REQUIRE f.path IS UNIQUE",

	// Directory identity
	"CREATE CONSTRAINT directory_path IF NOT EXISTS FOR (d:Directory) REQUIRE d.path IS UNIQUE",

	// Evidence story identity
	"CREATE CONSTRAINT evidence_artifact_id IF NOT EXISTS FOR (a:EvidenceArtifact) REQUIRE a.id IS UNIQUE",
	"CREATE CONSTRAINT environment_name IF NOT EXISTS FOR (e:Environment) REQUIRE e.name IS UNIQUE",

	// Code entity node-key constraints
	"CREATE CONSTRAINT function_unique IF NOT EXISTS FOR (f:Function) REQUIRE (f.name, f.path, f.line_number) IS UNIQUE",
	"CREATE CONSTRAINT class_unique IF NOT EXISTS FOR (c:Class) REQUIRE (c.name, c.path, c.line_number) IS UNIQUE",
	"CREATE CONSTRAINT trait_unique IF NOT EXISTS FOR (t:Trait) REQUIRE (t.name, t.path, t.line_number) IS UNIQUE",
	"CREATE CONSTRAINT interface_unique IF NOT EXISTS FOR (i:Interface) REQUIRE (i.name, i.path, i.line_number) IS UNIQUE",
	"CREATE CONSTRAINT macro_unique IF NOT EXISTS FOR (m:Macro) REQUIRE (m.name, m.path, m.line_number) IS UNIQUE",
	"CREATE CONSTRAINT variable_unique IF NOT EXISTS FOR (v:Variable) REQUIRE (v.name, v.path, v.line_number) IS UNIQUE",
	// Module uses a regular index instead of a uniqueness constraint because
	// the canonical import-graph path MERGEs on name (globally shared) while
	// the semantic entity path MERGEs on uid (per-repo). A global name
	// uniqueness constraint causes ConstraintValidationFailed when multiple
	// repos share module names like "consts" or "index".
	"CREATE INDEX module_name_lookup IF NOT EXISTS FOR (m:Module) ON (m.name)",
	"CREATE CONSTRAINT struct_cpp IF NOT EXISTS FOR (cstruct: Struct) REQUIRE (cstruct.name, cstruct.path, cstruct.line_number) IS UNIQUE",
	"CREATE CONSTRAINT enum_cpp IF NOT EXISTS FOR (cenum: Enum) REQUIRE (cenum.name, cenum.path, cenum.line_number) IS UNIQUE",
	"CREATE CONSTRAINT union_cpp IF NOT EXISTS FOR (cunion: Union) REQUIRE (cunion.name, cunion.path, cunion.line_number) IS UNIQUE",
	"CREATE CONSTRAINT annotation_unique IF NOT EXISTS FOR (a:Annotation) REQUIRE (a.name, a.path, a.line_number) IS UNIQUE",
	"CREATE CONSTRAINT record_unique IF NOT EXISTS FOR (r:Record) REQUIRE (r.name, r.path, r.line_number) IS UNIQUE",
	"CREATE CONSTRAINT property_unique IF NOT EXISTS FOR (p:Property) REQUIRE (p.name, p.path, p.line_number) IS UNIQUE",

	// Infrastructure entity constraints
	"CREATE CONSTRAINT k8s_resource_unique IF NOT EXISTS FOR (k:K8sResource) REQUIRE (k.name, k.kind, k.path, k.line_number) IS UNIQUE",
	"CREATE CONSTRAINT argocd_app_unique IF NOT EXISTS FOR (a:ArgoCDApplication) REQUIRE (a.name, a.path, a.line_number) IS UNIQUE",
	"CREATE CONSTRAINT argocd_appset_unique IF NOT EXISTS FOR (a:ArgoCDApplicationSet) REQUIRE (a.name, a.path, a.line_number) IS UNIQUE",
	"CREATE CONSTRAINT atlantis_project_unique IF NOT EXISTS FOR (p:AtlantisProject) REQUIRE (p.name, p.path, p.line_number) IS UNIQUE",
	"CREATE CONSTRAINT atlantis_workflow_unique IF NOT EXISTS FOR (w:AtlantisWorkflow) REQUIRE (w.name, w.path, w.line_number) IS UNIQUE",
	"CREATE CONSTRAINT gitlab_pipeline_unique IF NOT EXISTS FOR (p:GitlabPipeline) REQUIRE (p.name, p.path, p.line_number) IS UNIQUE",
	"CREATE CONSTRAINT gitlab_job_unique IF NOT EXISTS FOR (j:GitlabJob) REQUIRE (j.name, j.path, j.line_number) IS UNIQUE",
	"CREATE CONSTRAINT xrd_unique IF NOT EXISTS FOR (x:CrossplaneXRD) REQUIRE (x.name, x.path, x.line_number) IS UNIQUE",
	"CREATE CONSTRAINT composition_unique IF NOT EXISTS FOR (c:CrossplaneComposition) REQUIRE (c.name, c.path, c.line_number) IS UNIQUE",
	"CREATE CONSTRAINT claim_unique IF NOT EXISTS FOR (cl:CrossplaneClaim) REQUIRE (cl.name, cl.kind, cl.path, cl.line_number) IS UNIQUE",
	"CREATE CONSTRAINT kustomize_unique IF NOT EXISTS FOR (ko:KustomizeOverlay) REQUIRE ko.path IS UNIQUE",
	"CREATE CONSTRAINT helm_chart_unique IF NOT EXISTS FOR (h:HelmChart) REQUIRE (h.name, h.path) IS UNIQUE",
	"CREATE CONSTRAINT helm_values_unique IF NOT EXISTS FOR (hv:HelmValues) REQUIRE hv.path IS UNIQUE",
	"CREATE CONSTRAINT helm_value_definition_unique IF NOT EXISTS FOR (hvd:HelmValueDefinition) REQUIRE (hvd.name, hvd.path, hvd.line_number) IS UNIQUE",
	"CREATE CONSTRAINT helm_template_value_usage_unique IF NOT EXISTS FOR (htvu:HelmTemplateValueUsage) REQUIRE (htvu.name, htvu.path, htvu.line_number) IS UNIQUE",

	// Terraform entity constraints
	"CREATE CONSTRAINT tf_resource_unique IF NOT EXISTS FOR (r:TerraformResource) REQUIRE (r.name, r.path, r.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_variable_unique IF NOT EXISTS FOR (v:TerraformVariable) REQUIRE (v.name, v.path, v.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_output_unique IF NOT EXISTS FOR (o:TerraformOutput) REQUIRE (o.name, o.path, o.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_module_unique IF NOT EXISTS FOR (m:TerraformModule) REQUIRE (m.name, m.path) IS UNIQUE",
	"CREATE CONSTRAINT tf_datasource_unique IF NOT EXISTS FOR (ds:TerraformDataSource) REQUIRE (ds.name, ds.path, ds.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_provider_unique IF NOT EXISTS FOR (p:TerraformProvider) REQUIRE (p.name, p.path, p.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_local_unique IF NOT EXISTS FOR (l:TerraformLocal) REQUIRE (l.name, l.path, l.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_backend_unique IF NOT EXISTS FOR (b:TerraformBackend) REQUIRE (b.name, b.path, b.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_import_unique IF NOT EXISTS FOR (i:TerraformImport) REQUIRE (i.name, i.path, i.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_moved_unique IF NOT EXISTS FOR (m:TerraformMovedBlock) REQUIRE (m.name, m.path, m.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_removed_unique IF NOT EXISTS FOR (r:TerraformRemovedBlock) REQUIRE (r.name, r.path, r.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_check_unique IF NOT EXISTS FOR (c:TerraformCheck) REQUIRE (c.name, c.path, c.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tf_lock_provider_unique IF NOT EXISTS FOR (p:TerraformLockProvider) REQUIRE (p.name, p.path, p.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tg_config_unique IF NOT EXISTS FOR (tg:TerragruntConfig) REQUIRE tg.path IS UNIQUE",
	"CREATE CONSTRAINT tg_dependency_unique IF NOT EXISTS FOR (td:TerragruntDependency) REQUIRE (td.name, td.path, td.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tg_input_unique IF NOT EXISTS FOR (ti:TerragruntInput) REQUIRE (ti.name, ti.path, ti.line_number) IS UNIQUE",
	"CREATE CONSTRAINT tg_local_unique IF NOT EXISTS FOR (tl:TerragruntLocal) REQUIRE (tl.name, tl.path, tl.line_number) IS UNIQUE",

	// Type annotation constraint
	"CREATE CONSTRAINT type_annotation_unique IF NOT EXISTS FOR (ta:TypeAnnotation) REQUIRE (ta.name, ta.path, ta.line_number) IS UNIQUE",

	// CloudFormation entity constraints
	"CREATE CONSTRAINT cf_resource_unique IF NOT EXISTS FOR (r:CloudFormationResource) REQUIRE (r.name, r.path, r.line_number) IS UNIQUE",
	"CREATE CONSTRAINT cf_parameter_unique IF NOT EXISTS FOR (p:CloudFormationParameter) REQUIRE (p.name, p.path, p.line_number) IS UNIQUE",
	"CREATE CONSTRAINT cf_output_unique IF NOT EXISTS FOR (o:CloudFormationOutput) REQUIRE (o.name, o.path, o.line_number) IS UNIQUE",

	// Ecosystem / workload constraints
	"CREATE CONSTRAINT ecosystem_name IF NOT EXISTS FOR (e:Ecosystem) REQUIRE e.name IS UNIQUE",
	"CREATE CONSTRAINT tier_name IF NOT EXISTS FOR (t:Tier) REQUIRE t.name IS UNIQUE",
	"CREATE CONSTRAINT workload_id IF NOT EXISTS FOR (w:Workload) REQUIRE w.id IS UNIQUE",
	"CREATE CONSTRAINT workload_instance_id IF NOT EXISTS FOR (i:WorkloadInstance) REQUIRE i.id IS UNIQUE",
	"CREATE CONSTRAINT endpoint_id IF NOT EXISTS FOR (e:Endpoint) REQUIRE e.id IS UNIQUE",

	// Cloud action identity — the closed-catalog action a Function invokes via an
	// AWS SDK call (#2723). Keyed by id so the inline INVOKES_CLOUD_ACTION MERGE
	// is an O(1) lookup rather than a CloudAction label scan per row.
	"CREATE CONSTRAINT cloud_action_id IF NOT EXISTS FOR (a:CloudAction) REQUIRE a.id IS UNIQUE",

	// Codeowners team identity — the owner token a CODEOWNERS rule names
	// verbatim (issue #5419 Phase 3). Keyed by ref so the inline
	// DECLARES_CODEOWNER MERGE is an O(1) lookup rather than a CodeownerTeam
	// label scan per row.
	"CREATE CONSTRAINT codeowner_team_ref IF NOT EXISTS FOR (t:CodeownerTeam) REQUIRE t.ref IS UNIQUE",

	// Platform identity
	"CREATE CONSTRAINT platform_id IF NOT EXISTS FOR (p:Platform) REQUIRE p.id IS UNIQUE",

	// Source-local projection record identity — required for MERGE performance.
	// Without this constraint, MERGE on SourceLocalRecord does a full label scan
	// per row, turning large-repo projection into O(n²).
	"CREATE CONSTRAINT source_local_record_unique IF NOT EXISTS FOR (n:SourceLocalRecord) REQUIRE (n.scope_id, n.generation_id, n.record_id) IS UNIQUE",

	// Parameter constraint
	"CREATE CONSTRAINT parameter_unique IF NOT EXISTS FOR (p:Parameter) REQUIRE (p.name, p.path, p.function_line_number) IS UNIQUE",
}

// uidConstraintLabels lists entity labels that receive a uid uniqueness
// constraint. The set is maintained here as part of the Go-owned graph schema.
var uidConstraintLabels = []string{
	"AnalyticsModel",
	"Annotation",
	"ArgoCDApplication",
	"ArgoCDApplicationSet",
	"AtlantisProject",
	"AtlantisWorkflow",
	"CidrBlock",
	"Class",
	"CloudResource",
	"CloudFormationOutput",
	"CloudFormationParameter",
	"CloudFormationResource",
	"CodeTaintEvidence",
	"ContainerImage",
	"ContainerImageDescriptor",
	"ContainerImageIndex",
	"ContainerImageTagObservation",
	"CrossplaneClaim",
	"CrossplaneComposition",
	"CrossplaneXRD",
	"DashboardAsset",
	"DataAsset",
	"DataColumn",
	"DataContract",
	"DataOwner",
	"DataQualityCheck",
	"Enum",
	"ExternalPrincipal",
	"File",
	"FluxBucket",
	"FluxGitRepository",
	"FluxHelmRelease",
	"FluxHelmRepository",
	"FluxKustomization",
	"FluxOCIRepository",
	"Function",
	"GitlabJob",
	"GitlabPipeline",
	"HelmChart",
	"HelmValues",
	"HelmValueDefinition",
	"HelmTemplateValueUsage",
	"ImplBlock",
	"IncidentRoutingEvidence",
	"Interface",
	"K8sResource",
	"KubernetesWorkload",
	"KustomizeOverlay",
	"Macro",
	"Module",
	"Package",
	"PackageDependency",
	"PackageRegistryPackageDependency",
	"PackageRegistryPackage",
	"PackageRegistryPackageVersion",
	"PackageVersion",
	"PrefixList",
	"Property",
	"Protocol",
	"ProtocolImplementation",
	"QueryExecution",
	"Record",
	"SecretsIAMSecretMetadataPath",
	"SecretsIAMServiceAccount",
	"SecretsIAMVaultAuthRole",
	"SecretsIAMVaultPolicy",
	"SecurityGroupRule",
	"ShellCommand",
	"SqlColumn",
	"SqlFunction",
	"SqlIndex",
	"SqlMigration",
	"SqlTable",
	"SqlTrigger",
	"SqlView",
	"Struct",
	"TerraformDataSource",
	"TerraformBackend",
	"TerraformCheck",
	"TerraformImport",
	"TerraformLockProvider",
	"TerraformLocal",
	"TerraformModule",
	"TerraformMovedBlock",
	"TerraformOutput",
	"TerraformProvider",
	"TerraformRemovedBlock",
	"TerraformResource",
	"TerraformVariable",
	"TerragruntConfig",
	"TerragruntDependency",
	"TerragruntInput",
	"TerragruntLocal",
	"TypeAlias",
	"TypeAnnotation",
	"Typedef",
	"Trait",
	"Union",
	"Variable",
	"Component",
	"OciImageDescriptor",
	"OciImageIndex",
	"OciImageManifest",
	"OciImageReferrer",
	"OciImageTagObservation",
	"OciRegistryRepository",
}

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
			"['K8sResource', 'TerraformResource', 'ArgoCDApplication', " +
			"'ArgoCDApplicationSet', 'AtlantisProject', 'AtlantisWorkflow', 'GitlabPipeline', 'GitlabJob', 'CrossplaneXRD', 'CrossplaneComposition', " +
			"'CrossplaneClaim', 'KustomizeOverlay', 'HelmChart', 'HelmValues', " +
			"'TerraformVariable', 'TerraformOutput', 'TerraformModule', " +
			"'TerraformDataSource', 'TerraformProvider', 'TerraformLocal', " +
			"'TerraformBackend', 'TerraformImport', 'TerraformMovedBlock', " +
			"'TerraformRemovedBlock', 'TerraformCheck', 'TerraformLockProvider', " +
			"'TerragruntConfig', 'CloudFormationResource', " +
			"'CloudFormationParameter', 'CloudFormationOutput'], " +
			"['name', 'kind', 'resource_type'])",
		fallback: "CREATE FULLTEXT INDEX infra_search_index IF NOT EXISTS " +
			"FOR (n:K8sResource|TerraformResource|ArgoCDApplication|" +
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
