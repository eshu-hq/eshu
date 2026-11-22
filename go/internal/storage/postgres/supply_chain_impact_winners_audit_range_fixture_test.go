// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres_test

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// The #11131 audit sweep fixture: a small corpus with every shape the range
// rules must handle. Identifier g (1..identifiers) has package p(g/8),
// repository repo-((g/2)%3), an empty or set digest, CVE g (empty on g%7==0,
// so c0 falls back to the advisory) and advisory GHSA-g. Four active source
// scopes each hold 1-3 facts per identifier over two observed versions, so
// an identifier carries several canonical keys and several holders. One
// identifier (g=1000, between packages) holds 70 facts: an oversize
// identifier at small budgets. Exclusions: a superseded generation, a
// derived-image scope, tombstones, and retracted classifications. Operator
// overrides suppress some keys, and one operator fact has no package.

const (
	auditRangeFixtureIdentifiers = 60
	auditRangeOversizeG          = 1000
	auditRangeOversizeFacts      = 70
)

// auditRangeFactsSQL inserts one scope's facts. $1 scope, $2 generation, $3
// fact kind, $4 identifiers, $5 scope index (varies holders per identifier),
// $6 observed-version prefix, $7 tombstone.
const auditRangeFactsSQL = `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $2 || ':' || g || ':' || j, $1, $2, $3, $2 || ':' || g || ':' || j, '1.0.0', 'synthetic',
  $2 || ':' || g || ':' || j, now(), now(), $7::boolean,
  jsonb_build_object(
    'finding_id', 'finding:' || g || ':' || (j % 2),
    'cve_id', CASE WHEN g % 7 = 0 THEN '' ELSE 'CVE-2026-' || g END,
    'advisory_id', 'GHSA-' || g,
    'package_id', 'pkg:npm/p' || lpad((g / 8)::text, 2, '0'),
    'repository_id', 'repo-' || ((g / 2) % 3),
    'subject_digest', CASE WHEN g % 2 = 0 THEN '' ELSE 'sha256:' || (g % 5) END,
    'impact_status', 'affected_exact',
    'observed_version', $6 || (j % 2),
    'priority_score', (g * 7 + j * 3 + $5 * 11) % 90,
    'ecosystem', 'npm')
FROM generate_series(1, $4::int) AS g
CROSS JOIN LATERAL generate_series(1, 1 + (g + $5) % 3) AS j`

// auditRangeOversizeSQL inserts the oversize identifier's facts in one scope.
const auditRangeOversizeSQL = `
INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $2 || ':big:' || j, $1, $2, $3, $2 || ':big:' || j, '1.0.0', 'synthetic', $2 || ':big:' || j, now(), now(), false,
  jsonb_build_object('finding_id', 'finding:big:' || (j % 3), 'cve_id', 'CVE-2026-` + "1000" + `',
    'advisory_id', 'GHSA-1000', 'package_id', 'pkg:npm/p04x', 'repository_id', 'repo-0',
    'subject_digest', '', 'impact_status', 'affected_exact', 'observed_version', '2.' || (j % 3),
    'priority_score', j % 50, 'ecosystem', 'npm')
FROM generate_series(1, $4::int) AS j`

