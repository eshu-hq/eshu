// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicetools

import (
	"strings"
	"testing"
)

// storyArtifactException is the sentence every statement of the handles view
// must carry: deployment artifact rows keep only their handle keys, so
// confidence is not one of the labels an artifact row keeps (#8798).
const storyArtifactException = "deployment artifact rows keep id, relationship_type, and resolved_id; get_relationship_evidence by resolved_id returns the rest, including confidence"

// storyOmissionTotalCaveat is the one sentence every statement of the handles
// view uses for a truth.omissions total: the story's own pre-cap count, a lower
// bound wherever the read behind the family was itself bounded. The per-family
// list lives on the Context evidence budget reference (#8798).
const storyOmissionTotalCaveat = "Each total is the story's own pre-cap count: a lower bound wherever the underlying read was itself bounded (a *_truncated flag or deployment_artifacts_* limitation is set, or the read stops at 50 rows with no flag), and not cut by the story's cap otherwise; the Context evidence budget reference lists the rule for each family"

// TestGetServiceStoryEvidenceDetailNamesArtifactException pins that the schema
// description of evidence_detail does not read as if deployment artifact rows
// keep their row-level truth labels. The artifact rows use the shared context
// route projection, which keeps handle keys only.
func TestGetServiceStoryEvidenceDetailNamesArtifactException(t *testing.T) {
	t.Parallel()

	for _, tool := range ContextTools() {
		if tool.Name != "get_service_story" {
			continue
		}
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("get_service_story input schema type = %T, want map[string]any", tool.InputSchema)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok {
			t.Fatalf("get_service_story properties type = %T, want map[string]any", schema["properties"])
		}
		detail, ok := properties["evidence_detail"].(map[string]any)
		if !ok {
			t.Fatalf("get_service_story evidence_detail type = %T, want map[string]any", properties["evidence_detail"])
		}
		description, _ := detail["description"].(string)
		for _, want := range []string{storyArtifactException, storyOmissionTotalCaveat} {
			if !strings.Contains(description, want) {
				t.Fatalf("evidence_detail description = %q, want it to contain %q", description, want)
			}
		}
		for _, banned := range []string{"true total", "exact only when", "exact otherwise"} {
			if strings.Contains(description, banned) {
				t.Fatalf("evidence_detail description = %q, must not say %q about an omission total", description, banned)
			}
		}
		return
	}
	t.Fatal("get_service_story is not registered")
}
