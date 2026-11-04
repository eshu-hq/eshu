// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8880 disposable PostgreSQL 18 proof that every supply-chain impact active-evidence branch plans on its catalog anchor under force_generic_plan and force_custom_plan, and that dropping each new anchor index (RED) moves the plan off it at higher buffer cost until the migration statement rebuilds it (GREEN); wall time is logged, not gated
//eshu:readiness:env ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN

import (
	"context"
	"database/sql"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

var supplyChainImpactPlanModes = []string{"force_generic_plan", "force_custom_plan"}

// TestSupplyChainImpactBranchPlansStayOnAnchorsLive is ruling 4.1c/4.1d on
// the scaled fixture: 150 repository scopes of TOASTed files, the arm matrix,
// 4,000 non-matching rows of every other kind spread over those scopes'
// active and superseded generations, and 40,000 wide padding rows that give the
// heap the size at which the upstream key probe beats the kind walk on the CI
// image at stock settings (see seedSupplyChainImpactPlanPadding). The
// production loader runs one call with every input set, so every catalog
// branch issues its real statement with real binds.
// Each must plan its fact_records read on its anchor and on nothing else,
// under both plan_cache_mode values. Then each #8880 index is dropped (RED:
// the branches that anchor on it plan elsewhere) and rebuilt from its
// migration statement (GREEN: back on the anchor, at strictly fewer buffers).
func TestSupplyChainImpactBranchPlansStayOnAnchorsLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedSupplyChainImpactScaledCorpus(t, ctx, database, 150, 40, 500)
	seedSupplyChainImpactMatrix(t, ctx, database)
	seedSupplyChainImpactGrowthBulk(t, ctx, database, 4000, 150)
	seedSupplyChainImpactPlanPadding(t, ctx, database, 40000)
	recorder := &supplyChainImpactRecordingDB{SQLDB: SQLDB{DB: database}}
	filter := supplyChainImpactMatrixAllFilter()
	for n := 1; n <= 8; n++ {
		filter.FileRepositoryIDs = append(filter.FileRepositoryIDs, supplyChainImpactScaledRepositoryID(n*7))
	}
	// A full chunk, so the upstream branch's plan is proven at the size it runs at.
	for n := 1; len(filter.UpstreamCVEIDs) < factimpact.MaxCVEIDsPerStatement; n++ {
		filter.UpstreamCVEIDs = append(filter.UpstreamCVEIDs, fmt.Sprintf("CVE-1999-%05d", n))
	}
	_, statements := supplyChainImpactProductionRows(t, ctx, recorder, filter)

	byBranch := map[string]supplyChainImpactRecordedStatement{}
	for _, statement := range statements {
		for _, branch := range factimpact.Branches() {
			if branch.SQL == statement.Query {
				byBranch[branch.Name] = statement
			}
		}
	}
	for _, branch := range factimpact.Branches() {
		statement, ok := byBranch[branch.Name]
		if !ok {
			t.Fatalf("branch %s never ran for the all-inputs filter; the plan proof cannot cover it", branch.Name)
		}
		for _, mode := range supplyChainImpactPlanModes {
			_, replays := supplyChainImpactStatementBuffers(t, ctx, database, []supplyChainImpactRecordedStatement{statement}, mode)
			if problem := supplyChainImpactAnchorProblem(t, replays[0].Plan, append([]string{branch.Anchor}, branch.EquivalentAnchors...)...); problem != "" {
				t.Errorf("%s %s: %s\n%s", branch.Name, mode, problem, supplyChainImpactPlanSummary(t, replays[0].Plan))
			}
			for _, node := range supplyChainImpactPlanNodes(t, replays[0].Plan) {
				if !strings.HasPrefix(node.IndexName, "fact_records") {
					continue
				}
				// Ruling Addendum B: a resolved-scope probe bounds both leading
				// columns; a bounded kind's anchor node is recorded as read.
				if branch.Shape == factimpact.ShapeScope &&
					(!strings.Contains(node.IndexCond, "scope_id = ANY") || !strings.Contains(node.IndexCond, "generation_id = ANY")) {
					t.Errorf("%s %s: Index Cond %q must bound scope_id and generation_id", branch.Name, mode, node.IndexCond)
				}
				t.Logf("anchor %s %s: %s on %s cond=%q rows=%.0f rechecked=%.0f buffers=%d",
					branch.Name, mode, node.NodeType, node.IndexName, node.IndexCond, node.ActualRows, node.Rechecked, node.SharedHit+node.SharedRead)
			}
		}
	}

	for _, index := range factimpact.NewIndexes() {
		var anchored []factimpact.Branch
		for _, branch := range factimpact.Branches() {
			if branch.Anchor == index.Name {
				anchored = append(anchored, branch)
			}
		}
		if len(anchored) == 0 {
			t.Fatalf("index %s anchors no branch", index.Name)
		}
		supplyChainImpactExecAnalyze(t, ctx, database, "DROP INDEX "+index.Name)
		red := map[string]int64{}
		for _, branch := range anchored {
			for _, mode := range supplyChainImpactPlanModes {
				buffers, replays := supplyChainImpactStatementBuffers(t, ctx, database, []supplyChainImpactRecordedStatement{byBranch[branch.Name]}, mode)
				if strings.Contains(replays[0].Plan, index.Name) {
					t.Fatalf("RED %s: %s %s still names the dropped index", index.Name, branch.Name, mode)
				}
				red[branch.Name+"/"+mode] = buffers
				t.Logf("RED %s %s %s plan:\n%s", index.Name, branch.Name, mode, supplyChainImpactPlanSummary(t, replays[0].Plan))
			}
		}
		buildStart := time.Now()
		supplyChainImpactExecAnalyze(t, ctx, database, index.Statement)
		var size int64
		if err := database.QueryRowContext(ctx, "SELECT pg_relation_size($1::regclass)", index.Name).Scan(&size); err != nil {
			t.Fatalf("size of %s: %v", index.Name, err)
		}
		t.Logf("index %s: rebuild with ANALYZE %s, size %d bytes", index.Name, time.Since(buildStart).Round(time.Millisecond), size)
		for _, branch := range anchored {
			for _, mode := range supplyChainImpactPlanModes {
				buffers, replays := supplyChainImpactStatementBuffers(t, ctx, database, []supplyChainImpactRecordedStatement{byBranch[branch.Name]}, mode)
				if problem := supplyChainImpactAnchorProblem(t, replays[0].Plan, index.Name); problem != "" {
					t.Fatalf("GREEN %s: %s %s: %s", index.Name, branch.Name, mode, problem)
				}
				key := branch.Name + "/" + mode
				t.Logf("%s %s: RED (dropped) buffers=%d, GREEN (anchored) buffers=%d", index.Name, key, red[key], buffers)
				if buffers >= red[key] {
					t.Errorf("%s %s: anchored plan costs %d buffers, more than the %d without the index", index.Name, key, buffers, red[key])
				}
			}
		}
	}
}

