package cypher

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

// mockExecutor records all Execute() calls in order for assertion.
type mockExecutor struct {
	calls []Statement
	err   error // optional error to return
}

func (m *mockExecutor) Execute(_ context.Context, stmt Statement) error {
	m.calls = append(m.calls, stmt)
	return m.err
}

type selectiveErrorExecutor struct {
	calls []Statement
	match func(Statement) bool
	err   error
}

func (m *selectiveErrorExecutor) Execute(_ context.Context, stmt Statement) error {
	m.calls = append(m.calls, stmt)
	if m.match != nil && m.match(stmt) {
		return m.err
	}
	return nil
}

// mockGroupExecutor implements both Executor and GroupExecutor for testing
// the atomic canonical write path.
type mockGroupExecutor struct {
	executeCalls []Statement // calls via Execute()
	groupCalls   int         // number of ExecuteGroup() invocations
	groupStmts   []Statement // statements from the last ExecuteGroup() call
	groupErr     error       // error to return from ExecuteGroup()
}

func (m *mockGroupExecutor) Execute(_ context.Context, stmt Statement) error {
	m.executeCalls = append(m.executeCalls, stmt)
	return nil
}

func (m *mockGroupExecutor) ExecuteGroup(_ context.Context, stmts []Statement) error {
	m.groupCalls++
	m.groupStmts = stmts
	return m.groupErr
}

type mockPhaseGroupExecutor struct {
	executeCalls    []Statement
	phaseGroupCalls int
	phaseGroups     [][]Statement
	phaseGroupErr   error
}

func (m *mockPhaseGroupExecutor) Execute(_ context.Context, stmt Statement) error {
	m.executeCalls = append(m.executeCalls, stmt)
	return nil
}

func (m *mockPhaseGroupExecutor) ExecutePhaseGroup(_ context.Context, stmts []Statement) error {
	m.phaseGroupCalls++
	cloned := append([]Statement(nil), stmts...)
	m.phaseGroups = append(m.phaseGroups, cloned)
	return m.phaseGroupErr
}

