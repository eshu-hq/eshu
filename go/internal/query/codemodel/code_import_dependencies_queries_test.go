// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func importQueriesTestRequest() ImportDependencyRequest {
	return ImportDependencyRequest{
		RepoID:   "repo-1",
		Language: "go",
		Access:   querycontract.RepositoryAccessFilter{AllScopes: true},
	}
}

// TestDirectImportRowsCypherShape proves the direct import-edge builder
// anchors on the repository, projects the paged import columns, and leaves
// grant predicates out for an unscoped caller.
func TestDirectImportRowsCypherShape(t *testing.T) {
	t.Parallel()

	cypher := DirectImportRowsCypher(importQueriesTestRequest())

	for _, want := range []string{
		"MATCH (repo:Repository {id: $repo_id})",
		"-[rel:IMPORTS]->",
		"SKIP $offset\nLIMIT $limit",
		"(source_file.language = $language OR source_file.lang = $language OR target_module.lang = $language)",
	} {
		if !strings.Contains(cypher, want) {
			t.Errorf("direct import cypher missing %q:\n%s", want, cypher)
		}
	}
}

// TestDirectImportRowsCypherScopedGrant proves a scoped caller's grant
// predicate is routed into the statement ahead of the scan bound.
func TestDirectImportRowsCypherScopedGrant(t *testing.T) {
	t.Parallel()

	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-1"}}
	req := importQueriesTestRequest()
	req.Access = access
	cypher := DirectImportRowsCypher(req)

	if !strings.Contains(cypher, access.GraphCondition("repo")) {
		t.Errorf("direct import cypher missing the repo grant predicate:\n%s", cypher)
	}
	where := strings.Index(cypher, "WHERE ")
	skip := strings.Index(cypher, "SKIP $offset")
	if where < 0 || skip < 0 || where > skip {
		t.Errorf("grant WHERE must precede the paging bound:\n%s", cypher)
	}
}

// TestPackageImportRowsCypherScanShape proves the scoped package scan
// returns the scan-bound shape while the unscoped page keeps SKIP/LIMIT.
func TestPackageImportRowsCypherScanShape(t *testing.T) {
	t.Parallel()

	scopes := []map[string]any{{"repo_id": "repo-1", "path": "a.go"}}
	scoped := PackageImportRowsCypher(importQueriesTestRequest(), scopes)
	if !strings.Contains(scoped, "LIMIT $scan_limit") {
		t.Errorf("scoped package cypher missing the scan bound:\n%s", scoped)
	}
	if strings.Contains(scoped, "SKIP $offset") {
		t.Errorf("scoped package cypher must not page:\n%s", scoped)
	}

	paged := PackageImportRowsCypher(importQueriesTestRequest(), nil)
	for _, want := range []string{"SKIP $offset\nLIMIT $limit", "RETURN DISTINCT"} {
		if !strings.Contains(paged, want) {
			t.Errorf("paged package cypher missing %q:\n%s", want, paged)
		}
	}
}

// TestModuleFilesCypherLanguageFilter proves both module-membership
// builders add the language predicate only when a filter is set.
func TestModuleFilesCypherLanguageFilter(t *testing.T) {
	t.Parallel()

	withLang := SourceModuleFilesCypher(importQueriesTestRequest())
	if !strings.Contains(withLang, "(source_file.language = $language OR source_file.lang = $language)") {
		t.Errorf("source-module cypher missing the language predicate:\n%s", withLang)
	}
	target := TargetModuleFilesCypher(importQueriesTestRequest())
	if !strings.Contains(target, "(target_file.language = $language OR target_file.lang = $language)") {
		t.Errorf("target-module cypher missing the language predicate:\n%s", target)
	}

	req := importQueriesTestRequest()
	req.Language = ""
	if got := SourceModuleFilesCypher(req); strings.Contains(got, "$language") {
		t.Errorf("unfiltered source-module cypher must not mention $language:\n%s", got)
	}
	if got := TargetModuleFilesCypher(req); strings.Contains(got, "$language") {
		t.Errorf("unfiltered target-module cypher must not mention $language:\n%s", got)
	}
}

// TestSourceModuleImportRowsCypherShape proves the membership candidate
// fetch constrains on the scope paths and carries the scan bound.
func TestSourceModuleImportRowsCypherShape(t *testing.T) {
	t.Parallel()

	cypher := SourceModuleImportRowsCypher(importQueriesTestRequest(), nil)
	for _, want := range []string{"source_file.path IN $source_paths", "LIMIT $scan_limit"} {
		if !strings.Contains(cypher, want) {
			t.Errorf("source-module import cypher missing %q:\n%s", want, cypher)
		}
	}
}

// TestFileImportCycleEdgeRowsCypherShape proves the cycle edge fetch
// filters on the cycle language and stays scan-bounded.
func TestFileImportCycleEdgeRowsCypherShape(t *testing.T) {
	t.Parallel()

	cypher := FileImportCycleEdgeRowsCypher(importQueriesTestRequest())
	for _, want := range []string{
		"$cycle_language",
		"LIMIT $scan_limit",
		"rel.source_package as source_package",
	} {
		if !strings.Contains(cypher, want) {
			t.Errorf("cycle edge cypher missing %q:\n%s", want, cypher)
		}
	}
}

// TestCrossModuleCallRowsCypherModules proves the call-edge builder
// projects module anchors only when the request names modules, and
// grants both endpoints independently for a scoped caller.
func TestCrossModuleCallRowsCypherModules(t *testing.T) {
	t.Parallel()

	req := importQueriesTestRequest()
	req.SourceModule = "a"
	req.TargetModule = "b"
	withModules := CrossModuleCallRowsCypher(req, nil, nil)
	for _, want := range []string{"$source_module as source_module", "$target_module as target_module"} {
		if !strings.Contains(withModules, want) {
			t.Errorf("module-anchored call cypher missing %q:\n%s", want, withModules)
		}
	}

	plain := CrossModuleCallRowsCypher(importQueriesTestRequest(), nil, nil)
	for _, unwanted := range []string{"$source_module as source_module", "$target_module as target_module"} {
		if strings.Contains(plain, unwanted) {
			t.Errorf("unanchored call cypher must not project %q:\n%s", unwanted, plain)
		}
	}

	access := querycontract.RepositoryAccessFilter{AllowedRepositoryIDs: []string{"repo-1"}}
	scopedReq := importQueriesTestRequest()
	scopedReq.Access = access
	scoped := CrossModuleCallRowsCypher(scopedReq, nil, nil)
	for _, alias := range []string{"source_repo", "target_repo"} {
		if !strings.Contains(scoped, access.GraphCondition(alias)) {
			t.Errorf("scoped call cypher missing the %s grant predicate:\n%s", alias, scoped)
		}
	}
}

// TestWriteCypherPredicatesEmpty proves an empty predicate list leaves
// the statement without a WHERE clause.
func TestWriteCypherPredicatesEmpty(t *testing.T) {
	t.Parallel()

	var cypher strings.Builder
	cypher.WriteString("MATCH (n)")
	writeCypherPredicates(&cypher, nil)
	if strings.Contains(cypher.String(), "WHERE") {
		t.Errorf("empty predicates must not emit WHERE: %q", cypher.String())
	}
}
