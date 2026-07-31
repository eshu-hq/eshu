// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestHandleTerraformConfigStateDriftFindingsDistinguishesUnresolvedFromNoDrift
// is the positive/negative proof pair for issue #5594's follow-up: a scope
// whose backend ownership could not be resolved at all
// (tfstatebackend.ErrNoConfigRepoOwnsBackend, durably written by
// TerraformConfigStateDriftHandler.writeUnresolvedOwner in the reducer) must
// be reported differently from a scope that resolved cleanly and simply has
// no drift. Before this fix both cases returned an identical empty page
// (findings_count=0); a caller had no way to tell "nothing wrong" from
// "never checked."
func TestHandleTerraformConfigStateDriftFindingsDistinguishesUnresolvedFromNoDrift(t *testing.T) {
	t.Parallel()

	// Negative/positive control: resolution succeeded and found no drift.
	// The store returns zero rows -- this is the genuinely clean case, and
	// must stay a plain empty page, not gain a synthetic finding.
	noDriftHandler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store:   fakeTerraformConfigStateDriftStore{rows: nil},
	}
	noDriftPayload := postTerraformConfigStateDriftFindings(t, noDriftHandler, `{
		"scope_id": "state_snapshot:s3:hash-clean",
		"limit": 10
	}`)
	if got, want := noDriftPayload["findings_count"], float64(0); got != want {
		t.Fatalf("evaluated-no-drift findings_count = %#v, want %v", got, want)
	}
	// drift_findings marshals to JSON null (not []) when the store returns a
	// nil slice, matching terraformConfigStateDriftFindingRows' existing
	// nil-passthrough behavior; either shape is an empty page.
	if findings, ok := noDriftPayload["drift_findings"].([]any); ok && len(findings) != 0 {
		t.Fatalf("evaluated-no-drift drift_findings = %#v, want empty", noDriftPayload["drift_findings"])
	}

	// The case under test: ownership never resolved, durably recorded as one
	// "unresolved" finding.
	unresolvedHandler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			rows: []TerraformConfigStateDriftFindingRow{
				{
					FactID: "fact:tf-unresolved-1", ScopeID: "state_snapshot:local:hash-unresolved",
					GenerationID: "generation:tf-1", SourceSystem: "collector/terraform-state",
					CanonicalID:   "canonical:unresolved-owner:state_snapshot:local:hash-unresolved",
					CandidateID:   "unresolved_owner:state_snapshot:local:hash-unresolved",
					CandidateKind: "terraform_config_state_drift_unresolved_owner",
					Outcome:       "unresolved",
					BackendKind:   "local", LocatorHash: "hash-unresolved", Confidence: 1,
				},
			},
		},
	}
	unresolvedPayload := postTerraformConfigStateDriftFindings(t, unresolvedHandler, `{
		"scope_id": "state_snapshot:local:hash-unresolved",
		"limit": 10
	}`)
	if got, want := unresolvedPayload["findings_count"], float64(1); got != want {
		t.Fatalf("unresolved findings_count = %#v, want %v (must NOT look like an empty/clean page)", got, want)
	}
	findings, ok := unresolvedPayload["drift_findings"].([]any)
	if !ok || len(findings) != 1 {
		t.Fatalf("unresolved drift_findings = %#v, want one finding", unresolvedPayload["drift_findings"])
	}
	finding, ok := findings[0].(map[string]any)
	if !ok {
		t.Fatalf("drift_findings[0] = %#v, want map", findings[0])
	}
	if got, want := finding["outcome"], "unresolved"; got != want {
		t.Fatalf("finding outcome = %#v, want %q", got, want)
	}
	if got, ok := finding["address"]; ok && got != "" {
		t.Fatalf("finding address = %#v, want empty/absent (no anchor was resolved to classify against)", got)
	}

	groups, ok := unresolvedPayload["outcome_groups"].([]any)
	if !ok {
		t.Fatalf("outcome_groups = %#v, want array", unresolvedPayload["outcome_groups"])
	}
	sawUnresolvedGroup := false
	for _, raw := range groups {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if group["outcome"] == "unresolved" && group["count"] == float64(1) {
			sawUnresolvedGroup = true
		}
	}
	if !sawUnresolvedGroup {
		t.Fatalf("outcome_groups = %#v, want an unresolved:1 entry", groups)
	}

	limitations, ok := unresolvedPayload["limitations"].([]any)
	if !ok {
		t.Fatalf("limitations = %#v, want array", unresolvedPayload["limitations"])
	}
	sawUnresolvedExplanation := false
	for _, raw := range limitations {
		if text, ok := raw.(string); ok && strings.Contains(text, "unresolved") {
			sawUnresolvedExplanation = true
		}
	}
	if !sawUnresolvedExplanation {
		t.Fatalf("limitations = %#v, want at least one entry explaining \"unresolved\"", limitations)
	}
}

