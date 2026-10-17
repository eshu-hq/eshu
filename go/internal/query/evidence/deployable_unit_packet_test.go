// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package evidence

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/auth"
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// stubDeployableUnitPacketResponder records the deployable-unit packet seam
// calls so the handler tests prove the leaf passes its capability/filtered
// read results through, mirroring supply/chain's stubImpactPacketResponder
// precedent. It composes nothing: packet bytes stay root's job, proved by
// the unchanged root packet tests.
type stubDeployableUnitPacketResponder struct {
	composed []AdmissionDecisionResult
	subject  map[string]string
	truth    *querycontract.TruthEnvelope
	refusals int
}

func (s *stubDeployableUnitPacketResponder) RespondDeployableUnitPacket(
	_ http.ResponseWriter,
	_ *http.Request,
	decisions []AdmissionDecisionResult,
	subject map[string]string,
	truth *querycontract.TruthEnvelope,
) {
	s.composed = append([]AdmissionDecisionResult(nil), decisions...)
	s.subject = subject
	s.truth = truth
}

func (s *stubDeployableUnitPacketResponder) RespondDeployableUnitScopeRefusal(
	w http.ResponseWriter,
	_ *http.Request,
) {
	s.refusals++
	w.WriteHeader(http.StatusOK)
}

// recordingDeployableUnitDecisionStore answers the admission decision read
// with fixed rows and records the filter the handler derived.
type recordingDeployableUnitDecisionStore struct {
	rows       []AdmissionDecisionReadRow
	lastFilter AdmissionDecisionReadFilter
	calls      int
}

func (s *recordingDeployableUnitDecisionStore) ListAdmissionDecisions(
	_ context.Context,
	filter AdmissionDecisionReadFilter,
) ([]AdmissionDecisionReadRow, error) {
	s.calls++
	s.lastFilter = filter
	return s.rows, nil
}

func (s *recordingDeployableUnitDecisionStore) ListAdmissionDecisionEvidence(
	context.Context, string, int,
) ([]AdmissionDecisionEvidenceRow, error) {
	return nil, nil
}

func deployableUnitSeamRow(decisionID, state string) AdmissionDecisionReadRow {
	return AdmissionDecisionReadRow{
		DecisionID:   decisionID,
		Domain:       "deployable_unit_correlation",
		State:        state,
		ScopeID:      "scope-1",
		GenerationID: "generation-1",
	}
}

// TestGetDeployableUnitPacketComposesThroughResponder proves the positive and
// ambiguous cases reach the seam: admitted and ambiguous rows pass through
// with the request subject and a truth envelope, and the store saw the
// deployable-unit domain filter.
func TestGetDeployableUnitPacketComposesThroughResponder(t *testing.T) {
	t.Parallel()

	store := &recordingDeployableUnitDecisionStore{rows: []AdmissionDecisionReadRow{
		deployableUnitSeamRow("decision-1", "admitted"),
		deployableUnitSeamRow("decision-2", "ambiguous"),
	}}
	stub := &stubDeployableUnitPacketResponder{}
	handler := &Handler{AdmissionDecisions: store, PacketResponder: stub}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/deployable-unit/packet?scope_id=scope-1&generation_id=generation-1&repository_id=repo://team/api", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := store.lastFilter.Domain, "deployable_unit_correlation"; got != want {
		t.Fatalf("filter.Domain = %q, want %q", got, want)
	}
	if len(stub.composed) != 2 {
		t.Fatalf("composed decisions = %d, want 2 (admitted + ambiguous pass through)", len(stub.composed))
	}
	if got, want := stub.composed[0].DecisionID, "decision-1"; got != want {
		t.Fatalf("composed[0].DecisionID = %q, want %q", got, want)
	}
	if got, want := stub.composed[1].State, "ambiguous"; got != want {
		t.Fatalf("composed[1].State = %q, want %q", got, want)
	}
	if stub.subject["scope_id"] != "scope-1" || stub.subject["generation_id"] != "generation-1" ||
		stub.subject["repository_id"] != "repo://team/api" {
		t.Fatalf("subject = %#v, want scope/generation/repository ids", stub.subject)
	}
	if stub.truth == nil {
		t.Fatal("truth = nil, want the admission decision truth envelope")
	}
	if stub.refusals != 0 {
		t.Fatalf("refusals = %d, want 0", stub.refusals)
	}
}

// TestGetDeployableUnitPacketScopedOutOfGrantWritesRefusal proves the
// negative case: an out-of-grant scoped caller reaches the refusal seam
// without a store read.
func TestGetDeployableUnitPacketScopedOutOfGrantWritesRefusal(t *testing.T) {
	t.Parallel()

	store := &recordingDeployableUnitDecisionStore{rows: []AdmissionDecisionReadRow{
		deployableUnitSeamRow("decision-1", "admitted"),
	}}
	stub := &stubDeployableUnitPacketResponder{}
	handler := &Handler{AdmissionDecisions: store, PacketResponder: stub}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/deployable-unit/packet?scope_id=scope-other&generation_id=generation-1", nil)
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:            auth.AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: []string{"scope-1"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if store.calls != 0 {
		t.Fatal("admission decision store was read for an out-of-grant scope")
	}
	if got, want := stub.refusals, 1; got != want {
		t.Fatalf("refusals = %d, want %d", got, want)
	}
	if len(stub.composed) != 0 {
		t.Fatalf("composed decisions = %d, want 0", len(stub.composed))
	}
}

// TestGetDeployableUnitPacketNilResponderReturns503 proves an unwired seam is
// a wiring bug, not an empty read: the compose path answers 503 with the
// backend-unavailable contract code.
func TestGetDeployableUnitPacketNilResponderReturns503(t *testing.T) {
	t.Parallel()

	store := &recordingDeployableUnitDecisionStore{rows: []AdmissionDecisionReadRow{
		deployableUnitSeamRow("decision-1", "admitted"),
	}}
	handler := &Handler{AdmissionDecisions: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/deployable-unit/packet?scope_id=scope-1&generation_id=generation-1", nil)
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertDeployableUnitSeamUnavailable(t, rec)
}

// TestGetDeployableUnitPacketNilResponderRefusalReturns503 proves the refusal
// path also answers 503 when the seam is unwired, instead of composing a
// refusal packet it cannot build.
func TestGetDeployableUnitPacketNilResponderRefusalReturns503(t *testing.T) {
	t.Parallel()

	store := &recordingDeployableUnitDecisionStore{rows: []AdmissionDecisionReadRow{
		deployableUnitSeamRow("decision-1", "admitted"),
	}}
	handler := &Handler{AdmissionDecisions: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/deployable-unit/packet?scope_id=scope-other&generation_id=generation-1", nil)
	req = req.WithContext(auth.ContextWithAuthContext(req.Context(), auth.AuthContext{
		Mode:            auth.AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: []string{"scope-1"},
	}))
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	assertDeployableUnitSeamUnavailable(t, rec)
}

func assertDeployableUnitSeamUnavailable(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"backend_unavailable"`) {
		t.Fatalf("body = %s, want backend_unavailable envelope", rec.Body.String())
	}
}
