// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package failure_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
	"github.com/eshu-hq/eshu/go/internal/query/tracing"
)

const (
	probePath    = "/api/v0/probe"
	probeMessage = "probe read failed"
)

// probeTracer is the sample package's swappable handler tracer, the same
// shape every converted query family keeps.
var probeTracer = tracing.HandlerTracer()

// answer is how a sample route turns its read failure into a response.
type answer func(w http.ResponseWriter, r *http.Request, err error)

// probe is a sample route whose one read fails with err. ownSpan opens a
// handler span with probeTracer before answering; sibling also opens and ends
// a second span directly under the request span.
type probe struct {
	err     error
	ownSpan bool
	sibling bool
	answer  answer
}

func (p probe) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET "+probePath, func(w http.ResponseWriter, r *http.Request) {
		if p.sibling {
			_, extra := probeTracer.Start(r.Context(), "probe.sibling")
			extra.End()
		}
		if p.ownSpan {
			var span trace.Span
			r, span = tracing.StartHandlerSpanWith(probeTracer, r, "probe.read", probePath, "probe")
			defer span.End()
		}
		p.answer(w, r, p.err)
	})
}

// correct is the converted shape: the reader fence first, then the shared
// server-failure helper with a fixed message.
func correct(w http.ResponseWriter, r *http.Request, err error) {
	if querycontract.WriteGraphReadError(w, r, err, "probe") {
		return
	}
	tracing.WriteServerFailure(w, r, err, http.StatusInternalServerError, probeMessage)
}

// probeSuite builds a one-route suite whose route answers with respond.
// ownSpan names probeTracer as the suite's handler tracer and makes the route
// open a span with it.
func probeSuite(ownSpan, sibling bool, respond answer) failure.Suite {
	suite := failure.Suite{Routes: []failure.Route{{
		Name: "probe", Path: probePath, Message: probeMessage,
		Handler: func(err error) failure.Mounter {
			return probe{err: err, ownSpan: ownSpan, sibling: sibling, answer: respond}
		},
	}}}
	if ownSpan {
		suite.HandlerTracer = &probeTracer
	}
	return suite
}

// violations serves every case once for suite's route and returns the
// assertions any case violated. It also requires Check to agree with
// Evaluate on every case.
func violations(t *testing.T, suite failure.Suite) map[failure.Assertion]bool {
	t.Helper()
	violated := map[failure.Assertion]bool{}
	for _, tc := range failure.Cases() {
		found, err := failure.Evaluate(suite, suite.Routes[0], tc)
		if err != nil {
			t.Fatalf("%s: Evaluate() error = %v", tc.Name, err)
		}
		for _, v := range found {
			violated[v.Assertion] = true
		}
		if checkErr := failure.Check(suite, suite.Routes[0], tc); (checkErr != nil) != (len(found) > 0) {
			t.Fatalf("%s: Check() = %v but Evaluate() found %v", tc.Name, checkErr, found)
		}
	}
	return violated
}

// TestCheckPassesConvertedRoute is the GREEN half: a route built on
// querycontract.WriteGraphReadError and tracing.WriteServerFailure violates
// nothing, whether it opens its own handler span or answers on the request
// span. Not parallel: the handler-span suite swaps probeTracer.
func TestCheckPassesConvertedRoute(t *testing.T) {
	original := probeTracer
	for _, ownSpan := range []bool{true, false} {
		suite := probeSuite(ownSpan, false, correct)
		if got := violations(t, suite); len(got) != 0 {
			t.Errorf("ownSpan=%v: violated %v, want nothing", ownSpan, got)
		}
		t.Run(fmt.Sprintf("ownSpan=%v", ownSpan), func(t *testing.T) { failure.Run(t, suite) })
	}
	if probeTracer != original {
		t.Fatal("Check left probeTracer swapped; want it restored")
	}
}

// TestEveryAssertionHasOneWrongSample is the registry: every assertion Check
// makes has exactly one wrong sample route, and every sample names a real
// assertion. Adding an assertion without a sample fails here.
func TestEveryAssertionHasOneWrongSample(t *testing.T) {
	t.Parallel()
	assertions := failure.Assertions()
	if len(wrongSamples) != len(assertions) {
		t.Fatalf("wrong samples = %d, assertions = %d; want one sample per assertion", len(wrongSamples), len(assertions))
	}
	sampled := map[failure.Assertion]int{}
	for _, sample := range wrongSamples {
		sampled[sample.assertion]++
	}
	for _, assertion := range assertions {
		if sampled[assertion] != 1 {
			t.Errorf("assertion %q has %d wrong samples, want 1", assertion, sampled[assertion])
		}
	}
}

