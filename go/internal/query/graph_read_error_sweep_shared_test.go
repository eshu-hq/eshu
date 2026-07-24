// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// graphReadSweepCase is one bounded graph-read availability case: a sentinel
// wrapped in a private detail that must map to the stable HTTP contract
// without leaking the wrapped cause.
type graphReadSweepCase struct {
	name       string
	err        error
	wantStatus int
	wantCode   ErrorCode
}

// graphReadSweepCases is the shared unavailable/deadline table used by every
// graph-read availability regression test in this package. Each handler that
// routes a graph-derived failure through WriteGraphReadError is exercised
// against both sentinels from this one table.
func graphReadSweepCases() []graphReadSweepCase {
	return []graphReadSweepCase{
		{name: "unavailable", err: fmt.Errorf("private graph detail: %w", ErrGraphUnavailable), wantStatus: http.StatusServiceUnavailable, wantCode: ErrorCodeBackendUnavailable},
		{name: "deadline", err: fmt.Errorf("private graph detail: %w", ErrGraphReadDeadline), wantStatus: http.StatusGatewayTimeout, wantCode: ErrorCodeBackendTimeout},
	}
}

// assertGraphReadSweepResponse asserts the bounded-availability status, the
// envelope error code, and that the private wrapped cause never reaches the
// response body.
func assertGraphReadSweepResponse(t *testing.T, rec *httptest.ResponseRecorder, test graphReadSweepCase) {
	t.Helper()
	if rec.Code != test.wantStatus {
		t.Fatalf("status = %d, want %d body=%s", rec.Code, test.wantStatus, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"code":"`+string(test.wantCode)+`"`) {
		t.Fatalf("body = %s, want code %q", rec.Body.String(), test.wantCode)
	}
	if strings.Contains(rec.Body.String(), "private") {
		t.Fatalf("body leaked private cause: %s", rec.Body.String())
	}
}
