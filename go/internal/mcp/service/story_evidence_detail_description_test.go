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

// storyOmissionTotalCaveat is the sentence that keeps a truth.omissions total
// from reading as exact for the families whose own read is bounded (#8798).
const storyOmissionTotalCaveat = "for dependents, consumer_repositories, provisioning_source_chains, and uncorrelated_cloud_resources it is a lower bound when the matching *_truncated flag is set, and cloud_resources never reports past 50"

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
		if strings.Contains(description, "true total") {
			t.Fatalf("evidence_detail description = %q, must not call an omission total true", description)
		}
		return
	}
	t.Fatal("get_service_story is not registered")
}
