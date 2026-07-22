// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// TestTerraformConfigStateDriftFilterToPostgresThreadsScopeGrantFields is the
// #5442 P2 regression: the query-layer-to-postgres filter mapping must carry
// Scoped/AllowedScopeIDs so the SQL layer also enforces the caller's grant
// (defense-in-depth), not only ScopeID/Address/Outcome/DriftKinds/Limit/
// Offset.
func TestTerraformConfigStateDriftFilterToPostgresThreadsScopeGrantFields(t *testing.T) {
	t.Parallel()

	filter := TerraformConfigStateDriftFindingFilter{
		ScopeID:         "state_snapshot:s3:hash-1",
		Address:         "aws_s3_bucket.x",
		Outcome:         "ambiguous",
		DriftKinds:      []string{"added_in_state"},
		Limit:           25,
		Offset:          5,
		Scoped:          true,
		AllowedScopeIDs: []string{"repo-a", "state_snapshot:s3:hash-1"},
	}
	got := terraformConfigStateDriftFilterToPostgres(filter)
	want := postgres.TerraformConfigStateDriftFindingFilter{
		ScopeID:         "state_snapshot:s3:hash-1",
		Address:         "aws_s3_bucket.x",
		Outcome:         "ambiguous",
		DriftKinds:      []string{"added_in_state"},
		Limit:           25,
		Offset:          5,
		Scoped:          true,
		AllowedScopeIDs: []string{"repo-a", "state_snapshot:s3:hash-1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("terraformConfigStateDriftFilterToPostgres() = %#v, want %#v", got, want)
	}
}

// TestBindTerraformConfigStateDriftFilterAccessSetsScopeGrant proves the
// handler-side binder (the #5442 P2 companion to the P1 access precheck)
// populates Scoped/AllowedScopeIDs from the caller's merged repository/scope
// grant for a scoped caller, and leaves the filter unscoped for an
// all-scopes caller.
func TestBindTerraformConfigStateDriftFilterAccessSetsScopeGrant(t *testing.T) {
	t.Parallel()

	scopedAccess := repositoryAccessFilterFromContext(ContextWithAuthContext(context.Background(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"state_snapshot:s3:hash-1", "repo-a"},
	}))
	filter := bindTerraformConfigStateDriftFilterAccess(scopedAccess, TerraformConfigStateDriftFindingFilter{ScopeID: "state_snapshot:s3:hash-1"})
	if !filter.Scoped {
		t.Fatal("filter.Scoped = false, want true for a scoped caller")
	}
	want := []string{"repo-a", "state_snapshot:s3:hash-1"}
	if !reflect.DeepEqual(filter.AllowedScopeIDs, want) {
		t.Fatalf("filter.AllowedScopeIDs = %#v, want %#v", filter.AllowedScopeIDs, want)
	}

	unscopedAccess := repositoryAccessFilterFromContext(context.Background())
	unscopedFilter := bindTerraformConfigStateDriftFilterAccess(unscopedAccess, TerraformConfigStateDriftFindingFilter{ScopeID: "state_snapshot:s3:hash-1"})
	if unscopedFilter.Scoped {
		t.Fatal("unscopedFilter.Scoped = true, want false for an all-scopes caller")
	}
	if unscopedFilter.AllowedScopeIDs != nil {
		t.Fatalf("unscopedFilter.AllowedScopeIDs = %#v, want nil for an all-scopes caller", unscopedFilter.AllowedScopeIDs)
	}
}

// terraformConfigStateDriftExactFixtureRowWithEvidence returns an "exact"
// finding whose Evidence[] mirrors terraformConfigStateDriftRowFromPostgres's
// real shape for a candidate built by tfconfigstate.buildOneCandidate: an
// address atom and a state atom scoped to the finding's own granted
// state_snapshot scope, plus a config atom scoped to
// resolver.CommitAnchor.ScopeID -- the CONFIG repo's own repo-snapshot scope,
// a different identifier the caller was never granted (#5442 P3).
func terraformConfigStateDriftExactFixtureRowWithEvidence() TerraformConfigStateDriftFindingRow {
	return TerraformConfigStateDriftFindingRow{
		FactID: "fact:tf-exact-evidence-1", ScopeID: "state_snapshot:s3:hash-1", GenerationID: "generation:tf-exact-evidence-1",
		SourceSystem: "reducer/terraform_config_state_drift", CanonicalID: "canonical:tf-exact-evidence-1",
		CandidateID: "drift:hash-1:aws_s3_bucket.tenant_a:added_in_state", CandidateKind: "terraform_config_state_drift",
		Outcome: "exact", Address: "aws_s3_bucket.tenant_a", DriftKind: "added_in_state",
		BackendKind: "s3", LocatorHash: "hash-1", Confidence: 1,
		Evidence: []map[string]any{
			{
				"id":            "drift:hash-1:aws_s3_bucket.tenant_a:added_in_state/address",
				"source_system": "reducer/terraform_config_state_drift", "evidence_type": "terraform_drift_address",
				"scope_id": "state_snapshot:s3:hash-1", "key": "resource_address",
				"value": "aws_s3_bucket.tenant_a", "confidence": 1.0,
			},
			{
				"id":            "drift:hash-1:aws_s3_bucket.tenant_a:added_in_state/config",
				"source_system": "reducer/terraform_config_state_drift", "evidence_type": "terraform_config_resource",
				"scope_id": "repo:config-repo@deadbeef", "key": "resource_address",
				"value": "aws_s3_bucket.tenant_a", "confidence": 1.0,
			},
			{
				"id":            "drift:hash-1:aws_s3_bucket.tenant_a:added_in_state/state",
				"source_system": "reducer/terraform_config_state_drift", "evidence_type": "terraform_state_resource",
				"scope_id": "state_snapshot:s3:hash-1", "key": "resource_address",
				"value": "aws_s3_bucket.tenant_a", "confidence": 1.0,
			},
		},
	}
}

