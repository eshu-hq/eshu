package cypher

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterFileScopedContainmentKeepsNormalOneRowBatchGrouped(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil).
		WithEntityContainmentInEntityUpsert().
		WithEntityLabelBatchSize("K8sResource", 1)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{
				EntityID:     "k8s-1",
				Label:        "K8sResource",
				EntityName:   "route",
				FilePath:     "/repos/my-repo/charts/routes.yaml",
				RelativePath: "charts/routes.yaml",
				StartLine:    1,
				EndLine:      1,
				Language:     "yaml",
				RepoID:       "repo-1",
			},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if got, want := len(stmts), 1; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d", got, want)
	}
	stmt := stmts[0]
	if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("entity cypher = %q, want one-row batch to keep UNWIND hot-path shape", stmt.Cypher)
	}
	if got := stmt.Parameters[StatementMetadataPhaseGroupModeKey]; got != nil {
		t.Fatalf("phase group mode = %#v, want grouped one-row batch without execute-only mode", got)
	}
	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", stmt.Parameters["rows"])
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if got, want := rows[0]["entity_id"], "k8s-1"; got != want {
		t.Fatalf("row entity_id = %#v, want %#v", got, want)
	}
}

func TestCanonicalNodeWriterFileScopedContainmentOnlySingletonsFallbackRows(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil).
		WithEntityContainmentInEntityUpsert().
		WithEntityBatchSize(10)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{
				EntityID:     "fn-1",
				Label:        "Function",
				EntityName:   "normalOne",
				FilePath:     "/repos/my-repo/src/routes.go",
				RelativePath: "src/routes.go",
				StartLine:    1,
				EndLine:      2,
				Language:     "go",
				RepoID:       "repo-1",
			},
			{
				EntityID:     "fn-shortest",
				Label:        "Function",
				EntityName:   "TestHandleCallChainReturnsShortestPath",
				FilePath:     "/repos/my-repo/src/routes.go",
				RelativePath: "src/routes.go",
				StartLine:    3,
				EndLine:      4,
				Language:     "go",
				RepoID:       "repo-1",
			},
			{
				EntityID:     "fn-2",
				Label:        "Function",
				EntityName:   "normalTwo",
				FilePath:     "/repos/my-repo/src/routes.go",
				RelativePath: "src/routes.go",
				StartLine:    5,
				EndLine:      6,
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if got, want := len(stmts), 3; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d", got, want)
	}

	for _, idx := range []int{0, 2} {
		stmt := stmts[idx]
		if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
			t.Fatalf("statement %d cypher = %q, want grouped one-row UNWIND shape", idx, stmt.Cypher)
		}
		if got := stmt.Parameters[StatementMetadataPhaseGroupModeKey]; got != nil {
			t.Fatalf("statement %d phase group mode = %#v, want absent", idx, got)
		}
		rows, ok := stmt.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("statement %d rows type = %T, want []map[string]any", idx, stmt.Parameters["rows"])
		}
		if got, want := len(rows), 1; got != want {
			t.Fatalf("statement %d rows = %d, want %d", idx, got, want)
		}
	}

	fallback := stmts[1]
	if strings.Contains(fallback.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("fallback cypher = %q, want singleton shape without UNWIND", fallback.Cypher)
	}
	if got, want := fallback.Parameters[StatementMetadataPhaseGroupModeKey], PhaseGroupModeExecuteOnly; got != want {
		t.Fatalf("fallback phase group mode = %#v, want %#v", got, want)
	}
	if got, want := fallback.Parameters["entity_id"], "fn-shortest"; got != want {
		t.Fatalf("fallback entity_id = %#v, want %#v", got, want)
	}
}

func TestCanonicalNodeWriterFileScopedContainmentBatchesTerraformVariableCurlyBraceMetadata(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil).
		WithEntityContainmentInEntityUpsert().
		WithEntityBatchSize(10)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{
				EntityID:     "tf-var-1",
				Label:        "TerraformVariable",
				EntityName:   "environment_vars",
				FilePath:     "/repos/my-repo/env/common.tf",
				RelativePath: "env/common.tf",
				StartLine:    12,
				EndLine:      16,
				Language:     "hcl",
				RepoID:       "repo-1",
				Metadata: map[string]any{
					"default":  "{}",
					"var_type": "any",
				},
			},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if got, want := len(stmts), 1; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d", got, want)
	}
	stmt := stmts[0]
	if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("entity cypher = %q, want batched shape for curly brace metadata", stmt.Cypher)
	}
	if got := stmt.Parameters[StatementMetadataPhaseGroupModeKey]; got != nil {
		t.Fatalf("phase group mode = %#v, want grouped row", got)
	}
	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", stmt.Parameters["rows"])
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if got, want := rows[0]["entity_id"], "tf-var-1"; got != want {
		t.Fatalf("row entity_id = %#v, want %#v", got, want)
	}
}

func TestCanonicalNodeWriterFileScopedContainmentBatchesTerraformVariableDescriptionBraces(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil).
		WithEntityContainmentInEntityUpsert().
		WithEntityBatchSize(10)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{
				EntityID:     "tf-var-1",
				Label:        "TerraformVariable",
				EntityName:   "passwords_require_symbols",
				FilePath:     "/repos/my-repo/modules/cognito/variables.tf",
				RelativePath: "modules/cognito/variables.tf",
				StartLine:    54,
				EndLine:      58,
				Language:     "hcl",
				RepoID:       "repo-1",
				Metadata: map[string]any{
					"default":     "cty.True",
					"var_type":    "bool",
					"description": `symbols from the following set: ^$*.[]{}()?"!@#%&/\\,><':;|_~`,
				},
			},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if got, want := len(stmts), 1; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d", got, want)
	}
	stmt := stmts[0]
	if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("entity cypher = %q, want batched shape for TerraformVariable description braces", stmt.Cypher)
	}
	if got := stmt.Parameters[StatementMetadataPhaseGroupModeKey]; got != nil {
		t.Fatalf("phase group mode = %#v, want grouped row", got)
	}
	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", stmt.Parameters["rows"])
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	props, ok := rows[0]["props"].(map[string]any)
	if !ok {
		t.Fatalf("props type = %T, want map[string]any", rows[0]["props"])
	}
	if got, want := props["description"], `symbols from the following set: ^$*.[]{}()?"!@#%&/\\,><':;|_~`; got != want {
		t.Fatalf("description = %#v, want %#v", got, want)
	}
}

func TestCanonicalNodeWriterFileScopedContainmentKeepsNonDefaultCurlyMetadataBatched(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil).
		WithEntityContainmentInEntityUpsert().
		WithEntityBatchSize(10)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{
				EntityID:     "tf-local-1",
				Label:        "TerraformLocal",
				EntityName:   "tags",
				FilePath:     "/repos/my-repo/env/common.tf",
				RelativePath: "env/common.tf",
				StartLine:    20,
				EndLine:      24,
				Language:     "hcl",
				RepoID:       "repo-1",
				Metadata: map[string]any{
					"value": "${var.environment}",
				},
			},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if got, want := len(stmts), 1; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d", got, want)
	}
	stmt := stmts[0]
	if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("entity cypher = %q, want grouped UNWIND shape", stmt.Cypher)
	}
	if got := stmt.Parameters[StatementMetadataPhaseGroupModeKey]; got != nil {
		t.Fatalf("phase group mode = %#v, want grouped row", got)
	}
}
