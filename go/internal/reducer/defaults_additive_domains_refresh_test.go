// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
)

// TestAdditiveDomainsRegisterValueFlowRefresh pins refresh registration: the
// code_value_flow_refresh domain registers exactly when the fixpoint
// projector is wired, carrying a handler that re-runs the global fixpoint.
func TestAdditiveDomainsRegisterValueFlowRefresh(t *testing.T) {
	t.Parallel()

	withProjector := appendAdditiveDomainDefinitions(nil, DefaultHandlers{
		CodeEvidenceHandlers: CodeEvidenceHandlers{
			ValueFlowFixpointProjector: &replayRecordingFixpointProjector{},
		},
	})
	var found *DomainDefinition
	for i, def := range withProjector {
		if def.Domain == DomainCodeValueFlowRefresh {
			found = &withProjector[i]
		}
	}
	if found == nil {
		t.Fatal("DomainCodeValueFlowRefresh not registered with fixpoint projector wired")
	}
	if found.Handler == nil {
		t.Fatal("DomainCodeValueFlowRefresh registered with nil handler")
	}

	withoutProjector := appendAdditiveDomainDefinitions(nil, DefaultHandlers{})
	for _, def := range withoutProjector {
		if def.Domain == DomainCodeValueFlowRefresh {
			t.Fatal("DomainCodeValueFlowRefresh registered without fixpoint projector")
		}
	}
}
