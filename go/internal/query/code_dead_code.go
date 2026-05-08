package query

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"
)

type deadCodeRequest struct {
	RepoID               string   `json:"repo_id"`
	Limit                int      `json:"limit"`
	ExcludeDecoratedWith []string `json:"exclude_decorated_with"`
}

const (
	deadCodeDefaultLimit = 100
	deadCodeMaxLimit     = 500

	deadCodeCandidateLabelPredicate = "(e:Function OR e:Class OR e:Struct OR e:Interface)"

	deadCodeCandidateQueryMultiplier = 10
	deadCodeCandidateQueryMin        = 501
	deadCodeCandidateQueryMax        = 1000
)

// handleDeadCode finds graph-backed dead-code candidates and then applies the
// current default reachability policy before returning a derived result.
func (h *CodeHandler) handleDeadCode(w http.ResponseWriter, r *http.Request) {
	if capabilityUnsupported(h.profile(), "code_quality.dead_code") {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"dead code analysis requires authoritative graph mode",
			ErrorCodeUnsupportedCapability,
			"code_quality.dead_code",
			h.profile(),
			requiredProfile("code_quality.dead_code"),
		)
		return
	}

	var req deadCodeRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Limit <= 0 {
		req.Limit = deadCodeDefaultLimit
	}
	if req.Limit > deadCodeMaxLimit {
		req.Limit = deadCodeMaxLimit
	}
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}

	candidateLimit := deadCodeCandidateQueryLimit(req.Limit)
	rows, err := h.Neo4j.Run(r.Context(), buildDeadCodeGraphCypher(req.RepoID != "", h.graphBackend()), deadCodeGraphParams(req.RepoID, candidateLimit))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	results, contentByID, err := h.buildDeadCodeResults(r.Context(), rows)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	results, policyStats := filterDeadCodeResultsByDefaultPolicy(results, contentByID)
	classifyDeadCodeResults(results, contentByID)
	results = filterResultsByDecoratorExclusions(results, req.ExcludeDecoratedWith)
	truncated := len(rows) >= candidateLimit || len(results) > req.Limit
	if len(results) > req.Limit {
		results = results[:req.Limit]
	}

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"repo_id":   req.RepoID,
		"limit":     req.Limit,
		"truncated": truncated,
		"results":   results,
		"analysis":  buildDeadCodeAnalysis(results, req.ExcludeDecoratedWith, policyStats),
	}, BuildTruthEnvelope(h.profile(), "code_quality.dead_code", TruthBasisHybrid, "resolved from graph-backed dead-code candidates with partial root modeling"))
}

func buildDeadCodeGraphCypher(hasRepoID bool, backend GraphBackend) string {
	cypher := `
		MATCH (e)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
	`
	if hasRepoID {
		cypher = `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e)
	`
	}
	if backend == GraphBackendNornicDB {
		cypher += ` WHERE ` + deadCodeCandidateLabelPredicate + ` AND NOT EXISTS { MATCH (e)<-[:CALLS|IMPORTS|REFERENCES]-() }`
	} else {
		cypher += ` WHERE ` + deadCodeCandidateLabelPredicate + ` AND NOT ()-[:CALLS|IMPORTS|REFERENCES]->(e)`
	}
	cypher += `
		RETURN coalesce(e.id, e.uid) as entity_id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY f.relative_path, e.name
		LIMIT $limit
	`
	return cypher
}

func deadCodeCandidateQueryLimit(displayLimit int) int {
	if displayLimit <= 0 {
		displayLimit = deadCodeDefaultLimit
	}
	candidateLimit := displayLimit*deadCodeCandidateQueryMultiplier + 1
	if candidateLimit < displayLimit+1 {
		return displayLimit + 1
	}
	if candidateLimit < deadCodeCandidateQueryMin {
		return deadCodeCandidateQueryMin
	}
	if candidateLimit > deadCodeCandidateQueryMax {
		return deadCodeCandidateQueryMax
	}
	return candidateLimit
}

func deadCodeGraphParams(repoID string, limit int) map[string]any {
	params := map[string]any{"limit": limit}
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
	}
	return params
}

