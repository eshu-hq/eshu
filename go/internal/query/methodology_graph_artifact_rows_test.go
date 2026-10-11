// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build queryplan_profile_live

package query

import (
	"fmt"
	"sort"
	"strings"
)

// methodologyStatementIDs projects only the columns that identify physical
// fixture rows. It preserves order and duplicate import edges.
func methodologyStatementIDs(entry string, rows []map[string]any) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		value := func(name string) any { return row[name] }
		switch entry {
		case "QP-CODE-IMPORT-SOURCE-MODULE-FILES":
			ids = append(ids, fmt.Sprintf("%v|%v|%v", value("repo_id"), value("source_path"), value("source_module")))
		case "QP-CODE-IMPORT-TARGET-MODULE-FILES":
			ids = append(ids, fmt.Sprintf("%v|%v|%v", value("repo_id"), value("target_path"), value("target_module")))
		case "QP-CODE-IMPORT-CROSS-MODULE-CALLS":
			ids = append(ids, fmt.Sprintf("%v|%v|%v|%v|%v|%v", value("source_repo_id"), value("source_path"), value("target_repo_id"), value("target_path"), value("source_id"), value("target_id")))
		case "QP-CODE-IMPORT-PACKAGES":
			if path, ok := row["source_path"]; ok {
				ids = append(ids, fmt.Sprintf("%v|%v|%v|%v", value("repo_id"), path, value("target_module"), value("language")))
			} else {
				ids = append(ids, fmt.Sprintf("%v|%v|%v", value("repo_id"), value("target_module"), value("language")))
			}
		default:
			ids = append(ids, fmt.Sprintf("%v|%v|%v|%v", value("repo_id"), value("source_file"), value("target_module"), value("line_number")))
		}
	}
	return ids
}

// methodologyOracleStatementIDs derives the raw statement answer from the
// fixture ledger and parameters. This code does not inspect production query
// text, result rows, or a query shaper.
func methodologyOracleStatementIDs(entry string, params map[string]any) []string {
	if entry == "QP-CODE-IMPORT-SOURCE-MODULE-FILES" || entry == "QP-CODE-IMPORT-TARGET-MODULE-FILES" {
		return methodologyOracleMembershipIDs(entry, params)
	}
	if entry == "QP-CODE-IMPORT-CROSS-MODULE-CALLS" {
		return methodologyOracleCallIDs(params)
	}
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
	ids := make([]string, 0, len(edges))
	seen := make(map[string]struct{})
	for _, edge := range edges {
		if !methodologyOracleAllowsRepo(params, edge.repo) || !methodologyOracleParamEquals(params, "language", "python") {
			continue
		}
		// The cycle statement fetches a bounded edge ledger. Source/target
		// direction filters apply during Go enumeration after the fetch.
		if entry != "QP-CODE-IMPORT-CYCLE-EDGES" &&
			(!methodologyOracleParamEquals(params, "source_file", edge.source) ||
				!methodologyOracleParamEquals(params, "target_module", edge.target) ||
				!methodologyOracleParamContains(params, "source_paths", methodologyOracleFilePath(edge.repo, edge.source))) {
			continue
		}
		switch entry {
		case "QP-CODE-IMPORT-PACKAGES":
			if _, scoped := params["source_paths"]; scoped {
				ids = append(ids, fmt.Sprintf("%s|%s|%s|python", edge.repo, methodologyOracleFilePath(edge.repo, edge.source), edge.target))
				continue
			}
			id := edge.repo + "|" + edge.target + "|python"
			if _, exists := seen[id]; !exists {
				ids = append(ids, id)
				seen[id] = struct{}{}
			}
		default:
			ids = append(ids, fmt.Sprintf("%s|%s|%s|%d", edge.repo, edge.source, edge.target, edge.line))
		}
	}
	if entry == "QP-CODE-IMPORT-PACKAGES" {
		sort.Strings(ids)
	}
	return methodologyOracleWindow(ids, entry, params)
}

