// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9227 disposable PostgreSQL API proof that committed NVD cassette snapshots certify known and absent CVEs through the producer-shaped descriptor
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
)

const (
	cassetteNVDZeroScope     = "vulnerability_intelligence:supply-chain-demo:nvd-zero"
	cassetteNVDZeroInstance  = "collector-vulnintel-nvd-zero"
	cassetteNVDZeroCVE       = "CVE-2026-09227"
	cassetteNVDKnownScope    = "vulnerability_intelligence:supply-chain-demo:nvd-known"
	cassetteNVDKnownInstance = "collector-vulnintel-nvd-known"
	cassetteNVDKnownCVE      = "CVE-2026-19227"
	// cassetteNVDEndpoint is the synthetic base_url the cassette's NVD
	// targets configure (see TestCommittedNVDCoverageCassetteMatchesProducer).
	cassetteNVDEndpoint = "https://nvd.supply-chain-demo.test/rest/json/cves/2.0"
)

// TestSupplyChainImpactReadinessKnownCVECoverageLive pins the #9227 F1 ruling
// through the live API. The NVD snapshots come from the committed golden
// cassette, whose descriptors TestCommittedNVDCoverageCassetteMatchesProducer
// binds to the real producer, so this also ties producer output to the SQL
// expected descriptor (reviewer F6).
func TestSupplyChainImpactReadinessKnownCVECoverageLive(t *testing.T) {
	for _, tc := range []struct {
		name        string
		scope       string
		instance    string
		cveID       string
		snapshot    bool
		dropCVEFact bool
		advisory    bool
		resultCount int
		want        impact.ReadinessState
		wantMissing string
		wantReason  string
	}{
		{
			name: "known CVE covered with committed advisory", scope: cassetteNVDKnownScope, instance: cassetteNVDKnownInstance,
			cveID: cassetteNVDKnownCVE, snapshot: true, resultCount: 1, want: impact.ReadinessStateReadyZeroFindings,
		},
		{
			name: "known CVE covered without advisory", scope: cassetteNVDKnownScope, instance: cassetteNVDKnownInstance,
			cveID: cassetteNVDKnownCVE, snapshot: true, dropCVEFact: true, resultCount: 1,
			want: impact.ReadinessStateEvidenceIncomplete, wantMissing: impact.MissingEvidenceAdvisorySources,
		},
		{
			name: "no snapshot with advisory from another scope", scope: cassetteNVDKnownScope, instance: cassetteNVDKnownInstance,
			cveID: cassetteNVDKnownCVE, advisory: true, want: impact.ReadinessStateTargetIncomplete,
			wantMissing: impact.MissingEvidenceTargetCollection, wantReason: "nvd:committed_snapshot_missing",
		},
		{
			name: "zero snapshot with advisory", scope: cassetteNVDZeroScope, instance: cassetteNVDZeroInstance,
			cveID: cassetteNVDZeroCVE, snapshot: true, advisory: true, want: impact.ReadinessStateReadyZeroFindings,
		},
		{
			name: "zero snapshot certifies absence", scope: cassetteNVDZeroScope, instance: cassetteNVDZeroInstance,
			cveID: cassetteNVDZeroCVE, snapshot: true, want: impact.ReadinessStateReadyZeroFindings,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, db, now := readinessScopeLiveDatabase(t)
			seedReadinessScopeInstance(t, ctx, db, now, tc.instance, []map[string]any{{
				"source": "nvd", "scope_id": tc.scope, "cve_ids": []string{tc.cveID}, "base_url": cassetteNVDEndpoint,
			}})
			seedReadinessScopeState(t, ctx, db, now, tc.instance, tc.scope, "nvd", "fresh", "succeeded")
			if _, err := db.ExecContext(ctx, `UPDATE vulnerability_source_states SET result_count = $1`, tc.resultCount); err != nil {
				t.Fatalf("seed source state result count: %v", err)
			}
			if tc.snapshot {
				seedReadinessCassetteScope(t, ctx, db, now, tc.scope)
			}
			if tc.dropCVEFact {
				execReadinessCoverageMutation(t, ctx, db, `DELETE FROM fact_records WHERE fact_kind = 'vulnerability.cve'`)
			}
			if tc.advisory {
				seedReadinessActiveAdvisoryFact(t, ctx, db, now, tc.cveID)
			}
			readiness := readReadinessScopeAPI(t, db, tc.cveID)
			if readiness.State != tc.want {
				t.Fatalf("readiness = %+v, want %q", readiness, tc.want)
			}
			if tc.want == impact.ReadinessStateReadyZeroFindings && readiness.Freshness != "fresh" {
				t.Fatalf("freshness = %q, want fresh", readiness.Freshness)
			}
			if tc.wantMissing != "" && !slices.Contains(readiness.MissingEvidence, tc.wantMissing) {
				t.Fatalf("missing evidence = %v, want %s", readiness.MissingEvidence, tc.wantMissing)
			}
			if tc.wantReason == "" && slices.Contains(readiness.IncompleteReasons, "nvd:committed_snapshot_missing") {
				t.Fatalf("matching snapshot reported committed_snapshot_missing: %v", readiness.IncompleteReasons)
			}
			if tc.wantReason != "" && !slices.Contains(readiness.IncompleteReasons, tc.wantReason) {
				t.Fatalf("incomplete reasons = %v, want %s", readiness.IncompleteReasons, tc.wantReason)
			}
		})
	}
}

