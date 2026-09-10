// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/entitysemantics"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type DeadCodeRequest struct {
	CandidateKind        string   `json:"candidate_kind"`
	RepoID               string   `json:"repo_id"`
	Language             string   `json:"language"`
	Limit                int      `json:"limit"`
	ExcludeDecoratedWith []string `json:"exclude_decorated_with"`
}

// codeshaping.DeadCodeDefaultLimit and the codeshaping.DeadCodeCandidateQuery* scan bounds moved to
// codeshaping/code_dead_code_candidate_schedule.go with the schedule
// (#6060 lane A L3); family_code_shim_shaping.go aliases them back so this
// orchestrator keeps its names.
const (
	// DeadCodeMaxLimit caps a dead-code request limit. It is exported
	// because the staying dead-code contract tests in codequery name it.
	DeadCodeMaxLimit = 500
)

// HandleDeadCode finds graph-backed dead-code candidates and then applies the
// current default reachability policy before returning a derived result.
func (a *Analyzer) HandleDeadCode(w http.ResponseWriter, r *http.Request) {
	if querycontract.CapabilityUnsupported(a.deps.Profile, "code_quality.dead_code") {
		a.deps.WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"dead code analysis requires authoritative graph mode",
			querycontract.ErrorCodeUnsupportedCapability,
			"code_quality.dead_code",
			a.deps.Profile,
			querycontract.RequiredProfile("code_quality.dead_code"),
		)
		return
	}

	var req DeadCodeRequest
	if err := a.deps.ReadJSON(r, &req); err != nil {
		a.deps.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Limit <= 0 {
		req.Limit = codeshaping.DeadCodeDefaultLimit
	}
	if req.Limit > DeadCodeMaxLimit {
		req.Limit = DeadCodeMaxLimit
	}
	req.Language = normalizeDeadCodeLanguage(req.Language)
	req.CandidateKind = strings.TrimSpace(req.CandidateKind)
	if req.CandidateKind != "" && !IsDeadCodeCandidateLabel(req.CandidateKind) {
		a.deps.WriteError(w, http.StatusBadRequest, "unsupported candidate_kind")
		return
	}
	if !a.deps.ApplySelector(w, r, &req.RepoID, "code_quality.dead_code") {
		return
	}

	scan, err := a.ScanDeadCodeCandidates(r.Context(), req)
	if err != nil {
		if a.deps.WriteGraphReadError(w, r, err, "code_quality.dead_code") {
			return
		}
		a.deps.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	truncated := scan.CandidateScanTruncated || scan.DisplayTruncated

	a.deps.WriteSuccess(w, r, http.StatusOK, map[string]any{
		"candidate_kind":                 req.CandidateKind,
		"repo_id":                        req.RepoID,
		"language":                       req.Language,
		"limit":                          req.Limit,
		"truncated":                      truncated,
		"display_truncated":              scan.DisplayTruncated,
		"candidate_scan_truncated":       scan.CandidateScanTruncated,
		"candidate_scan_limit":           scan.CandidateScanLimit,
		"candidate_scan_limit_per_label": scan.CandidateScanLimitPerLabel,
		"candidate_scan_pages":           scan.CandidateScanPages,
		"candidate_scan_rows":            scan.CandidateScanRows,
		"results":                        scan.Results,
		"analysis":                       codemodel.BuildDeadCodeAnalysisForLanguage(scan.Results, req.ExcludeDecoratedWith, scan.PolicyStats, req.Language),
	}, querycontract.BuildTruthEnvelope(a.deps.Profile, "code_quality.dead_code", querycontract.TruthBasisHybrid, "resolved from graph-backed dead-code candidates with partial root modeling"))
}

func (a *Analyzer) buildDeadCodeResults(
	ctx context.Context,
	rows []map[string]any,
) ([]map[string]any, map[string]*EntityContent, error) {
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		result := map[string]any{
			"entity_id":  querycontract.StringVal(row, "entity_id"),
			"name":       querycontract.StringVal(row, "name"),
			"labels":     querycontract.StringSliceVal(row, "labels"),
			"file_path":  querycontract.StringVal(row, "file_path"),
			"repo_id":    querycontract.StringVal(row, "repo_id"),
			"repo_name":  querycontract.StringVal(row, "repo_name"),
			"language":   querycontract.StringVal(row, "language"),
			"start_line": querycontract.IntVal(row, "start_line"),
			"end_line":   querycontract.IntVal(row, "end_line"),
		}
		if metadata := querycontract.GraphResultMetadata(row); len(metadata) > 0 {
			result["metadata"] = metadata
		}
		results = append(results, result)
	}

	return a.enrichDeadCodeResultsWithContent(ctx, results)
}

func (a *Analyzer) enrichDeadCodeResultsWithContent(
	ctx context.Context,
	results []map[string]any,
) ([]map[string]any, map[string]*EntityContent, error) {
	contentByID := make(map[string]*EntityContent, len(results))
	if len(results) == 0 {
		return results, contentByID, nil
	}

	for i := range results {
		if metadata, ok := results[i]["metadata"].(map[string]any); ok && len(metadata) > 0 {
			entitysemantics.AttachSemanticSummary(results[i])
		}
	}
	if a == nil || a.deps.Content == nil {
		return results, contentByID, nil
	}

	if batchContent, ok := a.deps.Content.(deadCodeEntityContentBatchStore); ok {
		return a.enrichDeadCodeResultsWithContentBatch(ctx, results, contentByID, batchContent)
	}

	for i := range results {
		entityID := querycontract.StringVal(results[i], "entity_id")
		if entityID == "" {
			continue
		}
		entity, err := a.deps.Content.GetEntityContent(ctx, entityID)
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
		results[i]["metadata"] = a.deps.MergeMetadata(results[i]["metadata"], entity.Metadata)
		entitysemantics.AttachSemanticSummary(results[i])
	}

	return results, contentByID, nil
}

