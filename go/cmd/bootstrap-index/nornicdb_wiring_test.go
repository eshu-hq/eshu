package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/runtime"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

func TestBootstrapCanonicalExecutorUsesNornicDBPhaseGroupsByDefault(t *testing.T) {
	t.Parallel()

	raw := &recordingBootstrapGroupExecutor{}
	executor, err := bootstrapCanonicalExecutorForGraphBackend(
		raw,
		runtime.GraphBackendNornicDB,
		func(key string) string {
			if key == nornicDBFilePhaseGroupStatementsEnv {
				return "2"
			}
			return ""
		},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("bootstrapCanonicalExecutorForGraphBackend() error = %v, want nil", err)
	}
	if _, ok := executor.(sourcecypher.GroupExecutor); ok {
		t.Fatal("NornicDB bootstrap executor exposes GroupExecutor, want bounded PhaseGroupExecutor only")
	}
	phaseExecutor, ok := executor.(sourcecypher.PhaseGroupExecutor)
	if !ok {
		t.Fatal("NornicDB bootstrap executor does not implement PhaseGroupExecutor")
	}

	stmts := []sourcecypher.Statement{
		bootstrapTestStatement("phase=files rows=1 chunk=1/6"),
		bootstrapTestStatement("phase=files rows=1 chunk=2/6"),
		bootstrapTestStatement("phase=files rows=1 chunk=3/6"),
		bootstrapTestStatement("phase=files rows=1 chunk=4/6"),
		bootstrapTestStatement("phase=files rows=1 chunk=5/6"),
		bootstrapTestStatement("phase=files rows=1 chunk=6/6"),
	}
	if err := phaseExecutor.ExecutePhaseGroup(context.Background(), stmts); err != nil {
		t.Fatalf("ExecutePhaseGroup() error = %v, want nil", err)
	}

	if got, want := raw.groupSizes, []int{2, 2, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("group sizes = %v, want %v", got, want)
	}
}

func TestBootstrapNornicDBPhaseGroupExecutorWrapsChunkFailure(t *testing.T) {
	t.Parallel()

	rawErr := errors.New("txn too large")
	raw := &recordingBootstrapGroupExecutor{err: rawErr}
	executor := bootstrapNornicDBPhaseGroupExecutor{
		inner:             raw,
		maxStatements:     500,
		fileMaxStatements: 2,
	}

	err := executor.ExecutePhaseGroup(context.Background(), []sourcecypher.Statement{
		bootstrapTestStatement("phase=files rows=1 chunk=1/3"),
		bootstrapTestStatement("phase=files rows=1 chunk=2/3"),
		bootstrapTestStatement("phase=files rows=1 chunk=3/3"),
	})
	if err == nil {
		t.Fatal("ExecutePhaseGroup() error = nil, want failure")
	}
	for _, want := range []string{
		"phase-group chunk 1/1",
		"statements 1-2 of 2",
		`first_statement="phase=files rows=1 chunk=1/3"`,
		rawErr.Error(),
	} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("ExecutePhaseGroup() error = %q, want %q", err.Error(), want)
		}
	}
}

