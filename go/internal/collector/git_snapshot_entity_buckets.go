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
	// Issue #5531: these five buckets were registered in content/shape's
	// contentEntityBuckets (and, for the CloudFormation three and
	// TerraformBlock, are already exercised by content/shape's own
	// materialize_test.go and by query/entity_content_iac_fallback_test.go's
	// content-fallback path) but were missing from this twin, so the HCL and
	// JSON parsers' real terraform_blocks/cloudformation_conditions/
	// cloudformation_cross_stack_imports/cloudformation_cross_stack_exports
	// output was silently dropped before a content_entity fact was ever built
	// -- no fact, no content row, no error, no failing unit test, exactly the
	// #5483 C1 defect class. protocol_implementations has no parser producing
	// that bucket key today (ProtocolImplementation is reached instead via
	// entityLabelForBucket's module_kind rewrite of the "modules" bucket); it
	// is registered here only so this twin stays an exact mirror of
	// content/shape's table, per the sync gate in
	// go/internal/content/shape/bucket_sync_gate_test.go (CI gate
	// content-entity-bucket-sync). Four of the five (terraform_blocks and the
	// three CloudFormation extended labels) are not wired into the projector's
	// graph-write path (entityTypeLabelMap in go/internal/projector/canonical.go);
	// ProtocolImplementation IS already registered there
	// ("protocol_implementation" -> "ProtocolImplementation") and
	// EntityTypeLabel also recognizes the PascalCase value directly, so that
	// entry is inert only because no parser produces the
	// protocol_implementations bucket key today, not because the projector
	// cannot name the label. See canonical.go's entityTypeLabelMap comment and
	// the gate's knownMissingProjectorLabels ledger for the four labels still
	// missing, and issue #5954 tracking full graph support (schema constraint,
	// retract-label coverage, golden-corpus proof) for those.
	{bucket: "terraform_blocks", label: "TerraformBlock"},
	{bucket: "protocol_implementations", label: "ProtocolImplementation"},
	{bucket: "cloudformation_conditions", label: "CloudFormationCondition"},
	{bucket: "cloudformation_cross_stack_imports", label: "CloudFormationImport"},
	{bucket: "cloudformation_cross_stack_exports", label: "CloudFormationExport"},
}
