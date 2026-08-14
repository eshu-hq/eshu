// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apierr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/cli/apierr"
)

// stubStatusError stands in for go/cmd/eshu's apiHTTPError, which this test
// cannot import: cmd/eshu is package main. The real type is exercised against
// StatusCode by TestAPIHTTPErrorSatisfiesCLIStatusBoundary in that package;
// this test covers the shapes only a consumer can build (wrapped, absent,
// nil).
type stubStatusError struct {
	code int
}

func (e *stubStatusError) Error() string { return fmt.Sprintf("api error %d", e.code) }

func (e *stubStatusError) HTTPStatusCode() int { return e.code }

func TestStatusCodeReadsTheInterfaceWithoutTheConcreteType(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		err      error
		wantCode int
		wantOK   bool
	}{
		{name: "direct", err: &stubStatusError{code: 404}, wantCode: 404, wantOK: true},
		{
			name:     "wrapped once",
			err:      fmt.Errorf("fetch packet: %w", &stubStatusError{code: 503}),
			wantCode: 503,
			wantOK:   true,
		},
		{
			name:     "wrapped twice",
			err:      fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", &stubStatusError{code: 401})),
			wantCode: 401,
			wantOK:   true,
		},
		{name: "no status in chain", err: errors.New("connection refused"), wantCode: 0, wantOK: false},
		{name: "nil error", err: nil, wantCode: 0, wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			code, ok := apierr.StatusCode(tc.err)
			if ok != tc.wantOK {
				t.Fatalf("apierr.StatusCode(%v) ok = %t, want %t", tc.err, ok, tc.wantOK)
			}
			if code != tc.wantCode {
				t.Fatalf("apierr.StatusCode(%v) code = %d, want %d", tc.err, code, tc.wantCode)
			}
		})
	}
}

// TestHTTPStatusErrorIsAnErrorsAsTarget pins the property the four pending
// internal/cli extractions depend on: the interface works as an errors.As
// target, so a consumer can classify a transport error by status without
// naming a type that lives in package main.
func TestHTTPStatusErrorIsAnErrorsAsTarget(t *testing.T) {
	t.Parallel()

	var target apierr.HTTPStatusError
	err := fmt.Errorf("fetch entity map: %w", &stubStatusError{code: 409})
	if !errors.As(err, &target) {
		t.Fatalf("errors.As(%v, *apierr.HTTPStatusError) = false, want true", err)
	}
	if got := target.HTTPStatusCode(); got != 409 {
		t.Fatalf("target.HTTPStatusCode() = %d, want 409", got)
	}
}
