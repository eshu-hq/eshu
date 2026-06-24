package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingPrivilegePostureStore struct {
	rows       []SecretsIAMPrivilegePostureObservationRow
	lastFilter SecretsIAMPrivilegePostureObservationFilter
}

func (s *recordingPrivilegePostureStore) ListSecretsIAMPrivilegePostureObservations(
	_ context.Context, filter SecretsIAMPrivilegePostureObservationFilter,
) ([]SecretsIAMPrivilegePostureObservationRow, error) {
	s.lastFilter = filter
	return append([]SecretsIAMPrivilegePostureObservationRow(nil), s.rows...), nil
}

type recordingSecretAccessPathStore struct {
	rows       []SecretsIAMSecretAccessPathRow
	lastFilter SecretsIAMSecretAccessPathFilter
}

func (s *recordingSecretAccessPathStore) ListSecretsIAMSecretAccessPaths(
	_ context.Context, filter SecretsIAMSecretAccessPathFilter,
) ([]SecretsIAMSecretAccessPathRow, error) {
	s.lastFilter = filter
	return append([]SecretsIAMSecretAccessPathRow(nil), s.rows...), nil
}

type recordingPostureGapStore struct {
	rows       []SecretsIAMPostureGapRow
	lastFilter SecretsIAMPostureGapFilter
}

func (s *recordingPostureGapStore) ListSecretsIAMPostureGaps(
	_ context.Context, filter SecretsIAMPostureGapFilter,
) ([]SecretsIAMPostureGapRow, error) {
	s.lastFilter = filter
	return append([]SecretsIAMPostureGapRow(nil), s.rows...), nil
}