func TestCanonicalNodeWriterWritePhaseOrder(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "my-repo",
			Path:      "/repos/my-repo",
			LocalPath: "/repos/my-repo",
			RemoteURL: "https://github.com/org/my-repo",
			RepoSlug:  "org/my-repo",
			HasRemote: true,
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
		Modules: []projector.ModuleRow{
			{Name: "fmt", Language: "go"},
		},
		Imports: []projector.ImportRow{
			{FilePath: "/repos/my-repo/src/main.go", ModuleName: "fmt", ImportedName: "fmt", LineNumber: 3},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if len(exec.calls) == 0 {
		t.Fatal("expected executor calls, got 0")
	}

	// Verify strict phase order, including post-entity stale cleanup.
	// Find the phase boundaries by inspecting operation types and cypher content.
	phaseOrder := []string{}
	for _, call := range exec.calls {
		switch call.Operation {
		case OperationCanonicalRetract:
			phase, _ := call.Parameters[StatementMetadataPhaseKey].(string)
			if phase == "repository_cleanup" {
				if len(phaseOrder) == 0 || phaseOrder[len(phaseOrder)-1] != "repository_cleanup" {
					phaseOrder = append(phaseOrder, "repository_cleanup")
				}
			} else if phase == "entity_retract" {
				if len(phaseOrder) == 0 || phaseOrder[len(phaseOrder)-1] != "entity_retract" {
					phaseOrder = append(phaseOrder, "entity_retract")
				}
			} else if len(phaseOrder) == 0 || phaseOrder[len(phaseOrder)-1] != "retract" {
				phaseOrder = append(phaseOrder, "retract")
			}
		case OperationCanonicalUpsert:
			if strings.Contains(call.Cypher, "MERGE (r:Repository") {
				phaseOrder = append(phaseOrder, "repository")
			} else if strings.Contains(call.Cypher, "MATCH (d:Directory {path: row.path})") ||
				strings.Contains(call.Cypher, "MERGE (d:Directory") ||
				strings.Contains(call.Cypher, "CREATE (d:Directory") {
				if len(phaseOrder) == 0 || phaseOrder[len(phaseOrder)-1] != "directories" {
					phaseOrder = append(phaseOrder, "directories")
				}
			} else if strings.Contains(call.Cypher, "MATCH (f:File {path: row.path})") ||
				strings.Contains(call.Cypher, "MERGE (f:File") ||
				strings.Contains(call.Cypher, "CREATE (f:File") {
				if len(phaseOrder) == 0 || phaseOrder[len(phaseOrder)-1] != "files" {
					phaseOrder = append(phaseOrder, "files")
				}
			} else if strings.Contains(call.Cypher, "MERGE (n:Function") || strings.Contains(call.Cypher, "MERGE (n:Class") {
				if len(phaseOrder) == 0 || phaseOrder[len(phaseOrder)-1] != "entities" {
					phaseOrder = append(phaseOrder, "entities")
				}
			} else if call.Parameters[StatementMetadataPhaseKey] == CanonicalPhaseEntityContainment {
				if len(phaseOrder) == 0 || phaseOrder[len(phaseOrder)-1] != "entity_containment" {
					phaseOrder = append(phaseOrder, "entity_containment")
				}
			} else if strings.Contains(call.Cypher, "MERGE (m:Module") {
				if len(phaseOrder) == 0 || phaseOrder[len(phaseOrder)-1] != "modules" {
					phaseOrder = append(phaseOrder, "modules")
				}
			} else if strings.Contains(call.Cypher, "IMPORTS") || strings.Contains(call.Cypher, "HAS_PARAMETER") || strings.Contains(call.Cypher, "CONTAINS") {
				if len(phaseOrder) == 0 || phaseOrder[len(phaseOrder)-1] != "structural_edges" {
					phaseOrder = append(phaseOrder, "structural_edges")
				}
			}
		}
	}

	expected := []string{"retract", "repository_cleanup", "repository", "directories", "files", "entities", "entity_retract", "entity_containment", "modules", "structural_edges"}
	if len(phaseOrder) != len(expected) {
		t.Fatalf("phase order = %v, want %v", phaseOrder, expected)
	}
	for i := range expected {
		if phaseOrder[i] != expected[i] {
			t.Fatalf("phase[%d] = %q, want %q (full order: %v)", i, phaseOrder[i], expected[i], phaseOrder)
		}
	}
}

func TestCanonicalNodeWriterWriteReportsSequentialPhaseOnFailure(t *testing.T) {
	t.Parallel()

	exec := &selectiveErrorExecutor{
		match: func(stmt Statement) bool {
			return stmt.Operation == OperationCanonicalUpsert && strings.Contains(stmt.Cypher, "IMPORTS")
		},
		err: errors.New("boom"),
	}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "my-repo",
			Path:      "/repos/my-repo",
			LocalPath: "/repos/my-repo",
			RemoteURL: "https://github.com/org/my-repo",
			RepoSlug:  "org/my-repo",
			HasRemote: true,
		},
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		},
		Imports: []projector.ImportRow{
			{FilePath: "/repos/my-repo/src/main.go", ModuleName: "fmt", ImportedName: "fmt", LineNumber: 3},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err == nil {
		t.Fatal("Write() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "canonical sequential write (structural_edges)") {
		t.Fatalf("Write() error = %q, want structural_edges phase context", err.Error())
	}
}

func TestCanonicalNodeWriterDirectoryDepthOrder(t *testing.T) {
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
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo/src/pkg/sub", Name: "sub", ParentPath: "/repos/my-repo/src/pkg", RepoID: "repo-1", Depth: 2},
			{Path: "/repos/my-repo/src", Name: "src", ParentPath: "/repos/my-repo", RepoID: "repo-1", Depth: 0},
			{Path: "/repos/my-repo/src/pkg", Name: "pkg", ParentPath: "/repos/my-repo/src", RepoID: "repo-1", Depth: 1},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Collect directory-phase calls.
	var dirCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			strings.Contains(call.Cypher, "Directory {path: row.path}") {
			dirCalls = append(dirCalls, call)
		}
	}

	if len(dirCalls) == 0 {
		t.Fatal("expected directory calls, got 0")
	}

	// Depth-0 dirs use MATCH (r:Repository ...), depth 1+ use MATCH (p:Directory ...)
	// First call must be depth 0 (Repository parent)
	if !strings.Contains(dirCalls[0].Cypher, "MATCH (r:Repository") {
		t.Fatalf("first directory call should match Repository parent, got: %s", dirCalls[0].Cypher)
	}

	// Subsequent depth groups must use Directory parents. Each depth emits an
	// update-existing and guarded create-missing statement.
	for i := 2; i < len(dirCalls); i++ {
		if !strings.Contains(dirCalls[i].Cypher, "MATCH (p:Directory") {
			t.Fatalf("directory call[%d] should match Directory parent, got: %s", i, dirCalls[i].Cypher)
		}
	}
}

