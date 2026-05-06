package cypher

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterSeparatesEntityUpsertsFromContainmentEdges(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-1",
			Name:   "my-repo",
			Path:   "/repos/my-repo",
		},
		Files: []projector.FileRow{
			{
				Path:         "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				Name:         "main.go",
				Language:     "go",
				RepoID:       "repo-1",
				DirPath:      "/repos/my-repo/src",
			},
		},
		Entities: []projector.EntityRow{
			{
				EntityID:     "entity-1",
				Label:        "Function",
				EntityName:   "handleRelationships",
				FilePath:     "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				StartLine:    12,
				EndLine:      34,
				Language:     "go",
				RepoID:       "repo-1",
			},
			{
				EntityID:     "entity-2",
				Label:        "Function",
				EntityName:   "transitiveRelationshipsGraphResponse",
				FilePath:     "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				StartLine:    40,
				EndLine:      68,
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
	}

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var entityCalls []Statement
	var containmentCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			strings.Contains(call.Cypher, "MERGE (n:Function {uid: row.entity_id})") {
			entityCalls = append(entityCalls, call)
		}
		if call.Operation == OperationCanonicalUpsert &&
			call.Parameters[StatementMetadataPhaseKey] == "entity_containment" {
			containmentCalls = append(containmentCalls, call)
		}
	}

	if got, want := len(entityCalls), 1; got != want {
		t.Fatalf("entity batch count = %d, want %d", got, want)
	}
	if got, want := len(containmentCalls), 1; got != want {
		t.Fatalf("entity containment batch count = %d, want %d", got, want)
	}

	stmt := entityCalls[0]
	if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("entity upsert cypher = %q, want batched UNWIND shape", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "MATCH (f:File") {
		t.Fatalf("entity upsert cypher = %q, want node-only upsert without file MATCH", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "SET n += row.props") {
		t.Fatalf("entity upsert cypher = %q, want row.props merge", stmt.Cypher)
	}
	if strings.Contains(stmt.Cypher, "MERGE (f)-[:CONTAINS]->(n)") {
		t.Fatalf("entity upsert cypher = %q, want containment in separate phase", stmt.Cypher)
	}

	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", stmt.Parameters["rows"])
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("rows count = %d, want %d", got, want)
	}
	if _, ok := stmt.Parameters["file_path"]; ok {
		t.Fatalf("entity statement unexpectedly carries statement-level file_path: %#v", stmt.Parameters)
	}
	if got := stmt.Parameters[StatementMetadataScopeIDKey]; got != "scope-1" {
		t.Fatalf("entity statement scope metadata = %#v, want scope-1", got)
	}
	if got := stmt.Parameters[StatementMetadataGenerationIDKey]; got != "gen-1" {
		t.Fatalf("entity statement generation metadata = %#v, want gen-1", got)
	}
	for _, row := range rows {
		if _, ok := row["file_path"]; ok {
			t.Fatalf("entity row unexpectedly contains file_path: %#v", row)
		}
		props, ok := row["props"].(map[string]any)
		if !ok {
			t.Fatalf("row[props] type = %T, want map[string]any", row["props"])
		}
		if _, ok := props["name"]; !ok {
			t.Fatalf("row props = %#v, want projected entity properties", props)
		}
	}

	containment := containmentCalls[0]
	if !strings.Contains(containment.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("entity containment cypher = %q, want batched UNWIND shape", containment.Cypher)
	}
	if !strings.Contains(containment.Cypher, "MATCH (f:File {path: $file_path})") {
		t.Fatalf("entity containment cypher = %q, want file-scoped MATCH", containment.Cypher)
	}
	if !strings.Contains(containment.Cypher, "MATCH (n:Function {uid: row.entity_id})") {
		t.Fatalf("entity containment cypher = %q, want entity MATCH by uid", containment.Cypher)
	}
	if got := containment.Parameters["file_path"]; got != "/repos/my-repo/src/main.go" {
		t.Fatalf("containment file_path = %#v, want /repos/my-repo/src/main.go", got)
	}
	if got := containment.Parameters[StatementMetadataScopeIDKey]; got != "scope-1" {
		t.Fatalf("containment statement scope metadata = %#v, want scope-1", got)
	}
	if got := containment.Parameters[StatementMetadataGenerationIDKey]; got != "gen-1" {
		t.Fatalf("containment statement generation metadata = %#v, want gen-1", got)
	}
	containmentRows, ok := containment.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("containment rows type = %T, want []map[string]any", containment.Parameters["rows"])
	}
	if got, want := len(containmentRows), 2; got != want {
		t.Fatalf("containment rows count = %d, want %d", got, want)
	}
	for _, row := range containmentRows {
		if _, ok := row["file_path"]; ok {
			t.Fatalf("containment row unexpectedly contains file_path: %#v", row)
		}
		if got := row["entity_id"]; got == "" {
			t.Fatalf("containment row missing entity_id: %#v", row)
		}
	}
}

