package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const (
	importDependencyCapability   = "symbol_graph.import_dependencies"
	importDependencyDefaultLimit = 25
	importDependencyMaxLimit     = 200
	importDependencyMaxOffset    = 10000
)

var errImportDependencyUnavailable = errors.New("import dependency graph is unavailable")

type importDependencyRequest struct {
	QueryType    string `json:"query_type"`
	RepoID       string `json:"repo_id"`
	Language     string `json:"language"`
	SourceFile   string `json:"source_file"`
	TargetFile   string `json:"target_file"`
	SourceModule string `json:"source_module"`
	TargetModule string `json:"target_module"`
	Limit        int    `json:"limit"`
	Offset       int    `json:"offset"`
}

func (h *CodeHandler) handleImportDependencyInvestigation(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryImportDependencyInvestigation,
		"POST /api/v0/code/imports/investigate",
		importDependencyCapability,
	)
	defer span.End()

	var req importDependencyRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), importDependencyCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"import dependency investigation requires a supported query profile",
			ErrorCodeUnsupportedCapability,
			importDependencyCapability,
			h.profile(),
			requiredProfile(importDependencyCapability),
		)
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}

	data, err := h.importDependencyData(r.Context(), req)
	if err != nil {
		if errors.Is(err, errImportDependencyUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), importDependencyCapability, TruthBasisAuthoritativeGraph, "resolved from bounded graph import dependency lookup"),
	)
}

func (r importDependencyRequest) validate() error {
	if _, ok := importDependencyQueryTypes()[r.queryType()]; !ok {
		return fmt.Errorf("query_type must be one of: %s", strings.Join(importDependencyQueryTypeNames(), ", "))
	}
	if r.Limit > importDependencyMaxLimit {
		return fmt.Errorf("limit must be <= 200")
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
	if r.queryType() == "file_import_cycles" {
		language := r.normalizedLanguage()
		if language != "" && language != "python" {
			return fmt.Errorf("file_import_cycles currently supports python module-name cycle detection")
		}
	}
	return nil
}

func (r importDependencyRequest) queryType() string {
	queryType := strings.ToLower(strings.TrimSpace(r.QueryType))
	if queryType == "" {
		return "imports_by_file"
	}
	return queryType
}

func (r importDependencyRequest) normalizedLanguage() string {
	return strings.ToLower(strings.TrimSpace(r.Language))
}

func (r importDependencyRequest) normalizedLimit() int {
	switch {
	case r.Limit <= 0:
		return importDependencyDefaultLimit
	case r.Limit > importDependencyMaxLimit:
		return importDependencyMaxLimit
	default:
		return r.Limit
	}
}

func (r importDependencyRequest) queryLimit() int {
	return r.normalizedLimit() + 1
}

func (r importDependencyRequest) hasScopeFilter() bool {
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

func (h *CodeHandler) importDependencyData(ctx context.Context, req importDependencyRequest) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errImportDependencyUnavailable
	}
	cypher, params := importDependencyCypher(req)
	rows, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	return importDependencyResponse(req, rows), nil
}

func importDependencyCypher(req importDependencyRequest) (string, map[string]any) {
	params := importDependencyParams(req)
	switch req.queryType() {
	case "cross_module_calls":
		return crossModuleCallsCypher(req), params
	case "file_import_cycles":
		return fileImportCyclesCypher(req), params
	default:
		return importRowsCypher(req), params
	}
}

func importDependencyParams(req importDependencyRequest) map[string]any {
	params := map[string]any{
		"limit":  req.queryLimit(),
		"offset": req.Offset,
	}
	if repoID := strings.TrimSpace(req.RepoID); repoID != "" {
		params["repo_id"] = repoID
	}
	if language := req.normalizedLanguage(); language != "" {
		params["language"] = language
	}
	if sourceFile := strings.TrimSpace(req.SourceFile); sourceFile != "" {
		params["source_file"] = sourceFile
	}
	if targetFile := strings.TrimSpace(req.TargetFile); targetFile != "" {
		params["target_file"] = targetFile
	}
	if sourceModule := strings.TrimSpace(req.SourceModule); sourceModule != "" {
		params["source_module"] = sourceModule
	}
	if targetModule := strings.TrimSpace(req.TargetModule); targetModule != "" {
		params["target_module"] = targetModule
	}
	return params
}

