// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"errors"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func TestImportDependencyRequestRejectsTargetFileForImportRows(t *testing.T) {
	t.Parallel()

	for _, queryType := range []string{
		"imports_by_file",
		"importers",
		"module_dependencies",
		"package_imports",
	} {
		t.Run(queryType, func(t *testing.T) {
			t.Parallel()

			err := (codemodel.ImportDependencyRequest{
				QueryType:  queryType,
				TargetFile: "src/target.py",
			}).Validate()
			if err == nil || !strings.Contains(err.Error(), "target_file") {
				t.Fatalf("validate() error = %v, want target_file contract error", err)
			}
		})
	}
}

func TestDirectImportRowsCypherUsesOneConnectedMatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		req  codemodel.ImportDependencyRequest
		want string
	}{
		{
			name: "repository",
			req:  codemodel.ImportDependencyRequest{RepoID: "repo-1"},
			want: "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File)-[rel:IMPORTS]->(target_module:Module)",
		},
		{
			name: "repository and file",
			req:  codemodel.ImportDependencyRequest{RepoID: "repo-1", SourceFile: "src/app.py"},
			want: "MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File {relative_path: $source_file})-[rel:IMPORTS]->(target_module:Module)",
		},
		{
			name: "file across repositories",
			req:  codemodel.ImportDependencyRequest{SourceFile: "src/app.py"},
			want: "MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file:File {relative_path: $source_file})-[rel:IMPORTS]->(target_module:Module)",
		},
		{
			name: "target module",
			req:  codemodel.ImportDependencyRequest{TargetModule: "requests"},
			want: "MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file:File)-[rel:IMPORTS]->(target_module:Module {name: $target_module})",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			cypher := codemodel.DirectImportRowsCypher(test.req)
			if got := strings.Count(cypher, "MATCH "); got != 1 {
				t.Fatalf("MATCH count = %d, want 1\n%s", got, cypher)
			}
			if !strings.Contains(cypher, test.want) {
				t.Fatalf("cypher = %q, want connected anchor %q", cypher, test.want)
			}
			if !strings.Contains(cypher, "ORDER BY repo.id, source_file.relative_path") {
				t.Fatalf("cypher = %q, want globally total repository-first order", cypher)
			}
		})
	}
}

func TestPackageImportRowsCypherPagesDistinctLogicalModules(t *testing.T) {
	t.Parallel()

	cypher := codemodel.PackageImportRowsCypher(codemodel.ImportDependencyRequest{
		QueryType: "package_imports",
		RepoID:    "repo-1",
	}, nil)
	if got := strings.Count(cypher, "MATCH "); got != 1 {
		t.Fatalf("MATCH count = %d, want 1\n%s", got, cypher)
	}
	if !strings.Contains(cypher, "RETURN DISTINCT repo.id as repo_id") {
		t.Fatalf("cypher = %q, want logical module distinct before paging", cypher)
	}
	if !strings.Contains(cypher, "ORDER BY repo_id, target_module, language") {
		t.Fatalf("cypher = %q, want stable logical module ordering", cypher)
	}
	if !strings.Contains(cypher, "SKIP $offset\nLIMIT $limit") {
		t.Fatalf("cypher = %q, want paging after distinct projection", cypher)
	}
}

func TestModuleScopedCypherPreservesRepositoryPathIdentity(t *testing.T) {
	t.Parallel()

	scopes := []map[string]any{{"repo_id": "repo-1", "path": "/shared/src/app.py"}}
	tests := map[string]string{
		"package imports": codemodel.PackageImportRowsCypher(codemodel.ImportDependencyRequest{
			QueryType:    "package_imports",
			SourceModule: "payments.api",
		}, scopes),
		"source module imports": codemodel.SourceModuleImportRowsCypher(codemodel.ImportDependencyRequest{
			SourceModule: "payments.api",
		}, scopes),
		"cross-module source": codemodel.CrossModuleCallRowsCypher(codemodel.ImportDependencyRequest{
			QueryType:    "cross_module_calls",
			SourceModule: "payments.api",
		}, scopes, nil),
		"cross-module target": codemodel.CrossModuleCallRowsCypher(codemodel.ImportDependencyRequest{
			QueryType:    "cross_module_calls",
			TargetModule: "payments.client",
		}, nil, scopes),
	}

	for name, cypher := range tests {
		cypher := cypher
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(cypher, ".path IN $") {
				t.Fatalf("cypher = %q, want backend-safe candidate path predicate", cypher)
			}
		})
	}
}

