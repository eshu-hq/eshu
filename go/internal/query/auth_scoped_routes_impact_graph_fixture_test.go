// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

// impactEvidenceWorkloadRepositoryRows supplies the repository candidate that
// the production workload resolver now requires before reading repository-
// owned evidence. The authorization tests still vary the downstream row that
// attempts to cross the repository boundary.
func impactEvidenceWorkloadRepositoryRows(cypher string) ([]map[string]any, bool) {
	if !strings.Contains(cypher, "MATCH (w:Workload {id: $workload_id})<-[:DEFINES]-(r:Repository)") {
		return nil, false
	}
	return []map[string]any{{"repo_id": "repo-a", "repo_name": "orders-api-repo"}}, true
}
