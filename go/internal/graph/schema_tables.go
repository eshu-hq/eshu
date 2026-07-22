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

	// Terraform entity constraints. TerraformResource is config-declared only
	// as of #5443 (state-observed resources now MERGE under
	// TerraformStateResource, uid-constrained only — see uidConstraintLabels
	// below); this composite constraint's (name, path, line_number) shape is
	// meaningful again because every row now has a real file path and line.
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
	// TerraformStateResource is the state-side sibling of TerraformResource
	// (#5443). Before this split, both config-declared and Terraform-state-
	// observed resources shared the TerraformResource label; the state-side
	// canonical writer (tfstate_canonical_writer.go) now MERGEs its own label
	// so config-declared and state-applied resources are distinguishable by
	// label alone, with zero heuristics. Only a uid uniqueness constraint is
	// registered here (see schema_tables.go's Terraform entity constraints
	// comment for why the composite (name, path, line_number) shape does not
	// carry over: state rows use a synthetic tfstate:// path and a hardcoded
	// line_number of 1, so uid — already a collision-resistant sha256 over
	// kind/scope/lineage/address — is state resources' only meaningful
	// identity key).
	"TerraformStateResource",
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