func importRowsCypher(req importDependencyRequest) string {
	var cypher strings.Builder
	switch {
	case strings.TrimSpace(req.RepoID) != "" && strings.TrimSpace(req.SourceFile) != "":
		cypher.WriteString("MATCH (repo:Repository {id: $repo_id})-[:REPO_CONTAINS]->(source_file:File {relative_path: $source_file})\n")
	case strings.TrimSpace(req.SourceFile) != "":
		cypher.WriteString("MATCH (source_file:File {relative_path: $source_file})\n")
		cypher.WriteString("MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file)\n")
	case strings.TrimSpace(req.TargetModule) != "":
		cypher.WriteString("MATCH (source_file:File)-[rel:IMPORTS]->(target_module:Module {name: $target_module})\n")
		cypher.WriteString("MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file)\n")
	case strings.TrimSpace(req.SourceModule) != "":
		cypher.WriteString("MATCH (source_file)-[:CONTAINS]->(source_module:Module {name: $source_module})\n")
		cypher.WriteString("MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file)\n")
	default:
		cypher.WriteString("MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file:File)\n")
	}
	if strings.TrimSpace(req.TargetModule) == "" {
		cypher.WriteString("MATCH (source_file)-[rel:IMPORTS]->(target_module:Module)\n")
	}
	if strings.TrimSpace(req.SourceModule) != "" && !strings.Contains(cypher.String(), "source_module:Module") {
		cypher.WriteString("MATCH (source_file)-[:CONTAINS]->(source_module:Module {name: $source_module})\n")
	}
	where := importDependencyImportPredicates(req)
	if len(where) > 0 {
		cypher.WriteString("WHERE ")
		cypher.WriteString(strings.Join(where, " AND "))
		cypher.WriteString("\n")
	}
	cypher.WriteString(`RETURN repo.id as repo_id,
       source_file.relative_path as source_file,
       source_file.name as source_name,
       coalesce(source_file.language, target_module.lang) as language,
       target_module.name as target_module,
       rel.imported_name as imported_name,
       rel.alias as alias,
       rel.line_number as line_number
ORDER BY source_file.relative_path, target_module.name, rel.line_number
SKIP $offset
LIMIT $limit`)
	return cypher.String()
}

func importDependencyImportPredicates(req importDependencyRequest) []string {
	predicates := make([]string, 0, 5)
	if strings.TrimSpace(req.RepoID) != "" && strings.TrimSpace(req.SourceFile) == "" {
		predicates = append(predicates, "repo.id = $repo_id")
	}
	if strings.TrimSpace(req.SourceFile) != "" && strings.TrimSpace(req.RepoID) == "" {
		predicates = append(predicates, "source_file.relative_path = $source_file")
	}
	if language := req.normalizedLanguage(); language != "" {
		predicates = append(predicates, "(source_file.language = $language OR target_module.lang = $language)")
	}
	if strings.TrimSpace(req.TargetModule) != "" {
		predicates = append(predicates, "target_module.name = $target_module")
	}
	return predicates
}