func methodologyOracleMembershipIDs(entry string, params map[string]any) []string {
	var ids []string
	for _, repo := range []string{"other-repository", "proof-repository"} {
		if !methodologyOracleAllowsRepo(params, repo) || !methodologyOracleParamEquals(params, "language", "python") {
			continue
		}
		if entry == "QP-CODE-IMPORT-SOURCE-MODULE-FILES" {
			if methodologyOracleParamEquals(params, "source_module", "proof.source") && methodologyOracleParamEquals(params, "source_file", "src/proof.py") {
				ids = append(ids, repo+"|"+methodologyOracleFilePath(repo, "src/proof.py")+"|proof.source")
			}
		} else if methodologyOracleParamEquals(params, "target_module", "proof.target") && methodologyOracleParamEquals(params, "target_file", "src/target.py") {
			ids = append(ids, repo+"|"+methodologyOracleFilePath(repo, "src/target.py")+"|proof.target")
		}
	}
	return methodologyOracleWindow(ids, entry, params)
}

func methodologyOracleCallIDs(params map[string]any) []string {
	var ids []string
	for _, call := range []struct{ sourceRepo, sourcePath, targetRepo, targetPath, sourceID, targetID string }{
		{"other-repository", "src/proof.py", "proof-repository", "src/target.py", "fn-other", "fn-target"},
		{"proof-repository", "src/proof.py", "proof-repository", "src/target.py", "fn-proof", "fn-target"},
	} {
		if !methodologyOracleAllowsRepo(params, call.sourceRepo) || !methodologyOracleAllowsRepo(params, call.targetRepo) ||
			!methodologyOracleParamEquals(params, "source_file", call.sourcePath) || !methodologyOracleParamEquals(params, "target_file", call.targetPath) ||
			!methodologyOracleParamEquals(params, "language", "python") ||
			!methodologyOracleParamContains(params, "source_paths", methodologyOracleFilePath(call.sourceRepo, call.sourcePath)) ||
			!methodologyOracleParamContains(params, "target_paths", methodologyOracleFilePath(call.targetRepo, call.targetPath)) {
			continue
		}
		ids = append(ids, fmt.Sprintf("%s|%s|%s|%s|%s|%s", call.sourceRepo, methodologyOracleFilePath(call.sourceRepo, call.sourcePath), call.targetRepo, methodologyOracleFilePath(call.targetRepo, call.targetPath), call.sourceID, call.targetID))
	}
	return methodologyOracleWindow(ids, "QP-CODE-IMPORT-CROSS-MODULE-CALLS", params)
}

func methodologyOracleFilePath(repo, relative string) string {
	return "/" + strings.TrimSuffix(repo, "-repository") + "/" + relative
}

func methodologyOracleAllowsRepo(params map[string]any, repo string) bool {
	if !methodologyOracleParamEquals(params, "repo_id", repo) {
		return false
	}
	_, scoped := params["allowed_repository_ids"]
	if !scoped {
		return true
	}
	return methodologyOracleParamContains(params, "allowed_repository_ids", repo) || methodologyOracleParamContains(params, "allowed_scope_ids", strings.TrimSuffix(repo, "-repository")+"-scope")
}

func methodologyOracleParamEquals(params map[string]any, key, want string) bool {
	value, exists := params[key]
	return !exists || value == want
}

func methodologyOracleParamContains(params map[string]any, key, want string) bool {
	value, exists := params[key]
	if !exists {
		return true
	}
	list, ok := value.([]string)
	if !ok {
		return false
	}
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func methodologyOracleWindow(ids []string, entry string, params map[string]any) []string {
	limitKey := "scan_limit"
	if entry == "QP-CODE-IMPORT-ROWS-REPOSITORY" || entry == "QP-CODE-IMPORT-PACKAGES" {
		if _, scanned := params["scan_limit"]; !scanned {
			limitKey = "limit"
		}
	}
	if limitKey == "limit" {
		if offset, ok := params["offset"].(int); ok && offset < len(ids) {
			ids = ids[offset:]
		} else if ok {
			return nil
		}
	}
	if limit, ok := params[limitKey].(int); ok && limit < len(ids) {
		return ids[:limit]
	}
	return ids
}