func TestCanonicalNodeWriterEntityUpsertsRemainLabelScoped(t *testing.T) {
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
		Entities: []projector.EntityRow{
			{EntityID: "f1", Label: "Function", EntityName: "foo", FilePath: "/f.go", RelativePath: "f.go", StartLine: 1, EndLine: 5, Language: "go", RepoID: "repo-1"},
			{EntityID: "c1", Label: "Class", EntityName: "Bar", FilePath: "/b.py", RelativePath: "b.py", StartLine: 1, EndLine: 10, Language: "python", RepoID: "repo-1"},
			{EntityID: "f2", Label: "Function", EntityName: "baz", FilePath: "/f.go", RelativePath: "f.go", StartLine: 7, EndLine: 12, Language: "go", RepoID: "repo-1"},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Collect batched entity-phase calls
	var entityCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			(strings.Contains(call.Cypher, "MERGE (n:Function") || strings.Contains(call.Cypher, "MERGE (n:Class")) {
			entityCalls = append(entityCalls, call)
		}
	}

	if len(entityCalls) != 2 {
		t.Fatalf("expected 2 entity batches (Function + Class), got %d", len(entityCalls))
	}

	var functionCount, classCount int
	for _, call := range entityCalls {
		rows, ok := call.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("rows type = %T, want []map[string]any", call.Parameters["rows"])
		}
		if strings.Contains(call.Cypher, "MERGE (n:Function") {
			functionCount += len(rows)
			if got, want := len(rows), 2; got != want {
				t.Fatalf("function batch size = %d, want %d", got, want)
			}
			continue
		}
		if strings.Contains(call.Cypher, "MERGE (n:Class") {
			classCount += len(rows)
			if got, want := len(rows), 1; got != want {
				t.Fatalf("class batch size = %d, want %d", got, want)
			}
			continue
		}
		t.Fatalf("unexpected entity cypher: %s", call.Cypher)
	}
	if got, want := functionCount, 2; got != want {
		t.Fatalf("function upsert count = %d, want %d", got, want)
	}
	if got, want := classCount, 1; got != want {
		t.Fatalf("class upsert count = %d, want %d", got, want)
	}
}