// TestSupplyChainImpactNewIndexInsertCostLive measures the write cost the 13
// #8880 indexes add (index doctrine): the same batch of file and growth-kind
// rows is inserted with every new index present and with all of them
// dropped. It logs, never gates: wall time on a shared machine is evidence,
// not a contract.
func TestSupplyChainImpactNewIndexInsertCostLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedSupplyChainImpactScaledCorpus(t, ctx, database, 50, 20, 100)
	insert := func(label string) time.Duration {
		started := time.Now()
		// 2,000 TOASTed file rows: every file insert evaluates the guard
		// CASE and the JS/TS predicate.
		if _, err := database.ExecContext(ctx, `
INSERT INTO fact_records (
  fact_id, scope_id, generation_id, fact_kind, stable_fact_key, schema_version,
  collector_kind, fencing_token, source_confidence, source_system, source_fact_key,
  observed_at, ingested_at, payload
)
SELECT format('bulk:file:%s', n), 'git:scaled:0001', 'gen:scaled:0001:active', 'file',
  format('stable:bulk:file:%s', n), '1.0.0', 'git', 1, 'reported', 'git', format('source:bulk:file:%s', n),
  now(), now(), jsonb_build_object('repo_id', 'repository:scaled:0001', 'relative_path', format('src/b_%s.ts', n),
    'parsed_file_data', jsonb_build_object('language', CASE WHEN n % 4 = 0 THEN 'typescript' ELSE 'go' END,
      'content', (SELECT string_agg(md5(n::text || ':' || g::text), '') FROM generate_series(1, 160) AS g)))
FROM generate_series(1, 2000) AS n`); err != nil {
			t.Fatalf("insert bulk file rows: %v", err)
		}
		fileElapsed := time.Since(started)
		seedSupplyChainImpactGrowthBulk(t, ctx, database, 500, 50)
		elapsed := time.Since(started)
		if _, err := database.ExecContext(ctx, "DELETE FROM fact_records WHERE fact_id LIKE 'bulk:%'"); err != nil {
			t.Fatalf("delete bulk rows: %v", err)
		}
		t.Logf("%s: 2,000 TOASTed file rows in %s; plus %d rows (500 per non-file kind) and VACUUM ANALYZE, %s total",
			label, fileElapsed.Round(time.Millisecond), 500*len(supplyChainImpactBulkKinds()), elapsed.Round(time.Millisecond))
		return fileElapsed
	}
	insert("warmup with #8880 indexes")
	with := insert("with #8880 indexes")
	for _, index := range factimpact.NewIndexes() {
		supplyChainImpactExecAnalyze(t, ctx, database, "DROP INDEX "+index.Name)
	}
	without := insert("without #8880 indexes")
	t.Logf("file insert cost: with=%s without=%s delta=%s for 2,000 rows", with.Round(time.Millisecond), without.Round(time.Millisecond), (with - without).Round(time.Millisecond))
}

