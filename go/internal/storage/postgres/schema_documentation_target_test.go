// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"
)

func TestBootstrapDefinitionsIncludeDocumentationTargetReferenceIndex(t *testing.T) {
	t.Parallel()

	var facts Definition
	for _, def := range BootstrapDefinitions() {
		if def.Name == "fact_records" {
			facts = def
			break
		}
	}
	if facts.Name == "" {
		t.Fatal("fact_records definition missing")
	}
	for _, want := range []string{
		"fact_records_documentation_target_refs_idx",
		"USING GIN (payload jsonb_path_ops)",
		"fact_kind IN ('documentation_entity_mention', 'documentation_claim_candidate', 'documentation_finding')",
		"is_tombstone = FALSE",
	} {
		if !strings.Contains(facts.SQL, want) {
			t.Fatalf("fact_records SQL missing %q", want)
		}
	}
}
