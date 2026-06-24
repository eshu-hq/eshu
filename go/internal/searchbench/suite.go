// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package searchbench

import (
	"fmt"
	"strings"
)

// ValidateQuerySuite checks the issue #417 semantic retrieval suite contract.
func ValidateQuerySuite(suite QuerySuite) error {
	var problems []string
	version := strings.TrimSpace(suite.Version)
	if version == "" {
		problems = append(problems, "version is required")
	} else if version != QuerySuiteVersion {
		problems = append(problems, "version is invalid")
	}
	if len(suite.Queries) < MinimumQuerySuiteSize {
		problems = append(problems, fmt.Sprintf("at least %d queries are required", MinimumQuerySuiteSize))
	}

	seen := make(map[string]struct{}, len(suite.Queries))
	for i, query := range suite.Queries {
		prefix := fmt.Sprintf("queries[%d]", i)
		queryID := strings.TrimSpace(query.ID)
		if queryID == "" {
			problems = append(problems, prefix+".id is required")
		} else if _, ok := seen[queryID]; ok {
			problems = append(problems, prefix+".id duplicates "+queryID)
		} else {
			seen[queryID] = struct{}{}
		}
		if strings.TrimSpace(query.Text) == "" {
			problems = append(problems, prefix+".text is required")
		}
		if !validMode(query.Mode) {
			problems = append(problems, prefix+".mode is invalid")
		}
		if query.Limit <= 0 {
			problems = append(problems, prefix+".limit is required")
		} else if query.Limit > MaximumQueryLimit {
			problems = append(problems, fmt.Sprintf("%s.limit exceeds maximum of %d", prefix, MaximumQueryLimit))
		}
		if !queryHasScope(query) {
			problems = append(problems, prefix+".scope is required")
		}
		if len(nonEmptyHandles(query.ExpectedHandles)) == 0 {
			problems = append(problems, prefix+".expected_handles are required")
		}
	}
	return joinedValidationError(problems)
}

// ScoreQuerySuite scores a valid query suite in suite order.
func ScoreQuerySuite(
	suite QuerySuite,
	resultsByQueryID map[string][]Result,
) (QuerySuiteScore, error) {
	if err := ValidateQuerySuite(suite); err != nil {
		return QuerySuiteScore{}, err
	}

	score := QuerySuiteScore{
		QueryCount: len(suite.Queries),
		PerQuery:   make([]QueryScore, 0, len(suite.Queries)),
	}
	falseCanonicalClaims := 0
	for _, query := range suite.Queries {
		query.ID = strings.TrimSpace(query.ID)
		metrics := ScoreQueryResults(query, resultsByQueryID[query.ID])
		score.Metrics.Recall += metrics.Recall
		score.Metrics.Precision += metrics.Precision
		score.Metrics.NDCG += metrics.NDCG
		if metrics.FalseCanonicalClaimCount != nil {
			falseCanonicalClaims += *metrics.FalseCanonicalClaimCount
		}
		score.PerQuery = append(score.PerQuery, QueryScore{
			QueryID: query.ID,
			Metrics: metrics,
		})
	}

	divisor := float64(score.QueryCount)
	score.Metrics.Recall /= divisor
	score.Metrics.Precision /= divisor
	score.Metrics.NDCG /= divisor
	score.Metrics.FalseCanonicalClaimCount = &falseCanonicalClaims
	return score, nil
}

func queryHasScope(query Query) bool {
	return strings.TrimSpace(query.ServiceID) != "" ||
		strings.TrimSpace(query.WorkloadID) != "" ||
		strings.TrimSpace(query.RepoID) != "" ||
		strings.TrimSpace(query.Environment) != ""
}

func nonEmptyHandles(handles []string) []string {
	out := make([]string, 0, len(handles))
	for _, handle := range handles {
		if strings.TrimSpace(handle) != "" {
			out = append(out, handle)
		}
	}
	return out
}