func TestCanonicalNodeWriterSplitsEntityContainmentByFile(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		Entities: []projector.EntityRow{
			{
				EntityID:   "entity-1",
				Label:      "Function",
				FilePath:   "/repos/my-repo/src/a.go",
				StartLine:  1,
				EndLine:    2,
				Language:   "go",
				RepoID:     "repo-1",
				EntityName: "a",
			},
			{
				EntityID:   "entity-2",
				Label:      "Function",
				FilePath:   "/repos/my-repo/src/b.go",
				StartLine:  3,
				EndLine:    4,
				Language:   "go",
				RepoID:     "repo-1",
				EntityName: "b",
			},
		},
	}

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var containmentCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			call.Parameters[StatementMetadataPhaseKey] == CanonicalPhaseEntityContainment {
			containmentCalls = append(containmentCalls, call)
		}
	}
	if got, want := len(containmentCalls), 2; got != want {
		t.Fatalf("containment statement count = %d, want %d", got, want)
	}

	gotFiles := []string{
		containmentCalls[0].Parameters["file_path"].(string),
		containmentCalls[1].Parameters["file_path"].(string),
	}
	wantFiles := []string{"/repos/my-repo/src/a.go", "/repos/my-repo/src/b.go"}
	for i, want := range wantFiles {
		if gotFiles[i] != want {
			t.Fatalf("containment file order = %#v, want %#v", gotFiles, wantFiles)
		}
	}
}

func TestCanonicalNodeWriterCanInlineEntityContainmentForBackendCompatibility(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil).
		WithEntityContainmentInEntityUpsert()

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		Entities: []projector.EntityRow{
			{
				EntityID:   "entity-1",
				Label:      "Function",
				FilePath:   "/repos/my-repo/src/main.go",
				StartLine:  1,
				EndLine:    2,
				Language:   "go",
				RepoID:     "repo-1",
				EntityName: "a",
			},
			{
				EntityID:   "entity-2",
				Label:      "Function",
				FilePath:   "/repos/my-repo/src/main.go",
				StartLine:  3,
				EndLine:    4,
				Language:   "go",
				RepoID:     "repo-1",
				EntityName: "b",
			},
		},
	}

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var entityCalls []Statement
	var containmentCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			call.Parameters[StatementMetadataPhaseKey] == CanonicalPhaseEntities {
			entityCalls = append(entityCalls, call)
		}
		if call.Operation == OperationCanonicalUpsert &&
			call.Parameters[StatementMetadataPhaseKey] == CanonicalPhaseEntityContainment {
			containmentCalls = append(containmentCalls, call)
		}
	}
	if got, want := len(entityCalls), 1; got != want {
		t.Fatalf("entity statement count = %d, want %d", got, want)
	}
	if got := len(containmentCalls); got != 0 {
		t.Fatalf("separate containment statement count = %d, want 0", got)
	}

	stmt := entityCalls[0]
	if !strings.Contains(stmt.Cypher, "UNWIND $rows AS row") {
		t.Fatalf("entity cypher = %q, want batched UNWIND shape", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MATCH (f:File {path: $file_path})") {
		t.Fatalf("entity cypher = %q, want file-scoped MATCH", stmt.Cypher)
	}
	if !strings.Contains(stmt.Cypher, "MERGE (f)-[rel:CONTAINS]->(n)") {
		t.Fatalf("entity cypher = %q, want inline containment MERGE", stmt.Cypher)
	}
	if got := stmt.Parameters["file_path"]; got != "/repos/my-repo/src/main.go" {
		t.Fatalf("file_path = %#v, want /repos/my-repo/src/main.go", got)
	}
}

