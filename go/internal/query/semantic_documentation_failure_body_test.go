// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestSemanticAndDocumentationFailureBodiesKeepTheirShape pins that #7674
// slice 14 changed only the status and span of a failed read, not the body.
// A server fault (500) and a client cancel (499) answer the same body each
// route answered before: the documentation routes keep error_code, message,
// and correlation_id with no capability field, the semantic evidence routes
// keep error_code, message, and capability, and the envelope form carries the
// same fields under error.
func TestSemanticAndDocumentationFailureBodiesKeepTheirShape(t *testing.T) {
	t.Parallel()

	const correlationID = "corr-7674-s14"
	cases := []struct {
		name   string
		cancel bool
		err    error
		status int
	}{
		{name: "fault", err: errors.New("backend: " + failure.Canary), status: http.StatusInternalServerError},
		{
			name: "client cancel", cancel: true,
			err:    fmt.Errorf("backend: %s: %w", failure.Canary, context.Canceled),
			status: querycontract.StatusClientClosedRequest,
		},
	}
	for _, route := range semanticDocumentationFailureRoutes() {
		semantic := route.route.Message == semanticEvidenceReadFailedMessage
		for _, tc := range cases {
			for _, envelope := range []bool{false, true} {
				label := fmt.Sprintf("%s / %s / envelope=%v", route.route.Name, tc.name, envelope)
				rec := serveFailureBody(route.route, tc.err, tc.cancel, envelope, correlationID)
				if rec.Code != tc.status {
					t.Errorf("%s: status = %d, want %d", label, rec.Code, tc.status)
					continue
				}
				got := decodeFailureBody(t, label, rec.Body.Bytes(), envelope)
				want := map[string]any{
					"message": route.route.Message,
				}
				codeKey := "error_code"
				if envelope {
					codeKey = "code"
				}
				want[codeKey] = string(ErrorCodeInternalError)
				if semantic {
					want["capability"] = route.capability
				} else {
					want["correlation_id"] = correlationID
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("%s: body fields = %v, want %v", label, failureBodyKeys(got), failureBodyKeys(want))
					t.Errorf("%s: body = %v, want %v", label, got, want)
				}
			}
		}
	}
}

// serveFailureBody serves one request for route with its read failing with
// err, through the real mux, with or without the envelope media type.
func serveFailureBody(
	route failure.Route, err error, cancel, envelope bool, correlationID string,
) *httptest.ResponseRecorder {
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	if cancel {
		stop()
	}
	mux := http.NewServeMux()
	route.Handler(err).Mount(mux)
	req := httptest.NewRequest(http.MethodGet, route.Path, nil).WithContext(ctx)
	req.Header.Set("X-Correlation-ID", correlationID)
	if envelope {
		req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// decodeFailureBody returns the error fields of body: the plain object, or the
// envelope's error object.
func decodeFailureBody(t *testing.T, label string, body []byte, envelope bool) map[string]any {
	t.Helper()
	if !envelope {
		var plain map[string]any
		if err := json.Unmarshal(body, &plain); err != nil {
			t.Fatalf("%s: decode body: %v; body = %s", label, err, body)
		}
		return plain
	}
	var wrapped struct {
		Data  any            `json:"data"`
		Truth any            `json:"truth"`
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(body, &wrapped); err != nil {
		t.Fatalf("%s: decode envelope: %v; body = %s", label, err, body)
	}
	if wrapped.Data != nil || wrapped.Truth != nil {
		t.Errorf("%s: envelope data/truth = %v/%v, want null", label, wrapped.Data, wrapped.Truth)
	}
	return wrapped.Error
}

// failureBodyKeys lists m's keys in order for a failure message.
func failureBodyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
