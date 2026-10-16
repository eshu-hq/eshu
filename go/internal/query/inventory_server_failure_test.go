// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/testutil/failure"
)

// TestInventorySpannedRoutesAnswerFixedServerFailures is the #7674 slice 10
// regression for the image, tag-history, graph entity, admission decision,
// and runtime drift reads that open a handler span. Each answered 500 with
// the backend error text, or (admission decisions and runtime drift, both on
// the guarded PostgreSQL reader) answered a stale reader with a 500 instead of
// the retryable 503; and every one answered a client cancel as a server
// fault. Not parallel: it swaps queryHandlerTracer.
func TestInventorySpannedRoutesAnswerFixedServerFailures(t *testing.T) {
	const (
		admissionPath = "/api/v0/evidence/admission-decisions?domain=cloud_inventory" +
			"&scope_id=git-repository-scope:team/api&generation_id=generation-1&limit=10"
		tagHistoryPath = "/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0"
		driftBody      = `{"scope_id":"aws-account-1"}`
	)
	graphEntityKindPath := "/api/v0/graph/entities?kind=" + graphEntityKinds[0].key
	routes := []statusFailureRoute{
		{
			route: failure.Route{
				Name: "image list", Path: "/api/v0/images", Message: imageListReadFailedMessage,
				Handler: func(err error) failure.Mounter { return &ImageHandler{Neo4j: failingRunSingleGraph{err: err}} },
			},
			capability: imageListCapability,
		},
		{
			route: failure.Route{
				Name: "tag history", Path: tagHistoryPath, Message: tagHistoryReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &TagHistoryHandler{Neo4j: &fakeTagHistoryGraphReader{err: err}}
				},
			},
			capability: tagHistoryCapability,
		},
		{
			route: failure.Route{
				Name: "graph entity kind counts", Path: "/api/v0/graph/entities",
				Message: graphEntityKindCountReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &GraphEntityInventoryHandler{Neo4j: failingRunSingleGraph{err: err}}
				},
			},
			capability: graphEntityInventoryCapability,
		},
		{
			route: failure.Route{
				Name: "graph entity list", Path: graphEntityKindPath, Message: graphEntityListReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &GraphEntityInventoryHandler{Neo4j: &secondRunFailsGraph{first: validGraphEntityKindCounts(), err: err}}
				},
			},
			capability: graphEntityInventoryCapability,
		},
		{
			route: failure.Route{
				Name: "admission decisions", Path: admissionPath, Message: admissionDecisionReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &EvidenceHandler{AdmissionDecisions: erroringAdmissionDecisionStore{err: err}, Profile: ProfileProduction}
				},
			},
			capability: admissionDecisionCapability,
		},
		{
			route: failure.Route{
				Name: "admission decision evidence", Path: admissionPath + "&include_evidence=true",
				Message: admissionDecisionEvidenceReadFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &EvidenceHandler{AdmissionDecisions: evidenceFailingAdmissionDecisionStore{err: err}, Profile: ProfileProduction}
				},
			},
			capability: admissionDecisionCapability,
		},
		{
			route: failure.Route{
				Name: "runtime drift count", Method: http.MethodPost, Path: "/api/v0/cloud/runtime-drift/findings",
				Body: driftBody, Message: cloudRuntimeDriftReadbackFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &CloudRuntimeDriftHandler{Store: fakeMultiCloudRuntimeDriftStore{countErr: err}}
				},
			},
			capability: cloudRuntimeDriftReadbackCapability,
		},
		{
			route: failure.Route{
				Name: "runtime drift list", Method: http.MethodPost, Path: "/api/v0/cloud/runtime-drift/findings",
				Body: driftBody, Message: cloudRuntimeDriftReadbackFailedMessage,
				Handler: func(err error) failure.Mounter {
					return &CloudRuntimeDriftHandler{Store: fakeMultiCloudRuntimeDriftStore{listErr: err}}
				},
			},
			capability: cloudRuntimeDriftReadbackCapability,
		},
	}
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer}
	for _, r := range routes {
		suite.Routes = append(suite.Routes, r.route)
	}
	failure.Run(t, suite)
	for _, r := range routes {
		assertFenceCapability(t, suite, r.route, r.capability)
	}
}

// TestOperatorControlPlaneAnswersFixedServerFailures covers GET
// /api/v0/status/operator-control-plane, which opens no handler span. Its
// status snapshot read answered 500 with the reader error text
// (fmt.Sprintf("load status: %v", err)). The snapshot reads through the
// guarded PostgreSQL reader and the route has no capability-matrix key
// (#8227), so its fence envelope names none.
func TestOperatorControlPlaneAnswersFixedServerFailures(t *testing.T) {
	route := failure.Route{
		Name: "operator control plane", Path: "/api/v0/status/operator-control-plane",
		Message: operatorControlPlaneStatusReadFailedMessage,
		Handler: func(err error) failure.Mounter { return &StatusHandler{StatusReader: fakeStatusReader{err: err}} },
	}
	suite := failure.Suite{Routes: []failure.Route{route}}
	failure.Run(t, suite)
	assertFenceCapability(t, suite, route, "")
}

