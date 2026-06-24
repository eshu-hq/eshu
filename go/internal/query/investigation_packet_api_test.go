// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInvestigationPacketAPISupplyChainMatchesSharedBuilder(t *testing.T) {
	t.Parallel()

	row := exactManifestAndImageExplanationRow()
	filter := SupplyChainImpactExplanationFilter{FindingID: row.Finding.FindingID}
	readinessSnapshot := SupplyChainImpactReadinessSnapshot{
		EvidenceSources: []SupplyChainImpactEvidenceFamily{
			{Family: EvidenceFamilyVulnerabilityAdvisory, FactCount: 1, Freshness: FreshnessLabelFresh},
			{Family: EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: FreshnessLabelFresh},
		},
	}
	readiness := BuildSupplyChainImpactReadiness(
		findingReadinessScope(row.Finding, filter),
		[]SupplyChainImpactFindingResult{SupplyChainImpactFindingResult(row.Finding)},
		false,
		readinessSnapshot,
	)
	truth := BuildTruthEnvelope(
		ProfileProduction,
		supplyChainImpactExplanationCapability,
		TruthBasisSemanticFacts,
		"resolved from one reducer-owned impact finding and its bounded evidence fact ids; reachability and deployment anchors are reported only when evidence exists",
	)
	expected, err := BuildSupplyChainImpactPacket(
		BuildSupplyChainImpactExplanation(filter, row, readiness),
		truth,
		nil,
	)
	if err != nil {
		t.Fatalf("BuildSupplyChainImpactPacket() error = %v", err)
	}

	handler := &SupplyChainHandler{
		ImpactExplanations: &recordingSupplyChainImpactExplanationStore{row: row},
		Readiness:          &recordingSupplyChainImpactReadinessStore{snapshot: readinessSnapshot},
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	got := requestPacket(t, mux, http.MethodGet,
		"/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1", nil)
	assertPacketJSONEqual(t, got, expected)
}

