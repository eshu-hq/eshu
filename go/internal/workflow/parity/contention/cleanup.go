// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// Cleanup entry env: the stranded seed plus the exact pending-item and run
// counts the killed leg's record shows. All three are required and
// explicit: without them the entry skips, never deletes.
const (
	envCleanupSeed    = "ESHU_CONTENTION_CLEANUP_SEED"
	envCleanupPending = "ESHU_CONTENTION_CLEANUP_EXPECT_PENDING"
	envCleanupRuns    = "ESHU_CONTENTION_CLEANUP_EXPECT_RUNS"
)

// resurveyDelay waits out one satellite reconcile tick (60s) plus margin,
// proving the deleted rows do not come back.
const resurveyDelay = 70 * time.Second

// checkStrandedPreconditions fails closed unless the database holds exactly
// the stranded leg's record: wantPending pending items and wantRuns runs of
// this seed, with every sibling table the delete touches at zero — no
// claimed items, no claim rows, no live claims, no terminal duplicates, no
// progress, completeness, quota, or pacing rows, and exactly one
// instance-family row.
func checkStrandedPreconditions(c Census, snap Snapshot, nonterm, doubleTerm []string, seed, wantPending, wantRuns int64) error {
	if got := c.Prefixed["workflow_work_items|pending"]; got != int(wantPending) {
		return fmt.Errorf("pending items = %d, want exactly %d", got, wantPending)
	}
	for key, n := range c.Prefixed {
		table, _, _ := strings.Cut(key, "|")
		switch table {
		case "workflow_work_items":
			if key != "workflow_work_items|pending" && n != 0 {
				return fmt.Errorf("census %s = %d, want 0", key, n)
			}
		case "workflow_claims":
			if n != 0 {
				return fmt.Errorf("census %s = %d, want 0 claim rows", key, n)
			}
		case "workflow_run_progress", "workflow_run_completeness", "component_quota_windows", "workflow_claim_pacing":
			if n != 0 {
				return fmt.Errorf("census %s = %d, want 0", key, n)
			}
		}
	}
	runs := 0
	for key, n := range c.Prefixed {
		if strings.HasPrefix(key, "workflow_runs|") {
			runs += n
		}
	}
	if int64(runs) != wantRuns {
		return fmt.Errorf("seed runs = %d, want exactly %d", runs, wantRuns)
	}
	if snap.ActiveClaims != 0 {
		return fmt.Errorf("active claims = %d, want 0", snap.ActiveClaims)
	}
	if len(doubleTerm) != 0 {
		return fmt.Errorf("double terminal items = %d, want 0", len(doubleTerm))
	}
	prefix := strings.TrimSuffix(seedItemPattern(seed), "%")
	mine, foreign := 0, []string{}
	for _, entry := range nonterm {
		id, status, _ := strings.Cut(entry, ":")
		if !strings.HasPrefix(id, prefix) {
			foreign = append(foreign, entry)
			continue
		}
		mine++
		if status != string(workflow.WorkItemStatusPending) {
			return fmt.Errorf("seed item %q status %q, want pending", id, status)
		}
	}
	_ = foreign
	if int64(mine) != wantPending {
		return fmt.Errorf("seed nonterminal items = %d, want %d", mine, wantPending)
	}
	instances := 0
	for key, n := range c.Prefixed {
		if strings.HasPrefix(key, "collector_instances|") {
			instances += n
		}
	}
	if instances != 1 {
		return fmt.Errorf("instance-family rows = %d, want exactly 1", instances)
	}
	return nil
}
