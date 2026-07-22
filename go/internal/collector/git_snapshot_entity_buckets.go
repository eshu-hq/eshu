// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

// snapshotEntityBuckets lists every content-entity bucket the collector reads
// off a parsed file when building content entities (entityBucketsFromParsed)
// and when labeling entity-cap-hit diagnostics. Extracted from
// git_snapshot_native.go to keep that file within the repo file-size budget;
// consumed by git_snapshot_materialization.go and read-only elsewhere.
var snapshotEntityBuckets = []struct {
	bucket string
	label  string
}{
	{bucket: "functions", label: "Function"},
	{bucket: "classes", label: "Class"},
	{bucket: "modules", label: "Module"},
	{bucket: "variables", label: "Variable"},
	{bucket: "type_annotations", label: "TypeAnnotation"},
	{bucket: "traits", label: "Trait"},
	{bucket: "interfaces", label: "Interface"},
	{bucket: "macros", label: "Macro"},
	{bucket: "structs", label: "Struct"},
	{bucket: "enums", label: "Enum"},
	{bucket: "protocols", label: "Protocol"},
	{bucket: "unions", label: "Union"},
	{bucket: "typedefs", label: "Typedef"},
	{bucket: "type_aliases", label: "TypeAlias"},
	{bucket: "annotations", label: "Annotation"},
	{bucket: "records", label: "Record"},
	{bucket: "properties", label: "Property"},
	{bucket: "components", label: "Component"},
	{bucket: "k8s_resources", label: "K8sResource"},
	{bucket: "argocd_applications", label: "ArgoCDApplication"},
	{bucket: "argocd_applicationsets", label: "ArgoCDApplicationSet"},
	{bucket: "crossplane_xrds", label: "CrossplaneXRD"},
	{bucket: "crossplane_compositions", label: "CrossplaneComposition"},
	{bucket: "crossplane_claims", label: "CrossplaneClaim"},
	{bucket: "kustomize_overlays", label: "KustomizeOverlay"},
	{bucket: "helm_charts", label: "HelmChart"},
	{bucket: "helm_values", label: "HelmValues"},
	{bucket: "terraform_resources", label: "TerraformResource"},
	{bucket: "terraform_variables", label: "TerraformVariable"},
	{bucket: "terraform_outputs", label: "TerraformOutput"},
	{bucket: "terraform_modules", label: "TerraformModule"},
	{bucket: "terraform_data_sources", label: "TerraformDataSource"},
	{bucket: "terraform_providers", label: "TerraformProvider"},
	{bucket: "terraform_locals", label: "TerraformLocal"},
	{bucket: "terraform_backends", label: "TerraformBackend"},
	{bucket: "terraform_imports", label: "TerraformImport"},
	{bucket: "terraform_moved_blocks", label: "TerraformMovedBlock"},
	{bucket: "terraform_removed_blocks", label: "TerraformRemovedBlock"},
	{bucket: "terraform_checks", label: "TerraformCheck"},
	{bucket: "terraform_lock_providers", label: "TerraformLockProvider"},
	{bucket: "terragrunt_configs", label: "TerragruntConfig"},
	{bucket: "terragrunt_dependencies", label: "TerragruntDependency"},
	{bucket: "terragrunt_locals", label: "TerragruntLocal"},
	{bucket: "terragrunt_inputs", label: "TerragruntInput"},
	{bucket: "cloudformation_resources", label: "CloudFormationResource"},
	{bucket: "cloudformation_parameters", label: "CloudFormationParameter"},
	{bucket: "cloudformation_outputs", label: "CloudFormationOutput"},
	{bucket: "atlantis_projects", label: "AtlantisProject"},
	{bucket: "atlantis_workflows", label: "AtlantisWorkflow"},
	{bucket: "gitlab_pipelines", label: "GitlabPipeline"},
	{bucket: "gitlab_jobs", label: "GitlabJob"},
	{bucket: "sql_tables", label: "SqlTable"},
	{bucket: "sql_columns", label: "SqlColumn"},
	{bucket: "sql_views", label: "SqlView"},
	{bucket: "sql_functions", label: "SqlFunction"},
	{bucket: "sql_triggers", label: "SqlTrigger"},
	{bucket: "sql_indexes", label: "SqlIndex"},
	{bucket: "analytics_models", label: "AnalyticsModel"},
	{bucket: "data_assets", label: "DataAsset"},
	{bucket: "data_columns", label: "DataColumn"},
	{bucket: "query_executions", label: "QueryExecution"},
	{bucket: "dashboard_assets", label: "DashboardAsset"},
	{bucket: "data_quality_checks", label: "DataQualityCheck"},
	{bucket: "data_owners", label: "DataOwner"},
	{bucket: "data_contracts", label: "DataContract"},
	{bucket: "impl_blocks", label: "ImplBlock"},
	{bucket: "pagerduty_declarations", label: "PagerDutyDeclaration"},
	{bucket: "helm_value_definitions", label: "HelmValueDefinition"},
	{bucket: "helm_template_value_usages", label: "HelmTemplateValueUsage"},
	{bucket: "sql_migrations", label: "SqlMigration"},
	// Flux typed entities: appended at the end to mirror
	// content/shape/materialize_tables.go's frozen contentEntityBuckets order
	// (issue #5360 PR A). This list is the collector-side twin of that one:
	// entityBucketsFromParsed walks ONLY these buckets to emit content
	// entities, so a bucket registered in the parser and content/shape but
	// missing here silently drops every entity (no fact, no graph node). The
	// FluxHelmRelease/FluxHelmRepository buckets (issue #5483 C1) must stay in
	// lockstep with both the parser dispatch and contentEntityBuckets --
	// TestSnapshotEmitsFluxHelmReleaseAndRepositoryContentEntities guards that.
	{bucket: "flux_kustomizations", label: "FluxKustomization"},
	{bucket: "flux_git_repositories", label: "FluxGitRepository"},
	{bucket: "flux_oci_repositories", label: "FluxOCIRepository"},
	{bucket: "flux_buckets", label: "FluxBucket"},
	{bucket: "flux_helm_releases", label: "FluxHelmRelease"},
	{bucket: "flux_helm_repositories", label: "FluxHelmRepository"},
}
