// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build integration

package query

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/supplychain/impact"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestSupplyChainImpactRuntimeDigestListRouteLive retains the runnable #5469
// route proof. It drives the real findings handler and production owner-ledger
// resolver against more matching runtime rows than the 200-candidate contract
// permits, rather than timing a hand-written SQL approximation.
func TestSupplyChainImpactRuntimeDigestListRouteLive(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("ESHU_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("set ESHU_POSTGRES_TEST_DSN to run the live runtime-digest route proof")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open Postgres: %v", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })

	ctx := t.Context()
	seedCloudResourceListLiveCorpus(t, ctx, db)
	const digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := db.ExecContext(ctx, `
UPDATE graph_node_owner
SET winning_row = jsonb_set(winning_row, '{running_image_digest}', to_jsonb($1::text))
WHERE uid BETWEEN 'uid-000001' AND 'uid-001000'
`, digest); err != nil {
		t.Fatalf("seed hot route digest: %v", err)
	}
	if _, err := db.ExecContext(ctx, "ANALYZE graph_node_owner"); err != nil {
		t.Fatalf("analyze hot route digest: %v", err)
	}

	handler := &SupplyChainHandler{
		ImpactFindings: &recordingSupplyChainImpactFindingStore{
			rows: []impact.SupplyChainImpactFindingRow{{
				FindingID:     "finding-runtime-route-proof",
				CVEID:         "CVE-2026-00069",
				PackageID:     "pkg:npm/example",
				ImpactStatus:  "affected_exact",
				SubjectDigest: digest,
			}},
		},
		Readiness:              &recordingSupplyChainImpactReadinessStore{},
		CloudResourceInventory: NewPostgresCloudResourceListStore(db),
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	durations := make([]time.Duration, 0, 15)
	for range 15 {
		start := time.Now()
		req := httptest.NewRequest(
			http.MethodGet,
			"/api/v0/supply-chain/impact/findings?cve_id=CVE-2026-00069&limit=10",
			nil,
		)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		durations = append(durations, time.Since(start))
		if got, want := response.Code, http.StatusOK; got != want {
			t.Fatalf("route status = %d, want %d; body = %s", got, want, response.Body.String())
		}
		var body struct {
			Findings []impact.SupplyChainImpactFindingResult `json:"findings"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode route response: %v", err)
		}
		if len(body.Findings) != 1 {
			t.Fatalf("route findings = %#v, want exactly one", body.Findings)
		}
		if got, want := body.Findings[0].DeploymentTruthTier, "runtime_confirmed"; got != want {
			t.Fatalf("deployment truth tier = %q, want %q", got, want)
		}
		if got, want := len(body.Findings[0].CloudRuntimeResourceRefs), supplyChainCloudRuntimeProbeMaxResults; got != want {
			t.Fatalf("runtime resource refs = %d, want bounded %d", got, want)
		}
	}
	slices.Sort(durations)
	median := durations[len(durations)/2]
	if median > cloudResourceListInteractiveSLO {
		t.Fatalf("hot runtime-digest route median = %s, want <= %s", median, cloudResourceListInteractiveSLO)
	}
	// The bound counts eligible rows since #5789, not candidates, and a
	// single-digest page gets the whole shared budget as its per-digest bound.
	t.Logf("20,000-row hot runtime-digest list route median=%s runs=%d per_digest_bound=%d", median, len(durations), supplyChainCloudRuntimeProbeMaxResults)
}