// TestCheckCatchesEachWrongSampleOnItsAssertionOnly is the RED half: every
// wrong sample violates its own assertion and no other. A weakened assertion
// shows up as a sample that violates nothing; a sample wrong in two ways
// shows up as an extra violation. Not parallel: it swaps probeTracer.
func TestCheckCatchesEachWrongSampleOnItsAssertionOnly(t *testing.T) {
	for _, sample := range wrongSamples {
		violated := violations(t, probeSuite(true, sample.sibling, sample.respond))
		if !violated[sample.assertion] {
			t.Errorf("sample for %q violated %v; want it caught by %q", sample.assertion, violated, sample.assertion)
		}
		for assertion := range violated {
			if assertion != sample.assertion {
				t.Errorf("sample for %q also violated %q; a sample must be wrong in one way", sample.assertion, assertion)
			}
		}
	}
}

// TestCanaryScanChecksEveryToken proves the leak scan matches each part of
// the Canary on its own: a body that quotes only the credential, only the
// host, only the relation, or only the Cypher fragment is a NoCanaryInBody
// violation and nothing else. The token list is fixed here, not read from the
// harness, so dropping a token from the scan fails this test. Not parallel:
// it swaps probeTracer.
func TestCanaryScanChecksEveryToken(t *testing.T) {
	canary := failure.Canary
	for _, token := range []string{"hunter2", "pg-primary.canary.internal", "canary_secret_table", "MATCH (n)"} {
		if !strings.Contains(canary, token) {
			t.Fatalf("Canary %q does not contain token %q", canary, token)
		}
		violated := violations(t, probeSuite(true, false, leakOnly(token)))
		if len(violated) != 1 || !violated[failure.NoCanaryInBody] {
			t.Errorf("body leaking only %q violated %v, want only %q", token, violated, failure.NoCanaryInBody)
		}
	}
}

// recordingTB records Error calls instead of failing the enclosing test.
type recordingTB struct {
	testing.TB
	errors []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Error(args ...any) { r.errors = append(r.errors, fmt.Sprint(args...)) }

// TestRunReportsEveryWrongCase proves Run reports each failing case, named by
// route and case, and stays quiet on a converted route. Not parallel: it
// swaps probeTracer.
func TestRunReportsEveryWrongCase(t *testing.T) {
	wrong := &recordingTB{TB: t}
	failure.Run(wrong, probeSuite(true, false, leaky))
	if len(wrong.errors) != 3 {
		t.Fatalf("Run on a leaking route reported %d errors, want 3 (fault, client cancel, inner cancel): %q",
			len(wrong.errors), wrong.errors)
	}
	for _, msg := range wrong.errors {
		if !strings.HasPrefix(msg, "probe / ") || !strings.Contains(msg, string(failure.NoCanaryInBody)) {
			t.Fatalf("Run error %q, want it to name the route and %q", msg, failure.NoCanaryInBody)
		}
	}
	clean := &recordingTB{TB: t}
	failure.Run(clean, probeSuite(true, false, correct))
	if len(clean.errors) != 0 {
		t.Fatalf("Run on a converted route reported %q, want nothing", clean.errors)
	}
}

// TestCasesCarryCanaryAndCause pins the case table: one case per kind, every
// error quoting the canary, and each wrapping the cause its kind names.
func TestCasesCarryCanaryAndCause(t *testing.T) {
	t.Parallel()
	seen := map[failure.Kind]bool{}
	for _, tc := range failure.Cases() {
		if seen[tc.Kind] {
			t.Fatalf("kind %v appears twice", tc.Kind)
		}
		seen[tc.Kind] = true
		if !strings.Contains(tc.Err.Error(), failure.Canary) {
			t.Fatalf("%s: error %q does not quote the canary", tc.Name, tc.Err)
		}
		canceled := errors.Is(tc.Err, context.Canceled)
		if wantCanceled := tc.Kind == failure.ClientCancel || tc.Kind == failure.InnerCancel; canceled != wantCanceled {
			t.Fatalf("%s: wraps context.Canceled = %v, want %v", tc.Name, canceled, wantCanceled)
		}
	}
	for _, kind := range []failure.Kind{failure.Fault, failure.ClientCancel, failure.InnerCancel, failure.ReaderFence} {
		if !seen[kind] {
			t.Fatalf("kind %v has no case", kind)
		}
	}
}

// TestMessageReadsBothErrorShapes pins Message on the plain detail body and
// the envelope body.
func TestMessageReadsBothErrorShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ body, want string }{
		{`{"detail":"fixed"}`, "fixed"},
		{`{"error":{"code":"internal_error","message":"fixed"}}`, "fixed"},
		{`not json`, ""},
	} {
		if got := failure.Message([]byte(tc.body)); got != tc.want {
			t.Fatalf("Message(%s) = %q, want %q", tc.body, got, tc.want)
		}
	}
}
