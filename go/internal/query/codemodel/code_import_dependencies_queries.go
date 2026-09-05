// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"fmt"
	"strings"
)

// DirectImportRowsCypher returns import edges from one connected repository path.
func DirectImportRowsCypher(req ImportDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("-[rel:IMPORTS]->")
	writeModuleNode(&cypher, "target_module", "target_module", req.TargetModule)
	cypher.WriteString("\n")

	predicates := importRowPredicates(req, nil)
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

// PackageImportRowsCypher pages distinct logical modules rather than raw edges.
func PackageImportRowsCypher(req ImportDependencyRequest, sourceScopes []map[string]any) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("-[rel:IMPORTS]->")
	writeModuleNode(&cypher, "target_module", "target_module", req.TargetModule)
	cypher.WriteString("\n")

	predicates := importRowPredicates(req, sourceScopes)
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

// SourceModuleFilesCypher resolves bounded file membership for a source module.
func SourceModuleFilesCypher(req ImportDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeModuleNode(&cypher, "source_module", "source_module", req.SourceModule)
	cypher.WriteString("<-[:CONTAINS]-")
	writeFileNode(&cypher, "source_file", "source_file", req.SourceFile)
	cypher.WriteString("<-[:REPO_CONTAINS]-")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("\n")

	predicates := make([]string, 0, 2)
	if req.NormalizedLanguage() != "" {
		predicates = append(predicates, "(source_file.language = $language OR source_file.lang = $language)")
	}
	predicates = append(predicates, importDependencyGrantPredicates(req.Access, "repo")...)
	writeCypherPredicates(&cypher, predicates)
	cypher.WriteString(`RETURN DISTINCT repo.id as repo_id,
       repo.name as repo_name,
       source_file.path as source_path,
       source_file.relative_path as source_file,
       source_module.name as source_module
ORDER BY repo_id, source_path, source_file, source_module
LIMIT $scan_limit`)
	return cypher.String()
}

// TargetModuleFilesCypher resolves bounded file membership for a target module.
func TargetModuleFilesCypher(req ImportDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeModuleNode(&cypher, "target_module", "target_module", req.TargetModule)
	cypher.WriteString("<-[:CONTAINS]-")
	writeFileNode(&cypher, "target_file", "target_file", req.TargetFile)
	cypher.WriteString("<-[:REPO_CONTAINS]-")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("\n")

	predicates := make([]string, 0, 2)
	if req.NormalizedLanguage() != "" {
		predicates = append(predicates, "(target_file.language = $language OR target_file.lang = $language)")
	}
	predicates = append(predicates, importDependencyGrantPredicates(req.Access, "repo")...)
	writeCypherPredicates(&cypher, predicates)
	cypher.WriteString(`RETURN DISTINCT repo.id as repo_id,
       repo.name as repo_name,
       target_file.path as target_path,
       target_file.relative_path as target_file,
       target_module.name as target_module
ORDER BY repo_id, target_path, target_file, target_module
LIMIT $scan_limit`)
	return cypher.String()
}

// SourceModuleImportRowsCypher reads a bounded import-edge candidate set for
// source-module membership. Paging happens after the scan in Go.
func SourceModuleImportRowsCypher(req ImportDependencyRequest, sourceScopes []map[string]any) string {
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
	if req.NormalizedLanguage() != "" {
		predicates = append(predicates, "(source_file.language = $language OR source_file.lang = $language OR target_module.lang = $language)")
	}
	predicates = append(predicates, importDependencyGrantPredicates(req.Access, "repo")...)
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

// FileImportCycleEdgeRowsCypher returns a bounded, ordered import-edge list.
// Reciprocal cycle reconstruction happens in Go so the pinned NornicDB path
// never relies on a second MATCH or a repeated repository pattern.
func FileImportCycleEdgeRowsCypher(req ImportDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString("MATCH ")
	writeRepositoryNode(&cypher, "repo", req.RepoID)
	cypher.WriteString("-[:REPO_CONTAINS]->")
	writeFileNode(&cypher, "source_file", "source_file", "")
	cypher.WriteString("-[rel:IMPORTS]->")
	writeModuleNode(&cypher, "target_module", "target_module", "")
	cypher.WriteString("\n")
	writeCypherPredicates(&cypher, append(
		[]string{"(source_file.language = $cycle_language OR source_file.lang = $cycle_language)"},
		importDependencyGrantPredicates(req.Access, "repo")...,
	))
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

// CrossModuleCallRowsCypher returns a bounded candidate set from one connected
// call path. Cross-repository candidates are intentionally filtered in Go.
func CrossModuleCallRowsCypher(
	req ImportDependencyRequest,
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
	if req.NormalizedLanguage() != "" {
		predicates = append(predicates, "(source_file.language = $language OR source_file.lang = $language OR target_file.language = $language OR target_file.lang = $language)")
	}
	if strings.TrimSpace(req.SourceModule) != "" || len(sourceScopes) > 0 {
		predicates = append(predicates, "source_file.path IN $source_paths")
	}
	if strings.TrimSpace(req.TargetModule) != "" || len(targetScopes) > 0 {
		predicates = append(predicates, "target_file.path IN $target_paths")
	}
	// Both endpoints, independently. A caller granted only the caller's
	// repository must not learn the callee's repository identity, and the Go
	// pass that drops a mismatched pair (crossModuleCallRowMatches) runs AFTER
	// $scan_limit, so an out-of-grant callee would otherwise spend the scan
	// budget a granted repository's rows need.
	predicates = append(predicates, importDependencyGrantPredicates(req.Access, "source_repo", "target_repo")...)
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

// importDependencyGrantPredicates returns the caller's repository grant
// condition for each Repository alias the pattern binds, or nothing for an
// unscoped caller.
//
// Every builder on this route routes its predicates through
// writeCypherPredicates, which always attaches its WHERE to the single
// anchoring MATCH, so a condition added here decides row membership at the
// anchor -- ahead of SKIP/LIMIT on the paged builders and ahead of
// LIMIT $scan_limit on the ones that page in Go. That ordering is the point:
// applied after the scan bound instead, an out-of-grant repository could fill
// the 25,000-row budget and push a granted repository's rows past it
// (#5167 W3 P1 filter-before-limit).
func importDependencyGrantPredicates(access repositoryAccessFilter, aliases ...string) []string {
	if !access.Scoped() {
		return nil
	}
	predicates := make([]string, 0, len(aliases))
	for _, alias := range aliases {
		predicates = append(predicates, access.GraphCondition(alias))
	}
	return predicates
}

func importRowPredicates(req ImportDependencyRequest, sourceScopes []map[string]any) []string {
	predicates := make([]string, 0, 2)
	if req.NormalizedLanguage() != "" {
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

// ImportDependencyRequest is the decoded import-dependency investigation
// request. It split here from root code_import_dependencies.go (#6060 lane
// A L1): all seven builders take one, and Go requires a type's methods to
// live with its declaration, so the request's methods move with it. The
// *CodeHandler route methods stay in root; root's family_code_shim.go
// aliases this type back so the staying handler, executors, and tests keep
// their names. Validate, EffectiveQueryType, NormalizedLanguage, and
// QueryLimit are exported because staying root callers use them;
// hasScopeFilter and normalizedLimit serve only this package.
//
// Access carries the caller's repository grant, set by the handler from
// the request's AuthContext and never decoded from the body: the json:"-"
// tag preserves the unexported field's old decode behavior now that the
// field must be exported for staying root constructors to set it.
type ImportDependencyRequest struct {
	QueryType    string `json:"query_type"`
	RepoID       string `json:"repo_id"`
	Language     string `json:"language"`
	SourceFile   string `json:"source_file"`
	TargetFile   string `json:"target_file"`
	SourceModule string `json:"source_module"`
	TargetModule string `json:"target_module"`
	Limit        int    `json:"limit"`
	Offset       int    `json:"offset"`

	Access repositoryAccessFilter `json:"-"`
}

const (
	importDependencyDefaultLimit = 25
	importDependencyMaxLimit     = 200
	importDependencyMaxOffset    = 10000
)

// Validate rejects an unbounded or malformed investigation request before
// any graph read runs.
func (r ImportDependencyRequest) Validate() error {
	if _, ok := importDependencyQueryTypes()[r.EffectiveQueryType()]; !ok {
		return fmt.Errorf("query_type must be one of: %s", strings.Join(importDependencyQueryTypeNames(), ", "))
	}
	if r.Limit > importDependencyMaxLimit {
		return fmt.Errorf("limit must be <= 200")
	}
	if r.Limit < 0 {
		return fmt.Errorf("limit must be >= 0")
	}
	if r.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if r.Offset > importDependencyMaxOffset {
		return fmt.Errorf("offset must be <= 10000")
	}
	if !r.hasScopeFilter() {
		return fmt.Errorf("one of repo_id, source_file, target_file, source_module, or target_module is required")
	}
	if strings.TrimSpace(r.TargetFile) != "" && r.EffectiveQueryType() != "file_import_cycles" && r.EffectiveQueryType() != "cross_module_calls" {
		return fmt.Errorf("target_file is supported only for file_import_cycles and cross_module_calls")
	}
	if r.EffectiveQueryType() == "file_import_cycles" {
		language := r.NormalizedLanguage()
		if language != "" && language != "python" {
			return fmt.Errorf("file_import_cycles currently supports python module-name cycle detection")
		}
	}
	return nil
}

// EffectiveQueryType normalizes the requested investigation family,
// defaulting to the per-file import listing. It cannot be named QueryType:
// the request struct already has a QueryType field carrying the raw decoded
// value.
func (r ImportDependencyRequest) EffectiveQueryType() string {
	queryType := strings.ToLower(strings.TrimSpace(r.QueryType))
	if queryType == "" {
		return "imports_by_file"
	}
	return queryType
}

// NormalizedLanguage lowercases the requested language filter.
func (r ImportDependencyRequest) NormalizedLanguage() string {
	return strings.ToLower(strings.TrimSpace(r.Language))
}

func (r ImportDependencyRequest) normalizedLimit() int {
	switch {
	case r.Limit <= 0:
		return importDependencyDefaultLimit
	case r.Limit > importDependencyMaxLimit:
		return importDependencyMaxLimit
	default:
		return r.Limit
	}
}

// QueryLimit bounds one investigation page with a limit+1 truncation probe.
func (r ImportDependencyRequest) QueryLimit() int {
	return r.normalizedLimit() + 1
}

func (r ImportDependencyRequest) hasScopeFilter() bool {
	for _, value := range []string{r.RepoID, r.SourceFile, r.TargetFile, r.SourceModule, r.TargetModule} {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func importDependencyQueryTypes() map[string]struct{} {
	return map[string]struct{}{
		"imports_by_file":     {},
		"importers":           {},
		"module_dependencies": {},
		"package_imports":     {},
		"file_import_cycles":  {},
		"cross_module_calls":  {},
	}
}

func importDependencyQueryTypeNames() []string {
	return []string{"imports_by_file", "importers", "module_dependencies", "package_imports", "file_import_cycles", "cross_module_calls"}
}
