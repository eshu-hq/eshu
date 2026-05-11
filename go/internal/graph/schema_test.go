package graph

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSchemaStatementsContainsExpectedConstraints(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()
	if len(stmts) == 0 {
		t.Fatal("SchemaStatements() returned empty slice")
	}

	expected := []string{
		"CREATE CONSTRAINT repository_id IF NOT EXISTS",
		"CREATE CONSTRAINT repository_path IF NOT EXISTS",
		"CREATE CONSTRAINT path IF NOT EXISTS FOR (f:File)",
		"CREATE CONSTRAINT directory_path IF NOT EXISTS",
		"CREATE CONSTRAINT evidence_artifact_id IF NOT EXISTS",
		"CREATE CONSTRAINT environment_name IF NOT EXISTS",
		"CREATE CONSTRAINT function_unique IF NOT EXISTS",
		"CREATE CONSTRAINT class_unique IF NOT EXISTS",
		"CREATE CONSTRAINT parameter_unique IF NOT EXISTS",
		"CREATE CONSTRAINT platform_id IF NOT EXISTS",
		"CREATE CONSTRAINT source_local_record_unique IF NOT EXISTS",
	}
	for _, want := range expected {
		found := false
		for _, stmt := range stmts {
			if strings.Contains(stmt, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SchemaStatements() missing constraint containing %q", want)
		}
	}
}

func TestSchemaStatementsForBackendPreservesNeo4jCompositeUniqueness(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", SchemaBackendNeo4j, err)
	}

	assertContainsStatement(t, stmts, "CREATE CONSTRAINT function_unique IF NOT EXISTS FOR (f:Function) REQUIRE (f.name, f.path, f.line_number) IS UNIQUE")
	assertNoStatementContains(t, stmts, "Function) REQUIRE (f.name, f.path, f.line_number) IS NODE KEY")
}

func TestSchemaStatementsForBackendSkipsNornicDBCompositeUniqueness(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", SchemaBackendNornicDB, err)
	}

	assertNoStatementContains(t, stmts, "Function) REQUIRE (f.name, f.path, f.line_number) IS UNIQUE")
	assertNoStatementContains(t, stmts, "Function) REQUIRE (f.name, f.path, f.line_number) IS NODE KEY")
	assertContainsStatement(t, stmts, "CREATE CONSTRAINT function_uid_unique IF NOT EXISTS FOR (n:Function) REQUIRE n.uid IS UNIQUE")
}

