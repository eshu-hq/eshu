// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// failingMultiCloudRuntimeDriftStore records whether the drift finding store
// was read and always errors, so #5167 W5 scope tests can prove an empty or
// out-of-grant scoped drift-packet request never touches it.
type failingMultiCloudRuntimeDriftStore struct {
	called bool
}

func (s *failingMultiCloudRuntimeDriftStore) ListActiveMultiCloudRuntimeDriftFindings(
	context.Context,
	MultiCloudRuntimeDriftFilter,
) ([]MultiCloudRuntimeDriftFindingRow, error) {
	s.called = true
	return nil, errors.New("broad multi-cloud runtime drift read")
}

func (s *failingMultiCloudRuntimeDriftStore) CountActiveMultiCloudRuntimeDriftFindings(
	context.Context,
	MultiCloudRuntimeDriftFilter,
) (int, error) {
	s.called = true
	return 0, errors.New("broad multi-cloud runtime drift count read")
}

// TestInvestigationPacketAPIDriftScopedEmptyGrantReturnsRefusalWithoutStoreRead
// proves a scoped token with no AllowedScopeIDs grant never reaches the drift
// finding store: drift findings key on a cloud ingestion scope_id, not a
// repository, so an empty scoped grant can never legitimately match one.
func TestInvestigationPacketAPIDriftScopedEmptyGrantReturnsRefusalWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingMultiCloudRuntimeDriftStore{}
	handler := &CloudRuntimeDriftHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/drift/packet?scope_id=aws-account-1", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:        AuthModeScoped,
		TenantID:    "tenant-a",
		WorkspaceID: "workspace-a",
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("drift finding store was called for an empty scoped grant")
	}
	assertDriftPacketRefused(t, rec.Body.Bytes())
}

// TestInvestigationPacketAPIDriftScopedOutOfGrantScopeReturnsRefusalWithoutStoreRead
// proves a scoped token granted a different cloud scope than the one
// requested never reaches the drift finding store, mirroring the sibling
// out-of-grant repository tests for the supply-chain and deployable-unit
// packet routes.
func TestInvestigationPacketAPIDriftScopedOutOfGrantScopeReturnsRefusalWithoutStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingMultiCloudRuntimeDriftStore{}
	handler := &CloudRuntimeDriftHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/drift/packet?scope_id=aws-account-1", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-b",
		WorkspaceID:     "workspace-b",
		AllowedScopeIDs: []string{"aws-account-2"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("drift finding store was called for an out-of-grant scope_id")
	}
	assertDriftPacketRefused(t, rec.Body.Bytes())
	if strings.Contains(rec.Body.String(), "aws-account-1") {
		t.Fatalf("out-of-grant response leaked requested scope_id: %s", rec.Body.String())
	}
}

// TestInvestigationPacketAPIDriftScopedGrantsAcrossTenants is the #5167 W5
// two-tenant proof for GET /api/v0/investigations/drift/packet: tenant A's
// exact AllowedScopeIDs grant for aws-account-1 reaches the drift finding
// store and returns the same packet an unscoped caller would see, while
// tenant B's disjoint grant for aws-account-2 is refused the identical
// request without a store read (proven above). This test proves the granted
// side of that pair.
func TestInvestigationPacketAPIDriftScopedGrantsAcrossTenants(t *testing.T) {
	t.Parallel()

	rows := multiCloudRuntimeDriftFixtureRows()
	store := fakeMultiCloudRuntimeDriftStore{rows: rows}
	handler := &CloudRuntimeDriftHandler{Store: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/drift/packet?scope_id=cloud-scope:gcp:project-synthetic", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:            AuthModeScoped,
		TenantID:        "tenant-a",
		WorkspaceID:     "workspace-a",
		AllowedScopeIDs: []string{"cloud-scope:gcp:project-synthetic"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("tenant-a status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	var envelope struct {
		Data InvestigationEvidencePacket `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v; body = %s", err, rec.Body.String())
	}
	if envelope.Data.Refusal != PacketRefusalNone {
		t.Fatalf("tenant-a refusal = %q, want none", envelope.Data.Refusal)
	}
	if len(envelope.Data.SourceFacts) == 0 {
		t.Fatal("tenant-a packet carries no source facts, want fixture drift findings")
	}
}

func assertDriftPacketRefused(t *testing.T, body []byte) {
	t.Helper()

	var envelope struct {
		Data InvestigationEvidencePacket `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode envelope: %v; body = %s", err, string(body))
	}
	if got, want := envelope.Data.Identity.Family, InvestigationFamilyDrift; got != want {
		t.Fatalf("family = %q, want %q", got, want)
	}
	if got, want := envelope.Data.Refusal, PacketRefusalScopeNotFound; got != want {
		t.Fatalf("refusal = %q, want %q", got, want)
	}
}
