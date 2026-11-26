// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11466 disposable PostgreSQL 18 proof of the durable set-once-per-value enrichment first-carry stamp: a seeded EPSS rescore and KEV listing drive the real finding build and the production writer, the real full and keyed resweeps stamp first_carried_at per (canonical_key, enrichment_source) with set-once-per-value semantics, the QA-shaped lag join measures a finite arrival-to-rescore lag after the stamp where none was measurable before it, rewrites and same-value re-ingests never move the stamp, a KEV delist moves it observably, a backfilled value is stamped but excluded from the steady-state cohort, and concurrent resweeps leave exactly one stamp row; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"context"
	"database/sql"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Failing-first proof for eshu-hq/eshu#11466 (cases a-f): the durable
// set-once-per-value enrichment first-carry stamp, measured end to end.
//
// A seeded CVE with EPSS and KEV enrichment drives the real finding build
// (supplychaincore.BuildSupplyChainImpactFindings) and the real production
// writer (PostgresSupplyChainImpactWriter) on a disposable database, and the
// real full and keyed resweeps (SupplyChainImpactWinnersStore) claim the
// carry stamp in the resweep transaction. Before the stamp exists (pre-stamp
// main) the resweep fails on the missing relation and the lag is
// UNMEASURABLE; after it, the QA-shaped join measures a finite lag.
//
// Steady-state QA measurement per source starts at T0 once >= 2 real cycles
// per source land and the arbiter reviews; until then no budget is set from
// this proof. This proof covers the stamp, the carry writer on both resweep
// paths, and the cohort rule, not the QA budgets.

// enrichmentCarryLiveT0 is the seeded steady-state cohort start: values
// ingested at or after T0 with a real stamp enter the cohort.
var enrichmentCarryLiveT0 = time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)

const (
	enrichCarryLiveCVE     = "CVE-2026-11466"
	enrichCarryLivePackage = "pkg:npm/enrich-11466"
	enrichCarryLiveRepo    = "repository:enrich-11466"
	enrichCarryLiveScope   = "scope:11466:carry"
	enrichCarryLiveScopeB  = "scope:11466:carry-backfill"
	enrichCarryLiveBackCVE = "CVE-2023-11466"
)

var (
	enrichCarryLiveGen1 = time.Date(2026, time.November, 20, 10, 0, 0, 0, time.UTC)
	enrichCarryLiveGen2 = time.Date(2026, time.November, 21, 10, 0, 0, 0, time.UTC)
	enrichCarryLiveGen3 = time.Date(2026, time.November, 22, 10, 0, 0, 0, time.UTC)
	enrichCarryLiveGen4 = time.Date(2026, time.November, 23, 10, 0, 0, 0, time.UTC)
	enrichCarryLiveGenB = time.Date(2023, time.April, 11, 9, 0, 0, 0, time.UTC)

	enrichCarryLiveScoreD1 = "2026-11-19"
	enrichCarryLiveScoreD2 = "2026-11-21"
	enrichCarryLiveScoreB  = "2023-04-10"
)

// enrichCarryLiveTokens hands out strictly increasing fencing tokens, like
// the production sequence, so later passes are fresher than earlier ones.
var enrichCarryLiveTokens atomic.Int64

func enrichCarryLiveNextToken() int64 { return enrichCarryLiveTokens.Add(1) }

