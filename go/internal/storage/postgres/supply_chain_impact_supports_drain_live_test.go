// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #11198 and #11199 hot-key page contract on real PostgreSQL: the supports drain returns exactly the rows the frozen finding-fact read returns, in the database's fact_id order, on adversarial seeds (two full pages of superseded-generation rows in the middle of a key and at its end, raw counts of exactly one page and one page plus one row, rows that match two, three and four arms, more than one page of rows that each match all four arms, ids that sort differently under the C collation), and two seeded-wrong readers (stopping on a short active page, advancing on the last active id) fail the same seeds
//eshu:readiness:env ESHU_POSTGRES_DSN ESHU_POSTGRES_DISPOSABLE

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// drainSeedGroup is one adversarial key of the hot-key equality seed.
type drainSeedGroup struct {
	prefix     string
	count      int
	scope      string
	generation string
	repository string
	pkg        string
	cve        string
	advisory   string
	tombstone  bool
}

// drainInsertGroup writes count impact finding facts straight into fact_records,
// ids prefix + a five-digit sequence, so the id order is the insertion order
// within a prefix and the prefixes interleave a key's active and superseded rows.
func drainInsertGroup(t *testing.T, ctx context.Context, db *sql.DB, group drainSeedGroup) {
	t.Helper()
	if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $1 || lpad(i::text, 5, '0'), $2, $3, $4, $1 || lpad(i::text, 5, '0'), '1.0.0', 'reducer',
       1, 'exact', 'vulnerability_intelligence', $1 || lpad(i::text, 5, '0'), now(), now(), $5,
       jsonb_build_object('repository_id', $6::text, 'package_id', $7::text, 'cve_id', $8::text,
                          'advisory_id', $9::text, 'impact_status', 'affected_exact', 'group', $1::text)
FROM generate_series(1, $10::int) AS i`,
		group.prefix, group.scope, group.generation, facts.ReducerSupplyChainImpactFindingFactKind, group.tombstone,
		group.repository, group.pkg, group.cve, group.advisory, group.count); err != nil {
		t.Fatalf("seed group %s: %v", group.prefix, err)
	}
}

// drainSeed plants every adversarial key. Scope A holds the active generation,
// scope B an active generation and a superseded one (b2cGenBOld) whose findings
// must never be read.
func drainSeed(t *testing.T, ctx context.Context, db *sql.DB) {
	t.Helper()
	b2cSeedScopes(t, ctx, db)
	page := listFactsByKindPageSize
	groups := []drainSeedGroup{
		// Two full pages of superseded rows in the middle of one key: the raw
		// pages 2 and 3 hold no active row, and the key continues after them.
		{prefix: "g1a-", count: page, scope: b2cScopeA, generation: b2cGenA, pkg: "pkg:g1"},
		{prefix: "g1b-", count: 2 * page, scope: b2cScopeB, generation: b2cGenBOld, pkg: "pkg:g1"},
		{prefix: "g1c-", count: page, scope: b2cScopeA, generation: b2cGenA, pkg: "pkg:g1"},
		// Two full pages of superseded rows at the end of a key.
		{prefix: "g2a-", count: page, scope: b2cScopeA, generation: b2cGenA, pkg: "pkg:g2"},
		{prefix: "g2b-", count: 2 * page, scope: b2cScopeB, generation: b2cGenBOld, pkg: "pkg:g2"},
		// Raw counts of exactly one page and one page plus one row.
		{prefix: "g3-", count: page, scope: b2cScopeA, generation: b2cGenA, pkg: "pkg:g3"},
		{prefix: "g4-", count: page + 1, scope: b2cScopeA, generation: b2cGenA, pkg: "pkg:g4"},
		// Rows that match two, three and four arms, and more than one page of rows
		// that each match all four arms.
		{prefix: "m2-", count: 1, scope: b2cScopeC, generation: b2cGenC, repository: "repo:m", pkg: "pkg:m"},
		{prefix: "m3-", count: 1, scope: b2cScopeC, generation: b2cGenC, repository: "repo:m", pkg: "pkg:m", cve: "CVE-M"},
		{prefix: "m4-", count: page + 100, scope: b2cScopeC, generation: b2cGenC, repository: "repo:m", pkg: "pkg:m", cve: "CVE-M", advisory: "GHSA-M"},
		// A tombstoned row never reaches the supports table.
		{prefix: "tomb-", count: 3, scope: b2cScopeA, generation: b2cGenA, pkg: "pkg:g3", tombstone: true},
	}
	for _, group := range groups {
		drainInsertGroup(t, ctx, db, group)
	}
	// Ids that sort differently under the C collation than under a locale one.
	for i, id := range []string{"Case-B", "case-a", "CASE-c", "_case-d", "-case-e", "case_f", "Case-Z"} {
		if _, err := db.ExecContext(ctx, `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version, collector_kind,
  fencing_token, source_confidence, source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
VALUES ($1, $2, $3, $4, $1, '1.0.0', 'reducer', 1, 'exact', 'vulnerability_intelligence', $1, now(), now(), FALSE,
        jsonb_build_object('repository_id', '', 'package_id', 'pkg:case', 'cve_id', '', 'advisory_id', '', 'seq', $5::int))`,
			id, b2cScopeA, b2cGenA, facts.ReducerSupplyChainImpactFindingFactKind, i); err != nil {
			t.Fatalf("seed collation id %q: %v", id, err)
		}
	}
	b2cMakeReady(t, ctx, db)
	// Non-vacuity: the supports of the superseded generation exist (the backfill
	// keeps them, as the table does until the generation is deleted), so the raw
	// pages of the seed really hold inactive rows.
	var superseded int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM supply_chain_impact_finding_supports WHERE generation_id = $1`, b2cGenBOld).Scan(&superseded); err != nil {
		t.Fatalf("count superseded supports: %v", err)
	}
	if superseded != 4*page {
		t.Fatalf("supports of the superseded generation = %d, want %d: the seed would not exercise inactive raw pages", superseded, 4*page)
	}
}

