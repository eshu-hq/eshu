// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicetools

import (
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestGetServiceStoryEvidenceDetailNamesEveryHandleFamily keeps the
// get_service_story tool text in step with the lists the handles view
// projects: a family added to the shaper without a mention here fails this test.
func TestGetServiceStoryEvidenceDetailNamesEveryHandleFamily(t *testing.T) {
	t.Parallel()

	var description string
	for _, def := range ContextTools() {
		if def.Name != "get_service_story" {
			continue
		}
		schema, _ := def.InputSchema.(map[string]any)
		props, _ := schema["properties"].(map[string]any)
		detail, _ := props["evidence_detail"].(map[string]any)
		description, _ = detail["description"].(string)
	}
	if description == "" {
		t.Fatal("get_service_story has no evidence_detail description")
	}
	for _, family := range querycontract.StoryHandleListFamilies() {
		if !namesFamily(description, family) {
			t.Errorf("get_service_story evidence_detail text does not name handles family %q", family)
		}
	}
}

// namesFamily reports whether text names family as a whole token, so
// cloud_resources is not satisfied by uncorrelated_cloud_resources.
func namesFamily(text, family string) bool {
	return regexp.MustCompile(`(^|[^a-z_])` + regexp.QuoteMeta(family) + `([^a-z_]|$)`).MatchString(text)
}