func TestCanonicalNodeWriterProjectsTypeScriptClassFamilyMetadata(t *testing.T) {
	t.Parallel()

	exec := &mockExecutor{}
	writer := NewCanonicalNodeWriter(exec, 500, nil)

	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-ts-1",
		GenerationID: "gen-ts-1",
		RepoID:       "repo-ts-1",
		RepoPath:     "/repos/ts",
		Repository: &projector.RepositoryRow{
			RepoID: "repo-ts-1",
			Name:   "ts-repo",
			Path:   "/repos/ts",
		},
		Entities: []projector.EntityRow{
			{
				EntityID:     "class-1",
				Label:        "Class",
				EntityName:   "Service",
				FilePath:     "/repos/ts/src/service.ts",
				RelativePath: "src/service.ts",
				StartLine:    3,
				EndLine:      18,
				Language:     "typescript",
				RepoID:       "repo-ts-1",
				Metadata: map[string]any{
					"decorators":              []any{"@sealed"},
					"type_parameters":         []any{"T"},
					"declaration_merge_group": "Service",
					"declaration_merge_count": 2,
					"declaration_merge_kinds": []any{"class", "namespace"},
				},
			},
			{
				EntityID:     "interface-1",
				Label:        "Interface",
				EntityName:   "Service",
				FilePath:     "/repos/ts/src/service.ts",
				RelativePath: "src/service.ts",
				StartLine:    20,
				EndLine:      32,
				Language:     "typescript",
				RepoID:       "repo-ts-1",
				Metadata: map[string]any{
					"declaration_merge_group": "Service",
					"declaration_merge_count": 2,
					"declaration_merge_kinds": []any{"class", "interface"},
				},
			},
			{
				EntityID:     "enum-1",
				Label:        "Enum",
				EntityName:   "ServiceKind",
				FilePath:     "/repos/ts/src/service.ts",
				RelativePath: "src/service.ts",
				StartLine:    34,
				EndLine:      42,
				Language:     "typescript",
				RepoID:       "repo-ts-1",
				Metadata: map[string]any{
					"declaration_merge_group": "ServiceKind",
					"declaration_merge_count": 2,
					"declaration_merge_kinds": []any{"enum", "namespace"},
				},
			},
		},
	}

	err := writer.Write(context.Background(), mat)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var entityCalls []Statement
	for _, call := range exec.calls {
		if call.Operation == OperationCanonicalUpsert &&
			(strings.Contains(call.Cypher, "MERGE (n:Class") ||
				strings.Contains(call.Cypher, "MERGE (n:Interface") ||
				strings.Contains(call.Cypher, "MERGE (n:Enum")) {
			entityCalls = append(entityCalls, call)
		}
	}
	if len(entityCalls) != 3 {
		t.Fatalf("expected 3 TS class-family entity upserts, got %d", len(entityCalls))
	}

	for _, call := range entityCalls {
		if !strings.Contains(call.Cypher, "SET n += row.props") {
			t.Fatalf("TS class-family cypher missing row.props merge: %s", call.Cypher)
		}
	}

	var classProperties map[string]any
	for _, call := range entityCalls {
		if strings.Contains(call.Cypher, "MERGE (n:Class") {
			rows, ok := call.Parameters["rows"].([]map[string]any)
			if !ok {
				t.Fatalf("class rows type = %T, want []map[string]any", call.Parameters["rows"])
			}
			if got, want := len(rows), 1; got != want {
				t.Fatalf("class row count = %d, want %d", got, want)
			}
			props, ok := rows[0]["props"].(map[string]any)
			if !ok {
				t.Fatalf("class props type = %T, want map[string]any", rows[0]["props"])
			}
			classProperties = props
			break
		}
	}
	if len(classProperties) == 0 {
		t.Fatal("missing Class properties in TS class-family entity calls")
	}
	decorators, ok := classProperties["decorators"].([]string)
	if !ok {
		t.Fatalf("class properties[decorators] type = %T, want []string", classProperties["decorators"])
	}
	if got, want := len(decorators), 1; got != want || decorators[0] != "@sealed" {
		t.Fatalf("class properties[decorators] = %#v, want [@sealed]", decorators)
	}
	typeParameters, ok := classProperties["type_parameters"].([]string)
	if !ok {
		t.Fatalf("class properties[type_parameters] type = %T, want []string", classProperties["type_parameters"])
	}
	if got, want := len(typeParameters), 1; got != want || typeParameters[0] != "T" {
		t.Fatalf("class properties[type_parameters] = %#v, want [T]", typeParameters)
	}
}
