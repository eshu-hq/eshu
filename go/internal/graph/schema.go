// Package graph schema.go provides Neo4j schema initialization.
//
// EnsureSchema creates all node constraints, performance indexes, and
// full-text indexes required by Eshu. The constraint
// and index definitions are the checked-in Go-owned schema contract for the
// rewritten platform.
package graph

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// SchemaBackend identifies the graph database dialect used for schema DDL.
type SchemaBackend string

const (
	// SchemaBackendNeo4j preserves Eshu's shared production schema contract.
	SchemaBackendNeo4j SchemaBackend = "neo4j"
	// SchemaBackendNornicDB applies the narrow compatibility dialect proven by
	// the opt-in NornicDB syntax gate.
	SchemaBackendNornicDB SchemaBackend = "nornicdb"
)

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
	"CREATE CONSTRAINT xrd_unique IF NOT EXISTS FOR (x:CrossplaneXRD) REQUIRE (x.name, x.path, x.line_number) IS UNIQUE",
	"CREATE CONSTRAINT composition_unique IF NOT EXISTS FOR (c:CrossplaneComposition) REQUIRE (c.name, c.path, c.line_number) IS UNIQUE",
	"CREATE CONSTRAINT claim_unique IF NOT EXISTS FOR (cl:CrossplaneClaim) REQUIRE (cl.name, cl.kind, cl.path, cl.line_number) IS UNIQUE",
	"CREATE CONSTRAINT kustomize_unique IF NOT EXISTS FOR (ko:KustomizeOverlay) REQUIRE ko.path IS UNIQUE",
	"CREATE CONSTRAINT helm_chart_unique IF NOT EXISTS FOR (h:HelmChart) REQUIRE (h.name, h.path) IS UNIQUE",
	"CREATE CONSTRAINT helm_values_unique IF NOT EXISTS FOR (hv:HelmValues) REQUIRE hv.path IS UNIQUE",

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
	"Class",
	"CloudFormationOutput",
	"CloudFormationParameter",
	"CloudFormationResource",
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
	"File",
	"Function",
	"HelmChart",
	"HelmValues",
	"ImplBlock",
	"Interface",
	"K8sResource",
	"KustomizeOverlay",
	"Macro",
	"Module",
	"Package",
	"PackageDependency",
	"PackageRegistryPackageDependency",
	"PackageRegistryPackage",
	"PackageRegistryPackageVersion",
	"PackageVersion",
	"Property",
	"Protocol",
	"ProtocolImplementation",
	"QueryExecution",
	"Record",
	"SqlColumn",
	"SqlFunction",
	"SqlIndex",
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
	"CREATE INDEX class_lang IF NOT EXISTS FOR (c:Class) ON (c.lang)",
	"CREATE INDEX annotation_lang IF NOT EXISTS FOR (a:Annotation) ON (a.lang)",
	"CREATE INDEX k8s_kind IF NOT EXISTS FOR (k:K8sResource) ON (k.kind)",
	"CREATE INDEX k8s_namespace IF NOT EXISTS FOR (k:K8sResource) ON (k.namespace)",
	"CREATE INDEX tf_resource_type IF NOT EXISTS FOR (r:TerraformResource) ON (r.resource_type)",
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
	"CREATE INDEX package_version_package_id IF NOT EXISTS FOR (v:PackageVersion) ON (v.package_id)",
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
	"CREATE INDEX nornicdb_directory_path_lookup IF NOT EXISTS FOR (d:Directory) ON (d.path)",
	"CREATE INDEX nornicdb_file_path_lookup IF NOT EXISTS FOR (f:File) ON (f.path)",
	"CREATE INDEX nornicdb_workload_id_lookup IF NOT EXISTS FOR (w:Workload) ON (w.id)",
	"CREATE INDEX nornicdb_workload_instance_id_lookup IF NOT EXISTS FOR (i:WorkloadInstance) ON (i.id)",
	"CREATE INDEX nornicdb_platform_id_lookup IF NOT EXISTS FOR (p:Platform) ON (p.id)",
	"CREATE INDEX nornicdb_endpoint_id_lookup IF NOT EXISTS FOR (e:Endpoint) ON (e.id)",
	"CREATE INDEX nornicdb_evidence_artifact_id_lookup IF NOT EXISTS FOR (a:EvidenceArtifact) ON (a.id)",
	"CREATE INDEX nornicdb_environment_name_lookup IF NOT EXISTS FOR (e:Environment) ON (e.name)",
	"CREATE INDEX nornicdb_source_local_record_scope_lookup IF NOT EXISTS FOR (n:SourceLocalRecord) ON (n.scope_id)",
	"CREATE INDEX nornicdb_parameter_path_lookup IF NOT EXISTS FOR (n:Parameter) ON (n.path)",
}

