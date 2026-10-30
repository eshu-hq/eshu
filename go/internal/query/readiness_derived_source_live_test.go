// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9227 F7 disposable PostgreSQL API proof that derived owned-package OSV scopes reach readiness through recorded package keys as one aggregate per instance, source, and ecosystem, without unrelated scopes counting toward the 200-target bound
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

// derivedReadinessView decodes the wire JSON, not the Go type, so the same
// assertions can run against a build that predates the derived fields.
type derivedReadinessView struct {
	State             string             `json:"readiness_state"`
	IncompleteReasons []string           `json:"incomplete_reasons"`
	SourceStates      []derivedStateView `json:"source_states"`
}

type derivedStateView struct {
	CollectorInstanceID   string   `json:"collector_instance_id"`
	ScopeID               string   `json:"scope_id"`
	Source                string   `json:"source"`
	Ecosystem             string   `json:"ecosystem"`
	FreshnessState        string   `json:"freshness_state"`
	TerminalStatus        string   `json:"terminal_status"`
	Derived               bool     `json:"derived"`
	ScopeCount            int      `json:"scope_count"`
	PackageCount          int      `json:"package_count"`
	UnmatchedPackageCount int      `json:"unmatched_package_count"`
	SkippedPackageCount   int      `json:"skipped_package_count"`
	SkipReasons           []string `json:"skip_reasons"`
	FreshCount            int      `json:"fresh_count"`
	FailedCount           int      `json:"failed_count"`
	ScopeIDs              []string `json:"scope_ids"`
}

func readDerivedReadiness(t *testing.T, db *sql.DB, rawQuery string) derivedReadinessView {
	t.Helper()
	handler := &SupplyChainHandler{
		ImpactFindings: &recordingSupplyChainImpactFindingStore{},
		Readiness:      impact.NewPostgresReadinessStore(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	request := httptest.NewRequest(http.MethodGet, "/api/v0/supply-chain/impact/findings?"+rawQuery+"&limit=10", nil)
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Readiness derivedReadinessView `json:"readiness"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return body.Readiness
}

func seedDerivedInstance(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, id string, ecosystems ...string) {
	t.Helper()
	configuration, err := json.Marshal(map[string]any{
		"targets": []any{},
		"derive_from_owned_packages": map[string]any{
			"enabled": true, "sources": []string{"osv"}, "ecosystems": ecosystems,
		},
	})
	if err != nil {
		t.Fatalf("marshal derived configuration: %v", err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO collector_instances
        (instance_id, collector_kind, mode, enabled, configuration, last_observed_at, created_at, updated_at)
        VALUES ($1, 'vulnerability_intelligence', 'continuous', TRUE, $2, $3, $3, $3)`, id, configuration, now); err != nil {
		t.Fatalf("seed derived instance %s: %v", id, err)
	}
}

// seedDerivedScope writes one derived source state and, unless keys is nil,
// its package-key rows, the way the collector's upsert does.
func seedDerivedScope(t *testing.T, ctx context.Context, db *sql.DB, at time.Time,
	instance, scope, ecosystem, freshness, terminal string, keys [][2]string,
) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `INSERT INTO vulnerability_source_states
        (collector_instance_id, scope_id, source, ecosystem, freshness_state, terminal_status,
         last_attempt_at, last_success_at, updated_at)
        VALUES ($1, $2, 'osv', $3, $4, $5, $6, $6, $6)`,
		instance, scope, ecosystem, freshness, terminal, at); err != nil {
		t.Fatalf("seed derived state %s: %v", scope, err)
	}
	for _, key := range keys {
		if _, err := db.ExecContext(ctx, `INSERT INTO vulnerability_source_state_packages
            (collector_instance_id, scope_id, ecosystem, package_name) VALUES ($1, $2, $3, $4)`,
			instance, scope, key[0], key[1]); err != nil {
			t.Fatalf("seed package key %s/%s: %v", scope, key[1], err)
		}
	}
}

