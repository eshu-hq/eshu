// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package change

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/cli/apierr"
)

// FailureKind names why a change command stopped short of a clean answer.
//
// This package returns the reason and go/cmd/eshu turns it into a process exit
// code, because the exit-code table is part of the CLI's user-facing contract
// and belongs with the cobra wrapper. Keeping the two apart is not cosmetic:
// the shared code-to-exit-code table answers 1 for "building", while both
// change commands answer 4 for a still-building index. A wrapper that routed
// KindFreshness through that table instead of answering 4 directly would
// change the exit code operators script against.
type FailureKind string

const (
	// KindInvalidArgument is a flag combination Validate rejected before any
	// request went out.
	KindInvalidArgument FailureKind = "invalid_argument"
	// KindEnvelope is an error the API reported in the envelope, or a
	// transport error EnvelopeFromTransportError turned into one. Only this
	// kind sets Failure.Code.
	KindEnvelope FailureKind = "envelope"
	// KindFreshness is an answer built from stale or still-building truth.
	KindFreshness FailureKind = "freshness"
	// KindIncomplete is a blocked, partial, or truncated answer.
	KindIncomplete FailureKind = "incomplete"
)

// Kinds returns every FailureKind declared above, in declaration order, so a
// caller that maps a kind onto something else can prove it covered all of them.
//
// It exists because nothing else notices a missing arm. go/cmd/eshu's
// changeExitCode ends in a default that answers 1, and go/.golangci.yml sets
// default-signifies-exhaustive, so the exhaustive linter treats that switch as
// complete no matter which kinds it lists. A new kind added without its arm
// would fall to the default, and a hand-written test table cannot notice a kind
// it does not mention. TestChangeExitCodeMapping walks this slice instead.
//
// Add a new kind here as well as to the const block.
// TestKindsListsEveryDeclaredConstant parses this file and fails when the two
// lists disagree, so a constant that never reaches this slice is caught here
// rather than passing silently through the walk above.
func Kinds() []FailureKind {
	return []FailureKind{KindInvalidArgument, KindEnvelope, KindFreshness, KindIncomplete}
}

// Failure is the error this package returns for every unclean outcome.
// Message is operator-facing text; Kind and Code are what the caller maps to
// an exit code.
type Failure struct {
	// Kind is the reason bucket. Always set.
	Kind FailureKind
	// Code is the envelope's own error code, exactly as the API sent it,
	// untrimmed. Set only when Kind is KindEnvelope, because that is the only
	// kind whose exit code depends on the API's classification rather than on
	// the reason alone.
	Code string
	// Message is what the operator sees.
	Message string
}

// Error implements error.
func (f Failure) Error() string { return f.Message }

// ErrorCodeFromTransport classifies a failed API call into the envelope error
// code the change commands render and exit on.
//
// It is a copy of traceErrorCodeFromTransport in go/cmd/eshu/trace.go, which
// still serves `eshu trace`, `eshu map`, component_api, and the freshness
// family. That directory is package main, so neither side can import the
// other, and both keep their callers. Change one and you must change the
// other: TestTransportErrorCodeParity in go/cmd/eshu feeds one error table to
// both and fails when their answers differ.
//
// The two message checks run before the status switch, and that order is the
// contract, not an accident. An error whose text names a refused connection or
// a failed request classifies as backend_unavailable even when it also carries
// an HTTP status: a retry wrapper that gave up after several attempts can
// carry the last response's status while the real answer is that the backend
// was not reachable. TestErrorCodeFromTransportPrecedence pins this with a
// status-400 error whose message says "connection refused"; that case is the
// only one in the table that distinguishes the two orders.
//
// The nil checks on the two string comparisons are load-bearing -- err.Error()
// panics on a nil error. The status branch needs no such guard, and its
// absence is deliberate rather than an oversight: apierr.StatusCode calls
// errors.As, which reports false for a nil error, so a nil err falls through
// to api_error on its own.
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

