// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitymap

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/apierr"
)

// route is the API path that answers an entity-map request.
const route = "/api/v0/impact/entity-map"

// EnvelopePoster posts a JSON body to an Eshu API route and decodes the
// canonical envelope response into result. go/cmd/eshu's *APIClient satisfies
// it. The interface is declared here, at the point of use, because that type
// lives in package main and cannot be imported.
type EnvelopePoster interface {
	PostEnvelope(path string, body, result any) error
}

// Options is one resolved entity-map request. The cobra wrapper in
// go/cmd/eshu builds it from flags; nothing in this package reads a flag, the
// process environment, or Eshu config.
type Options struct {
	// From is the entity handle to map, such as terraform/aws_lb.main.
	From string
	// FromType narrows resolution to one entity type, such as service or
	// terraform_resource. Empty means no hint.
	FromType string
	// Repo narrows resolution to one repository selector.
	Repo string
	// Environment narrows runtime and resource relationships to one
	// environment.
	Environment string
	// Relationship filters the mapped relationships to one type, such as
	// DEPENDS_ON. The wrapper upper-cases it before it gets here.
	Relationship string
	// Depth is the maximum relationship depth to traverse.
	Depth int
	// Limit is the maximum number of mapped relationships to return.
	Limit int
	// JSON selects the canonical envelope as output instead of the text
	// summary.
	JSON bool
}

// Envelope is the canonical entity-map response. Data and Truth stay untyped
// because the renderers read a handful of members and pass everything else
// through to --json unchanged; typing them here would silently drop members
// the API adds later.
type Envelope struct {
	Data  map[string]any `json:"data"`
	Truth map[string]any `json:"truth"`
	Error *EnvelopeError `json:"error"`
}

// EnvelopeError is the error member of the canonical envelope. Code is the
// machine-readable classification the CLI turns into an exit code; Message is
// what an operator reads.
type EnvelopeError struct {
	Code       string         `json:"code,omitempty"`
	Message    string         `json:"message,omitempty"`
	Capability string         `json:"capability,omitempty"`
	Details    map[string]any `json:"details,omitempty"`
}

// FailureKind names why an entity map did not succeed.
//
// The kind exists so that mapping an outcome to a process exit code stays in
// the cobra wrapper, where the rest of the CLI's exit-code contract lives.
// This package classifies; it never decides what the process returns.
type FailureKind int

const (
	// FailureEnvelope is an error the API reported, or a transport error
	// classified into one. Failure.Code carries the canonical error code.
	FailureEnvelope FailureKind = iota + 1
	// FailureFreshness is an index that is stale or still building, so the
	// map would answer from truth the operator has not been told is behind.
	// Failure.Code carries the freshness state.
	FailureFreshness
	// FailureAmbiguous is a selector that resolved to more than one entity.
	FailureAmbiguous
	// FailureNoMatch is a selector that resolved to no supported entity.
	FailureNoMatch
)

// Failure describes a non-success entity-map outcome. It implements error so
// callers can return it directly, and carries Kind and Code so the wrapper can
// pick the exit code without re-deriving the reason from the message text.
type Failure struct {
	Kind    FailureKind
	Code    string
	Message string
}

// Error returns the operator-facing message. It is the message verbatim, with
// no prefix, because it is printed as the command's only failure line.
func (f *Failure) Error() string {
	return f.Message
}

// Fetch posts opts to the entity-map route and returns the decoded envelope.
// A transport failure returns the zero Envelope and the client's error; pass
// both to Resolve rather than inspecting the error here.
func Fetch(client EnvelopePoster, opts Options) (Envelope, error) {
	body := map[string]any{
		"from":         opts.From,
		"from_type":    opts.FromType,
		"repo_id":      opts.Repo,
		"environment":  opts.Environment,
		"relationship": opts.Relationship,
		"depth":        opts.Depth,
		"limit":        opts.Limit,
	}
	var envelope Envelope
	if err := client.PostEnvelope(route, body, &envelope); err != nil {
		// The message reaches the operator verbatim through
		// Resolve/EnvelopeError.Message, and ErrorCodeFromTransport
		// classifies on its substrings; wrapping would change both.
		return Envelope{}, err //nolint:wrapcheck // preserves operator-visible text and transport classification
	}
	return envelope, nil
}