func TestSecretsIAMPostureEndpointsRequireScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &SecretsIAMHandler{
		PrivilegePostureObservations: &recordingPrivilegePostureStore{},
		SecretAccessPaths:            &recordingSecretAccessPathStore{},
		PostureGaps:                  &recordingPostureGapStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		// missing limit
		"/api/v0/secrets-iam/privilege-posture-observations?scope_id=s",
		"/api/v0/secrets-iam/secret-access-paths?scope_id=s",
		"/api/v0/secrets-iam/posture-gaps?scope_id=s",
		// missing scope anchor
		"/api/v0/secrets-iam/privilege-posture-observations?limit=10",
		"/api/v0/secrets-iam/secret-access-paths?limit=10",
		"/api/v0/secrets-iam/posture-gaps?limit=10",
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

func TestSecretsIAMPrivilegePostureObservationsListsWithCursor(t *testing.T) {
	t.Parallel()

	store := &recordingPrivilegePostureStore{
		rows: []SecretsIAMPrivilegePostureObservationRow{
			{ObservationID: "obs-1", RiskType: "external_trust_without_external_id", Severity: "high", State: "partial"},
			{ObservationID: "obs-2", RiskType: "wildcard_action", State: "partial"},
		},
	}
	handler := &SecretsIAMHandler{PrivilegePostureObservations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/secrets-iam/privilege-posture-observations?scope_id=s&risk_type=external_trust_without_external_id&severity=high&limit=1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastFilter.RiskType != "external_trust_without_external_id" || store.lastFilter.Severity != "high" {
		t.Fatalf("filter risk/severity not threaded: %+v", store.lastFilter)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d (over-fetch by one)", got, want)
	}
	var resp struct {
		Rows      []SecretsIAMPrivilegePostureObservationResult `json:"privilege_posture_observations"`
		Truncated bool                                          `json:"truncated"`
		Cursor    map[string]string                             `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(resp.Rows) != 1 || !resp.Truncated || resp.Cursor["after_observation_id"] != "obs-1" {
		t.Fatalf("unexpected page: rows=%d truncated=%v cursor=%v", len(resp.Rows), resp.Truncated, resp.Cursor)
	}
}

func TestSecretsIAMSecretAccessPathsThreadsChainAnchor(t *testing.T) {
	t.Parallel()

	store := &recordingSecretAccessPathStore{
		rows: []SecretsIAMSecretAccessPathRow{
			{PathID: "p-1", ChainID: "c-1", State: "exact", VaultPolicyJoinKey: "sha256:pol", Capabilities: []string{"read"}},
		},
	}
	handler := &SecretsIAMHandler{SecretAccessPaths: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/secrets-iam/secret-access-paths?chain_id=c-1&state=exact&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if store.lastFilter.ChainID != "c-1" || store.lastFilter.State != "exact" {
		t.Fatalf("filter not threaded: %+v", store.lastFilter)
	}
}

func TestSecretsIAMPostureGapsUnsupportedWhenBackendUnavailable(t *testing.T) {
	t.Parallel()

	handler := &SecretsIAMHandler{PostureGaps: nil, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet,
		"/api/v0/secrets-iam/posture-gaps?scope_id=s&limit=10", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestSecretsIAMPostureGapsTruthLabel(t *testing.T) {
	t.Parallel()

	store := &recordingPostureGapStore{rows: []SecretsIAMPostureGapRow{{GapID: "g-1", GapType: "missing_evidence", State: "unresolved"}}}
	handler := &SecretsIAMHandler{PostureGaps: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/secrets-iam/posture-gaps?scope_id=s&limit=10", nil)
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
	if resp.Truth == nil || resp.Truth.Capability != secretsIAMPostureGapsCapability {
		t.Fatalf("unexpected truth envelope: %+v", resp.Truth)
	}
}

func TestSecretsIAMPostureQueriesUseActiveFactReadModel(t *testing.T) {
	t.Parallel()

	checks := map[string][]string{
		listSecretsIAMPrivilegePostureObservationsQuery: {
			"fact.fact_kind = $1", "generation.status = 'active'",
			"fact.payload->>'risk_type' = $4", "fact.payload->>'severity' = $5", "fact.fact_id > $7",
		},
		listSecretsIAMSecretAccessPathsQuery: {
			"fact.payload->>'chain_id' = $4", "fact.payload->>'vault_mount_join_key' = $5", "ORDER BY fact.fact_id ASC",
		},
		listSecretsIAMPostureGapsQuery: {
			"fact.payload->>'gap_type' = $4", "fact.payload->>'service_account_join_key' = $5",
		},
	}
	for q, wants := range checks {
		for _, want := range wants {
			if !strings.Contains(q, want) {
				t.Fatalf("query missing %q:\n%s", want, q)
			}
		}
	}
}

func TestSecretsIAMPostureStoresRejectNilDBAndUnboundedScope(t *testing.T) {
	t.Parallel()

	if _, err := (PostgresSecretsIAMPrivilegePostureObservationStore{}).ListSecretsIAMPrivilegePostureObservations(
		context.Background(), SecretsIAMPrivilegePostureObservationFilter{ScopeID: "s", Limit: 10},
	); err == nil ||
		!strings.Contains(err.Error(), "database is required") {
		t.Fatalf("privilege posture nil-DB error = %v", err)
	}
	if _, err := (PostgresSecretsIAMSecretAccessPathStore{DB: failingSecretsIAMTrustChainQueryer{t: t}}).ListSecretsIAMSecretAccessPaths(
		context.Background(), SecretsIAMSecretAccessPathFilter{Limit: 10},
	); err == nil ||
		!strings.Contains(err.Error(), "is required") {
		t.Fatalf("secret access path unbounded-scope error = %v", err)
	}
	if _, err := (PostgresSecretsIAMPostureGapStore{DB: failingSecretsIAMTrustChainQueryer{t: t}}).ListSecretsIAMPostureGaps(
		context.Background(), SecretsIAMPostureGapFilter{Limit: 10},
	); err == nil ||
		!strings.Contains(err.Error(), "is required") {
		t.Fatalf("posture gap unbounded-scope error = %v", err)
	}
}
