// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/workloadinstance"
)

// workloadCloudRelationshipAnchors lists each row's (workload, environment).
func workloadCloudRelationshipAnchors(rows []map[string]any) []workloadinstance.Anchor {
	anchors := make([]workloadinstance.Anchor, 0, len(rows))
	for _, row := range rows {
		anchors = append(anchors, workloadinstance.Anchor{WorkloadID: anyToString(row["workload_id"]), Environment: anyToString(row["environment"])})
	}
	return anchors
}

// workloadCloudRelationshipReadyRows counts the rows whose WorkloadInstance
// exists, which are the rows the writer's MATCH binds. With no lookup every
// row counts.
func workloadCloudRelationshipReadyRows(rows []map[string]any, eval workloadinstance.Evaluation) int {
	if len(eval.Missing) == 0 {
		return len(rows)
	}
	ready := 0
	for _, anchor := range workloadCloudRelationshipAnchors(rows) {
		if _, ok := eval.Existing[anchor]; ok {
			ready++
		}
	}
	return ready
}
