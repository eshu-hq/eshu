// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
)

func TestQueryClientChecksHTTPShapes(t *testing.T) {
	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	shapesByPath := make(map[string]QueryShape, len(snap.QueryShapes.HTTP))
	for key, shape := range snap.QueryShapes.HTTP {
		_, path, err := parseHTTPShapeKey(key)
		if err != nil {
			t.Fatal(err)
		}
		shapesByPath[path] = shape
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		shape, ok := shapesByPath[req.URL.RequestURI()]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fakeQueryShapeResponse(shape)); err != nil {
			t.Errorf("write query shape response: %v", err)
		}
	}))
	defer srv.Close()

	client := newQueryClient(srv.URL, "k")
	var r Report
	if err := checkQuery(context.Background(), client, snap, &r); err != nil {
		t.Fatalf("checkQuery err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected query shapes to pass; findings: %+v", r.Findings)
	}
}

func TestQueryClientChecksPostEnvelopeShapes(t *testing.T) {
	snap := Snapshot{QueryShapes: QueryShapes{HTTP: map[string]QueryShape{
		"POST /api/v0/code/dead-code/cross-repo": {
			Envelope:               true,
			RequestBody:            map[string]any{"repo_id": "deadcode-producer", "language": "go", "limit": float64(20)},
			RequiredResponseFields: []string{"data", "truth", "error"},
			RequiredJSONPaths: []string{
				"data.candidate_buckets.live_by_consumer[].consumer_evidence[].citation",
			},
			RequiredJSONValues: map[string]any{
				"truth.level":      "derived",
				"truth.basis":      "hybrid",
				"data.query_shape": "bounded_cross_repo_dead_code",
			},
		},
	}}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", req.Method)
		}
		if got, want := req.Header.Get("Accept"), EnvelopeMIMEType; got != want {
			t.Errorf("Accept = %q, want %q", got, want)
		}
		var body map[string]any
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["repo_id"] != "deadcode-producer" || body["language"] != "go" {
			t.Fatalf("request body = %#v, want deadcode-producer go selector", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
		  "data": {
		    "query_shape": "bounded_cross_repo_dead_code",
		    "candidate_buckets": {
		      "live_by_consumer": [{
		        "consumer_evidence": [{"citation": "code_reachability_rows:scope/gen/consumer/root/entity"}]
		      }]
		    }
		  },
		  "truth": {"level": "derived", "basis": "hybrid"},
		  "error": null
		}`))
	}))
	defer srv.Close()

	var r Report
	if err := checkQuery(context.Background(), newQueryClient(srv.URL, ""), snap, &r); err != nil {
		t.Fatalf("checkQuery err = %v", err)
	}
	if r.Failed() {
		t.Fatalf("expected POST envelope query shape to pass; findings: %+v", r.Findings)
	}
}

func fakeQueryShapeResponse(shape QueryShape) map[string]any {
	resp := make(map[string]any, len(shape.RequiredResponseFields))
	// ResultsField names the array the shape actually asserts against
	// (eshu-hq/eshu#5566); this generator must populate that exact field as
	// an array, not just the first required_response_fields entry, or a
	// shape whose asserted collection is not the first-listed field would
	// get a fixture that does not match what EvaluateQueryShape checks.
	arrayField := shape.ResultsField
	for _, field := range shape.RequiredResponseFields {
		if arrayField != "" && field == arrayField {
			count := max(shape.MinimumResults, 1)
			items := make([]map[string]any, count)
			for i := range items {
				item := make(map[string]any, len(shape.ResultItemRequiredFields))
				for _, itemField := range shape.ResultItemRequiredFields {
					item[itemField] = "value"
				}
				items[i] = item
			}
			resp[field] = items
			continue
		}
		resp[field] = map[string]any{}
	}
	for _, path := range shape.RequiredJSONPaths {
		fakeSetJSONPath(resp, path, "value")
	}
	objectMatchPaths := make([]string, 0, len(shape.RequiredJSONObjectMatches))
	for path := range shape.RequiredJSONObjectMatches {
		objectMatchPaths = append(objectMatchPaths, path)
	}
	sort.Slice(objectMatchPaths, func(i, j int) bool {
		leftDepth := strings.Count(objectMatchPaths[i], ".")
		rightDepth := strings.Count(objectMatchPaths[j], ".")
		if leftDepth == rightDepth {
			return objectMatchPaths[i] < objectMatchPaths[j]
		}
		return leftDepth < rightDepth
	})
	for _, path := range objectMatchPaths {
		matches := shape.RequiredJSONObjectMatches[path]
		fakeSetJSONObjectMatches(resp, path, matches)
	}
	for path, value := range shape.RequiredJSONValues {
		fakeSetJSONPath(resp, path, value)
	}
	return resp
}

func fakeSetJSONPath(root map[string]any, path string, value any) {
	segments := strings.Split(path, ".")
	var current any = root
	for i, rawSegment := range segments {
		last := i == len(segments)-1
		arraySegment := strings.HasSuffix(rawSegment, "[]")
		segment := strings.TrimSuffix(rawSegment, "[]")
		obj, ok := current.(map[string]any)
		if !ok || segment == "" {
			return
		}
		if arraySegment {
			if last {
				if arr, ok := obj[segment].([]any); ok {
					obj[segment] = append(arr, value)
				} else {
					obj[segment] = []any{value}
				}
				return
			}
			switch arr := obj[segment].(type) {
			case []map[string]any:
				if len(arr) > 0 {
					current = arr[0]
					continue
				}
			case []any:
				if len(arr) > 0 {
					current = arr[0]
					continue
				}
			}
			arr := []any{map[string]any{}}
			obj[segment] = arr
			current = arr[0]
			continue
		}
		if last {
			obj[segment] = value
			return
		}
		next, _ := obj[segment].(map[string]any)
		if next == nil {
			next = map[string]any{}
			obj[segment] = next
		}
		current = next
	}
}

func TestQueryClientFailsOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	snap, err := LoadSnapshot(goldenSnapshotPath())
	if err != nil {
		t.Fatal(err)
	}
	var r Report
	if err := checkQuery(context.Background(), newQueryClient(srv.URL, ""), snap, &r); err != nil {
		t.Fatalf("checkQuery err = %v", err)
	}
	if !r.Failed() {
		t.Fatal("expected failure on HTTP 500")
	}
}

func TestParseHTTPShapeKey(t *testing.T) {
	if _, p, err := parseHTTPShapeKey("GET /api/v0/repositories"); err != nil || p != "/api/v0/repositories" {
		t.Errorf("GET parse = %q, %v", p, err)
	}
	if method, p, err := parseHTTPShapeKey("POST /api/v0/code/dead-code"); err != nil || method != http.MethodPost || p != "/api/v0/code/dead-code" {
		t.Errorf("POST parse = method %q path %q err %v", method, p, err)
	}
	if _, _, err := parseHTTPShapeKey("bogus"); err == nil {
		t.Error("malformed key must be rejected")
	}
}
