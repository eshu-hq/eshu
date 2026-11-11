// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/code"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// errCodeFlowAdmissionProbe is the sentinel the failing guarded
// reader below answers every read with. A non-nil error (never a
// panic, never a confident empty result) is the fail-closed proof
// the satellite-consumable constructor wires the guarded port
// through.
var errCodeFlowAdmissionProbe = errors.New("code-flow wire-shape probe: reader unavailable")

// rejectingCodeFlowQueryer is a guarded reader that answers every
// read with the probe error. It implements the db.Queryer surface,
// so constructing the admission satisfier over it proves the
// satellite path needs nothing wider than the request-scoped
// guarded handle.
type rejectingCodeFlowQueryer struct{ err error }

func (q rejectingCodeFlowQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return nil, q.err
}

var _ db.Queryer = rejectingCodeFlowQueryer{}

// codeFlowAdmissionCannedRow is one ListCodeFlow result row:
// fact identity, generation, kind, observation time, and payload.
type codeFlowAdmissionCannedRow struct {
	factID       string
	generationID string
	factKind     string
	observedAt   time.Time
	payload      []byte
}

// codeFlowAdmissionCannedRows serves a fixed row list through the
// db.Rows cursor surface ListCodeFlow ranges over.
type codeFlowAdmissionCannedRows struct {
	rows   []codeFlowAdmissionCannedRow
	index  int
	closed bool
}

func (r *codeFlowAdmissionCannedRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}

func (r *codeFlowAdmissionCannedRows) Scan(dest ...any) error {
	row := r.rows[r.index-1]
	*dest[0].(*string) = row.factID
	*dest[1].(*string) = row.generationID
	*dest[2].(*string) = row.factKind
	*dest[3].(*time.Time) = row.observedAt
	*dest[4].(*[]byte) = row.payload
	return nil
}

func (*codeFlowAdmissionCannedRows) Err() error { return nil }

func (r *codeFlowAdmissionCannedRows) Close() error { r.closed = true; return nil }

// codeFlowAdmissionCannedQueryer serves one fixed row list per
// QueryContext call, so the want and got stores under comparison
// each read identical rows.
type codeFlowAdmissionCannedQueryer struct {
	rows []codeFlowAdmissionCannedRow
}

func (q codeFlowAdmissionCannedQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return &codeFlowAdmissionCannedRows{rows: q.rows}, nil
}

func codeFlowAdmissionObservedAt() time.Time {
	return time.Date(2026, time.June, 28, 1, 0, 0, 0, time.UTC)
}

func codeFlowAdmissionFunctionRow() codeFlowAdmissionCannedRow {
	return codeFlowAdmissionCannedRow{
		factID:       "func-fact-1",
		generationID: "gen-1",
		factKind:     code.DataflowFunctionFactKind,
		observedAt:   codeFlowAdmissionObservedAt(),
		payload: []byte(`{"repo_id":"repo-1","relative_path":"src/handler.go",` +
			`"function_name":"handle","function_uid":"func-handle","language":"go",` +
			`"line_number":3,"cfg_blocks":[{"id":0}],"cfg_edges":[{"from":0,"to":1}],` +
			`"def_use":[{"binding":"q"}],"control_dependencies":[{"controller":4}]}`),
	}
}

func codeFlowAdmissionTaintRow() codeFlowAdmissionCannedRow {
	return codeFlowAdmissionCannedRow{
		factID:       "taint-fact-1",
		generationID: "gen-1",
		factKind:     code.TaintEvidenceFactKind,
		observedAt:   codeFlowAdmissionObservedAt(),
		payload: []byte(`{"repo_id":"repo-1","relative_path":"src/handler.go",` +
			`"function_name":"handle","language":"go",` +
			`"source_kind":"http_request","sink_kind":"sql",` +
			`"source_line":4,"sink_line":5,"confidence":0.8}`),
	}
}

