// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"strings"
	"testing"
)

// BenchmarkK8sWorkloadMatchTargetDirectedScan is the committed enforcement of
// the #5363 D2 performance contract: the prepared-target directed match loop
// (one 20-label workload, 5000 candidate Services, all in the same namespace so
// there is no early namespace short-circuit -- the matcher worst case) must
// stay well under 10 ms/op. It measures the design's load-bearing property: the
// workload's pod-template labels are parsed ONCE per op (newK8sWorkloadMatchTarget
// outside the candidate loop), so each candidate parses only its own selector.
// The prove-the-theory shim measured 5.57 ms/op for this shape versus 16.13
// ms/op when k8sSelectMatch re-parsed the workload labels per candidate; see
// evidence-5363-impact-trace-k8s-fetch.md. There is no CI wall-clock assert
// (that would be a flake generator); the recorded ns/op is the enforcement.
func BenchmarkK8sWorkloadMatchTargetDirectedScan(b *testing.B) {
	labels := make([]string, 0, 20)
	for i := range 20 {
		labels = append(labels, fmt.Sprintf("k%02d=v%02d", i, i))
	}
	workload := k8sSelectMatchInput{
		kind:                     "Deployment",
		name:                     "web",
		namespace:                "ns-0",
		podTemplateLabels:        strings.Join(labels, ","),
		podTemplateLabelsPresent: true,
	}

	candidates := make([]K8sSelectCandidate, 0, 5000)
	for i := range 5000 {
		selectorPairs := make([]string, 0, 6)
		for j := range 6 {
			key := (i + j) % 20
			selectorPairs = append(selectorPairs, fmt.Sprintf("k%02d=v%02d", key, key))
		}
		candidates = append(candidates, K8sSelectCandidate{
			EntityID:        fmt.Sprintf("svc-%04d", i),
			EntityName:      fmt.Sprintf("svc-%04d", i),
			Kind:            "Service",
			Namespace:       "ns-0",
			Selector:        strings.Join(selectorPairs, ","),
			SelectorPresent: true,
		})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		target := newK8sWorkloadMatchTarget(workload)
		matched := 0
		for _, candidate := range candidates {
			if ok, _, _ := target.Match(candidate.matchInput()); ok {
				matched++
			}
		}
		if matched == 0 {
			b.Fatal("expected at least one match in the directed scan")
		}
	}
}
