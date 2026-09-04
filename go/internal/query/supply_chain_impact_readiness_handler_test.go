// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"
)

type recordingSupplyChainImpactReadinessStore struct {
	snapshot  impact.SupplyChainImpactReadinessSnapshot
	err       error
	lastQuery impact.SupplyChainImpactReadinessQuery
	calls     int
}

func (s *recordingSupplyChainImpactReadinessStore) ReadSupplyChainImpactReadiness(
	_ context.Context,
	query impact.SupplyChainImpactReadinessQuery,
) (impact.SupplyChainImpactReadinessSnapshot, error) {
	s.lastQuery = query
	s.calls++
	if s.err != nil {
		return impact.SupplyChainImpactReadinessSnapshot{}, s.err
	}
	clone := impact.SupplyChainImpactReadinessSnapshot{
		EvidenceSources:    append([]impact.SupplyChainImpactEvidenceFamily(nil), s.snapshot.EvidenceSources...),
		SourceSnapshots:    append([]impact.SupplyChainImpactSourceSnapshot(nil), s.snapshot.SourceSnapshots...),
		SourceStates:       append([]impact.SupplyChainImpactSourceState(nil), s.snapshot.SourceStates...),
		UnsupportedTargets: append([]impact.SupplyChainImpactUnsupportedTarget(nil), s.snapshot.UnsupportedTargets...),
		TargetIncomplete:   s.snapshot.TargetIncomplete,
		IncompleteReasons:  append([]string(nil), s.snapshot.IncompleteReasons...),
	}
	return clone, nil
}

func TestSupplyChainListImpactFindingsAttachesReadinessForZeroFindings(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: impact.SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []impact.SupplyChainImpactEvidenceFamily{
				{Family: impact.EvidenceFamilyVulnerabilityAdvisory, FactCount: 5, Freshness: impact.FreshnessLabelFresh},
				{Family: impact.EvidenceFamilyPackageConsumption, FactCount: 2, Freshness: impact.FreshnessLabelFresh},
				{Family: impact.EvidenceFamilyPackageRegistry, FactCount: 1, Freshness: impact.FreshnessLabelFresh},
			},
		},
	}
	findings := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{
		ImpactFindings: findings,
		Readiness:      readiness,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if readiness.calls != 1 {
		t.Fatalf("readiness.calls = %d, want 1", readiness.calls)
	}
	if got, want := readiness.lastQuery.RepositoryID, "repo://example/api"; got != want {
		t.Fatalf("readiness.lastQuery.RepositoryID = %q, want %q", got, want)
	}

	var resp struct {
		Findings  []impact.SupplyChainImpactFindingResult   `json:"findings"`
		Count     int                                       `json:"count"`
		Limit     int                                       `json:"limit"`
		Truncated bool                                      `json:"truncated"`
		Readiness impact.SupplyChainImpactReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Readiness.State != impact.ReadinessStateReadyZeroFindings {
		t.Fatalf("readiness.state = %q, want %q", resp.Readiness.State, impact.ReadinessStateReadyZeroFindings)
	}
	if resp.Readiness.TargetScope.RepositoryID != "repo://example/api" {
		t.Fatalf("readiness.target_scope.repository_id = %q, want repo://example/api", resp.Readiness.TargetScope.RepositoryID)
	}
	if resp.Readiness.Freshness != impact.FreshnessLabelFresh {
		t.Fatalf("readiness.freshness = %q, want %q", resp.Readiness.Freshness, impact.FreshnessLabelFresh)
	}
	if resp.Count != 0 || resp.Truncated {
		t.Fatalf("count/truncated = %d/%v, want zero", resp.Count, resp.Truncated)
	}
}

func TestSupplyChainListImpactFindingsReadinessSurfacesNotConfigured(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{}
	findings := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{
		ImpactFindings: findings,
		Readiness:      readiness,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=5",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Readiness impact.SupplyChainImpactReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Readiness.State != impact.ReadinessStateNotConfigured {
		t.Fatalf("readiness.state = %q, want %q", resp.Readiness.State, impact.ReadinessStateNotConfigured)
	}
	if resp.Readiness.TargetScope.CVEID != "CVE-2026-0001" {
		t.Fatalf("readiness.target_scope.cve_id = %q, want CVE-2026-0001", resp.Readiness.TargetScope.CVEID)
	}
	if !impact.ReadinessMissingContains(resp.Readiness.MissingEvidence, impact.MissingEvidenceAdvisorySources) {
		t.Fatalf("missing_evidence = %#v, want advisory_sources", resp.Readiness.MissingEvidence)
	}
}

func TestSupplyChainListImpactFindingsReadinessWithFindings(t *testing.T) {
	t.Parallel()

	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: impact.SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []impact.SupplyChainImpactEvidenceFamily{
				{Family: impact.EvidenceFamilyVulnerabilityAdvisory, FactCount: 4, Freshness: impact.FreshnessLabelFresh},
			},
		},
	}
	findings := &recordingSupplyChainImpactFindingStore{
		rows: []impact.SupplyChainImpactFindingRow{
			{FindingID: "finding-1", CVEID: "CVE-2026-0001", ImpactStatus: "affected_exact"},
			{FindingID: "finding-2", CVEID: "CVE-2026-0001", ImpactStatus: "possibly_affected"},
		},
	}
	handler := &SupplyChainHandler{
		ImpactFindings: findings,
		Readiness:      readiness,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Findings  []impact.SupplyChainImpactFindingResult   `json:"findings"`
		Truncated bool                                      `json:"truncated"`
		Readiness impact.SupplyChainImpactReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Readiness.State != impact.ReadinessStateReadyWithFindings {
		t.Fatalf("readiness.state = %q, want %q", resp.Readiness.State, impact.ReadinessStateReadyWithFindings)
	}
	if !resp.Readiness.Counts.FindingsTruncated {
		t.Fatal("readiness.counts.findings_truncated = false, want true (page limit was 1, store had 2)")
	}
	if got, want := resp.Readiness.Counts.FindingsReturned, len(resp.Findings); got != want {
		t.Fatalf("readiness.counts.findings_returned = %d, want %d", got, want)
	}
}

