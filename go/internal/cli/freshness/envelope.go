// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package freshness

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/apierr"
)

// EnvelopeFetcher is the transport the freshness commands need: one GET that
// decodes a canonical Eshu envelope into result. It is declared here, where it
// is consumed, because the concrete client lives in go/cmd/eshu, which is
// package main and cannot be imported.
type EnvelopeFetcher interface {
	GetEnvelope(path string, result any) error
}

// Envelope is the canonical Eshu response shape as the freshness commands
// consume it. Data and Truth stay generic maps: the CLI renders a handful of
// named keys and passes everything else through to --json untouched, so a
// server-side field addition reaches operators without a CLI release.
type Envelope struct {
	Data  map[string]any `json:"data"`
	Truth map[string]any `json:"truth"`
	Error *EnvelopeError `json:"error"`
}

// EnvelopeError is the envelope's error member. Code drives the process exit
// code through ExitCodeForErrorCode; Message is rendered verbatim.
type EnvelopeError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// Failure is the error the Run functions return when the command must exit
// non-zero. It carries the operator-facing message and the numeric exit code
// this family has always used, but not the CLI's exit-error type: mapping
// Failure onto that type is go/cmd/eshu's job, since the type is defined there.
type Failure struct {
	Message string
	Code    int
}

// Error implements error so a Failure can travel as the Run functions' return
// value and be recovered with errors.As.
func (f *Failure) Error() string { return f.Message }

// EnvelopeFailure converts an envelope error into the Failure the command
// should exit with, or nil when the envelope reported no error. An error with
// no message falls back to its code, and an error with neither falls back to a
// fixed sentence, so the operator never sees an empty failure line.
func EnvelopeFailure(e *EnvelopeError) *Failure {
	if e == nil {
		return nil
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Code)
	}
	if message == "" {
		message = "generation lifecycle request failed"
	}
	return &Failure{Message: message, Code: ExitCodeForErrorCode(e.Code)}
}

// ExitCodeForErrorCode maps an envelope error code to the process exit code
// the freshness commands have always returned for it. The mapping is over
// error *codes* only. Freshness *states* -- "building", "stale" as they appear
// under truth.freshness.state -- are not codes and are deliberately absent:
// this family reports a non-fresh state in its output and still exits 0, which
// is where it differs from `eshu trace service`, `eshu change impact`, and
// `eshu map`, all of which exit 4 on a non-fresh state. See
// TestExitCodeForErrorCodeRejectsBuildingSpelling.
func ExitCodeForErrorCode(code string) int {
	switch strings.TrimSpace(code) {
	case "ambiguous":
		return 3
	case "index_building", "stale":
		return 4
	case "capability_degraded", "partial":
		return 5
	case "unsupported_capability":
		return 6
	case "invalid_argument", "not_found", "scope_not_found":
		return 2
	default:
		return 1
	}
}

// ErrorCodeFromTransport classifies a transport-level failure into the error
// code the synthesized envelope carries.
//
// It is one of three identical copies. The original is
// traceErrorCodeFromTransport in go/cmd/eshu/trace.go, which still serves
// `eshu trace`, `eshu map`, and component_api; change.ErrorCodeFromTransport
// is the third. cmd/eshu is package main, so no one can import the original,
// and each copy keeps its own callers. Change one and you must change all
// three: TestTransportErrorCodeParity in go/cmd/eshu feeds one error table to
// every copy and fails when their answers differ.
//
// Ordering is load-bearing: the two message checks run before the status
// switch, so an API response that carries an HTTP status *and* mentions a
// refused connection classifies as backend_unavailable rather than by its
// status. The nil guards on the message checks are required -- err.Error()
// panics on a nil error. The status branch needs no guard because errors.As,
// which apierr.StatusCode uses, reports false for nil.
func ErrorCodeFromTransport(err error) string {
	if err != nil && strings.Contains(err.Error(), "connection refused") {
		return "backend_unavailable"
	}
	if err != nil && strings.Contains(err.Error(), "request failed") {
		return "backend_unavailable"
	}
	if status, ok := apierr.StatusCode(err); ok {
		switch status {
		case 400:
			return "invalid_argument"
		case 404:
			return "not_found"
		case 501:
			return "unsupported_capability"
		case 503:
			return "backend_unavailable"
		default:
			return "api_error"
		}
	}
	return "api_error"
}

// transportEnvelope builds the envelope a command renders when the request
// never produced one, so the JSON and human paths report a transport failure
// in the same shape as a server-reported failure.
func transportEnvelope(err error) Envelope {
	return Envelope{Error: &EnvelopeError{
		Code:    ErrorCodeFromTransport(err),
		Message: err.Error(),
	}}
}

// summaryRenderer writes one command's human-readable summary.
type summaryRenderer func(io.Writer, Envelope) error

// finish writes a command's terminal output and returns the error the command
// exits with. In --json mode the envelope is always written, failure or not,
// and a write error outranks the command failure because a truncated envelope
// is the more urgent thing to tell the operator about.
func finish(w io.Writer, jsonOutput bool, env Envelope, render summaryRenderer, cmdErr error) error {
	if jsonOutput {
		if writeErr := WriteJSON(w, env); writeErr != nil {
			return writeErr
		}
		return cmdErr
	}
	if cmdErr != nil {
		if renderErr := RenderEnvelopeError(w, env); renderErr != nil {
			return renderErr
		}
		return cmdErr
	}
	return render(w, env)
}

// RenderEnvelopeError writes the one-line failure summary for an envelope that
// reported an error. It writes nothing when the envelope carries no error.
//
// The message is rendered verbatim, including a transport error's text. For a
// request that never reached the API that text is net/http's, which embeds the
// request URL and therefore every selector the operator passed on the command
// line. Nothing here screens it; see TestRenderedErrorCarriesTheRequestURL and
// this package's README for the reasoning and the limit.
func RenderEnvelopeError(w io.Writer, env Envelope) error {
	if env.Error == nil {
		return nil
	}
	return writef(w, "Generation lifecycle error (%s): %s\n", env.Error.Code, env.Error.Message)
}

// WriteJSON writes v as indented JSON with HTML escaping off, so a selector
// containing <, > or & round-trips as the operator typed it.
//
//nolint:wrapcheck // Same reason as writef: this error is the operator-facing text of a failed write, and wrapping would change it.
func WriteJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// truthFreshnessState reads truth.freshness.state, the one truth field every
// freshness summary leads with.
func truthFreshnessState(env Envelope) string {
	return stringValue(mapValue(env.Truth, "freshness"), "state")
}

// renderFreshnessLine writes the leading "Truth freshness:" line, or nothing
// when the envelope carried no freshness state.
func renderFreshnessLine(w io.Writer, env Envelope) error {
	state := truthFreshnessState(env)
	if state == "" {
		return nil
	}
	return writef(w, "Truth freshness: %s\n", state)
}
