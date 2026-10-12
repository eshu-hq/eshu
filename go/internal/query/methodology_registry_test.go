// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/queryplan"
)

// The registered case inventory must agree with real production builders.
// Both directions matter: a missing case and an unregistered new emission fail.
func TestMethodologyRequiredProductionVariants(t *testing.T) {
	manifest, err := queryplan.LoadManifestFile("../queryplan/testdata/handler-hot-cypher.yaml")
	if err != nil {
		t.Fatal(err)
	}
	production := cloudResourceListQueryplanVariants()
	for name, statement := range importDependencyQueryplanVariants() {
		if _, duplicate := production[name]; duplicate {
			t.Fatalf("duplicate production variant %s", name)
		}
		production[name] = statement
	}
	seen := make(map[string]bool)
	for _, entry := range manifest.Entries {
		if entry.Contract == nil {
			continue
		}
		schema := make([]string, 0, len(entry.Contract.RequiredSchema))
		for _, name := range entry.Contract.RequiredSchema {
			schema = append(schema, fmt.Sprintf("CREATE INDEX %s ON fixture(identity)", name))
		}
		for _, candidate := range entry.Contract.RequiredCases {
			statement, exists := production[candidate.VariantID]
			if !exists {
				t.Fatalf("required production variant %s absent", candidate.VariantID)
			}
			if err := queryplan.ValidatePilotCaseQuery(entry.QueryKind, statement, *entry.Contract, candidate, schema); err != nil {
				t.Fatalf("%s/%s: %v", candidate.VariantID, candidate.CaseID, err)
			}
			seen[candidate.VariantID] = true
		}
	}
	for name := range production {
		if !seen[name] {
			t.Errorf("production variant %s has no executable contract case", name)
		}
	}
	if len(seen) != 344 {
		t.Fatalf("required production variants=%d, want 344", len(seen))
	}
}
