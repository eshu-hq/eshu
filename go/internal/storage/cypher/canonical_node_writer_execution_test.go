package cypher

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterErrorPropagation(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{err: errors.New("neo4j connection failed")}
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
	}

	err := writer.Write(context.Background(), mat)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "neo4j connection failed") {
		t.Fatalf("error = %v, want to contain 'neo4j connection failed'", err)
	}
}

func TestCanonicalNodeWriterDefaultBatchSize(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 0, nil) // 0 should default to DefaultBatchSize

	if writer.batchSize != DefaultBatchSize {
		t.Fatalf("batchSize = %d, want %d", writer.batchSize, DefaultBatchSize)
	}
}

func TestCanonicalNodeWriterDirectoryGenerationID(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-dir",
		GenerationID: "gen-dir",
		RepoID:       "repo-dir",
		RepoPath:     "/repos/dir-repo",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-dir",
			Name:   "dir-repo",
			Path:   "/repos/dir-repo",
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repos/dir-repo/src", Name: "src", ParentPath: "/repos/dir-repo", RepoID: "repo-dir", Depth: 0},
			{Path: "/repos/dir-repo/src/pkg", Name: "pkg", ParentPath: "/repos/dir-repo/src", RepoID: "repo-dir", Depth: 1},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Verify directory row maps include generation_id and scope_id
	for _, call := range exec.calls {
		if call.Operation != OperationCanonicalUpsert ||
			!strings.Contains(call.Cypher, "Directory {path: row.path}") {
			continue
		}
		rows, ok := call.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatal("directory upsert missing rows parameter")
		}
		for i, row := range rows {
			if row["generation_id"] != "gen-dir" {
				t.Fatalf("directory row[%d] generation_id = %v, want gen-dir", i, row["generation_id"])
			}
			if row["scope_id"] != "scope-dir" {
				t.Fatalf("directory row[%d] scope_id = %v, want scope-dir", i, row["scope_id"])
			}
		}
	}

	// Verify directory MERGE Cypher sets generation_id
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			strings.Contains(call.Cypher, "Directory {path: row.path}") {
			if !strings.Contains(call.Cypher, "d.generation_id") {
				t.Fatalf("directory write Cypher missing generation_id: %s", call.Cypher)
			}
			if !strings.Contains(call.Cypher, "d.scope_id") {
				t.Fatalf("directory write Cypher missing scope_id: %s", call.Cypher)
			}
		}
	}

	// Verify directory retract Cypher includes generation_id filter
	for _, call := range exec.calls {
		phase, _ := call.Parameters[StatementMetadataPhaseKey].(string)
		if call.Operation == OperationCanonicalRetract &&
			phase != "directory_cleanup" &&
			strings.Contains(call.Cypher, "Directory") {
			if !strings.Contains(call.Cypher, "generation_id") {
				t.Fatalf("directory retract Cypher missing generation_id filter: %s", call.Cypher)
			}
		}
	}
}

func TestCanonicalNodeWriterAtomicGroupExecutor(t *testing.T) {
	t.Parallel()

	exec := &mockGroupExecutor{}
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
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo/src", Name: "src", ParentPath: "/repos/my-repo", RepoID: "repo-1", Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "main", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 5, EndLine: 10, Language: "go", RepoID: "repo-1"},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// When GroupExecutor is available, ALL statements go through a single ExecuteGroup call
	if exec.groupCalls != 1 {
		t.Fatalf("ExecuteGroup calls = %d, want 1", exec.groupCalls)
	}
	if len(exec.executeCalls) != 0 {
		t.Fatalf("Execute calls = %d, want 0 (should use atomic path)", len(exec.executeCalls))
	}

	// Verify statements include all phases in order, including the post-entity
	// stale cleanup that must run after current entity upserts.
	stmts := exec.groupStmts
	if len(stmts) == 0 {
		t.Fatal("expected grouped statements, got 0")
	}

	// First statements should be retractions
	if stmts[0].Operation != OperationCanonicalRetract {
		t.Fatalf("first statement operation = %q, want %q", stmts[0].Operation, OperationCanonicalRetract)
	}

	// Last statements should be upserts
	last := stmts[len(stmts)-1]
	if last.Operation != OperationCanonicalUpsert {
		t.Fatalf("last statement operation = %q, want %q", last.Operation, OperationCanonicalUpsert)
	}

	lastEarlyRetractIdx := -1
	firstUpsertIdx := -1
	entityUpsertIdx := -1
	entityRetractIdx := -1
	for i, stmt := range stmts {
		phase, _ := stmt.Parameters[StatementMetadataPhaseKey].(string)
		if stmt.Operation == OperationCanonicalRetract &&
			phase != "directory_cleanup" &&
			phase != "entity_retract" {
			lastEarlyRetractIdx = i
		}
		if stmt.Operation == OperationCanonicalUpsert && firstUpsertIdx == -1 {
			firstUpsertIdx = i
		}
		if phase == "entities" && entityUpsertIdx == -1 {
			entityUpsertIdx = i
		}
		if phase == "entity_retract" && entityRetractIdx == -1 {
			entityRetractIdx = i
		}
	}
	if firstUpsertIdx >= 0 && lastEarlyRetractIdx >= firstUpsertIdx {
		t.Fatalf("early retraction at index %d came after upsert at index %d", lastEarlyRetractIdx, firstUpsertIdx)
	}
	if entityUpsertIdx >= 0 && entityRetractIdx >= 0 && entityRetractIdx <= entityUpsertIdx {
		t.Fatalf("entity_retract at index %d came before entity upsert at index %d", entityRetractIdx, entityUpsertIdx)
	}
}

