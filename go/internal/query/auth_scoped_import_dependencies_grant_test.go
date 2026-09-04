// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// #5167 code-family batch 2a: two-tenant proof for
// POST /api/v0/code/imports/investigate, one case per query_type.
//
// repo_id is optional on this route -- req.validate() accepts any one of
// repo_id, source_file, target_file, source_module or target_module -- so
// {"source_file": "..."} ran every one of the seven builders corpus-wide with
// no grant bound at all. All seven write their repository node through
// writeRepositoryNode and their predicates through writeCypherPredicates, which
// always emits a MATCH-attached WHERE on the single anchoring MATCH, so the
// grant lands in one place per builder, ahead of SKIP/LIMIT and ahead of the
// 25,000-row internal scan bound.

const (
	importGrantGrantedModule   = "granted_module"
	importGrantUngrantedModule = "ungranted_module"
)

// importGrantRow is one row the fake can return, tagged with the repository
// each Repository alias in the pattern resolves to.
type importGrantRow struct {
	repos map[string]string
	row   map[string]any
}

// importGrantAnswer serves the statements its match reports true for.
type importGrantAnswer struct {
	match func(normalized string) bool
	rows  []importGrantRow
}

// evaluatingImportDependencyGraph applies the repository predicates the emitted
// statement actually carries -- the inline {id: $repo_id} anchor and, for a
// scoped caller, the grant condition -- per Repository alias. Drop a builder's
// grant and the other tenant's rows come back, which is what the body
// assertions below fail on.
type evaluatingImportDependencyGraph struct {
	answers    []importGrantAnswer
	statements []string
}

func (g *evaluatingImportDependencyGraph) Run(
	_ context.Context,
	cypher string,
	params map[string]any,
) ([]map[string]any, error) {
	g.statements = append(g.statements, cypher)
	normalized := normalizeCypherWhitespace(cypher)
	for _, answer := range g.answers {
		if !answer.match(normalized) {
			continue
		}
		rows := make([]map[string]any, 0, len(answer.rows))
		for _, seed := range answer.rows {
			if importGrantRowAdmitted(normalized, params, seed) {
				rows = append(rows, cloneGraphRow(seed.row))
			}
		}
		return rows, nil
	}
	return nil, nil
}

