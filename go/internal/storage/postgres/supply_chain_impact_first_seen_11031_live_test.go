// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11031 disposable PostgreSQL 18 proof of the durable set-once finding first-seen stamp: a seeded NVD advisory with a known published_at drives the real finding build and the production writer, the publish-to-finding lag equals first-seen minus publish at the pinned write time, a rewrite with an advanced clock keeps the original stamp, a backfilled historical advisory is stamped but excluded from the steady-state cohort, and concurrent first claims of one key leave exactly one stamp row; enrolled in the live-postgres-readiness runner; wall time is logged, not gated
//eshu:readiness:env ESHU_POSTGRES_TEST_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DSN ESHU_SUPPLY_CHAIN_IMPACT_PROOF_DISPOSABLE

import (
	"context"
	"database/sql"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	"github.com/eshu-hq/eshu/go/internal/reducer/packages/correlation"
)

// Failing-first proof for eshu-hq/eshu#11031 criterion 6 (arbiter fork
// ruling): the durable finding first-seen stamp, measured end to end.
//
// A seeded advisory with a known published_at drives the real finding build
// (supplychaincore.BuildSupplyChainImpactFindings) and the real production
// writer (PostgresSupplyChainImpactWriter) on a disposable database. The
// test asserts the publish-to-finding lag equals first-seen minus publish
// within the seeded bound, the stamp never moves on rewrite, a backfilled
// historical advisory is excluded from the steady-state cohort, and
// concurrent first claims of one key leave exactly one stamp row.
//
// Steady-state QA measurement per source starts at T0 once #9432 retires or
// its full-rotation census passes; until then osv measurement is observe-only
// and no budget is set from it. This proof covers the stamp and the cohort
// rule, not the QA budgets.

// firstSeenLiveCVE is the seeded advisory: NVD-published at a known instant,
// last-modified a day later, so the published-vs-lastModified differential is
// observable in the lag.
const (
	firstSeenLiveCVE       = "CVE-2026-11031"
	firstSeenLivePublished = "2026-11-20T10:00:00Z"
	firstSeenLiveModified  = "2026-11-21T15:30:00Z"
	firstSeenLivePackage   = "pkg:npm/example"
	firstSeenLiveRepo      = "repository:first-seen-11031"
	firstSeenLiveScopeA    = "scope:11031:first-seen-a"
	firstSeenLiveScopeB    = "scope:11031:first-seen-b"
	firstSeenLiveScopeC    = "scope:11031:first-seen-c"
	firstSeenLiveGenSuffix = "11031-first-seen-gen"
)

// firstSeenLiveT0 is the seeded steady-state cohort start: advisories
// published at or after T0 with a real stamp enter the cohort.
var firstSeenLiveT0 = time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)

// firstSeenLiveTokens hands out strictly increasing fencing tokens, like the
// production sequence, so later passes are fresher than earlier ones.
var firstSeenLiveTokens atomic.Int64

func firstSeenLiveNextToken() int64 { return firstSeenLiveTokens.Add(1) }

func firstSeenLiveWriter(db *sql.DB, now time.Time) supplychaincore.PostgresSupplyChainImpactWriter {
	return supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  postgres.SupplyChainImpactBeginner{Beginner: postgres.SQLDB{DB: db}},
		Now: func() time.Time { return now },
	}
}

// firstSeenLiveEnvelopes seeds one owned-package CVE with a known NVD
// publish instant, a later lastModified, and an explicit source, so the
// build must attribute the publish instant to NVD.
func firstSeenLiveEnvelopes(cveID, publishedAt, modifiedAt string) []facts.Envelope {
	return []facts.Envelope{
		{
			FactID:   "cve-first-seen-" + cveID,
			FactKind: chain.VulnerabilityCVEFactKind,
			Payload: map[string]any{
				"cve_id":       cveID,
				"advisory_id":  cveID,
				"source":       "nvd",
				"cvss_score":   7.5,
				"published_at": publishedAt,
				"modified_at":  modifiedAt,
			},
		},
		{
			FactID:   "affected-first-seen-" + cveID,
			FactKind: chain.VulnerabilityAffectedPackageFactKind,
			Payload: map[string]any{
				"cve_id":            cveID,
				"advisory_id":       cveID,
				"package_id":        firstSeenLivePackage,
				"ecosystem":         "npm",
				"package_name":      "example",
				"affected_versions": []any{"1.2.3"},
				"fixed_versions":    []any{"1.3.0"},
			},
		},
		{
			FactID:   "consume-first-seen-" + cveID,
			FactKind: correlation.PackageConsumptionFactKind,
			Payload: map[string]any{
				"package_id":        firstSeenLivePackage,
				"relationship_kind": "consumption",
				"repository_id":     firstSeenLiveRepo,
				"dependency_range":  "1.2.3",
				"canonical_writes":  1,
				"evidence_fact_ids": []any{"manifest-lock-11031"},
			},
		},
	}
}

