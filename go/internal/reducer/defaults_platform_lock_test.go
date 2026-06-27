// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestNewDefaultRegistryWiresPlatformGraphLocker(t *testing.T) {
	t.Parallel()

	locker := &recordingPlatformGraphLocker{}
	registry, err := NewDefaultRegistry(DefaultHandlers{
		PlatformMaterializationWriter:      &recordingPlatformMaterializationWriter{},
		FactLoader:                         &stubFactLoader{},
		InfrastructurePlatformMaterializer: NewInfrastructurePlatformMaterializer(&recordingCypherExecutor{}),
		PlatformGraphLocker:                locker,
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v, want nil", err)
	}

	def, ok := registry.Definition(DomainPlatformInfraMaterialization)
	if !ok {
		t.Fatal("platform_infra_materialization definition missing")
	}
	handler, ok := def.Handler.(PlatformInfraMaterializationHandler)
	if !ok {
		t.Fatalf("handler type = %T, want PlatformInfraMaterializationHandler", def.Handler)
	}
	if handler.PlatformGraphLocker != locker {
		t.Fatal("PlatformGraphLocker was not wired")
	}
}
