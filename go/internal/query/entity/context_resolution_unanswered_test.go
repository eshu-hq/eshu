// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entity

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// failingContentStore is a content store whose reads can be made to fail, to
// drive the error exits that follow a graph answer or a graph miss.
type failingContentStore struct {
	content.FakePortContentStore
	entity     *querycontract.EntityContent
	getErr     error
	byIDsError error
}

func (f failingContentStore) GetEntityContent(_ context.Context, entityID string) (*querycontract.EntityContent, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.entity != nil && f.entity.EntityID == entityID {
		found := *f.entity
		return &found, nil
	}
	return nil, nil
}

func (f failingContentStore) ListRepoEntitiesByIDs(context.Context, string, []string, int) ([]querycontract.EntityContent, error) {
	return nil, f.byIDsError
}

// assertUnanswered fails when the request left a resolved_by trace: no span
// attribute and no counter data point. wantTried < 0 means the request never
// reached the graph read and must carry no statements_tried either.
func assertUnanswered(t *testing.T, obs resolutionObservation, wantTried int) {
	t.Helper()
	if v, ok := obs.spanAttrs[resolvedBySpanAttr]; ok {
		t.Fatalf("span carries %s=%v on a request that did not answer", resolvedBySpanAttr, v)
	}
	if len(obs.counts) != 0 {
		t.Fatalf("%s = %v, want no data points", entityContextResolutionMetric, obs.counts)
	}
	got, ok := obs.spanAttrs[statementsTriedSpanAttr]
	if wantTried < 0 {
		if ok {
			t.Fatalf("span carries %s=%v on a request rejected before the graph read", statementsTriedSpanAttr, got)
		}
		return
	}
	if !ok || got.AsInt64() != int64(wantTried) {
		t.Fatalf("span %s = %v (present=%v), want %d", statementsTriedSpanAttr, got, ok, wantTried)
	}
}

// TestGetEntityContextRejectedBeforeTheGraphReadEmitsNoResolutionSignal: a 400
// for a missing id and a 404 for an empty scoped grant never reach the graph,
// so they carry neither span attribute and are not counted.
func TestGetEntityContextRejectedBeforeTheGraphReadEmitsNoResolutionSignal(t *testing.T) {
	t.Parallel()

	reads := 0
	newHandler := func() *Handler {
		return &Handler{
			GraphBackend: querycontract.GraphBackendNeo4j,
			Neo4j: graph.FakeGraphReader{
				RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) {
					reads++
					return hitRow("fn-1"), nil
				},
			},
			Profile: querycontract.ProfileLocalAuthoritative,
		}
	}

	t.Run("missing_entity_id_is_400", func(t *testing.T) {
		obs := observeEntityContext(t, newHandler(), "")
		if obs.rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400; body=%s", obs.rec.Code, obs.rec.Body.String())
		}
		assertUnanswered(t, obs, -1)
	})
	t.Run("empty_scoped_grant_is_404", func(t *testing.T) {
		obs := observeEntityContextWithAuth(t, newHandler(), "fn-1", auth.AuthContext{Mode: auth.AuthModeScoped})
		if obs.rec.Code != http.StatusNotFound {
			t.Fatalf("status = %d, want 404; body=%s", obs.rec.Code, obs.rec.Body.String())
		}
		assertUnanswered(t, obs, -1)
	})
	if reads != 0 {
		t.Fatalf("graph reads = %d, want 0 for a request rejected before the graph read", reads)
	}
}

// TestGetEntityContextErrorAfterTheGraphReadEmitsNoResolvedBy: every error exit
// that follows the graph read (or a graph miss) answers its existing failure
// status and leaves no resolved_by, even when the graph already produced a row.
// statements_tried still reports how many statements were sent.
func TestGetEntityContextErrorAfterTheGraphReadEmitsNoResolvedBy(t *testing.T) {
	t.Parallel()

	boom := errors.New("content store down")
	grantedRow := func() map[string]any {
		row := hitRow("fn-1")
		row["repo_id"] = "repo-a"
		row["repo_name"] = "repo-a-name"
		return row
	}
	for _, tc := range []struct {
		name       string
		row        map[string]any
		content    failingContentStore
		builder    querycontract.ContentRelationshipBuilder
		wantStatus int
		wantTried  int
	}{
		{
			// The graph row lacks repo identity, so hydration reads the content
			// store and fails: 500 after the graph answered.
			name:       "hydration_read_error",
			row:        hitRow("fn-1"),
			content:    failingContentStore{getErr: boom},
			wantStatus: http.StatusInternalServerError,
			wantTried:  1,
		},
		{
			// Repo identity is present, so hydration is a no-op and the
			// metadata enrichment read fails: 500 after the graph answered.
			name:       "metadata_enrichment_error",
			row:        grantedRow(),
			content:    failingContentStore{byIDsError: boom},
			wantStatus: http.StatusInternalServerError,
			wantTried:  1,
		},
		{
			// The graph misses and the content-store fallback read fails.
			name:       "content_fallback_error",
			content:    failingContentStore{getErr: boom},
			builder:    scriptedContentRelationshipBuilder{},
			wantStatus: http.StatusInternalServerError,
			wantTried:  2,
		},
		{
			// The graph misses, content has the entity, but no relationship
			// builder is configured: 503.
			name: "content_fallback_builder_not_configured",
			content: failingContentStore{entity: &querycontract.EntityContent{
				EntityID: "fn-1", RepoID: "repo-1", RelativePath: "a.yaml", EntityType: "K8sResource", EntityName: "web",
			}},
			wantStatus: http.StatusServiceUnavailable,
			wantTried:  2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			row := tc.row
			handler := &Handler{
				GraphBackend: querycontract.GraphBackendNeo4j,
				Neo4j: graph.FakeGraphReader{
					RunSingleFn: func(context.Context, string, map[string]any) (map[string]any, error) { return row, nil },
				},
				Content:              tc.content,
				ContentRelationships: tc.builder,
				Profile:              querycontract.ProfileLocalAuthoritative,
			}
			obs := observeEntityContext(t, handler, "fn-1")

			if obs.rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", obs.rec.Code, tc.wantStatus, obs.rec.Body.String())
			}
			assertUnanswered(t, obs, tc.wantTried)
		})
	}
}