// marshalCodeFlowAdmissionWire projects one read model through the
// same encoding the handlers use (no HTML escaping), so the mirror
// compares want and got through one projection instead of
// hand-written expectations that could drift from the wire.
func marshalCodeFlowAdmissionWire(t *testing.T, model codemodel.CodeFlowReadModel) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(model); err != nil {
		t.Fatalf("marshal read model: %v", err)
	}
	return strings.TrimSpace(sb.String())
}

// TestCodeFlowReadAdmissionTransportIsSatelliteConsumable proves the
// WithReadStore constructor serves the pinned read over nothing
// wider than the guarded db.Queryer the satellite holds: a failing
// guarded reader fails closed (never a panic, never confident
// emptiness), and over canned rows the admission constructor answers
// byte-identical read models to the direct
// NewPostgresCodeFlowStoreWithReadStore constructor — the full
// wire-object mirror for both the function and taint row shapes,
// with want and got unified through one projection.
func TestCodeFlowReadAdmissionTransportIsSatelliteConsumable(t *testing.T) {
	t.Parallel()

	admission := NewCodeFlowReadAdmissionWithReadStore(rejectingCodeFlowQueryer{err: errCodeFlowAdmissionProbe})
	if admission == nil {
		t.Fatal("NewCodeFlowReadAdmissionWithReadStore() = nil, want a CodeFlowReadAdmission bound to the guarded reader")
	}

	ctx := context.Background()
	if _, err := admission.ListCodeFlow(ctx, codemodel.CodeFlowFilter{Kind: codemodel.CodeFlowKindCFGSummary, RepoID: "repo-1"}); err == nil {
		t.Error("ListCodeFlow over an unavailable reader = nil error, want a fail-closed error")
	}

	t.Run("mirror/function", func(t *testing.T) {
		t.Parallel()
		rows := []codeFlowAdmissionCannedRow{codeFlowAdmissionFunctionRow()}
		want, wantErr := codemodel.NewPostgresCodeFlowStoreWithReadStore(codeFlowAdmissionCannedQueryer{rows: rows}).ListCodeFlow(ctx, codemodel.CodeFlowFilter{Kind: codemodel.CodeFlowKindCFGSummary, RepoID: "repo-1"})
		got, gotErr := NewCodeFlowReadAdmissionWithReadStore(codeFlowAdmissionCannedQueryer{rows: rows}).ListCodeFlow(ctx, codemodel.CodeFlowFilter{Kind: codemodel.CodeFlowKindCFGSummary, RepoID: "repo-1"})
		if wantErr != nil || gotErr != nil {
			t.Fatalf("ListCodeFlow() errors = (%v, %v), want nil over canned rows", wantErr, gotErr)
		}
		if len(want.Functions) != 1 || want.Functions[0].FunctionUID != "func-handle" || want.Functions[0].Language != "go" {
			t.Fatalf("want side = %+v, want the full canned function row", want.Functions)
		}
		if wantBytes, gotBytes := marshalCodeFlowAdmissionWire(t, want), marshalCodeFlowAdmissionWire(t, got); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})

	t.Run("mirror/taint", func(t *testing.T) {
		t.Parallel()
		rows := []codeFlowAdmissionCannedRow{codeFlowAdmissionTaintRow()}
		want, wantErr := codemodel.NewPostgresCodeFlowStoreWithReadStore(codeFlowAdmissionCannedQueryer{rows: rows}).ListCodeFlow(ctx, codemodel.CodeFlowFilter{Kind: codemodel.CodeFlowKindTaintPath, RepoID: "repo-1"})
		got, gotErr := NewCodeFlowReadAdmissionWithReadStore(codeFlowAdmissionCannedQueryer{rows: rows}).ListCodeFlow(ctx, codemodel.CodeFlowFilter{Kind: codemodel.CodeFlowKindTaintPath, RepoID: "repo-1"})
		if wantErr != nil || gotErr != nil {
			t.Fatalf("ListCodeFlow() errors = (%v, %v), want nil over canned rows", wantErr, gotErr)
		}
		if len(want.TaintPaths) != 1 || want.TaintPaths[0].SinkKind != "sql" || want.TaintPaths[0].Confidence != 0.8 {
			t.Fatalf("want side = %+v, want the full canned taint row", want.TaintPaths)
		}
		if wantBytes, gotBytes := marshalCodeFlowAdmissionWire(t, want), marshalCodeFlowAdmissionWire(t, got); wantBytes != gotBytes {
			t.Errorf("wire bytes differ:\n want %s\n got  %s", wantBytes, gotBytes)
		}
	})
}

