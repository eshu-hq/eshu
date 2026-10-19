// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// serveDeltaUnionVerdict serves one completeness GET through the real mux with
// a graph read that fails with err. accept, when set, is the Accept header.
func serveDeltaUnionVerdict(t *testing.T, err error, accept string) *httptest.ResponseRecorder {
	t.Helper()
	checker, _ := deltaUnionChecker()
	checker.Graph = &stubDeltaUnionGraph{err: err}
	mux := http.NewServeMux()
	(&Handler{Completeness: checker}).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/admin/delta-union/completeness?scope=scope-1", nil)
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestDeltaUnionCompletenessMapsGraphReadVerdicts is the #8292 regression. The
// route answered a graph outage or a graph deadline as a plain 500 with no
// Retry-After, where every other converted graph route answers the shared
// retryable 503 or the 504 verdict. The verdict now comes from the shared
// querycontract mapper, the backend text never reaches the body, and the
// {"status","detail"} body shape that callers read stays.
func TestDeltaUnionCompletenessMapsGraphReadVerdicts(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		err        error
		wantStatus int
		wantDetail string
		retryAfter bool
	}{
		{
			name:       "graph outage",
			err:        fmt.Errorf("read delta union graph for repo repo-1: %w: %s", querycontract.ErrGraphUnavailable, adminFailureCanary),
			wantStatus: http.StatusServiceUnavailable,
			wantDetail: querycontract.ErrGraphUnavailable.Error(),
			retryAfter: true,
		},
		{
			name:       "graph deadline",
			err:        fmt.Errorf("read delta union graph for repo repo-1: %w: %s", querycontract.ErrGraphReadDeadline, adminFailureCanary),
			wantStatus: http.StatusGatewayTimeout,
			wantDetail: querycontract.ErrGraphReadDeadline.Error(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := serveDeltaUnionVerdict(t, tc.err, "")
			body := rec.Body.String()
			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body = %s", rec.Code, tc.wantStatus, body)
			}
			for _, fragment := range []string{"hunter2", "db.internal.example", "fact_work_items", "pq:"} {
				if strings.Contains(body, fragment) {
					t.Fatalf("body leaked backend error text %q: %s", fragment, body)
				}
			}
			retryAfter := rec.Header().Get("Retry-After")
			if tc.retryAfter && retryAfter != fmt.Sprint(querycontract.BackendUnavailableRetryAfterSeconds) {
				t.Fatalf("Retry-After = %q, want %d", retryAfter, querycontract.BackendUnavailableRetryAfterSeconds)
			}
			if !tc.retryAfter && retryAfter != "" {
				t.Fatalf("Retry-After = %q, want none on a %d", retryAfter, tc.wantStatus)
			}
			var decoded map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &decoded); err != nil {
				t.Fatalf("body is not JSON: %v; body = %s", err, body)
			}
			if len(decoded) != 2 || decoded["status"] != "error" || decoded["detail"] != tc.wantDetail {
				t.Fatalf("body = %v, want exactly status=error detail=%q", decoded, tc.wantDetail)
			}
		})
	}
}

// TestDeltaUnionCompletenessVerdictNamesCapabilityInEnvelope pins the opt-in
// envelope: a caller that asks for the Eshu envelope reads the same verdict
// with the route's registered capability-matrix key, as on every other
// converted route (#8292).
func TestDeltaUnionCompletenessVerdictNamesCapabilityInEnvelope(t *testing.T) {
	t.Parallel()

	rec := serveDeltaUnionVerdict(t, fmt.Errorf("graph: %w", querycontract.ErrGraphUnavailable), querycontract.EnvelopeMIMEType)
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q; want 503 with Retry-After; body = %s",
			rec.Code, rec.Header().Get("Retry-After"), rec.Body.String())
	}
	var envelope querycontract.ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("body is not an envelope: %v; body = %s", err, rec.Body.String())
	}
	if envelope.Error == nil || envelope.Error.Capability != deltaUnionCompletenessCapability ||
		envelope.Error.Code != querycontract.ErrorCodeBackendUnavailable {
		t.Fatalf("error = %+v, want backend_unavailable under %q", envelope.Error, deltaUnionCompletenessCapability)
	}
}