// enrichCarryLiveEnvelopes seeds one owned-package CVE with an EPSS
// observation (score_date selects the rescore universe) and an optional KEV
// listing. Fact IDs carry the generation tag so every generation's collector
// rows are distinct and the QA join resolves the selected generation.
func enrichCarryLiveEnvelopes(cveID, pkg, repo, tag, scoreDate, prob, pct string, withKEV bool) []facts.Envelope {
	envelopes := []facts.Envelope{
		{
			FactID:   "cve-11466-" + tag,
			FactKind: chain.VulnerabilityCVEFactKind,
			Payload: map[string]any{
				"cve_id": cveID, "advisory_id": cveID, "source": "nvd",
				"cvss_score": 7.5, "aliases": []any{cveID},
			},
		},
		{
			FactID:   "affected-11466-" + tag,
			FactKind: chain.VulnerabilityAffectedPackageFactKind,
			Payload: map[string]any{
				"cve_id": cveID, "advisory_id": cveID, "package_id": pkg,
				"ecosystem": "npm", "package_name": "enrich-11466",
				"affected_versions": []any{"1.2.3"}, "fixed_versions": []any{"1.3.0"},
			},
		},
		{
			FactID:   "consume-11466-" + tag,
			FactKind: correlation.PackageConsumptionFactKind,
			Payload: map[string]any{
				"package_id": pkg, "relationship_kind": "consumption",
				"repository_id": repo, "dependency_range": "1.2.3",
				"canonical_writes": 1, "evidence_fact_ids": []any{"manifest-lock-11466"},
			},
		},
		{
			FactID:   "epss-11466-" + tag,
			FactKind: chain.VulnerabilityEPSSScoreFactKind,
			Payload: map[string]any{
				"cve_id": cveID, "probability": prob, "percentile": pct,
				"score_date": scoreDate,
			},
		},
	}
	if withKEV {
		envelopes = append(envelopes, facts.Envelope{
			FactID:   "kev-11466-" + tag,
			FactKind: chain.VulnerabilityKnownExploitedFactKind,
			Payload:  map[string]any{"cve_id": cveID, "date_added": "2026-05-16"},
		})
	}
	return envelopes
}

func mustEnrichCarryLiveFindings(t *testing.T, cveID, pkg, repo, tag, scoreDate, prob, pct string, withKEV bool) []supplychaincore.SupplyChainImpactFinding {
	t.Helper()
	findings := supplychaincore.BuildSupplyChainImpactFindings(
		enrichCarryLiveEnvelopes(cveID, pkg, repo, tag, scoreDate, prob, pct, withKEV))
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	return findings
}

// enrichCarryLiveSeedGeneration seeds one scope generation with a fixed
// ingested_at and makes it the scope's active generation.
func enrichCarryLiveSeedGeneration(t *testing.T, ctx context.Context, db *sql.DB, scope, gen string, ingestedAt time.Time) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic',
          $1, $2, $2, 'active', '{}'::jsonb)
ON CONFLICT (scope_id) DO NOTHING`,
		scope, ingestedAt,
	); err != nil {
		t.Fatalf("insert scope %s: %v", scope, err)
	}
	if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', $3, $3, 'active', $3, '{}'::jsonb)`,
		gen, scope, ingestedAt,
	); err != nil {
		t.Fatalf("insert generation %s: %v", gen, err)
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		gen, scope,
	); err != nil {
		t.Fatalf("activate generation %s: %v", gen, err)
	}
}

// enrichCarryLiveSupersede retires a generation the way a newer collector
// sync leaves it: the winner select reads only active generations.
func enrichCarryLiveSupersede(t *testing.T, ctx context.Context, db *sql.DB, gen string) {
	t.Helper()
	if _, err := db.ExecContext(ctx,
		`UPDATE scope_generations SET status = 'superseded' WHERE generation_id = $1`, gen,
	); err != nil {
		t.Fatalf("supersede generation %s: %v", gen, err)
	}
}

// enrichCarryLiveCollectorPayload is the stored collector fact body behind
// the QA-shaped T_ingest join: only cve_id plus the kind's own fields.
func enrichCarryLiveCollectorPayload(kind, cveID, scoreDate, prob, pct string, withKEV bool) string {
	switch kind {
	case chain.VulnerabilityEPSSScoreFactKind:
		return `{"cve_id": "` + cveID + `", "probability": "` + prob + `", "percentile": "` + pct + `", "score_date": "` + scoreDate + `"}`
	case chain.VulnerabilityKnownExploitedFactKind:
		return `{"cve_id": "` + cveID + `", "date_added": "2026-05-16"}`
	default:
		return `{"cve_id": "` + cveID + `"}`
	}
}

