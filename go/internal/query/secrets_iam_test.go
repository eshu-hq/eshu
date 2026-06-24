// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingSecretsIAMIdentityTrustChainStore struct {
	rows       []SecretsIAMIdentityTrustChainRow
	lastFilter SecretsIAMIdentityTrustChainFilter
}

func (s *recordingSecretsIAMIdentityTrustChainStore) ListSecretsIAMIdentityTrustChains(
	_ context.Context,
	filter SecretsIAMIdentityTrustChainFilter,
) ([]SecretsIAMIdentityTrustChainRow, error) {
	s.lastFilter = filter
	return append([]SecretsIAMIdentityTrustChainRow(nil), s.rows...), nil
}

func TestSecretsIAMListIdentityTrustChainsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SecretsIAMHandler{IdentityTrustChains: &recordingSecretsIAMIdentityTrustChainStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/secrets-iam/identity-trust-chains?limit=10",             // no anchor
		"/api/v0/secrets-iam/identity-trust-chains?workload_object_id=w", // no limit
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestSecretsIAMListIdentityTrustChainsUsesBoundedStore(t *testing.T) {
	t.Parallel()

	store := &recordingSecretsIAMIdentityTrustChainStore{
		rows: []SecretsIAMIdentityTrustChainRow{
			{
				ChainID:               "secrets-iam-identity-trust-chain-1",
				State:                 "exact",
				Confidence:            "high",
				ServiceAccountJoinKey: "sha256:sajoin",
				WorkloadObjectID:      "deployment/payments/checkout",
				WorkloadKind:          "Deployment",
				IAMRoleFingerprint:    "sha256:role",
				VaultPolicyJoinKeys:   []string{"sha256:policy"},
				EvidenceFactIDs:       []string{"fact-a", "fact-b"},
			},
			{ChainID: "secrets-iam-identity-trust-chain-2", State: "partial"},
		},
	}
	handler := &SecretsIAMHandler{IdentityTrustChains: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/secrets-iam/identity-trust-chains?workload_object_id=deployment/payments/checkout&state=exact&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.WorkloadObjectID, "deployment/payments/checkout"; got != want {
		t.Fatalf("WorkloadObjectID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.State, "exact"; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d (handler must over-fetch by one to detect truncation)", got, want)
	}

	var resp struct {
		Chains    []SecretsIAMIdentityTrustChainResult `json:"identity_trust_chains"`
		Count     int                                  `json:"count"`
		Limit     int                                  `json:"limit"`
		Truncated bool                                 `json:"truncated"`
		Cursor    map[string]string                    `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Chains), 1; got != want {
		t.Fatalf("len(identity_trust_chains) = %d, want %d", got, want)
	}
	if got, want := resp.Chains[0].State, "exact"; got != want {
		t.Fatalf("State = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.Cursor["after_chain_id"], "secrets-iam-identity-trust-chain-1"; got != want {
		t.Fatalf("next_cursor.after_chain_id = %q, want %q", got, want)
	}
}

func TestSecretsIAMIdentityTrustChainsTruthLabel(t *testing.T) {
	t.Parallel()

	store := &recordingSecretsIAMIdentityTrustChainStore{
		rows: []SecretsIAMIdentityTrustChainRow{{ChainID: "c1", State: "exact"}},
	}
	handler := &SecretsIAMHandler{IdentityTrustChains: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/secrets-iam/identity-trust-chains?scope_id=scope-123&limit=10",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var resp struct {
		Truth *TruthEnvelope `json:"truth"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if resp.Truth == nil {
		t.Fatal("truth envelope is nil, want non-nil")
	}
	if got, want := resp.Truth.Capability, secretsIAMIdentityTrustChainsCapability; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
}

func TestSecretsIAMIdentityTrustChainsUnsupportedWhenBackendUnavailable(t *testing.T) {
	t.Parallel()

	handler := &SecretsIAMHandler{IdentityTrustChains: nil, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/secrets-iam/identity-trust-chains?scope_id=scope-123&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSecretsIAMIdentityTrustChainQueryUsesActiveFactReadModel(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.fact_kind = $1",
		"fact.is_tombstone = FALSE",
		"generation.status = 'active'",
		"fact.payload->>'workload_object_id' = $4",
		"fact.payload->>'service_account_join_key' = $5",
		"fact.payload->>'iam_role_fingerprint' = $6",
		"fact.payload->>'state' = $7",
		"fact.fact_id > $8",
		"ORDER BY fact.fact_id ASC",
	} {
		if !strings.Contains(listSecretsIAMIdentityTrustChainsQuery, want) {
			t.Fatalf("listSecretsIAMIdentityTrustChainsQuery missing %q:\n%s", want, listSecretsIAMIdentityTrustChainsQuery)
		}
	}
}

// failingSecretsIAMTrustChainQueryer fails the test if any query reaches the
// database. It proves scope/anchor validation rejects an unbounded read before
// a SQL statement is ever issued.
type failingSecretsIAMTrustChainQueryer struct {
	t *testing.T
}

func (q failingSecretsIAMTrustChainQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (*sql.Rows, error) {
	q.t.Helper()
	q.t.Fatal("QueryContext called: unbounded scope reached the database instead of being rejected")
	return nil, nil
}

func TestSecretsIAMIdentityTrustChainFilterRejectsUnboundedScope(t *testing.T) {
	t.Parallel()

	store := PostgresSecretsIAMIdentityTrustChainStore{DB: failingSecretsIAMTrustChainQueryer{t: t}}
	_, err := store.ListSecretsIAMIdentityTrustChains(context.Background(), SecretsIAMIdentityTrustChainFilter{Limit: 10})
	if err == nil {
		t.Fatal("ListSecretsIAMIdentityTrustChains() error = nil, want non-nil for unbounded scope")
	}
	if want := "is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}
}

func TestSecretsIAMIdentityTrustChainFilterRejectsNilDB(t *testing.T) {
	t.Parallel()

	store := PostgresSecretsIAMIdentityTrustChainStore{DB: nil}
	_, err := store.ListSecretsIAMIdentityTrustChains(context.Background(), SecretsIAMIdentityTrustChainFilter{
		WorkloadObjectID: "deployment/payments/checkout",
		Limit:            10,
	})
	if err == nil {
		t.Fatal("ListSecretsIAMIdentityTrustChains() error = nil, want non-nil for nil DB")
	}
	if want := "database is required"; !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}
}