func TestCanonicalNodeWriterFallsBackToSequential(t *testing.T) {
	t.Parallel()

	// mockExecutor only implements Executor, NOT GroupExecutor
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
			{Path: "/f1.go", RelativePath: "f1.go", Name: "f1.go", Language: "go", RepoID: "repo-1", DirPath: "/src"},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Sequential path: all calls go through Execute()
	if len(exec.calls) == 0 {
		t.Fatal("expected Execute() calls for sequential fallback, got 0")
	}
}

func TestCanonicalNodeWriterUsesPhaseGroupExecutor(t *testing.T) {
	t.Parallel()

	exec := &mockPhaseGroupExecutor{}
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
			{Path: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "main", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 5, EndLine: 10, Language: "go", RepoID: "repo-1"},
		},
	}

	if err := writer.Write(context.Background(), mat); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if got := len(exec.executeCalls); got != 0 {
		t.Fatalf("Execute calls = %d, want 0 for phase-group path", got)
	}
	if got := exec.phaseGroupCalls; got == 0 {
		t.Fatal("phaseGroupCalls = 0, want at least one phase group")
	}
	if got := len(exec.phaseGroups); got < 4 {
		t.Fatalf("phase group count = %d, want multiple ordered phases", got)
	}
	if got, want := exec.phaseGroups[0][0].Operation, OperationCanonicalRetract; got != want {
		t.Fatalf("first phase first operation = %q, want %q", got, want)
	}
}

func TestCanonicalNodeWriterEntityStatementsIncludePhaseDiagnostics(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{
				EntityID:     "e1",
				Label:        "Function",
				EntityName:   "main",
				FilePath:     "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				StartLine:    5,
				EndLine:      10,
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if len(stmts) == 0 {
		t.Fatal("buildEntityStatements() returned no statements")
	}
	for _, stmt := range stmts {
		if got, want := stmt.Parameters["_eshu_phase"], "entities"; got != want {
			t.Fatalf("entity statement _eshu_phase = %#v, want %#v", got, want)
		}
	}
}

// TestCanonicalNodeWriterTriggerSubstringRowStaysBatched is the inverted form
// of the prior "SingletonFallbackMarksExecuteOnlyMode" test. Before Path C,
// a single Function row whose EntityName contained the substring
// "ShortestPath" was routed through the per-row parameterized singleton path
// and tagged with _eshu_phase_group_mode="execute_only". The NornicDB-side
// regression test TestUnwindMergeChainBatch_EshuSingletonFallbackUnnecessary
// proved that parameterized UNWIND-batched cypher handles such trigger
// substrings safely (parameters are bound separately from cypher text per the
// Bolt protocol). The fallback is therefore obsolete; the row stays in the
// UNWIND-batched path and carries no execute-only marker.
func TestCanonicalNodeWriterTriggerSubstringRowStaysBatched(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{
				EntityID:     "e-shortest",
				Label:        "Function",
				EntityName:   "TestHandleCallChainReturnsShortestPath",
				FilePath:     "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				StartLine:    5,
				EndLine:      10,
				Language:     "go",
				RepoID:       "repo-1",
			},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if len(stmts) != 1 {
		t.Fatalf("buildEntityStatements() count = %d, want 1", len(stmts))
	}
	if got := stmts[0].Parameters[StatementMetadataPhaseGroupModeKey]; got != nil {
		t.Fatalf("%s = %#v, want absent (trigger substring no longer marks execute-only)", StatementMetadataPhaseGroupModeKey, got)
	}
	if !strings.Contains(stmts[0].Cypher, "UNWIND $rows AS row") {
		t.Fatalf("entity cypher = %q, want UNWIND-batched shape (trigger substring no longer forces singleton)", stmts[0].Cypher)
	}
}

func TestCanonicalNodeWriterEntityBatchSizeOverride(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil).WithEntityBatchSize(2)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "one", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 1, EndLine: 2, Language: "go", RepoID: "repo-1"},
			{EntityID: "e2", Label: "Function", EntityName: "two", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 3, EndLine: 4, Language: "go", RepoID: "repo-1"},
			{EntityID: "e3", Label: "Function", EntityName: "three", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 5, EndLine: 6, Language: "go", RepoID: "repo-1"},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if got, want := len(stmts), 2; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d", got, want)
	}
	firstRows, _ := stmts[0].Parameters["rows"].([]map[string]any)
	secondRows, _ := stmts[1].Parameters["rows"].([]map[string]any)
	if got, want := len(firstRows), 2; got != want {
		t.Fatalf("first entity batch rows = %d, want %d", got, want)
	}
	if got, want := len(secondRows), 1; got != want {
		t.Fatalf("second entity batch rows = %d, want %d", got, want)
	}
}
