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

// fakeTagHistoryGraphReader records the last query and returns canned rows so
// the tag-history handler can be exercised without a graph backend.
type fakeTagHistoryGraphReader struct {
	rows       []map[string]any
	err        error
	lastCypher string
	lastParams map[string]any
}

func (f *fakeTagHistoryGraphReader) Run(_ context.Context, cypher string, params map[string]any) ([]map[string]any, error) {
	f.lastCypher = cypher
	f.lastParams = params
	if f.err != nil {
		return nil, f.err
	}
	return f.rows, nil
}

func (*fakeTagHistoryGraphReader) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, nil
}

func newTagHistoryRequest(target string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	return req
}

func tagHistoryRowMap(tag, resolvedDigest, previousDigest, firstObservedAt string, mutated bool) map[string]any {
	return map[string]any{
		"tag":               tag,
		"resolved_digest":   resolvedDigest,
		"previous_digest":   previousDigest,
		"mutated":           mutated,
		"first_observed_at": firstObservedAt,
		"repository_id":     "oci-registry://ghcr.io/eshu-hq/demo",
		"identity_strength": "weak_tag",
		"uid":               "uid-" + resolvedDigest,
	}
}

func decodeTagHistoryBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
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

func TestTagHistoryHandlerHappyPathOrdered(t *testing.T) {
	t.Parallel()

	reader := &fakeTagHistoryGraphReader{rows: []map[string]any{
		tagHistoryRowMap("1.0.0", "sha256:aaa", "", "2026-06-25T00:00:00Z", false),
		tagHistoryRowMap("1.0.0", "sha256:bbb", "sha256:aaa", "2026-06-26T00:00:00Z", true),
	}}
	handler := &TagHistoryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTagHistoryRequest("/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0"))

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)
	history, ok := data["tag_history"].([]any)
	if !ok || len(history) != 2 {
		t.Fatalf("tag_history = %#v, want 2 rows", data["tag_history"])
	}
	first := history[0].(map[string]any)
	if got, want := first["resolved_digest"], "sha256:aaa"; got != want {
		t.Fatalf("history[0].resolved_digest = %#v, want %#v", got, want)
	}
	second := history[1].(map[string]any)
	if got, want := second["previous_digest"], "sha256:aaa"; got != want {
		t.Fatalf("history[1].previous_digest = %#v, want %#v", got, want)
	}
	if got, want := second["mutated"], true; got != want {
		t.Fatalf("history[1].mutated = %#v, want %#v", got, want)
	}
	if got, want := data["image_ref"], "ghcr.io/eshu-hq/demo:1.0.0"; got != want {
		t.Fatalf("image_ref = %#v, want %#v", got, want)
	}
	if got, want := reader.lastParams["image_ref"], "ghcr.io/eshu-hq/demo:1.0.0"; got != want {
		t.Fatalf("query image_ref param = %#v, want %#v", got, want)
	}
}

func TestTagHistoryHandlerDefaultsLimit(t *testing.T) {
	t.Parallel()

	reader := &fakeTagHistoryGraphReader{rows: nil}
	handler := &TagHistoryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTagHistoryRequest("/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0"))

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := reader.lastParams["limit"], tagHistoryDefaultLim+1; got != want {
		t.Fatalf("default query limit = %#v, want %#v", got, want)
	}
}

func TestTagHistoryHandlerLimitValidation(t *testing.T) {
	t.Parallel()

	handler := &TagHistoryHandler{Neo4j: &fakeTagHistoryGraphReader{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	cases := []string{"0", "-1", "201", "abc"}
	for _, raw := range cases {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, newTagHistoryRequest("/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0&limit="+raw))
		if got, want := w.Code, http.StatusBadRequest; got != want {
			t.Fatalf("limit=%s status = %d, want %d; body = %s", raw, got, want, w.Body.String())
		}
	}
}

func TestTagHistoryHandlerTruncationAndCursor(t *testing.T) {
	t.Parallel()

	reader := &fakeTagHistoryGraphReader{rows: []map[string]any{
		tagHistoryRowMap("1.0.0", "sha256:aaa", "", "2026-06-25T00:00:00Z", false),
		tagHistoryRowMap("1.0.0", "sha256:bbb", "sha256:aaa", "2026-06-26T00:00:00Z", true),
	}}
	handler := &TagHistoryHandler{Neo4j: reader, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTagHistoryRequest("/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0&limit=1&offset=10"))

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	data := decodeTagHistoryBody(t, w)
	history := data["tag_history"].([]any)
	if len(history) != 1 {
		t.Fatalf("tag_history = %d rows, want 1 (truncated)", len(history))
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

func TestTagHistoryHandlerMissingSelectorFields(t *testing.T) {
	t.Parallel()

	handler := &TagHistoryHandler{Neo4j: &fakeTagHistoryGraphReader{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/images/tag-history",
		"/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo",
		"/api/v0/images/tag-history?tag=1.0.0",
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, newTagHistoryRequest(target))
		if got, want := w.Code, http.StatusBadRequest; got != want {
			t.Fatalf("target=%s status = %d, want %d; body = %s", target, got, want, w.Body.String())
		}
	}
}

func TestTagHistoryHandlerRepositoryIDWithoutOCIPrefixIsRejected(t *testing.T) {
	t.Parallel()

	handler := &TagHistoryHandler{Neo4j: &fakeTagHistoryGraphReader{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTagHistoryRequest("/api/v0/images/tag-history?repository_id=ghcr.io/eshu-hq/demo&tag=1.0.0"))
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestTagHistoryHandlerBackendUnavailable(t *testing.T) {
	t.Parallel()

	handler := &TagHistoryHandler{Neo4j: nil, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTagHistoryRequest("/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0"))

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestTagHistoryHandlerUnsupportedCapability(t *testing.T) {
	t.Parallel()

	handler := &TagHistoryHandler{Neo4j: &fakeTagHistoryGraphReader{}, Profile: ProfileLocalLightweight}
	mux := http.NewServeMux()
	handler.Mount(mux)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, newTagHistoryRequest("/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0"))

	if got, want := w.Code, http.StatusNotImplemented; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestTagHistoryComposeOCIImageRef(t *testing.T) {
	t.Parallel()

	cases := []struct {
		repositoryID string
		tag          string
		want         string
	}{
		{"oci-registry://ghcr.io/eshu-hq/demo", "1.0.0", "ghcr.io/eshu-hq/demo:1.0.0"},
		{"ghcr.io/eshu-hq/demo", "1.0.0", ""},
		{"oci-registry://ghcr.io/eshu-hq/demo", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		if got := composeOCIImageRef(tc.repositoryID, tc.tag); got != tc.want {
			t.Fatalf("composeOCIImageRef(%q, %q) = %q, want %q", tc.repositoryID, tc.tag, got, tc.want)
		}
	}
}
