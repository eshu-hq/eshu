// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kuberneteslive

import (
	"context"
	"fmt"
	"testing"
)

// benchWorkloads builds n synthetic WorkloadObjects for the given resource
// (apiGroup/resource) so the benchmarks below can compare collectWorkloads
// cost as the number of listed kinds grows from 3 (pre-#5433) to 7
// (post-#5433), without duplicating the production collectWorkloads code path.
func benchWorkloads(apiGroup, resource string, n int) []WorkloadObject {
	items := make([]WorkloadObject, n)
	for i := range items {
		items[i] = WorkloadObject{
			Meta: ObjectMeta{
				APIGroup: apiGroup, Version: "v1", Resource: resource,
				Namespace: "bench", Name: fmt.Sprintf("%s-%d", resource, i),
				UID: fmt.Sprintf("uid-%s-%d", resource, i),
			},
			Containers: []ContainerSummary{{Name: "app", Image: "img:1"}},
		}
	}
	return items
}

// BenchmarkCollectWorkloadsThreeKinds measures Source.Next with only the
// pre-#5433 resource families populated (deployments, replicasets, pods);
// StatefulSet/DaemonSet/Job/CronJob still get listed but return empty
// results, mirroring how a cluster with none of the new kinds behaves today.
func BenchmarkCollectWorkloadsThreeKinds(b *testing.B) {
	const perKind = 50
	client := &fakeClient{
		namespaces:  ListResult[ObjectMeta]{Items: []ObjectMeta{{Version: "v1", Resource: "namespaces", Name: "bench", UID: "uid-ns"}}},
		deployments: ListResult[WorkloadObject]{Items: benchWorkloads("apps", "deployments", perKind)},
		replicasets: ListResult[WorkloadObject]{Items: benchWorkloads("apps", "replicasets", perKind)},
		pods:        ListResult[WorkloadObject]{Items: benchWorkloads("", "pods", perKind)},
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		source := newSource(client)
		if _, _, err := source.Next(context.Background()); err != nil {
			b.Fatalf("Next() error = %v", err)
		}
	}
}

// BenchmarkCollectWorkloadsSevenKinds measures Source.Next with all seven
// workload kinds populated (post-#5433: deployments, replicasets,
// statefulsets, daemonsets, jobs, cronjobs, pods), the same object count per
// kind as BenchmarkCollectWorkloadsThreeKinds. The delta between the two
// benchmarks isolates the marginal cost of the 4 new bounded LIST round-trips
// plus their proportional per-object mapping cost (#5433).
func BenchmarkCollectWorkloadsSevenKinds(b *testing.B) {
	const perKind = 50
	client := &fakeClient{
		namespaces:   ListResult[ObjectMeta]{Items: []ObjectMeta{{Version: "v1", Resource: "namespaces", Name: "bench", UID: "uid-ns"}}},
		deployments:  ListResult[WorkloadObject]{Items: benchWorkloads("apps", "deployments", perKind)},
		replicasets:  ListResult[WorkloadObject]{Items: benchWorkloads("apps", "replicasets", perKind)},
		statefulsets: ListResult[WorkloadObject]{Items: benchWorkloads("apps", "statefulsets", perKind)},
		daemonsets:   ListResult[WorkloadObject]{Items: benchWorkloads("apps", "daemonsets", perKind)},
		jobs:         ListResult[WorkloadObject]{Items: benchWorkloads("batch", "jobs", perKind)},
		cronjobs:     ListResult[WorkloadObject]{Items: benchWorkloads("batch", "cronjobs", perKind)},
		pods:         ListResult[WorkloadObject]{Items: benchWorkloads("", "pods", perKind)},
	}

	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		source := newSource(client)
		if _, _, err := source.Next(context.Background()); err != nil {
			b.Fatalf("Next() error = %v", err)
		}
	}
}