func TestModuleFilesCypherUsesExactModuleAnchorAndConnectedPath(t *testing.T) {
	t.Parallel()

	for name, cypher := range map[string]string{
		"source": codemodel.SourceModuleFilesCypher(codemodel.ImportDependencyRequest{
			RepoID:       "repo-1",
			SourceFile:   "src/app.py",
			SourceModule: "payments.api",
		}),
		"target": codemodel.TargetModuleFilesCypher(codemodel.ImportDependencyRequest{
			RepoID:       "repo-1",
			TargetFile:   "src/client.py",
			TargetModule: "payments.client",
		}),
	} {
		cypher := cypher
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := strings.Count(cypher, "MATCH "); got != 1 {
				t.Fatalf("MATCH count = %d, want 1\n%s", got, cypher)
			}
			if !strings.HasPrefix(cypher, "MATCH ("+name+"_module:Module {name: $"+name+"_module})<-") {
				t.Fatalf("cypher = %q, want exact module-first anchor", cypher)
			}
			if !strings.Contains(cypher, "[:REPO_CONTAINS]-(repo:Repository {id: $repo_id})") {
				t.Fatalf("cypher = %q, want one connected repository path", cypher)
			}
			if !strings.Contains(cypher, "LIMIT $scan_limit") {
				t.Fatalf("cypher = %q, want fail-closed membership bound", cypher)
			}
		})
	}
}

