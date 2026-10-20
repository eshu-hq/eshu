// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// BuildRunsInRowMap converts a runs_in intent payload into the flat UNWIND
// parameter map for the RUNS_IN upsert. It skips the row (ok=false) when any
// MATCH key — function_id or repo_id — is empty so an unresolvable edge is never
// written. The ambiguous flag rides through verbatim: the reducer marks every
// edge ambiguous=true because it cannot count the repo's materialized Workloads
// at intent-build time, so the edge is always a candidate-set member.
func BuildRunsInRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	functionID := PayloadString(payload, "function_id")
	repoID := PayloadString(payload, "repo_id")
	if functionID == "" || repoID == "" {
		return "", nil, false
	}
	return node.BatchCanonicalRunsInEdgeUpsertCypher, map[string]any{
		"function_id":       functionID,
		"repo_id":           repoID,
		"resolution_method": PayloadString(payload, "resolution_method"),
		"confidence":        PayloadFloat(payload, "confidence"),
		"ambiguous":         PayloadBool(payload, "ambiguous"),
		"evidence_source":   evidenceSource,
	}, true
}
