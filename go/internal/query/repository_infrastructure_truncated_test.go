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

// TestGetRepositoryContextInfrastructureTruncatedAddsReason is the P2-2
// review follow-up to #5764's infrastructure ATTRIBUTED-DEGRADE fix: a
// HEALTHY graph read that lands past repositoryInfrastructureEntityLimit rows
// (more rows exist beyond it) must add infrastructureTruncatedReason to
// partial_reasons, distinct
// from infrastructureReadDegradedReason (no error occurred here -- the read
// simply may have more rows past the bound). Before this fix, a repository
// with more than 5000 infrastructure entities silently returned a clipped
// panel while partial_reasons stayed empty, affirmatively asserting nothing
// was partial.
func TestGetRepositoryContextInfrastructureTruncatedAddsReason(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &RepositoryHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-infra-truncated-1", "name": "repo-infra-truncated-one"}, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, infrastructureGraphReadCypherFragment) {
					return nil, nil
				}
				limit := IntVal(params, "limit")
				rows := make([]map[string]any, limit)
				for i := range rows {
					rows[i] = map[string]any{"type": "K8sResource", "name": fmt.Sprintf("res-%d", i)}
				}
				return rows, nil
			},
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-infra-truncated-1/context", nil)
	req.SetPathValue("repo_id", "repo-infra-truncated-1")
	rec := httptest.NewRecorder()

	handler.getRepositoryContext(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	infrastructure, ok := body["infrastructure"].([]any)
	if !ok || len(infrastructure) != repositoryInfrastructureEntityLimit {
		t.Fatalf("len(body[infrastructure]) = %d, want %d (bounded, not fabricated empty)", len(infrastructure), repositoryInfrastructureEntityLimit)
	}

	partialReasons, ok := body["partial_reasons"].([]any)
	if !ok {
		t.Fatalf("body[partial_reasons] missing or wrong type: %#v", body["partial_reasons"])
	}
	if !jsonStringSliceContains(partialReasons, infrastructureTruncatedReason) {
		t.Fatalf("partial_reasons = %#v, want to contain %q", partialReasons, infrastructureTruncatedReason)
	}
	if jsonStringSliceContains(partialReasons, infrastructureReadDegradedReason) {
		t.Fatalf("partial_reasons = %#v, want no %q: this read did not fail", partialReasons, infrastructureReadDegradedReason)
	}

	logText := logs.String()
	if !strings.Contains(logText, `"stage":"infrastructure"`) {
		t.Fatalf("logs missing infrastructure stage; logs = %s", logText)
	}
	if !strings.Contains(logText, `"truncated":true`) {
		t.Fatalf("logs missing truncated=true; logs = %s", logText)
	}
}

// TestGetRepositoryContextInfrastructureUnderLimitDoesNotAddTruncatedReason
// is the negative companion: a healthy graph read one row under the bound
// must NOT add infrastructureTruncatedReason.
func TestGetRepositoryContextInfrastructureUnderLimitDoesNotAddTruncatedReason(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	handler := &RepositoryHandler{
		Neo4j: fakeGraphReader{
			runSingle: func(context.Context, string, map[string]any) (map[string]any, error) {
				return map[string]any{"id": "repo-infra-under-1", "name": "repo-infra-under-one"}, nil
			},
			run: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, infrastructureGraphReadCypherFragment) {
					return nil, nil
				}
				limit := IntVal(params, "limit")
				rows := make([]map[string]any, limit-1)
				for i := range rows {
					rows[i] = map[string]any{"type": "K8sResource", "name": fmt.Sprintf("res-%d", i)}
				}
				return rows, nil
			},
		},
		Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v0/repositories/repo-infra-under-1/context", nil)
	req.SetPathValue("repo_id", "repo-infra-under-1")
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
	if jsonStringSliceContains(partialReasons, infrastructureTruncatedReason) {
		t.Fatalf("partial_reasons = %#v, want no %q for a read one row under the limit", partialReasons, infrastructureTruncatedReason)
	}
	if strings.Contains(logs.String(), `"truncated":true`) {
		t.Fatalf("logs unexpectedly carry truncated=true for a read under the limit; logs = %s", logs.String())
	}
}