func (g *evaluatingImportDependencyGraph) RunSingle(
	ctx context.Context,
	cypher string,
	params map[string]any,
) (map[string]any, error) {
	rows, err := g.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func importGrantRowAdmitted(normalized string, params map[string]any, seed importGrantRow) bool {
	for alias, repoID := range seed.repos {
		if strings.Contains(normalized, "("+alias+":Repository {id: $repo_id})") {
			bound, _ := params["repo_id"].(string)
			if repoID != bound || repoID == "" {
				return false
			}
		}
		if strings.Contains(normalized, alias+".id IN $allowed_repository_ids") {
			if !graphParamContains(params, "allowed_repository_ids", repoID) &&
				!graphParamContains(params, "allowed_scope_ids", repoID) {
				return false
			}
		}
	}
	return true
}

// importGrantImportEdge is one IMPORTS row shaped for directImportRowsCypher,
// packageImportRowsCypher and fileImportCycleEdgeRowsCypher, all of which
// project through the `repo` alias.
func importGrantImportEdge(repoID, sourceFile, targetModule string, line int) importGrantRow {
	sourceName := sourceFile
	if at := strings.LastIndex(sourceFile, "/"); at >= 0 {
		sourceName = sourceFile[at+1:]
	}
	return importGrantRow{
		repos: map[string]string{"repo": repoID},
		row: map[string]any{
			"repo_id":       repoID,
			"repo_name":     repoID,
			"source_path":   "/proof/" + repoID + "/" + sourceFile,
			"source_file":   sourceFile,
			"source_name":   sourceName,
			"language":      "python",
			"target_module": targetModule,
			"line_number":   line,
		},
	}
}

// importGrantCrossModuleCall is one CALLS row shaped for
// crossModuleCallRowsCypher, whose pattern binds two Repository aliases.
func importGrantCrossModuleCall(sourceRepo, targetRepo, name string) importGrantRow {
	return importGrantRow{
		repos: map[string]string{"source_repo": sourceRepo, "target_repo": targetRepo},
		row: map[string]any{
			"source_repo_id": sourceRepo,
			"target_repo_id": targetRepo,
			"repo_name":      sourceRepo,
			"source_path":    "/proof/" + sourceRepo + "/src/api.py",
			"target_path":    "/proof/" + targetRepo + "/src/service.py",
			"source_file":    "src/api.py",
			"target_file":    "src/service.py",
			"source_name":    name,
			"source_id":      "function-" + name,
			"target_name":    name + "_callee",
			"target_id":      "function-" + name + "-callee",
			"call_kind":      "direct",
		},
	}
}

// importGrantQueryTypeCase is one query_type's two-tenant fixture: the request
// body, the seeded rows, the token the granted tenant's answer must carry, and
// the tokens the other tenant's must never reach.
//
// present/leaked differ by query type because the Go passes behind each one
// keep different columns. The cycle rows survive reconstruction only when both
// tenants use the same file names, so that case is distinguished by repository
// id rather than by module name.
type importGrantQueryTypeCase struct {
	queryType string
	body      map[string]any
	seeds     []importGrantRow
	present   string
	leaked    []string
}

func importGrantCycleEdges() []importGrantRow {
	edges := make([]importGrantRow, 0, 4)
	for _, repoID := range []string{codeGrantGrantedRepo, codeGrantOtherRepo} {
		edges = append(edges,
			importGrantImportEdge(repoID, "src/module_a.py", "module_b", 4),
			importGrantImportEdge(repoID, "src/module_b.py", "module_a", 7),
		)
	}
	return edges
}

func importGrantQueryTypeCases() []importGrantQueryTypeCase {
	importEdges := []importGrantRow{
		importGrantImportEdge(codeGrantGrantedRepo, "src/module_a.py", importGrantGrantedModule, 3),
		importGrantImportEdge(codeGrantOtherRepo, "src/module_a.py", importGrantUngrantedModule, 3),
	}
	moduleLeak := []string{importGrantUngrantedModule, codeGrantOtherRepo}
	importBody := map[string]any{"source_file": "src/module_a.py"}
	cases := make([]importGrantQueryTypeCase, 0, 6)
	for _, queryType := range []string{"imports_by_file", "importers", "module_dependencies", "package_imports"} {
		cases = append(cases, importGrantQueryTypeCase{
			queryType: queryType,
			body:      importBody,
			seeds:     importEdges,
			present:   importGrantGrantedModule,
			leaked:    moduleLeak,
		})
	}
	return append(cases,
		importGrantQueryTypeCase{
			queryType: "file_import_cycles",
			body:      map[string]any{"language": "python", "target_file": "src/module_b.py"},
			seeds:     importGrantCycleEdges(),
			present:   codeGrantGrantedRepo,
			leaked:    []string{codeGrantOtherRepo},
		},
		importGrantQueryTypeCase{
			queryType: "cross_module_calls",
			body:      map[string]any{"source_file": "src/api.py"},
			seeds: []importGrantRow{
				importGrantCrossModuleCall(codeGrantGrantedRepo, codeGrantGrantedRepo, importGrantGrantedModule),
				importGrantCrossModuleCall(codeGrantOtherRepo, codeGrantOtherRepo, importGrantUngrantedModule),
			},
			present: importGrantGrantedModule,
			leaked:  moduleLeak,
		},
	)
}

// requestBody returns the case's request body with its query_type set.
func (c importGrantQueryTypeCase) requestBody() map[string]any {
	body := map[string]any{"query_type": c.queryType}
	for key, value := range c.body {
		body[key] = value
	}
	return body
}

func newImportGrantGraph(seeds []importGrantRow) *evaluatingImportDependencyGraph {
	return &evaluatingImportDependencyGraph{answers: []importGrantAnswer{{
		match: func(string) bool { return true },
		rows:  seeds,
	}}}
}

func runImportGrantRequest(
	t *testing.T,
	graph GraphQuery,
	body map[string]any,
	auth *AuthContext,
) *httptest.ResponseRecorder {
	t.Helper()

	handler := &CodeHandler{Profile: ProfileLocalAuthoritative, Neo4j: graph}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := newCodeGrantRouteRequest(t, "/api/v0/code/imports/investigate", body, auth)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestImportDependenciesFilterByRepositoryGrant(t *testing.T) {
	t.Parallel()

	for _, tc := range importGrantQueryTypeCases() {
		t.Run(tc.queryType, func(t *testing.T) {
			t.Parallel()

			graph := newImportGrantGraph(tc.seeds)
			auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
			rec := runImportGrantRequest(t, graph, tc.requestBody(), &auth)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.present) {
				t.Fatalf("granted tenant's rows are missing %q: %s", tc.present, body)
			}
			for _, leaked := range tc.leaked {
				if strings.Contains(body, leaked) {
					t.Fatalf("scoped %s query leaked %q: %s", tc.queryType, leaked, body)
				}
			}
		})
	}
}

func TestImportDependenciesEmptyGrantReachesNoBackend(t *testing.T) {
	t.Parallel()

	for _, tc := range importGrantQueryTypeCases() {
		t.Run(tc.queryType, func(t *testing.T) {
			t.Parallel()

			graph := newImportGrantGraph(tc.seeds)
			auth := codeGrantScopedAuthContext(nil)
			rec := runImportGrantRequest(t, graph, tc.requestBody(), &auth)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			if len(graph.statements) != 0 {
				t.Fatalf("a grantless scoped caller reached the graph: %v", graph.statements)
			}
			data := decodeEnvelopeData(t, rec.Body.Bytes())
			rowKey := importGrantRowKey(tc.queryType)
			value, ok := data[rowKey]
			if !ok {
				t.Fatalf("response has no %q field: %s", rowKey, rec.Body.String())
			}
			rows, ok := value.([]any)
			if !ok {
				t.Fatalf("%q = %#v, want an empty JSON array, not null: %s", rowKey, value, rec.Body.String())
			}
			if len(rows) != 0 {
				t.Fatalf("%q = %#v, want no rows for a grantless caller", rowKey, rows)
			}
		})
	}
}

// importGrantRowKey is the one canonical row key each query type answers with.
func importGrantRowKey(queryType string) string {
	switch queryType {
	case "file_import_cycles":
		return "cycles"
	case "cross_module_calls":
		return "cross_module_calls"
	case "package_imports":
		return "modules"
	default:
		return "dependencies"
	}
}

func TestImportDependenciesResolveAScopeOnlyGrantToItsRepository(t *testing.T) {
	t.Parallel()

	for _, tc := range importGrantQueryTypeCases() {
		t.Run(tc.queryType, func(t *testing.T) {
			t.Parallel()

			graph := newImportGrantGraph(tc.seeds)
			auth := codeGrantScopeOnlyAuthContext([]string{codeGrantGrantedRepo})
			rec := runImportGrantRequest(t, graph, tc.requestBody(), &auth)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			if !strings.Contains(body, tc.present) {
				t.Fatalf("scope-only grant returned no row for the repository it names: %s", body)
			}
			for _, leaked := range tc.leaked {
				if strings.Contains(body, leaked) {
					t.Fatalf("scope-only %s query leaked %q: %s", tc.queryType, leaked, body)
				}
			}
		})
	}
}

func TestImportDependenciesSharedKeyReadIsUnchanged(t *testing.T) {
	t.Parallel()

	for _, tc := range importGrantQueryTypeCases() {
		t.Run(tc.queryType, func(t *testing.T) {
			t.Parallel()

			graph := newImportGrantGraph(tc.seeds)
			rec := runImportGrantRequest(t, graph, tc.requestBody(), nil)

			if got, want := rec.Code, http.StatusOK; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
			}
			body := rec.Body.String()
			for _, want := range append([]string{tc.present}, tc.leaked...) {
				if !strings.Contains(body, want) {
					t.Fatalf("unscoped %s query lost %q: %s", tc.queryType, want, body)
				}
			}
			for _, statement := range graph.statements {
				if strings.Contains(statement, "allowed_repository_ids") {
					t.Fatalf("unscoped %s query carries a grant condition:\n%s", tc.queryType, statement)
				}
			}
		})
	}
}

