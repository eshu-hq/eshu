// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9875 fleet acceptance gate on disposable PostgreSQL 18: an advisory, a KEV listing, and an owned package on a catalogued service drain through the real SupplyChainImpactHandler and queue, the keyed winners tick, and the grouped inventory HTTP route; the service bucket must be complete with the KEV-scored critical winner, the unmapped control must sit in the unattributed bucket, and source commit to rollup read must stay within ESHU_FLEET_GATE_MAX_ELAPSED (default 30s); withholding the KEV, affected_package, or service mapping fact must fail the gate; reads ESHU_FLEET_GATE_PROOF_DSN with its disposable opt-in
//eshu:readiness:env ESHU_FLEET_GATE_PROOF_DISPOSABLE ESHU_FLEET_GATE_PROOF_DSN

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu-sdk/factschema"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/query"
	"github.com/eshu-hq/eshu/go/internal/query/supply/chain/impact"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/testutil/postgresproof"
)

const (
	fleetGateCVE          = "CVE-2026-9875"
	fleetGatePackage      = "pkg:npm/fleet-9875@1.0.0"
	fleetGateAdvisory     = "scope:9875:nvd"
	fleetGateKEV          = "scope:9875:kev"
	fleetGateRepo         = "repository:r_9875_svc"
	fleetGateUnmappedRepo = "repository:r_9875_unmapped"
)

// fleetGateSeed selects which facts the gate seeds. The clean path seeds all
// three; each planted violation withholds exactly one.
type fleetGateSeed struct {
	affectedPackage bool
	kev             bool
	serviceMapping  bool
}

// fleetGateRun is one disposable database driven through the gate.
type fleetGateRun struct {
	*convergence
	mux http.Handler
}

// newFleetGateRun opens a disposable database with the production bootstrap
// and the #9695 convergence worker (real fact loader, writer, fencing issuer,
// fan-out store, and queue). It opens the database through its own
// ESHU_FLEET_GATE_PROOF_DSN pair, so the gate runs in the live-postgres-readiness
// runner rather than the reducer contention gate.
func newFleetGateRun(t *testing.T) *fleetGateRun {
	t.Helper()
	ctx, db := postgresproof.OpenDisposableBootstrapDatabase(
		t,
		os.Getenv("ESHU_FLEET_GATE_PROOF_DSN"),
		os.Getenv("ESHU_FLEET_GATE_PROOF_DISPOSABLE"),
		5*time.Minute,
		func(ctx context.Context, database *sql.DB) error {
			return postgres.ApplyBootstrapWithoutContentSearchIndexes(ctx, postgres.SQLDB{DB: database})
		},
	)
	now := time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	fanOut := fanOutLiveStore(db)
	c := &convergence{
		t: t, ctx: ctx, db: db, queue: fanOutLiveQueue(db), fanOut: fanOut,
		handler: supplychaincore.SupplyChainImpactHandler{
			FactLoader:         postgres.NewFactStore(postgres.SQLDB{DB: db}),
			FencingTokenIssuer: postgres.PostgresSupplyChainImpactFencingTokenIssuer{DB: postgres.SQLDB{DB: db}},
			Writer: supplychaincore.PostgresSupplyChainImpactWriter{
				DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
				Now: func() time.Time { return now },
			},
			FanOut: fanOut,
			Now:    func() time.Time { return now },
		},
	}
	handler := &query.SupplyChainHandler{
		ImpactGroupedInventory: impact.PostgresGroupedInventoryStore{DB: db, ReadFromWinners: true},
		ImpactAggregates:       impact.PostgresAggregateStore{DB: db},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)
	return &fleetGateRun{convergence: c, mux: mux}
}

