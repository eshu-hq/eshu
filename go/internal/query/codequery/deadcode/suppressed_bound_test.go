// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/content"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/graph"
)

// suppressedBoundFixture returns count modeled-root (suppressed) Python
// candidates in the producer repository, plus the rows the candidate scan
// serves for them.
func suppressedBoundFixture(count int) (map[string]deadcode.EntityContent, []map[string]any) {
	entities := make(map[string]deadcode.EntityContent, count)
	rows := make([]map[string]any, 0, count)
	for index := 0; index < count; index++ {
		entityID := fmt.Sprintf("py-route-%02d", index)
		path := fmt.Sprintf("api/route_%02d.py", index)
		entities[entityID] = deadcode.EntityContent{
			EntityID:     entityID,
			RepoID:       "repo-producer",
			RelativePath: path,
			EntityType:   "Function",
			EntityName:   entityID,
			StartLine:    30,
			EndLine:      35,
			Language:     "python",
			SourceCache:  "@app.route('/pay')\ndef handler(): pass",
			Metadata:     map[string]any{"dead_code_root_kinds": []string{"python.flask_route_decorator"}},
		}
		rows = append(rows, deadCodeInvestigationRow(entityID, entityID, "python", path, 30, 35))
	}
	return entities, rows
}

func postSuppressedBoundRequest(t *testing.T, handler *codequery.CodeHandler, path, body string) map[string]any {
	t.Helper()

	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	return testutil.DecodeEnvelopeData(t, w.Body.Bytes())
}

func newSuppressedBoundInvestigationHandler(count int) *codequery.CodeHandler {
	entities, rows := suppressedBoundFixture(count)
	return &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j:   graph.FakeGraphReader{},
		Content: &contentCandidateDeadCodeStore{
			fakeDeadCodeContentStore: fakeDeadCodeContentStore{
				FakePortContentStore: content.FakePortContentStore{
					Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repo-1", Name: "payments"}},
				},
				entities: entities,
			},
			rows: rows,
		},
	}
}

func newSuppressedBoundCrossRepoHandler(count int) *codequery.CodeHandler {
	entities, rows := suppressedBoundFixture(count)
	return &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j:   graph.FakeGraphReader{},
		Content: &crossRepoDeadCodeContentStore{
			fakeDeadCodeContentStore: fakeDeadCodeContentStore{
				FakePortContentStore: content.FakePortContentStore{
					Repositories: []querycontract.RepositoryCatalogEntry{{ID: "repo-producer", Name: "payments-lib"}},
				},
				entities: entities,
			},
			rows: rows,
		},
	}
}

// TestInvestigationSuppressedBucketIsBoundedByLimit pins #7168: the suppressed
// bucket no longer holds up to 50 rows regardless of `limit`. It is capped at
// min(limit, 50), and the response says both that it was cut and where the
// cap sat, so a caller can tell a bound from a coincidence.
func TestInvestigationSuppressedBucketIsBoundedByLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		limit         int
		wantRows      int
		wantLimit     float64
		wantTruncated bool
	}{
		{name: "limit one", limit: 1, wantRows: 1, wantLimit: 1, wantTruncated: true},
		{name: "limit below cap", limit: 7, wantRows: 7, wantLimit: 7, wantTruncated: true},
		{name: "limit above cap keeps the 50 row ceiling", limit: 100, wantRows: 50, wantLimit: 50, wantTruncated: true},
		{name: "limit above the fixture is not truncated", limit: 100, wantRows: 50, wantLimit: 50, wantTruncated: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			count := 60
			if !tt.wantTruncated {
				count = 12
				tt.wantRows = count
			}
			data := postSuppressedBoundRequest(
				t,
				newSuppressedBoundInvestigationHandler(count),
				"/api/v0/code/dead-code/investigate",
				fmt.Sprintf(`{"repo_id":"payments","limit":%d}`, tt.limit),
			)
			buckets := requireDeadCodeInvestigationMap(t, data, "candidate_buckets")
			suppressed := requireDeadCodeInvestigationSlice(t, buckets, "suppressed")
			if got := len(suppressed); got != tt.wantRows {
				t.Fatalf("len(suppressed) = %d, want %d", got, tt.wantRows)
			}
			if got := data["suppressed_truncated"]; got != tt.wantTruncated {
				t.Fatalf("suppressed_truncated = %#v, want %v", got, tt.wantTruncated)
			}
			if got := data["suppressed_limit"]; got != tt.wantLimit {
				t.Fatalf("suppressed_limit = %#v, want %v", got, tt.wantLimit)
			}
			counts := requireDeadCodeInvestigationMap(t, data, "bucket_counts")
			if got := counts["suppressed_truncated"]; got != tt.wantTruncated {
				t.Fatalf("bucket_counts.suppressed_truncated = %#v, want %v", got, tt.wantTruncated)
			}
			coverage := requireDeadCodeInvestigationMap(t, data, "coverage")
			if got := coverage["suppressed_limit"]; got != tt.wantLimit {
				t.Fatalf("coverage.suppressed_limit = %#v, want %v", got, tt.wantLimit)
			}
		})
	}
}

// TestCrossRepoSuppressedBucketIsBoundedByLimit pins the same bound on the
// cross-repo route, whose scan previously appended every suppressed row with
// no cap at all.
func TestCrossRepoSuppressedBucketIsBoundedByLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		limit         int
		count         int
		wantRows      int
		wantLimit     float64
		wantTruncated bool
	}{
		{name: "limit one", limit: 1, count: 60, wantRows: 1, wantLimit: 1, wantTruncated: true},
		{name: "limit above cap keeps the 50 row ceiling", limit: 100, count: 60, wantRows: 50, wantLimit: 50, wantTruncated: true},
		{name: "fixture under the bound is not truncated", limit: 100, count: 12, wantRows: 12, wantLimit: 50, wantTruncated: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := postSuppressedBoundRequest(
				t,
				newSuppressedBoundCrossRepoHandler(tt.count),
				"/api/v0/code/dead-code/cross-repo",
				fmt.Sprintf(`{"repo_id":"repo-producer","limit":%d}`, tt.limit),
			)
			buckets := requireDeadCodeInvestigationMap(t, data, "candidate_buckets")
			suppressed := requireDeadCodeInvestigationSlice(t, buckets, "suppressed")
			if got := len(suppressed); got != tt.wantRows {
				t.Fatalf("len(suppressed) = %d, want %d", got, tt.wantRows)
			}
			if got := data["suppressed_truncated"]; got != tt.wantTruncated {
				t.Fatalf("suppressed_truncated = %#v, want %v", got, tt.wantTruncated)
			}
			if got := data["suppressed_limit"]; got != tt.wantLimit {
				t.Fatalf("suppressed_limit = %#v, want %v", got, tt.wantLimit)
			}
		})
	}
}