// TestHandleTerraformConfigStateDriftFindingsRedactsEvidenceScopeIDOutsideGrant
// is the #5442 P3 regression: a scoped caller granted only the finding's own
// state_snapshot scope must not receive the config atom's ungranted config-repo
// scope_id ("repo:config-repo@deadbeef", anchor.ScopeID) inside evidence[] --
// and the finding must still report its real, legitimate data (address, drift
// kind, and the address/state atoms whose scope_id IS the granted scope).
func TestHandleTerraformConfigStateDriftFindingsRedactsEvidenceScopeIDOutsideGrant(t *testing.T) {
	t.Parallel()

	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			rows: []TerraformConfigStateDriftFindingRow{terraformConfigStateDriftExactFixtureRowWithEvidence()},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/terraform/config-state-drift/findings", bytes.NewBufferString(`{
		"scope_id": "state_snapshot:s3:hash-1",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req = req.WithContext(ContextWithAuthContext(req.Context(), AuthContext{
		Mode:                 AuthModeScoped,
		AllowedRepositoryIDs: []string{"state_snapshot:s3:hash-1"},
	}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	body := w.Body.String()
	if strings.Contains(body, "repo:config-repo@deadbeef") || strings.Contains(body, "deadbeef") {
		t.Fatalf("response leaked out-of-grant config-repo scope_id: %s", body)
	}

	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("payload[data] = %#v, want map", payload["data"])
	}
	findings, ok := data["drift_findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("data[drift_findings] = %#v, want one finding", data["drift_findings"])
	}
	first, ok := findings[0].(map[string]any)
	if !ok {
		t.Fatalf("findings[0] = %#v, want map", findings[0])
	}
	if got, want := first["outcome"], "exact"; got != want {
		t.Fatalf("first[outcome] = %#v, want %#v -- evidence redaction must not change the finding's own outcome", got, want)
	}
	if got, want := first["address"], "aws_s3_bucket.tenant_a"; got != want {
		t.Fatalf("first[address] = %#v, want %#v -- the finding's legitimate own data must survive", got, want)
	}
	evidence, ok := first["evidence"].([]any)
	if !ok || len(evidence) != 3 {
		t.Fatalf("first[evidence] = %#v, want all 3 atoms preserved (redacted, not dropped)", first["evidence"])
	}
	byType := map[string]map[string]any{}
	for _, raw := range evidence {
		atom, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("evidence entry = %#v, want map", raw)
		}
		byType[StringVal(atom, "evidence_type")] = atom
	}
	configAtom, ok := byType["terraform_config_resource"]
	if !ok {
		t.Fatalf("evidence missing terraform_config_resource atom: %#v", evidence)
	}
	if _, hasScopeID := configAtom["scope_id"]; hasScopeID {
		t.Fatalf("config atom = %#v, want scope_id removed (redacted), not merely blanked or present", configAtom)
	}
	if got, want := configAtom["value"], "aws_s3_bucket.tenant_a"; got != want {
		t.Fatalf("config atom[value] = %#v, want %#v -- only the ungranted scope_id identifier should be redacted", got, want)
	}
	addressAtom, ok := byType["terraform_drift_address"]
	if !ok {
		t.Fatalf("evidence missing terraform_drift_address atom: %#v", evidence)
	}
	if got, want := addressAtom["scope_id"], "state_snapshot:s3:hash-1"; got != want {
		t.Fatalf("address atom[scope_id] = %#v, want %#v -- the granted state scope must stay visible", got, want)
	}
	stateAtom, ok := byType["terraform_state_resource"]
	if !ok {
		t.Fatalf("evidence missing terraform_state_resource atom: %#v", evidence)
	}
	if got, want := stateAtom["scope_id"], "state_snapshot:s3:hash-1"; got != want {
		t.Fatalf("state atom[scope_id] = %#v, want %#v -- the granted state scope must stay visible", got, want)
	}
}

// TestHandleTerraformConfigStateDriftFindingsUnscopedCallerSeesFullEvidenceScopeIDs
// is the paired positive case: an unscoped (admin/local/shared-key) caller is
// unaffected by the #5442 P3 evidence redaction and keeps seeing every
// evidence atom's real scope_id, including the config repo's.
func TestHandleTerraformConfigStateDriftFindingsUnscopedCallerSeesFullEvidenceScopeIDs(t *testing.T) {
	t.Parallel()

	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			rows: []TerraformConfigStateDriftFindingRow{terraformConfigStateDriftExactFixtureRowWithEvidence()},
		},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/terraform/config-state-drift/findings", bytes.NewBufferString(`{
		"scope_id": "state_snapshot:s3:hash-1",
		"limit": 10
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "repo:config-repo@deadbeef") {
		t.Fatalf("unscoped caller must still see the config atom's real scope_id: %s", w.Body.String())
	}
}
