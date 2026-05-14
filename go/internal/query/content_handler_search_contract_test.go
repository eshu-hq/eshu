package query

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestContentHandlerSearchFilesRejectsUnsupportedPagedFallbackAsBadRequest(t *testing.T) {
	t.Parallel()

	handler := &ContentHandler{Content: fakePortContentStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"renderApp","repo_id":"repo-1","limit":10,"offset":1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "does not support paged file search") {
		t.Fatalf("body = %s, want paging contract error", w.Body.String())
	}
}

func TestContentHandlerSearchEntitiesRejectsUnsupportedPagedFallbackAsBadRequest(t *testing.T) {
	t.Parallel()

	handler := &ContentHandler{Content: fakePortContentStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/entities/search",
		bytes.NewBufferString(`{"pattern":"renderApp","repo_id":"repo-1","limit":10,"offset":1}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "does not support paged entity search") {
		t.Fatalf("body = %s, want paging contract error", w.Body.String())
	}
}

func TestContentHandlerSearchRejectsOffsetAboveBound(t *testing.T) {
	t.Parallel()

	handler := &ContentHandler{Content: fakePortContentStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/content/files/search",
		bytes.NewBufferString(`{"pattern":"renderApp","limit":10,"offset":10001}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "offset exceeds maximum") {
		t.Fatalf("body = %s, want max-offset rejection", w.Body.String())
	}
}
