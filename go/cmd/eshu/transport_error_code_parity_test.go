// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/change"
	"github.com/eshu-hq/eshu/go/internal/cli/freshness"
)

// TestTransportErrorCodeParity holds all three copies of the transport
// classifier to the same answers: traceErrorCodeFromTransport,
// change.ErrorCodeFromTransport, and freshness.ErrorCodeFromTransport.
//
// The three are copies with identical bodies. cmd/eshu is package main, so
// nothing can import the original, and each copy has its own callers:
// `eshu trace`, `eshu map`, and component_api use the original, `eshu change
// impact` and `eshu change plan` use the change copy, and the freshness
// commands use the freshness copy. #6117 edited the original mid-epic.
// Without this test the next such edit would reach one copy and leave the
// others classifying on the old table, with nothing in the tree going red.
//
// The freshness copy is why this test covers three subjects rather than two,
// and why it lives in its own file rather than beside the change tests. It
// arrived on a branch that predated the change copy, so each branch was
// self-consistent and the merge is what left a copy unpinned. Nothing
// conflicts at merge time here -- these are separate files in separate
// packages -- so the gap is only visible from a test that names every copy.
//
// Every branch of the shared body gets a row: the two strings.Contains checks
// that run first, the four mapped statuses, and the default that catches
// everything else. The precedence row -- an HTTP 400 whose body says
// "connection refused" -- is the only input whose answer changes if the
// message checks move after the status switch.
func TestTransportErrorCodeParity(t *testing.T) {
	t.Parallel()

	// Every copy of the classifier belongs here. A copy left out of this slice
	// is a copy nothing pins, which is the exact hole this test exists to
	// close, so the count is asserted below.
	subjects := []struct {
		name string
		fn   func(error) string
	}{
		{name: "traceErrorCodeFromTransport", fn: traceErrorCodeFromTransport},
		{name: "change.ErrorCodeFromTransport", fn: change.ErrorCodeFromTransport},
		{name: "freshness.ErrorCodeFromTransport", fn: freshness.ErrorCodeFromTransport},
	}

	cases := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil error", err: nil, want: "api_error"},
		{name: "message connection refused", err: errors.New("dial tcp 127.0.0.1:8080: connect: connection refused"), want: "backend_unavailable"},
		{name: "message request failed", err: errors.New("request failed: context deadline exceeded"), want: "backend_unavailable"},
		{name: "message beats status", err: &apiHTTPError{StatusCode: http.StatusBadRequest, Body: "connection refused"}, want: "backend_unavailable"},
		{name: "status 400", err: &apiHTTPError{StatusCode: http.StatusBadRequest, Body: "bad selector"}, want: "invalid_argument"},
		{name: "status 404", err: &apiHTTPError{StatusCode: http.StatusNotFound, Body: "no such repo"}, want: "not_found"},
		{name: "status 501", err: &apiHTTPError{StatusCode: http.StatusNotImplemented, Body: "no such capability"}, want: "unsupported_capability"},
		{name: "status 503", err: &apiHTTPError{StatusCode: http.StatusServiceUnavailable, Body: "backend down"}, want: "backend_unavailable"},
		{name: "status 409 falls through", err: &apiHTTPError{StatusCode: http.StatusConflict, Body: "ambiguous scope"}, want: "api_error"},
		{name: "status 500 falls through", err: &apiHTTPError{StatusCode: http.StatusInternalServerError, Body: "boom"}, want: "api_error"},
		{name: "no status carried", err: errors.New("json: cannot unmarshal"), want: "api_error"},
	}

	// Every arm of the switch, plus the default, has to be represented or a
	// divergence in an unvisited arm passes unnoticed.
	seen := map[string]bool{}
	for _, tc := range cases {
		seen[tc.want] = true
	}
	for _, code := range []string{"invalid_argument", "not_found", "unsupported_capability", "backend_unavailable", "api_error"} {
		if !seen[code] {
			t.Fatalf("no row expects %q; the table no longer covers every arm", code)
		}
	}
	if len(cases) != 11 {
		t.Fatalf("parity table has %d rows, want 11; move this number only alongside a row you added, and say which behavior the new row pins", len(cases))
	}
	if len(subjects) != 3 {
		t.Fatalf("parity covers %d copies, want 3; a copy dropped from this slice is a copy nothing pins, so move this number only alongside the copy you added or deleted", len(subjects))
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			all := make([]string, 0, len(subjects))
			disagreed := make([]string, 0, len(subjects))
			for _, subject := range subjects {
				answer := subject.fn(tc.err)
				all = append(all, fmt.Sprintf("%s=%q", subject.name, answer))
				if answer != tc.want {
					disagreed = append(disagreed, fmt.Sprintf("%s=%q", subject.name, answer))
				}
			}
			// Naming the copies that disagreed separates the two failures that
			// land here. A subset means the copies have drifted apart and the
			// named one is the copy an edit missed. All of them means the
			// copies still agree and the row above is what no longer matches
			// the shared body.
			if len(disagreed) > 0 {
				t.Fatalf("%v: want %q, but %s answered otherwise (every copy: %s)",
					tc.err, tc.want, strings.Join(disagreed, ", "), strings.Join(all, ", "))
			}
		})
	}
}
