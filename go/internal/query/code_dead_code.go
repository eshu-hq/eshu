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
	Language             string   `json:"language"`
	Limit                int      `json:"limit"`
	ExcludeDecoratedWith []string `json:"exclude_decorated_with"`
}

const (
	deadCodeDefaultLimit = 100
	deadCodeMaxLimit     = 500

	deadCodeCandidateQueryMultiplier = 10
	deadCodeCandidateQueryMin        = 100
	deadCodeCandidateQueryMax        = 250
	deadCodeCandidateScanMaxPages    = 10
)

var deadCodeCandidateLabels = []string{"Function", "Class", "Struct", "Interface", "Trait", "SqlFunction"}

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
	req.Language = normalizeDeadCodeLanguage(req.Language)
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}

	scan, err := h.scanDeadCodeCandidates(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := scan.CandidateScanTruncated || scan.DisplayTruncated

	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"repo_id":                  req.RepoID,
		"language":                 req.Language,
		"limit":                    req.Limit,
		"truncated":                truncated,
		"display_truncated":        scan.DisplayTruncated,
		"candidate_scan_truncated": scan.CandidateScanTruncated,
		"candidate_scan_limit":     scan.CandidateScanLimit,
		"candidate_scan_pages":     scan.CandidateScanPages,
		"candidate_scan_rows":      scan.CandidateScanRows,
		"results":                  scan.Results,
		"analysis":                 buildDeadCodeAnalysis(scan.Results, req.ExcludeDecoratedWith, scan.PolicyStats),
	}, BuildTruthEnvelope(h.profile(), "code_quality.dead_code", TruthBasisHybrid, "resolved from graph-backed dead-code candidates with partial root modeling"))
}

func buildDeadCodeGraphCypher(hasRepoID bool, _ GraphBackend) string {
	return buildDeadCodeGraphCypherForLabel(hasRepoID, "Function", "")
}

func buildDeadCodeGraphCypherForLabel(hasRepoID bool, label string, language string) string {
	if !isDeadCodeCandidateLabel(label) {
		label = "Function"
	}
	cypher := `
			MATCH (e:` + label + `)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(r:Repository)
		`
	if hasRepoID {
		cypher = `
			MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)-[:CONTAINS]->(e:` + label + `)
		`
	}
	if strings.TrimSpace(language) != "" {
		cypher += `
		WHERE toLower(coalesce(e.language, f.language, '')) = $language
	`
	}
	cypher += `
		RETURN coalesce(e.uid, e.id) as entity_id, e.name as name, labels(e) as labels,
		       f.relative_path as file_path,
		       r.id as repo_id, r.name as repo_name,
		       coalesce(e.language, f.language) as language,
		       e.start_line as start_line,
		       e.end_line as end_line,
` + graphSemanticMetadataProjection() + `
		ORDER BY f.relative_path, e.name, coalesce(e.uid, e.id)
		SKIP $skip
		LIMIT $limit
	`
	return cypher
}

func buildDeadCodeIncomingBatchProbeCypher(label string) string {
	if !isDeadCodeCandidateLabel(label) {
		label = "Function"
	}
	return `
		UNWIND $entity_ids AS entity_id
		MATCH (e:` + label + ` {uid: entity_id})<-[:CALLS|IMPORTS|REFERENCES|INHERITS|EXECUTES]-(source)
		RETURN DISTINCT coalesce(e.uid, e.id) as incoming_entity_id
	`
}

func isDeadCodeCandidateLabel(label string) bool {
	for _, candidate := range deadCodeCandidateLabels {
		if label == candidate {
			return true
		}
	}
	return false
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

func deadCodeCandidateScanLimit(displayLimit int) int {
	return deadCodeCandidateQueryLimit(displayLimit) * deadCodeCandidateScanMaxPages
}

func deadCodeGraphParams(repoID string, language string, limit int, skip int) map[string]any {
	params := map[string]any{"limit": limit, "skip": skip}
	if strings.TrimSpace(repoID) != "" {
		params["repo_id"] = strings.TrimSpace(repoID)
	}
	if strings.TrimSpace(language) != "" {
		params["language"] = strings.ToLower(strings.TrimSpace(language))
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

	if batchContent, ok := h.Content.(deadCodeEntityContentBatchStore); ok {
		return h.enrichDeadCodeResultsWithContentBatch(ctx, results, contentByID, batchContent)
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

func (h *CodeHandler) enrichDeadCodeResultsWithContentBatch(
	ctx context.Context,
	results []map[string]any,
	contentByID map[string]*EntityContent,
	batchContent deadCodeEntityContentBatchStore,
) ([]map[string]any, map[string]*EntityContent, error) {
	entityIDs := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for i := range results {
		entityID := StringVal(results[i], "entity_id")
		if entityID == "" {
			continue
		}
		if _, ok := seen[entityID]; ok {
			continue
		}
		seen[entityID] = struct{}{}
		entityIDs = append(entityIDs, entityID)
	}
	entities, err := batchContent.GetEntityContents(ctx, entityIDs)
	if err != nil {
		return nil, nil, err
	}
	for i := range results {
		entityID := StringVal(results[i], "entity_id")
		entity := entities[entityID]
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

type deadCodeEntityContentBatchStore interface {
	GetEntityContents(ctx context.Context, entityIDs []string) (map[string]*EntityContent, error)
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
	if deadCodeIsPythonAnonymousLambda(result, entity) {
		return true
	}
	if deadCodeIsJavaRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsKotlinRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsScalaRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsElixirRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsCRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsCSharpRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsCPPRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsRustRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsRustCargoAuxiliaryTarget(result, entity) {
		return true
	}
	if deadCodeIsRubyRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsGroovyRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsHaskellRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsPerlRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsPHPRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsSwiftRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsDartRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsJavaScriptFrameworkRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsNestedJavaScriptFunction(result, entity) {
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

func deadCodeIsNestedJavaScriptFunction(result map[string]any, entity *EntityContent) bool {
	switch strings.ToLower(deadCodeEntityLanguage(result, entity)) {
	case "javascript", "jsx", "typescript", "tsx":
	default:
		return false
	}
	if primaryEntityLabel(result) != "Function" {
		return false
	}
	metadata, _ := result["metadata"].(map[string]any)
	if strings.TrimSpace(StringVal(metadata, "enclosing_function")) != "" {
		return true
	}
	if entity != nil && strings.TrimSpace(StringVal(entity.Metadata, "enclosing_function")) != "" {
		return true
	}
	return false
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
