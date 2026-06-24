// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeImageGraphReader records the last query and returns canned rows so the
// image-list handler can be exercised without a graph backend.
type fakeImageGraphReader struct {
	rows       []map[string]any
	err        error
	lastCypher string
	lastParams map[string]any
}

func (f *fakeImageGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.lastCypher = cypher
	f.lastParams = params
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (*fakeImageGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func newImageRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	// Request the envelope so truth metadata is exercised.
	req.Header.Set("Accept", EnvelopeMIMEType)
	return req
}

func imageMap(id, digest, repoID, tag string) map[string]any {
	return map[string]any{
		"id":            id,
		"uid":           id,
		"digest":        digest,
		"repository_id": repoID,
		"name":          digest,
		"source_tag":    tag,
		"media_type":    "application/vnd.docker.distribution.manifest.v2+json",
		"size_bytes":    int64(2623),
		"source_system": "oci_registry",
	}
}

func decodeImageBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("json.Unmarshal: %v; body = %s", err, w.Body.String())
	}
	if data, ok := body["data"].(map[string]any); ok {
		return data
	}
	return body
}

func TestImageHandlerListHappyPath(t *testing.T) {
	t.Parallel()

	reader := &fakeImageGraphReader{rows: []map[string]any{
		imageMap("oci-descriptor://123456789012.dkr.ecr.us-east-1.amazonaws.com/svc-listings@sha256:aaa", "sha256:aaa", "oci-registry://123456789012.dkr.ecr.us-east-1.amazonaws.com/svc-listings", "4.3.0"),
	}}
	handler := &ImageHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newImageRequest("/api/v0/images?limit=5"))

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeImageBody(t, w)
	images, ok := data["images"].([]any)
	if !ok || len(images) != 1 {
		t.Fatalf("images = %#v, want 1 row", data["images"])
	}
	first := images[0].(map[string]any)
	if got, want := first["registry"], "123456789012.dkr.ecr.us-east-1.amazonaws.com"; got != want {
		t.Fatalf("registry = %#v, want %#v", got, want)
	}
	if got, want := first["repository"], "svc-listings"; got != want {
		t.Fatalf("repository = %#v, want %#v", got, want)
	}
	if got := data["truncated"]; got != false {
		t.Fatalf("truncated = %#v, want false", got)
	}
	// Handler requested limit+1 to detect truncation.
	if got, want := reader.lastParams["limit"], 6; got != want {
		t.Fatalf("query limit = %#v, want %#v", got, want)
	}
}

func TestImageHandlerListEmpty(t *testing.T) {
	t.Parallel()

	handler := &ImageHandler{Neo4j: &fakeImageGraphReader{rows: nil}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newImageRequest("/api/v0/images?limit=10"))

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeImageBody(t, w)
	images, ok := data["images"].([]any)
	if !ok || len(images) != 0 {
		t.Fatalf("images = %#v, want empty slice", data["images"])
	}
	if got := data["count"]; got != float64(0) {
		t.Fatalf("count = %#v, want 0", got)
	}
}

func TestImageHandlerListLimitValidation(t *testing.T) {
	t.Parallel()

	handler := &ImageHandler{Neo4j: &fakeImageGraphReader{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	cases := []string{"0", "-1", "201", "abc"}
	for _, raw := range cases {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, newImageRequest("/api/v0/images?limit="+raw))
		if got, want := w.Code, http.StatusBadRequest; got != want {
			t.Fatalf("limit=%s status = %d, want %d; body = %s", raw, got, want, w.Body.String())
		}
	}
}

func TestImageHandlerListDefaultsLimit(t *testing.T) {
	t.Parallel()

	reader := &fakeImageGraphReader{rows: nil}
	handler := &ImageHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newImageRequest("/api/v0/images"))

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := reader.lastParams["limit"], imageListDefaultLim+1; got != want {
		t.Fatalf("default query limit = %#v, want %#v", got, want)
	}
}

func TestImageHandlerListTruncationAndCursor(t *testing.T) {
	t.Parallel()

	// Two rows returned for limit=1 means limit+1 detected truncation.
	reader := &fakeImageGraphReader{rows: []map[string]any{
		imageMap("img-a", "sha256:a", "oci-registry://host/repo", "1.0"),
		imageMap("img-b", "sha256:b", "oci-registry://host/repo", "1.1"),
	}}
	handler := &ImageHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newImageRequest("/api/v0/images?limit=1&offset=10"))

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeImageBody(t, w)
	images := data["images"].([]any)
	if len(images) != 1 {
		t.Fatalf("images = %d rows, want 1 (truncated)", len(images))
	}
	if got := data["truncated"]; got != true {
		t.Fatalf("truncated = %#v, want true", got)
	}
	cursor, ok := data["next_cursor"].(map[string]any)
	if !ok {
		t.Fatalf("next_cursor = %#v, want object", data["next_cursor"])
	}
	if got, want := cursor["offset"], float64(11); got != want {
		t.Fatalf("next_cursor.offset = %#v, want %#v", got, want)
	}
}

func TestImageHandlerListFilters(t *testing.T) {
	t.Parallel()

	reader := &fakeImageGraphReader{rows: nil}
	handler := &ImageHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newImageRequest("/api/v0/images?limit=5&digest=sha256:zzz&repository_id=oci-registry://host/repo&tag=4.3.0"))

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := reader.lastParams["digest"], "sha256:zzz"; got != want {
		t.Fatalf("digest param = %#v, want %#v", got, want)
	}
	if got, want := reader.lastParams["repository_id"], "oci-registry://host/repo"; got != want {
		t.Fatalf("repository_id param = %#v, want %#v", got, want)
	}
	if got, want := reader.lastParams["tag"], "4.3.0"; got != want {
		t.Fatalf("tag param = %#v, want %#v", got, want)
	}
}

func TestImageHandlerBackendUnavailable(t *testing.T) {
	t.Parallel()

	handler := &ImageHandler{Neo4j: nil, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newImageRequest("/api/v0/images?limit=5"))

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSplitOCIRepositoryID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in           string
		wantRegistry string
		wantRepo     string
	}{
		{"oci-registry://host.example.com/team/api", "host.example.com", "team/api"},
		{"oci-registry://host.example.com/api", "host.example.com", "api"},
		{"host.example.com/api", "host.example.com", "api"},
		{"bare-repo", "", "bare-repo"},
		{"", "", ""},
	}
	for _, tc := range cases {
		gotRegistry, gotRepo := splitOCIRepositoryID(tc.in)
		if gotRegistry != tc.wantRegistry || gotRepo != tc.wantRepo {
			t.Fatalf("splitOCIRepositoryID(%q) = (%q, %q), want (%q, %q)", tc.in, gotRegistry, gotRepo, tc.wantRegistry, tc.wantRepo)
		}
	}
}
