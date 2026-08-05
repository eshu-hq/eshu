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

// storyWorkloadNamesCypherFragment uniquely identifies
// queryRepositoryStoryWorkloadNames's Cypher (repository_story_counts.go)
// among every other graph read issued by getRepositoryStory.
const storyWorkloadNamesCypherFragment = "RETURN DISTINCT w.name AS workload_name"

// TestGetRepositoryStoryRowsTruncatedIsDisclosed is the semantic guard for
// the P1 review follow-up to #5764: before this fix, a HEALTHY
// queryRepositoryStoryStringRows read that landed on
// repositoryStoryStringRowLimit was silently reported as the exact
// workload/platform count (len() of the returned list), with
// answer_metadata.truncated staying false and no reason disclosed anywhere --
// the exact fabricated-value defect #5764 exists to remove, reintroduced by
// the new bound itself. The fake reader below simulates a backend honoring
// the Cypher's own `LIMIT $limit` clause: it returns exactly
// repositoryStoryStringRowLimit+1 distinct workload rows, matching a
// repository with more real workloads than the bound discloses.
//
// Asserts all four: (1) 200 with the story response intact, (2) limitations
// contains storyRowsTruncatedReason, (3) answer_metadata.partial_reasons
// contains storyRowsTruncatedReason, (4) answer_metadata.truncated is true --
// the exact claim the P1 review flagged as false before this fix.
func TestGetRepositoryStoryRowsTruncatedIsDisclosed(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-story-rows-trunc-1", "name": "repo-story-rows-trunc-one"}, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, storyWorkloadNamesCypherFragment) {
					return nil, nil
				}
				limit := IntVal(params, "limit")
				rows := make([]map[string]any, 0, limit)
				for i := 0; i < limit; i++ {
					rows = append(rows, map[string]any{"workload_name": fmt.Sprintf("workload-%04d", i)})
				}
				return rows, nil
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story-rows-trunc-1/story", nil)
	req.SetPathValue("repo_id", "repo-story-rows-trunc-1")
	rec := httptest.NewRecorder()

	handler.getRepositoryStory(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := body["story"]; !ok {
		t.Fatalf("body missing story field, rest of response not intact: %s", rec.Body.String())
	}

	limitations, ok := body["limitations"].([]any)
	if !ok {
		t.Fatalf("body[limitations] missing or wrong type: %#v", body["limitations"])
	}
	if !jsonStringSliceContains(limitations, storyRowsTruncatedReason) {
		t.Fatalf("limitations = %#v, want to contain %q", limitations, storyRowsTruncatedReason)
	}

	answerMetadata, ok := body["answer_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("body[answer_metadata] missing or wrong type: %#v", body["answer_metadata"])
	}
	partialReasons, ok := answerMetadata["partial_reasons"].([]any)
	if !ok {
		t.Fatalf("answer_metadata[partial_reasons] missing or wrong type: %#v", answerMetadata["partial_reasons"])
	}
	if !jsonStringSliceContains(partialReasons, storyRowsTruncatedReason) {
		t.Fatalf("answer_metadata.partial_reasons = %#v, want to contain %q", partialReasons, storyRowsTruncatedReason)
	}
	truncated, ok := answerMetadata["truncated"].(bool)
	if !ok || !truncated {
		t.Fatalf("answer_metadata.truncated = %#v, want true", answerMetadata["truncated"])
	}
}

// TestGetRepositoryStoryRowsHealthyUnderLimitDoesNotDisclose is the negative
// companion: a healthy read genuinely under the bound must NOT add
// storyRowsTruncatedReason or claim answer_metadata.truncated=true. This is
// what separates "the repository genuinely has this many workloads" from
// "the read was capped" (P1 review follow-up to #5764).
func TestGetRepositoryStoryRowsHealthyUnderLimitDoesNotDisclose(t *testing.T) {
	t.Parallel()

	handler := &RepositoryHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-story-rows-ok-1", "name": "repo-story-rows-ok-one"}, nil
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, storyWorkloadNamesCypherFragment) {
					return nil, nil
				}
				return []map[string]any{{"workload_name": "checkout"}, {"workload_name": "payments"}}, nil
			},
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story-rows-ok-1/story", nil)
	req.SetPathValue("repo_id", "repo-story-rows-ok-1")
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
	if jsonStringSliceContains(limitations, storyRowsTruncatedReason) {
		t.Fatalf("limitations = %#v, want no %q for a healthy under-limit read", limitations, storyRowsTruncatedReason)
	}
	answerMetadata, ok := body["answer_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("body[answer_metadata] missing or wrong type: %#v", body["answer_metadata"])
	}
	if truncated, _ := answerMetadata["truncated"].(bool); truncated {
		t.Fatalf("answer_metadata.truncated = true, want false for a healthy under-limit read")
	}
}