// enrichCarryLiveSeedCollectorFacts stores the collector facts the QA join
// resolves T_ingest through, in the same generation the findings build
// selected them from. In production these rows arrive through the collector;
func enrichCarryLiveSeedCollectorFacts(t *testing.T, ctx context.Context, db *sql.DB, scope, gen, tag, cveID, scoreDate, prob, pct string, withKEV bool) {
	t.Helper()
	kinds := []string{chain.VulnerabilityCVEFactKind, chain.VulnerabilityAffectedPackageFactKind, chain.VulnerabilityEPSSScoreFactKind}
	if withKEV {
		kinds = append(kinds, chain.VulnerabilityKnownExploitedFactKind)
	}
	prefix := map[string]string{
		chain.VulnerabilityCVEFactKind:             "cve-11466-",
		chain.VulnerabilityAffectedPackageFactKind: "affected-11466-",
		chain.VulnerabilityEPSSScoreFactKind:       "epss-11466-",
		chain.VulnerabilityKnownExploitedFactKind:  "kev-11466-",
	}
	for _, kind := range kinds {
		factID := prefix[kind] + tag
		payload := enrichCarryLiveCollectorPayload(kind, cveID, scoreDate, prob, pct, withKEV)
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'synthetic', $1, now(), now(), false, $5::jsonb)`,
			factID, scope, gen, kind, payload,
		); err != nil {
			t.Fatalf("seed collector fact %s: %v", factID, err)
		}
	}
}

func enrichCarryLiveWriter(db *sql.DB, now time.Time) supplychaincore.PostgresSupplyChainImpactWriter {
	return supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		Now: func() time.Time { return now },
	}
}

// enrichCarryLiveSeedDecoys writes five enrichment-free findings in their own
// scope, which never changes afterwards. The winners table then holds enough
// stored rows for a true keyed tick on the target scope later: a keyed tick
// whose identifier set covers more than half the stored winners falls back
// to the full rebuild by design (#9805), which would prove the fallback
// instead of the keyed carry. The decoys are stripped of enrichment after
// the build so their only stamp is the KEV-false episode.
func enrichCarryLiveSeedDecoys(t *testing.T, ctx context.Context, db *sql.DB, scope, gen string, now time.Time) {
	t.Helper()
	var findings []supplychaincore.SupplyChainImpactFinding
	for i := range 5 {
		cveID := "CVE-2026-1147" + string(rune('0'+i))
		built := supplychaincore.BuildSupplyChainImpactFindings(
			enrichCarryLiveEnvelopes(cveID, enrichCarryLivePackage, enrichCarryLiveRepo,
				"decoy", enrichCarryLiveScoreD1, "", "", false))
		for j := range built {
			built[j].EPSSProbability, built[j].EPSSPercentile = "", ""
			built[j].EPSSScoreDate, built[j].EPSSModelVersion = "", ""
			built[j].KnownExploited = false
		}
		findings = append(findings, built...)
	}
	if len(findings) != 5 {
		t.Fatalf("len(decoy findings) = %d, want 5", len(findings))
	}
	enrichCarryLiveWrite(t, ctx, db, scope, gen, now, findings)
}

func enrichCarryLiveWrite(t *testing.T, ctx context.Context, db *sql.DB, scope, gen string, now time.Time, findings []supplychaincore.SupplyChainImpactFinding) {
	t.Helper()
	writer := enrichCarryLiveWriter(db, now)
	write := supplychaincore.SupplyChainImpactWrite{
		FencingToken: enrichCarryLiveNextToken(),
		IntentID:     "intent:11466:enrichment-carry",
		ScopeID:      scope,
		GenerationID: gen,
		SourceSystem: "vulnerability_intelligence",
		Cause:        "synthetic #11466 enrichment-carry proof",
		Findings:     findings,
	}
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
		t.Fatalf("write findings in %s: %v", gen, err)
	}
}

// enrichCarryLiveStampRow reads the durable carry row for one
// (canonical_key, source). It fails on pre-stamp main where the relation
// does not exist: that failure IS the failing-first RED.
func enrichCarryLiveStampRow(t *testing.T, ctx context.Context, db *sql.DB, canonicalKey, source string) (identity string, at time.Time) {
	t.Helper()
	if err := db.QueryRowContext(ctx, `
SELECT value_identity, first_carried_at
FROM supply_chain_impact_winners_enrichment_first_carry
WHERE canonical_key = $1 AND enrichment_source = $2`,
		canonicalKey, source,
	).Scan(&identity, &at); err != nil {
		t.Fatalf("read enrichment first-carry stamp (%s/%s): %v", canonicalKey, source, err)
	}
	return identity, at.UTC()
}

func enrichCarryLiveStampCount(t *testing.T, ctx context.Context, db *sql.DB, canonicalKey string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_winners_enrichment_first_carry WHERE canonical_key = $1`,
		canonicalKey,
	).Scan(&n); err != nil {
		t.Fatalf("count enrichment first-carry stamps: %v", err)
	}
	return n
}

