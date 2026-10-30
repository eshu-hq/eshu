// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9322 disposable PostgreSQL API proof that a multi-CVE NVD target says so in the response with multi_cve_target on its source-state row
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// multiCVEStateView decodes the wire JSON, not the Go type, so the assertion
// runs against a build that predates the multi_cve_target field.
type multiCVEStateView struct {
	ScopeID        string `json:"scope_id"`
	Source         string `json:"source"`
	MultiCVETarget bool   `json:"multi_cve_target"`
}

func readMultiCVEReadiness(t *testing.T, db *sql.DB, cveID string) ([]multiCVEStateView, []map[string]any) {
	t.Helper()
	handler := &SupplyChainHandler{
		ImpactFindings: &recordingSupplyChainImpactFindingStore{},
		Readiness:      impact.NewPostgresReadinessStore(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	request := httptest.NewRequest(http.MethodGet,
		"/api/v0/supply-chain/impact/findings?cve_id="+cveID+"&limit=10", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Readiness struct {
			SourceStates []multiCVEStateView `json:"source_states"`
		} `json:"readiness"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Raw decode pins the documented omit shape: presence checks by
	// consumers must see the key on multi-CVE rows and no key at all on
	// single-CVE rows, not an explicit false.
	var raw struct {
		Readiness struct {
			SourceStates []map[string]any `json:"source_states"`
		} `json:"readiness"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode raw response: %v", err)
	}
	return body.Readiness.SourceStates, raw.Readiness.SourceStates
}

func seedMultiCVENVDCommittedSnapshot(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, scopeID, instanceID string, cveIDs []string) {
	t.Helper()
	seedReadinessSourceSnapshotFact(t, ctx, db, now, scopeID, "nvd", "", instanceID, true)
	descriptor, err := json.Marshal(map[string]any{
		"version": "nvd.v1", "source": "nvd", "mode": "cve",
		"cve_ids":                cveIDs,
		"modified_start_unix_us": 0, "modified_end_unix_us": 0,
		"results_per_page":    0,
		"endpoint_candidates": []string{"https://services.nvd.nist.gov/rest/json/cves/2.0"},
		"executed_endpoint":   "https://services.nvd.nist.gov/rest/json/cves/2.0",
	})
	if err != nil {
		t.Fatalf("marshal NVD coverage descriptor: %v", err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE fact_records
        SET payload = JSONB_SET(payload, '{coverage_descriptor}', $2::jsonb)
        WHERE fact_id = $1`, scopeID+"-snapshot", descriptor); err != nil {
		t.Fatalf("seed NVD coverage descriptor: %v", err)
	}
}

// TestSupplyChainImpactReadinessMultiCVETargetSaysSoLive is #9322 C2: an NVD
// target that lists several CVEs reports multi_cve_target on its source-state
// row, while a single-CVE target does not, so the API says absence is per
// target.
func TestSupplyChainImpactReadinessMultiCVETargetSaysSoLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, "multi-cve-instance", []map[string]any{
		{"source": "nvd", "scope_id": "multi-target", "cve_ids": []string{"CVE-2026-9227", "CVE-2026-9228"}},
		{"source": "nvd", "scope_id": "single-target", "cve_ids": []string{"CVE-2026-9227"}},
	})
	seedReadinessScopeState(t, ctx, db, now, "multi-cve-instance", "multi-target", "nvd", "fresh", "succeeded")
	seedReadinessScopeState(t, ctx, db, now, "multi-cve-instance", "single-target", "nvd", "fresh", "succeeded")
	seedMultiCVENVDCommittedSnapshot(t, ctx, db, now, "multi-target", "multi-cve-instance",
		[]string{"CVE-2026-9227", "CVE-2026-9228"})
	seedMultiCVENVDCommittedSnapshot(t, ctx, db, now, "single-target", "multi-cve-instance",
		[]string{"CVE-2026-9227"})

	states, rawStates := readMultiCVEReadiness(t, db, "CVE-2026-9227")
	got := map[string]bool{}
	for _, state := range states {
		if state.Source == "nvd" {
			got[state.ScopeID] = state.MultiCVETarget
		}
	}
	if !got["multi-target"] {
		t.Fatalf("multi-CVE NVD target source states = %+v, want multi_cve_target on multi-target", states)
	}
	if got["single-target"] {
		t.Fatalf("multi-CVE NVD target source states = %+v, want no multi_cve_target on single-target", states)
	}
	rawGot := map[string]struct {
		present bool
		value   any
	}{}
	for _, state := range rawStates {
		scopeID, _ := state["scope_id"].(string)
		source, _ := state["source"].(string)
		if source != "nvd" {
			continue
		}
		value, present := state["multi_cve_target"]
		rawGot[scopeID] = struct {
			present bool
			value   any
		}{present: present, value: value}
	}
	multiRaw := rawGot["multi-target"]
	if !multiRaw.present || multiRaw.value != true {
		t.Fatalf("multi-target raw source state = %+v, want multi_cve_target present and true", multiRaw)
	}
	if singleRaw := rawGot["single-target"]; singleRaw.present {
		t.Fatalf("single-target raw source state = %+v, want multi_cve_target key omitted, not explicit false", singleRaw)
	}
}