// TestHandleTerraformConfigStateDriftFindingsAcceptsUnresolvedOutcomeFilter
// proves outcome=unresolved is now a valid request-side filter value,
// extending the pre-existing exact/ambiguous validation.
func TestHandleTerraformConfigStateDriftFindingsAcceptsUnresolvedOutcomeFilter(t *testing.T) {
	t.Parallel()

	var observed TerraformConfigStateDriftFindingFilter
	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			observedFilter: &observed,
		},
	}
	payload := postTerraformConfigStateDriftFindings(t, handler, `{
		"scope_id": "state_snapshot:local:hash-unresolved",
		"outcome": "unresolved",
		"limit": 10
	}`)
	if observed.Outcome != "unresolved" {
		t.Fatalf("observed.Outcome = %q, want %q", observed.Outcome, "unresolved")
	}
	if got, want := payload["outcome"], "unresolved"; got != want {
		t.Fatalf("payload[outcome] = %#v, want %q", got, want)
	}
}

// TestHandleTerraformConfigStateDriftFindingsAcceptsDerivedOutcomeFilter
// proves outcome=derived (issue #5572) is a valid request-side filter value,
// mirroring TestHandleTerraformConfigStateDriftFindingsAcceptsUnresolvedOutcomeFilter.
func TestHandleTerraformConfigStateDriftFindingsAcceptsDerivedOutcomeFilter(t *testing.T) {
	t.Parallel()

	var observed TerraformConfigStateDriftFindingFilter
	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store: fakeTerraformConfigStateDriftStore{
			observedFilter: &observed,
		},
	}
	payload := postTerraformConfigStateDriftFindings(t, handler, `{
		"scope_id": "state_snapshot:local:hash-derived",
		"outcome": "derived",
		"limit": 10
	}`)
	if observed.Outcome != "derived" {
		t.Fatalf("observed.Outcome = %q, want %q", observed.Outcome, "derived")
	}
	if got, want := payload["outcome"], "derived"; got != want {
		t.Fatalf("payload[outcome] = %#v, want %q", got, want)
	}
}

// TestHandleTerraformConfigStateDriftFindingsRejectsUnknownOutcome proves an
// outcome value outside the closed set (exact/derived/ambiguous/unresolved)
// is still rejected with 400, not silently accepted.
func TestHandleTerraformConfigStateDriftFindingsRejectsUnknownOutcome(t *testing.T) {
	t.Parallel()

	handler := &TerraformConfigStateDriftHandler{
		Profile: ProfileLocalAuthoritative,
		Store:   fakeTerraformConfigStateDriftStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/terraform/config-state-drift/findings", bytes.NewBufferString(`{
		"scope_id": "state_snapshot:s3:hash-1",
		"outcome": "bogus"
	}`))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

// postTerraformConfigStateDriftFindings is the shared request/response
// helper for these tests: POST the given JSON body and return the decoded
// truth-envelope "data" map.
func postTerraformConfigStateDriftFindings(
	t *testing.T,
	handler *TerraformConfigStateDriftHandler,
	body string,
) map[string]any {
	t.Helper()
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/terraform/config-state-drift/findings", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body = %s", w.Code, http.StatusOK, w.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("payload[data] = %#v, want map", payload["data"])
	}
	return data
}