func fileImportCyclesCypher(req importDependencyRequest) string {
	var cypher strings.Builder
	cypher.WriteString(`MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file:File)-[source_import:IMPORTS]->(target_module:Module)
MATCH (repo)-[:REPO_CONTAINS]->(target_file:File)-[target_import:IMPORTS]->(source_module:Module)
WHERE source_file.name = source_module.name + '.py'
  AND target_file.name = target_module.name + '.py'
  AND source_file.relative_path < target_file.relative_path`)
	if strings.TrimSpace(req.RepoID) != "" {
		cypher.WriteString("\n  AND repo.id = $repo_id")
	}
	if language := req.normalizedLanguage(); language != "" {
		_ = language
		cypher.WriteString("\n  AND source_file.language = $language AND target_file.language = $language")
	}
	if strings.TrimSpace(req.SourceFile) != "" {
		cypher.WriteString("\n  AND source_file.relative_path = $source_file")
	}
	if strings.TrimSpace(req.TargetFile) != "" {
		cypher.WriteString("\n  AND target_file.relative_path = $target_file")
	}
	if strings.TrimSpace(req.SourceModule) != "" {
		cypher.WriteString("\n  AND source_module.name = $source_module")
	}
	if strings.TrimSpace(req.TargetModule) != "" {
		cypher.WriteString("\n  AND target_module.name = $target_module")
	}
	cypher.WriteString(`
RETURN repo.id as repo_id,
       source_file.relative_path as source_file,
       target_file.relative_path as target_file,
       source_module.name as source_module,
       target_module.name as target_module,
       source_import.line_number as source_line_number,
       target_import.line_number as back_edge_line_number
ORDER BY source_file.relative_path, target_file.relative_path, source_module.name, target_module.name
SKIP $offset
LIMIT $limit`)
	return cypher.String()
}

func crossModuleCallsCypher(req importDependencyRequest) string {
	var cypher strings.Builder
	if strings.TrimSpace(req.SourceModule) != "" {
		cypher.WriteString("MATCH (source_file)-[:CONTAINS]->(source_module:Module {name: $source_module})\n")
	}
	if strings.TrimSpace(req.TargetModule) != "" {
		cypher.WriteString("MATCH (target_file)-[:CONTAINS]->(target_module:Module {name: $target_module})\n")
	}
	cypher.WriteString(`MATCH (source_file)-[:CONTAINS]->(caller:Function)
MATCH (caller)-[rel:CALLS]->(callee:Function)
MATCH (target_file)-[:CONTAINS]->(callee)
MATCH (repo:Repository)-[:REPO_CONTAINS]->(source_file)
MATCH (repo)-[:REPO_CONTAINS]->(target_file)
`)
	where := crossModuleCallPredicates(req)
	if len(where) > 0 {
		cypher.WriteString("WHERE ")
		cypher.WriteString(strings.Join(where, " AND "))
		cypher.WriteString("\n")
	}
	cypher.WriteString(`RETURN repo.id as repo_id,
       source_file.relative_path as source_file,
       target_file.relative_path as target_file,
       caller.name as source_name,
       coalesce(caller.id, caller.uid) as source_id,
       callee.name as target_name,
       coalesce(callee.id, callee.uid) as target_id,
       rel.call_kind as call_kind,
       rel.reason as reason`)
	if strings.TrimSpace(req.SourceModule) != "" {
		cypher.WriteString(",\n       source_module.name as source_module")
	}
	if strings.TrimSpace(req.TargetModule) != "" {
		cypher.WriteString(",\n       target_module.name as target_module")
	}
	cypher.WriteString(`
ORDER BY source_file.relative_path, caller.start_line, caller.name, target_file.relative_path, callee.start_line, callee.name
SKIP $offset
LIMIT $limit`)
	return cypher.String()
}

func crossModuleCallPredicates(req importDependencyRequest) []string {
	predicates := make([]string, 0, 5)
	if strings.TrimSpace(req.RepoID) != "" {
		predicates = append(predicates, "repo.id = $repo_id")
	}
	if strings.TrimSpace(req.SourceFile) != "" {
		predicates = append(predicates, "source_file.relative_path = $source_file")
	}
	if strings.TrimSpace(req.TargetFile) != "" {
		predicates = append(predicates, "target_file.relative_path = $target_file")
	}
	if language := req.normalizedLanguage(); language != "" {
		predicates = append(predicates, "(source_file.language = $language OR target_file.language = $language)")
	}
	return predicates
}
