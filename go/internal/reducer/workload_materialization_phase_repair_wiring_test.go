// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestNewDefaultRegistryWiresWorkloadMaterializationRepairQueue(t *testing.T) {
	t.Parallel()

	repairQueue := &recordingSemanticEntityRepairQueue{}
	registry, err := NewDefaultRegistry(DefaultHandlers{
		FactLoader:                    &stubFactLoader{},
		WorkloadMaterializer:          NewWorkloadMaterializer(&recordingCypherExecutor{}),
		WorkloadProjectionInputLoader: &stubWorkloadProjectionInputLoader{},
		GraphProjectionRepairQueue:    repairQueue,
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v", err)
	}

	def, ok := registry.Definition(DomainWorkloadMaterialization)
	if !ok {
		t.Fatal("workload materialization definition missing")
	}
	handler, ok := def.Handler.(WorkloadMaterializationHandler)
	if !ok {
		t.Fatalf("handler type = %T, want WorkloadMaterializationHandler", def.Handler)
	}
	if handler.RepairQueue != repairQueue {
		t.Fatalf("RepairQueue = %T, want %T", handler.RepairQueue, repairQueue)
	}
}
