// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package relationships

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
)

func TestDiscoverEvidenceRejectsUnsupportedCodegraphFileSchemaMajor(t *testing.T) {
	t.Parallel()

	envelopes := []facts.Envelope{
		{
			FactKind:      factschema.FactKindCodegraphFile,
			ScopeID:       "repo-infra",
			SchemaVersion: "2.0.0",
			Payload: map[string]any{
				"repo_id":          "repo-infra",
				"artifact_type":    "terraform",
				"relative_path":    "main.tf",
				"parsed_file_data": map[string]any{},
				"content":          `app_repo = "payments-service"`,
			},
		},
	}
	catalog := []CatalogEntry{
		{RepoID: "repo-payments", Aliases: []string{"payments-service", "payments"}},
	}

	evidence := DiscoverEvidence(envelopes, catalog)
	if len(evidence) != 0 {
		t.Fatalf("len = %d, want 0 for unsupported file schema major: %#v", len(evidence), evidence)
	}
}
