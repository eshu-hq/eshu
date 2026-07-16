// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

const argoCDCategoryProjection = `
	RETURN coalesce(n.id, '') as id, coalesce(n.name, '') as name, labels(n) as labels,
	       coalesce(n.kind, '') as kind, coalesce(n.provider, '') as provider,
	       coalesce(n.source_system, '') as source_system,
	       coalesce(n.environment, '') as environment,
	       coalesce(n.source, n.source_system, '') as source,
	       coalesce(n.config_path, '') as config_path,
	       coalesce(n.resource_type, n.data_type, '') as resource_type,
	       coalesce(n.resource_service, n.service_kind, '') as resource_service,
	       coalesce(n.resource_category, '') as resource_category,
	       coalesce(n.resource_id, '') as resource_id, coalesce(n.arn, '') as arn,
	       coalesce(n.account_id, '') as account_id, coalesce(n.region, '') as region,
	       coalesce(n.service_kind, '') as service_kind
	ORDER BY n.name, n.id, n.config_path
	LIMIT $limit
`

func isArgoCDCategoryOnly(
	query string,
	kind string,
	category string,
	provider string,
	environment string,
	resourceService string,
	resourceCategory string,
) bool {
	if !strings.EqualFold(category, "argocd") {
		return false
	}
	return query == "" && kind == "" && provider == "" && environment == "" &&
		resourceService == "" && resourceCategory == ""
}

// searchArgoCDCategoryRows avoids NornicDB's full-node scan for an OR across
// the two Argo CD labels. The reads are sequential and independently bounded;
// the second excludes dual-labeled nodes so the merged row set matches one
// broad MATCH. Fetching limit rows from each label is sufficient to select the
// first global limit rows after the deterministic merge.
func (h *InfraHandler) searchArgoCDCategoryRows(
	ctx context.Context,
	access repositoryAccessFilter,
	limit int,
) ([]map[string]any, error) {
	type labelRead struct {
		label      string
		extraWhere string
	}
	reads := []labelRead{
		{label: "ArgoCDApplication"},
		{label: "ArgoCDApplicationSet", extraWhere: " AND NOT n:ArgoCDApplication"},
	}
	params := access.graphParams(map[string]any{"limit": limit})
	rows := make([]map[string]any, 0, limit*len(reads))
	for _, read := range reads {
		cypher := "MATCH (n:" + read.label + ")\nWHERE true" +
			read.extraWhere + infraSearchScopeClause(access) + argoCDCategoryProjection
		labelRows, err := h.Neo4j.Run(ctx, cypher, params)
		if err != nil {
			return nil, fmt.Errorf("search %s resources: %w", read.label, err)
		}
		rows = append(rows, labelRows...)
	}
	sort.SliceStable(rows, func(left int, right int) bool {
		return infraSearchRowSortKey(rows[left]) < infraSearchRowSortKey(rows[right])
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func infraSearchRowSortKey(row map[string]any) string {
	return strings.Join([]string{
		StringVal(row, "name"),
		StringVal(row, "id"),
		StringVal(row, "config_path"),
		strings.Join(StringSliceVal(row, "labels"), "\x00"),
	}, "\x00")
}
