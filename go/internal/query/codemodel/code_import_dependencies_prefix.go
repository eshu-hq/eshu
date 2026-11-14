// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"
)

// Prefix and external-only matching for import-dependency reads (#10657).
//
// The builders here are NEW: they carry the target_module_prefix STARTS WITH
// predicate and the external_only exclusion, while the existing builders in
// code_import_dependencies_queries.go keep their exact text (their
// cypher_sha256 pins forbid relocation edits). The RETURN/ORDER/SKIP/LIMIT
// clauses below repeat the direct and package shapes verbatim so the new
// reads return the same row shape.
//
// The STARTS WITH predicate seeks the module_name_lookup RANGE index on
// Module.name instead of scanning every Module (proven by PROFILE on 30,000
// Module nodes in the issue proof: NodeIndexSeekByRange, ~27 DbHits per
// 25-row page, no AllNodesScan). No new index was needed.

// DirectImportPrefixRowsCypher returns import edges for one connected
// repository path, matching Module.name by prefix instead of the exact-match
// node DirectImportRowsCypher carries. The anchor stays on the repository
// and the grant predicates still precede the paging bound.
func DirectImportPrefixRowsCypher(req ImportDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("-[rel:IMPORTS]->")
	cypher.WriteString("(target_module:Module)")
	cypher.WriteString("\n")

	predicates := importRowPredicates(req, nil)
	predicates = append(predicates, importTargetModulePrefixPredicates(req)...)
	predicates = append(predicates, importDependencyGrantPredicates(req.Access, "repo")...)
	writeCypherPredicates(&cypher, predicates)
	cypher.WriteString(`RETURN repo.id as repo_id,
       repo.name as repo_name,
       source_file.relative_path as source_file,
       source_file.name as source_name,
       coalesce(source_file.language, source_file.lang, target_module.lang) as language,
       target_module.name as target_module,
       rel.imported_name as imported_name,
       rel.alias as alias,
       rel.line_number as line_number
ORDER BY repo.id, source_file.relative_path, target_module.name,
         coalesce(rel.line_number, 0), coalesce(rel.imported_name, ''), coalesce(rel.alias, '')
SKIP $offset
LIMIT $limit`)
	return cypher.String()
}

// PackageImportPrefixRowsCypher pages distinct logical modules matched by
// prefix rather than by exact name. Like PackageImportRowsCypher, the
// source-module variant scans a bounded candidate set (LIMIT $scan_limit,
// paged in Go) while the unscoped variant pages DISTINCT rows directly.
func PackageImportPrefixRowsCypher(req ImportDependencyRequest, sourceScopes []map[string]any) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("-[rel:IMPORTS]->")
	cypher.WriteString("(target_module:Module)")
	cypher.WriteString("\n")

	predicates := importRowPredicates(req, sourceScopes)
	predicates = append(predicates, importTargetModulePrefixPredicates(req)...)
	predicates = append(predicates, importDependencyGrantPredicates(req.Access, "repo")...)
	writeCypherPredicates(&cypher, predicates)
	if len(sourceScopes) > 0 {
		cypher.WriteString(`RETURN repo.id as repo_id,
       source_file.path as source_path,
       target_module.name as target_module,
       coalesce(source_file.language, source_file.lang, target_module.lang) as language
ORDER BY repo_id, source_path, target_module, language
LIMIT $scan_limit`)
		return cypher.String()
	}
	cypher.WriteString(`RETURN DISTINCT repo.id as repo_id,
       target_module.name as target_module,
       coalesce(source_file.language, source_file.lang, target_module.lang) as language
ORDER BY repo_id, target_module, language
SKIP $offset
LIMIT $limit`)
	return cypher.String()
}

// importTargetModulePrefixPredicates renders the Module-name predicates for
// the prefix reads: STARTS WITH when a prefix is set, and the external-only
// exclusion of dot- and slash-led specifiers (relative ./x, ../y and
// absolute /abs paths) when ExternalOnly is set. A bare specifier -- a Go
// import path, a dotted Python name, an @scope package -- passes.
func importTargetModulePrefixPredicates(req ImportDependencyRequest) []string {
	predicates := make([]string, 0, 3)
	if req.HasTargetModulePrefix() {
		predicates = append(predicates, "target_module.name STARTS WITH $target_module_prefix")
	}
	if req.ExternalOnly {
		predicates = append(predicates, "NOT target_module.name STARTS WITH '.'")
		predicates = append(predicates, "NOT target_module.name STARTS WITH '/'")
	}
	return predicates
}

// MatchTargetModulePrefixFilter reports whether a target module name passes
// the request's prefix/external filter in Go. It mirrors
// importTargetModulePrefixPredicates for the source-module scan path, whose
// pinned builder carries no Module-name predicate and pages after the scan.
func MatchTargetModulePrefixFilter(req ImportDependencyRequest, targetModule string) bool {
	if req.HasTargetModulePrefix() && !strings.HasPrefix(targetModule, req.NormalizedTargetModulePrefix()) {
		return false
	}
	if req.ExternalOnly && (strings.HasPrefix(targetModule, ".") || strings.HasPrefix(targetModule, "/")) {
		return false
	}
	return true
}
