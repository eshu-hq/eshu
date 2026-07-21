// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

const importDependencyInternalScanLimit = 25000

// directImportRowsCypher returns import edges from one connected repository path.
func directImportRowsCypher(req importDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("-[rel:IMPORTS]->")
	writeModuleNode(&cypher, "target_module", "target_module", req.TargetModule)
	cypher.WriteString("\n")

	predicates := importRowPredicates(req, nil)
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

// packageImportRowsCypher pages distinct logical modules rather than raw edges.
func packageImportRowsCypher(req importDependencyRequest, sourceScopes []map[string]any) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("-[rel:IMPORTS]->")
	writeModuleNode(&cypher, "target_module", "target_module", req.TargetModule)
	cypher.WriteString("\n")

	predicates := importRowPredicates(req, sourceScopes)
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

// sourceModuleFilesCypher resolves bounded file membership for a source module.
func sourceModuleFilesCypher(req importDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeModuleNode(&cypher, "source_module", "source_module", req.SourceModule)
	cypher.WriteString("<-[:CONTAINS]-")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("<-[:REPO_CONTAINS]-")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("\n")

	if req.normalizedLanguage() != "" {
		writeCypherPredicates(&cypher, []string{"(source_file.language = $language OR source_file.lang = $language)"})
	}
	cypher.WriteString(`RETURN DISTINCT repo.id as repo_id,
       repo.name as repo_name,
       source_file.path as source_path,
       source_file.relative_path as source_file,
       source_module.name as source_module
ORDER BY repo_id, source_path, source_file, source_module
LIMIT $scan_limit`)
	return cypher.String()
}

// targetModuleFilesCypher resolves bounded file membership for a target module.
func targetModuleFilesCypher(req importDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeModuleNode(&cypher, "target_module", "target_module", req.TargetModule)
	cypher.WriteString("<-[:CONTAINS]-")
	writeFileNode(&cypher, "target_file", "target_file", req.TargetFile)
	cypher.WriteString("<-[:REPO_CONTAINS]-")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("\n")

	if req.normalizedLanguage() != "" {
		writeCypherPredicates(&cypher, []string{"(target_file.language = $language OR target_file.lang = $language)"})
	}
	cypher.WriteString(`RETURN DISTINCT repo.id as repo_id,
       repo.name as repo_name,
       target_file.path as target_path,
       target_file.relative_path as target_file,
       target_module.name as target_module
ORDER BY repo_id, target_path, target_file, target_module
LIMIT $scan_limit`)
	return cypher.String()
}

// sourceModuleImportRowsCypher reads a bounded import-edge candidate set for
// source-module membership. Paging happens after the scan in Go.
func sourceModuleImportRowsCypher(req importDependencyRequest, sourceScopes []map[string]any) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("-[rel:IMPORTS]->")
	writeModuleNode(&cypher, "target_module", "target_module", req.TargetModule)
	cypher.WriteString("\n")

	_ = sourceScopes
	predicates := []string{"source_file.path IN $source_paths"}
	if req.normalizedLanguage() != "" {
		predicates = append(predicates, "(source_file.language = $language OR source_file.lang = $language OR target_module.lang = $language)")
	}
	writeCypherPredicates(&cypher, predicates)
	cypher.WriteString(`RETURN repo.id as repo_id,
       repo.name as repo_name,
       source_file.path as source_path,
       source_file.relative_path as source_file,
       source_file.name as source_name,
       coalesce(source_file.language, source_file.lang, target_module.lang) as language,
       target_module.name as target_module,
       rel.imported_name as imported_name,
       rel.alias as alias,
       rel.line_number as line_number
ORDER BY repo.id, source_file.path, source_file.relative_path, target_module.name,
         coalesce(rel.line_number, 0), coalesce(rel.imported_name, ''), coalesce(rel.alias, '')
LIMIT $scan_limit`)
	return cypher.String()
}

// fileImportCycleEdgeRowsCypher returns a bounded, ordered import-edge list.
// Reciprocal cycle reconstruction happens in Go so the pinned NornicDB path
// never relies on a second MATCH or a repeated repository pattern.
func fileImportCycleEdgeRowsCypher(req importDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", "")
	cypher.WriteString("-[rel:IMPORTS]->")
	writeModuleNode(&cypher, "target_module", "target_module", "")
	cypher.WriteString("\n")
	writeCypherPredicates(&cypher, []string{
		"(source_file.language = $cycle_language OR source_file.lang = $cycle_language)",
	})
	cypher.WriteString(`RETURN repo.id as repo_id,
       repo.name as repo_name,
       source_file.path as source_path,
       source_file.relative_path as source_file,
       source_file.name as source_name,
       coalesce(source_file.language, source_file.lang) as language,
       target_module.name as target_module,
       rel.line_number as line_number
ORDER BY repo.id, source_file.relative_path, target_module.name,
         coalesce(rel.line_number, 0), source_file.path
LIMIT $scan_limit`)
	return cypher.String()
}

