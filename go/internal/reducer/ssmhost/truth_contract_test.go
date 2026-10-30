// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ssmhost

import "testing"

// TestMaterializationDomainDefinition_truthContractValidates pins the reducer
// registration gate: a blank canonical kind fails startup with
// "canonical kind must not be blank" and stalls every domain (eshu-hq/eshu#9602
// corpus/differential neo4j stalls). The definition must validate standalone.
func TestMaterializationDomainDefinition_truthContractValidates(t *testing.T) {
	t.Parallel()

	def := MaterializationDomainDefinition()
	if err := def.TruthContract.Validate(); err != nil {
		t.Fatalf("truth contract must validate: %v", err)
	}
	if err := def.Validate(); err != nil {
		t.Fatalf("domain definition must validate: %v", err)
	}
}