// codeFlowAdmissionRouteDouble is the pin-satisfying double behind
// the route tests: it implements exactly the one pinned read and is
// compile-asserted against the pin, so the Mount proof below runs
// through the admission shape, not the wider staying port.
type codeFlowAdmissionRouteDouble struct {
	model codemodel.CodeFlowReadModel
	err   error
}

func (d *codeFlowAdmissionRouteDouble) ListCodeFlow(_ context.Context, _ codemodel.CodeFlowFilter) (codemodel.CodeFlowReadModel, error) {
	return d.model, d.err
}

var _ CodeFlowReadAdmission = (*codeFlowAdmissionRouteDouble)(nil)

// codeFlowAdmissionPinnedRoutes is the exact route surface the #9669
// W6a pin covers: one bounded code-flow read per route. The Mount
// tests below range over this list, so a route added without pin
// review fails the seal elsewhere and a route dropped breaks every
// subtest here.
var codeFlowAdmissionPinnedRoutes = []struct {
	route string
	key   string
	label string
}{
	{"/api/v0/code/flow/cfg-summary", "functions", "exact_parser_fact"},
	{"/api/v0/code/flow/pdg-summary", "summaries", "partial_derived_summary"},
	{"/api/v0/code/flow/reaching-def", "definitions", "exact_parser_fact"},
	{"/api/v0/code/flow/taint-path", "paths", "derived_reducer_evidence"},
}

// codeFlowAdmissionFixtureModel serves every pinned projection: one
// parser dataflow function and one reducer-owned taint path over a
// fixed observation time, so all four route bodies are deterministic.
func codeFlowAdmissionFixtureModel() codemodel.CodeFlowReadModel {
	return codemodel.CodeFlowReadModel{
		Freshness: FreshnessFresh,
		Functions: []codemodel.CodeFlowFunction{
			{
				RepoID: "repo-1", RelativePath: "src/handler.go", FunctionName: "handle",
				FunctionUID: "func-handle", Language: "go", LineNumber: 3,
				CFGBlocks:           []any{map[string]any{"id": 0, "kind": "entry"}},
				CFGEdges:            []any{map[string]any{"from": 0, "to": 1}},
				DefUse:              []map[string]any{{"binding": "q", "def_line": 4, "use_line": 5}},
				ControlDependencies: []map[string]any{{"controller": 4, "dependent": 5}},
				EvidenceHandle:      "fact://code_dataflow_function/func-fact-1",
				SourceGenerationID:  "gen-1",
				SourceObservedAt:    codeFlowAdmissionObservedAt(),
			},
		},
		TaintPaths: []codemodel.CodeFlowTaintPath{
			{
				RepoID: "repo-1", RelativePath: "src/handler.go", FunctionName: "handle",
				Language: "go", SourceKind: "http_request", SinkKind: "sql",
				SourceLine: 4, SinkLine: 5, Confidence: 0.8,
				EvidenceHandle:     "fact://code_taint_evidence/taint-fact-1",
				SourceGenerationID: "gen-1",
				SourceObservedAt:   codeFlowAdmissionObservedAt(),
			},
		},
	}
}