// crossModuleCallRowsCypher returns a bounded candidate set from one connected
// call path. Cross-repository candidates are intentionally filtered in Go.
func crossModuleCallRowsCypher(
	req importDependencyRequest,
	sourceScopes []map[string]any,
	targetScopes []map[string]any,
) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "source_repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("-[:CONTAINS]->(caller:Function)-[rel:CALLS]->(callee:Function)<-[:CONTAINS]-")
	writeFileNode(&cypher, "target_file", "target_file", req.TargetFile)
	cypher.WriteString("<-[:REPO_CONTAINS]-")
	writeRepositoryNode(&cypher, "target_repo", req.RepoID)
	cypher.WriteString("\n")

	predicates := make([]string, 0, 3)
	if req.normalizedLanguage() != "" {
		predicates = append(predicates, "(source_file.language = $language OR source_file.lang = $language OR target_file.language = $language OR target_file.lang = $language)")
	}
	if strings.TrimSpace(req.SourceModule) != "" || len(sourceScopes) > 0 {
		predicates = append(predicates, "source_file.path IN $source_paths")
	}
	if strings.TrimSpace(req.TargetModule) != "" || len(targetScopes) > 0 {
		predicates = append(predicates, "target_file.path IN $target_paths")
	}
	writeCypherPredicates(&cypher, predicates)

	cypher.WriteString(`RETURN source_repo.id as source_repo_id,
       source_repo.name as repo_name,
       target_repo.id as target_repo_id,
       source_file.path as source_path,
       target_file.path as target_path,
       source_file.relative_path as source_file,
       target_file.relative_path as target_file,
       coalesce(source_file.language, source_file.lang) as source_language,
       coalesce(target_file.language, target_file.lang) as target_language,
       caller.name as source_name,
       coalesce(caller.id, caller.uid) as source_id,
       callee.name as target_name,
       coalesce(callee.id, callee.uid) as target_id,
       rel.call_kind as call_kind,
       rel.reason as reason`)
	if strings.TrimSpace(req.SourceModule) != "" {
		cypher.WriteString(",\n       $source_module as source_module")
	}
	if strings.TrimSpace(req.TargetModule) != "" {
		cypher.WriteString(",\n       $target_module as target_module")
	}
	cypher.WriteString(`
ORDER BY source_repo.id, source_file.relative_path,
         coalesce(caller.id, caller.uid), target_repo.id, target_file.relative_path,
         coalesce(callee.id, callee.uid), coalesce(rel.call_kind, ''), coalesce(rel.reason, '')
LIMIT $scan_limit`)
	return cypher.String()
}

func importRowPredicates(req importDependencyRequest, sourceScopes []map[string]any) []string {
	predicates := make([]string, 0, 2)
	if req.normalizedLanguage() != "" {
		predicates = append(predicates, "(source_file.language = $language OR source_file.lang = $language OR target_module.lang = $language)")
	}
	if strings.TrimSpace(req.SourceModule) != "" || len(sourceScopes) > 0 {
		predicates = append(predicates, "source_file.path IN $source_paths")
	}
	return predicates
}

func writeCypherPredicates(cypher *strings.Builder, predicates []string) {
	if len(predicates) == 0 {
		return
	}
	cypher.WriteString("WHERE ")
	cypher.WriteString(strings.Join(predicates, " AND "))
	cypher.WriteString("\n")
}

func writeRepositoryNode(cypher *strings.Builder, variable, repoID string) {
	cypher.WriteString("(")
	cypher.WriteString(variable)
	cypher.WriteString(":Repository")
	if strings.TrimSpace(repoID) != "" {
		cypher.WriteString(" {id: $repo_id}")
	}
	cypher.WriteString(")")
}

func writeFileNode(cypher *strings.Builder, variable, parameter, file string) {
	cypher.WriteString("(")
	cypher.WriteString(variable)
	cypher.WriteString(":File")
	if strings.TrimSpace(file) != "" {
		cypher.WriteString(" {relative_path: $")
		cypher.WriteString(parameter)
		cypher.WriteString("}")
	}
	cypher.WriteString(")")
}

func writeModuleNode(cypher *strings.Builder, variable, parameter, module string) {
	cypher.WriteString("(")
	cypher.WriteString(variable)
	cypher.WriteString(":Module")
	if strings.TrimSpace(module) != "" {
		cypher.WriteString(" {name: $")
		cypher.WriteString(parameter)
		cypher.WriteString("}")
	}
	cypher.WriteString(")")
}