// EnvelopeFromTransportError builds the envelope the change commands render
// when the API call itself failed, so a transport failure prints and serializes
// through the same path as an error the API reported.
//
// A nil err returns the zero Envelope rather than dereferencing it. No caller
// reaches this on the success path today; the guard is here so a future one
// that does gets an empty envelope instead of a panic.
func EnvelopeFromTransportError(err error) Envelope {
	if err == nil {
		return Envelope{}
	}
	return Envelope{Error: &EnvelopeError{Code: ErrorCodeFromTransport(err), Message: err.Error()}}
}

// EnvelopeFailure converts an envelope error member into a Failure, walking
// three fallbacks for the operator-facing message: the API's message, then its
// error code, then a fixed string. It returns nil for a nil error member so
// callers can pass envelope.Error straight in.
//
// The fixed fallback names the pre-change impact for both subcommands. That is
// the shipped wording, and `eshu change plan` shares it because it shares this
// function; only an envelope carrying neither a message nor a code can reach
// it, which no route emits today.
func EnvelopeFailure(e *EnvelopeError) error {
	if e == nil {
		return nil
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = strings.TrimSpace(e.Code)
	}
	if message == "" {
		message = "pre-change impact failed"
	}
	return Failure{Kind: KindEnvelope, Code: e.Code, Message: message}
}

// Validate rejects the flag combinations the API would reject anyway, before a
// request is built. Every rejection is KindInvalidArgument, and the messages
// name the flags an operator typed, so they are part of the CLI contract.
func Validate(opts Options) error {
	if len(opts.Changes) > 0 && opts.RepoID == "" {
		return Failure{Kind: KindInvalidArgument, Message: "--repo-id is required when changed files are provided"}
	}
	if len(opts.Changes) == 0 && opts.BaseRef == "" && opts.HeadRef == "" && opts.Target == "" &&
		opts.ServiceName == "" && opts.WorkloadID == "" && opts.ResourceID == "" && opts.ModuleID == "" && opts.Topic == "" {
		return Failure{Kind: KindInvalidArgument, Message: "--file, --base/--head, --target, --service-name, or --topic is required"}
	}
	if opts.Limit <= 0 || opts.Limit > 100 {
		return Failure{Kind: KindInvalidArgument, Message: "--limit must be between 1 and 100"}
	}
	if opts.MaxDepth <= 0 || opts.MaxDepth > 8 {
		return Failure{Kind: KindInvalidArgument, Message: "--max-depth must be between 1 and 8"}
	}
	if opts.Offset < 0 {
		return Failure{Kind: KindInvalidArgument, Message: "--offset must be greater than or equal to 0"}
	}
	return nil
}

// ClassifyImpact fails a pre-change impact answer closed when the truth it was
// built from is stale or still building, or when the answer is truncated or
// partial. Freshness is checked first, so a stale answer reports staleness
// rather than the truncation staleness often causes.
func ClassifyImpact(envelope Envelope) error {
	if state := freshnessState(envelope); state == "stale" || state == "building" {
		return Failure{Kind: KindFreshness, Message: fmt.Sprintf("pre-change impact freshness is %s", state)}
	}
	if boolValue(envelope.Data, "truncated") || boolValue(mapValue(envelope.Data, "answer_packet"), "partial") {
		return Failure{Kind: KindIncomplete, Message: "pre-change impact is partial or truncated"}
	}
	return nil
}

// ClassifyPlan is ClassifyImpact's counterpart for the developer change plan.
// It reads one field more: a plan can come back `blocked`, meaning the API
// declined to recommend actions, which the impact response has no equivalent
// for.
func ClassifyPlan(envelope Envelope) error {
	if state := freshnessState(envelope); state == "stale" || state == "building" {
		return Failure{Kind: KindFreshness, Message: fmt.Sprintf("developer change plan freshness is %s", state)}
	}
	if boolValue(envelope.Data, "blocked") || boolValue(envelope.Data, "truncated") || boolValue(mapValue(envelope.Data, "answer_packet"), "partial") {
		return Failure{Kind: KindIncomplete, Message: "developer change plan is blocked, partial, or truncated"}
	}
	return nil
}