// seed commits the gate's facts and their pending work items. The advisory
// scope holds the CVE and (unless withheld) its affected_package record; the
// KEV scope lists the CVE; each repository scope holds the package
// consumption that makes it an owner of the affected version, and the service
// repository also holds its service/workload catalog mapping.
func (r *fleetGateRun) seed(s fleetGateSeed) {
	r.t.Helper()
	fanOutSeedScope(r.t, r.ctx, r.db, fleetGateAdvisory, "g1")
	gen := fanOutGen(fleetGateAdvisory, "g1")
	advisory := []facts.Envelope{
		r.envelope(fleetGateAdvisory, gen, "fact:9875:cve", chain.VulnerabilityCVEFactKind, map[string]any{
			"advisory_id": fleetGateCVE, "cve_id": fleetGateCVE, "source": "nvd", "cvss_score": 7.5, "status": "Analyzed",
		}),
	}
	if s.affectedPackage {
		advisory = append(advisory, r.envelope(fleetGateAdvisory, gen, "fact:9875:affected", chain.VulnerabilityAffectedPackageFactKind, map[string]any{
			"advisory_id": fleetGateCVE, "cve_id": fleetGateCVE, "package_id": fleetGatePackage, "ecosystem": "npm",
			"package_name": "fleet-9875", "affected_versions": []string{"1.0.0"}, "fixed_versions": []string{"1.1.0"},
		}))
	}
	r.upsert(advisory...)
	fanOutSeedItem(r.t, r.ctx, r.db, fleetGateAdvisory, "g1")

	if s.kev {
		fanOutSeedScope(r.t, r.ctx, r.db, fleetGateKEV, "g1")
		kevGen := fanOutGen(fleetGateKEV, "g1")
		r.upsert(r.envelope(fleetGateKEV, kevGen, "fact:9875:kev", chain.VulnerabilityKnownExploitedFactKind, map[string]any{
			"cve_id": fleetGateCVE, "date_added": "2026-11-03",
		}))
		fanOutSeedItem(r.t, r.ctx, r.db, fleetGateKEV, "g1")
	}

	for _, repo := range []string{fleetGateRepo, fleetGateUnmappedRepo} {
		scope := "git-repository-scope:" + repo
		fanOutSeedScope(r.t, r.ctx, r.db, scope, "g1")
		repoGen := fanOutGen(scope, "g1")
		repoFacts := []facts.Envelope{
			r.envelope(scope, repoGen, "fact:9875:consumption:"+repo, factschema.FactKindReducerPackageConsumptionCorrelation, map[string]any{
				"package_id": fleetGatePackage, "relationship_kind": "consumption", "repository_id": repo,
				"dependency_range": "1.0.0", "canonical_writes": 1, "evidence_fact_ids": []string{"fact:9875:manifest:" + repo},
			}),
		}
		if repo == fleetGateRepo && s.serviceMapping {
			repoFacts = append(repoFacts, r.envelope(scope, repoGen, "fact:9875:service", "reducer_service_catalog_correlation", map[string]any{
				"repository_id": repo, "service_id": fleetGateService, "workload_id": "workload:fleet-9875", "outcome": "exact",
			}))
		}
		r.upsert(repoFacts...)
		fanOutSeedItem(r.t, r.ctx, r.db, scope, "g1")
	}
}

// observe runs the gate end to end on a seeded database and returns what it
// read back, with the test-side stage stamps.
func (r *fleetGateRun) observe(s fleetGateSeed) fleetGateObservation {
	r.t.Helper()
	winners := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: r.db})
	// The reducer's startup full resweep runs before any finding exists, as in
	// production, so the measured winners stage is the steady-state keyed tick.
	if _, err := winners.ResweepWinners(r.ctx, time.Now().UTC()); err != nil {
		r.t.Fatalf("startup winners resweep: %v", err)
	}

	var o fleetGateObservation
	r.seed(s)
	o.Stamps.SourceCommitted = time.Now()
	r.drain()
	o.Stamps.FindingWritten = time.Now()
	tick, err := winners.ResweepWinnersKeyed(r.ctx, time.Now().UTC())
	if err != nil {
		r.t.Fatalf("keyed winners tick: %v", err)
	}
	o.Stamps.WinnersBuilt = time.Now()
	var rollup struct {
		Buckets          []fleetGateBucket `json:"buckets"`
		DistinctFindings int               `json:"distinct_findings"`
	}
	o.RollupStatus = r.get("/api/v0/supply-chain/impact/inventory?group_by=service_id&limit=50", &rollup)
	o.Stamps.RollupRead = time.Now()
	o.Buckets, o.DistinctFindings = rollup.Buckets, rollup.DistinctFindings
	r.t.Logf("keyed winners tick: path %s, identifiers %d, inserted %d, updated %d, skipped %t (handled %v)",
		tick.Path, tick.Identifiers, tick.Inserted, tick.Updated, tick.Skipped, r.handled)

	var tiers struct {
		Buckets []struct {
			Value string `json:"value"`
			Count int    `json:"count"`
		} `json:"buckets"`
	}
	o.TierStatus = r.get("/api/v0/supply-chain/impact/inventory?group_by=priority_bucket&service_id="+fleetGateService+"&limit=50", &tiers)
	o.ServiceTiers = map[string]int{}
	for _, bucket := range tiers.Buckets {
		o.ServiceTiers[bucket.Value] = bucket.Count
	}

	store := query.NewPostgresSupplyChainImpactFindingStoreWithReadModel(r.db, true)
	rows, err := store.ListSupplyChainImpactFindings(r.ctx, query.SupplyChainImpactFindingFilter{
		CVEID: fleetGateCVE, RepositoryID: fleetGateRepo, Limit: 10,
	})
	if err != nil {
		r.t.Fatalf("list winners: %v", err)
	}
	if len(rows) > 1 {
		r.t.Fatalf("winners for %s on %s = %d rows, want at most one canonical finding", fleetGateCVE, fleetGateRepo, len(rows))
	}
	if len(rows) == 1 {
		o.Winner = &fleetGateWinner{
			KnownExploited: rows[0].KnownExploited, PriorityScore: rows[0].PriorityScore,
			PriorityBucket: rows[0].PriorityBucket, PriorityReasonCodes: rows[0].PriorityReasonCodes,
			ServiceIDs: rows[0].ServiceIDs,
		}
	}
	return o
}