func TestConfigureBootstrapCanonicalWriterBatchesContainmentAcrossFilesForNeo4j(t *testing.T) {
	t.Parallel()

	executor := &recordingBootstrapGroupExecutor{}
	writer := sourcecypher.NewCanonicalNodeWriter(executor, 500, nil)
	writer = configureBootstrapCanonicalWriter(writer, bootstrapCanonicalWriterConfig{
		GraphBackend: runtime.GraphBackendNeo4j,
	})

	if err := writer.Write(context.Background(), bootstrapContainmentMaterialization()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var inlineEntities int
	for _, stmt := range executor.groupStatements {
		if stmt.Parameters[sourcecypher.StatementMetadataPhaseKey] == sourcecypher.CanonicalPhaseEntityContainment {
			t.Fatalf("Neo4j bootstrap writer emitted separate entity_containment statement: %s", stmt.Cypher)
		}
		if stmt.Parameters[sourcecypher.StatementMetadataPhaseKey] != sourcecypher.CanonicalPhaseEntities {
			continue
		}
		if strings.Contains(stmt.Cypher, "MATCH (f:File {path: row.file_path})") &&
			strings.Contains(stmt.Cypher, "MERGE (f)-[rel:CONTAINS]->(n)") {
			inlineEntities++
		}
	}
	if inlineEntities != 1 {
		t.Fatalf("batched entity containment statements = %d, want 1", inlineEntities)
	}
}

func TestBootstrapContainmentMaterializationIncludesFileDirectories(t *testing.T) {
	t.Parallel()

	materialization := bootstrapContainmentMaterialization()
	directoriesByPath := make(map[string]struct{}, len(materialization.Directories))
	for _, directory := range materialization.Directories {
		directoriesByPath[directory.Path] = struct{}{}
	}

	for _, file := range materialization.Files {
		if file.DirPath == "" {
			continue
		}
		if _, ok := directoriesByPath[file.DirPath]; !ok {
			t.Fatalf("file %q references missing directory %q", file.Path, file.DirPath)
		}
	}
}

func TestConfigureBootstrapCanonicalWriterBatchesContainmentAcrossFilesForNornicDBByDefault(t *testing.T) {
	t.Parallel()

	executor := &recordingBootstrapGroupExecutor{}
	writer := sourcecypher.NewCanonicalNodeWriter(executor, 500, nil)
	writer = configureBootstrapCanonicalWriter(writer, bootstrapCanonicalWriterConfig{
		GraphBackend: runtime.GraphBackendNornicDB,
	})

	if err := writer.Write(context.Background(), bootstrapContainmentMaterialization()); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var batchedEntities int
	for _, stmt := range executor.groupStatements {
		if stmt.Parameters[sourcecypher.StatementMetadataPhaseKey] != sourcecypher.CanonicalPhaseEntities {
			continue
		}
		if strings.Contains(stmt.Cypher, "MATCH (f:File {path: row.file_path})") &&
			strings.Contains(stmt.Cypher, "MERGE (f)-[rel:CONTAINS]->(n)") {
			batchedEntities++
		}
	}
	if batchedEntities != 1 {
		t.Fatalf("NornicDB batched containment statements = %d, want 1", batchedEntities)
	}
}

func TestBootstrapNeo4jProfileGroupStatementsParsesOptIn(t *testing.T) {
	t.Parallel()

	enabled, err := bootstrapNeo4jProfileGroupStatements(func(key string) string {
		if key == "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS" {
			return "true"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("bootstrapNeo4jProfileGroupStatements() error = %v, want nil", err)
	}
	if !enabled {
		t.Fatal("bootstrapNeo4jProfileGroupStatements() = false, want true")
	}
}

func TestBootstrapNeo4jProfileGroupStatementsRejectsInvalidBool(t *testing.T) {
	t.Parallel()

	_, err := bootstrapNeo4jProfileGroupStatements(func(key string) string {
		if key == "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS" {
			return "sometimes"
		}
		return ""
	})
	if err == nil {
		t.Fatal("bootstrapNeo4jProfileGroupStatements() error = nil, want non-nil")
	}
	if !strings.Contains(err.Error(), "ESHU_NEO4J_PROFILE_GROUP_STATEMENTS") {
		t.Fatalf("error = %q, want env var name", err.Error())
	}
}

func bootstrapTestStatement(summary string) sourcecypher.Statement {
	return sourcecypher.Statement{
		Cypher: "RETURN $value",
		Parameters: map[string]any{
			"value":                                  1,
			sourcecypher.StatementMetadataPhaseKey:   sourcecypher.CanonicalPhaseFiles,
			sourcecypher.StatementMetadataSummaryKey: summary,
		},
	}
}

type recordingBootstrapGroupExecutor struct {
	groupSizes      []int
	groupStatements []sourcecypher.Statement
	err             error
}

func (r *recordingBootstrapGroupExecutor) Execute(context.Context, sourcecypher.Statement) error {
	return nil
}

func (r *recordingBootstrapGroupExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	r.groupSizes = append(r.groupSizes, len(stmts))
	r.groupStatements = append(r.groupStatements, stmts...)
	return r.err
}

func bootstrapContainmentMaterialization() projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
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
			{
				Path:       "/repos/my-repo/src",
				Name:       "src",
				ParentPath: "/repos/my-repo",
				RepoID:     "repo-1",
				Depth:      0,
			},
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
		},
	}
}