// Resolve turns a Fetch result into the envelope to render and the failure to
// report, in the order the CLI checks them: transport error, envelope error,
// index freshness, then resolution status. It returns a nil *Failure on
// success.
//
// A transport error is replaced by a synthetic envelope carrying only the
// classified error, so --json prints the same shape whether the API answered
// or the call never reached it.
func Resolve(envelope Envelope, fetchErr error) (Envelope, *Failure) {
	if fetchErr != nil {
		synthetic := Envelope{
			Error: &EnvelopeError{
				Code:    ErrorCodeFromTransport(fetchErr),
				Message: fetchErr.Error(),
			},
		}
		return synthetic, envelopeFailure(synthetic.Error)
	}
	if envelope.Error != nil {
		return envelope, envelopeFailure(envelope.Error)
	}
	if state := FreshnessState(envelope); state == "stale" || state == "building" {
		return envelope, &Failure{
			Kind:    FailureFreshness,
			Code:    state,
			Message: fmt.Sprintf("entity map freshness is %s", state),
		}
	}
	switch stringField(envelope.Data, "status") {
	case "ambiguous":
		return envelope, &Failure{
			Kind:    FailureAmbiguous,
			Code:    "ambiguous",
			Message: "entity map selector is ambiguous",
		}
	case "no_match":
		return envelope, &Failure{
			Kind:    FailureNoMatch,
			Code:    "no_match",
			Message: "entity map selector did not match a supported entity",
		}
	}
	return envelope, nil
}

// envelopeFailure converts an envelope error into a Failure, falling back from
// the message to the code to a fixed string so an operator never sees a
// command fail with an empty explanation.
func envelopeFailure(e *EnvelopeError) *Failure {
	if e == nil {
		return nil
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Code)
	}
	if message == "" {
		message = "entity map failed"
	}
	return &Failure{Kind: FailureEnvelope, Code: e.Code, Message: message}
}

// FreshnessState returns truth.freshness.state, or the empty string when the
// envelope carries no freshness block.
func FreshnessState(envelope Envelope) string {
	return stringField(mapField(envelope.Truth, "freshness"), "state")
}

// ErrorCodeFromTransport classifies a transport error into a canonical
// envelope error code. It returns the empty string for a nil error.
//
// A 409 is checked first because only the entity map uses it, and it means the
// selector was ambiguous rather than that the request was bad.
func ErrorCodeFromTransport(err error) string {
	if err == nil {
		return ""
	}
	if status, ok := apierr.StatusCode(err); ok && status == http.StatusConflict {
		return "ambiguous"
	}
	return transportErrorCode(err)
}

// transportErrorCode is a local copy of traceErrorCodeFromTransport in
// go/cmd/eshu/trace.go, kept byte-for-byte in behavior.
// TestEntityMapTransportClassifierMatchesTrace in
// go/cmd/eshu/entitymap_parity_test.go runs one input table through both and
// fails when they diverge, and TestTransportClassifierMatchesItsTraceOriginal
// in twin_source_test.go compares the two bodies from this package's own test
// run, with the http.Status* constants normalised to the numbers the
// original spells out.
//
// The message checks run BEFORE the status switch on purpose: an error that
// carries both a status and "connection refused" reached the API layer through
// a broken connection, and reporting it as invalid_argument would send an
// operator to fix their selector instead of their backend. A test pins that
// precedence with a status-400 error whose text says "connection refused".
func transportErrorCode(err error) string {
	if err != nil && strings.Contains(err.Error(), "connection refused") {
		return "backend_unavailable"
	}
	if err != nil && strings.Contains(err.Error(), "request failed") {
		return "backend_unavailable"
	}
	// apierr.StatusCode reports false for a nil error, so the substring checks
	// above keep their precedence and no nil guard is needed here.
	if status, ok := apierr.StatusCode(err); ok {
		switch status {
		case http.StatusBadRequest:
			return "invalid_argument"
		case http.StatusNotFound:
			return "not_found"
		case http.StatusNotImplemented:
			return "unsupported_capability"
		case http.StatusServiceUnavailable:
			return "backend_unavailable"
		default:
			return "api_error"
		}
	}
	return "api_error"
}
