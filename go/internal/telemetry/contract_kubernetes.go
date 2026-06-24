// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import "slices"

const (
	// SpanQueryKubernetesCorrelations wraps reducer-owned Kubernetes workload
	// ownership and drift correlation reads from durable facts (issue #388).
	SpanQueryKubernetesCorrelations = "query.kubernetes_correlations"
)

// init inserts the Kubernetes correlation query span into the frozen span list.
// Package init runs in filename order, so this file's init runs before
// contract_service_catalog.go inserts SpanQueryServiceCatalogCorrelations. The
// primary anchor is therefore SpanQueryCICDRunCorrelations (already present),
// which contract_service_catalog.go also anchors on; service catalog inserts at
// the same position afterward and lands ahead of this span, yielding the frozen
// order ci_cd_run_correlations, service_catalog_correlations,
// kubernetes_correlations. contract_supply_chain.go then anchors on this span.
func init() {
	for idx, name := range spanNames {
		if name == SpanQueryCICDRunCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryKubernetesCorrelations)
			return
		}
	}
	for idx, name := range spanNames {
		if name == SpanQueryServiceCatalogCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryKubernetesCorrelations)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryKubernetesCorrelations)
}
