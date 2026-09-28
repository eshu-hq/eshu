// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Memory proof for #7320. The fold widens the rows a supersede sweep carries
// (the old failure columns join the update's target list), so this measures the
// executor memory and temp-file effect that timing cannot show: per-node peak
// memory, hash batches, sort and temp spill, and temp blocks, for the shipped
// statement against the same statement with the fold cut out, over the ops-qa
// scale case and the 64 KB stress case. The claim statements set no work_mem of
// their own (no SET LOCAL), so the server default applies; a second run at the
// PostgreSQL minimum shows how close each variant sits to spilling. Opt in with
// ESHU_7320_COST_PROOF=1 and the claim-proof DSN.

var (
	memoryKBPattern    = regexp.MustCompile(`(?:Memory Usage|Memory): (\d+)kB`)
	diskKBPattern      = regexp.MustCompile(`Disk: (\d+)kB`)
	batchesPattern     = regexp.MustCompile(`Batches: (\d+)`)
	tempBlocksPattern  = regexp.MustCompile(`temp read=(\d+)(?: written=(\d+))?`)
	tempWrittenPattern = regexp.MustCompile(`temp (?:read=\d+ )?written=(\d+)`)
)

// planMemory is the executor memory and spill summary of one EXPLAIN ANALYZE.
type planMemory struct {
	maxNodeKB, sumNodeKB, diskKB int
	multiBatchNodes, tempBlocks  int
	execMs, maxNodeLine          string
}

func summarizePlan(lines []string) planMemory {
	var m planMemory
	for _, l := range lines {
		for _, hit := range memoryKBPattern.FindAllStringSubmatch(l, -1) {
			kb, _ := strconv.Atoi(hit[1])
			m.sumNodeKB += kb
			if kb > m.maxNodeKB {
				m.maxNodeKB, m.maxNodeLine = kb, strings.TrimSpace(l)
			}
		}
		for _, hit := range diskKBPattern.FindAllStringSubmatch(l, -1) {
			kb, _ := strconv.Atoi(hit[1])
			m.diskKB += kb
		}
		if hit := batchesPattern.FindStringSubmatch(l); hit != nil {
			if n, _ := strconv.Atoi(hit[1]); n > 1 {
				m.multiBatchNodes++
			}
		}
		if strings.Contains(l, "temp ") {
			for _, hit := range tempBlocksPattern.FindAllStringSubmatch(l, -1) {
				n, _ := strconv.Atoi(hit[1])
				m.tempBlocks += n
			}
			for _, hit := range tempWrittenPattern.FindAllStringSubmatch(l, -1) {
				n, _ := strconv.Atoi(hit[1])
				m.tempBlocks += n
			}
		}
		if strings.HasPrefix(strings.TrimSpace(l), "Execution Time") {
			m.execMs = strings.TrimSpace(l)
		}
	}
	return m
}

func explainMemory(t *testing.T, database *sql.DB, workMem, statement string, args []any) planMemory {
	t.Helper()
	ctx := context.Background()
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, "SET LOCAL work_mem = '"+workMem+"'"); err != nil {
		t.Fatalf("set work_mem: %v", err)
	}
	rows, err := tx.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, VERBOSE) "+statement, args...)
	if err != nil {
		t.Fatalf("explain analyze: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var lines []string
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatalf("scan: %v", err)
		}
		lines = append(lines, l)
	}
	return summarizePlan(lines)
}

func TestSupersedePriorFailureClaimMemory(t *testing.T) {
	requireCostProof(t)
	dsn := claimMaintenanceProofDSN(t)
	database := openClaimDeadlockProofDB(t, dsn, 2)
	var serverWorkMem string
	if err := database.QueryRow(`SHOW work_mem`).Scan(&serverWorkMem); err != nil {
		t.Fatalf("show work_mem: %v", err)
	}
	t.Logf("host load: %s; server default work_mem = %s", hostLoad(), serverWorkMem)
	statements := []struct {
		name string
		text *string
		seed func(*testing.T, *sql.DB, int, int)
		args func(time.Time) []any
	}{
		{
			"projector_claim", &claimProjectorWorkQuery, costSeedProjector,
			func(now time.Time) []any { return []any{now, "mem", now.Add(time.Minute), ""} },
		},
		{
			"reducer_claim", &claimReducerWorkQuery, costSeedReducer,
			func(now time.Time) []any { return costReducerArgs(now, 0) },
		},
		{
			"reducer_claim_batch", &claimReducerWorkBatchQuery, costSeedReducer,
			func(now time.Time) []any { return costReducerArgs(now, 4) },
		},
	}
	scenarios := []struct {
		name        string
		rows, bytes int
	}{
		{"ops_qa_scale", costEnvInt("ESHU_7320_COST_ROWS", 3500), costEnvInt("ESHU_7320_COST_DETAIL_BYTES", 800)},
		{"stress_wide_details", costEnvInt("ESHU_7320_COST_STRESS_ROWS", 500), costEnvInt("ESHU_7320_COST_STRESS_BYTES", 65536)},
	}
	for _, st := range statements {
		after := *st.text
		fragment := " || " + priorFailureDetailsSQL("stale")
		if !strings.Contains(after, fragment) {
			t.Fatalf("%s: shipped statement does not contain the fold", st.name)
		}
		before := strings.Replace(after, fragment, "", 1)
		for _, sc := range scenarios {
			for _, workMem := range []string{serverWorkMem, "64kB"} {
				for _, variant := range []string{"before", "after"} {
					st.seed(t, database, sc.rows, sc.bytes)
					text := after
					if variant == "before" {
						text = before
					}
					m := explainMemory(t, database, workMem, text, st.args(time.Now().UTC()))
					t.Logf("MEM %s %s work_mem=%s %s: max_node_mem=%dkB sum_node_mem=%dkB disk=%dkB multi_batch_nodes=%d temp_blocks=%d %s | widest node: %s",
						st.name, sc.name, workMem, variant, m.maxNodeKB, m.sumNodeKB, m.diskKB, m.multiBatchNodes, m.tempBlocks, m.execMs, m.maxNodeLine)
				}
			}
		}
	}
}