// get serves one GET through the mounted supply-chain routes and decodes a
// 200 body into out.
func (r *fleetGateRun) get(target string, out any) int {
	r.t.Helper()
	rec := httptest.NewRecorder()
	r.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil).WithContext(r.ctx))
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			r.t.Fatalf("decode %s: %v; body = %s", target, err, rec.Body.String())
		}
	} else {
		r.t.Logf("%s answered %d: %s", target, rec.Code, rec.Body.String())
	}
	return rec.Code
}

// fleetGateLiveExpectation is the clean-path expectation: one finding on the
// service and one on the unmapped control repository. The service finding is
// critical only with both the KEV listing and the catalogued deployment
// evidence: measured on PostgreSQL 18.6, it scores 100 with both, 80 (high)
// without the KEV listing, and 90 (high) without the service mapping (the
// reducer's cisa_kev +30 and deployed_workload_evidence +20 contributions in
// reducer/supplychain/core/priority.go; critical starts at 95).
func fleetGateLiveExpectation(t *testing.T) fleetGateExpectation {
	t.Helper()
	bound, err := fleetGateBoundFrom(os.Getenv(fleetGateBoundEnv))
	if err != nil {
		t.Fatalf("bound: %v", err)
	}
	return fleetGateExpectation{
		ServiceID:         fleetGateService,
		ImpactStatus:      "affected_exact",
		PriorityTier:      "critical",
		UnattributedCount: 1,
		DistinctFindings:  2,
		Bound:             bound,
	}
}

// TestFleetAcceptanceGateKEVReachesServiceRollup9875Live is the #9875 gate's
// clean path: the seeded KEV CVE on a catalogued service reaches the service
// rollup with its KEV-scored critical tier inside the elapsed bound.
func TestFleetAcceptanceGateKEVReachesServiceRollup9875Live(t *testing.T) {
	run := newFleetGateRun(t)
	o := run.observe(fleetGateSeed{affectedPackage: true, kev: true, serviceMapping: true})
	for _, stage := range o.Stamps.stages() {
		t.Logf("stage %s: %v", stage.Name, stage.Elapsed)
	}
	if o.Winner != nil {
		t.Logf("winner: score %d bucket %s kev %t services %v reasons %v",
			o.Winner.PriorityScore, o.Winner.PriorityBucket, o.Winner.KnownExploited, o.Winner.ServiceIDs, o.Winner.PriorityReasonCodes)
	}
	t.Logf("rollup buckets %s, distinct %d, tiers %v", fleetGateBucketSummary(o.Buckets), o.DistinctFindings, o.ServiceTiers)
	if violations := fleetGateViolations(o, fleetGateLiveExpectation(t)); len(violations) != 0 {
		t.Fatalf("fleet gate failed on the clean seed:\n%v", violations)
	}
}

// TestFleetAcceptanceGateFailsWhenEvidenceIsWithheld9875Live is the RED half:
// each run withholds one fact the gate depends on, and the gate must fail with
// the named violations. A gate that passed here would pass with the finding
// withheld.
func TestFleetAcceptanceGateFailsWhenEvidenceIsWithheld9875Live(t *testing.T) {
	// Skip at the parent, so an unset DSN reads as a skip and never as a
	// passing parent over skipped subtests.
	if os.Getenv("ESHU_FLEET_GATE_PROOF_DSN") == "" {
		t.Skip("set ESHU_FLEET_GATE_PROOF_DSN to run the live #9875 fleet gate RED half on a disposable database")
	}
	for _, tc := range []struct {
		name   string
		seed   fleetGateSeed
		wanted []string
	}{
		{"KEV listing withheld", fleetGateSeed{affectedPackage: true, serviceMapping: true}, []string{"kev_missing", "priority_tier"}},
		{"affected package withheld", fleetGateSeed{kev: true, serviceMapping: true}, []string{"service_absent", "winner_absent", "unattributed_bucket"}},
		{"service mapping withheld", fleetGateSeed{affectedPackage: true, kev: true}, []string{"service_absent", "winner_service", "unattributed_bucket"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			run := newFleetGateRun(t)
			violations := fleetGateViolations(run.observe(tc.seed), fleetGateLiveExpectation(t))
			t.Logf("planted %q: violations %q", tc.name, violations)
			for _, code := range tc.wanted {
				if !fleetGateHasViolation(violations, code) {
					t.Fatalf("RED: withholding %s reported %q, want a %s violation", tc.name, violations, code)
				}
			}
		})
	}
}