// supplyChainImpactAnchorProblem reports why a plan is not anchored: one of
// the accepted anchors must appear, and fact_records may be read by no
// sequential scan and no other index.
func supplyChainImpactAnchorProblem(t *testing.T, plan string, anchors ...string) string {
	t.Helper()
	// PostgreSQL truncates identifiers to 63 bytes, and EXPLAIN reports the
	// stored name.
	for i, anchor := range anchors {
		anchors[i] = anchor[:min(len(anchor), 63)]
	}
	found := false
	for _, node := range supplyChainImpactPlanNodes(t, plan) {
		switch {
		case slices.Contains(anchors, node.IndexName):
			found = true
		case node.NodeType == "Seq Scan" && node.Relation == "fact_records":
			return "sequential scan of fact_records"
		case strings.HasPrefix(node.IndexName, "fact_records"):
			return "reads fact_records through " + node.IndexName + ", not " + strings.Join(anchors, " or ")
		}
	}
	if !found {
		return "no accepted anchor (" + strings.Join(anchors, ", ") + ") in plan"
	}
	return ""
}

// TestSupplyChainImpactUpstreamPlanHoldsAtEveryChunkSizeLive plans the upstream
// statement at every size the production chunker can emit, under both plan modes.
// The full-chunk proof above covers one size, and the estimate is not monotonic in
// the id count: the planner sums the per-id default below 200 ids and uses the
// independent formula above it, so the sizes under 200 are priced higher than the
// full chunk. The sizes follow MaxCVEIDsPerStatement. EXPLAIN without
// ANALYZE, because the plan choice is the claim and this runs once per size.
//
// Measured on PostgreSQL 18 at stock settings: with the padding every size stays on
// the anchor under both modes; without it the custom plan leaves the anchor from 114
// ids through 240, so this catches a walk.
func TestSupplyChainImpactUpstreamPlanHoldsAtEveryChunkSizeLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedSupplyChainImpactScaledCorpus(t, ctx, database, 150, 40, 500)
	seedSupplyChainImpactMatrix(t, ctx, database)
	seedSupplyChainImpactGrowthBulk(t, ctx, database, 4000, 150)
	seedSupplyChainImpactPlanPadding(t, ctx, database, 40000)
	if _, err := database.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze fact_records: %v", err)
	}

	var branch factimpact.Branch
	for _, candidate := range factimpact.Branches() {
		if candidate.Name == factimpact.BranchVulnerabilityUpstreamCVEID {
			branch = candidate
		}
	}
	sizes := slices.Sorted(maps.Keys(upstreamChunkSizes(t, max(260, 2*factimpact.MaxCVEIDsPerStatement+1))))
	if len(sizes) == 0 || sizes[len(sizes)-1] != factimpact.MaxCVEIDsPerStatement {
		t.Fatalf("chunk sizes = %v, want them to reach the bound %d", sizes, factimpact.MaxCVEIDsPerStatement)
	}
	ids := make([]string, 0, sizes[len(sizes)-1])
	for n := 1; len(ids) < cap(ids); n++ {
		ids = append(ids, fmt.Sprintf("CVE-1999-%05d", n))
	}

	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("dedicated sweep session: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.ExecContext(ctx, "PREPARE upstream_sweep AS "+branch.SQL); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	for _, mode := range supplyChainImpactPlanModes {
		if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+mode); err != nil {
			t.Fatalf("set plan_cache_mode: %v", err)
		}
		var off []int
		firstProblem := ""
		for _, size := range sizes {
			var plan []byte
			execute := "EXPLAIN (FORMAT JSON) EXECUTE upstream_sweep(" + supplyChainImpactSQLLiteral(t, ids[:size]) + ")"
			if err := conn.QueryRowContext(ctx, execute).Scan(&plan); err != nil {
				t.Fatalf("%s explain at %d ids: %v", mode, size, err)
			}
			anchors := append([]string{branch.Anchor}, branch.EquivalentAnchors...)
			if problem := supplyChainImpactAnchorProblem(t, string(plan), anchors...); problem != "" {
				off = append(off, size)
				if firstProblem == "" {
					firstProblem = problem + "\n" + supplyChainImpactPlanSummary(t, string(plan))
				}
			}
		}
		t.Logf("%s: planned %d sizes up to %d, off the anchor at %v", mode, len(sizes), sizes[len(sizes)-1], off)
		if len(off) > 0 {
			t.Errorf("%s: off the anchor at %d of %d sizes: %v\nfirst: %s", mode, len(off), len(sizes), off, firstProblem)
		}
	}
}

