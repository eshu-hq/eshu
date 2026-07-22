// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"context"
	"fmt"
	"testing"
)

// benchSelectorMatchFixture builds namespaceCount namespaces, each holding
// servicesPerNS Services and podsPerNS Pods, for the matchSelectors
// benchmarks below (issue #5437 finding 4, mirroring the
// collect_workloads_bench_test.go style used for #5433). Pods within a
// namespace are split into 4 label groups (tier-0..tier-3) so each Service's
// selector (matching exactly one tier) matches roughly podsPerNS/4 Pods in
// its own namespace and zero Pods in every other namespace — a realistic
// partial-match workload, not the degenerate all-match/no-match case.
func benchSelectorMatchFixture(namespaceCount, servicesPerNS, podsPerNS int) (services []ServiceObject, pods []WorkloadObject) {
	const tierCount = 4
	for ns := 0; ns < namespaceCount; ns++ {
		namespace := fmt.Sprintf("ns-%d", ns)
		for i := 0; i < servicesPerNS; i++ {
			services = append(services, ServiceObject{
				Meta: ObjectMeta{
					Version: "v1", Resource: "services",
					Namespace: namespace, Name: fmt.Sprintf("svc-%d-%d", ns, i),
					UID: fmt.Sprintf("uid-svc-%d-%d", ns, i),
				},
				Selector: map[string]string{"tier": fmt.Sprintf("tier-%d", i%tierCount)},
			})
		}
		for i := 0; i < podsPerNS; i++ {
			pods = append(pods, WorkloadObject{
				Meta: ObjectMeta{
					Version: "v1", Resource: "pods",
					Namespace: namespace, Name: fmt.Sprintf("pod-%d-%d", ns, i),
					UID:    fmt.Sprintf("uid-pod-%d-%d", ns, i),
					Labels: map[string]string{"tier": fmt.Sprintf("tier-%d", i%tierCount)},
				},
			})
		}
	}
	return services, pods
}

// newMatchSelectorsBenchBuilder builds a generationBuilder directly, indexed
// exactly as collectServices/collectWorkloads would leave it, so a benchmark
// measures only the matchSelectors pass itself and not the unrelated
// envelope construction every other resource family performs through the
// full Source.Next path.
func newMatchSelectorsBenchBuilder(services []ServiceObject, pods []WorkloadObject) *generationBuilder {
	builder := &generationBuilder{
		source:              &Source{},
		target:              ClusterTarget{ClusterID: "bench"},
		collectorInstanceID: "bench-collector",
		podLabelIndex:       make(map[string][]podLabelEntry),
	}
	for _, svc := range services {
		identity := identityFromMeta(builder.target.ClusterID, svc.Meta)
		builder.serviceSelectorIndex = append(builder.serviceSelectorIndex, serviceSelectorEntry{
			identity: identity,
			selector: svc.Selector,
		})
	}
	for _, pod := range pods {
		identity := identityFromMeta(builder.target.ClusterID, pod.Meta)
		builder.podLabelIndex[identity.Namespace] = append(builder.podLabelIndex[identity.Namespace], podLabelEntry{
			identity: identity,
			labels:   pod.Meta.Labels,
		})
	}
	return builder
}

// runMatchSelectorsBenchmark times repeated matchSelectors runs against a
// pre-built, pre-indexed fixture. Fixture and index construction happen
// before b.ResetTimer so only the namespace-bucketed matching pass and its
// envelope construction are measured.
func runMatchSelectorsBenchmark(b *testing.B, namespaceCount, servicesPerNS, podsPerNS int) {
	b.Helper()
	services, pods := benchSelectorMatchFixture(namespaceCount, servicesPerNS, podsPerNS)
	builder := newMatchSelectorsBenchBuilder(services, pods)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		builder.envelopes = nil
		if err := builder.matchSelectors(context.Background()); err != nil {
			b.Fatalf("matchSelectors() error = %v", err)
		}
	}
}

// BenchmarkMatchSelectorsSmallCluster measures the bucketed matchSelectors
// pass at 1/10th the namespace count of BenchmarkMatchSelectorsLargeCluster
// (5 namespaces x 20 Services x 200 Pods = 100 Services, 1,000 Pods), holding
// per-namespace density constant. Comparing this against
// BenchmarkMatchSelectorsLargeCluster isolates the effect of cluster size
// (namespace count) growth with fixed per-namespace shape, which is the
// realistic way a cluster grows — proving the pass is linear in namespace
// count rather than quadratic in total cluster-wide Service/Pod count (the
// pre-#5437-finding-4 full cross product was quadratic in the latter).
func BenchmarkMatchSelectorsSmallCluster(b *testing.B) {
	runMatchSelectorsBenchmark(b, 5, 20, 200)
}

// BenchmarkMatchSelectorsLargeCluster measures the bucketed matchSelectors
// pass at a realistic large-cluster shape: 50 namespaces x 20 Services x 200
// Pods per namespace = 1,000 Services, 10,000 Pods total (issue #5437 finding
// 4's requested shape). The per-namespace density (20 Services, 200 Pods,
// same 4-way label-tier split) matches BenchmarkMatchSelectorsSmallCluster
// exactly; only the namespace count differs (5 -> 50, 10x).
func BenchmarkMatchSelectorsLargeCluster(b *testing.B) {
	runMatchSelectorsBenchmark(b, 50, 20, 200)
}