func TestSchemaStatementsForBackendAddsNornicDBMergeLookupIndexes(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", SchemaBackendNornicDB, err)
	}

	expected := []string{
		"CREATE INDEX nornicdb_repository_id_lookup IF NOT EXISTS FOR (r:Repository) ON (r.id)",
		"CREATE INDEX nornicdb_directory_path_lookup IF NOT EXISTS FOR (d:Directory) ON (d.path)",
		"CREATE INDEX nornicdb_file_path_lookup IF NOT EXISTS FOR (f:File) ON (f.path)",
		"CREATE INDEX nornicdb_workload_id_lookup IF NOT EXISTS FOR (w:Workload) ON (w.id)",
		"CREATE INDEX nornicdb_workload_instance_id_lookup IF NOT EXISTS FOR (i:WorkloadInstance) ON (i.id)",
		"CREATE INDEX nornicdb_platform_id_lookup IF NOT EXISTS FOR (p:Platform) ON (p.id)",
		"CREATE INDEX nornicdb_endpoint_id_lookup IF NOT EXISTS FOR (e:Endpoint) ON (e.id)",
		"CREATE INDEX nornicdb_evidence_artifact_id_lookup IF NOT EXISTS FOR (a:EvidenceArtifact) ON (a.id)",
		"CREATE INDEX nornicdb_environment_name_lookup IF NOT EXISTS FOR (e:Environment) ON (e.name)",
		// SourceLocalRecord and Parameter declare composite UNIQUE constraints
		// for MERGE performance on Neo4j (schema.go:113-119). NornicDB silently
		// drops composite constraints in nornicDBSchemaConstraint, leaving MERGE
		// on those labels without a backing index. Without a single-property
		// index, source-local projection (go/internal/storage/cypher/writer.go
		// MERGE on SourceLocalRecord) and the canonical HAS_PARAMETER edge
		// (go/internal/storage/cypher/canonical_node_cypher.go MERGE on
		// Parameter) fall through to a full label scan that the schema.go
		// comment calls out as "O(n²)".
		"CREATE INDEX nornicdb_source_local_record_scope_lookup IF NOT EXISTS FOR (n:SourceLocalRecord) ON (n.scope_id)",
		"CREATE INDEX nornicdb_parameter_path_lookup IF NOT EXISTS FOR (n:Parameter) ON (n.path)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

func TestSchemaStatementsForBackendAddsNornicDBUIDLookupIndexes(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", SchemaBackendNornicDB, err)
	}

	expected := []string{
		"CREATE INDEX nornicdb_function_uid_lookup IF NOT EXISTS FOR (n:Function) ON (n.uid)",
		"CREATE INDEX nornicdb_type_alias_uid_lookup IF NOT EXISTS FOR (n:TypeAlias) ON (n.uid)",
		"CREATE INDEX nornicdb_variable_uid_lookup IF NOT EXISTS FOR (n:Variable) ON (n.uid)",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

func TestSchemaStatementsForBackendKeepsUIDLookupIndexesNornicDBOnly(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNeo4j)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", SchemaBackendNeo4j, err)
	}

	assertNoStatementContains(t, stmts, "nornicdb_function_uid_lookup")
	assertNoStatementContains(t, stmts, "nornicdb_type_alias_uid_lookup")
	assertNoStatementContains(t, stmts, "nornicdb_workload_id_lookup")
	// Neo4j keeps the composite UNIQUE constraints for SourceLocalRecord and
	// Parameter, which already create backing indexes via the constraint
	// machinery, so the NornicDB-only single-property lookup indexes must not
	// be emitted on Neo4j or the duplicate-index warning fires.
	assertNoStatementContains(t, stmts, "nornicdb_source_local_record_scope_lookup")
	assertNoStatementContains(t, stmts, "nornicdb_parameter_path_lookup")
}

func TestNornicDBSchemaSkipsEveryCompositeUniqueConstraint(t *testing.T) {
	t.Parallel()

	stmts, err := SchemaStatementsForBackend(SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("SchemaStatementsForBackend(%q) error = %v, want nil", SchemaBackendNornicDB, err)
	}

	for _, constraint := range schemaConstraints {
		if !isCompositeUniqueConstraint(constraint) {
			continue
		}
		translated := nornicDBSchemaConstraint(constraint)
		if translated != "" {
			t.Fatalf("NornicDB constraint = %q, want skipped for %q", translated, constraint)
		}
		assertNoStatementContains(t, stmts, constraint)
	}
}

