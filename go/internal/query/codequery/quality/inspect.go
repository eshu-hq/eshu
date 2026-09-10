// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quality

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Inspect runs the bounded refactoring-candidate scan for the request.
// A grantless caller resolves to no rows without touching the graph.
func Inspect(
	ctx context.Context,
	graph querycontract.GraphQuery,
	req Request,
	access querycontract.RepositoryAccessFilter,
) ([]map[string]any, error) {
	if graph == nil {
		return nil, fmt.Errorf("graph backend is required for code quality inspection")
	}
	// #5167 code family: the only repository predicate here was the caller's
	// own optional repo_id, so a scoped caller who omitted it inspected every
	// tenant's functions.
	if access.Empty() {
		return nil, nil
	}
	cypher, params := BuildCypher(req, access)
	return graph.Run(ctx, cypher, params)
}

// BuildCypher builds the bounded refactoring-candidate scan. access
// appends the caller's grant to the same MATCH-attached WHERE the optional
// repo_id/language/entity filters use, so the grant lands before the
// SKIP/LIMIT and the page is taken from the granted set.
func BuildCypher(
	req Request,
	access querycontract.RepositoryAccessFilter,
) (string, map[string]any) {
	params := map[string]any{
		"limit":          req.Limit + 1,
		"offset":         req.Offset,
		"min_complexity": req.MinComplexity,
		"min_lines":      req.MinLines,
		"min_arguments":  req.MinArguments,
	}
	where := make([]string, 0, 5)
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
	if access.Scoped() {
		where = append(where, access.GraphCondition("repo"))
		params = access.GraphParams(params)
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
	builder.WriteString(MetricFilter(req.Check))
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
	builder.WriteString(OrderBy(req.Check))
	builder.WriteString(`
SKIP $offset
LIMIT $limit
`)
	return builder.String(), params
}

// MetricFilter renders the metric floor for one check.
func MetricFilter(check string) string {
	switch check {
	case CheckComplex:
		return "WHERE complexity >= $min_complexity\n"
	case CheckLength:
		return "WHERE line_count >= $min_lines\n"
	case CheckArgs:
		return "WHERE argument_count >= $min_arguments\n"
	case CheckRefactor:
		return "WHERE complexity >= $min_complexity OR line_count >= $min_lines OR argument_count >= $min_arguments\n"
	default:
		return ""
	}
}

// OrderBy renders the result ordering for one check.
func OrderBy(check string) string {
	switch check {
	case CheckComplex:
		return "ORDER BY complexity DESC, e.name, e.id\n"
	case CheckLength:
		return "ORDER BY line_count DESC, e.name, e.id\n"
	case CheckArgs:
		return "ORDER BY argument_count DESC, e.name, e.id\n"
	case CheckRefactor:
		return "ORDER BY complexity DESC, line_count DESC, argument_count DESC, e.name, e.id\n"
	default:
		return "ORDER BY e.name, e.id\n"
	}
}
