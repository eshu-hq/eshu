// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var errAmbiguousTraceWorkloadSelector = errors.New("deployment trace workload selector is ambiguous")

// ErrAmbiguousTraceWorkloadSelector is the exported form of
// errAmbiguousTraceWorkloadSelector: the impact package matches it with
// errors.Is from outside this package. See #6060.
var ErrAmbiguousTraceWorkloadSelector = errAmbiguousTraceWorkloadSelector

func ResolveTraceWorkloadSelector(ctx context.Context, reader querycontract.GraphQuery, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if reader == nil || selector == "" {
		return "", nil
	}
	access := querycontract.RepositoryAccessFilterFromContext(ctx)
	if access.Empty() {
		return "", nil
	}
	params := access.GraphParams(map[string]any{"service_name": selector})
	query := func(whereClause string, suffix string) (map[string]any, error) {
		whereClause = querycontract.ScopedWorkloadWhereClause(whereClause, access)
		return reader.RunSingle(ctx, fmt.Sprintf(`
			MATCH (w:Workload) WHERE %s
			RETURN w.id as id, w.name as name, w.repo_id as repo_id
			ORDER BY w.id
			%s
		`, whereClause, suffix), params)
	}
	idRow, err := query("w.id = $service_name", "LIMIT 1")
	if err != nil {
		return "", err
	}
	if idRow != nil {
		return querycontract.StringVal(idRow, "id"), nil
	}
	firstNameRow, err := query("w.name = $service_name", "LIMIT 1")
	if err != nil {
		return "", err
	}
	if firstNameRow == nil {
		return "", nil
	}
	secondNameRow, err := query("w.name = $service_name", "SKIP 1 LIMIT 1")
	if err != nil {
		return "", err
	}
	if secondNameRow != nil && querycontract.StringVal(secondNameRow, "id") != querycontract.StringVal(firstNameRow, "id") {
		return "", fmt.Errorf("%w: %q matched at least two workload ids", errAmbiguousTraceWorkloadSelector, selector)
	}
	return querycontract.StringVal(firstNameRow, "id"), nil
}
