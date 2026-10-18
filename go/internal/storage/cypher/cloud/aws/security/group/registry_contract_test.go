// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package group

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// TestContractFamilyDerivedTypesAreCanonicalEdgeTypes proves the closed
// static types behind the security-group kind-C derivation name edges the
// graph can carry: a typo'd or invented type that is internally consistent
// between the registry and its retract would make the live gate assert an
// always-empty population. This is the security-group slice of the root
// cypher package's contract test (retract_registry_contract_test.go), moved
// with the writer: the root test cannot import this leaf (the leaf imports
// the root), so each side pins its own families.
func TestContractFamilyDerivedTypesAreCanonicalEdgeTypes(t *testing.T) {
	t.Parallel()

	canonical := map[string]struct{}{}
	for _, e := range edgetype.All() {
		canonical[string(e)] = struct{}{}
	}
	if len(canonical) == 0 {
		t.Fatal("edgetype.All() is empty; this test would pass vacuously")
	}
	derived := map[string]struct{}{}
	for relType := range SecurityGroupSGRuleRelationshipTypes() {
		derived[relType] = struct{}{}
	}
	if len(derived) == 0 {
		t.Fatal("SecurityGroupSGRuleRelationshipTypes() is empty; this test would pass vacuously")
	}
	for relType := range derived {
		if _, ok := canonical[relType]; !ok {
			t.Errorf("derived type %q is not a canonical edgetype; the live gate would assert an always-empty population for it", relType)
		}
	}
}