func (a *Analyzer) enrichDeadCodeResultsWithContentBatch(
	ctx context.Context,
	results []map[string]any,
	contentByID map[string]*EntityContent,
	batchContent deadCodeEntityContentBatchStore,
) ([]map[string]any, map[string]*EntityContent, error) {
	entityIDs := make([]string, 0, len(results))
	seen := make(map[string]struct{}, len(results))
	for i := range results {
		entityID := querycontract.StringVal(results[i], "entity_id")
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
		entityID := querycontract.StringVal(results[i], "entity_id")
		entity := entities[entityID]
		if entity == nil {
			continue
		}
		contentByID[entityID] = entity
		if len(entity.Metadata) == 0 {
			continue
		}
		results[i]["metadata"] = a.deps.MergeMetadata(results[i]["metadata"], entity.Metadata)
		entitysemantics.AttachSemanticSummary(results[i])
	}
	return results, contentByID, nil
}

type deadCodeEntityContentBatchStore interface {
	GetEntityContents(ctx context.Context, entityIDs []string) (map[string]*EntityContent, error)
}

// FilterDeadCodeResultsByDefaultPolicy applies the default reachability
// policy to candidate results. It is exported because the staying default
// policy tests in codequery name it.
func FilterDeadCodeResultsByDefaultPolicy(
	results []map[string]any,
	contentByID map[string]*EntityContent,
	downgraded codemodel.DeadCodeDowngradedRoots,
) ([]map[string]any, codemodel.DeadCodePolicyStats) {
	if len(results) == 0 {
		return results, codemodel.DeadCodePolicyStats{}
	}

	stats := codemodel.DeadCodePolicyStats{}
	filtered := make([]map[string]any, 0, len(results))
	for _, result := range results {
		entityID := querycontract.StringVal(result, "entity_id")
		if deadCodeResultExcludedByDefault(result, contentByID[entityID], &stats, downgraded) {
			continue
		}
		filtered = append(filtered, result)
	}
	return filtered, stats
}

func deadCodeResultExcludedByDefault(result map[string]any, entity *EntityContent, stats *codemodel.DeadCodePolicyStats, downgraded codemodel.DeadCodeDowngradedRoots) bool {
	if !deadCodeIsCandidateEntity(result, entity) {
		return true
	}
	if !codemodel.DeadCodeLanguageSupported(deadCodeEntityLanguage(result, entity)) {
		return true
	}

	goPolicy := codemodel.NewDeadCodeGoPolicyContext(result, entity)
	if goPolicy.Language == "go" && goPolicy.NormalizedSource == "" && entity != nil && len(goPolicy.RootKinds) == 0 {
		stats.RootsSkippedMissingSource++
	}

	if deadCodeIsLanguageEntrypoint(result, entity) {
		return true
	}
	if codemodel.DeadCodeIsGoSemanticRoot(result, goPolicy, stats) {
		return true
	}
	if codemodel.DeadCodeIsGoFrameworkRoot(result, goPolicy, stats) {
		return true
	}
	if codemodel.DeadCodeIsPythonFrameworkRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsPythonAnonymousLambda(result, entity) {
		return true
	}
	if codemodel.DeadCodeIsJavaRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsKotlinRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsScalaRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsElixirRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsCRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsCSharpRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsCPPRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsRustRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsRustCargoAuxiliaryTarget(result, entity) {
		return true
	}
	if codemodel.DeadCodeIsRubyRoot(result, entity, stats, downgraded) {
		return true
	}
	if codemodel.DeadCodeIsGroovyRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsHaskellRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsPerlRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsPHPRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsSwiftRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsDartRoot(result, entity, stats) {
		return true
	}
	if codemodel.DeadCodeIsJavaScriptFrameworkRoot(result, entity, stats) {
		return true
	}
	if deadCodeIsNestedJavaScriptFunction(result, entity) {
		return true
	}
	if deadCodeIsLibraryPublicAPIRoot(result, entity) {
		return true
	}
	if codemodel.DeadCodeIsTestFile(result, entity) {
		return true
	}
	return codemodel.DeadCodeIsGeneratedCode(result, entity)
}

func deadCodeIsLanguageEntrypoint(result map[string]any, entity *EntityContent) bool {
	if querycontract.PrimaryEntityLabel(result) != "Function" {
		return false
	}

	name := strings.TrimSpace(querycontract.StringVal(result, "name"))
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
	if querycontract.PrimaryEntityLabel(result) != "Function" {
		return false
	}
	metadata, _ := result["metadata"].(map[string]any)
	if strings.TrimSpace(querycontract.StringVal(metadata, "enclosing_function")) != "" {
		return true
	}
	if entity != nil && strings.TrimSpace(querycontract.StringVal(entity.Metadata, "enclosing_function")) != "" {
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

	name := strings.TrimSpace(querycontract.StringVal(result, "name"))
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
	return filepath.ToSlash(querycontract.StringVal(result, "file_path"))
}

func deadCodeEntityLanguage(result map[string]any, entity *EntityContent) string {
	if entity != nil && strings.TrimSpace(entity.Language) != "" {
		return entity.Language
	}
	return querycontract.StringVal(result, "language")
}
