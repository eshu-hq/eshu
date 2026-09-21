// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package refresh

import (
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestDefinitionRegistersRefreshDomain pins the additive domain definition:
// the refresh domain with a canonical-write truth surface over the
// re-projected cloud-sink edges.
func TestDefinitionRegistersRefreshDomain(t *testing.T) {
	t.Parallel()

	def := Definition()
	if def.Domain != reducercontract.DomainCodeValueFlowRefresh {
		t.Fatalf("Definition().Domain = %q, want %q", def.Domain, reducercontract.DomainCodeValueFlowRefresh)
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("Definition().Validate() = %v, want nil", err)
	}
	if !def.Ownership.CanonicalWrite {
		t.Error("Definition().Ownership.CanonicalWrite = false, want true: the fixpoint rewrites cloud-sink edges")
	}
}
