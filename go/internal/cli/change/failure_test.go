// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package change

import (
	"errors"
	"fmt"
	"testing"
)

// statusError is a stand-in for go/cmd/eshu's unexported apiHTTPError. It
// exists because that type is in package main and cannot be imported; what it
// reproduces is the only thing this package reads from it, the
// apierr.HTTPStatusError contract.
type statusError struct {
	status  int
	message string
}

func (e statusError) Error() string { return e.message }

func (e statusError) HTTPStatusCode() int { return e.status }

// TestErrorCodeFromTransportPrecedence pins the order the classification runs
// in, not just its answers.
//
// The discriminating case is "status 400 whose text says connection refused".
// A status switch placed ahead of the message checks answers invalid_argument
// for it; the shipped order answers backend_unavailable. Every other case in
// this table passes under either order, so this one case is what the test is
// for. Reordering ErrorCodeFromTransport to run the switch first must turn
// this row red and leave the rest green.
func TestErrorCodeFromTransportPrecedence(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			name: "refused message beats a 400 status",
			err:  statusError{status: 400, message: "Post \"http://localhost:8080/api/v0/impact/pre-change\": dial tcp 127.0.0.1:8080: connect: connection refused"},
			want: "backend_unavailable",
		},
		{
			name: "request failed message beats a 404 status",
			err:  statusError{status: 404, message: "request failed after 3 attempts"},
			want: "backend_unavailable",
		},
		{
			name: "refused message with no status at all",
			err:  errors.New("dial tcp: connect: connection refused"),
			want: "backend_unavailable",
		},
		{name: "400 without a matching message", err: statusError{status: 400, message: "API error 400: bad request"}, want: "invalid_argument"},
		{name: "404", err: statusError{status: 404, message: "API error 404: no such repo"}, want: "not_found"},
		{name: "501", err: statusError{status: 501, message: "API error 501: unsupported"}, want: "unsupported_capability"},
		{name: "503", err: statusError{status: 503, message: "API error 503: draining"}, want: "backend_unavailable"},
		{name: "500 falls through to api_error", err: statusError{status: 500, message: "API error 500: boom"}, want: "api_error"},
		{name: "no status and no matching message", err: errors.New("context deadline exceeded"), want: "api_error"},
		{name: "wrapped status is unwrapped", err: fmt.Errorf("post envelope: %w", statusError{status: 404, message: "API error 404: gone"}), want: "not_found"},
		{name: "nil error", err: nil, want: "api_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ErrorCodeFromTransport(tc.err); got != tc.want {
				t.Fatalf("ErrorCodeFromTransport(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

// TestValidateRejectsEveryBadFlagCombination covers each rejection Validate
// owns, with the exact operator-facing message, because those strings are the
// CLI's user contract and a move must not reword them.
func TestValidateRejectsEveryBadFlagCombination(t *testing.T) {
	t.Parallel()

	valid := Options{RepoID: "repo-1", Changes: []FileChange{{Path: "go/a.go", Status: "modified"}}, MaxDepth: 4, Limit: 25}

	for _, tc := range []struct {
		name string
		opts Options
		want string
	}{
		{
			name: "changed files without a repo id",
			opts: Options{Changes: []FileChange{{Path: "go/a.go"}}, MaxDepth: 4, Limit: 25},
			want: "--repo-id is required when changed files are provided",
		},
		{
			name: "no selector at all",
			opts: Options{RepoID: "repo-1", MaxDepth: 4, Limit: 25},
			want: "--file, --base/--head, --target, --service-name, or --topic is required",
		},
		{name: "limit zero", opts: withLimit(valid, 0), want: "--limit must be between 1 and 100"},
		{name: "limit over cap", opts: withLimit(valid, 101), want: "--limit must be between 1 and 100"},
		{name: "max depth zero", opts: withDepth(valid, 0), want: "--max-depth must be between 1 and 8"},
		{name: "max depth over cap", opts: withDepth(valid, 9), want: "--max-depth must be between 1 and 8"},
		{name: "negative offset", opts: withOffset(valid, -1), want: "--offset must be greater than or equal to 0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := Validate(tc.opts)
			var failure Failure
			if !errors.As(err, &failure) {
				t.Fatalf("Validate() error = %v, want a change.Failure", err)
			}
			if failure.Kind != KindInvalidArgument {
				t.Fatalf("Validate() kind = %q, want %q", failure.Kind, KindInvalidArgument)
			}
			if failure.Message != tc.want {
				t.Fatalf("Validate() message = %q, want %q", failure.Message, tc.want)
			}
		})
	}
}

// TestValidateAcceptsEachSelector proves the "no selector" rejection really is
// satisfied by every selector the message advertises, so a future edit cannot
// drop one from the condition while the message keeps promising it.
func TestValidateAcceptsEachSelector(t *testing.T) {
	t.Parallel()

	base := Options{RepoID: "repo-1", MaxDepth: 4, Limit: 25}
	for name, opts := range map[string]Options{
		"changes":      withChanges(base, []FileChange{{Path: "go/a.go", Status: "modified"}}),
		"base ref":     withBaseRef(base, "main"),
		"head ref":     withHeadRef(base, "feature"),
		"target":       withTarget(base, "svc:api"),
		"service name": withServiceName(base, "api"),
		"workload id":  withWorkloadID(base, "workload-1"),
		"resource id":  withResourceID(base, "arn:aws:x"),
		"module id":    withModuleID(base, "module-1"),
		"topic":        withTopic(base, "auth"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := Validate(opts); err != nil {
				t.Fatalf("Validate() error = %v, want nil", err)
			}
		})
	}
}

