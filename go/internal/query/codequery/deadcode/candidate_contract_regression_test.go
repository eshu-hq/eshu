// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
	"github.com/eshu-hq/eshu/go/internal/query/codequery/deadcode"
	"github.com/eshu-hq/eshu/go/internal/query/codeshaping"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

func TestDeadCodeCandidateEntityTypeMapsEveryAdvertisedLabel(t *testing.T) {
	t.Parallel()

	for _, label := range querycontract.DeadCodeCandidateLabels {
		label := label
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			got, ok := querycontract.DeadCodeCandidateEntityType(label)
			if !ok || got != label {
				t.Fatalf("deadCodeCandidateEntityType(%q) = %q, %v; want %q, true", label, got, ok, label)
			}
		})
	}
}

func TestHandleDeadCodeReportsSharedTotalAndPerLabelCandidateScanLimits(t *testing.T) {
	t.Parallel()

	handler := &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j: querytestutil.FakeGraphReader{
			RunFn: func(context.Context, string, map[string]any) ([]map[string]any, error) {
				return nil, nil
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"limit":50}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	perLabel := codeshaping.DeadCodeCandidateScanLimit(50)
	if got, want := response["candidate_scan_limit_per_label"], float64(perLabel); got != want {
		t.Fatalf("candidate_scan_limit_per_label = %#v, want %#v", got, want)
	}
	if got, want := response["candidate_scan_limit"], float64(perLabel); got != want {
		t.Fatalf("candidate_scan_limit = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeRejectsUnknownCandidateKind(t *testing.T) {
	t.Parallel()

	handler := &codequery.CodeHandler{Profile: querycontract.ProfileLocalAuthoritative, Neo4j: querytestutil.FakeGraphReader{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"candidate_kind":"Unknown","limit":10}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}

func TestDeadCodeScanRestrictsWorkAndMetadataToRequestedCandidateKind(t *testing.T) {
	t.Parallel()

	store := &recordingDeadCodeKindStore{}
	analyzer := newDeadCodeTestAnalyzer(store, querytestutil.FakeGraphReader{})
	scan, err := analyzer.ScanDeadCodeCandidates(context.Background(), deadcode.DeadCodeRequest{
		CandidateKind: "Trait",
		Limit:         10,
	})
	if err != nil {
		t.Fatalf("scanDeadCodeCandidates() error = %v, want nil", err)
	}
	if got, want := store.labels, []string{"Trait"}; !slices.Equal(got, want) {
		t.Fatalf("queried labels = %#v, want %#v", got, want)
	}
	if got, want := scan.CandidateScanLimit, scan.CandidateScanLimitPerLabel; got != want {
		t.Fatalf("candidate scan limit = %d, want one-label bound %d", got, want)
	}
}

type recordingDeadCodeKindStore struct {
	fakeDeadCodeContentStore
	labels []string
}

func (s *recordingDeadCodeKindStore) DeadCodeCandidateRows(
	_ context.Context,
	query codeshaping.DeadCodeCandidateQuery,
) ([]map[string]any, error) {
	label := query.Label
	s.labels = append(s.labels, label)
	return nil, nil
}

func TestHandleDeadCodeDistinguishesDisplayAndCandidateScanTruncation(t *testing.T) {
	t.Parallel()

	scanLimit := codeshaping.DeadCodeCandidateScanLimit(2)
	rawCandidates := make([]map[string]any, 0, scanLimit)
	for i := 0; i < scanLimit-1; i++ {
		rawCandidates = append(rawCandidates, map[string]any{
			"entity_id": "public-api", "name": "PublicAPI", "labels": []any{"Function"},
			"file_path": "pkg/payments/api.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
		})
	}
	rawCandidates = append(rawCandidates, map[string]any{
		"entity_id": "internal-helper", "name": "privateAlpha", "labels": []any{"Function"},
		"file_path": "internal/payments/a.go", "repo_id": "repo-1", "repo_name": "payments", "language": "go",
	})

	handler := &codequery.CodeHandler{
		Profile: querycontract.ProfileLocalAuthoritative,
		Neo4j: querytestutil.FakeGraphReader{
			RunFn: func(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
				if !strings.Contains(cypher, "e:Function") {
					return nil, nil
				}
				offset, ok := params["skip"].(int)
				if !ok {
					t.Fatalf("params[skip] type = %T, want int", params["skip"])
				}
				limit, ok := params["limit"].(int)
				if !ok {
					t.Fatalf("params[limit] type = %T, want int", params["limit"])
				}
				if offset >= len(rawCandidates) {
					return nil, nil
				}
				end := offset + limit
				if end > len(rawCandidates) {
					end = len(rawCandidates)
				}
				return rawCandidates[offset:end], nil
			},
		},
		Content: fakeDeadCodeContentStore{
			entities: map[string]deadcode.EntityContent{
				"public-api": {
					EntityID:     "public-api",
					RelativePath: "pkg/payments/api.go",
					EntityType:   "Function",
					EntityName:   "PublicAPI",
					Language:     "go",
					SourceCache:  "func PublicAPI() {}",
				},
				"internal-helper": {
					EntityID:     "internal-helper",
					RelativePath: "internal/payments/a.go",
					EntityType:   "Function",
					EntityName:   "privateAlpha",
					Language:     "go",
					SourceCache:  "func privateAlpha() {}",
				},
			},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/dead-code",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":2}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	results, ok := resp["results"].([]any)
	if !ok {
		t.Fatalf("results type = %T, want []any", resp["results"])
	}
	if got, want := len(results), 1; got != want {
		t.Fatalf("len(results) = %d, want %d", got, want)
	}
	if got, want := resp["truncated"], true; got != want {
		t.Fatalf("resp[truncated] = %#v, want %#v", got, want)
	}
	if got, want := resp["display_truncated"], false; got != want {
		t.Fatalf("resp[display_truncated] = %#v, want %#v", got, want)
	}
	if got, want := resp["candidate_scan_truncated"], true; got != want {
		t.Fatalf("resp[candidate_scan_truncated] = %#v, want %#v", got, want)
	}
	if got, want := resp["candidate_scan_limit_per_label"], float64(scanLimit); got != want {
		t.Fatalf("resp[candidate_scan_limit_per_label] = %#v, want %#v", got, want)
	}
	if got, want := resp["candidate_scan_limit"], float64(scanLimit); got != want {
		t.Fatalf("resp[candidate_scan_limit] = %#v, want %#v", got, want)
	}
}
