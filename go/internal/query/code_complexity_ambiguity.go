// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
)

const complexityNameCandidateLimit = 2

type complexityAmbiguousError struct {
	FunctionName string
	RepoID       string
	Candidates   []map[string]any
	Truncated    bool
}

func (e complexityAmbiguousError) Error() string {
	return fmt.Sprintf("function_name %q matched multiple entities in repository %q", e.FunctionName, e.RepoID)
}

func (e complexityAmbiguousError) Details() map[string]any {
	return map[string]any{
		"status":        "ambiguous",
		"function_name": e.FunctionName,
		"repo_id":       e.RepoID,
		"candidates":    e.Candidates,
		"truncated":     e.Truncated,
	}
}

func writeComplexityAmbiguousError(
	w http.ResponseWriter,
	r *http.Request,
	err complexityAmbiguousError,
	profile QueryProfile,
) {
	if acceptsEnvelope(r) {
		WriteJSON(w, http.StatusConflict, ResponseEnvelope{
			Data: nil,
			Truth: BuildTruthEnvelope(
				profile,
				"code_quality.complexity",
				TruthBasisAuthoritativeGraph,
				"refused ambiguous graph entity name before complexity calculation",
			),
			Error: &ErrorEnvelope{
				Code:       ErrorCodeAmbiguous,
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
	WriteJSON(w, http.StatusConflict, details)
}

func complexityCandidateMaps(rows []map[string]any) []map[string]any {
	candidates := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		entityID := StringVal(row, "id")
		candidates = append(candidates, map[string]any{
			"entity_id":   entityID,
			"handle":      "entity:" + entityID,
			"name":        StringVal(row, "name"),
			"entity_type": firstStringOrEmpty(StringSliceVal(row, "labels")),
			"file_path":   StringVal(row, "file_path"),
			"repo_id":     StringVal(row, "repo_id"),
			"repo_name":   StringVal(row, "repo_name"),
			"language":    StringVal(row, "language"),
			"start_line":  IntVal(row, "start_line"),
			"end_line":    IntVal(row, "end_line"),
		})
	}
	sort.Slice(candidates, func(i, j int) bool {
		return complexityCandidateSortKey(candidates[i]) < complexityCandidateSortKey(candidates[j])
	})
	return candidates
}

func complexityCandidateSortKey(candidate map[string]any) string {
	return strings.Join([]string{
		StringVal(candidate, "repo_id"),
		StringVal(candidate, "file_path"),
		fmt.Sprintf("%012d", IntVal(candidate, "start_line")),
		StringVal(candidate, "entity_id"),
	}, "\x00")
}

func firstStringOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