// TestClassifyImpactAndPlan pins both fail-closed ladders, including the two
// places the plan's contract is wider than the impact's: `blocked` counts as
// incomplete for the plan and is ignored for the impact.
func TestClassifyImpactAndPlan(t *testing.T) {
	t.Parallel()

	fresh := map[string]any{"freshness": map[string]any{"state": "fresh"}}

	for _, tc := range []struct {
		name     string
		envelope Envelope
		plan     bool
		wantKind FailureKind
		wantMsg  string
	}{
		{name: "impact clean", envelope: Envelope{Truth: fresh, Data: map[string]any{}}},
		{name: "plan clean", envelope: Envelope{Truth: fresh, Data: map[string]any{}}, plan: true},
		{
			name:     "impact stale",
			envelope: Envelope{Truth: map[string]any{"freshness": map[string]any{"state": "stale"}}},
			wantKind: KindFreshness, wantMsg: "pre-change impact freshness is stale",
		},
		{
			name:     "impact building",
			envelope: Envelope{Truth: map[string]any{"freshness": map[string]any{"state": "building"}}},
			wantKind: KindFreshness, wantMsg: "pre-change impact freshness is building",
		},
		{
			name:     "plan stale",
			envelope: Envelope{Truth: map[string]any{"freshness": map[string]any{"state": "stale"}}}, plan: true,
			wantKind: KindFreshness, wantMsg: "developer change plan freshness is stale",
		},
		{
			name:     "impact truncated",
			envelope: Envelope{Truth: fresh, Data: map[string]any{"truncated": true}},
			wantKind: KindIncomplete, wantMsg: "pre-change impact is partial or truncated",
		},
		{
			name:     "impact partial answer packet",
			envelope: Envelope{Truth: fresh, Data: map[string]any{"answer_packet": map[string]any{"partial": true}}},
			wantKind: KindIncomplete, wantMsg: "pre-change impact is partial or truncated",
		},
		{
			name:     "impact ignores blocked",
			envelope: Envelope{Truth: fresh, Data: map[string]any{"blocked": true}},
		},
		{
			name:     "plan blocked",
			envelope: Envelope{Truth: fresh, Data: map[string]any{"blocked": true}}, plan: true,
			wantKind: KindIncomplete, wantMsg: "developer change plan is blocked, partial, or truncated",
		},
		{
			name:     "freshness outranks truncation",
			envelope: Envelope{Truth: map[string]any{"freshness": map[string]any{"state": "stale"}}, Data: map[string]any{"truncated": true}},
			wantKind: KindFreshness, wantMsg: "pre-change impact freshness is stale",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ClassifyImpact(tc.envelope)
			if tc.plan {
				err = ClassifyPlan(tc.envelope)
			}
			if tc.wantKind == "" {
				if err != nil {
					t.Fatalf("classify = %v, want nil", err)
				}
				return
			}
			var failure Failure
			if !errors.As(err, &failure) {
				t.Fatalf("classify error = %v, want a change.Failure", err)
			}
			if failure.Kind != tc.wantKind || failure.Message != tc.wantMsg {
				t.Fatalf("classify = {%q %q}, want {%q %q}", failure.Kind, failure.Message, tc.wantKind, tc.wantMsg)
			}
		})
	}
}

