// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package crossscope

import (
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestCollectorKindForProducerDomainPinsScopeConstants keeps the string
// map values identical to the scope constants they name: the map stays
// free of the scope import, but a typo would silently unmap a producer and
// answer its waits as ready.
func TestCollectorKindForProducerDomainPinsScopeConstants(t *testing.T) {
	t.Parallel()

	for producer, want := range map[reducercontract.Domain]scope.CollectorKind{
		reducercontract.DomainContainerImageIdentity: scope.CollectorOCIRegistry,
		reducercontract.DomainCICDRunCorrelation:     scope.CollectorCICDRun,
	} {
		got, ok := CollectorKindForProducerDomain(producer)
		if !ok {
			t.Errorf("CollectorKindForProducerDomain(%q) unmapped, want %q", producer, want)
			continue
		}
		if got != string(want) {
			t.Errorf("CollectorKindForProducerDomain(%q) = %q, want %q", producer, got, want)
		}
	}
	if _, ok := CollectorKindForProducerDomain(reducercontract.DomainWorkloadMaterialization); ok {
		t.Errorf("CollectorKindForProducerDomain(workload_materialization) mapped, want unmapped")
	}
}