// TestCrossModuleCallsBindTargetRepositoryIndependently is the batch-1
// consumer-side rule at this route's one two-anchor pattern. Today
// crossModuleCallRowMatches also drops a row whose two endpoints disagree, but
// that runs in Go AFTER $scan_limit; binding target_repo in the statement is
// what keeps an out-of-grant callee from spending the scan budget.
func TestCrossModuleCallsBindTargetRepositoryIndependently(t *testing.T) {
	t.Parallel()

	graph := newImportGrantGraph([]importGrantRow{
		importGrantCrossModuleCall(codeGrantGrantedRepo, codeGrantGrantedRepo, importGrantGrantedModule),
		importGrantCrossModuleCall(codeGrantGrantedRepo, codeGrantOtherRepo, importGrantUngrantedModule),
	})
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runImportGrantRequest(t, graph, map[string]any{
		"query_type":  "cross_module_calls",
		"source_file": "src/api.py",
	}, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if len(graph.statements) == 0 {
		t.Fatal("no statement reached the graph")
	}
	normalized := normalizeCypherWhitespace(graph.statements[len(graph.statements)-1])
	for _, alias := range []string{"source_repo", "target_repo"} {
		if !strings.Contains(normalized, alias+".id IN $allowed_repository_ids") {
			t.Fatalf("cross-module call query does not bind %s to the grant:\n%s", alias, normalized)
		}
	}
	if strings.Contains(rec.Body.String(), codeGrantOtherRepo) {
		t.Fatalf("cross-module calls leaked the callee's repository: %s", rec.Body.String())
	}
}

// TestImportDependencyScanBoundIsSpentOnGrantedRowsOnly is the
// filter-before-limit rule (#5167 W3 P1) at this route's internal scan bound.
// The source-module path scans to $scan_limit and pages in Go, so an
// out-of-grant repository that fills the scan budget pushes a granted
// repository's rows past the bound -- here, past the 422 the overflow raises.
func TestImportDependencyScanBoundIsSpentOnGrantedRowsOnly(t *testing.T) {
	t.Parallel()

	seeds := importGrantCycleEdges()
	for i := 0; i <= importDependencyInternalScanLimit; i++ {
		seeds = append(seeds, importGrantImportEdge(
			codeGrantOtherRepo,
			fmt.Sprintf("src/other_%d.py", i),
			importGrantUngrantedModule,
			i,
		))
	}

	graph := newImportGrantGraph(seeds)
	auth := codeGrantScopedAuthContext([]string{codeGrantGrantedRepo})
	rec := runImportGrantRequest(t, graph, map[string]any{
		"query_type":  "file_import_cycles",
		"language":    "python",
		"target_file": "src/module_b.py",
	}, &auth)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; an out-of-grant repository spent the scan budget: %s", got, want, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), codeGrantGrantedRepo) {
		t.Fatalf("the granted repository's cycle was pushed past the scan bound: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), codeGrantOtherRepo) {
		t.Fatalf("scan leaked the out-of-grant repository's rows: %s", rec.Body.String())
	}
}