// supplyChainImpactExecAnalyze runs one DDL statement and refreshes planner
// statistics, which a dropped expression index takes with it.
func supplyChainImpactExecAnalyze(t *testing.T, ctx context.Context, database *sql.DB, statement string) {
	t.Helper()
	if _, err := database.ExecContext(ctx, statement); err != nil {
		t.Fatalf("%s: %v", statement, err)
	}
	if _, err := database.ExecContext(ctx, "ANALYZE fact_records"); err != nil {
		t.Fatalf("analyze fact_records: %v", err)
	}
}

// TestSupplyChainImpactUpstreamListReadsOnlySignalsLive is the #9914 fallback on
// real rows. An upstream id loads the CVE's KEV and EPSS facts and every suppression
// scoped to it however the scope spells the id, never its cve, affected_package or
// affected_product facts, and a list that spans chunks loses nothing.
func TestSupplyChainImpactUpstreamListReadsOnlySignalsLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedSupplyChainImpactMatrix(t, ctx, database)

	ids := []string{scimCVE}
	for n := 1; len(ids) <= 2*factimpact.MaxCVEIDsPerStatement+1; n++ {
		ids = append(ids, fmt.Sprintf("CVE-1999-%05d", n))
	}
	slices.Sort(ids)
	store := NewFactStore(SQLDB{DB: database})
	load := func(list []string) (map[string]int, []string) {
		loaded, _, err := store.ListActiveSupplyChainImpactFacts(ctx, supplychaincore.SupplyChainImpactFactFilter{UpstreamCVEIDs: list})
		if err != nil {
			t.Fatalf("ListActiveSupplyChainImpactFacts(): %v", err)
		}
		kinds := map[string]int{}
		var factIDs []string
		for _, envelope := range loaded {
			kinds[envelope.FactKind]++
			factIDs = append(factIDs, envelope.FactID)
		}
		slices.Sort(factIDs)
		return kinds, factIDs
	}
	kinds, one := load([]string{scimCVE})
	for kind, count := range kinds {
		switch kind {
		case factimpact.KindEPSSScore, factimpact.KindKnownExploited:
			if count != 1 {
				t.Errorf("%s rows = %d, want the CVE's one row", kind, count)
			}
		case factimpact.KindSuppression:
		case "reducer_container_image_identity":
			// The fixture's identity stream stub ignores its inputs. Production's is
			// keyed on digests and repositories, and this list reaches neither.
		default:
			t.Errorf("loaded %d %s rows: an upstream id must not load this kind", count, kind)
		}
	}
	if kinds[factimpact.KindEPSSScore] != 1 || kinds[factimpact.KindKnownExploited] != 1 {
		t.Errorf("kinds = %v, want the EPSS and KEV rows", kinds)
	}
	// The 80 bulk rows plus the lower-cased scope. The arm reads the scope, and a
	// top-level cve_id on a suppression is not a scope.
	if got, want := kinds[factimpact.KindSuppression], 80+1; got != want {
		t.Errorf("suppressions = %d, want %d", got, want)
	}
	if !slices.Contains(one, "m:supp:cve-lower") || slices.Contains(one, "m:supp:top-cve") {
		t.Errorf("suppression rows: want m:supp:cve-lower in and m:supp:top-cve out, got %d rows", len(one))
	}
	if _, spanning := load(ids); !slices.Equal(spanning, one) {
		t.Errorf("a list spanning chunks loaded %d rows, want the same %d", len(spanning), len(one))
	}
}
