// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

// No-Observability-Change: the fetchWorkloadLiveEvidence path reuses the
// existing h.Neo4j.Run driver instrumentation (neo4j.query spans,
// eshu_dp_neo4j_query_duration_seconds metric), so operators diagnose the
// bounded live-evidence probe through the same signals as every other
// deployment-trace graph read.

package query

import "context"

// fetchWorkloadLiveEvidence returns true when the graph contains at least
// one exact-live observation binding a KubernetesWorkload with a RUNS_IMAGE
// edge to the traced workload. The query is anchored on the workload id
// (backed by the Workload.id uniqueness constraint), bounded (LIMIT 1), and
// cancellable via ctx. It is a one-shot probe used by trace_deployment_chain
// to promote the deployment truth tier from config_only to runtime_confirmed.
//
// The kubernetes_correlation domain writes RUNS_IMAGE edges only from EXACT
// live outcomes (go/internal/reducer/kubernetes_correlation_edge_rows.go:164),
// so the presence of any RUNS_IMAGE edge through an INSTANCE_OF/RUNS_ON/
// OBSERVED_ON path is sufficient for the runtime_confirmed tier.
func (h *ImpactHandler) fetchWorkloadLiveEvidence(ctx context.Context, workloadID string) (bool, error) {
	if h == nil || h.Neo4j == nil || workloadID == "" {
		return false, nil
	}
	rows, err := h.Neo4j.Run(ctx, `
		OPTIONAL MATCH (w:Workload {id: $workload_id})
			<-[:INSTANCE_OF]-(:WorkloadInstance)
			-[:RUNS_ON]->(:Platform)
			<-[:OBSERVED_ON]-(kw:KubernetesWorkload)
			-[:RUNS_IMAGE]->(:OciImageManifest)
		RETURN kw IS NOT NULL AS has_live_evidence
		LIMIT 1
	`, map[string]any{"workload_id": workloadID})
	if err != nil {
		return false, err
	}
	if len(rows) == 0 {
		return false, nil
	}
	return BoolVal(rows[0], "has_live_evidence"), nil
}
