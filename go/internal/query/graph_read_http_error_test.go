// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteGraphReadErrorMapsOnlyGraphPolicyFailures(t *testing.T) {
	t.Parallel()
	const privateCause = "private.graph.invalid:7687"
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   ErrorCode
	}{
		{name: "unavailable", err: fmt.Errorf("%s: %w", privateCause, ErrGraphUnavailable), wantStatus: http.StatusServiceUnavailable, wantCode: ErrorCodeBackendUnavailable},
		{name: "policy deadline", err: fmt.Errorf("%s: %w", privateCause, ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout, wantCode: ErrorCodeBackendTimeout},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v0/test", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			if !WriteGraphReadError(rec, req, test.err, "platform_impact.context_overview") {
				t.Fatal("WriteGraphReadError() handled = false, want true")
			}
			if rec.Code != test.wantStatus || !strings.Contains(rec.Body.String(), `"code":"`+string(test.wantCode)+`"`) {
				t.Fatalf("response = status %d body=%s", rec.Code, rec.Body.String())
			}
			if strings.Contains(rec.Body.String(), privateCause) {
				t.Fatalf("response leaked private cause: %s", rec.Body.String())
			}
		})
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v0/test", nil)
	if WriteGraphReadError(rec, req, context.DeadlineExceeded, "platform_impact.context_overview") {
		t.Fatal("parent context deadline must not be mapped as graph-policy timeout")
	}
	if WriteGraphReadError(rec, req, errors.New("syntax error"), "platform_impact.context_overview") {
		t.Fatal("unrelated error must remain unhandled")
	}
}
