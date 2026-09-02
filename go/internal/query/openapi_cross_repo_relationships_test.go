// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestOpenAPIDocumentsCrossRepoRelationshipFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}

	paths := querytestutil.MustMapField(t, spec, "paths")
	relationshipStoryPath := querytestutil.MustMapField(t, paths, "/api/v0/code/relationships/story")
	relationshipStoryPost := querytestutil.MustMapField(t, relationshipStoryPath, "post")
	relationshipStoryBody := querytestutil.MustMapField(t, querytestutil.MustMapField(t, relationshipStoryPost, "requestBody"), "content")
	relationshipStoryJSON := querytestutil.MustMapField(t, relationshipStoryBody, "application/json")
	relationshipStoryProperties := querytestutil.MustMapField(t, querytestutil.MustMapField(t, relationshipStoryJSON, "schema"), "properties")
	if _, ok := relationshipStoryProperties["cross_repo"]; !ok {
		t.Fatal("code/relationships/story request schema missing cross_repo")
	}

	callChainPath := querytestutil.MustMapField(t, paths, "/api/v0/code/call-chain")
	callChainPost := querytestutil.MustMapField(t, callChainPath, "post")
	callChainBody := querytestutil.MustMapField(t, querytestutil.MustMapField(t, callChainPost, "requestBody"), "content")
	callChainJSON := querytestutil.MustMapField(t, callChainBody, "application/json")
	callChainProperties := querytestutil.MustMapField(t, querytestutil.MustMapField(t, callChainJSON, "schema"), "properties")
	for _, field := range []string{"cross_repo", "start_repo_id", "end_repo_id"} {
		if _, ok := callChainProperties[field]; !ok {
			t.Fatalf("code/call-chain request schema missing %s", field)
		}
	}
}
