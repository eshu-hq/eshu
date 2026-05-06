package cypher

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
)

func TestCanonicalNodeWriterEntityLabelBatchSizeOverride(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil).
		WithEntityBatchSize(100).
		WithEntityLabelBatchSize("Function", 2)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{EntityID: "c1", Label: "Class", EntityName: "One", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 1, EndLine: 2, Language: "go", RepoID: "repo-1"},
			{EntityID: "c2", Label: "Class", EntityName: "Two", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 3, EndLine: 4, Language: "go", RepoID: "repo-1"},
			{EntityID: "f1", Label: "Function", EntityName: "one", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 5, EndLine: 6, Language: "go", RepoID: "repo-1"},
			{EntityID: "f2", Label: "Function", EntityName: "two", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 7, EndLine: 8, Language: "go", RepoID: "repo-1"},
			{EntityID: "f3", Label: "Function", EntityName: "three", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 9, EndLine: 10, Language: "go", RepoID: "repo-1"},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if got, want := len(stmts), 3; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d", got, want)
	}

	var classRows []int
	var functionRows []int
	for _, stmt := range stmts {
		rows, _ := stmt.Parameters["rows"].([]map[string]any)
		summary, _ := stmt.Parameters["_eshu_statement_summary"].(string)
		switch {
		case strings.Contains(summary, "label=Class"):
			classRows = append(classRows, len(rows))
		case strings.Contains(summary, "label=Function"):
			functionRows = append(functionRows, len(rows))
		}
	}

	if got, want := len(classRows), 1; got != want {
		t.Fatalf("class batch count = %d, want %d", got, want)
	}
	if got, want := classRows[0], 2; got != want {
		t.Fatalf("class batch rows = %d, want %d", got, want)
	}
	if got, want := stmts[0].Parameters[StatementMetadataEntityLabelKey], "Class"; got != want {
		t.Fatalf("class statement entity label = %#v, want %#v", got, want)
	}
	if got, want := stmts[1].Parameters[StatementMetadataEntityLabelKey], "Function"; got != want {
		t.Fatalf("function statement entity label = %#v, want %#v", got, want)
	}
	if got, want := len(functionRows), 2; got != want {
		t.Fatalf("function batch count = %d, want %d", got, want)
	}
	if got, want := functionRows[0], 2; got != want {
		t.Fatalf("first function batch rows = %d, want %d", got, want)
	}
	if got, want := functionRows[1], 1; got != want {
		t.Fatalf("second function batch rows = %d, want %d", got, want)
	}
}

func TestCanonicalNodeWriterFileScopedContainmentHonorsLabelBatchSizeWithinFile(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil).
		WithEntityContainmentInEntityUpsert().
		WithEntityLabelBatchSize("K8sResource", 5)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
	}
	for i := 0; i < 12; i++ {
		mat.Entities = append(mat.Entities, projector.EntityRow{
			EntityID:     fmt.Sprintf("k8s-%02d", i),
			Label:        "K8sResource",
			EntityName:   fmt.Sprintf("route-%02d", i),
			FilePath:     "/repos/my-repo/charts/routes.yaml",
			RelativePath: "charts/routes.yaml",
			StartLine:    i + 1,
			EndLine:      i + 1,
			Language:     "yaml",
			RepoID:       "repo-1",
		})
	}

	stmts := writer.buildEntityStatements(mat)
	if got, want := len(stmts), 3; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d", got, want)
	}
	wantRows := []int{5, 5, 2}
	for i, stmt := range stmts {
		rows, ok := stmt.Parameters["rows"].([]map[string]any)
		if !ok {
			t.Fatalf("statement %d rows type = %T, want []map[string]any", i, stmt.Parameters["rows"])
		}
		if got := len(rows); got != wantRows[i] {
			t.Fatalf("statement %d rows = %d, want %d", i, got, wantRows[i])
		}
		if got, want := stmt.Parameters["file_path"], "/repos/my-repo/charts/routes.yaml"; got != want {
			t.Fatalf("statement %d file_path = %#v, want %#v", i, got, want)
		}
	}
}

func TestCanonicalNodeWriterEntityBatchesCrossFileBoundaries(t *testing.T) {
	t.Parallel()

	writer := NewCanonicalNodeWriter(&mockExecutor{}, 500, nil).WithEntityBatchSize(10)
	mat := projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Entities: []projector.EntityRow{
			{EntityID: "f1", Label: "Function", EntityName: "one", FilePath: "/repos/my-repo/src/a.go", RelativePath: "src/a.go", StartLine: 1, EndLine: 2, Language: "go", RepoID: "repo-1"},
			{EntityID: "f2", Label: "Function", EntityName: "two", FilePath: "/repos/my-repo/src/b.go", RelativePath: "src/b.go", StartLine: 3, EndLine: 4, Language: "go", RepoID: "repo-1"},
			{EntityID: "f3", Label: "Function", EntityName: "three", FilePath: "/repos/my-repo/src/a.go", RelativePath: "src/a.go", StartLine: 5, EndLine: 6, Language: "go", RepoID: "repo-1"},
		},
	}

	stmts := writer.buildEntityStatements(mat)
	if got, want := len(stmts), 1; got != want {
		t.Fatalf("buildEntityStatements() count = %d, want %d", got, want)
	}

	rows, _ := stmts[0].Parameters["rows"].([]map[string]any)
	if _, ok := stmts[0].Parameters["file_path"]; ok {
		t.Fatalf("entity batch unexpectedly has statement-level file_path: %#v", stmts[0].Parameters)
	}
	if got, want := len(rows), 3; got != want {
		t.Fatalf("entity batch rows = %d, want %d", got, want)
	}
	for _, row := range rows {
		if _, ok := row["file_path"]; ok {
			t.Fatalf("entity row unexpectedly contains file_path: %#v", row)
		}
	}
}

func TestCanonicalNodeWriterAtomicGroupExecutorError(t *testing.T) {
	t.Parallel()

	exec := &mockGroupExecutor{groupErr: errors.New("neo4j transaction too large")}
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
	if !strings.Contains(err.Error(), "neo4j transaction too large") {
		t.Fatalf("error = %v, want to contain 'neo4j transaction too large'", err)
	}
}
