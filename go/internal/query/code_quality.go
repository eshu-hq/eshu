package query

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

const (
	codeQualityDefaultLimit  = 10
	codeQualityMaxLimit      = 100
	codeQualityMaxOffset     = 10000
	codeQualityDefaultLines  = 20
	codeQualityDefaultArgs   = 5
	codeQualityDefaultCC     = 10
	codeQualityCapability    = "code_quality.refactoring"
	codeQualityCheckComplex  = "complexity"
	codeQualityCheckLength   = "function_length"
	codeQualityCheckArgs     = "argument_count"
	codeQualityCheckRefactor = "refactoring_candidates"
)

type codeQualityInspectionRequest struct {
	Check         string `json:"check"`
	RepoID        string `json:"repo_id"`
	Language      string `json:"language"`
	EntityID      string `json:"entity_id"`
	FunctionName  string `json:"function_name"`
	MinComplexity int    `json:"min_complexity"`
	MinLines      int    `json:"min_lines"`
	MinArguments  int    `json:"min_arguments"`
	Limit         int    `json:"limit"`
	Offset        int    `json:"offset"`
}

func (h *CodeHandler) handleCodeQualityInspection(w http.ResponseWriter, r *http.Request) {
	var req codeQualityInspectionRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	normalizeCodeQualityInspectionRequest(&req)
	if !isSupportedCodeQualityCheck(req.Check) {
		WriteError(w, http.StatusBadRequest, "unsupported code quality check")
		return
	}
	if req.Offset > codeQualityMaxOffset {
		WriteError(w, http.StatusBadRequest, "offset exceeds maximum")
		return
	}
	if !h.applyRepositorySelector(w, r, &req.RepoID) {
		return
	}

	rows, err := h.inspectCodeQuality(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	results, truncated := trimCodeQualityResults(codeQualityRows(rows), req.Limit)
	WriteSuccess(w, r, http.StatusOK, map[string]any{
		"source":                 "graph",
		"source_backend":         "graph",
		"check":                  req.Check,
		"repo_id":                req.RepoID,
		"language":               req.Language,
		"limit":                  req.Limit,
		"offset":                 req.Offset,
		"truncated":              truncated,
		"result_key":             "entity_id",
		"thresholds":             codeQualityThresholds(req),
		"results":                results,
		"recommended_next_calls": codeQualityNextCalls(results),
	}, BuildTruthEnvelope(h.profile(), codeQualityCapability, TruthBasisAuthoritativeGraph, "resolved from bounded graph code-quality metrics"))
}

func normalizeCodeQualityInspectionRequest(req *codeQualityInspectionRequest) {
	req.Check = strings.TrimSpace(req.Check)
	if req.Check == "" {
		req.Check = codeQualityCheckRefactor
	}
	req.RepoID = strings.TrimSpace(req.RepoID)
	req.Language = strings.TrimSpace(req.Language)
	req.EntityID = strings.TrimSpace(req.EntityID)
	req.FunctionName = strings.TrimSpace(req.FunctionName)
	req.Limit = normalizeCodeQualityLimit(req.Limit)
	if req.Offset < 0 {
		req.Offset = 0
	}
	if req.MinLines <= 0 {
		req.MinLines = codeQualityDefaultLines
	}
	if req.MinArguments <= 0 {
		req.MinArguments = codeQualityDefaultArgs
	}
	if req.MinComplexity <= 0 {
		if req.Check == codeQualityCheckComplex {
			req.MinComplexity = 1
		} else {
			req.MinComplexity = codeQualityDefaultCC
		}
	}
}

func isSupportedCodeQualityCheck(check string) bool {
	switch check {
	case codeQualityCheckComplex, codeQualityCheckLength, codeQualityCheckArgs, codeQualityCheckRefactor:
		return true
	default:
		return false
	}
}

func normalizeCodeQualityLimit(limit int) int {
	if limit <= 0 {
		return codeQualityDefaultLimit
	}
	if limit > codeQualityMaxLimit {
		return codeQualityMaxLimit
	}
	return limit
}

func (h *CodeHandler) inspectCodeQuality(ctx context.Context, req codeQualityInspectionRequest) ([]map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, fmt.Errorf("graph backend is required for code quality inspection")
	}
	cypher, params := buildCodeQualityCypher(req)
	return h.Neo4j.Run(ctx, cypher, params)
}

