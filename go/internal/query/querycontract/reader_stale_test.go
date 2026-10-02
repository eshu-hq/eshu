// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querycontract

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// privateWrapped mimics runtime/postgres privateError: a fixed public text
// that hides the driver cause, which stays reachable only through Unwrap.
type privateWrapped struct{ cause error }

func (e privateWrapped) Error() string { return "PostgreSQL reader connection unavailable" }
func (e privateWrapped) Unwrap() error { return e.cause }

func readerFenceErrors() map[string]error {
	return map[string]error{
		"stale joined with deadline": errors.Join(db.ErrReaderStale, context.DeadlineExceeded),
		"pool wait private wrapper": privateWrapped{
			cause: errors.Join(db.ErrReaderUnavailable, context.DeadlineExceeded),
		},
		"stale wrapped by caller": fmt.Errorf("scan dead code: %w", errors.Join(db.ErrReaderStale, context.DeadlineExceeded)),
	}
}

// TestWriteGraphReadErrorMapsReaderFenceFailuresToRetryable503 pins #7523: a
// reader that is stale or whose connection acquisition timed out is a retryable 503 with a
// Retry-After header and a stable envelope, never a 500 carrying Go error text.
func TestWriteGraphReadErrorMapsReaderFenceFailuresToRetryable503(t *testing.T) {
	t.Parallel()
	for name, readerErr := range readerFenceErrors() {
		for _, accept := range []string{EnvelopeMIMEType, ""} {
			t.Run(fmt.Sprintf("%s/accept=%q", name, accept), func(t *testing.T) {
				t.Parallel()
				req := httptest.NewRequest(http.MethodPost, "/api/v0/code/dead-code", nil)
				if accept != "" {
					req.Header.Set("Accept", accept)
				}
				rec := httptest.NewRecorder()

				if !WriteGraphReadError(rec, req, readerErr, "code_quality.dead_code") {
					t.Fatalf("WriteGraphReadError(%v) = false, want the reader error handled", readerErr)
				}
				if rec.Code != http.StatusServiceUnavailable {
					t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
				}
				if got, want := rec.Header().Get("Retry-After"), strconv.Itoa(BackendUnavailableRetryAfterSeconds); got != want {
					t.Fatalf("Retry-After = %q, want %q", got, want)
				}
				body := rec.Body.String()
				for _, leak := range []string{"PostgreSQL", "checkpoint", "deadline", "context"} {
					if strings.Contains(body, leak) {
						t.Fatalf("body leaks Go error text %q: %s", leak, body)
					}
				}
				if accept == "" {
					return
				}
				var envelope ResponseEnvelope
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("decode envelope: %v; body=%s", err, body)
				}
				if envelope.Error == nil || envelope.Error.Code != ErrorCodeBackendUnavailable {
					t.Fatalf("error = %#v, want code %q", envelope.Error, ErrorCodeBackendUnavailable)
				}
				if envelope.Error.Message != ErrReaderRetryable.Error() {
					t.Fatalf("message = %q, want fixed %q", envelope.Error.Message, ErrReaderRetryable.Error())
				}
				if envelope.Error.Capability != "code_quality.dead_code" {
					t.Fatalf("capability = %q", envelope.Error.Capability)
				}
				if got := envelope.Error.Details["retry_after_seconds"]; got != float64(BackendUnavailableRetryAfterSeconds) {
					t.Fatalf("details.retry_after_seconds = %v, want %d", got, BackendUnavailableRetryAfterSeconds)
				}
			})
		}
	}
}

// TestWriteGraphReadErrorLeavesUnknownErrorsUnclaimed proves the reader mapping
// does not over-reach: an error with no sentinel stays the caller's 500.
func TestWriteGraphReadErrorLeavesUnknownErrorsUnclaimed(t *testing.T) {
	t.Parallel()
	for name, err := range map[string]error{
		"plain":          errors.New("boom"),
		"wrong topology": errors.New("PostgreSQL reader topology mismatch"),
		"private no sentinel": privateWrapped{
			cause: errors.New("pq: syntax error"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			if WriteGraphReadError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err, "cap") {
				t.Fatalf("WriteGraphReadError claimed %v", err)
			}
			if rec.Body.Len() != 0 || rec.Header().Get("Retry-After") != "" {
				t.Fatalf("response touched for unclaimed error: %q %v", rec.Body.String(), rec.Header())
			}
		})
	}
}

