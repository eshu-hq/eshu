// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package language

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// directoryFakeGraph answers the three reads the directory branch makes, told
// apart by statement, and records every call.
//
// It routes on statement rather than on call order deliberately: a regression
// that stopped resolving names, or that read every repository id for a scoped
// caller, would still make "the right number of calls" and pass an
// order-indexed double.
type directoryFakeGraph struct {
	statements    []string
	params        []map[string]any
	directoryRows []map[string]any
	repositoryIDs []string
	names         map[string]string
}

func (f *directoryFakeGraph) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.statements = append(f.statements, cypher)
	f.params = append(f.params, params)

	switch {
	case strings.Contains(cypher, "(d:Directory {repo_id: rid})"):
		return f.directoryRows, nil
	case strings.Contains(cypher, "MATCH (r:Repository {id: rid})"):
		requested, _ := params["repo_ids"].([]string)
		rows := make([]map[string]any, 0, len(requested))
		for _, id := range requested {
			if name, ok := f.names[id]; ok {
				rows = append(rows, map[string]any{"repo_id": id, "repo_name": name})
			}
		}
		return rows, nil
	case strings.Contains(cypher, "MATCH (r:Repository)"):
		rows := make([]map[string]any, 0, len(f.repositoryIDs))
		for _, id := range f.repositoryIDs {
			rows = append(rows, map[string]any{"repo_id": id})
		}
		return rows, nil
	}
	return nil, nil
}

func (f *directoryFakeGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

// directoryStatementParams returns the params of the first directory statement
// the fake was asked to run.
func (f *directoryFakeGraph) directoryStatementParams(t *testing.T) map[string]any {
	t.Helper()

	for i, statement := range f.statements {
		if strings.Contains(statement, "(d:Directory {repo_id: rid})") {
			return f.params[i]
		}
	}
	t.Fatalf("no directory statement was run: %#v", f.statements)
	return nil
}

// directoryFakeRow is one row shaped like the statement's projection.
func directoryFakeRow(name, repoID string, fileCount int) map[string]any {
	return map[string]any{
		"entity_id":  nil,
		"name":       name,
		"labels":     []string{"Directory"},
		"file_path":  nil,
		"repo_id":    repoID,
		"file_count": fileCount,
	}
}

// directoryTestGrant grants exactly the named repositories.
func directoryTestGrant(repoIDs ...string) codequery.LanguageQueryGrant {
	return codequery.LanguageQueryGrant{Access: querycontract.RepositoryAccessFilter{
		AllowedRepositoryIDs: repoIDs,
	}}
}

// TestDirectoryLanguageQueryFillsRepoNameFromTheSecondRead proves the column
// the statement stopped projecting is restored on the way out, and that the
// name read is keyed on the page's repositories rather than on the grant.
func TestDirectoryLanguageQueryFillsRepoNameFromTheSecondRead(t *testing.T) {
	t.Parallel()

	graph := &directoryFakeGraph{
		directoryRows: []map[string]any{
			directoryFakeRow("lib", "repo-b", 4),
			directoryFakeRow("inner", "repo-a", 3),
		},
		names: map[string]string{"repo-a": "alpha-svc", "repo-b": "beta-svc", "repo-c": "gamma-svc"},
	}
	handler := &Handler{Neo4j: graph}

	rows, err := handler.directoryRowsByLanguage(context.Background(), "go", "", "", 50,
		directoryTestGrant("repo-a", "repo-b", "repo-c"))
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}

	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2: %#v", len(rows), rows)
	}
	for _, want := range []struct{ name, repoName string }{{"lib", "beta-svc"}, {"inner", "alpha-svc"}} {
		found := false
		for _, row := range rows {
			if querycontract.StringVal(row, "name") != want.name {
				continue
			}
			found = true
			if got := querycontract.StringVal(row, "repo_name"); got != want.repoName {
				t.Fatalf("row %q repo_name = %q, want %q", want.name, got, want.repoName)
			}
		}
		if !found {
			t.Fatalf("row %q missing: %#v", want.name, rows)
		}
	}

	// repo-c is granted but holds no directory on this page, so naming it
	// would be a read the answer does not need.
	for i, statement := range graph.statements {
		if !strings.Contains(statement, "MATCH (r:Repository {id: rid})") {
			continue
		}
		asked, _ := graph.params[i]["repo_ids"].([]string)
		if slices.Contains(asked, "repo-c") {
			t.Fatalf("the name read asked for repo-c, which is not on the page: %#v", asked)
		}
	}
}