func buildCodeQualityCypher(req codeQualityInspectionRequest) (string, map[string]any) {
	params := map[string]any{
		"limit":          req.Limit + 1,
		"offset":         req.Offset,
		"min_complexity": req.MinComplexity,
		"min_lines":      req.MinLines,
		"min_arguments":  req.MinArguments,
	}
	where := make([]string, 0, 4)
	if req.RepoID != "" {
		where = append(where, "repo.id = $repo_id")
		params["repo_id"] = req.RepoID
	}
	if req.Language != "" {
		where = append(where, "(e.language = $language OR f.language = $language)")
		params["language"] = req.Language
	}
	if req.EntityID != "" {
		where = append(where, "e.id = $entity_id")
		params["entity_id"] = req.EntityID
	}
	if req.FunctionName != "" {
		where = append(where, "e.name = $function_name")
		params["function_name"] = req.FunctionName
	}

	var builder strings.Builder
	builder.WriteString(`
MATCH (e:Function)<-[:CONTAINS]-(f:File)<-[:REPO_CONTAINS]-(repo:Repository)
`)
	if len(where) > 0 {
		builder.WriteString("WHERE ")
		builder.WriteString(strings.Join(where, " AND "))
		builder.WriteString("\n")
	}
	builder.WriteString(`
WITH e, f, repo,
     coalesce(e.cyclomatic_complexity, 0) as complexity,
     coalesce(e.parameter_count, 0) as parameter_count,
     coalesce(e.parameter_count, 0) as argument_count,
     coalesce(e.end_line, 0) - coalesce(e.start_line, 0) + 1 as line_count
`)
	builder.WriteString(codeQualityMetricFilter(req.Check))
	builder.WriteString(`
RETURN e.id as entity_id, e.name as name, labels(e) as labels,
       f.relative_path as file_path,
       repo.id as repo_id, repo.name as repo_name,
       coalesce(e.language, f.language) as language,
       e.start_line as start_line,
       e.end_line as end_line,
       line_count as line_count,
       parameter_count as argument_count,
       complexity as complexity
`)
	builder.WriteString(codeQualityOrderBy(req.Check))
	builder.WriteString(`
SKIP $offset
LIMIT $limit
`)
	return builder.String(), params
}

func codeQualityMetricFilter(check string) string {
	switch check {
	case codeQualityCheckComplex:
		return "WHERE complexity >= $min_complexity\n"
	case codeQualityCheckLength:
		return "WHERE line_count >= $min_lines\n"
	case codeQualityCheckArgs:
		return "WHERE argument_count >= $min_arguments\n"
	case codeQualityCheckRefactor:
		return "WHERE complexity >= $min_complexity OR line_count >= $min_lines OR argument_count >= $min_arguments\n"
	default:
		return ""
	}
}

func codeQualityOrderBy(check string) string {
	switch check {
	case codeQualityCheckComplex:
		return "ORDER BY complexity DESC, e.name, e.id\n"
	case codeQualityCheckLength:
		return "ORDER BY line_count DESC, e.name, e.id\n"
	case codeQualityCheckArgs:
		return "ORDER BY argument_count DESC, e.name, e.id\n"
	case codeQualityCheckRefactor:
		return "ORDER BY complexity DESC, line_count DESC, argument_count DESC, e.name, e.id\n"
	default:
		return "ORDER BY e.name, e.id\n"
	}
}

func codeQualityRows(rows []map[string]any) []map[string]any {
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		filePath := StringVal(row, "file_path")
		repoID := StringVal(row, "repo_id")
		startLine := IntVal(row, "start_line")
		endLine := IntVal(row, "end_line")
		results = append(results, map[string]any{
			"entity_id":      StringVal(row, "entity_id"),
			"name":           StringVal(row, "name"),
			"labels":         StringSliceVal(row, "labels"),
			"file_path":      filePath,
			"repo_id":        repoID,
			"repo_name":      StringVal(row, "repo_name"),
			"language":       StringVal(row, "language"),
			"start_line":     startLine,
			"end_line":       endLine,
			"line_count":     IntVal(row, "line_count"),
			"argument_count": IntVal(row, "argument_count"),
			"complexity":     IntVal(row, "complexity"),
			"source_handle":  symbolSourceHandle(repoID, filePath, startLine, endLine),
		})
	}
	return results
}

func trimCodeQualityResults(results []map[string]any, limit int) ([]map[string]any, bool) {
	if len(results) <= limit {
		return results, false
	}
	return results[:limit], true
}

func codeQualityThresholds(req codeQualityInspectionRequest) map[string]any {
	return map[string]any{
		"min_complexity": req.MinComplexity,
		"min_lines":      req.MinLines,
		"min_arguments":  req.MinArguments,
	}
}

func codeQualityNextCalls(results []map[string]any) []map[string]any {
	next := make([]map[string]any, 0, len(results))
	for _, result := range results {
		next = append(next, map[string]any{
			"tool":          "get_file_lines",
			"repo_id":       result["repo_id"],
			"relative_path": result["file_path"],
			"start_line":    result["start_line"],
			"end_line":      result["end_line"],
		})
	}
	return next
}