func (h *CodeHandler) buildDeadCodeResults(
	ctx context.Context,
	rows []map[string]any,
) ([]map[string]any, map[string]*EntityContent, error) {
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result := map[string]any{
			"entity_id":  StringVal(row, "entity_id"),
			"name":       StringVal(row, "name"),
			"labels":     StringSliceVal(row, "labels"),
			"file_path":  StringVal(row, "file_path"),
			"repo_id":    StringVal(row, "repo_id"),
			"repo_name":  StringVal(row, "repo_name"),
			"language":   StringVal(row, "language"),
			"start_line": IntVal(row, "start_line"),
			"end_line":   IntVal(row, "end_line"),
		}
		if metadata := graphResultMetadata(row); len(metadata) > 0 {
			result["metadata"] = metadata
		}
		results = append(results, result)
	}

	return h.enrichDeadCodeResultsWithContent(ctx, results)
}

func (h *CodeHandler) enrichDeadCodeResultsWithContent(
	ctx context.Context,
	results []map[string]any,
) ([]map[string]any, map[string]*EntityContent, error) {
	contentByID := make(map[string]*EntityContent, len(results))
	if len(results) == 0 {
		return results, contentByID, nil
	}

	for i := range results {
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			attachSemanticSummary(results[i])
		}
	}
	if h == nil || h.Content == nil {
		return results, contentByID, nil
	}

	for i := range results {
		entityID := StringVal(results[i], "entity_id")
		if entityID == "" {
			continue
		}
		entity, err := h.Content.GetEntityContent(ctx, entityID)
		if err != nil {
			return nil, nil, err
		}
		if entity == nil {
			continue
		}
		contentByID[entityID] = entity
		if len(entity.Metadata) == 0 {
			continue
		}
		results[i]["metadata"] = mergeGraphAndContentMetadata(results[i]["metadata"], entity.Metadata)
		attachSemanticSummary(results[i])
	}

	return results, contentByID, nil
}