// enrichCarryLiveCanonicalKey reads the winner row's canonical key for one CVE.
func enrichCarryLiveCanonicalKey(t *testing.T, ctx context.Context, db *sql.DB, cveID string) string {
	t.Helper()
	var key string
	if err := db.QueryRowContext(ctx, `
SELECT canonical_key FROM supply_chain_impact_canonical_winners WHERE cve_id = $1`, cveID,
	).Scan(&key); err != nil {
		t.Fatalf("read winner canonical key for %s: %v", cveID, err)
	}
	return key
}

// enrichCarryLiveLag runs the QA-shaped lag join the future QA harness will
// own: stamp first_carried_at minus the ingested_at of the generation on the
// winner's SELECTED enrichment fact (evidence fact id of that kind). The
// subtraction stays in Go so the verdict compares exact instants; the SQL
// only resolves the anchor generation.
func enrichCarryLiveLag(t *testing.T, ctx context.Context, db *sql.DB, canonicalKey, source, factKind string) (string, time.Time, time.Time, time.Duration) {
	t.Helper()
	var identity string
	var carriedAt, ingestedAt time.Time
	if err := db.QueryRowContext(ctx, `
SELECT s.value_identity, s.first_carried_at, g.ingested_at
FROM supply_chain_impact_winners_enrichment_first_carry AS s
JOIN supply_chain_impact_canonical_winners AS w USING (canonical_key)
JOIN LATERAL jsonb_array_elements_text(COALESCE(w.payload->'evidence_fact_ids', '[]'::jsonb)) AS ef(fact_id) ON true
JOIN fact_records AS f ON f.fact_id = ef.fact_id AND f.fact_kind = $3
JOIN scope_generations AS g ON g.generation_id = f.generation_id
WHERE s.canonical_key = $1 AND s.enrichment_source = $2`,
		canonicalKey, source, factKind,
	).Scan(&identity, &carriedAt, &ingestedAt); err != nil {
		t.Fatalf("QA-shaped lag join: %v", err)
	}
	carriedAt, ingestedAt = carriedAt.UTC(), ingestedAt.UTC()
	return identity, carriedAt, ingestedAt, carriedAt.Sub(ingestedAt)
}

func enrichCarryLiveResweepFull(t *testing.T, ctx context.Context, db *sql.DB) time.Duration {
	t.Helper()
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	start := time.Now()
	result, err := store.ResweepWinners(ctx)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("full resweep: %v", err)
	}
	if result.Skipped {
		t.Fatalf("full resweep skipped: %+v", result)
	}
	t.Logf("full resweep: inserted=%d updated=%d deleted=%d unchanged=%d path=%s took=%v",
		result.Inserted, result.Updated, result.Deleted, result.Unchanged, result.Path, took)
	return took
}

func enrichCarryLiveResweepKeyed(t *testing.T, ctx context.Context, db *sql.DB) time.Duration {
	t.Helper()
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	start := time.Now()
	result, err := store.ResweepWinnersKeyed(ctx)
	took := time.Since(start)
	if err != nil {
		t.Fatalf("keyed resweep: %v", err)
	}
	if result.Skipped {
		t.Fatalf("keyed resweep skipped: %+v", result)
	}
	// The ruling requires the carry on both resweep paths: pin the keyed
	// path here (the over-cap fallback would report full instead).
	if result.Path != telemetry.SupplyChainImpactWinnersPathKeyed {
		t.Fatalf("resweep path = %q, want keyed (over-cap fallback would prove the wrong carry)", result.Path)
	}
	t.Logf("keyed resweep: inserted=%d updated=%d deleted=%d unchanged=%d keys=%d path=%s took=%v",
		result.Inserted, result.Updated, result.Deleted, result.Unchanged, result.Keys, result.Path, took)
	return took
}