// TestEnvelopeFailureMessageFallbacks pins the three-step fallback the
// operator-facing message walks, and that Code survives untrimmed so
// go/cmd/eshu's exit-code table sees exactly what the API sent.
func TestEnvelopeFailureMessageFallbacks(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		in       *EnvelopeError
		wantMsg  string
		wantCode string
		wantNil  bool
	}{
		{name: "nil", in: nil, wantNil: true},
		{name: "message wins", in: &EnvelopeError{Code: "not_found", Message: "  no such repo  "}, wantMsg: "no such repo", wantCode: "not_found"},
		{name: "code when message is blank", in: &EnvelopeError{Code: " not_found ", Message: "   "}, wantMsg: "not_found", wantCode: " not_found "},
		{name: "constant when both are blank", in: &EnvelopeError{}, wantMsg: "pre-change impact failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := EnvelopeFailure(tc.in)
			if tc.wantNil {
				if err != nil {
					t.Fatalf("EnvelopeFailure(nil) = %v, want nil", err)
				}
				return
			}
			var failure Failure
			if !errors.As(err, &failure) {
				t.Fatalf("EnvelopeFailure() error = %v, want a change.Failure", err)
			}
			if failure.Kind != KindEnvelope {
				t.Fatalf("kind = %q, want %q", failure.Kind, KindEnvelope)
			}
			if failure.Message != tc.wantMsg || failure.Error() != tc.wantMsg {
				t.Fatalf("message = %q / %q, want %q", failure.Message, failure.Error(), tc.wantMsg)
			}
			if failure.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", failure.Code, tc.wantCode)
			}
		})
	}
}

// TestEnvelopeFromTransportError checks the envelope a transport failure is
// turned into, including the nil-error guard that keeps the helper from
// dereferencing a nil error if a future caller reaches it on the success path.
func TestEnvelopeFromTransportError(t *testing.T) {
	t.Parallel()

	envelope := EnvelopeFromTransportError(statusError{status: 503, message: "API error 503: draining"})
	if envelope.Error == nil {
		t.Fatal("EnvelopeFromTransportError() error member = nil")
	}
	if got, want := envelope.Error.Code, "backend_unavailable"; got != want {
		t.Fatalf("code = %q, want %q", got, want)
	}
	if got, want := envelope.Error.Message, "API error 503: draining"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
	if got := EnvelopeFromTransportError(nil); got.Error != nil {
		t.Fatalf("EnvelopeFromTransportError(nil) = %+v, want a zero Envelope", got)
	}
}

func withLimit(o Options, v int) Options            { o.Limit = v; return o }
func withDepth(o Options, v int) Options            { o.MaxDepth = v; return o }
func withOffset(o Options, v int) Options           { o.Offset = v; return o }
func withChanges(o Options, v []FileChange) Options { o.Changes = v; return o }
func withBaseRef(o Options, v string) Options       { o.BaseRef = v; return o }
func withHeadRef(o Options, v string) Options       { o.HeadRef = v; return o }
func withTarget(o Options, v string) Options        { o.Target = v; return o }
func withServiceName(o Options, v string) Options   { o.ServiceName = v; return o }
func withWorkloadID(o Options, v string) Options    { o.WorkloadID = v; return o }
func withResourceID(o Options, v string) Options    { o.ResourceID = v; return o }
func withModuleID(o Options, v string) Options      { o.ModuleID = v; return o }
func withTopic(o Options, v string) Options         { o.Topic = v; return o }
