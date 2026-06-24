// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

// Answer parity tests (issue #1795).
//
// These tests prove that the HTTP API, the MCP dispatch path, and the CLI
// envelope contract agree on the canonical answer for the SAME fixture graph.
// The deployable model must never yield a second interpretation of the graph,
// so the comparable envelope fields — truth level, truth basis, freshness,
// result limits, evidence handles, and missing-evidence behavior — are asserted
// EQUAL across surfaces.
//
// Design notes:
//   - The MCP dispatch path (dispatchTool -> resolveRoute -> the mounted
//     query.APIRouter handler -> parseCanonicalEnvelope) drives the request
//     through the exact same http.Handler the HTTP API serves. Driving the HTTP
//     surface directly here (httptest with Accept: envelope) and the MCP surface
//     through dispatchTool against the same handler instance pins both surfaces
//     to one envelope contract for one fixture.
//   - The CLI consumes the same canonical envelope shape (data/truth/error). The
//     CLI rendering lives in package main (go/cmd/eshu) and cannot be imported
//     here without a cycle, so these tests assert that the canonical envelope a
//     CLI parses (a Data/Truth/Error JSON document) round-trips losslessly: the
//     CLI summary is convenience, the envelope stays canonical. The matching
//     CLI-side proof that the rendered summary preserves access to the envelope
//     lives in go/cmd/eshu (trace_test.go round-trips the same shape).
//   - The MCP #1791 text summary is asserted as CONVENIENCE output only. It is
//     checked for presence and truth/error fidelity WITHOUT replacing the
//     structured-content assertions, which remain the source of truth.

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query"
)

// comparableEnvelope captures the cross-surface fields that every answer surface
// MUST agree on for a given fixture and question. It is the reduced projection
// the parity layer compares between the HTTP and MCP surfaces.
type comparableEnvelope struct {
	truthLevel      query.TruthLevel
	truthBasis      query.TruthBasis
	truthCapability string
	freshnessState  query.FreshnessState
	resultLimits    map[string]any
	evidenceHandles []string
	missingEvidence []string
	errorCode       query.ErrorCode
	errorCapability string
	hasError        bool
}

// extractComparable reduces a canonical ResponseEnvelope to the fields that must
// match across surfaces. It tolerates a nil truth (error envelopes) and reads
// result limits, evidence handles, and missing evidence from the well-known
// data shapes used by the answer workflows under test.
func extractComparable(t *testing.T, env *query.ResponseEnvelope) comparableEnvelope {
	t.Helper()

	if env == nil {
		t.Fatal("envelope is nil, want canonical envelope")
	}

	cmp := comparableEnvelope{}
	if env.Error != nil {
		cmp.hasError = true
		cmp.errorCode = env.Error.Code
		cmp.errorCapability = env.Error.Capability
	}
	if env.Truth != nil {
		cmp.truthLevel = env.Truth.Level
		cmp.truthBasis = env.Truth.Basis
		cmp.truthCapability = env.Truth.Capability
		cmp.freshnessState = env.Truth.Freshness.State
	}

	data, _ := env.Data.(map[string]any)
	cmp.resultLimits = resultLimitsFromData(data)
	cmp.evidenceHandles = evidenceHandlesFromData(data)
	cmp.missingEvidence = missingEvidenceFromData(data)
	return cmp
}

// requireParity asserts that two surface projections of the same question agree
// on every comparable field. It is the heart of the parity layer: an inequality
// here is a real cross-surface discrepancy, not a cosmetic difference.
func requireParity(t *testing.T, surfaceA, surfaceB string, a, b comparableEnvelope) {
	t.Helper()

	if a.hasError != b.hasError {
		t.Fatalf("%s hasError=%t but %s hasError=%t; surfaces disagree on error vs success", surfaceA, a.hasError, surfaceB, b.hasError)
	}
	if a.errorCode != b.errorCode {
		t.Fatalf("error code parity: %s=%q, %s=%q", surfaceA, a.errorCode, surfaceB, b.errorCode)
	}
	if a.errorCapability != b.errorCapability {
		t.Fatalf("error capability parity: %s=%q, %s=%q", surfaceA, a.errorCapability, surfaceB, b.errorCapability)
	}
	if a.truthLevel != b.truthLevel {
		t.Fatalf("truth level parity: %s=%q, %s=%q", surfaceA, a.truthLevel, surfaceB, b.truthLevel)
	}
	if a.truthBasis != b.truthBasis {
		t.Fatalf("truth basis parity: %s=%q, %s=%q", surfaceA, a.truthBasis, surfaceB, b.truthBasis)
	}
	if a.truthCapability != b.truthCapability {
		t.Fatalf("truth capability parity: %s=%q, %s=%q", surfaceA, a.truthCapability, surfaceB, b.truthCapability)
	}
	if a.freshnessState != b.freshnessState {
		t.Fatalf("freshness parity: %s=%q, %s=%q", surfaceA, a.freshnessState, surfaceB, b.freshnessState)
	}
	if !equalStringSlices(a.evidenceHandles, b.evidenceHandles) {
		t.Fatalf("evidence handle parity: %s=%v, %s=%v", surfaceA, a.evidenceHandles, surfaceB, b.evidenceHandles)
	}
	if !equalStringSlices(a.missingEvidence, b.missingEvidence) {
		t.Fatalf("missing evidence parity: %s=%v, %s=%v", surfaceA, a.missingEvidence, surfaceB, b.missingEvidence)
	}
	if !equalJSON(a.resultLimits, b.resultLimits) {
		t.Fatalf("result limits parity: %s=%v, %s=%v", surfaceA, a.resultLimits, surfaceB, b.resultLimits)
	}
}

