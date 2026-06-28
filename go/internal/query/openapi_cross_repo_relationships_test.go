// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"testing"
)

func TestOpenAPIDocumentsCrossRepoRelationshipFields(t *testing.T) {
	t.Parallel()

	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v", err)
	}

	paths := mustMapField(t, spec, "paths")
	relationshipStoryPath := mustMapField(t, paths, "/api/v0/code/relationships/story")
	relationshipStoryPost := mustMapField(t, relationshipStoryPath, "post")
	relationshipStoryBody := mustMapField(t, mustMapField(t, relationshipStoryPost, "requestBody"), "content")
	relationshipStoryJSON := mustMapField(t, relationshipStoryBody, "application/json")
	relationshipStoryProperties := mustMapField(t, mustMapField(t, relationshipStoryJSON, "schema"), "properties")
	if _, ok := relationshipStoryProperties["cross_repo"]; !ok {
		t.Fatal("code/relationships/story request schema missing cross_repo")
	}

	callChainPath := mustMapField(t, paths, "/api/v0/code/call-chain")
	callChainPost := mustMapField(t, callChainPath, "post")
	callChainBody := mustMapField(t, mustMapField(t, callChainPost, "requestBody"), "content")
	callChainJSON := mustMapField(t, callChainBody, "application/json")
	callChainProperties := mustMapField(t, mustMapField(t, callChainJSON, "schema"), "properties")
	for _, field := range []string{"cross_repo", "start_repo_id", "end_repo_id"} {
		if _, ok := callChainProperties[field]; !ok {
			t.Fatalf("code/call-chain request schema missing %s", field)
		}
	}
}
