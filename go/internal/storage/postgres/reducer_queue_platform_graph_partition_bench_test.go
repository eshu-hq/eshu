// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// BenchmarkReducerPlatformGraphConflictKey measures the cost of the new
// domain-partitioned platform_graph conflict key derivation (#3672).
func BenchmarkReducerPlatformGraphConflictKey(b *testing.B) {
	intent := projector.ReducerIntent{
		ScopeID: "scope:repo:acme:backend-service",
		Domain:  reducer.DomainWorkloadMaterialization,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reducerConflictDomainKey(intent)
	}
}

// BenchmarkReducerPlatformGraphConflictKeyAllDomains measures across all 5
// platform-graph domains to confirm uniform cost.
func BenchmarkReducerPlatformGraphConflictKeyAllDomains(b *testing.B) {
	domains := []reducer.Domain{
		reducer.DomainWorkloadMaterialization,
		reducer.DomainDeploymentMapping,
		reducer.DomainWorkloadIdentity,
		reducer.DomainDeployableUnitCorrelation,
		reducer.DomainCloudAssetResolution,
	}
	intents := make([]projector.ReducerIntent, len(domains))
	for i, d := range domains {
		intents[i] = projector.ReducerIntent{
			ScopeID: "scope:repo:acme:backend-service",
			Domain:  d,
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = reducerConflictDomainKey(intents[i%len(intents)])
	}
}