func mustFirstSeenLiveFindings(t *testing.T, cveID, publishedAt, modifiedAt string) []supplychaincore.SupplyChainImpactFinding {
	t.Helper()
	findings := supplychaincore.BuildSupplyChainImpactFindings(firstSeenLiveEnvelopes(cveID, publishedAt, modifiedAt))
	if len(findings) != 1 {
		t.Fatalf("len(findings) = %d, want 1: %#v", len(findings), findings)
	}
	return findings
}

// replaceSetSeedScopeForFirstSeen seeds one scope and its active generation
// the way the replace-set proof does, parameterized for this proof's scopes.
// firstSeenLiveGenFor names the generation per scope: generation ids are
// globally unique, so sibling scopes cannot share one.
func firstSeenLiveGenFor(scope string) string { return scope + ":" + firstSeenLiveGenSuffix }

func replaceSetSeedScopeForFirstSeen(t *testing.T, ctx context.Context, db *sql.DB, scope string) {
	t.Helper()
	now := time.Date(2026, 11, 24, 7, 0, 0, 0, time.UTC)
	if _, err := db.ExecContext(ctx, `
INSERT INTO ingestion_scopes (
  scope_id, scope_kind, source_system, source_key, collector_kind,
  partition_key, observed_at, ingested_at, status, payload
) VALUES ($1, 'vulnerability_intelligence', 'synthetic', $1, 'synthetic',
          $1, $2, $2, 'active', '{}'::jsonb)`,
		scope, now,
	); err != nil {
		t.Fatalf("insert scope: %v", err)
	}
	for _, generation := range []struct{ id, status string }{
		{firstSeenLiveGenFor(scope), "active"},
	} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO scope_generations (
  generation_id, scope_id, trigger_kind, observed_at, ingested_at,
  status, activated_at, payload
) VALUES ($1, $2, 'synthetic', $3, $3, $4, $3, '{}'::jsonb)`,
			generation.id, scope, now, generation.status,
		); err != nil {
			t.Fatalf("insert generation %s: %v", generation.id, err)
		}
	}
	if _, err := db.ExecContext(ctx,
		`UPDATE ingestion_scopes SET active_generation_id = $1 WHERE scope_id = $2`,
		firstSeenLiveGenFor(scope), scope,
	); err != nil {
		t.Fatalf("activate generation: %v", err)
	}
}

// firstSeenLivePayload reads one live finding payload back from fact_records.
func firstSeenLivePayload(t *testing.T, ctx context.Context, db *sql.DB, scopeID, genID, cveID string) (stableKey string, provenance map[string]any) {
	t.Helper()
	var payloadText string
	if err := db.QueryRowContext(ctx, `
SELECT stable_fact_key, payload::text FROM fact_records
WHERE fact_kind = 'reducer_supply_chain_impact_finding'
  AND scope_id = $1 AND generation_id = $2
  AND payload->>'cve_id' = $3 AND is_tombstone = FALSE`,
		scopeID, genID, cveID).Scan(&stableKey, &payloadText); err != nil {
		t.Fatalf("read live finding payload: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
		t.Fatalf("unmarshal live finding payload: %v", err)
	}
	raw, ok := payload["provenance"].(map[string]any)
	if !ok {
		t.Fatalf("live finding payload has no provenance map: %s", payloadText)
	}
	return stableKey, raw
}

// firstSeenLiveStampRow reads the durable stamp-table row for one stable key.
func firstSeenLiveStampRow(t *testing.T, ctx context.Context, db *sql.DB, stableKey string) (time.Time, string) {
	t.Helper()
	var at time.Time
	var source string
	if err := db.QueryRowContext(ctx, `
SELECT first_seen_at, publish_source FROM supply_chain_impact_finding_first_seen
WHERE stable_fact_key = $1`, stableKey).Scan(&at, &source); err != nil {
		t.Fatalf("read first-seen stamp row: %v", err)
	}
	return at, source
}

func firstSeenLiveStampCount(t *testing.T, ctx context.Context, db *sql.DB, stableKey string) int {
	t.Helper()
	var n int
	if err := db.QueryRowContext(ctx, `
SELECT count(*) FROM supply_chain_impact_finding_first_seen WHERE stable_fact_key = $1`, stableKey).Scan(&n); err != nil {
		t.Fatalf("count first-seen stamp rows: %v", err)
	}
	return n
}

func firstSeenLiveWrite(t *testing.T, ctx context.Context, db *sql.DB, scopeID string, now time.Time, findings []supplychaincore.SupplyChainImpactFinding) {
	genID := firstSeenLiveGenFor(scopeID)
	t.Helper()
	writer := firstSeenLiveWriter(db, now)
	write := supplychaincore.SupplyChainImpactWrite{
		FencingToken: firstSeenLiveNextToken(),
		IntentID:     "intent:11031:first-seen",
		ScopeID:      scopeID,
		GenerationID: genID,
		SourceSystem: "vulnerability_intelligence",
		Cause:        "synthetic #11031 first-seen proof",
		Findings:     findings,
	}
	if _, err := writer.WriteSupplyChainImpactFindings(ctx, write); err != nil {
		t.Fatalf("write findings: %v", err)
	}
}

func TestSupplyChainImpactFirstSeenStampLive11031(t *testing.T) {
	_ = supplyChainImpactLiveDSN(t, "#11031 first-seen stamp proof")
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	t.Cleanup(cancel)
	db := openAdvisorySuppressionIsolatedDB(t)
	t.Cleanup(func() { _ = db.Close() })
	for _, scope := range []string{firstSeenLiveScopeA, firstSeenLiveScopeB, firstSeenLiveScopeC} {
		replaceSetSeedScopeForFirstSeen(t, ctx, db, scope)
	}

	published, err := time.Parse(time.RFC3339, firstSeenLivePublished)
	if err != nil {
		t.Fatalf("parse seeded published_at: %v", err)
	}

	// Pass 1: the seeded advisory drives the real build and the real writer.
	// The lag equals first-seen minus publish within the seeded bound, and
	// the publish instant is credited to NVD.
	firstWrite := time.Date(2026, time.November, 24, 8, 0, 0, 0, time.UTC)
	firstSeenLiveWrite(t, ctx, db, firstSeenLiveScopeA, firstWrite,
		mustFirstSeenLiveFindings(t, firstSeenLiveCVE, firstSeenLivePublished, firstSeenLiveModified))

	stableKey, provenance := firstSeenLivePayload(t, ctx, db, firstSeenLiveScopeA, firstSeenLiveGenFor(firstSeenLiveScopeA), firstSeenLiveCVE)
	rawStamp, ok := provenance["first_seen_at"].(string)
	if !ok {
		t.Fatalf("provenance has no first_seen_at string: %#v", provenance)
	}
	stamp, err := time.Parse(time.RFC3339Nano, rawStamp)
	if err != nil {
		t.Fatalf("parse first_seen_at %q: %v", rawStamp, err)
	}
	if !stamp.Equal(firstWrite) {
		t.Fatalf("first_seen_at = %v, want write-time %v", stamp, firstWrite)
	}
	if got, _ := provenance["selected_publish_source"].(string); got != "nvd" {
		t.Fatalf("selected_publish_source = %q, want nvd", got)
	}
	tableStamp, tableSource := firstSeenLiveStampRow(t, ctx, db, stableKey)
	if !tableStamp.Equal(firstWrite) || tableSource != "nvd" {
		t.Fatalf("stamp table = (%v, %q), want (%v, nvd)", tableStamp, tableSource, firstWrite)
	}
	lag := stamp.Sub(published)
	if want := firstWrite.Sub(published); lag != want {
		t.Fatalf("lag = %v, want first-seen minus publish = %v", lag, want)
	}
	t.Logf("lag = %v for %s (publish %v, first-seen %v)", lag, firstSeenLiveCVE, published, stamp)

	verdict := supplychaincore.ClassifyPublishCohortSample(firstSeenLivePublished, stamp, firstSeenLiveT0)
	if !verdict.InCohort || verdict.Lag != lag {
		t.Fatalf("steady-state verdict = %+v, want in-cohort with lag %v", verdict, lag)
	}

	// Pass 2 (rewrite with the clock advanced): the durable stamp never
	// moves, so the lag is stable across rewrites.
	secondWrite := firstWrite.Add(45 * time.Minute)
	firstSeenLiveWrite(t, ctx, db, firstSeenLiveScopeA, secondWrite,
		mustFirstSeenLiveFindings(t, firstSeenLiveCVE, firstSeenLivePublished, firstSeenLiveModified))
	_, provenance2 := firstSeenLivePayload(t, ctx, db, firstSeenLiveScopeA, firstSeenLiveGenFor(firstSeenLiveScopeA), firstSeenLiveCVE)
	rawStamp2, ok := provenance2["first_seen_at"].(string)
	if !ok {
		t.Fatalf("rewrite provenance has no first_seen_at: %#v", provenance2)
	}
	stamp2, err := time.Parse(time.RFC3339Nano, rawStamp2)
	if err != nil {
		t.Fatalf("parse rewritten first_seen_at %q: %v", rawStamp2, err)
	}
	if !stamp2.Equal(firstWrite) {
		t.Fatalf("stamp moved to %v on rewrite at %v, want set-once %v", stamp2, secondWrite, firstWrite)
	}
	if n := firstSeenLiveStampCount(t, ctx, db, stableKey); n != 1 {
		t.Fatalf("stamp rows for key = %d, want exactly 1", n)
	}

	// Seeded-violation pair: a backfilled historical advisory is stamped
	// (the write path treats every identity alike) but excluded from the
	// steady-state cohort: it measures fleet age, not publish-to-finding.
	const backfillCVE = "CVE-2023-11031"
	firstSeenLiveWrite(t, ctx, db, firstSeenLiveScopeA, secondWrite,
		mustFirstSeenLiveFindings(t, backfillCVE, "2023-04-11T09:00:00Z", "2023-04-12T09:00:00Z"))
	backfillKey, _ := firstSeenLivePayload(t, ctx, db, firstSeenLiveScopeA, firstSeenLiveGenFor(firstSeenLiveScopeA), backfillCVE)
	backfillStamp, _ := firstSeenLiveStampRow(t, ctx, db, backfillKey)
	backfillVerdict := supplychaincore.ClassifyPublishCohortSample("2023-04-11T09:00:00Z", backfillStamp, firstSeenLiveT0)
	if backfillVerdict.InCohort {
		t.Fatalf("backfilled advisory verdict = %+v, want excluded from the steady-state cohort", backfillVerdict)
	}
	if backfillVerdict.ExclusionReason != supplychaincore.PublishCohortExclusionBackfill {
		t.Fatalf("backfill exclusion reason = %q, want backfill", backfillVerdict.ExclusionReason)
	}
	t.Logf("backfilled %s excluded: reason=%q", backfillCVE, backfillVerdict.ExclusionReason)

	// Concurrent first claims of one fresh key from two scopes leave exactly
	// one stamp row, and both payload stamps equal the table stamp: the
	// set-once write is idempotent under concurrency.
	const raceCVE = "CVE-2026-11032"
	raceWrite := secondWrite.Add(time.Hour)
	// Built once on the test goroutine: testing.Fatalf stays legal, and both
	// racers claim the identical logical finding.
	raceFindings := mustFirstSeenLiveFindings(t, raceCVE, firstSeenLivePublished, firstSeenLiveModified)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, scope := range []string{firstSeenLiveScopeB, firstSeenLiveScopeC} {
		wg.Add(1)
		go func(i int, scope string) {
			defer wg.Done()
			writer := firstSeenLiveWriter(db, raceWrite)
			write := supplychaincore.SupplyChainImpactWrite{
				FencingToken: firstSeenLiveNextToken(),
				IntentID:     "intent:11031:first-seen-race",
				ScopeID:      scope,
				GenerationID: firstSeenLiveGenFor(scope),
				SourceSystem: "vulnerability_intelligence",
				Cause:        "synthetic #11031 first-seen race proof",
				Findings:     raceFindings,
			}
			_, errs[i] = writer.WriteSupplyChainImpactFindings(ctx, write)
		}(i, scope)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent write %d: %v", i, err)
		}
	}
	raceKey, _ := firstSeenLivePayload(t, ctx, db, firstSeenLiveScopeB, firstSeenLiveGenFor(firstSeenLiveScopeB), raceCVE)
	if n := firstSeenLiveStampCount(t, ctx, db, raceKey); n != 1 {
		t.Fatalf("concurrent stamp rows for key = %d, want exactly 1", n)
	}
	raceTableStamp, _ := firstSeenLiveStampRow(t, ctx, db, raceKey)
	for _, scope := range []string{firstSeenLiveScopeB, firstSeenLiveScopeC} {
		_, prov := firstSeenLivePayload(t, ctx, db, scope, firstSeenLiveGenFor(scope), raceCVE)
		raw, ok := prov["first_seen_at"].(string)
		if !ok {
			t.Fatalf("scope %s provenance has no first_seen_at: %#v", scope, prov)
		}
		got, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			t.Fatalf("parse scope %s first_seen_at: %v", scope, err)
		}
		if !got.Equal(raceTableStamp) {
			t.Fatalf("scope %s stamp %v != table stamp %v", scope, got, raceTableStamp)
		}
	}
}
