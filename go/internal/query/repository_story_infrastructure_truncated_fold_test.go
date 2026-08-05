// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetRepositoryStoryInfrastructureTruncatedSetsTopLevelTruncated is the
// P3 review follow-up to #5764: before this fix, the story response's
// top-level "truncated" field (and therefore answer_metadata.truncated) was
// set only from storySummary.truncated (the workload/platform/language
// narrative row bound) -- infrastructure_truncated landed in limitations /
// answer_metadata.partial_reasons but never flipped "truncated" itself, the
// same affirmative-false-claim shape #5764 exists to remove from the story
// rows themselves. This proves the chosen fix: getRepositoryStory
// (repository.go) now ORs the infrastructure panel's own truncation into the
// top-level field. The fake graph reader returns few workload rows (well
// under repositoryStoryStringRowLimit, so storyRowsTruncatedReason must NOT
// fire) but repositoryInfrastructureEntityLimit+1 infrastructure rows (the
// content store is empty, so queryRepoInfrastructure falls through to this
// graph read), isolating the infrastructure-only truncation source.
func TestGetRepositoryStoryInfrastructureTruncatedSetsTopLevelTruncated(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-story-infra-trunc-1", "name": "repo-story-infra-trunc-one"}, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				switch {
				case strings.Contains(cypher, storyWorkloadNamesCypherFragment):
					return []map[string]any{{"workload_name": "checkout"}}, nil
				case strings.Contains(cypher, infrastructureGraphReadCypherFragment):
					limit := IntVal(params, "limit")
					rows := make([]map[string]any, limit)
					for i := range rows {
						rows[i] = map[string]any{"type": "K8sResource", "name": fmt.Sprintf("res-%d", i)}
					}
					return rows, nil
				default:
					return nil, nil
				}
			},
		},
		Content: fakePortContentStore{},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story-infra-trunc-1/story", nil)
	req.SetPathValue("repo_id", "repo-story-infra-trunc-1")
	rec := httptest.NewRecorder()

	handler.getRepositoryStory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}

	limitations, ok := body["limitations"].([]any)
	if !ok {
		t.Fatalf("body[limitations] missing or wrong type: %#v", body["limitations"])
	}
	if !jsonStringSliceContains(limitations, infrastructureTruncatedReason) {
		t.Fatalf("limitations = %#v, want to contain %q", limitations, infrastructureTruncatedReason)
	}
	if jsonStringSliceContains(limitations, storyRowsTruncatedReason) {
		t.Fatalf("limitations = %#v, want no %q (only infrastructure should be truncated)", limitations, storyRowsTruncatedReason)
	}

	truncated, ok := body["truncated"].(bool)
	if !ok || !truncated {
		t.Fatalf("body[truncated] = %#v, want true when only infrastructure_truncated fired", body["truncated"])
	}

	answerMetadata, ok := body["answer_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("body[answer_metadata] missing or wrong type: %#v", body["answer_metadata"])
	}
	if metadataTruncated, _ := answerMetadata["truncated"].(bool); !metadataTruncated {
		t.Fatalf("answer_metadata.truncated = %#v, want true when only infrastructure_truncated fired", answerMetadata["truncated"])
	}
}