func nornicDBUIDLookupIndexes() []string {
	indexes := make([]string, 0, len(uidConstraintLabels))
	for _, label := range uidConstraintLabels {
		indexes = append(indexes, fmt.Sprintf(
			"CREATE INDEX nornicdb_%s_uid_lookup IF NOT EXISTS FOR (n:%s) ON (n.uid)",
			labelToSnake(label), label,
		))
	}
	return indexes
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
			"'ArgoCDApplicationSet', 'CrossplaneXRD', 'CrossplaneComposition', " +
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
			"ArgoCDApplicationSet|CrossplaneXRD|CrossplaneComposition|" +
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

// fulltextIndex pairs a primary procedure-based fulltext statement with
// its modern CREATE FULLTEXT INDEX fallback.
type fulltextIndex struct {
	primary  string
	fallback string
}

// SchemaStatements returns the complete ordered list of Cypher statements
// that EnsureSchema would execute. Useful for inspection and testing.
func SchemaStatements() []string {
	// Keep the legacy no-argument helper on Neo4j. Runtime bootstraps that
	// need the configured default call SchemaStatementsForBackend instead.
	stmts, _ := SchemaStatementsForBackend(SchemaBackendNeo4j)
	return stmts
}

// SchemaStatementsForBackend returns the ordered Cypher schema statements for
// a specific graph backend without executing them.
func SchemaStatementsForBackend(backend SchemaBackend) ([]string, error) {
	dialect, err := schemaDialectForBackend(backend)
	if err != nil {
		return nil, err
	}

	stmts := make([]string, 0,
		len(schemaConstraints)+
			len(uidConstraintLabels)+
			len(schemaPerformanceIndexes)+
			len(schemaFulltextIndexes))
	for _, cypher := range schemaConstraints {
		if cypher = dialect.constraint(cypher); cypher != "" {
			stmts = append(stmts, cypher)
		}
	}
	stmts = append(stmts, schemaPerformanceIndexes...)
	if dialect.includeMergeLookupIndexes {
		stmts = append(stmts, nornicDBMergeLookupIndexes...)
		stmts = append(stmts, nornicDBUIDLookupIndexes()...)
	}
	for _, label := range uidConstraintLabels {
		stmts = append(stmts, fmt.Sprintf(
			"CREATE CONSTRAINT %s_uid_unique IF NOT EXISTS FOR (n:%s) REQUIRE n.uid IS UNIQUE",
			labelToSnake(label), label,
		))
	}
	for _, ft := range schemaFulltextIndexes {
		stmts = append(stmts, ft.primary)
	}
	return stmts, nil
}

// EnsureSchema creates all constraints and indexes required by the platform
// context graph. Each statement is executed individually; failures are logged
// as warnings but do not abort the remaining statements. Full-text index
// creation automatically falls back to modern syntax when the procedure-based
// API is unavailable.
func EnsureSchema(ctx context.Context, executor CypherExecutor, logger *slog.Logger) error {
	return EnsureSchemaWithBackend(ctx, executor, logger, SchemaBackendNeo4j)
}

// EnsureSchemaWithBackend creates all constraints and indexes required by the
// selected graph backend. NornicDB uses only syntax translations proven by
// compatibility tests.
func EnsureSchemaWithBackend(ctx context.Context, executor CypherExecutor, logger *slog.Logger, backend SchemaBackend) error {
	if executor == nil {
		return fmt.Errorf("schema executor is required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	dialect, err := schemaDialectForBackend(backend)
	if err != nil {
		return err
	}

	var failed int

	// Constraints
	for _, cypher := range schemaConstraints {
		cypher = dialect.constraint(cypher)
		if cypher == "" {
			continue
		}
		if err := executeSchemaStatement(ctx, executor, cypher); err != nil {
			failed++
			logger.Warn("schema statement warning",
				"error", err,
				"cypher", cypher,
				"graph_backend", dialect.backend)
		}
	}

	// Performance indexes
	for _, cypher := range schemaPerformanceIndexes {
		if err := executeSchemaStatement(ctx, executor, cypher); err != nil {
			failed++
			logger.Warn("schema statement warning",
				"error", err,
				"cypher", cypher,
				"graph_backend", dialect.backend)
		}
	}
	if dialect.includeMergeLookupIndexes {
		for _, cypher := range nornicDBMergeLookupIndexes {
			if err := executeSchemaStatement(ctx, executor, cypher); err != nil {
				failed++
				logger.Warn("schema statement warning",
					"error", err,
					"cypher", cypher,
					"graph_backend", dialect.backend)
			}
		}
		for _, cypher := range nornicDBUIDLookupIndexes() {
			if err := executeSchemaStatement(ctx, executor, cypher); err != nil {
				failed++
				logger.Warn("schema statement warning",
					"error", err,
					"cypher", cypher,
					"graph_backend", dialect.backend)
			}
		}
	}

	// UID uniqueness constraints for content entity labels
	for _, label := range uidConstraintLabels {
		cypher := fmt.Sprintf(
			"CREATE CONSTRAINT %s_uid_unique IF NOT EXISTS FOR (n:%s) REQUIRE n.uid IS UNIQUE",
			labelToSnake(label), label,
		)
		if err := executeSchemaStatement(ctx, executor, cypher); err != nil {
			failed++
			logger.Warn("schema statement warning",
				"error", err,
				"cypher", cypher,
				"graph_backend", dialect.backend)
		}
	}

	// Full-text indexes with fallback
	for _, ft := range schemaFulltextIndexes {
		if err := executeSchemaStatement(ctx, executor, ft.primary); err != nil {
			if dialect.skipFulltextFallback {
				failed++
				logger.Warn("fulltext index warning",
					"primary_error", err,
					"primary", ft.primary,
					"graph_backend", dialect.backend)
				continue
			}
			if err2 := executeSchemaStatement(ctx, executor, ft.fallback); err2 != nil {
				failed++
				logger.Warn("fulltext index warning",
					"primary_error", err, "fallback_error", err2,
					"primary", ft.primary, "fallback", ft.fallback,
					"graph_backend", dialect.backend)
			}
		}
	}

	if failed > 0 {
		logger.Warn("schema creation completed with warnings", "failed", failed, "graph_backend", dialect.backend)
	} else {
		logger.Info("database schema verified/created successfully", "graph_backend", dialect.backend)
	}

	return nil
}

type schemaDialect struct {
	backend                   SchemaBackend
	constraint                func(string) string
	skipFulltextFallback      bool
	includeMergeLookupIndexes bool
}

func schemaDialectForBackend(backend SchemaBackend) (schemaDialect, error) {
	normalized, err := normalizeSchemaBackend(backend)
	if err != nil {
		return schemaDialect{}, err
	}
	switch normalized {
	case SchemaBackendNeo4j:
		return schemaDialect{backend: normalized, constraint: neo4jSchemaConstraint}, nil
	case SchemaBackendNornicDB:
		return schemaDialect{
			backend:                   normalized,
			constraint:                nornicDBSchemaConstraint,
			skipFulltextFallback:      true,
			includeMergeLookupIndexes: true,
		}, nil
	}
	return schemaDialect{}, fmt.Errorf("unsupported schema backend %q", backend)
}

func normalizeSchemaBackend(backend SchemaBackend) (SchemaBackend, error) {
	switch backend {
	case "", SchemaBackendNeo4j:
		return SchemaBackendNeo4j, nil
	case SchemaBackendNornicDB:
		return SchemaBackendNornicDB, nil
	default:
		return "", fmt.Errorf("unsupported schema backend %q", backend)
	}
}

func neo4jSchemaConstraint(cypher string) string {
	return cypher
}

func nornicDBSchemaConstraint(cypher string) string {
	if isCompositeUniqueConstraint(cypher) {
		// NornicDB's current parser accepts NODE KEY but rejects Neo4j's
		// composite IS UNIQUE form. Do not translate these constraints to
		// NODE KEY: node keys require every participating property to be
		// present, while several Eshu semantic labels are intentionally sparse.
		// Canonical writes use separate uid uniqueness constraints instead.
		return ""
	}
	return cypher
}

func isCompositeUniqueConstraint(cypher string) bool {
	return strings.Contains(cypher, " REQUIRE (") && strings.Contains(cypher, ") IS UNIQUE")
}

// executeSchemaStatement runs one DDL statement through the executor.
func executeSchemaStatement(ctx context.Context, executor CypherExecutor, cypher string) error {
	return executor.ExecuteCypher(ctx, CypherStatement{
		Cypher:     cypher,
		Parameters: map[string]any{},
	})
}

// labelToSnake converts a PascalCase label to lower_snake_case for use in
// constraint names (e.g., "CrossplaneXRD" -> "crossplane_x_r_d").
func labelToSnake(label string) string {
	result := make([]byte, 0, len(label)+4)
	for i, b := range []byte(label) {
		if b >= 'A' && b <= 'Z' {
			lower := b + ('a' - 'A')
			if i > 0 {
				result = append(result, '_')
			}
			result = append(result, lower)
		} else {
			result = append(result, b)
		}
	}
	return string(result)
}