func filterDeadCodeResultsByDefaultPolicy(
	results []map[string]any,
	contentByID map[string]*EntityContent,
) ([]map[string]any, deadCodePolicyStats) {
	if len(results) == 0 {
		return results, deadCodePolicyStats{}
	}

	stats := deadCodePolicyStats{}
	filtered := make([]map[string]any, 0, len(results))
	for _, result := range results {
		entityID := StringVal(result, "entity_id")
		if deadCodeResultExcludedByDefault(result, contentByID[entityID], &stats) {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered, stats
}

func deadCodeResultExcludedByDefault(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if !deadCodeIsCandidateEntity(result, entity) {
		return true
	}
	if !deadCodeLanguageSupported(deadCodeEntityLanguage(result, entity)) {
		return true
	}

	goPolicy := newDeadCodeGoPolicyContext(result, entity)
	if goPolicy.language == "go" && goPolicy.normalizedSource == "" && entity != nil && len(goPolicy.rootKinds) == 0 {
		stats.RootsSkippedMissingSource++
	}

	if deadCodeIsLanguageEntrypoint(result, entity) {
		return true
	}
	if deadCodeIsGoSemanticRoot(result, goPolicy, stats) {
		return true
	}
	if deadCodeIsGoFrameworkRoot(result, goPolicy, stats) {
		return true
	}
	if deadCodeIsPythonFrameworkRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsJavaScriptFrameworkRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsLibraryPublicAPIRoot(result, entity) {
		return true
	}
	if deadCodeIsTestFile(result, entity) {
		return true
	}
	return deadCodeIsGeneratedCode(result, entity)
}

func deadCodeIsCandidateEntity(result map[string]any, entity *EntityContent) bool {
	for _, label := range StringSliceVal(result, "labels") {
		if deadCodeIsCandidateEntityType(label) {
			return true
		}
	}
	if entity == nil {
		return false
	}
	return deadCodeIsCandidateEntityType(entity.EntityType)
}

func deadCodeIsCandidateEntityType(entityType string) bool {
	switch strings.TrimSpace(entityType) {
	case "Function", "Class", "Struct", "Interface":
		return true
	default:
		return false
	}
}

func deadCodeIsLanguageEntrypoint(result map[string]any, entity *EntityContent) bool {
	if primaryEntityLabel(result) != "Function" {
		return false
	}

	name := strings.TrimSpace(StringVal(result, "name"))
	language := strings.ToLower(deadCodeEntityLanguage(result, entity))
	switch language {
	case "go":
		return name == "main" || name == "init"
	case "python":
		return name == "__main__"
	default:
		return false
	}
}

func deadCodeIsLibraryPublicAPIRoot(result map[string]any, entity *EntityContent) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "go" {
		return false
	}
	if !deadCodeIsSupportedGoPublicAPIEntity(result, entity) {
		return false
	}

	path := strings.ToLower(deadCodeEntityPath(result, entity))
	switch {
	case path == "",
		strings.HasPrefix(path, "cmd/"),
		strings.Contains(path, "/cmd/"),
		strings.HasPrefix(path, "internal/"),
		strings.Contains(path, "/internal/"),
		strings.HasPrefix(path, "vendor/"),
		strings.Contains(path, "/vendor/"):
		return false
	}

	name := strings.TrimSpace(StringVal(result, "name"))
	if name == "" {
		return false
	}
	for _, r := range name {
		return unicode.IsUpper(r)
	}
	return false
}

func deadCodeIsSupportedGoPublicAPIEntity(result map[string]any, entity *EntityContent) bool {
	return deadCodeIsCandidateEntity(result, entity)
}

func deadCodeIsTestFile(result map[string]any, entity *EntityContent) bool {
	path := strings.ToLower(deadCodeEntityPath(result, entity))
	base := filepath.Base(path)
	switch {
	case strings.HasSuffix(path, "_test.go"):
		return true
	case strings.Contains(path, "/__tests__/"),
		strings.Contains(path, "/tests/"),
		strings.Contains(path, "/test/"),
		strings.HasPrefix(path, "__tests__/"),
		strings.HasPrefix(path, "tests/"),
		strings.HasPrefix(path, "test/"):
		return true
	case strings.HasPrefix(base, "test_"),
		strings.HasSuffix(base, "_test.py"),
		strings.HasSuffix(base, ".test.js"),
		strings.HasSuffix(base, ".test.jsx"),
		strings.HasSuffix(base, ".test.ts"),
		strings.HasSuffix(base, ".test.tsx"),
		strings.HasSuffix(base, ".spec.js"),
		strings.HasSuffix(base, ".spec.jsx"),
		strings.HasSuffix(base, ".spec.ts"),
		strings.HasSuffix(base, ".spec.tsx"):
		return true
	default:
		return false
	}
}

func deadCodeIsGeneratedCode(result map[string]any, entity *EntityContent) bool {
	path := strings.ToLower(deadCodeEntityPath(result, entity))
	base := filepath.Base(path)
	switch {
	case strings.Contains(path, "/gen/"),
		strings.Contains(path, "/generated/"),
		strings.Contains(path, "/.generated/"),
		strings.HasSuffix(base, ".pb.go"),
		strings.HasSuffix(base, ".gen.go"),
		strings.HasSuffix(base, "_generated.go"):
		return true
	}

	source := ""
	if entity != nil {
		source = strings.ToLower(entity.SourceCache)
	}
	return strings.Contains(source, "code generated by") ||
		strings.Contains(source, "do not edit") ||
		strings.Contains(source, "@generated")
}

func deadCodeEntityPath(result map[string]any, entity *EntityContent) string {
	if entity != nil && strings.TrimSpace(entity.RelativePath) != "" {
		return filepath.ToSlash(entity.RelativePath)
	}
	return filepath.ToSlash(StringVal(result, "file_path"))
}

func deadCodeEntityLanguage(result map[string]any, entity *EntityContent) string {
	if entity != nil && strings.TrimSpace(entity.Language) != "" {
		return entity.Language
	}
	return StringVal(result, "language")
}

func deadCodeIsGoFrameworkRoot(result map[string]any, policy deadCodeGoPolicyContext, stats *deadCodePolicyStats) bool {
	if policy.language != "go" {
		return false
	}
	if len(policy.rootKinds) > 0 {
		if deadCodeIsGoCLICommandRoot(result, policy) ||
			deadCodeIsGoHTTPHandlerRoot(result, policy) ||
			deadCodeIsGoFrameworkCallbackRoot(result, policy) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
		return false
	}

	if deadCodeIsGoCLICommandRoot(result, policy) ||
		deadCodeIsGoHTTPHandlerRoot(result, policy) ||
		deadCodeIsGoFrameworkCallbackRoot(result, policy) {
		stats.SourceFallbackFrameworkRoots++
		return true
	}
	return false
}
