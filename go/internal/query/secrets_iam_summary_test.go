// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingPostureSummaryStore struct {
	summary     SecretsIAMPostureSummary
	lastScopeID string
}

func (s *recordingPostureSummaryStore) SummarizeSecretsIAMPosture(
	_ context.Context, scopeID string,
) (SecretsIAMPostureSummary, error) {
	s.lastScopeID = scopeID
	return s.summary, nil
}

func TestSecretsIAMPostureSummaryRequiresScope(t *testing.T) {
	t.Parallel()

	handler := &SecretsIAMHandler{Summary: &recordingPostureSummaryStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestSecretsIAMPostureSummaryReturnsGroupedCounts(t *testing.T) {
	t.Parallel()

	store := &recordingPostureSummaryStore{
		summary: SecretsIAMPostureSummary{
			IdentityTrustChainsByState: []SecretsIAMBucketCount{{Bucket: "exact", Count: 3}, {Bucket: "partial", Count: 1}},
			PostureGapsByGapType:       []SecretsIAMBucketCount{{Bucket: "missing_evidence", Count: 2}},
		},
	}
	handler := &SecretsIAMHandler{Summary: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary?scope_id=scope-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastScopeID != "scope-1" {
		t.Fatalf("lastScopeID = %q, want scope-1", store.lastScopeID)
	}
	var resp struct {
		ScopeID string                   `json:"scope_id"`
		Summary SecretsIAMPostureSummary `json:"summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.ScopeID != "scope-1" || len(resp.Summary.IdentityTrustChainsByState) != 2 || resp.Summary.IdentityTrustChainsByState[0].Count != 3 {
		t.Fatalf("unexpected summary body: %+v", resp)
	}
}

func TestSecretsIAMPostureSummaryUnsupportedWhenBackendUnavailable(t *testing.T) {
	t.Parallel()

	handler := &SecretsIAMHandler{Summary: nil, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary?scope_id=s", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}
}

func TestSecretsIAMPostureSummaryStoreRejectsNilDBAndScope(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresSecretsIAMPostureSummaryStore{}).SummarizeSecretsIAMPosture(context.Background(), "s"); err == nil ||
		!strings.Contains(err.Error(), "database is required") {
		t.Fatalf("nil-DB error = %v", err)
	}
	if _, err := (PostgresSecretsIAMPostureSummaryStore{DB: failingSecretsIAMTrustChainQueryer{t: t}}).SummarizeSecretsIAMPosture(context.Background(), ""); err == nil ||
		!strings.Contains(err.Error(), "scope_id is required") {
		t.Fatalf("empty-scope error = %v", err)
	}
}

func TestSecretsIAMPostureSummaryRejectsOffAllowlistBucketField(t *testing.T) {
	t.Parallel()

	// The bucket field is interpolated into the SQL, so the allow-list guard is
	// the defense that keeps it injection-safe. Lock that an off-list field is
	// rejected before any query is issued (the failing queryer would fatal the
	// test if reached).
	store := PostgresSecretsIAMPostureSummaryStore{DB: failingSecretsIAMTrustChainQueryer{t: t}}
	_, err := store.bucketCounts(context.Background(), secretsIAMPostureGapFactKind, "evil; DROP TABLE fact_records", "scope-1")
	if err == nil || !strings.Contains(err.Error(), "unsupported summary bucket field") {
		t.Fatalf("bucketCounts off-allowlist error = %v, want unsupported summary bucket field", err)
	}
}

type recordingGrantPostureStore struct {
	posture     SecretsIAMGrantPosture
	err         error
	lastScopeID string
}

func (s *recordingGrantPostureStore) SummarizeS3ExternalPrincipalGrantPosture(
	_ context.Context, scopeID string,
) (SecretsIAMGrantPosture, error) {
	s.lastScopeID = scopeID
	if s.err != nil {
		return SecretsIAMGrantPosture{}, s.err
	}
	return s.posture, nil
}

func TestSecretsIAMPostureSummaryIncludesGrantPosture(t *testing.T) {
	t.Parallel()

	grants := &recordingGrantPostureStore{
		posture: SecretsIAMGrantPosture{
			TotalGrants:            4,
			GrantsByOutcome:        []SecretsIAMBucketCount{{Bucket: "allowed", Count: 3}, {Bucket: "unknown", Count: 1}},
			GrantsByResolutionMode: []SecretsIAMBucketCount{{Bucket: "exact_arn", Count: 2}, {Bucket: "unknown", Count: 2}},
			PublicGrants:           1,
			CrossAccountGrants:     2,
			ServicePrincipalGrants: 1,
		},
	}
	handler := &SecretsIAMHandler{
		Summary:      &recordingPostureSummaryStore{},
		GrantPosture: grants,
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary?scope_id=scope-1", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if grants.lastScopeID != "scope-1" {
		t.Fatalf("grant posture lastScopeID = %q, want scope-1", grants.lastScopeID)
	}
	var resp struct {
		Data struct {
			Summary SecretsIAMPostureSummary `json:"summary"`
		} `json:"data"`
		Truth struct {
			Basis string `json:"basis"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	got := resp.Data.Summary.S3ExternalPrincipalGrantPosture
	if got == nil {
		t.Fatalf("summary missing s3_external_principal_grant_posture: %s", w.Body.String())
	}
	if got.TotalGrants != 4 || got.PublicGrants != 1 || got.CrossAccountGrants != 2 ||
		got.ServicePrincipalGrants != 1 || len(got.GrantsByOutcome) != 2 ||
		got.GrantsByOutcome[0].Bucket != "allowed" || got.GrantsByOutcome[0].Count != 3 {
		t.Fatalf("unexpected grant posture: %+v", got)
	}
	// When the graph-backed grant section is present, the response blends
	// reducer semantic facts with canonical GRANTS_ACCESS_TO graph aggregates,
	// so the truth basis is hybrid, not pure semantic_facts.
	if got, want := resp.Truth.Basis, string(TruthBasisHybrid); got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
}

func TestSecretsIAMPostureSummaryOmitsGrantPostureWhenUnwired(t *testing.T) {
	t.Parallel()

	handler := &SecretsIAMHandler{Summary: &recordingPostureSummaryStore{}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary?scope_id=scope-1", nil)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "s3_external_principal_grant_posture") {
		t.Fatalf("unwired grant posture must be omitted, got body: %s", w.Body.String())
	}
	// With no graph section, the response is pure reducer semantic facts.
	var resp struct {
		Truth struct {
			Basis string `json:"basis"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Truth.Basis, string(TruthBasisSemanticFacts); got != want {
		t.Fatalf("truth.basis = %q, want %q", got, want)
	}
}

func TestSecretsIAMPostureSummaryGrantPostureErrorFailsRequest(t *testing.T) {
	t.Parallel()

	handler := &SecretsIAMHandler{
		Summary:      &recordingPostureSummaryStore{},
		GrantPosture: &recordingGrantPostureStore{err: fmt.Errorf("graph unavailable")},
		Profile:      ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary?scope_id=scope-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusInternalServerError; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSecretsIAMPostureSummaryQueryGroupsActiveFacts(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"generation.status = 'active'",
		"fact.scope_id = $2",
		"GROUP BY bucket",
		"ORDER BY bucket ASC",
	} {
		if !strings.Contains(secretsIAMPostureSummaryQueryTemplate, want) {
			t.Fatalf("summary query template missing %q", want)
		}
	}
}

// TestSecretsIAMPostureSummaryGraphReadSweep proves the grant-section read maps
// bounded graph-read sentinels onto the 503/504 contract. GrantPosture is wired
// in production to a graph-backed store (NewGraphSecretsIAMGrantPostureStore),
// so a backend timeout or outage during SummarizeS3ExternalPrincipalGrantPosture
// must not collapse into a generic 500. The Postgres Summary read succeeds
// first, so the sentinel can only originate from the graph grant read.
func TestSecretsIAMPostureSummaryGraphReadSweep(t *testing.T) {
	t.Parallel()
	for _, test := range graphReadSweepCases() {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			handler := &SecretsIAMHandler{
				Summary:      &recordingPostureSummaryStore{},
				GrantPosture: &recordingGrantPostureStore{err: test.err},
				Profile:      ProfileProduction,
			}
			mux := http.NewServeMux()
			handler.Mount(mux)

			req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-summary?scope_id=scope-1", nil)
			req.Header.Set("Accept", EnvelopeMIMEType)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			assertGraphReadSweepResponse(t, rec, test)
		})
	}
}
