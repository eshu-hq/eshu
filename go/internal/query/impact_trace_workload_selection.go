// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

var errAmbiguousTraceWorkloadSelector = errors.New("deployment trace workload selector is ambiguous")

func resolveTraceWorkloadSelector(ctx context.Context, reader GraphQuery, selector string) (string, error) {
	selector = strings.TrimSpace(selector)
	if reader == nil || selector == "" {
		return "", nil
	}
	access := repositoryAccessFilterFromContext(ctx)
	if access.empty() {
		return "", nil
	}
	params := access.graphParams(map[string]any{"service_name": selector})
	query := func(whereClause string, suffix string) (map[string]any, error) {
		whereClause = scopedWorkloadWhereClause(whereClause, access)
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
		return StringVal(idRow, "id"), nil
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
	if secondNameRow != nil && StringVal(secondNameRow, "id") != StringVal(firstNameRow, "id") {
		return "", fmt.Errorf("%w: %q matched at least two workload ids", errAmbiguousTraceWorkloadSelector, selector)
	}
	return StringVal(firstNameRow, "id"), nil
}