func TestSupplyChainImpactWinnersEnrichmentCarry11466Live(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#11466 enrichment first-carry proof")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	const tag1, tag2, tag3, tag4, tagB = "g1", "g2", "g3", "g4", "gb"
	genFor := func(scope, tag string) string { return scope + ":" + tag + ":11466-carry-gen" }

	// Pass 1 (case a, lag): gen1 carries EPSS score_date D1 and a KEV
	// listing. The full resweep stamps both values; the QA-shaped join
	// measures a finite lag where none was measurable before the stamp.
	// The decoy scope never changes afterwards; its five stored winners
	// keep pass 3's keyed tick under the over-cap fallback.
	enrichCarryLiveSeedGeneration(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag1), enrichCarryLiveGen1)
	enrichCarryLiveSeedCollectorFacts(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag1),
		tag1, enrichCarryLiveCVE, enrichCarryLiveScoreD1, "0.71", "0.98", true)
	enrichCarryLiveWrite(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag1),
		enrichCarryLiveGen1,
		mustEnrichCarryLiveFindings(t, enrichCarryLiveCVE, enrichCarryLivePackage, enrichCarryLiveRepo,
			tag1, enrichCarryLiveScoreD1, "0.71", "0.98", true))
	const decoyScope, decoyGen = "scope:11466:carry-decoys", "scope:11466:carry-decoys:g1"
	enrichCarryLiveSeedGeneration(t, ctx, db, decoyScope, decoyGen, enrichCarryLiveGen1)
	enrichCarryLiveSeedDecoys(t, ctx, db, decoyScope, decoyGen, enrichCarryLiveGen1)
	tookR1 := enrichCarryLiveResweepFull(t, ctx, db)

	key := enrichCarryLiveCanonicalKey(t, ctx, db, enrichCarryLiveCVE)
	if n := enrichCarryLiveStampCount(t, ctx, db, key); n != 2 {
		t.Fatalf("stamp rows = %d, want 2 (first_epss + cisa_kev)", n)
	}
	epssID, epssAt, epssIngest, epssLag := enrichCarryLiveLag(t, ctx, db, key,
		supplychaincore.EnrichmentSourceFirstEPSS, chain.VulnerabilityEPSSScoreFactKind)
	if epssID != enrichCarryLiveScoreD1 {
		t.Fatalf("stamped EPSS identity = %q, want score_date %s", epssID, enrichCarryLiveScoreD1)
	}
	if !epssIngest.Equal(enrichCarryLiveGen1) {
		t.Fatalf("T_ingest = %v, want gen1 ingested_at %v", epssIngest, enrichCarryLiveGen1)
	}
	if epssLag != epssAt.Sub(enrichCarryLiveGen1) || epssLag < 0 {
		t.Fatalf("lag = %v, want first-carried minus gen1 = %v", epssLag, epssAt.Sub(enrichCarryLiveGen1))
	}
	kevID, kevAt, _, kevLag := enrichCarryLiveLag(t, ctx, db, key,
		supplychaincore.EnrichmentSourceCISAKEV, chain.VulnerabilityKnownExploitedFactKind)
	if kevID != "true" {
		t.Fatalf("stamped KEV identity = %q, want true", kevID)
	}
	t.Logf("pass 1: epss lag=%v kev lag=%v (full resweep %v)", epssLag, kevLag, tookR1)
	verdict := supplychaincore.ClassifyEnrichmentCarrySample(
		supplychaincore.EnrichmentSourceFirstEPSS, epssID, epssAt, epssIngest, enrichmentCarryLiveT0)
	if !verdict.InCohort || verdict.Lag != epssLag {
		t.Fatalf("cohort verdict = %+v, want in-cohort with lag %v", verdict, epssLag)
	}

	// Pass 2 (case b, rewrite-stability): the same findings rewritten with
	// an advanced clock resolve the same values, so the stamps never move.
	enrichCarryLiveWrite(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag1),
		enrichCarryLiveGen1.Add(45*time.Minute),
		mustEnrichCarryLiveFindings(t, enrichCarryLiveCVE, enrichCarryLivePackage, enrichCarryLiveRepo,
			tag1, enrichCarryLiveScoreD1, "0.71", "0.98", true))
	enrichCarryLiveResweepFull(t, ctx, db)
	if id, at := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceFirstEPSS); id != enrichCarryLiveScoreD1 || !at.Equal(epssAt) {
		t.Fatalf("EPSS stamp moved to (%q, %v) on rewrite, want set-once (%q, %v)",
			id, at, enrichCarryLiveScoreD1, epssAt)
	}
	if id, at := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceCISAKEV); id != "true" || !at.Equal(kevAt) {
		t.Fatalf("KEV stamp moved to (%q, %v) on rewrite, want set-once (true, %v)", id, at, kevAt)
	}

	// Pass 3 (case e, same-value re-ingest, keyed path): gen2 re-ingests
	// the identical values. The keyed resweep carries without moving the
	// stamps, and the re-ingest sample is correctly excluded from the
	// arrival cohort: the value did not arrive, it persisted.
	enrichCarryLiveSupersede(t, ctx, db, genFor(enrichCarryLiveScope, tag1))
	enrichCarryLiveSeedGeneration(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag2), enrichCarryLiveGen2)
	enrichCarryLiveSeedCollectorFacts(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag2),
		tag2, enrichCarryLiveCVE, enrichCarryLiveScoreD1, "0.71", "0.98", true)
	enrichCarryLiveWrite(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag2),
		enrichCarryLiveGen2,
		mustEnrichCarryLiveFindings(t, enrichCarryLiveCVE, enrichCarryLivePackage, enrichCarryLiveRepo,
			tag2, enrichCarryLiveScoreD1, "0.71", "0.98", true))
	tookR3 := enrichCarryLiveResweepKeyed(t, ctx, db)
	if id, at := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceFirstEPSS); id != enrichCarryLiveScoreD1 || !at.Equal(epssAt) {
		t.Fatalf("EPSS stamp moved to (%q, %v) on same-value re-ingest, want set-once (%q, %v)",
			id, at, enrichCarryLiveScoreD1, epssAt)
	}
	// The QA-shaped join resolves the re-ingest anchor: the winner now
	// carries gen2's selected fact, so T_ingest moves to gen2 while the
	// stamp still reads the original first carry. Seeded clocks run ahead
	// of every generation here (the resweep stamp S1 is wall-clock now),
	// so the join arithmetic stays positive; in production the re-ingest
	// generation lands after the previous resweep (S1 < T2) and the cohort
	// rule excludes the persisting value as carried-before-ingest -- that
	// production shape is pinned by the classifier unit
	// TestClassifyEnrichmentCarrySample11466, not by this seeded clock.
	_, _, ingest2, lag2 := enrichCarryLiveLag(t, ctx, db, key,
		supplychaincore.EnrichmentSourceFirstEPSS, chain.VulnerabilityEPSSScoreFactKind)
	if !ingest2.Equal(enrichCarryLiveGen2) {
		t.Fatalf("re-ingest T_ingest = %v, want gen2 ingested_at %v", ingest2, enrichCarryLiveGen2)
	}
	if lag2 != epssAt.Sub(enrichCarryLiveGen2) {
		t.Fatalf("re-ingest lag = %v, want original carry minus gen2 = %v", lag2, epssAt.Sub(enrichCarryLiveGen2))
	}
	t.Logf("pass 3: same-value re-ingest carried nothing new (keyed resweep %v)", tookR3)

	// Pass 4 (case a, new value): gen3 carries a newer EPSS score_date.
	// The stamp moves to the new value and the new lag anchors at gen3.
	enrichCarryLiveSupersede(t, ctx, db, genFor(enrichCarryLiveScope, tag2))
	enrichCarryLiveSeedGeneration(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag3), enrichCarryLiveGen3)
	enrichCarryLiveSeedCollectorFacts(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag3),
		tag3, enrichCarryLiveCVE, enrichCarryLiveScoreD2, "0.12", "0.32", true)
	enrichCarryLiveWrite(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag3),
		enrichCarryLiveGen3,
		mustEnrichCarryLiveFindings(t, enrichCarryLiveCVE, enrichCarryLivePackage, enrichCarryLiveRepo,
			tag3, enrichCarryLiveScoreD2, "0.12", "0.32", true))
	enrichCarryLiveResweepFull(t, ctx, db)
	id3, at3 := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceFirstEPSS)
	if id3 != enrichCarryLiveScoreD2 || !at3.After(epssAt) {
		t.Fatalf("EPSS stamp = (%q, %v), want moved to (%q, after %v)", id3, at3, enrichCarryLiveScoreD2, epssAt)
	}
	_, _, ingest3, lag3 := enrichCarryLiveLag(t, ctx, db, key,
		supplychaincore.EnrichmentSourceFirstEPSS, chain.VulnerabilityEPSSScoreFactKind)
	if !ingest3.Equal(enrichCarryLiveGen3) {
		t.Fatalf("T_ingest = %v, want gen3 ingested_at %v", ingest3, enrichCarryLiveGen3)
	}
	if lag3 != at3.Sub(enrichCarryLiveGen3) || lag3 < 0 {
		t.Fatalf("rescore lag = %v, want first-carried minus gen3 = %v", lag3, at3.Sub(enrichCarryLiveGen3))
	}
	if id, at := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceCISAKEV); id != "true" || !at.Equal(kevAt) {
		t.Fatalf("KEV stamp moved to (%q, %v) on EPSS-only rescore, want set-once (true, %v)", id, at, kevAt)
	}
	t.Logf("pass 4: rescore lag=%v (D1 %v -> D2 %v)", lag3, enrichCarryLiveScoreD1, enrichCarryLiveScoreD2)

	// Pass 5 (case d, KEV flip): gen4 drops the KEV listing. The KEV
	// stamp moves to false observably while the EPSS stamp holds, and a
	// same-value rewrite keeps the false stamp set-once.
	enrichCarryLiveSupersede(t, ctx, db, genFor(enrichCarryLiveScope, tag3))
	enrichCarryLiveSeedGeneration(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag4), enrichCarryLiveGen4)
	enrichCarryLiveSeedCollectorFacts(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag4),
		tag4, enrichCarryLiveCVE, enrichCarryLiveScoreD2, "0.12", "0.32", false)
	enrichCarryLiveWrite(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag4),
		enrichCarryLiveGen4,
		mustEnrichCarryLiveFindings(t, enrichCarryLiveCVE, enrichCarryLivePackage, enrichCarryLiveRepo,
			tag4, enrichCarryLiveScoreD2, "0.12", "0.32", false))
	enrichCarryLiveResweepFull(t, ctx, db)
	kevID4, kevAt4 := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceCISAKEV)
	if kevID4 != "false" || !kevAt4.After(kevAt) {
		t.Fatalf("KEV stamp = (%q, %v), want flipped to (false, after %v)", kevID4, kevAt4, kevAt)
	}
	if id, at := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceFirstEPSS); id != enrichCarryLiveScoreD2 || !at.Equal(at3) {
		t.Fatalf("EPSS stamp moved to (%q, %v) on KEV-only flip, want set-once (%q, %v)",
			id, at, enrichCarryLiveScoreD2, at3)
	}
	enrichCarryLiveWrite(t, ctx, db, enrichCarryLiveScope, genFor(enrichCarryLiveScope, tag4),
		enrichCarryLiveGen4.Add(time.Hour),
		mustEnrichCarryLiveFindings(t, enrichCarryLiveCVE, enrichCarryLivePackage, enrichCarryLiveRepo,
			tag4, enrichCarryLiveScoreD2, "0.12", "0.32", false))
	enrichCarryLiveResweepFull(t, ctx, db)
	if id, at := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceCISAKEV); id != "false" || !at.Equal(kevAt4) {
		t.Fatalf("KEV stamp moved to (%q, %v) on false rewrite, want set-once (false, %v)", id, at, kevAt4)
	}

	// Pass 6 (case c, backfill exclusion): a historical CVE is stamped by
	// the mechanism like any other value, but the cohort rule excludes it:
	// it measures fleet age, not arrival-to-rescore.
	enrichCarryLiveSeedGeneration(t, ctx, db, enrichCarryLiveScopeB, genFor(enrichCarryLiveScopeB, tagB), enrichCarryLiveGenB)
	enrichCarryLiveSeedCollectorFacts(t, ctx, db, enrichCarryLiveScopeB, genFor(enrichCarryLiveScopeB, tagB),
		tagB, enrichCarryLiveBackCVE, enrichCarryLiveScoreB, "0.44", "0.61", true)
	enrichCarryLiveWrite(t, ctx, db, enrichCarryLiveScopeB, genFor(enrichCarryLiveScopeB, tagB),
		enrichCarryLiveGenB,
		mustEnrichCarryLiveFindings(t, enrichCarryLiveBackCVE, enrichCarryLivePackage, enrichCarryLiveRepo,
			tagB, enrichCarryLiveScoreB, "0.44", "0.61", true))
	enrichCarryLiveResweepFull(t, ctx, db)
	backKey := enrichCarryLiveCanonicalKey(t, ctx, db, enrichCarryLiveBackCVE)
	backID, backAt := enrichCarryLiveStampRow(t, ctx, db, backKey, supplychaincore.EnrichmentSourceFirstEPSS)
	if backID != enrichCarryLiveScoreB {
		t.Fatalf("backfill stamp identity = %q, want %s", backID, enrichCarryLiveScoreB)
	}
	backVerdict := supplychaincore.ClassifyEnrichmentCarrySample(
		supplychaincore.EnrichmentSourceFirstEPSS, backID, backAt, enrichCarryLiveGenB, enrichmentCarryLiveT0)
	if backVerdict.InCohort || backVerdict.ExclusionReason != supplychaincore.EnrichmentCohortExclusionBackfill {
		t.Fatalf("backfill verdict = %+v, want backfill exclusion from the steady-state cohort", backVerdict)
	}
	t.Logf("pass 6: backfilled %s stamped at %v but excluded: reason=%q",
		enrichCarryLiveBackCVE, backAt, backVerdict.ExclusionReason)
}

