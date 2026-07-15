// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDeadCodeCandidateEntityTypeMapsEveryAdvertisedLabel(t *testing.T) {
	t.Parallel()

	for _, label := range deadCodeCandidateLabels {
		label := label
		t.Run(label, func(t *testing.T) {
			t.Parallel()

			got, ok := deadCodeCandidateEntityType(label)
			if !ok || got != label {
				t.Fatalf("deadCodeCandidateEntityType(%q) = %q, %v; want %q, true", label, got, ok, label)
			}
		})
	}
}

func TestContentReaderDeadCodeCandidateRowsKeepsTraitTypeAndRepositoryScope(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentReaderDB(t, []recordingContentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "entity_name", "entity_type", "repo_id", "relative_path",
				"language", "start_line", "end_line", "metadata",
			},
			rows: [][]driver.Value{
				{"trait-1", "Payments", "Trait", "repo-1", "src/payments.scala", "scala", 10, 20, []byte(`{}`)},
			},
		},
	})

	reader := NewContentReader(db)
	rows, err := reader.DeadCodeCandidateRows(context.Background(), "repo-1", "Trait", "scala", 10, 0)
	if err != nil {
		t.Fatalf("DeadCodeCandidateRows() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := StringSliceVal(rows[0], "labels"), []string{"Trait"}; !equalStringSlices(got, want) {
		t.Fatalf("labels = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][0], driver.Value("repo-1"); got != want {
		t.Fatalf("repo argument = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][1], driver.Value("Trait"); got != want {
		t.Fatalf("entity type argument = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeReportsTotalAndPerLabelCandidateScanLimits(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(context.Context, string, map[string]any) ([]map[string]any, error) {
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
	perLabel := deadCodeCandidateScanLimit(50)
	if got, want := response["candidate_scan_limit_per_label"], float64(perLabel); got != want {
		t.Fatalf("candidate_scan_limit_per_label = %#v, want %#v", got, want)
	}
	if got, want := response["candidate_scan_limit"], float64(perLabel*len(deadCodeCandidateLabels)); got != want {
		t.Fatalf("candidate_scan_limit = %#v, want %#v", got, want)
	}
}

func TestHandleDeadCodeDistinguishesDisplayAndCandidateScanTruncation(t *testing.T) {
	t.Parallel()

	scanLimit := deadCodeCandidateScanLimit(2)
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

	handler := &CodeHandler{
		Profile: ProfileLocalAuthoritative,
		Neo4j: fakeGraphReader{
			run: func(_ context.Context, _ string, params map[string]any) ([]map[string]any, error) {
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
			entities: map[string]EntityContent{
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
	if got, want := resp["candidate_scan_limit"], float64(scanLimit*len(deadCodeCandidateLabels)); got != want {
		t.Fatalf("resp[candidate_scan_limit] = %#v, want %#v", got, want)
	}
}