// drainOracle returns the active impact fact rows the frozen finding-fact read
// returns for a filter, in the database's fact_id order, as "id|payload" text.
func drainOracle(t *testing.T, ctx context.Context, db *sql.DB, repos, pkgs, cves, advisories []string) []string {
	t.Helper()
	rows := b2cOracleRows(t, ctx, db, `
SELECT fact.fact_id, fact.payload
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'reducer_supply_chain_impact_finding'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
  AND (
      fact.payload->>'repository_id' = ANY($1::text[])
      OR fact.payload->>'package_id' = ANY($2::text[])
      OR fact.payload->>'cve_id' = ANY($3::text[])
      OR fact.payload->>'advisory_id' = ANY($4::text[])
  )
ORDER BY fact.fact_id ASC`, repos, pkgs, cves, advisories)
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.FactID+"|"+b2cPayloadText(t, row.Payload))
	}
	return out
}

// drainRows renders drained envelopes as "id|payload" text.
func drainRows(t *testing.T, envelopes []facts.Envelope) []string {
	t.Helper()
	out := make([]string, 0, len(envelopes))
	for _, envelope := range envelopes {
		out = append(out, envelope.FactID+"|"+b2cNormalize(t, envelope.Payload))
	}
	return out
}

// drainWrongReader is a deliberately wrong drain loop, the reader bugs the
// ruling names. stopOnShortActive stops when a page holds fewer than a page of
// ACTIVE rows; advanceOnActive moves the cursor to the last ACTIVE id.
func drainWrongReader(
	ctx context.Context, d supplyChainImpactSupportsDrain, db *sql.DB, stopOnShortActive, advanceOnActive bool,
) ([]string, error) {
	var out []string
	cursor := ""
	for iteration := 0; iteration < 50; iteration++ {
		query, args := d.firstQuery, d.keyArgs
		if cursor != "" {
			query, args = d.nextQuery, append(append([]any{}, d.keyArgs...), cursor)
		}
		raw, lastRaw, active, err := d.page(ctx, SQLDB{DB: db}, query, args)
		if err != nil {
			return nil, err
		}
		for _, envelope := range active {
			out = append(out, envelope.FactID)
		}
		if raw < listFactsByKindPageSize || (stopOnShortActive && len(active) < listFactsByKindPageSize) {
			return out, nil
		}
		cursor = lastRaw
		if advanceOnActive {
			if len(active) == 0 {
				return out, nil
			}
			cursor = active[len(active)-1].FactID
		}
	}
	return out, nil
}