// httpEnvelope drives a request straight through a mounted query handler using
// the canonical envelope Accept header, mirroring what a hosted HTTP client
// receives. It returns the parsed canonical envelope so the HTTP surface can be
// compared against MCP.
func httpEnvelope(t *testing.T, handler http.Handler, method, path string, body any) *query.ResponseEnvelope {
	t.Helper()

	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal HTTP body: %v", err)
		}
		reader = strings.NewReader(string(encoded))
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", query.EnvelopeMIMEType)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	env, ok := parseCanonicalEnvelope(rec.Body.Bytes())
	if !ok {
		t.Fatalf("HTTP %s %s did not return a canonical envelope; status=%d body=%s", method, path, rec.Code, rec.Body.String())
	}
	return env
}

// mcpEnvelope drives the same logical question through the MCP dispatch path and
// returns both the canonical envelope and the #1791 convenience text summary so
// callers can assert the summary is present WITHOUT weakening the structured
// assertions.
func mcpEnvelope(t *testing.T, handler http.Handler, tool string, args map[string]any) (*query.ResponseEnvelope, string) {
	t.Helper()

	result, err := dispatchTool(
		context.Background(),
		handler,
		tool,
		args,
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool(%q) error = %v, want nil", tool, err)
	}
	if result.Envelope == nil {
		t.Fatalf("dispatchTool(%q) envelope is nil, want canonical envelope", tool)
	}
	summary := summarizeToolText(tool, result.Envelope)
	return result.Envelope, summary
}

// resultLimitsFromData returns the canonical bounded-drilldown block when
// present. Compare-environment answers expose paging truth at the top level
// (limit/truncated/coverage); context answers expose result_limits. The parity
// layer normalizes both into one comparable map.
func resultLimitsFromData(data map[string]any) map[string]any {
	if data == nil {
		return nil
	}
	if block, ok := data["result_limits"].(map[string]any); ok {
		return block
	}
	limits := map[string]any{}
	if v, ok := data["limit"]; ok {
		limits["limit"] = v
	}
	if v, ok := data["truncated"]; ok {
		limits["truncated"] = v
	}
	if coverage, ok := data["coverage"].(map[string]any); ok {
		for _, key := range []string{"left_truncated", "right_truncated"} {
			if v, ok := coverage[key]; ok {
				limits[key] = v
			}
		}
	}
	if len(limits) == 0 {
		return nil
	}
	return limits
}

// evidenceHandlesFromData extracts the stable evidence handles an answer cites,
// in deterministic order, so the HTTP and MCP surfaces can be asserted to cite
// the identical evidence. Compare answers cite cloud-resource ids on each side.
func evidenceHandlesFromData(data map[string]any) []string {
	if data == nil {
		return nil
	}
	handles := []string{}
	for _, side := range []string{"left", "right"} {
		snap, ok := data[side].(map[string]any)
		if !ok {
			continue
		}
		for _, raw := range asSlice(snap["cloud_resources"]) {
			row, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if id := query.StringVal(row, "id"); id != "" {
				handles = append(handles, side+":"+id)
			}
		}
	}
	return handles
}

// missingEvidenceFromData extracts the explicit missing/unsupported evidence
// markers an answer surfaces. Compare answers mark a side "unsupported" or
// "inferred" with a reason; the parity layer compares those markers across
// surfaces so neither surface silently fabricates confidence.
func missingEvidenceFromData(data map[string]any) []string {
	if data == nil {
		return nil
	}
	missing := []string{}
	for _, side := range []string{"left", "right"} {
		snap, ok := data[side].(map[string]any)
		if !ok {
			continue
		}
		status := query.StringVal(snap, "status")
		if status == "unsupported" || status == "inferred" {
			missing = append(missing, side+":"+status+":"+query.StringVal(snap, "reason"))
		}
	}
	return missing
}

func asSlice(v any) []any {
	switch typed := v.(type) {
	case []any:
		return typed
	default:
		return nil
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalJSON(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}
