// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// statusErr is a stand-in for go/cmd/eshu's unexported transport error. It
// implements apierr.HTTPStatusError structurally, which is the only thing
// ErrorCodeFromTransport reads.
type statusErr struct {
	status int
	text   string
}

func (e statusErr) Error() string       { return e.text }
func (e statusErr) HTTPStatusCode() int { return e.status }

// TestExitCodeForErrorCodeTable pins every envelope error code the freshness
// API can return to the exit code operators and scripts depend on. The numbers
// are the family's observed behaviour, captured from the built binary against a
// canned API (see the PR body's parity table), not copied from a shared table.
func TestExitCodeForErrorCodeTable(t *testing.T) {
	for _, tc := range []struct {
		code string
		want int
	}{
		{"ambiguous", 3},
		{"index_building", 4},
		{"stale", 4},
		{"capability_degraded", 5},
		{"partial", 5},
		{"unsupported_capability", 6},
		{"invalid_argument", 2},
		{"not_found", 2},
		{"scope_not_found", 2},
		{"backend_unavailable", 1},
		{"api_error", 1},
		{"weird_code", 1},
		{"", 1},
		{"  index_building  ", 4},
	} {
		if got := ExitCodeForErrorCode(tc.code); got != tc.want {
			t.Errorf("ExitCodeForErrorCode(%q) = %d, want %d", tc.code, got, tc.want)
		}
	}
}

// TestExitCodeForErrorCodeRejectsBuildingSpelling pins the one place a later
// "simplification" would silently change an operator-visible exit code. Sibling
// command families (trace, change, map) hard-code exit 4 when the truth
// envelope's freshness state is the literal string "building", but that string
// is a freshness *state*, never an error *code*. This family classifies error
// codes only, and "building" is not one of them, so it falls to 1. If someone
// later folds the sibling states into this table, this test fails instead of
// quietly turning a script's exit 1 into a 4.
func TestExitCodeForErrorCodeRejectsBuildingSpelling(t *testing.T) {
	if got := ExitCodeForErrorCode("building"); got != 1 {
		t.Fatalf(`ExitCodeForErrorCode("building") = %d, want 1 (a freshness state is not an error code)`, got)
	}
	if got := ExitCodeForErrorCode("truncated"); got != 1 {
		t.Fatalf(`ExitCodeForErrorCode("truncated") = %d, want 1`, got)
	}
	if ExitCodeForErrorCode("index_building") == ExitCodeForErrorCode("building") {
		t.Fatal(`"index_building" and "building" must not share an exit code`)
	}
}

// TestErrorCodeFromTransportMessagePrecedesStatus is the discriminating case
// for the substring-before-status ordering. A 400 whose message happens to
// carry "connection refused" classifies as backend_unavailable, not
// invalid_argument, because the message checks run first. Reordering the status
// switch ahead of them fails exactly this case.
func TestErrorCodeFromTransportMessagePrecedesStatus(t *testing.T) {
	err := statusErr{status: 400, text: `API error 400: dial tcp 10.0.0.1:80: connect: connection refused`}
	if got := ErrorCodeFromTransport(err); got != "backend_unavailable" {
		t.Fatalf("ErrorCodeFromTransport(status 400 + connection refused) = %q, want backend_unavailable", got)
	}
	wrapped := statusErr{status: 404, text: `request failed: Get "http://h/x": no route`}
	if got := ErrorCodeFromTransport(wrapped); got != "backend_unavailable" {
		t.Fatalf("ErrorCodeFromTransport(status 404 + request failed) = %q, want backend_unavailable", got)
	}
}

func TestErrorCodeFromTransportStatusMapping(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   string
	}{
		{400, "invalid_argument"},
		{404, "not_found"},
		{501, "unsupported_capability"},
		{503, "backend_unavailable"},
		{500, "api_error"},
		{418, "api_error"},
	} {
		err := statusErr{status: tc.status, text: fmt.Sprintf("API error %d: body", tc.status)}
		if got := ErrorCodeFromTransport(err); got != tc.want {
			t.Errorf("ErrorCodeFromTransport(status %d) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// TestErrorCodeFromTransportNilError proves the nil guards hold. The two
// strings.Contains calls would panic on a nil error, so they are guarded; the
// status branch needs no guard because errors.As reports false for nil.
func TestErrorCodeFromTransportNilError(t *testing.T) {
	if got := ErrorCodeFromTransport(nil); got != "api_error" {
		t.Fatalf("ErrorCodeFromTransport(nil) = %q, want api_error", got)
	}
	if got := ErrorCodeFromTransport(errors.New("some other failure")); got != "api_error" {
		t.Fatalf("ErrorCodeFromTransport(plain error) = %q, want api_error", got)
	}
}

func TestEnvelopeFailureMessageFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		in          *EnvelopeError
		wantNil     bool
		wantMessage string
		wantCode    int
	}{
		{name: "nil", in: nil, wantNil: true},
		{
			name:        "message wins",
			in:          &EnvelopeError{Code: "index_building", Message: " index is building "},
			wantMessage: "index is building",
			wantCode:    4,
		},
		{
			name:        "code fills an empty message",
			in:          &EnvelopeError{Code: "  capability_degraded  "},
			wantMessage: "capability_degraded",
			wantCode:    5,
		},
		{
			name:        "both empty falls back to the generic sentence",
			in:          &EnvelopeError{},
			wantMessage: "generation lifecycle request failed",
			wantCode:    1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := EnvelopeFailure(tc.in)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("EnvelopeFailure(nil) = %#v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("EnvelopeFailure() = nil, want a failure")
			}
			if got.Message != tc.wantMessage {
				t.Errorf("Message = %q, want %q", got.Message, tc.wantMessage)
			}
			if got.Code != tc.wantCode {
				t.Errorf("Code = %d, want %d", got.Code, tc.wantCode)
			}
			if got.Error() != tc.wantMessage {
				t.Errorf("Error() = %q, want %q", got.Error(), tc.wantMessage)
			}
		})
	}
}

// TestFailureIsRecoverableWithErrorsAs proves the wrapper's exit-code mapping
// can find the failure through the error interface it is handed.
func TestFailureIsRecoverableWithErrorsAs(t *testing.T) {
	var err error = &Failure{Message: "boom", Code: 6}
	var failure *Failure
	if !errors.As(err, &failure) {
		t.Fatal("errors.As did not recover *Failure")
	}
	if failure.Code != 6 {
		t.Fatalf("Code = %d, want 6", failure.Code)
	}
}

func TestWriteJSONDoesNotEscapeHTML(t *testing.T) {
	out := &bytes.Buffer{}
	if err := WriteJSON(out, Envelope{Data: map[string]any{"q": "a<b&c>d"}}); err != nil {
		t.Fatalf("WriteJSON() error = %v", err)
	}
	if !strings.Contains(out.String(), "a<b&c>d") {
		t.Fatalf("WriteJSON escaped HTML: %q", out.String())
	}
	if !strings.Contains(out.String(), "\n  \"data\"") {
		t.Fatalf("WriteJSON lost its two-space indent: %q", out.String())
	}
}

func TestRenderEnvelopeErrorSkipsNilError(t *testing.T) {
	out := &bytes.Buffer{}
	if err := RenderEnvelopeError(out, Envelope{}); err != nil {
		t.Fatalf("RenderEnvelopeError() error = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("RenderEnvelopeError wrote %q for a nil error, want nothing", out.String())
	}
}
