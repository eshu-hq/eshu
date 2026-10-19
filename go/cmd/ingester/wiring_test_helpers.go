// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
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
	// probeCalls counts ExecuteProbe calls (#6852): the bounded existence
	// probe that precedes a bare-label retract drain now dispatches through
	// PhaseGroupExecutor.Inner as a sourcecypher.ProbeExecutor, so this fake
	// -- used as Inner in the shared drain-loop tests -- must implement it.
	probeCalls int
}

// ExecuteProbe always reports a match (found=true), mirroring the old
// drainCountReader.RunProbe fake it replaces, so existing drain-loop
// iteration-count assertions are unaffected by the probe's outcome.
func (r *recordingGroupChunkExecutor) ExecuteProbe(context.Context, sourcecypher.Statement) (bool, error) {
	r.probeCalls++
	return true, nil
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

// equalIntSlices reports whether got and want hold the same ints in order.
func equalIntSlices(got []int, want []int) bool {
	return slices.Equal(got, want)
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

func canonicalWriterContainmentMaterialization() canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &canonical.RepositoryRow{
			RepoID: "repo-1",
			Name:   "my-repo",
			Path:   "/repos/my-repo",
		},
		Directories: []canonical.DirectoryRow{
			{
				Path:       "/repos/my-repo/src",
				Name:       "src",
				ParentPath: "/repos/my-repo",
				RepoID:     "repo-1",
				Depth:      0,
			},
		},
		Files: []canonical.FileRow{
			{
				Path:         "/repos/my-repo/src/main.go",
				RelativePath: "src/main.go",
				Name:         "main.go",
				Language:     "go",
				RepoID:       "repo-1",
				DirPath:      "/repos/my-repo/src",
			},
		},
		Entities: []canonical.EntityRow{
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

func minimalCanonicalMaterialization() canonical.CanonicalMaterialization {
	return canonical.CanonicalMaterialization{
		ScopeID:      "scope-1",
		GenerationID: "gen-1",
		RepoID:       "repo-1",
		RepoPath:     "/repos/my-repo",
		Repository: &canonical.RepositoryRow{
			RepoID:    "repo-1",
			Name:      "my-repo",
			Path:      "/repos/my-repo",
			LocalPath: "/repos/my-repo",
		},
		Directories: []canonical.DirectoryRow{
			{Path: "/repos/my-repo/src", Name: "src", ParentPath: "/repos/my-repo", RepoID: "repo-1", Depth: 0},
		},
		Files: []canonical.FileRow{
			{Path: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", Name: "main.go", Language: "go", RepoID: "repo-1", DirPath: "/repos/my-repo/src"},
		},
		Entities: []canonical.EntityRow{
			{EntityID: "e1", Label: "Function", EntityName: "main", FilePath: "/repos/my-repo/src/main.go", RelativePath: "src/main.go", StartLine: 1, EndLine: 5, Language: "go", RepoID: "repo-1"},
		},
	}
}

// queryErrorDB answers every database call with an error. Wiring tests use it
// where they used postgres.SQLDB{}: the projector runtime builder runs the
// config-artifact startup checks (#8155) over the database, and a nil *sql.DB
// panics on the first query. Failing every call proves the builders survive
// failed checks: the derive stays off, the wiring succeeds, and reads stay
// live.
type queryErrorDB struct{}

func (queryErrorDB) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, errors.New("queryErrorDB: no database")
}

func (queryErrorDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("queryErrorDB: no database")
}

// Begin completes the artifacts.Database surface so the projector runtime
// builder accepts the fake. Wiring never opens a transaction; a call fails
// instead of returning a half transaction.
func (queryErrorDB) Begin(context.Context) (db.Transaction, error) {
	return nil, errors.New("queryErrorDB: no database")
}
