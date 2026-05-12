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

// TestCanonicalNodeWriterFileScopedContainment_TriggerSubstringsStayInBatch
// is the inverted form of the prior "OnlySingletonsFallbackRows" test. The
// canonicalEntityRowNeedsSingletonFallback security check at
// canonical_node_writer_entities_singleton.go was originally implemented to
// route rows whose values contained Cypher keyword substrings ("shortestpath",
// "allshortestpaths", "remove ") into per-row parameterized singletons,
// avoiding a hypothetical parser-confusion risk. The K8s native CPU profile
// captured for ADR row 1809 attributed 17,000 such singletons (~26% of
// NornicDB canonical_write CPU) to Kubernetes Function entities whose
// docstrings contain the English word "remove" followed by a space; the
// wall-clock measurement of returning those rows to the batched path is in
// ADR row 1815.
//
// The NornicDB-side test
// `TestUnwindMergeChainBatch_EshuSingletonFallbackUnnecessary` on branch
// perf/k8s-tier2-canonical-write-followups proves that parameterized UNWIND-
// batched cypher handles all three trigger substrings safely: parameters are
// bound separately from cypher text per Bolt protocol, so parameter values
// containing Cypher keywords never become cypher syntax. The security check
// is therefore obsolete.
//
// This test asserts the new behavior: all rows including those whose
// EntityName contains a former trigger substring remain in a single batched
// UNWIND statement.
func TestCanonicalNodeWriterFileScopedContainment_TriggerSubstringsStayInBatch(t *testing.T) {
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
	if got, want := len(stmts), 1; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d "+
			"(all rows including trigger substrings must stay in one UNWIND-batched statement)",
			got, want)
	}
	stmt := stmts[0]
	if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("statement cypher = %q, want batched UNWIND shape — singleton fallback removed", stmt.Cypher)
	}
	if got := stmt.Parameters[StatementMetadataPhaseGroupModeKey]; got != nil {
		t.Fatalf("phase group mode = %#v, want grouped batch without execute-only mode", got)
	}
	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", stmt.Parameters["rows"])
	}
	if got, want := len(rows), 3; got != want {
		t.Fatalf("rows = %d, want %d (all 3 entities batched together)", got, want)
	}
	seen := make(map[string]bool, len(rows))
	for _, row := range rows {
		if id, ok := row["entity_id"].(string); ok {
			seen[id] = true
		}
	}
	for _, want := range []string{"fn-1", "fn-shortest", "fn-2"} {
		if !seen[want] {
			t.Fatalf("entity_id %q missing from batched rows; the trigger substring must no longer cause fallback", want)
		}
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