// TestGraphReadErrorEnvelopeSeamCarriesRetryHint covers the seams that return
// an envelope instead of writing the response themselves.
func TestGraphReadErrorEnvelopeSeamCarriesRetryHint(t *testing.T) {
	t.Parallel()
	status, env, ok := GraphReadErrorEnvelope(errors.Join(db.ErrReaderStale, context.DeadlineExceeded), "cap")
	if !ok || status != http.StatusServiceUnavailable || env.Code != ErrorCodeBackendUnavailable {
		t.Fatalf("got status=%d env=%#v ok=%t", status, env, ok)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	WriteErrorEnvelope(rec, req, status, env)
	if got := rec.Header().Get("Retry-After"); got != strconv.Itoa(BackendUnavailableRetryAfterSeconds) {
		t.Fatalf("Retry-After = %q", got)
	}
}

// TestClassifyBoundedGraphReadErrorKeepsReaderVerdict: the reader's own stale
// verdict wraps context.DeadlineExceeded; the classifier must not turn it into
// a 504 graph-read deadline, or the response would disagree with the reader.
func TestClassifyBoundedGraphReadErrorKeepsReaderVerdict(t *testing.T) {
	t.Parallel()
	expired, cancel := WithBoundedGraphReadDeadlineFor(context.Background(), 1)
	defer cancel()
	<-expired.Done()
	for name, readerErr := range readerFenceErrors() {
		got := ClassifyBoundedGraphReadError(expired, readerErr)
		if errors.Is(got, ErrGraphReadDeadline) {
			t.Fatalf("%s: classified as graph deadline: %v", name, got)
		}
		if got != readerErr && fmt.Sprint(got) != fmt.Sprint(readerErr) {
			t.Fatalf("%s: err changed: %v", name, got)
		}
	}
}

// nonTransientReaderErrors are reader failures that carry
// db.ErrReaderUnavailable but are not a timeout: runtime/postgres
// joins that sentinel onto every connection, identity-query, and replay-query
// failure, including permanent ones. They must stay unclaimed (a 500), never a
// "retry shortly" 503 (#7523 review).
func nonTransientReaderErrors() map[string]error {
	return map[string]error{
		"permission denied on identity query": privateWrapped{
			cause: errors.Join(db.ErrReaderUnavailable, errors.New("pq: permission denied for function pg_control_system")),
		},
		"connection refused": privateWrapped{
			cause: errors.Join(db.ErrReaderUnavailable, errors.New("dial tcp 10.0.0.9:5432: connect: connection refused")),
		},
		"client canceled": privateWrapped{
			cause: errors.Join(db.ErrReaderUnavailable, context.Canceled),
		},
		"bare sentinel": db.ErrReaderUnavailable,
	}
}

// TestWriteGraphReadErrorLeavesNonTransientReaderFailuresUnclaimed pins the
// narrowed contract: only a timeout inside the replay window
// (ErrReaderUnavailable joined with context.DeadlineExceeded: pool wait, dial,
// or identity check) or a stale replay is retryable. A permanent reader
// failure must fall through to the caller's own 500.
func TestWriteGraphReadErrorLeavesNonTransientReaderFailuresUnclaimed(t *testing.T) {
	t.Parallel()
	for name, err := range nonTransientReaderErrors() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			if WriteGraphReadError(rec, httptest.NewRequest(http.MethodGet, "/", nil), err, "cap") {
				t.Fatalf("WriteGraphReadError claimed non-transient reader failure %v", err)
			}
			if _, _, ok := GraphReadErrorEnvelope(err, "cap"); ok {
				t.Fatalf("GraphReadErrorEnvelope claimed non-transient reader failure %v", err)
			}
			if rec.Body.Len() != 0 || rec.Header().Get("Retry-After") != "" {
				t.Fatalf("response touched: %q %v", rec.Body.String(), rec.Header())
			}
		})
	}
}

// TestClassifyBoundedGraphReadErrorDoesNotTreatNonTransientReaderAsFence keeps
// the classifier and the mapper in agreement: a non-deadline reader failure is
// not a fence verdict, so it falls through exactly as before #7523 (an expired
// bounded context still classifies it as the graph deadline, a live one leaves
// it unchanged).
func TestClassifyBoundedGraphReadErrorDoesNotTreatNonTransientReaderAsFence(t *testing.T) {
	t.Parallel()
	for name, err := range nonTransientReaderErrors() {
		if isReaderFenceError(err) {
			t.Fatalf("%s: isReaderFenceError = true, want false", name)
		}
		if got := ClassifyBoundedGraphReadError(context.Background(), err); got != err {
			t.Fatalf("%s: live ctx changed err: %v", name, got)
		}
	}
}
