// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ComplexityNameCandidateLimit caps the ambiguous-name candidate lookup.
// It is exported because the staying complexity reader bounds through it
// via the root forward.
const ComplexityNameCandidateLimit = 2

// ComplexityAmbiguousError refuses an ambiguous function name before complexity calculation.
type ComplexityAmbiguousError struct {
	FunctionName string
	RepoID       string
	Candidates   []map[string]any
	Truncated    bool
}

func (e ComplexityAmbiguousError) Error() string {
	return fmt.Sprintf("function_name %q matched multiple entities in repository %q", e.FunctionName, e.RepoID)
}

func (e ComplexityAmbiguousError) Details() map[string]any {
	return map[string]any{
		"status":        "ambiguous",
		"function_name": e.FunctionName,
		"repo_id":       e.RepoID,
		"candidates":    e.Candidates,
		"truncated":     e.Truncated,
	}
}

// WriteComplexityAmbiguousError writes the ambiguous-function-name refusal.
func WriteComplexityAmbiguousError(
	w http.ResponseWriter,
	r *http.Request,
	err ComplexityAmbiguousError,
	profile querycontract.QueryProfile,
) {
	if querycontract.AcceptsEnvelope(r) {
		querycontract.WriteJSON(w, http.StatusConflict, querycontract.ResponseEnvelope{
			Data: nil,
			Truth: querycontract.BuildTruthEnvelope(
				profile,
				"code_quality.complexity",
				querycontract.TruthBasisAuthoritativeGraph,
				"refused ambiguous graph entity name before complexity calculation",
			),
			Error: &querycontract.ErrorEnvelope{
				Code:       querycontract.ErrorCodeAmbiguous,
				Message:    err.Error(),
				Capability: "code_quality.complexity",
				Details:    err.Details(),
			},
		})
		return
	}
	details := err.Details()
	details["error"] = http.StatusText(http.StatusConflict)
	details["detail"] = err.Error()
	querycontract.WriteJSON(w, http.StatusConflict, details)
}

// ComplexityCandidateMaps shapes ambiguous complexity candidate rows.
func ComplexityCandidateMaps(rows []map[string]any) []map[string]any {
	candidates := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entityID := querycontract.StringVal(row, "id")
		candidates = append(candidates, map[string]any{
			"entity_id":   entityID,
			"handle":      "entity:" + entityID,
			"name":        querycontract.StringVal(row, "name"),
			"entity_type": firstStringOrEmpty(querycontract.StringSliceVal(row, "labels")),
			"file_path":   querycontract.StringVal(row, "file_path"),
			"repo_id":     querycontract.StringVal(row, "repo_id"),
			"repo_name":   querycontract.StringVal(row, "repo_name"),
			"language":    querycontract.StringVal(row, "language"),
			"start_line":  querycontract.IntVal(row, "start_line"),
			"end_line":    querycontract.IntVal(row, "end_line"),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return complexityCandidateSortKey(candidates[i]) < complexityCandidateSortKey(candidates[j])
	})
	return candidates
}

func complexityCandidateSortKey(candidate map[string]any) string {
	return strings.Join([]string{
		querycontract.StringVal(candidate, "repo_id"),
		querycontract.StringVal(candidate, "file_path"),
		fmt.Sprintf("%012d", querycontract.IntVal(candidate, "start_line")),
		querycontract.StringVal(candidate, "entity_id"),
	}, "\x00")
}

func firstStringOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