func TestSupplyChainListImpactFindingsReadinessSurfacesUnsupported(t *testing.T) {
	t.Parallel()

	// End-to-end proof that the API attaches an unsupported readiness state
	// to a zero-finding response when the readiness store reports observed
	// unsupported target evidence. The state and unsupported_targets[]
	// payload come through the JSON envelope without leaking package names.
	readiness := &recordingSupplyChainImpactReadinessStore{
		snapshot: impact.SupplyChainImpactReadinessSnapshot{
			EvidenceSources: []impact.SupplyChainImpactEvidenceFamily{
				{Family: impact.EvidenceFamilyVulnerabilityAdvisory, FactCount: 2, Freshness: impact.FreshnessLabelFresh},
				{Family: impact.EvidenceFamilyPackageConsumption, FactCount: 1, Freshness: impact.FreshnessLabelFresh},
			},
			UnsupportedTargets: []impact.SupplyChainImpactUnsupportedTarget{
				{TargetKind: impact.UnsupportedTargetKindEcosystem, Reason: "unsupported_ecosystem", Ecosystem: "pypi", Count: 2},
			},
		},
	}
	findings := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{
		ImpactFindings: findings,
		Readiness:      readiness,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Readiness impact.SupplyChainImpactReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Readiness.State != impact.ReadinessStateUnsupported {
		t.Fatalf("readiness.state = %q, want %q", resp.Readiness.State, impact.ReadinessStateUnsupported)
	}
	if !impact.ReadinessMissingContains(resp.Readiness.MissingEvidence, impact.MissingEvidenceUnsupportedTargets) {
		t.Fatalf("missing_evidence = %#v, want unsupported_targets", resp.Readiness.MissingEvidence)
	}
	if len(resp.Readiness.UnsupportedTargets) != 1 ||
		resp.Readiness.UnsupportedTargets[0].TargetKind != impact.UnsupportedTargetKindEcosystem ||
		resp.Readiness.UnsupportedTargets[0].Count != 2 {
		t.Fatalf("unsupported_targets = %#v, want one ecosystem entry with count=2", resp.Readiness.UnsupportedTargets)
	}
}

func TestSupplyChainListImpactFindingsReadinessWithoutStore(t *testing.T) {
	t.Parallel()

	findings := &recordingSupplyChainImpactFindingStore{}
	handler := &SupplyChainHandler{ImpactFindings: findings}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-0001&limit=5",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Readiness *impact.SupplyChainImpactReadinessEnvelope `json:"readiness"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Readiness == nil {
		t.Fatal("readiness = nil, want envelope")
	}
	if resp.Readiness.State != impact.ReadinessStateNotConfigured {
		t.Fatalf("readiness.state = %q, want %q (no store => no source evidence)", resp.Readiness.State, impact.ReadinessStateNotConfigured)
	}
}
