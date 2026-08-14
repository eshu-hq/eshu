// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package entitymap

import (
	"errors"
	"testing"
)

// statusErr is a transport error carrying an HTTP status, matching what
// go/cmd/eshu's apiHTTPError exposes through apierr.HTTPStatusError.
type statusErr struct {
	status  int
	message string
}

func (e *statusErr) Error() string { return e.message }

func (e *statusErr) HTTPStatusCode() int { return e.status }

// recordingPoster captures the route and body Fetch sends and replays a fixed
// response, so the request shape is asserted without an HTTP server.
type recordingPoster struct {
	path     string
	body     map[string]any
	response Envelope
	err      error
}

func (p *recordingPoster) PostEnvelope(path string, body, result any) error {
	p.path = path
	if typed, ok := body.(map[string]any); ok {
		p.body = typed
	}
	if p.err != nil {
		return p.err
	}
	if envelope, ok := result.(*Envelope); ok {
		*envelope = p.response
	}
	return nil
}

func TestFetchPostsCanonicalRequestBody(t *testing.T) {
	poster := &recordingPoster{response: Envelope{Data: map[string]any{"status": "mapped"}}}

	got, err := Fetch(poster, Options{
		From:         "terraform/aws_lb.main",
		FromType:     "terraform_resource",
		Repo:         "repo-infra",
		Environment:  "prod",
		Relationship: "PROVISIONS_DEPENDENCY_FOR",
		Depth:        2,
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v, want nil", err)
	}
	if poster.path != "/api/v0/impact/entity-map" {
		t.Fatalf("path = %q, want the entity-map route", poster.path)
	}
	for key, want := range map[string]any{
		"from":         "terraform/aws_lb.main",
		"from_type":    "terraform_resource",
		"repo_id":      "repo-infra",
		"environment":  "prod",
		"relationship": "PROVISIONS_DEPENDENCY_FOR",
		"depth":        2,
		"limit":        10,
	} {
		if got := poster.body[key]; got != want {
			t.Fatalf("body[%s] = %#v, want %#v", key, got, want)
		}
	}
	if got.Data["status"] != "mapped" {
		t.Fatalf("Data = %#v, want the poster's response", got.Data)
	}
}

func TestFetchReturnsTransportErrorUnwrapped(t *testing.T) {
	want := &statusErr{status: 503, message: "API error 503: down"}
	poster := &recordingPoster{err: want}

	envelope, err := Fetch(poster, Options{From: "orders"})
	if !errors.Is(err, want) {
		t.Fatalf("Fetch() error = %v, want the transport error itself", err)
	}
	if envelope.Data != nil || envelope.Error != nil {
		t.Fatalf("envelope = %#v, want the zero Envelope on a transport error", envelope)
	}
}

// TestErrorCodeFromTransportPrecedence pins the classification order the
// command has always used. The discriminating case is "status 400 with a
// connection-refused message": moving the status switch ahead of the message
// checks in transportErrorCode turns that case from backend_unavailable into
// invalid_argument, which is the mistake this test exists to catch.
func TestErrorCodeFromTransportPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"nil error", nil, ""},
		{"conflict is ambiguous", &statusErr{status: 409, message: "API error 409: two matches"}, "ambiguous"},
		{
			"conflict wins over a connection-refused message",
			&statusErr{status: 409, message: "API error 409: connection refused"},
			"ambiguous",
		},
		{
			"connection refused wins over a 400 status",
			&statusErr{status: 400, message: "API error 400: connection refused"},
			"backend_unavailable",
		},
		{
			"request failed wins over a 404 status",
			&statusErr{status: 404, message: "request failed: no route to host"},
			"backend_unavailable",
		},
		{"plain connection refused", errors.New("dial tcp: connection refused"), "backend_unavailable"},
		{"plain request failed", errors.New("request failed: timeout"), "backend_unavailable"},
		{"bad request", &statusErr{status: 400, message: "API error 400: bad selector"}, "invalid_argument"},
		{"not found", &statusErr{status: 404, message: "API error 404: missing"}, "not_found"},
		{"not implemented", &statusErr{status: 501, message: "API error 501: no capability"}, "unsupported_capability"},
		{"service unavailable", &statusErr{status: 503, message: "API error 503: draining"}, "backend_unavailable"},
		{"other status", &statusErr{status: 418, message: "API error 418: teapot"}, "api_error"},
		{"no status at all", errors.New("decode response: unexpected EOF"), "api_error"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ErrorCodeFromTransport(tc.err); got != tc.want {
				t.Fatalf("ErrorCodeFromTransport(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestResolveClassifiesEveryOutcome(t *testing.T) {
	for _, tc := range []struct {
		name        string
		envelope    Envelope
		fetchErr    error
		wantKind    FailureKind
		wantCode    string
		wantMessage string
	}{
		{
			name:     "success",
			envelope: Envelope{Data: sampleData("mapped"), Truth: freshTruth()},
		},
		{
			name:        "transport error becomes a synthetic envelope error",
			fetchErr:    &statusErr{status: 503, message: "API error 503: draining"},
			wantKind:    FailureEnvelope,
			wantCode:    "backend_unavailable",
			wantMessage: "API error 503: draining",
		},
		{
			name:        "envelope error",
			envelope:    Envelope{Error: &EnvelopeError{Code: "unsupported_capability", Message: "entity map is not enabled"}},
			wantKind:    FailureEnvelope,
			wantCode:    "unsupported_capability",
			wantMessage: "entity map is not enabled",
		},
		{
			name:        "envelope error falls back to its code",
			envelope:    Envelope{Error: &EnvelopeError{Code: "not_found", Message: "   "}},
			wantKind:    FailureEnvelope,
			wantCode:    "not_found",
			wantMessage: "not_found",
		},
		{
			name:        "envelope error with neither message nor code",
			envelope:    Envelope{Error: &EnvelopeError{}},
			wantKind:    FailureEnvelope,
			wantMessage: "entity map failed",
		},
		{
			name:        "stale index",
			envelope:    Envelope{Data: sampleData("mapped"), Truth: truthWithFreshness("stale")},
			wantKind:    FailureFreshness,
			wantCode:    "stale",
			wantMessage: "entity map freshness is stale",
		},
		{
			name:        "building index",
			envelope:    Envelope{Data: sampleData("mapped"), Truth: truthWithFreshness("building")},
			wantKind:    FailureFreshness,
			wantCode:    "building",
			wantMessage: "entity map freshness is building",
		},
		{
			name:        "ambiguous selector",
			envelope:    Envelope{Data: sampleData("ambiguous"), Truth: freshTruth()},
			wantKind:    FailureAmbiguous,
			wantCode:    "ambiguous",
			wantMessage: "entity map selector is ambiguous",
		},
		{
			name:        "no match",
			envelope:    Envelope{Data: sampleData("no_match"), Truth: freshTruth()},
			wantKind:    FailureNoMatch,
			wantCode:    "no_match",
			wantMessage: "entity map selector did not match a supported entity",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			envelope, failure := Resolve(tc.envelope, tc.fetchErr)
			if tc.wantKind == 0 {
				if failure != nil {
					t.Fatalf("failure = %#v, want nil", failure)
				}
				return
			}
			if failure == nil {
				t.Fatalf("failure = nil, want kind %d", tc.wantKind)
			}
			if failure.Kind != tc.wantKind {
				t.Fatalf("Kind = %d, want %d", failure.Kind, tc.wantKind)
			}
			if failure.Code != tc.wantCode {
				t.Fatalf("Code = %q, want %q", failure.Code, tc.wantCode)
			}
			if failure.Message != tc.wantMessage || failure.Error() != tc.wantMessage {
				t.Fatalf("Message = %q / Error() = %q, want %q", failure.Message, failure.Error(), tc.wantMessage)
			}
			if tc.fetchErr != nil && envelope.Error == nil {
				t.Fatalf("envelope = %#v, want a synthetic error envelope", envelope)
			}
		})
	}
}

// TestResolveChecksFreshnessBeforeStatus pins the order: an ambiguous map on a
// stale index reports the stale index, because re-running the selector against
// truth that is known to be behind cannot resolve the ambiguity.
func TestResolveChecksFreshnessBeforeStatus(t *testing.T) {
	_, failure := Resolve(Envelope{Data: sampleData("ambiguous"), Truth: truthWithFreshness("stale")}, nil)
	if failure == nil || failure.Kind != FailureFreshness {
		t.Fatalf("failure = %#v, want FailureFreshness", failure)
	}
}

func TestFreshnessStateReadsTruthBlock(t *testing.T) {
	for _, tc := range []struct {
		name     string
		envelope Envelope
		want     string
	}{
		{"fresh", Envelope{Truth: freshTruth()}, "fresh"},
		{"no truth block", Envelope{}, ""},
		{"no freshness member", Envelope{Truth: map[string]any{"level": "exact"}}, ""},
		{"freshness is not an object", Envelope{Truth: map[string]any{"freshness": "stale"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := FreshnessState(tc.envelope); got != tc.want {
				t.Fatalf("FreshnessState() = %q, want %q", got, tc.want)
			}
		})
	}
}

func sampleData(status string) map[string]any {
	data := map[string]any{
		"status": status,
		"from":   "terraform/aws_lb.main",
		"resolution": map[string]any{
			"status": "resolved",
			"selected": map[string]any{
				"id":     "tfstate:aws_lb.main",
				"name":   "aws_lb.main",
				"labels": []any{"TerraformResource"},
			},
		},
		"sections": map[string]any{
			"defined_by": []any{
				map[string]any{"relationship_type": "DEFINES", "entity_name": "infra-repo"},
			},
			"deployed_by": []any{},
			"runs_as":     []any{},
			"depends_on": []any{
				map[string]any{"relationship_type": "PROVISIONS_DEPENDENCY_FOR", "entity_name": "checkout", "repo_id": "repo-api"},
			},
			"consumed_by": []any{},
		},
		"evidence": map[string]any{
			"relationship_count": float64(2),
			"truncated":          false,
		},
	}
	if status == "ambiguous" {
		data["resolution"] = map[string]any{
			"status": "ambiguous",
			"candidates": []any{
				map[string]any{"id": "workload:orders-api", "name": "orders", "repo_id": "repo-api"},
				map[string]any{"id": "workload:orders-worker", "name": "orders", "repo_id": "repo-worker"},
			},
		}
	}
	return data
}

func freshTruth() map[string]any { return truthWithFreshness("fresh") }

func truthWithFreshness(state string) map[string]any {
	return map[string]any{
		"level":      "exact",
		"capability": "platform_impact.entity_map",
		"freshness":  map[string]any{"state": state},
	}
}
