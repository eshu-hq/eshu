// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #8880 disposable PostgreSQL 18 proof that every supply-chain impact active-evidence branch plans on its catalog anchor under force_generic_plan and force_custom_plan, and that dropping each new anchor index (RED) moves the plan off it at higher buffer cost until the migration statement rebuilds it (GREEN); wall time is logged, not gated
//eshu:readiness:env ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DISPOSABLE ESHU_SUPPLY_CHAIN_IMPACT_EVIDENCE_PROOF_DSN

import (
	"context"
	"database/sql"
	"slices"
	"strings"
	"testing"

	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
)

var supplyChainImpactPlanModes = []string{"force_generic_plan", "force_custom_plan"}

// TestSupplyChainImpactBranchPlansStayOnAnchorsLive is ruling 4.1c/4.1d on
// the scaled fixture: 150 repository scopes of TOASTed files, the arm matrix,
// and 4,000 non-matching rows of every other kind spread over those scopes'
// active and superseded generations. The production loader runs one call with every input
// set, so every catalog branch issues its real statement with real binds.
// Each must plan its fact_records read on its anchor and on nothing else,
// under both plan_cache_mode values. Then each #8880 index is dropped (RED:
// the branches that anchor on it plan elsewhere) and rebuilt from its
// migration statement (GREEN: back on the anchor, at strictly fewer buffers).
func TestSupplyChainImpactBranchPlansStayOnAnchorsLive(t *testing.T) {
	ctx, database := openSupplyChainImpactEvidenceProofDB(t)
	seedSupplyChainImpactScaledCorpus(t, ctx, database, 150, 40, 500)
	seedSupplyChainImpactMatrix(t, ctx, database)
	seedSupplyChainImpactGrowthBulk(t, ctx, database, 4000, 150)
	recorder := &supplyChainImpactRecordingDB{SQLDB: SQLDB{DB: database}}
	filter := supplyChainImpactMatrixAllFilter()
	for n := 1; n <= 8; n++ {
		filter.FileRepositoryIDs = append(filter.FileRepositoryIDs, supplyChainImpactScaledRepositoryID(n*7))
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
		supplyChainImpactExecAnalyze(t, ctx, database, index.Statement)
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
