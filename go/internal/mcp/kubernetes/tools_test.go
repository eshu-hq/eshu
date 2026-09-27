// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package kubernetestools

import (
	"testing"
)

func TestToolsOwnsOneDefinitionInRegistrationOrder(t *testing.T) {
	t.Parallel()

	tools := Tools()
	if len(tools) != 1 {
		t.Fatalf("Tools() returned %d definitions, want 1", len(tools))
	}
	if tools[0].Name != "list_kubernetes_correlations" {
		t.Fatalf("Tools()[0].Name = %q, want %q", tools[0].Name, "list_kubernetes_correlations")
	}
}