// TestFixedFailureStepsWithoutABackendRead covers the failure steps that read
// no store: the graph entity count decode, the tag-history continuation seal,
// and the embedded surface inventory load. Each answers 500 with its fixed
// message and records the cause on the span. The decode and the seal had
// echoed the error text or a fixed text with no span record before. Not
// parallel: it swaps queryHandlerTracer.
func TestFixedFailureStepsWithoutABackendRead(t *testing.T) {
	spanned := failure.Suite{HandlerTracer: &queryHandlerTracer}
	sealErr := fmt.Errorf("seal: %s", failure.Canary)
	steps := []struct {
		suite   failure.Suite
		route   failure.Route
		handler failure.Mounter
	}{
		{
			suite: spanned,
			route: failure.Route{Name: "graph entity count decode", Path: "/api/v0/graph/entities", Message: graphEntityKindCountDecodeFailedMessage},
			// fakeTagHistoryGraphReader stands in as a generic GraphQuery that
			// answers fixed rows: here one count row with an unknown key.
			handler: &GraphEntityInventoryHandler{Neo4j: &fakeTagHistoryGraphReader{rows: []map[string]any{{failure.Canary: 1}}}},
		},
		{
			suite: spanned,
			route: failure.Route{
				Name: "tag history continuation seal", Message: tagHistoryContinuationSealFailedMessage,
				Path: "/api/v0/images/tag-history?repository_id=oci-registry://ghcr.io/eshu-hq/demo&tag=1.0.0&limit=1",
			},
			handler: &TagHistoryHandler{
				Neo4j: &fakeTagHistoryGraphReader{rows: []map[string]any{
					tagHistoryRowMap("t1", "sha256:d1", "", "2026-06-01T00:00:00Z", false),
					tagHistoryRowMap("t2", "sha256:d2", "sha256:d1", "2026-06-02T00:00:00Z", true),
				}},
				Cursors: failingTagHistorySealer{err: sealErr},
			},
		},
		{
			route:   failure.Route{Name: "surface inventory load", Path: "/api/v0/surface-inventory", Message: surfaceInventoryUnavailableMessage},
			handler: surfaceInventoryWithLoadError(fmt.Errorf("decode: %s", failure.Canary)),
		},
	}
	for _, step := range steps {
		rec, spans := failure.Serve(step.suite, step.route, step.handler, false)
		if rec.Code != http.StatusInternalServerError || failure.Message(rec.Body.Bytes()) != step.route.Message {
			t.Errorf("%s: status = %d, body = %s; want 500 %q", step.route.Name, rec.Code, rec.Body.String(), step.route.Message)
		}
		assertOneSpanException(t, spans, step.route.Message)
	}
}

// assertOneSpanException finds the span a failure helper marks (the handler
// span when one opened, else the request span) and requires Error status
// described by message and exactly one exception event.
func assertOneSpanException(t *testing.T, spans []sdktrace.ReadOnlySpan, message string) {
	t.Helper()
	marked := markedSpan(t, spans)
	if marked.Status().Code != codes.Error || marked.Status().Description != message {
		t.Errorf("span %s status = %v %q, want Error %q", marked.Name(), marked.Status().Code, marked.Status().Description, message)
	}
	if got := exceptionEvents(marked); got != 1 {
		t.Errorf("span %s exception events = %d, want 1", marked.Name(), got)
	}
}

// markedSpan returns the span directly under the harness's "request" span
// when the route opened one, else the request span itself. It matches by
// parent span ID, not by end order, so the pick does not depend on which span
// the recorder saw end first.
func markedSpan(t *testing.T, spans []sdktrace.ReadOnlySpan) sdktrace.ReadOnlySpan {
	t.Helper()
	var request sdktrace.ReadOnlySpan
	for _, span := range spans {
		if span.Name() == "request" {
			request = span
		}
	}
	if request == nil {
		t.Fatalf("no request span among %d ended spans", len(spans))
	}
	for _, span := range spans {
		if span.Parent().SpanID() == request.SpanContext().SpanID() {
			return span
		}
	}
	return request
}

// exceptionEvents counts the exception events on span.
func exceptionEvents(span sdktrace.ReadOnlySpan) int {
	n := 0
	for _, event := range span.Events() {
		if event.Name == "exception" {
			n++
		}
	}
	return n
}

// TestGraphEntityGraphVerdictRecordsOnHandlerSpan pins the one span signal
// GET /api/v0/graph/entities keeps beyond the shared helper: a graph outage
// answers the retryable 503 through WriteGraphReadError and still records the
// graph error on the handler span as one exception event, without setting the
// span status to Error (a verdict, not a server fault). Not parallel: it
// swaps queryHandlerTracer.
func TestGraphEntityGraphVerdictRecordsOnHandlerSpan(t *testing.T) {
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer}
	route := failure.Route{Name: "graph entity graph outage", Path: "/api/v0/graph/entities"}
	handler := &GraphEntityInventoryHandler{
		Neo4j: failingRunSingleGraph{err: fmt.Errorf("%s: %w", failure.Canary, ErrGraphUnavailable)},
	}
	rec, spans := failure.Serve(suite, route, handler, false)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body = %s", rec.Code, rec.Body.String())
	}
	marked := markedSpan(t, spans)
	if marked.Name() == "request" {
		t.Fatalf("no handler span opened under the request span")
	}
	if got := exceptionEvents(marked); got != 1 {
		t.Errorf("handler span exception events = %d, want 1 (the graph error)", got)
	}
	if marked.Status().Code == codes.Error {
		t.Errorf("handler span status = Error, want a graph 503 verdict to leave it unset")
	}
}