func TestInvestigationPacketAPISupplyChainScopedRequiresRepositoryBeforeStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingSupplyChainImpactExplanationStore{}
	handler := &SupplyChainHandler{
		ImpactExplanations: store,
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://team/api"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("impact explanation store was called without a repository-scoped subject")
	}
	var envelope struct {
		Data InvestigationEvidencePacket `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v; body = %s", err, rec.Body.String())
	}
	if got, want := envelope.Data.Refusal, PacketRefusalScopeNotFound; got != want {
		t.Fatalf("refusal = %q, want %q", got, want)
	}
}

func TestInvestigationPacketAPISupplyChainScopedRejectsOutOfGrantRepositoryBeforeStoreRead(t *testing.T) {
	t.Parallel()

	store := &failingSupplyChainImpactExplanationStore{}
	handler := &SupplyChainHandler{
		ImpactExplanations: store,
		Profile:            ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/investigations/supply-chain/impact/packet?finding_id=finding-1&repository_id=repo://team/other", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		TenantID:             "tenant-a",
		WorkspaceID:          "workspace-a",
		AllowedRepositoryIDs: []string{"repo://team/api"},
	}))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusNotFound; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
	if store.called {
		t.Fatal("impact explanation store was called for an out-of-grant repository selector")
	}
}

func TestInvestigationPacketAPIDeployableUnitMatchesSharedBuilder(t *testing.T) {
	t.Parallel()

	decisions := []AdmissionDecisionReadRow{
		admissionDecisionReadRow("decision-1", "admitted"),
		admissionDecisionReadRow("decision-2", "ambiguous"),
	}
	store := &recordingAdmissionDecisionReadStore{rows: decisions}
	truth := BuildTruthEnvelope(
		ProfileProduction,
		admissionDecisionCapability,
		TruthBasisSemanticFacts,
		"resolved from reducer-owned correlation admission decision read model",
	)
	subject := map[string]string{
		"scope_id":      "git-repository-scope:team/api",
		"generation_id": "generation-1",
		"repository_id": "repo://team/api",
	}
	expected, err := BuildDeployableUnitPacket(
		[]AdmissionDecisionResult{
			admissionDecisionResult(decisions[0]),
			admissionDecisionResult(decisions[1]),
		},
		subject,
		truth,
		nil,
	)
	if err != nil {
		t.Fatalf("BuildDeployableUnitPacket() error = %v", err)
	}

	handler := &EvidenceHandler{AdmissionDecisions: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	got := requestPacket(t, mux, http.MethodGet,
		"/api/v0/investigations/deployable-unit/packet?scope_id=git-repository-scope:team/api&generation_id=generation-1&repository_id=repo://team/api", nil)
	assertPacketJSONEqual(t, got, expected)
	if got, want := store.lastFilter.Domain, "deployable_unit_correlation"; got != want {
		t.Fatalf("filter.Domain = %q, want %q", got, want)
	}
}

type failingSupplyChainImpactExplanationStore struct {
	called bool
}

func (s *failingSupplyChainImpactExplanationStore) ExplainSupplyChainImpact(
	context.Context,
	SupplyChainImpactExplanationFilter,
) (SupplyChainImpactExplanationRow, error) {
	s.called = true
	return SupplyChainImpactExplanationRow{}, errors.New("broad supply-chain impact explanation read")
}

func TestInvestigationPacketAPIDriftMatchesSharedBuilder(t *testing.T) {
	t.Parallel()

	rows := multiCloudRuntimeDriftFixtureRows()
	views := cloudRuntimeDriftFindingViews(rows)
	truth := BuildTruthEnvelope(
		ProfileProduction,
		cloudRuntimeDriftReadbackCapability,
		TruthBasisSemanticFacts,
		"resolved from active reducer-materialized provider-neutral runtime drift findings (reducer_multi_cloud_runtime_drift_finding)",
	)
	subject := map[string]string{
		"scope_id":           "aws-account-1",
		"provider":           "aws",
		"cloud_resource_uid": "aws:ec2:us-east-1:instance/i-123",
	}
	expected, err := BuildDriftPacket(views, subject, truth, nil)
	if err != nil {
		t.Fatalf("BuildDriftPacket() error = %v", err)
	}

	handler := &CloudRuntimeDriftHandler{
		Store: fakeMultiCloudRuntimeDriftStore{rows: rows},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	got := requestPacket(t, mux, http.MethodGet,
		"/api/v0/investigations/drift/packet?scope_id=aws-account-1&provider=aws&cloud_resource_uid=aws:ec2:us-east-1:instance/i-123", nil)
	assertPacketJSONEqual(t, got, expected)
}

func requestPacket(t *testing.T, handler http.Handler, method, target string, body []byte) InvestigationEvidencePacket {
	t.Helper()

	req := httptest.NewRequest(method, target, bytes.NewReader(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}

	var envelope struct {
		Data  json.RawMessage `json:"data"`
		Truth *TruthEnvelope  `json:"truth"`
		Error *ErrorEnvelope  `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode envelope: %v; body = %s", err, rec.Body.String())
	}
	if envelope.Error != nil {
		t.Fatalf("envelope error = %#v", envelope.Error)
	}
	if envelope.Truth == nil {
		t.Fatal("envelope truth = nil, want packet truth")
	}
	var packet InvestigationEvidencePacket
	if err := json.Unmarshal(envelope.Data, &packet); err != nil {
		t.Fatalf("decode packet: %v; data = %s", err, string(envelope.Data))
	}
	return packet
}

func assertPacketJSONEqual(t *testing.T, got, want InvestigationEvidencePacket) {
	t.Helper()

	gotJSON, err := RenderInvestigationPacket(got, InvestigationPacketFormatJSON)
	if err != nil {
		t.Fatalf("render got packet: %v", err)
	}
	wantJSON, err := RenderInvestigationPacket(want, InvestigationPacketFormatJSON)
	if err != nil {
		t.Fatalf("render want packet: %v", err)
	}
	if !bytes.Equal(gotJSON, wantJSON) {
		t.Fatalf("packet JSON mismatch\ngot:  %s\nwant: %s", gotJSON, wantJSON)
	}
}
