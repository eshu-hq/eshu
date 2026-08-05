// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// infrastructureGraphReadCypherFragment uniquely identifies
// queryRepoInfrastructureFromGraph's Cypher (repository_infrastructure.go)
// among every other graph read issued by getRepositoryContext/getRepositoryStory.
const infrastructureGraphReadCypherFragment = "infra:K8sResource"

// TestGetRepositoryContextInfrastructureDegradeAttributesFailure covers
// repository_infrastructure.go's queryRepoInfrastructureFromGraph
// (#5764 site 4, ATTRIBUTED-DEGRADE): infrastructure is a genuine auxiliary
// panel, so a bounded graph-read failure keeps the 200 response but must make
// the degradation visible rather than silently returning an empty
// infrastructure list indistinguishable from "no infrastructure detected".
// Asserts all three: (1) 200 with the rest of the response intact, (2) the
// partial_reasons entry is present, (3) the stage log carries the bounded
// failure_class. Content is left nil so no read-model short-circuits the
// graph read.
func TestGetRepositoryContextInfrastructureDegradeAttributesFailure(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &RepositoryHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-infra-degrade-1", "name": "repo-infra-degrade-one"}, nil
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, infrastructureGraphReadCypherFragment) {
					return nil, fmt.Errorf("private graph detail: %w", ErrGraphReadDeadline)
				}
				return nil, nil
			},
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-infra-degrade-1/context", nil)
	req.SetPathValue("repo_id", "repo-infra-degrade-1")
	rec := httptest.NewRecorder()

	handler.getRepositoryContext(rec, req)

	// (1) 200 with the rest of the response intact.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if _, ok := body["repository"]; !ok {
		t.Fatalf("body missing repository field, rest of response not intact: %s", rec.Body.String())
	}
	infrastructure, ok := body["infrastructure"].([]any)
	if !ok || len(infrastructure) != 0 {
		t.Fatalf("body[infrastructure] = %#v, want empty list (degraded, not fabricated rows)", body["infrastructure"])
	}

	// (2) partial_reasons carries the degradation.
	partialReasons, ok := body["partial_reasons"].([]any)
	if !ok {
		t.Fatalf("body[partial_reasons] missing or wrong type: %#v", body["partial_reasons"])
	}
	if !jsonStringSliceContains(partialReasons, infrastructureReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want to contain %q", partialReasons, infrastructureReadDegradedReason)
	}

	// (3) the stage log carries the bounded failure_class.
	logText := logs.String()
	if !strings.Contains(logText, `"stage":"infrastructure"`) {
		t.Fatalf("logs missing infrastructure stage; logs = %s", logText)
	}
	if !strings.Contains(logText, `"failure_class":"`+infrastructureReadDegradedReason+`"`) {
		t.Fatalf("logs missing failure_class=%s; logs = %s", infrastructureReadDegradedReason, logText)
	}
}

// TestGetRepositoryContextInfrastructureHealthyEmptyDoesNotDegrade is the
// negative companion to the degrade test above: a healthy graph read that
// genuinely returns zero infrastructure rows (no error) must NOT add
// infrastructureReadDegradedReason to partial_reasons or emit a
// failure_class log. This is what separates "no infrastructure" from
// "couldn't read infrastructure" (#5764).
func TestGetRepositoryContextInfrastructureHealthyEmptyDoesNotDegrade(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &RepositoryHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-infra-healthy-1", "name": "repo-infra-healthy-one"}, nil
			},
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, nil
			},
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-infra-healthy-1/context", nil)
	req.SetPathValue("repo_id", "repo-infra-healthy-1")
	rec := httptest.NewRecorder()

	handler.getRepositoryContext(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	partialReasons, ok := body["partial_reasons"].([]any)
	if !ok {
		t.Fatalf("body[partial_reasons] missing or wrong type: %#v", body["partial_reasons"])
	}
	if jsonStringSliceContains(partialReasons, infrastructureReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want no %q for a healthy empty read", partialReasons, infrastructureReadDegradedReason)
	}
	if strings.Contains(logs.String(), "failure_class") {
		t.Fatalf("logs unexpectedly carry failure_class for a healthy empty read; logs = %s", logs.String())
	}
}

// TestGetRepositoryStoryInfrastructureDegradeAttributesFailure covers the
// same queryRepoInfrastructureFromGraph degradation reached through
// getRepositoryStory's "infrastructure" stage (#5764 site 4). Content is a
// zero-value fake (entities empty) so the `if h.Content != nil` block is
// entered -- required to reach the infrastructure read in the story handler
// at all -- but ListRepoEntities returns no rows, so
// queryRepoInfrastructureRows falls through to the graph read that then
// fails. Asserts all three: 200 with the story response intact, the
// limitations entry present (which attachAnswerMetadata converts into
// answer_metadata.partial_reasons), and the stage log failure_class.
func TestGetRepositoryStoryInfrastructureDegradeAttributesFailure(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &RepositoryHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-story-infra-degrade-1", "name": "repo-story-infra-degrade-one"}, nil
			},
			run: func(_ context.Context, cypher string, _ map[string]any) ([]map[string]any, error) {
				if strings.Contains(cypher, infrastructureGraphReadCypherFragment) {
					return nil, fmt.Errorf("private graph detail: %w", ErrGraphReadDeadline)
				}
				return nil, nil
			},
		},
		Content: fakePortContentStore{},
		Logger:  slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-story-infra-degrade-1/story", nil)
	req.SetPathValue("repo_id", "repo-story-infra-degrade-1")
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
	if !jsonStringSliceContains(limitations, infrastructureReadDegradedReason) {
		t.Fatalf("limitations = %#v, want to contain %q", limitations, infrastructureReadDegradedReason)
	}
	answerMetadata, ok := body["answer_metadata"].(map[string]any)
	if !ok {
		t.Fatalf("body[answer_metadata] missing or wrong type: %#v", body["answer_metadata"])
	}
	partialReasons, ok := answerMetadata["partial_reasons"].([]any)
	if !ok {
		t.Fatalf("answer_metadata[partial_reasons] missing or wrong type: %#v", answerMetadata["partial_reasons"])
	}
	if !jsonStringSliceContains(partialReasons, infrastructureReadDegradedReason) {
		t.Fatalf("answer_metadata.partial_reasons = %#v, want to contain %q", partialReasons, infrastructureReadDegradedReason)
	}

	logText := logs.String()
	if !strings.Contains(logText, `"operation":"repository_story"`) || !strings.Contains(logText, `"stage":"infrastructure"`) {
		t.Fatalf("logs missing repository_story infrastructure stage; logs = %s", logText)
	}
	if !strings.Contains(logText, `"failure_class":"`+infrastructureReadDegradedReason+`"`) {
		t.Fatalf("logs missing failure_class=%s; logs = %s", infrastructureReadDegradedReason, logText)
	}
}

// containsString reports whether a JSON-decoded []any of strings contains want.
func jsonStringSliceContains(values []any, want string) bool {
	for _, value := range values {
		if str, ok := value.(string); ok && str == want {
			return true
		}
	}
	return false
}