// TestRuntimeDriftReadbackFailureKeepsProfileBlock pins the drift readback
// failure envelope: a server fault on POST /api/v0/cloud/runtime-drift/findings
// and on GET /api/v0/investigations/drift/packet still answers the 500
// internal_error envelope with the profile block the old WriteContractError
// built, now that writeReadbackFailure builds the envelope itself. Not
// parallel: it swaps queryHandlerTracer.
func TestRuntimeDriftReadbackFailureKeepsProfileBlock(t *testing.T) {
	suite := failure.Suite{HandlerTracer: &queryHandlerTracer}
	routes := []failure.Route{
		{Name: "runtime drift findings", Method: http.MethodPost, Path: "/api/v0/cloud/runtime-drift/findings", Body: `{"scope_id":"aws-account-1"}`},
		{Name: "drift packet", Path: "/api/v0/investigations/drift/packet?scope_id=aws-account-1"},
	}
	for _, route := range routes {
		handler := &CloudRuntimeDriftHandler{Store: fakeMultiCloudRuntimeDriftStore{
			countErr: errors.New("backend: " + failure.Canary), listErr: errors.New("backend: " + failure.Canary),
		}}
		rec, _ := failure.Serve(suite, route, handler, false)
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("%s: status = %d, want 500; body = %s", route.Name, rec.Code, rec.Body.String())
			continue
		}
		var body struct {
			Error struct {
				Code     string            `json:"code"`
				Profiles map[string]string `json:"profiles"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Errorf("%s: decode body: %v; body = %s", route.Name, err, rec.Body.String())
			continue
		}
		wantRequired := string(querycontract.RequiredProfile(cloudRuntimeDriftReadbackCapability))
		if body.Error.Code != string(ErrorCodeInternalError) ||
			body.Error.Profiles["current"] != string(ProfileProduction) || body.Error.Profiles["required"] != wantRequired {
			t.Errorf("%s: error = %+v, want internal_error with profiles current=%s required=%s",
				route.Name, body.Error, ProfileProduction, wantRequired)
		}
	}
}

// secondRunFailsGraph answers its first Run with first and fails every later
// Run with err, so a route's second graph read is the one that fails.
type secondRunFailsGraph struct {
	first []map[string]any
	err   error
	runs  int
}

func (g *secondRunFailsGraph) Run(context.Context, string, map[string]any) ([]map[string]any, error) {
	g.runs++
	if g.runs == 1 {
		return g.first, nil
	}
	return nil, g.err
}

func (g *secondRunFailsGraph) RunSingle(context.Context, string, map[string]any) (map[string]any, error) {
	return nil, g.err
}

// validGraphEntityKindCounts is one facet-count row naming every kind with a
// zero count, which decodeGraphEntityKindCounts accepts.
func validGraphEntityKindCounts() []map[string]any {
	row := make(map[string]any, len(graphEntityKinds))
	for _, kind := range graphEntityKinds {
		row[kind.key] = int64(0)
	}
	return []map[string]any{row}
}

// evidenceFailingAdmissionDecisionStore answers one decision and fails its
// evidence read with err.
type evidenceFailingAdmissionDecisionStore struct{ err error }

func (s evidenceFailingAdmissionDecisionStore) ListAdmissionDecisions(
	context.Context, AdmissionDecisionReadFilter,
) ([]AdmissionDecisionReadRow, error) {
	return []AdmissionDecisionReadRow{{DecisionID: "decision-1"}}, nil
}

func (s evidenceFailingAdmissionDecisionStore) ListAdmissionDecisionEvidence(
	context.Context, string, int,
) ([]AdmissionDecisionEvidenceRow, error) {
	return nil, s.err
}

// failingTagHistorySealer fails every Seal with err.
type failingTagHistorySealer struct{ err error }

func (s failingTagHistorySealer) Seal([]byte, []byte) (string, error) { return "", s.err }

func (s failingTagHistorySealer) Open(string, []byte) ([]byte, error) {
	return nil, errors.New("failingTagHistorySealer opens nothing")
}

// surfaceInventoryWithLoadError returns a handler whose embedded inventory
// load failed with err. The real load reads a compiled-in artifact that does
// not fail, so the test spends the handler's sync.Once on a no-op and sets the
// cached error the way a failed load would leave it.
func surfaceInventoryWithLoadError(err error) *SurfaceInventoryHandler {
	h := &SurfaceInventoryHandler{}
	h.once.Do(func() {})
	h.loadErr = err
	return h
}