func TestSchemaStatementsForBackendRejectsUnknownBackend(t *testing.T) {
	t.Parallel()

	_, err := SchemaStatementsForBackend(SchemaBackend("falkordb"))
	if err == nil {
		t.Fatal("SchemaStatementsForBackend() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "unsupported schema backend") {
		t.Fatalf("error = %q, want unsupported schema backend", err.Error())
	}
}

func TestSchemaStatementsContainsPerformanceIndexes(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()

	expected := []string{
		"CREATE INDEX function_lang IF NOT EXISTS",
		"CREATE INDEX class_lang IF NOT EXISTS",
		"CREATE INDEX function_name IF NOT EXISTS",
		"CREATE INDEX class_name IF NOT EXISTS",
		"CREATE INDEX workload_name IF NOT EXISTS",
		"CREATE INDEX workload_instance_workload_id IF NOT EXISTS",
		"CREATE INDEX workload_instance_repo_id IF NOT EXISTS",
	}
	for _, want := range expected {
		found := false
		for _, stmt := range stmts {
			if strings.Contains(stmt, want) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("SchemaStatements() missing index containing %q", want)
		}
	}
}

func TestSchemaStatementsContainsUIDConstraints(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()

	expected := []string{
		"CREATE CONSTRAINT function_uid_unique IF NOT EXISTS FOR (n:Function) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT class_uid_unique IF NOT EXISTS FOR (n:Class) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT variable_uid_unique IF NOT EXISTS FOR (n:Variable) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT typedef_uid_unique IF NOT EXISTS FOR (n:Typedef) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT type_alias_uid_unique IF NOT EXISTS FOR (n:TypeAlias) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT component_uid_unique IF NOT EXISTS FOR (n:Component) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT module_uid_unique IF NOT EXISTS FOR (n:Module) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT impl_block_uid_unique IF NOT EXISTS FOR (n:ImplBlock) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT protocol_uid_unique IF NOT EXISTS FOR (n:Protocol) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT protocol_implementation_uid_unique IF NOT EXISTS FOR (n:ProtocolImplementation) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT k8s_resource_uid_unique IF NOT EXISTS FOR (n:K8sResource) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT terraform_resource_uid_unique IF NOT EXISTS FOR (n:TerraformResource) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT terraform_module_uid_unique IF NOT EXISTS FOR (n:TerraformModule) REQUIRE n.uid IS UNIQUE",
		"CREATE CONSTRAINT terraform_output_uid_unique IF NOT EXISTS FOR (n:TerraformOutput) REQUIRE n.uid IS UNIQUE",
	}
	for _, want := range expected {
		assertContainsStatement(t, stmts, want)
	}
}

func TestSchemaStatementsContainsFulltextIndexes(t *testing.T) {
	t.Parallel()

	stmts := SchemaStatements()

	found := false
	for _, stmt := range stmts {
		if strings.Contains(stmt, "code_search_index") {
			found = true
			break
		}
	}
	if !found {
		t.Error("SchemaStatements() missing code_search_index fulltext statement")
	}

	found = false
	for _, stmt := range stmts {
		if strings.Contains(stmt, "infra_search_index") {
			found = true
			break
		}
	}
	if !found {
		t.Error("SchemaStatements() missing infra_search_index fulltext statement")
	}
}

func TestEnsureSchemaExecutesAllStatements(t *testing.T) {
	t.Parallel()

	executor := &schemaRecordingExecutor{}
	err := EnsureSchema(context.Background(), executor, nil)
	if err != nil {
		t.Fatalf("EnsureSchema() error = %v, want nil", err)
	}

	expectedCount := len(SchemaStatements())
	if got := len(executor.calls); got != expectedCount {
		t.Fatalf("executor calls = %d, want %d", got, expectedCount)
	}
}

func TestEnsureSchemaWithBackendExecutesNornicDBStatements(t *testing.T) {
	t.Parallel()

	executor := &schemaRecordingExecutor{}
	err := EnsureSchemaWithBackend(context.Background(), executor, nil, SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("EnsureSchemaWithBackend() error = %v, want nil", err)
	}

	assertContainsExecutedStatement(t, executor.calls, "CREATE CONSTRAINT function_uid_unique IF NOT EXISTS FOR (n:Function) REQUIRE n.uid IS UNIQUE")
	assertContainsExecutedStatement(t, executor.calls, "CREATE CONSTRAINT module_uid_unique IF NOT EXISTS FOR (n:Module) REQUIRE n.uid IS UNIQUE")
	assertContainsExecutedStatement(t, executor.calls, "CREATE CONSTRAINT impl_block_uid_unique IF NOT EXISTS FOR (n:ImplBlock) REQUIRE n.uid IS UNIQUE")
	assertContainsExecutedStatement(t, executor.calls, "CREATE CONSTRAINT protocol_uid_unique IF NOT EXISTS FOR (n:Protocol) REQUIRE n.uid IS UNIQUE")
	assertContainsExecutedStatement(t, executor.calls, "CREATE CONSTRAINT protocol_implementation_uid_unique IF NOT EXISTS FOR (n:ProtocolImplementation) REQUIRE n.uid IS UNIQUE")
	assertContainsExecutedStatement(t, executor.calls, "CREATE CONSTRAINT k8s_resource_uid_unique IF NOT EXISTS FOR (n:K8sResource) REQUIRE n.uid IS UNIQUE")
	assertNoExecutedStatementContains(t, executor.calls, "Function) REQUIRE (f.name, f.path, f.line_number) IS UNIQUE")
	assertNoExecutedStatementContains(t, executor.calls, "Function) REQUIRE (f.name, f.path, f.line_number) IS NODE KEY")
}

func TestEnsureSchemaRequiresExecutor(t *testing.T) {
	t.Parallel()

	err := EnsureSchema(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("EnsureSchema() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "executor is required") {
		t.Fatalf("error = %q, want 'executor is required'", err.Error())
	}
}

func TestEnsureSchemaFulltextFallback(t *testing.T) {
	t.Parallel()

	// Executor that fails on procedure-based fulltext calls but succeeds on
	// CREATE FULLTEXT INDEX fallback.
	executor := &schemaRecordingExecutor{
		failOn: func(cypher string) bool {
			return strings.Contains(cypher, "db.index.fulltext.createNodeIndex")
		},
	}

	err := EnsureSchema(context.Background(), executor, nil)
	if err != nil {
		t.Fatalf("EnsureSchema() error = %v, want nil", err)
	}

	// Verify fallback statements were executed.
	var fallbackCount int
	for _, call := range executor.calls {
		if strings.Contains(call.Cypher, "CREATE FULLTEXT INDEX") {
			fallbackCount++
		}
	}
	if fallbackCount != len(schemaFulltextIndexes) {
		t.Fatalf("fallback fulltext calls = %d, want %d", fallbackCount, len(schemaFulltextIndexes))
	}
}

func TestEnsureSchemaWithBackendSkipsFulltextFallbackForNornicDB(t *testing.T) {
	t.Parallel()

	executor := &schemaRecordingExecutor{
		failOn: func(cypher string) bool {
			return strings.Contains(cypher, "db.index.fulltext.createNodeIndex")
		},
	}

	err := EnsureSchemaWithBackend(context.Background(), executor, nil, SchemaBackendNornicDB)
	if err != nil {
		t.Fatalf("EnsureSchemaWithBackend() error = %v, want nil", err)
	}

	for _, call := range executor.calls {
		if strings.Contains(call.Cypher, "CREATE FULLTEXT INDEX") {
			t.Fatalf("NornicDB schema attempted unsupported fulltext fallback %q", call.Cypher)
		}
	}
}

func TestEnsureSchemaContinuesOnFailure(t *testing.T) {
	t.Parallel()

	// Executor that fails on every call.
	executor := &schemaRecordingExecutor{
		failOn: func(_ string) bool { return true },
	}

	err := EnsureSchema(context.Background(), executor, nil)
	if err != nil {
		t.Fatalf("EnsureSchema() error = %v, want nil (warnings logged, not returned)", err)
	}

	// Should still attempt all statements.
	if len(executor.calls) == 0 {
		t.Fatal("executor should have been called at least once")
	}
}

func TestLabelToSnake(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{"Function", "function"},
		{"Class", "class"},
		{"CrossplaneXRD", "crossplane_x_r_d"},
		{"TerraformResource", "terraform_resource"},
		{"K8sResource", "k8s_resource"},
	}

	for _, tt := range tests {
		if got := labelToSnake(tt.input); got != tt.want {
			t.Errorf("labelToSnake(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

type schemaRecordingExecutor struct {
	calls  []CypherStatement
	failOn func(string) bool
}

func (r *schemaRecordingExecutor) ExecuteCypher(_ context.Context, stmt CypherStatement) error {
	r.calls = append(r.calls, stmt)
	if r.failOn != nil && r.failOn(stmt.Cypher) {
		return errors.New("simulated schema error")
	}
	return nil
}

func assertContainsStatement(t *testing.T, stmts []string, want string) {
	t.Helper()

	for _, stmt := range stmts {
		if stmt == want {
			return
		}
	}
	t.Fatalf("statements missing %q", want)
}

func assertNoStatementContains(t *testing.T, stmts []string, disallowed string) {
	t.Helper()

	for _, stmt := range stmts {
		if strings.Contains(stmt, disallowed) {
			t.Fatalf("statement %q contains disallowed fragment %q", stmt, disallowed)
		}
	}
}

func assertContainsExecutedStatement(t *testing.T, calls []CypherStatement, wantFragment string) {
	t.Helper()

	for _, call := range calls {
		if strings.Contains(call.Cypher, wantFragment) {
			return
		}
	}
	t.Fatalf("executed statements missing fragment %q", wantFragment)
}

func assertNoExecutedStatementContains(t *testing.T, calls []CypherStatement, disallowed string) {
	t.Helper()

	for _, call := range calls {
		if strings.Contains(call.Cypher, disallowed) {
			t.Fatalf("executed statement %q contains disallowed fragment %q", call.Cypher, disallowed)
		}
	}
}