// codeFlowAdmissionServe mounts a handler over store and serves one
// request through the real mux, so the tests below prove the
// registered routes, not direct method calls.
func codeFlowAdmissionServe(t *testing.T, store CodeFlowStore, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	(&CodeHandler{CodeFlow: store, Profile: ProfileLocalAuthoritative}).Mount(mux)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestCodeFlowReadAdmissionRoutesArePinSatisfiable proves the four
// pinned routes serve byte-exact ListCodeFlow projections through
// the real Mount when the store behind the handler satisfies the
// pin: the pin-double body and the staying-port double body over
// the same model are byte-identical per route, and each route
// carries its own projection key and fact label.
func TestCodeFlowReadAdmissionRoutesArePinSatisfiable(t *testing.T) {
	t.Parallel()

	const body = `{"repo_id":"repo-1","language":"go","symbol":"handle","limit":5}`
	for _, pinned := range codeFlowAdmissionPinnedRoutes {
		t.Run(pinned.route, func(t *testing.T) {
			t.Parallel()
			model := codeFlowAdmissionFixtureModel()
			pinRec := codeFlowAdmissionServe(t, &codeFlowAdmissionRouteDouble{model: model}, pinned.route, body)
			portRec := codeFlowAdmissionServe(t, &fakeCodeFlowStore{model: model}, pinned.route, body)

			if pinRec.Code != http.StatusOK {
				t.Fatalf("pin-double status = %d, want 200; body = %s", pinRec.Code, pinRec.Body.String())
			}
			if portRec.Code != http.StatusOK {
				t.Fatalf("port-double status = %d, want 200; body = %s", portRec.Code, portRec.Body.String())
			}
			if pinRec.Body.String() != portRec.Body.String() {
				t.Errorf("pin-double body differs from port-double body:\n pin  %s\n port %s", pinRec.Body.String(), portRec.Body.String())
			}

			env := decodeCodeFlowEnvelope(t, pinRec)
			data, ok := env.Data.(map[string]any)
			if !ok {
				t.Fatalf("env.Data type = %T, want map[string]any", env.Data)
			}
			rows, ok := data[pinned.key].([]any)
			if !ok || len(rows) != 1 {
				t.Fatalf("data[%q] = %#v, want one projected row", pinned.key, data[pinned.key])
			}
			if got := rows[0].(map[string]any)["fact_label"]; got != pinned.label {
				t.Errorf("fact_label = %#v, want %#v", got, pinned.label)
			}
		})
	}
}

// TestCodeFlowReadAdmissionRoutesFailClosed proves the pinned routes
// keep the staying failure contract through Mount: a store error
// answers the existing fixed message (never the backend error), and
// a nil store preserves the flow.go:104 unavailable verdict.
func TestCodeFlowReadAdmissionRoutesFailClosed(t *testing.T) {
	t.Parallel()

	storeErr := errors.New("code-flow admission probe: backend exploded")
	const body = `{"repo_id":"repo-1","language":"go","symbol":"handle","limit":5}`
	for _, pinned := range codeFlowAdmissionPinnedRoutes {
		t.Run(pinned.route+"/store-error", func(t *testing.T) {
			t.Parallel()
			rec := codeFlowAdmissionServe(t, &codeFlowAdmissionRouteDouble{err: storeErr}, pinned.route, body)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("store-error status = %d, want 500; body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "code-flow read failed") {
				t.Errorf("store-error body = %s, want the fixed %q", rec.Body.String(), "code-flow read failed")
			}
			if strings.Contains(rec.Body.String(), "backend exploded") {
				t.Errorf("store-error body leaks the backend error: %s", rec.Body.String())
			}
		})

		t.Run(pinned.route+"/nil-store", func(t *testing.T) {
			t.Parallel()
			rec := codeFlowAdmissionServe(t, nil, pinned.route, body)
			if rec.Code != http.StatusServiceUnavailable {
				t.Fatalf("nil-store status = %d, want 503; body = %s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), "code-flow store is unavailable") {
				t.Errorf("nil-store body = %s, want the fixed %q", rec.Body.String(), "code-flow store is unavailable")
			}
		})
	}
}
