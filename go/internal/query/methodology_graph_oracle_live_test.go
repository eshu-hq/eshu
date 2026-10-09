// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"fmt"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

// The oracle ledger describes fixture edges and expected public identities.
// It does not inspect produced Cypher, result rows, or the query shapers.
type methodologyOracleImport struct {
	repo   string
	source string
	target string
	line   int
}

func methodologyFixtureImports() []methodologyOracleImport {
	edges := []methodologyOracleImport{
		{"proof-repository", "src/proof.py", "proof.target", 1},
		{"proof-repository", "src/proof.py", "proof.target", 3},
		{"proof-repository", "src/proof.py", "proof.target", 4},
		{"proof-repository", "src/target.py", "proof.source", 2},
		{"other-repository", "src/proof.py", "proof.target", 1},
		{"other-repository", "src/target.py", "proof.source", 2},
	}
	for i := 1; i <= 128; i++ {
		edges = append(edges, methodologyOracleImport{"other-repository", fmt.Sprintf("src/skew-%d.py", i), "proof.target", i})
	}
	return edges
}

func methodologyExpectedIdentityPage(request codemodel.ImportDependencyRequest) ([]string, bool) {
	if request.Access.Empty() {
		return nil, false
	}
	var ids []string
	switch request.EffectiveQueryType() {
	case "file_import_cycles":
		for _, repo := range []string{"other-repository", "proof-repository"} {
			if methodologyOracleRepoAllowed(request, repo) {
				ids = append(ids, repo+"|src/proof.py|src/target.py|2")
			}
		}
	case "cross_module_calls":
		if methodologyOracleRepoAllowed(request, "proof-repository") {
			ids = append(ids, "proof-repository|src/proof.py|src/target.py|fn-proof|fn-target")
		}
	default:
		edges := methodologyFixtureImports()
		sort.Slice(edges, func(i, j int) bool {
			a, b := edges[i], edges[j]
			if a.repo != b.repo {
				return a.repo < b.repo
			}
			if a.source != b.source {
				return a.source < b.source
			}
			if a.target != b.target {
				return a.target < b.target
			}
			return a.line < b.line
		})
		seen := make(map[string]struct{})
		for _, edge := range edges {
			if !methodologyOracleRepoAllowed(request, edge.repo) {
				continue
			}
			if request.SourceFile != "" && request.SourceFile != edge.source {
				continue
			}
			if request.SourceModule != "" && edge.source != "src/proof.py" {
				continue
			}
			if request.TargetModule != "" && request.TargetModule != edge.target {
				continue
			}
			if request.EffectiveQueryType() == "package_imports" {
				key := edge.repo + "|" + edge.target + "|python"
				if _, exists := seen[key]; !exists {
					ids = append(ids, key)
					seen[key] = struct{}{}
				}
			} else {
				ids = append(ids, fmt.Sprintf("%s|%s|%s|%d", edge.repo, edge.source, edge.target, edge.line))
			}
		}
		if request.EffectiveQueryType() == "package_imports" {
			sort.Strings(ids)
		}
	}
	offset := request.Offset
	if offset >= len(ids) {
		return nil, false
	}
	limit := request.Limit
	if limit == 0 {
		limit = 25
	}
	end := offset + limit
	hasMore := end < len(ids)
	if end > len(ids) {
		end = len(ids)
	}
	return ids[offset:end], hasMore
}

func methodologyOracleRepoAllowed(request codemodel.ImportDependencyRequest, repo string) bool {
	if request.RepoID != "" && request.RepoID != repo {
		return false
	}
	if request.Access.Scoped() && repo != "proof-repository" {
		return false
	}
	return true
}

func methodologyResponseIdentities(t *testing.T, request codemodel.ImportDependencyRequest, response map[string]any) []string {
	t.Helper()
	key := "dependencies"
	switch request.EffectiveQueryType() {
	case "file_import_cycles":
		key = "cycles"
	case "cross_module_calls":
		key = "cross_module_calls"
	case "package_imports":
		key = "modules"
	}
	rows, ok := response[key].([]map[string]any)
	if !ok {
		t.Fatalf("%s response rows type=%T", key, response[key])
	}
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		switch request.EffectiveQueryType() {
		case "file_import_cycles":
			ids = append(ids, fmt.Sprintf("%v|%v|%v|%v", row["repo_id"], row["source_file"], row["target_file"], row["cycle_length"]))
		case "cross_module_calls":
			ids = append(ids, fmt.Sprintf("%v|%v|%v|%v|%v", row["repo_id"], row["source_file"], row["target_file"], row["source_id"], row["target_id"]))
		case "package_imports":
			ids = append(ids, fmt.Sprintf("%v|%v|%v", row["repo_id"], row["module"], row["language"]))
		default:
			ids = append(ids, fmt.Sprintf("%v|%v|%v|%v", row["repo_id"], row["source_file"], row["target_module"], row["line_number"]))
		}
	}
	return ids
}