// TestSupplyChainImpactReadinessStaticWindowStaysHealthOnlyLive pins the #9227
// F2 ruling: a static modified-window target whose window ended long ago but
// whose last success is recent stays fresh and never blocks a covered CVE.
func TestSupplyChainImpactReadinessStaticWindowStaysHealthOnlyLive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, cassetteNVDZeroInstance, []map[string]any{{
		"source": "nvd", "scope_id": cassetteNVDZeroScope, "cve_ids": []string{cassetteNVDZeroCVE}, "base_url": cassetteNVDEndpoint,
	}})
	seedReadinessScopeState(t, ctx, db, now, cassetteNVDZeroInstance, cassetteNVDZeroScope, "nvd", "fresh", "succeeded")
	seedReadinessCassetteScope(t, ctx, db, now, cassetteNVDZeroScope)
	windowStart, windowEnd := now.Add(-60*24*time.Hour), now.Add(-30*24*time.Hour)
	seedReadinessScopeInstance(t, ctx, db, now, "historic-window", []map[string]any{{
		"source": "nvd", "scope_id": "historic-window-scope",
		"modified_start": windowStart.Format(time.RFC3339), "modified_end": windowEnd.Format(time.RFC3339),
	}})
	seedReadinessScopeState(t, ctx, db, now, "historic-window", "historic-window-scope", "nvd", "fresh", "succeeded")
	if _, err := db.ExecContext(ctx, `UPDATE vulnerability_source_states SET window_start = $1, window_end = $2
        WHERE collector_instance_id = 'historic-window'`, windowStart, windowEnd); err != nil {
		t.Fatalf("seed historic collection window: %v", err)
	}
	readiness := readReadinessScopeAPI(t, db, cassetteNVDZeroCVE)
	if readiness.State != impact.ReadinessStateReadyZeroFindings || readiness.Freshness != "fresh" {
		t.Fatalf("readiness = %+v, want ready_zero_findings/fresh", readiness)
	}
	if len(readiness.SourceStates) != 2 {
		t.Fatalf("source states = %+v, want covered CVE target plus window target", readiness.SourceStates)
	}
	for _, state := range readiness.SourceStates {
		if state.FreshnessState != "fresh" {
			t.Fatalf("source state %s = %q, want fresh by success time", state.ScopeID, state.FreshnessState)
		}
	}
}

// TestSupplyChainImpactReadinessLowercaseConfiguredCVELive pins reviewer F5: a
// lowercase configured CVE selects the same target as its uppercase spelling,
// as the collector's case-insensitive match does.
func TestSupplyChainImpactReadinessLowercaseConfiguredCVELive(t *testing.T) {
	ctx, db, now := readinessScopeLiveDatabase(t)
	seedReadinessScopeInstance(t, ctx, db, now, cassetteNVDZeroInstance, []map[string]any{{
		"source": "nvd", "scope_id": cassetteNVDZeroScope, "cve_ids": []string{" cve-2026-09227 "}, "base_url": cassetteNVDEndpoint,
	}})
	seedReadinessScopeState(t, ctx, db, now, cassetteNVDZeroInstance, cassetteNVDZeroScope, "nvd", "fresh", "succeeded")
	seedReadinessCassetteScope(t, ctx, db, now, cassetteNVDZeroScope)
	readiness := readReadinessScopeAPI(t, db, cassetteNVDZeroCVE)
	if readiness.State != impact.ReadinessStateReadyZeroFindings || len(readiness.SourceStates) != 1 {
		t.Fatalf("lowercase configured CVE readiness = %+v, want ready_zero_findings with one state", readiness)
	}
}

// seedReadinessCassetteScope loads one committed vulnerability-intelligence
// cassette scope as the active generation, with observation time now so the
// committed snapshot reads as fresh.
func seedReadinessCassetteScope(t *testing.T, ctx context.Context, db *sql.DB, now time.Time, scopeID string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "cassettes",
		"vulnerabilityintelligence", "supply-chain-demo.json"))
	if err != nil {
		t.Fatalf("read vulnerability intelligence cassette: %v", err)
	}
	var file struct {
		Scopes []struct {
			ScopeID      string `json:"scope_id"`
			GenerationID string `json:"generation_id"`
			Facts        []struct {
				FactKind      string          `json:"fact_kind"`
				StableFactKey string          `json:"stable_fact_key"`
				Payload       json.RawMessage `json:"payload"`
			} `json:"facts"`
		} `json:"scopes"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("decode cassette: %v", err)
	}
	for _, scope := range file.Scopes {
		if scope.ScopeID != scopeID {
			continue
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO ingestion_scopes
            (scope_id, scope_kind, source_system, source_key, collector_kind, partition_key, observed_at, ingested_at, status, active_generation_id)
            VALUES ($1, 'vulnerability', 'vulnerability_intelligence', $1, 'vulnerability_intelligence', 'nvd', $2, $2, 'active', $3)`,
			scope.ScopeID, now, scope.GenerationID); err != nil {
			t.Fatalf("seed cassette scope: %v", err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO scope_generations
            (scope_id, generation_id, trigger_kind, observed_at, ingested_at, status)
            VALUES ($1, $2, 'snapshot', $3, $3, 'active')`, scope.ScopeID, scope.GenerationID, now); err != nil {
			t.Fatalf("seed cassette generation: %v", err)
		}
		for _, fact := range scope.Facts {
			if _, err := db.ExecContext(ctx, `INSERT INTO fact_records
                (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, source_system,
                 source_fact_key, observed_at, ingested_at, is_tombstone, payload)
                VALUES ($1, $2, $3, $4, $1, 'vulnerability_intelligence', $1, $5, $5, FALSE, $6::jsonb)`,
				fact.StableFactKey, scope.ScopeID, scope.GenerationID, fact.FactKind, now, string(fact.Payload)); err != nil {
				t.Fatalf("seed cassette fact %s: %v", fact.StableFactKey, err)
			}
		}
		return
	}
	t.Fatalf("cassette scope %s not found", scopeID)
}
