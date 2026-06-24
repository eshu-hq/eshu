// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestNewDefaultRegistryWiresPlatformGraphLocker(t *testing.T) {
	t.Parallel()

	locker := &recordingPlatformGraphLocker{}
	registry, err := NewDefaultRegistry(DefaultHandlers{
		PlatformMaterializationWriter: &recordingPlatformMaterializationWriter{},
		PlatformGraphLocker:           locker,
	})
	if err != nil {
		t.Fatalf("NewDefaultRegistry() error = %v, want nil", err)
	}

	def, ok := registry.Definition(DomainDeploymentMapping)
	if !ok {
		t.Fatal("deployment_mapping definition missing")
	}
	handler, ok := def.Handler.(PlatformMaterializationHandler)
	if !ok {
		t.Fatalf("handler type = %T, want PlatformMaterializationHandler", def.Handler)
	}
	if handler.PlatformGraphLocker != locker {
		t.Fatal("PlatformGraphLocker was not wired")
	}
}