// auditRangeSeed builds the fixture and fills the stored winners with one
// full resweep. It returns the live finding-fact count (tombstones excluded).
func auditRangeSeed(t *testing.T, h *keyedHarness, identifiers int) int {
	t.Helper()
	kind := facts.ReducerSupplyChainImpactFindingFactKind
	for k := 0; k < 4; k++ {
		scope := fmt.Sprintf("vuln-intel://11131/s%d", k)
		gen := winnersLiveSeedScope(t, h.ctx, h.db, scope)
		h.exec(auditRangeFactsSQL, scope, gen, kind, identifiers, k, "1.", false)
		if k == 0 {
			h.exec(auditRangeOversizeSQL, scope, gen, kind, auditRangeOversizeFacts)
			// Tombstones never count.
			h.exec(strings.Replace(auditRangeFactsSQL, "':' || g || ':' || j", "':t:' || g || ':' || j", 3),
				scope, gen, kind, identifiers/3, k, "8.", true)
			// A superseded generation of the same scope.
			old := scope + "/old"
			h.exec(`INSERT INTO scope_generations (generation_id, scope_id, trigger_kind, observed_at, ingested_at, status, activated_at, payload)
VALUES ($1, $2, 'synthetic', now(), now(), 'superseded', now(), '{}'::jsonb)`, old, scope)
			h.exec(auditRangeFactsSQL, scope, old, kind, identifiers, k, "9.", false)
		}
	}
	// Collation: the database collation, not byte order, sorts these. Under
	// a libc or ICU collation '@', '.', '-' and '_' are ignored at the first
	// level, so '@tootallnate/once' sorts among the t's. Every range bound and
	// cursor comparison runs in SQL, so the sweep must still visit each once.
	punct := "vuln-intel://11131/punct"
	pgen := winnersLiveSeedScope(t, h.ctx, h.db, punct)
	h.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT $1 || ':' || p.ord || ':' || j, $2, $1, $3, $1 || ':' || p.ord || ':' || j, '1.0.0', 'synthetic',
  $1 || ':' || p.ord || ':' || j, now(), now(), false,
  jsonb_build_object('finding_id', 'finding:punct:' || p.ord || ':' || j, 'cve_id', 'CVE-2026-P' || p.ord,
    'advisory_id', 'GHSA-P' || p.ord, 'package_id', p.pkg, 'repository_id', 'repo-' || (j % 2),
    'subject_digest', '', 'impact_status', 'affected_exact', 'observed_version', '3.' || j,
    'priority_score', (p.ord * 5 + j) % 90, 'ecosystem', 'npm')
FROM unnest(ARRAY['@tootallnate/once', '@scope/pkg', 'pkg:npm/@types/node', '.dotted', '-dashed', '_under',
  'pkg:npm/p01-x', 'pkg:npm/p01.y', 'Pkg:npm/upper', 'tootallnate']) WITH ORDINALITY AS p(pkg, ord)
CROSS JOIN generate_series(1, 3) AS j`, pgen, punct, kind)
	derived := "image://derived/11131"
	dgen := winnersLiveSeedScope(t, h.ctx, h.db, derived)
	h.exec(auditRangeFactsSQL, derived, dgen, kind, identifiers/2, 7, "7.", false)
	// Retracted classifications drop some source facts.
	h.exec(`INSERT INTO scanner_image_result_fact_classifications (finding_fact_id, source_class, lifecycle)
SELECT fact_id, 'static', 'retracted' FROM fact_records
WHERE scope_id = 'vuln-intel://11131/s1' AND fact_id LIKE '%:1' AND (split_part(fact_id, ':', 3))::int % 5 = 0`)
	// Operator overrides: suppress keys of g % 6 == 0 (observed version 1.1),
	// plus one override with no package that matches nothing.
	op := "operator:vulnerability_suppressions"
	opGen := winnersLiveSeedScope(t, h.ctx, h.db, op)
	h.exec(`INSERT INTO fact_records (fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  source_system, source_fact_key, observed_at, ingested_at, is_tombstone, payload)
SELECT 'op:' || g, $1, $2, $3, 'op:' || g, '1.0.0', 'synthetic', 'op:' || g, now(), now(), false,
  jsonb_build_object('cve_id', CASE WHEN g % 7 = 0 THEN '' ELSE 'CVE-2026-' || g END, 'advisory_id', 'GHSA-' || g,
    'package_id', 'pkg:npm/p' || lpad((g / 8)::text, 2, '0'), 'repository_id', 'repo-' || ((g / 2) % 3),
    'subject_digest', CASE WHEN g % 2 = 0 THEN '' ELSE 'sha256:' || (g % 5) END,
    'impact_status', 'affected_exact', 'observed_version', '1.1', 'suppression_state', 'suppressed',
    'suppression', jsonb_build_object('expires_at', '2030-01-01T00:00:00Z'))
FROM generate_series(6, $4::int, 6) AS g
UNION ALL
SELECT 'op:nopkg', $1, $2, $3, 'op:nopkg', '1.0.0', 'synthetic', 'op:nopkg', now(), now(), false,
  jsonb_build_object('cve_id', 'CVE-2026-9', 'suppression_state', 'suppressed')`, op, opGen, kind, identifiers)
	h.exec(`ANALYZE fact_records`)
	if _, err := h.store.ResweepWinners(h.ctx); err != nil {
		t.Fatalf("populating full: %v", err)
	}
	// Operator facts load whole on every range and are not range facts.
	return h.count(`SELECT count(*) FROM fact_records WHERE fact_kind = $1 AND is_tombstone = FALSE AND scope_id <> $2`, kind, op)
}

// auditRangePerturb makes stored winners stale, deletes some (missing), and
// adds orphans before the first identifier, between packages, between
// repositories, on the oversize identifier, and after the last identifier.
func auditRangePerturb(t *testing.T, h *keyedHarness) {
	t.Helper()
	h.exec(`UPDATE supply_chain_impact_canonical_winners SET priority_score = priority_score + 1
WHERE abs(hashtext(canonical_key)) % 9 = 0`)
	h.exec(`UPDATE supply_chain_impact_canonical_winners SET suppression_state = 'active'
WHERE suppression_state <> 'active' AND abs(hashtext(canonical_key)) % 2 = 0`)
	h.exec(`DELETE FROM supply_chain_impact_canonical_winners WHERE abs(hashtext(canonical_key)) % 13 = 1`)
	for i, o := range [][3]string{
		{"a-orphan-first", "repo-0", ""},
		{"pkg:npm/p02x", "repo-0", ""},
		{"pkg:npm/p01", "repo-1x", "sha256:1"},
		{"pkg:npm/p04x", "repo-0", ""},
		{"zzz-orphan-last", "repo-9", "sha256:9"},
		{"@tootallnate/orphan", "repo-0", ""},
	} {
		cve := "CVE-2026-1000"
		if o[0] != "pkg:npm/p04x" {
			cve = fmt.Sprintf("CVE-ORPHAN-%d", i)
		}
		h.exec(`INSERT INTO supply_chain_impact_canonical_winners
  (canonical_key, winner_fact_id, finding_id, package_id, repository_id, subject_digest, cve_id, advisory_id, materialized_at)
VALUES ($1, 'gone:' || $1, 'finding:orphan:' || $1, $2, $3, $4, $5, $6, now())`,
			fmt.Sprintf("orphan|%d|%s", i, o[0]), o[0], o[1], o[2], cve, "GHSA-1000")
	}
}

// auditRangeAdvanceToFirstCandidate returns the sweep id and the first range,
// in the sweep's own key order, that carries a candidate. Ranges before it
// carry none, take no lock, and are verified (advancing the persisted cursor)
// so the returned range starts at the stored cursor, ready to verify. Where
// the orphans sort depends on the database collation (a libc collation
// ignores '-', '.', '_' and '@' at the first level, the C collation of the
// CI image does not), so a proof must not assume which identifier opens the
// sweep: the orphan "a-orphan-first" opens it under en_US and does not under C.
func auditRangeAdvanceToFirstCandidate(t *testing.T, h *keyedHarness, budget int) (int64, postgres.WinnersAuditRange) {
	t.Helper()
	sweep, err := h.store.ReadWinnersAuditSweep(h.ctx)
	if err != nil {
		t.Fatalf("read sweep: %v", err)
	}
	reader := postgres.SQLDB{DB: h.db}
	var lo *postgres.WinnersAuditRangeIdent
	for ranges := 0; ranges < 10000; ranges++ {
		rng, err := postgres.ReadWinnersAuditRange(h.ctx, reader, lo, budget)
		if err != nil {
			t.Fatalf("range %d: %v", ranges, err)
		}
		if len(rng.Candidates) > 0 {
			return sweep.SweepID, rng
		}
		if rng.Last {
			t.Fatalf("no range of the sweep carries a candidate after %d ranges; the seeded orphans must", ranges+1)
		}
		out, err := h.store.VerifyWinnersAuditRange(h.ctx, postgres.WinnersAuditRangeVerify{SweepID: sweep.SweepID, Range: rng})
		if err != nil || !out.Advanced || out.Locked {
			t.Fatalf("candidate-free range %d verify = %+v, %v; want an unlocked advance", ranges, out, err)
		}
		lo = rng.Hi
	}
	t.Fatal("sweep did not reach a candidate range in 10000 ranges")
	return 0, postgres.WinnersAuditRange{}
}

// auditIdentKey renders an identifier for set comparison in Go; the order of
// the sweep is never computed here.
func auditIdentKey(i postgres.WinnersAuditIdent) string {
	return strings.Join([]string{i.PackageID.String, i.RepositoryID.String, i.SubjectDigest.String, i.C0.String, i.AdvisoryID.String}, "\x1f")
}

// auditSingleStatementCandidates runs the shipped single-statement audit
// candidate SELECT.
func auditSingleStatementCandidates(t *testing.T, ctx context.Context, db *sql.DB) map[string]bool {
	t.Helper()
	rows, err := db.QueryContext(ctx, postgres.WinnersAuditCandidateSelectSQLForTest)
	if err != nil {
		t.Fatalf("single-statement candidates: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var i postgres.WinnersAuditIdent
		if err := rows.Scan(&i.PackageID, &i.RepositoryID, &i.SubjectDigest, &i.C0, &i.AdvisoryID); err != nil {
			t.Fatalf("scan single-statement candidate: %v", err)
		}
		out[auditIdentKey(i)] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("single-statement candidates: %v", err)
	}
	return out
}

// auditSweepReading is the union of one full sweep's ranges.
type auditSweepReading struct {
	candidates map[string]int
	ranges     int
	oversize   int
	facts      int64
}

// auditSweepAll walks one sweep at budget with the given statement pair.
func auditSweepAll(t *testing.T, ctx context.Context, db *sql.DB, budget int, first, next string) auditSweepReading {
	t.Helper()
	out := auditSweepReading{candidates: map[string]int{}}
	var lo *postgres.WinnersAuditRangeIdent
	for {
		rng, err := postgres.ReadWinnersAuditRangeWithSQLForTest(ctx, postgres.SQLDB{DB: db}, lo, budget, first, next)
		if err != nil {
			t.Fatalf("budget %d range %d: %v", budget, out.ranges, err)
		}
		out.ranges++
		out.facts += rng.Facts
		if rng.Oversize {
			out.oversize++
		}
		if rng.Pipe {
			t.Fatalf("budget %d range %d reported '|' in an identifier", budget, out.ranges)
		}
		for _, c := range rng.Candidates {
			out.candidates[auditIdentKey(c)]++
		}
		if rng.Last {
			return out
		}
		if out.ranges > 100000 {
			t.Fatalf("budget %d: sweep did not end", budget)
		}
		lo = rng.Hi
	}
}

// auditSetDiff lists the keys of want missing from got.
func auditSetDiff(want map[string]bool, got map[string]int) []string {
	var out []string
	for k := range want {
		if got[k] == 0 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// explain runs an EXPLAIN statement and returns its plan text.
func (h *keyedHarness) explain(query string, args ...any) string {
	h.t.Helper()
	rows, err := h.db.QueryContext(h.ctx, query, args...)
	if err != nil {
		h.t.Fatalf("explain: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			h.t.Fatalf("scan explain: %v", err)
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		h.t.Fatalf("explain: %v", err)
	}
	return strings.Join(lines, "\n")
}