func TestBuildFileImportCycleRowsFindsReciprocalEdgesAndPaginates(t *testing.T) {
	t.Parallel()

	edges := []map[string]any{
		{
			"repo_id":       "repo-1",
			"repo_name":     "platform",
			"source_path":   "/proof/repo-1/src/a.py",
			"source_file":   "src/a.py",
			"source_name":   "a.py",
			"language":      "python",
			"target_module": "b",
			"line_number":   8,
		},
		{
			"repo_id":       "repo-1",
			"repo_name":     "platform",
			"source_path":   "/proof/repo-1/src/b.py",
			"source_file":   "src/b.py",
			"source_name":   "b.py",
			"language":      "python",
			"target_module": "a",
			"line_number":   13,
		},
		// A duplicate import edge must not duplicate the logical cycle.
		{
			"repo_id":       "repo-1",
			"repo_name":     "platform",
			"source_path":   "/proof/repo-1/src/a.py",
			"source_file":   "src/a.py",
			"source_name":   "a.py",
			"language":      "python",
			"target_module": "b",
			"line_number":   21,
		},
	}

	rows, err := codemodel.BuildFileImportCycleRows(codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    "repo-1",
		Language:  "python",
		Limit:     1,
	}, edges)
	if err != nil {
		t.Fatalf("codemodel.BuildFileImportCycleRows() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]
	if got, want := StringVal(row, "source_file"), "src/a.py"; got != want {
		t.Fatalf("source_file = %q, want %q", got, want)
	}
	if got, want := StringVal(row, "target_file"), "src/b.py"; got != want {
		t.Fatalf("target_file = %q, want %q", got, want)
	}
	if got, want := IntVal(row, "source_line_number"), 8; got != want {
		t.Fatalf("source_line_number = %d, want earliest %d", got, want)
	}
	if got, want := IntVal(row, "back_edge_line_number"), 13; got != want {
		t.Fatalf("back_edge_line_number = %d, want %d", got, want)
	}
}

func TestFileImportCycleEdgeRowsCypherDefersDirectionalFilters(t *testing.T) {
	t.Parallel()

	req := codemodel.ImportDependencyRequest{
		QueryType:    "file_import_cycles",
		RepoID:       "repo-1",
		SourceFile:   "src/a.py",
		TargetFile:   "src/b.py",
		SourceModule: "a",
		TargetModule: "b",
	}
	cypher := codemodel.FileImportCycleEdgeRowsCypher(req)
	if !strings.Contains(cypher, "Repository {id: $repo_id}") {
		t.Fatalf("cypher = %q, want repository anchor", cypher)
	}
	for _, directionalParameter := range []string{"$source_file", "$target_file", "$source_module", "$target_module"} {
		if strings.Contains(cypher, directionalParameter) {
			t.Fatalf("cypher = %q, directional filter %s removes reciprocal candidates", cypher, directionalParameter)
		}
	}

	edges := []map[string]any{
		importDependencyCycleProofEdge("src/a.py", "a.py", "b", 4),
		importDependencyCycleProofEdge("src/b.py", "b.py", "a", 8),
	}
	rows, err := codemodel.BuildFileImportCycleRows(req, edges)
	if err != nil {
		t.Fatalf("codemodel.BuildFileImportCycleRows() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("cycle rows = %d, want %d: %#v", got, want, rows)
	}
}

func TestUniquePackageImportRowsOrdersLogicalIdentity(t *testing.T) {
	t.Parallel()

	rows := codemodel.UniquePackageImportRows([]map[string]any{
		{"repo_id": "repo-1", "source_path": "/a.py", "target_module": "zeta", "language": "python"},
		{"repo_id": "repo-1", "source_path": "/b.py", "target_module": "alpha", "language": "python"},
		{"repo_id": "repo-1", "source_path": "/c.py", "target_module": "zeta", "language": "python"},
	})
	if got, want := len(rows), 2; got != want {
		t.Fatalf("unique package rows = %d, want %d: %#v", got, want, rows)
	}
	if got, want := StringVal(rows[0], "target_module"), "alpha"; got != want {
		t.Fatalf("first target_module = %q, want %q", got, want)
	}
	if got, want := StringVal(rows[1], "target_module"), "zeta"; got != want {
		t.Fatalf("second target_module = %q, want %q", got, want)
	}
}

func TestBuildFileImportCycleRowsFailsClosedAtScanLimit(t *testing.T) {
	t.Parallel()

	edges := make([]map[string]any, querycontract.ImportDependencyInternalScanLimit+1)
	_, err := codemodel.BuildFileImportCycleRows(codemodel.ImportDependencyRequest{
		QueryType: "file_import_cycles",
		RepoID:    "repo-1",
	}, edges)
	if !errors.Is(err, codemodel.ErrImportDependencyScopeTooBroad) {
		t.Fatalf("error = %v, want codemodel.ErrImportDependencyScopeTooBroad", err)
	}
}

func TestCrossModuleCallRowsCypherUsesOneConnectedPath(t *testing.T) {
	t.Parallel()

	cypher := codemodel.CrossModuleCallRowsCypher(codemodel.ImportDependencyRequest{
		QueryType: "cross_module_calls",
		RepoID:    "repo-1",
	}, nil, nil)
	if got := strings.Count(cypher, "MATCH "); got != 1 {
		t.Fatalf("MATCH count = %d, want 1\n%s", got, cypher)
	}
	if !strings.Contains(cypher, "(source_repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File)-[:CONTAINS]->(caller:Function)-[rel:CALLS]->(callee:Function)<-[:CONTAINS]-(target_file:File)<-[:REPO_CONTAINS]-(target_repo:Repository {id: $repo_id})") {
		t.Fatalf("cypher = %q, want one connected repository-scoped call path", cypher)
	}
	if !strings.Contains(cypher, "ORDER BY source_repo.id, source_file.relative_path") {
		t.Fatalf("cypher = %q, want globally total call order", cypher)
	}
}

func TestFilterCrossModuleCallRowsDropsCrossRepositoryRowsBeforePaging(t *testing.T) {
	t.Parallel()

	rows := []map[string]any{
		{
			"source_repo_id": "repo-1",
			"target_repo_id": "repo-2",
			"source_file":    "src/a.py",
			"target_file":    "src/b.py",
			"source_id":      "caller-cross",
			"target_id":      "callee-cross",
		},
		{
			"source_repo_id": "repo-1",
			"target_repo_id": "repo-1",
			"source_file":    "src/c.py",
			"target_file":    "src/d.py",
			"source_id":      "caller-local",
			"target_id":      "callee-local",
		},
	}

	filtered, err := codemodel.FilterCrossModuleCallRows(codemodel.ImportDependencyRequest{
		QueryType:  "cross_module_calls",
		SourceFile: "src/c.py",
		Limit:      1,
	}, rows, nil, nil)
	if err != nil {
		t.Fatalf("codemodel.FilterCrossModuleCallRows() error = %v, want nil", err)
	}
	if got, want := len(filtered), 1; got != want {
		t.Fatalf("len(filtered) = %d, want %d", got, want)
	}
	if got, want := StringVal(filtered[0], "repo_id"), "repo-1"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
	if _, ok := filtered[0]["target_repo_id"]; ok {
		t.Fatalf("filtered row leaked target_repo_id: %#v", filtered[0])
	}
}
