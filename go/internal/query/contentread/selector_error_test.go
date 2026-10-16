// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contentread

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/selector"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// selectorErrorRoute mounts a probe route that answers err through
// writeContentSelectorError, the shared selector answer of every content read
// and search route, so its fallback can be driven with an error the
// classified answers do not cover.
type selectorErrorRoute struct{ err error }

func (s selectorErrorRoute) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /selector-error", func(w http.ResponseWriter, r *http.Request) {
		writeContentSelectorError(w, r, s.err, "code_search.content_search")
	})
}

// TestContentSelectorUnclassifiedErrorAnswersFixedText pins the fallback of
// writeContentSelectorError (#7674): an error that is neither a reader
// verdict, a lookup failure, nor a typed not-found or ambiguous answer is a
// server fault. It answers selector.LookupFailureMessage with 500 (499 on a
// client cancel), never a 400 that echoes the error text.
func TestContentSelectorUnclassifiedErrorAnswersFixedText(t *testing.T) {
	failure.Run(t, failure.Suite{Routes: []failure.Route{{
		Name: "unclassified selector error", Path: "/selector-error", Message: selector.LookupFailureMessage,
		Handler: func(err error) failure.Mounter { return selectorErrorRoute{err: err} },
	}}})
}

// TestContentSelectorTypedErrorsKeepTheirAnswers pins the classified answers
// the fallback change leaves alone: an unmatched selector answers 404 and an
// ambiguous one 400, each with the typed answer's own text, which quotes only
// the caller's selector and the matched repository ids.
func TestContentSelectorTypedErrorsKeepTheirAnswers(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"not found", selector.NotFoundError{Selector: "payments-svc"}, http.StatusNotFound},
		{"ambiguous", selector.AmbiguousError{Selector: "payments", Matches: []string{"repository:a", "repository:b"}}, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mux := http.NewServeMux()
			selectorErrorRoute{err: tc.err}.Mount(mux)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/selector-error", nil))
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if got := failure.Message(rec.Body.Bytes()); got != tc.err.Error() {
				t.Fatalf("message = %q, want the typed answer %q", got, tc.err.Error())
			}
			if strings.Contains(rec.Body.String(), string(querycontract.ErrorCodeInternalError)) {
				t.Fatalf("typed answer carries an internal error code: %s", rec.Body.String())
			}
		})
	}
}