// TestSupplyChainImpactSupportsDrainEqualsFactReadOnAdversarialSeedsLive is the
// hot-key equality proof (ruling arbiter-hotkey-page.md, proof 4): for every
// adversarial key the drain returns the same ids, payloads and order as the
// frozen finding-fact read, once per row, and two readers with the named bugs do
// not.
func TestSupplyChainImpactSupportsDrainEqualsFactReadOnAdversarialSeedsLive(t *testing.T) {
	db, ctx := openIsolatedLiveDB(t, "b2c_drain_equality",
		workflowControlIntegrationDSNEnv+" is not set; skipping Postgres integration test")
	drainSeed(t, ctx, db)

	type shape struct {
		name                   string
		repos, pkgs, cves, adv []string
	}
	shapes := []shape{
		{name: "superseded pages in the middle of a key", pkgs: []string{"pkg:g1"}},
		{name: "superseded pages at the end of a key", pkgs: []string{"pkg:g2"}},
		{name: "exactly one page", pkgs: []string{"pkg:g3"}},
		{name: "one page plus one row", pkgs: []string{"pkg:g4"}},
		{name: "one row matching two arms", repos: []string{"repo:m"}, pkgs: []string{"pkg:m"}},
		{name: "all four arms over more than one page", repos: []string{"repo:m"}, pkgs: []string{"pkg:m"}, cves: []string{"CVE-M"}, adv: []string{"GHSA-M"}},
		{name: "cve arm alone", cves: []string{"CVE-M"}},
		{name: "ids that sort differently under C", pkgs: []string{"pkg:case"}},
		{name: "every key at once", repos: []string{"repo:m"}, pkgs: []string{"pkg:g1", "pkg:g2", "pkg:g3", "pkg:g4", "pkg:m", "pkg:case"}, cves: []string{"CVE-M"}, adv: []string{"GHSA-M"}},
		{name: "unmatched", pkgs: []string{"pkg:nope"}},
	}
	for _, tc := range shapes {
		t.Run(tc.name, func(t *testing.T) {
			want := drainOracle(t, ctx, db, tc.repos, tc.pkgs, tc.cves, tc.adv)
			drain := newSupplyChainImpactSupportsDrain(nil, "drain_equality", securityAlertReconciliationSupportsArms,
				tc.repos, tc.pkgs, tc.cves, tc.adv)
			got, err := drain.run(ctx, SQLDB{DB: db})
			if err != nil {
				t.Fatalf("drain error = %v, want nil", err)
			}
			if gotRows := drainRows(t, got); !reflect.DeepEqual(gotRows, want) {
				t.Fatalf("drain returned %d rows, the fact read %d; first difference at %s", len(gotRows), len(want), firstDifference(gotRows, want))
			}
		})
	}

	// Non-vacuity and the seeded-wrong readers. The middle-superseded key holds
	// 2,000 active rows behind 1,000 superseded ones, so a reader that stops on
	// a short ACTIVE page, or advances on the last ACTIVE id, returns fewer rows.
	middle := newSupplyChainImpactSupportsDrain(nil, "drain_equality", securityAlertReconciliationSupportsArms,
		[]string(nil), []string{"pkg:g1"}, []string(nil), []string(nil))
	wantMiddle := drainOracle(t, ctx, db, nil, []string{"pkg:g1"}, nil, nil)
	if len(wantMiddle) != 2*listFactsByKindPageSize {
		t.Fatalf("oracle rows for pkg:g1 = %d, want %d active rows around the superseded pages", len(wantMiddle), 2*listFactsByKindPageSize)
	}
	for name, mode := range map[string][2]bool{"stops on a short active page": {true, false}, "advances on the last active id": {false, true}} {
		got, err := drainWrongReader(ctx, middle, db, mode[0], mode[1])
		if err != nil {
			t.Fatalf("%s: error = %v", name, err)
		}
		if len(got) >= len(wantMiddle) {
			t.Fatalf("the seeded-wrong reader that %s returned %d rows, want fewer than the %d active rows: the seed would not catch the bug", name, len(got), len(wantMiddle))
		}
	}
}

// firstDifference names the first index where two row lists differ.
func firstDifference(got, want []string) string {
	for i := 0; i < len(got) && i < len(want); i++ {
		if got[i] != want[i] {
			return fmt.Sprintf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
	return fmt.Sprintf("length: got %d, want %d", len(got), len(want))
}