func TestSupplyChainImpactWinnersEnrichmentCarryConcurrent11466Live(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#11466 enrichment first-carry concurrency proof")
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	t.Cleanup(func() { _ = db.Close() })

	const scope, tag = "scope:11466:carry-race", "g1"
	gen := scope + ":11466-carry-gen"
	enrichCarryLiveSeedGeneration(t, ctx, db, scope, gen, enrichCarryLiveGen1)
	enrichCarryLiveSeedCollectorFacts(t, ctx, db, scope, gen, tag,
		enrichCarryLiveCVE, enrichCarryLiveScoreD1, "0.71", "0.98", true)
	enrichCarryLiveWrite(t, ctx, db, scope, gen, enrichCarryLiveGen1,
		mustEnrichCarryLiveFindings(t, enrichCarryLiveCVE, enrichCarryLivePackage, enrichCarryLiveRepo,
			tag, enrichCarryLiveScoreD1, "0.71", "0.98", true))

	// Case f: N concurrent full resweeps race the single-flight lock. Each
	// reports success or a clean skip; the carry claims collapse on the
	// primary key so exactly one stamp row per source survives, and every
	// claimant resolved to it (no stamp moves under concurrency).
	store := postgres.NewSupplyChainImpactWinnersStore(postgres.SQLDB{DB: db})
	release := make(chan struct{})
	var wg sync.WaitGroup
	type outcome struct {
		result supplychaincore.SupplyChainImpactWinnersResweep
		err    error
	}
	outcomes := make([]outcome, 4)
	for i := range outcomes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-release
			result, err := store.ResweepWinners(ctx)
			outcomes[i] = outcome{result: result, err: err}
		}(i)
	}
	close(release)
	wg.Wait()
	ran := 0
	for i, o := range outcomes {
		if o.err != nil {
			t.Fatalf("concurrent resweep %d: %v", i, o.err)
		}
		if !o.result.Skipped {
			ran++
		}
	}
	if ran == 0 {
		t.Fatalf("all %d concurrent resweeps skipped, want at least one to reconcile", len(outcomes))
	}
	key := enrichCarryLiveCanonicalKey(t, ctx, db, enrichCarryLiveCVE)
	if n := enrichCarryLiveStampCount(t, ctx, db, key); n != 2 {
		t.Fatalf("concurrent stamp rows = %d, want exactly 2 (one per source)", n)
	}
	epssID, epssAt := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceFirstEPSS)
	kevID, kevAt := enrichCarryLiveStampRow(t, ctx, db, key, supplychaincore.EnrichmentSourceCISAKEV)
	if epssID != enrichCarryLiveScoreD1 || kevID != "true" {
		t.Fatalf("concurrent stamps = (%q, %q), want (%q, true)", epssID, kevID, enrichCarryLiveScoreD1)
	}
	t.Logf("concurrent: %d ran / %d skipped, stamps stable at epss=%v kev=%v",
		ran, len(outcomes)-ran, epssAt, kevAt)
}
