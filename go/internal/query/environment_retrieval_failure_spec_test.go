// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/testutil"
)

// TestOpenAPICompareAndSemanticSearchDeclareTheirFailureStatuses pins the
// #7674 slice 13 failure contract in the published spec. Both routes declare
// the shared 500 and 503 and never 499. Each operation description says what
// a 503 with and without Retry-After means and names the capability its
// reader-verdict envelope carries. POST /api/v0/search/semantic answered 500
// before this slice without declaring it; it now also answers 500 where it
// answered 503 (a scope-resolver fault) and 400 (a stored candidate that
// fails normalization), and its description says so.
func TestOpenAPICompareAndSemanticSearchDeclareTheirFailureStatuses(t *testing.T) {
	t.Parallel()

	paths := adminSpecPaths(t)
	for _, route := range []struct {
		path, capability, noRetryAfter string
	}{
		{
			path:         "/api/v0/compare/environments",
			capability:   "platform_impact.environment_compare",
			noRetryAfter: "every 503 it answers carries Retry-After",
		},
		{
			path:         "/api/v0/search/semantic",
			capability:   "semantic_search.curated_retrieval",
			noRetryAfter: "a 503 without Retry-After means",
		},
	} {
		op := testutil.MustMapField(t, testutil.MustMapField(t, paths, route.path), "post")
		responses := testutil.MustMapField(t, op, "responses")
		wantRefs := map[string]string{
			"500": "#/components/responses/InternalError",
			"503": "#/components/responses/ServiceUnavailable",
		}
		for status, want := range wantRefs {
			response, ok := responses[status].(map[string]any)
			if !ok {
				t.Errorf("post %s: no %s declared", route.path, status)
				continue
			}
			if ref, _ := response["$ref"].(string); ref != want {
				t.Errorf("post %s: %s = %v, want %s", route.path, status, response, want)
			}
		}
		if _, ok := responses["499"]; ok {
			t.Errorf("post %s: declares 499; a client cancel is never declared", route.path)
		}
		description, _ := op["description"].(string)
		for _, want := range []string{readerFenceSentence, route.noRetryAfter, "names capability " + route.capability} {
			if !strings.Contains(description, want) {
				t.Errorf("post %s: description lacks %q: %q", route.path, want, description)
			}
		}
	}
}

// TestOpenAPISemanticSearchDescribesItsStatusChanges pins the sentence that
// tells a client which failures moved to 500 in #7674 slice 13.
func TestOpenAPISemanticSearchDescribesItsStatusChanges(t *testing.T) {
	t.Parallel()

	op := testutil.MustMapField(t, testutil.MustMapField(t, adminSpecPaths(t), "/api/v0/search/semantic"), "post")
	description, _ := op["description"].(string)
	const want = "A scope-resolver fault and a stored result that fails normalization answer 500"
	if !strings.Contains(description, want) {
		t.Fatalf("description lacks %q: %q", want, description)
	}
}