func TestCanonicalNodeWriterCanInlineEntityContainmentAcrossFilesForPatchedBackends(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil).
		WithBatchedEntityContainmentInEntityUpsert()

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		Entities: []projector.EntityRow{
			{
				EntityID:   "entity-1",
				Label:      "Function",
				FilePath:   "/repos/my-repo/src/a.go",
				StartLine:  1,
				EndLine:    2,
				Language:   "go",
				RepoID:     "repo-1",
				EntityName: "a",
			},
			{
				EntityID:   "entity-2",
				Label:      "Function",
				FilePath:   "/repos/my-repo/src/b.go",
				StartLine:  3,
				EndLine:    4,
				Language:   "go",
				RepoID:     "repo-1",
				EntityName: "b",
			},
		},
	}

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var entityCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			call.Parameters[StatementMetadataPhaseKey] == CanonicalPhaseEntities {
			entityCalls = append(entityCalls, call)
		}
	}
	if got, want := len(entityCalls), 1; got != want {
		t.Fatalf("entity statement count = %d, want %d", got, want)
	}

	stmt := entityCalls[0]
	fileMatchIndex := strings.Index(stmt.Cypher, "MATCH (f:File {path: row.file_path})")
	entityMergeIndex := strings.Index(stmt.Cypher, "MERGE (n:Function {uid: row.entity_id})")
	if fileMatchIndex < 0 || entityMergeIndex < 0 {
		t.Fatalf("entity cypher = %q, want row-scoped file MATCH and entity MERGE", stmt.Cypher)
	}
	if fileMatchIndex > entityMergeIndex {
		t.Fatalf("entity cypher = %q, want row-scoped file MATCH before entity MERGE for NornicDB hot path", stmt.Cypher)
	}
	if _, ok := stmt.Parameters["file_path"]; ok {
		t.Fatalf("entity statement unexpectedly carries statement-level file_path: %#v", stmt.Parameters)
	}
	rows, ok := stmt.Parameters["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("rows type = %T, want []map[string]any", stmt.Parameters["rows"])
	}
	if got, want := len(rows), 2; got != want {
		t.Fatalf("rows count = %d, want %d", got, want)
	}
	if got := rows[0]["file_path"]; got != "/repos/my-repo/src/a.go" {
		t.Fatalf("rows[0] file_path = %#v, want /repos/my-repo/src/a.go", got)
	}
	if got := rows[1]["file_path"]; got != "/repos/my-repo/src/b.go" {
		t.Fatalf("rows[1] file_path = %#v, want /repos/my-repo/src/b.go", got)
	}
}

func TestCanonicalNodeWriterSplitsShortestPathRowsIntoSingletonFallback(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 2, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Files: []projector.FileRow{
			{
				Path:         "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				Name:         "main.go",
				Language:     "go",
				RepoID:       "repo-1",
				DirPath:      "/repos/my-repo/src",
			},
		},
		Entities: []projector.EntityRow{
			{
				EntityID:     "entity-1",
				Label:        "Function",
				EntityName:   "handleRelationships",
				FilePath:     "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				StartLine:    12,
				EndLine:      34,
				Language:     "go",
				RepoID:       "repo-1",
			},
			{
				EntityID:     "entity-2",
				Label:        "Function",
				EntityName:   "TestHandleCallChainReturnsShortestPath",
				FilePath:     "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				StartLine:    40,
				EndLine:      68,
				Language:     "go",
				RepoID:       "repo-1",
			},
			{
				EntityID:     "entity-3",
				Label:        "Function",
				EntityName:   "transitiveRelationshipsGraphResponse",
				FilePath:     "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				StartLine:    72,
				EndLine:      96,
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
	}

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var entityCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			(strings.Contains(call.Cypher, "MERGE (n:Function {uid: row.entity_id})") ||
				strings.Contains(call.Cypher, "MERGE (n:Function {uid: $entity_id})")) {
			entityCalls = append(entityCalls, call)
		}
	}

	if got, want := len(entityCalls), 3; got != want {
		t.Fatalf("entity statement count = %d, want %d", got, want)
	}

	if !strings.Contains(entityCalls[0].Cypher, "UNWIND $rows AS row") {
		t.Fatalf("first entity statement = %q, want batched UNWIND shape", entityCalls[0].Cypher)
	}
	firstRows, ok := entityCalls[0].Parameters["rows"].([]map[string]any)
	if !ok || len(firstRows) != 1 {
		t.Fatalf("first batch rows = %#v, want 1 safe row", entityCalls[0].Parameters["rows"])
	}
	if got := firstRows[0]["entity_id"]; got != "entity-1" {
		t.Fatalf("first batch entity_id = %#v, want entity-1", got)
	}

	if !strings.Contains(entityCalls[1].Cypher, "MERGE (n:Function {uid: $entity_id})") {
		t.Fatalf("second entity statement = %q, want singleton parameterized shape", entityCalls[1].Cypher)
	}
	if got := entityCalls[1].Parameters["entity_id"]; got != "entity-2" {
		t.Fatalf("singleton entity_id = %#v, want entity-2", got)
	}

	if !strings.Contains(entityCalls[2].Cypher, "UNWIND $rows AS row") {
		t.Fatalf("third entity statement = %q, want trailing batched UNWIND shape", entityCalls[2].Cypher)
	}
	lastRows, ok := entityCalls[2].Parameters["rows"].([]map[string]any)
	if !ok || len(lastRows) != 1 {
		t.Fatalf("last batch rows = %#v, want 1 safe row", entityCalls[2].Parameters["rows"])
	}
	if got := lastRows[0]["entity_id"]; got != "entity-3" {
		t.Fatalf("last batch entity_id = %#v, want entity-3", got)
	}
}