// TestDirectoryLanguageQueryTakesTheGlobalTopNFromAPerRepositorySuperset is the
// handler-level proof of the row bound. The fake returns what the pinned
// NornicDB build returns for a limit of 2 across two repositories: each group
// ordered and cut to 2, four rows in all. The caller must still get 2.
func TestDirectoryLanguageQueryTakesTheGlobalTopNFromAPerRepositorySuperset(t *testing.T) {
	t.Parallel()

	graph := &directoryFakeGraph{
		directoryRows: []map[string]any{
			directoryFakeRow("inner", "repo-a", 3),
			directoryFakeRow("src", "repo-a", 2),
			directoryFakeRow("lib", "repo-b", 4),
			directoryFakeRow("cmd", "repo-b", 2),
		},
		names: map[string]string{"repo-a": "alpha-svc", "repo-b": "beta-svc"},
	}
	handler := &Handler{Neo4j: graph}

	rows, err := handler.directoryRowsByLanguage(context.Background(), "go", "", "", 2,
		directoryTestGrant("repo-a", "repo-b"))
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}

	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2; the per-repository superset reached the caller: %#v", len(rows), rows)
	}
	if got := querycontract.StringVal(rows[0], "name"); got != "lib" {
		t.Fatalf("first row = %q, want \"lib\"", got)
	}
	if got := querycontract.StringVal(rows[1], "name"); got != "inner" {
		t.Fatalf("second row = %q, want \"inner\"", got)
	}
}

// TestDirectoryLanguageQueryReadsEveryRepositoryIDForAnUnscopedCaller pins the
// unscoped admin path: the id list comes from the graph, and it reaches the
// directory statement.
func TestDirectoryLanguageQueryReadsEveryRepositoryIDForAnUnscopedCaller(t *testing.T) {
	t.Parallel()

	graph := &directoryFakeGraph{
		repositoryIDs: []string{"repo-a", "repo-b"},
		directoryRows: []map[string]any{directoryFakeRow("lib", "repo-b", 4)},
		names:         map[string]string{"repo-b": "beta-svc"},
	}
	handler := &Handler{Neo4j: graph}
	grant := codequery.LanguageQueryGrant{Access: querycontract.RepositoryAccessFilter{AllScopes: true}}

	rows, err := handler.directoryRowsByLanguage(context.Background(), "go", "", "", 50, grant)
	if err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}

	if len(rows) != 1 || querycontract.StringVal(rows[0], "repo_name") != "beta-svc" {
		t.Fatalf("rows = %#v, want one row named beta-svc", rows)
	}
	bound, _ := graph.directoryStatementParams(t)["repo_ids"].([]string)
	if !slices.Equal(bound, []string{"repo-a", "repo-b"}) {
		t.Fatalf("directory statement bound repo_ids = %#v, want every repository id from the graph", bound)
	}
}

// TestDirectoryLanguageQueryOnAGrantThatAllowsNothingReadsNoBackend is the
// impossible-value control. A grant that matches nothing must answer an empty
// page, and it must do so without asking the backend anything: a statement
// UNWINDing an empty list is a read that cannot return rows.
func TestDirectoryLanguageQueryOnAGrantThatAllowsNothingReadsNoBackend(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		repoID string
		grant  codequery.LanguageQueryGrant
	}{
		{
			name:  "a scoped caller with no grants",
			grant: codequery.LanguageQueryGrant{Access: querycontract.RepositoryAccessFilter{}},
		},
		{
			name:   "a repository outside the grant",
			repoID: "repo-z",
			grant:  directoryTestGrant("repo-a"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			graph := &directoryFakeGraph{
				directoryRows: []map[string]any{directoryFakeRow("leaked", "repo-a", 9)},
				names:         map[string]string{"repo-a": "alpha-svc"},
			}
			handler := &Handler{Neo4j: graph}

			rows, err := handler.directoryRowsByLanguage(context.Background(), "go", "", tc.repoID, 50, tc.grant)
			if err != nil {
				t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
			}
			if len(rows) != 0 {
				t.Fatalf("rows = %#v, want none", rows)
			}
			if len(graph.statements) != 0 {
				t.Fatalf("the backend was read %d time(s) for a grant that allows nothing: %#v", len(graph.statements), graph.statements)
			}
		})
	}
}

// TestDirectoryLanguageQueryScopedCallerNeverReadsEveryRepositoryID proves the
// unscoped whole-label read is not on the scoped path. A scoped caller's list
// follows from its grant, and reading every repository id would both cost a
// label scan and reach outside the grant.
func TestDirectoryLanguageQueryScopedCallerNeverReadsEveryRepositoryID(t *testing.T) {
	t.Parallel()

	graph := &directoryFakeGraph{
		repositoryIDs: []string{"repo-a", "repo-b", "repo-secret"},
		directoryRows: []map[string]any{directoryFakeRow("inner", "repo-a", 3)},
		names:         map[string]string{"repo-a": "alpha-svc"},
	}
	handler := &Handler{Neo4j: graph}

	if _, err := handler.directoryRowsByLanguage(context.Background(), "go", "", "", 50, directoryTestGrant("repo-a")); err != nil {
		t.Fatalf("directoryRowsByLanguage() error = %v, want nil", err)
	}

	for _, statement := range graph.statements {
		if strings.Contains(statement, "MATCH (r:Repository)\n") {
			t.Fatalf("a scoped caller ran the unscoped every-repository read:\n%s", statement)
		}
	}
	bound, _ := graph.directoryStatementParams(t)["repo_ids"].([]string)
	if !slices.Equal(bound, []string{"repo-a"}) {
		t.Fatalf("directory statement bound repo_ids = %#v, want only the granted repository", bound)
	}
}
