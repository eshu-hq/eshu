package main

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

type groupCapableIngesterExecutor struct {
	executeCalls int
	groupCalls   int
}

func (g *groupCapableIngesterExecutor) Execute(context.Context, sourcecypher.Statement) error {
	g.executeCalls++
	return nil
}

func (g *groupCapableIngesterExecutor) ExecuteGroup(context.Context, []sourcecypher.Statement) error {
	g.groupCalls++
	return nil
}

type contextBlockingIngesterExecutor struct{}

func (contextBlockingIngesterExecutor) Execute(ctx context.Context, _ sourcecypher.Statement) error {
	<-ctx.Done()
	return ctx.Err()
}

type recordingGroupChunkExecutor struct {
	groupSizes        []int
	groupParams       []map[string]any
	groupStatements   []sourcecypher.Statement
	executeParams     []map[string]any
	executeStatements []sourcecypher.Statement
	callCount         int
	failAtCall        int
	err               error
}

func (r *recordingGroupChunkExecutor) Execute(_ context.Context, stmt sourcecypher.Statement) error {
	r.executeParams = append(r.executeParams, stmt.Parameters)
	r.executeStatements = append(r.executeStatements, stmt)
	return nil
}

func (r *recordingGroupChunkExecutor) ExecuteGroup(_ context.Context, stmts []sourcecypher.Statement) error {
	r.callCount++
	r.groupSizes = append(r.groupSizes, len(stmts))
	r.groupStatements = append(r.groupStatements, stmts...)
	if len(stmts) > 0 {
		r.groupParams = append(r.groupParams, stmts[0].Parameters)
	}
	if r.failAtCall > 0 && r.callCount == r.failAtCall {
		return r.err
	}
	return nil
}

func equalIntSlices(got []int, want []int) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func statementsContaining(stmts []sourcecypher.Statement, needle string) []sourcecypher.Statement {
	matches := make([]sourcecypher.Statement, 0)
	for _, stmt := range stmts {
		if strings.Contains(stmt.Cypher, needle) {
			matches = append(matches, stmt)
		}
	}
	return matches
}

func canonicalWriterContainmentMaterialization() projector.CanonicalMaterialization {
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

func minimalCanonicalMaterialization() projector.CanonicalMaterialization {
	return projector.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &projector.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "my-repo",
			Path:      "/repos/my-repo",
			LocalPath: "/repos/my-repo",
		},
		Directories: []projector.DirectoryRow{
			{Path: "/repos/my-repo/src", Name: "src", ParentPath: "/repos/my-repo", RepoID: "repo-1", Depth: 0},
		},
		Files: []projector.FileRow{
			{Path: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		},
		Entities: []projector.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "main", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 1, EndLine: 5, Language: "go", RepoID: "repo-1"},
		},
	}
}