// seedUnrelatedDerivedScopes writes count fresh npm scopes, each keyed to its
// own package that no test requests.
func seedUnrelatedDerivedScopes(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, instance string, count int) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
WITH scopes AS (
    SELECT FORMAT('vuln-intel://osv/npm/unrelated-%s?version=1.0.0', i) AS scope_id,
        FORMAT('unrelated-%s', i) AS package_name
    FROM GENERATE_SERIES(0, $3 - 1) AS i
), states AS (
    INSERT INTO vulnerability_source_states
        (collector_instance_id, scope_id, source, ecosystem, freshness_state, terminal_status,
         last_attempt_at, last_success_at, updated_at)
    SELECT $1, scope_id, 'osv', 'npm', 'fresh', 'succeeded', $2, $2, $2 FROM scopes
)
INSERT INTO vulnerability_source_state_packages (collector_instance_id, scope_id, ecosystem, package_name)
SELECT $1, scope_id, 'npm', package_name FROM scopes`, instance, now, count); err != nil {
		t.Fatalf("seed %d unrelated derived scopes: %v", count, err)
	}
}

func onlyDerivedState(t *testing.T, readiness derivedReadinessView) derivedStateView {
	t.Helper()
	var derived []derivedStateView
	for _, state := range readiness.SourceStates {
		if state.Derived {
			derived = append(derived, state)
		}
	}
	if len(derived) != 1 {
		t.Fatalf("derived source states = %+v, want exactly one; readiness = %+v", derived, readiness)
	}
	return derived[0]
}

const lodashScope = "vuln-intel://osv/npm/lodash?version=4.17.21"

var lodashKeys = [][2]string{{"npm", "lodash"}}

func TestSupplyChainImpactReadinessDerivedUnrelatedScopesDoNotOverflowLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-npm", "npm")
	seedUnrelatedDerivedScopes(t, ctx, db, now, "derived-npm", 201)
	seedDerivedScope(t, ctx, db, now, "derived-npm", lodashScope, "npm", "fresh", "succeeded", lodashKeys)

	readiness := readDerivedReadiness(t, db, "package_id=npm://registry.npmjs.org/lodash")
	if readiness.State == string(impact.ReadinessStateReadinessUnavailable) {
		t.Fatalf("201 unrelated derived scopes made readiness unavailable: %+v", readiness)
	}
	state := onlyDerivedState(t, readiness)
	if state.ScopeCount != 1 || state.PackageCount != 1 || state.UnmatchedPackageCount != 0 ||
		state.FreshnessState != "fresh" || state.ScopeID != "derived://osv/npm" ||
		!slices.Equal(state.ScopeIDs, []string{lodashScope}) {
		t.Fatalf("derived aggregate = %+v, want one fresh lodash scope", state)
	}
}

func TestSupplyChainImpactReadinessDerivedMissingScopeReadsPendingLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedDerivedInstance(t, ctx, db, now, "derived-npm", "npm")

	readiness := readDerivedReadiness(t, db, "package_id=npm://registry.npmjs.org/lodash")
	state := onlyDerivedState(t, readiness)
	if state.ScopeCount != 0 || state.UnmatchedPackageCount != 1 ||
		state.FreshnessState != "pending" || state.TerminalStatus != "pending" {
		t.Fatalf("derived aggregate = %+v, want scope_count 0, unmatched 1, pending", state)
	}
	if readiness.State != string(impact.ReadinessStateTargetIncomplete) ||
		!slices.Contains(readiness.IncompleteReasons, "osv:pending") {
		t.Fatalf("readiness = %+v, want target_incomplete with osv:pending", readiness)
	}
}

func TestSupplyChainImpactReadinessDerivedNewestStateWinsLive(t *testing.T) {
	for _, tc := range []struct {
		name               string
		older, newer, want string
	}{
		{"older fresh, newer failed", "fresh", "failed", "failed"},
		{"older failed, newer fresh", "failed", "fresh", "fresh"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedDerivedInstance(t, ctx, db, now, "derived-npm", "npm")
			terminal := map[string]string{"fresh": "succeeded", "failed": "failed_retryable"}
			seedDerivedScope(t, ctx, db, now.Add(-time.Hour), "derived-npm",
				"vuln-intel://osv/npm/lodash?version=4.17.20", "npm", tc.older, terminal[tc.older], lodashKeys)
			seedDerivedScope(t, ctx, db, now, "derived-npm",
				lodashScope, "npm", tc.newer, terminal[tc.newer], lodashKeys)

			state := onlyDerivedState(t, readDerivedReadiness(t, db, "package_id=npm://registry.npmjs.org/lodash"))
			if state.FreshnessState != tc.want || state.ScopeCount != 1 ||
				!slices.Equal(state.ScopeIDs, []string{lodashScope}) {
				t.Fatalf("derived aggregate = %+v, want only the newer scope (%s)", state, tc.want)
			}
		})
	}
}

func TestSupplyChainImpactReadinessConfiguredOSVOverflowStillRefusesLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	targets := make([]map[string]any, 0, 201)
	for index := range 201 {
		targets = append(targets, map[string]any{
			"source": "osv", "scope_id": fmt.Sprintf("configured-osv-%03d", index), "ecosystem": "npm",
			"queries": []map[string]any{{"name": fmt.Sprintf("pkg-%03d", index), "version": "1.0.0"}},
		})
	}
	seedReadinessScopeInstance(t, ctx, db, now, "configured-osv", targets)

	readiness := readDerivedReadiness(t, db, "package_id=npm://registry.npmjs.org/lodash")
	if readiness.State != string(impact.ReadinessStateReadinessUnavailable) {
		t.Fatalf("201 configured OSV targets readiness = %+v, want readiness_unavailable", readiness)
	}
}
