// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package contention

import (
	"context"
	"sort"
	"time"
)

// Snapshot is the cheap per-sample view of one leg's synthetic rows.
type Snapshot struct {
	ItemsByStatus map[string]int `json:"items_by_status"`
	ItemTotal     int            `json:"item_total"`
	ActiveClaims  int            `json:"active_claims"`
	// DualLive lists synthetic items holding more than one active claim
	// row at the sampled instant (§6 exactly-once).
	DualLive []string `json:"dual_live,omitempty"`
}

// nonTerminal counts synthetic items not in a terminal status.
func (s Snapshot) nonTerminal() int {
	n := 0
	for status, count := range s.ItemsByStatus {
		if status != "completed" && status != "failed_terminal" {
			n += count
		}
	}
	return n
}

// Census counts rows per table and status, split by the synthetic prefix.
// Keys are "table|status". Baseline and residue compare censuses (§5
// snapshots, §6 residue).
type Census struct {
	Prefixed map[string]int `json:"prefixed"`
	Other    map[string]int `json:"other"`
}

// prefixedTotal is the number of synthetic rows across every table.
func (c Census) prefixedTotal() int {
	n := 0
	for _, v := range c.Prefixed {
		n += v
	}
	return n
}

// otherDelta lists every non-synthetic key whose count differs from base.
func (c Census) otherDelta(base Census) []string {
	keys := map[string]struct{}{}
	for k := range c.Other {
		keys[k] = struct{}{}
	}
	for k := range base.Other {
		keys[k] = struct{}{}
	}
	var out []string
	for k := range keys {
		if c.Other[k] != base.Other[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// ClaimRow is one claim row read back for the reclaim analysis.
type ClaimRow struct {
	ClaimID        string
	WorkItemID     string
	FencingToken   int64
	Status         string
	LeaseExpiresAt time.Time
	FinishedAt     time.Time
	// PinnedUntil is the satellite commit pin; zero on the in-tree leg.
	PinnedUntil time.Time
}

// QuotaBucket is one component_quota_windows row under the prefix.
type QuotaBucket struct {
	Component   string    `json:"component"`
	ScopeID     string    `json:"scope_id"`
	WindowStart time.Time `json:"window_start"`
	Grants      int       `json:"grants"`
}

// Observer reads and cleans one leg's database. The SQL implementation
// serves both legs (the tables share names and the columns the harness
// reads); tests use an in-memory fake with seeded violations.
type Observer interface {
	Snapshot(ctx context.Context) (Snapshot, error)
	// FencingViolations returns synthetic items whose claim tokens are
	// duplicated or exceed the item's current token (a regression).
	FencingViolations(ctx context.Context) ([]string, error)
	// DoubleTerminal returns synthetic items with more than one terminal
	// claim row (completed or failed_terminal): an exactly-once breach.
	DoubleTerminal(ctx context.Context) ([]string, error)
	Census(ctx context.Context) (Census, error)
	ClaimRows(ctx context.Context, claimIDs []string) (map[string]ClaimRow, error)
	QuotaBuckets(ctx context.Context) ([]QuotaBucket, error)
	// AnalyzedAt reports when workflow_work_items last had planner
	// statistics refreshed (manual or auto ANALYZE); zero when never. It
	// only reads pg_stat_user_tables.
	AnalyzedAt(ctx context.Context) (time.Time, error)
	// NonTerminalItems lists up to limit synthetic items not yet terminal.
	NonTerminalItems(ctx context.Context, limit int) ([]string, error)
	// DeletePrefixed deletes synthetic rows child-first in bounded batches
	// and returns rows deleted per table. Never TRUNCATE.
	DeletePrefixed(ctx context.Context, batch int) (map[string]int, error)
}
