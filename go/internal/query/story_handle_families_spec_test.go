// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"regexp"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestOpenAPIServiceStoryEvidenceDetailNamesEveryHandleFamily keeps the
// evidence_detail description in step with the lists the handles view projects:
// a family added to storyListHandleKeys without a mention in the description
// fails this test.
func TestOpenAPIServiceStoryEvidenceDetailNamesEveryHandleFamily(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}
	paths := testutil.MustMapField(t, spec, "paths")
	get := testutil.MustMapField(t, testutil.MustMapField(t, paths, "/api/v0/services/{service_name}/story"), "get")
	var description string
	params, _ := get["parameters"].([]any)
	for _, raw := range params {
		param, _ := raw.(map[string]any)
		if param["name"] == "evidence_detail" {
			description, _ = param["description"].(string)
		}
	}
	if description == "" {
		t.Fatal("getServiceStory has no evidence_detail parameter description")
	}
	for _, family := range querycontract.StoryHandleListFamilies() {
		if !namesFamily(description, family) {
			t.Errorf("evidence_detail description does not name handles family %q", family)
		}
	}
}

// namesFamily reports whether text names family as a whole token, so
// cloud_resources is not satisfied by uncorrelated_cloud_resources.
func namesFamily(text, family string) bool {
	return regexp.MustCompile(`(^|[^a-z_])` + regexp.QuoteMeta(family) + `([^a-z_]|$)`).MatchString(text)
}
