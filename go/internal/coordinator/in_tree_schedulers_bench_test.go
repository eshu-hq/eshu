// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"fmt"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// inTreeTargetsConfiguration builds an in-tree target list of the given size, the
// shape the in-tree planners read as their own configuration.
func inTreeTargetsConfiguration(targets int) string {
	var builder strings.Builder
	builder.WriteString(`{"targets":[`)
	for index := 0; index < targets; index++ {
		if index > 0 {
			builder.WriteByte(',')
		}
		fmt.Fprintf(&builder, `{"scope_id":"scope-%04d","uri":"https://example.invalid/%04d","labels":{"team":"platform"}}`, index, index)
	}
	builder.WriteString(`]}`)
	return builder.String()
}

type benchmarkConfiguration struct {
	name          string
	configuration string
}

// benchmarkConfigurations are the instance configurations every selector pays
// for each reconcile cycle: a small and a large in-tree target list, and a
// component activation document.
func benchmarkConfigurations() []benchmarkConfiguration {
	return []benchmarkConfiguration{
		{"in-tree 10 targets", inTreeTargetsConfiguration(10)},
		{"in-tree 1000 targets", inTreeTargetsConfiguration(1000)},
		{"component document", testComponentActivationConfig},
	}
}

// BenchmarkInTreeSchedulable measures the selector rule with the component
// exclusion: the kind and gate checks, then one configuration parse.
func BenchmarkInTreeSchedulable(b *testing.B) {
	for _, tc := range benchmarkConfigurations() {
		instance := selectorTestInstance(scope.CollectorGrafana, tc.configuration)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(tc.configuration)))
			for i := 0; i < b.N; i++ {
				_ = inTreeSchedulable(instance, scope.CollectorGrafana)
			}
		})
	}
}

// BenchmarkInTreeSchedulableGatesOnly is the baseline: the kind, enabled and
// claims checks the selectors applied before the exclusion, with no parse.
func BenchmarkInTreeSchedulableGatesOnly(b *testing.B) {
	for _, tc := range benchmarkConfigurations() {
		instance := selectorTestInstance(scope.CollectorGrafana, tc.configuration)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = gatesOnly(instance, scope.CollectorGrafana)
			}
		})
	}
}

// gatesOnly is the pre-#8919 selector rule, kept here as the benchmark baseline.
func gatesOnly(instance workflow.CollectorInstance, kind scope.CollectorKind) bool {
	return instance.CollectorKind == kind && instance.Enabled && instance.ClaimsEnabled
}
