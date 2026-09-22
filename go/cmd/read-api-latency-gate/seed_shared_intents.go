// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// Shared projection intents are the reducer's projection queue. On a live
// instance completed rows accumulate into the millions while only the newest
// few are pending, and the status read path's domain backlog aggregate must
// read the pending rows through the partial pending indexes rather than scan
// the table (#6794). Issue #6820: the gate did not seed this table, so a
// regression in that aggregate (a lost `completed_at IS NULL` predicate or a
// second full pass) could not move any status route's work budget. The
// corpus below is sized so that one full scan of the heap exceeds the 3x
// blocks budget of the status routes that read it, which is what makes the
// seeded-violation half of the proof fail.

const (
	// defaultSharedIntentCount is the seeded row count. Measured on
	// postgres:18 with the production schema: 2.5M rows occupy ~51k heap
	// pages, so a lost-predicate variant of the backlog aggregate adds ~51k
	// blocks per request to a status route whose GREEN work is ~19.5k blocks
	// and whose budget is 3x that; the shipped aggregate reads the pending
	// rows by index-only scan in a few hundred blocks.
	defaultSharedIntentCount = 2_500_000
	// defaultSharedIntentPendingPercent is the pending share of the corpus.
	defaultSharedIntentPendingPercent = 1
	// sharedIntentSpacing is the created_at step between consecutive rows, so
	// the corpus spans a realistic window and the pending rows are the newest.
	sharedIntentSpacing = time.Second
)

// SharedIntentPlanOptions sizes BuildSharedIntentPlan.
type SharedIntentPlanOptions struct {
	// Total is the number of shared_projection_intents rows to seed.
	Total int
	// Pending is how many of the newest rows stay uncompleted.
	Pending int
}

// SharedIntentRow is one planned shared_projection_intents row.
type SharedIntentRow struct {
	IntentID         string
	ProjectionDomain contract.Domain
	PartitionKey     string
	ScopeID          string
	AcceptanceUnitID string
	RepositoryID     string
	SourceRunID      string
	GenerationID     string
	PartitionHash    *int64
	Payload          string
	CreatedAt        time.Time
	CompletedAt      *time.Time
}

// SharedIntentPlan derives every row from its index, so a multi-million-row
// corpus is never materialized: the COPY source asks for Row(i) as it streams.
type SharedIntentPlan struct {
	total       int
	pending     int
	domains     []contract.Domain
	generations []SeedGeneration
	base        time.Time
}

// BuildSharedIntentPlan spreads opts.Total intents across every reducer
// projection domain and every generation in plan, oldest first, with the
// newest opts.Pending rows left uncompleted. It is pure and deterministic.
func BuildSharedIntentPlan(plan SeedPlan, opts SharedIntentPlanOptions) (SharedIntentPlan, error) {
	if opts.Total < 0 || opts.Pending < 0 {
		return SharedIntentPlan{}, fmt.Errorf("shared intent plan: counts must not be negative (total=%d pending=%d)", opts.Total, opts.Pending)
	}
	if opts.Pending > opts.Total {
		return SharedIntentPlan{}, fmt.Errorf("shared intent plan: pending %d exceeds total %d", opts.Pending, opts.Total)
	}
	if opts.Total == 0 {
		return SharedIntentPlan{}, nil
	}
	generations := flattenGenerations(plan)
	if len(generations) == 0 {
		return SharedIntentPlan{}, fmt.Errorf("shared intent plan: seed plan has no generations to attach intents to")
	}
	return SharedIntentPlan{
		total:       opts.Total,
		pending:     opts.Pending,
		domains:     contract.ProjectionDomains(),
		generations: generations,
		// A fixed epoch keeps the plan deterministic; the rows are still the
		// newest thing in the table because nothing else seeds this table.
		base: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC).Add(-time.Duration(opts.Total) * sharedIntentSpacing),
	}, nil
}

// Len is the number of planned rows.
func (p SharedIntentPlan) Len() int { return p.total }

// Row derives row i. Rows are created one second apart in index order, so
// index order is created_at order and the last Pending rows are the pending
// ones. Every generation and every domain is visited round-robin, and the
// partition key varies within a generation so the per-partition indexes carry
// realistic cardinality. Every tenth row is a refresh intent (the generated
// is_refresh_intent column and its indexes then have both values), and every
// sixteenth row stays on the legacy unhashed lane.
func (p SharedIntentPlan) Row(i int) SharedIntentRow {
	generation := p.generations[i%len(p.generations)]
	domain := p.domains[i%len(p.domains)]
	createdAt := p.base.Add(time.Duration(i) * sharedIntentSpacing)
	row := SharedIntentRow{
		IntentID:         fmt.Sprintf("seed-intent-%09d", i),
		ProjectionDomain: domain,
		PartitionKey:     fmt.Sprintf("repo:%s:p%d", generation.ScopeID, i%37),
		ScopeID:          generation.ScopeID,
		AcceptanceUnitID: fmt.Sprintf("%s:au-%d", generation.ScopeID, i%5),
		RepositoryID:     generation.ScopeID,
		SourceRunID:      generation.GenerationID,
		GenerationID:     generation.GenerationID,
		Payload:          `{"action":"upsert"}`,
		CreatedAt:        createdAt,
	}
	if i%10 == 0 {
		row.Payload = `{"action":"refresh"}`
	}
	if i%16 != 0 {
		hash := int64(i % 1024)
		row.PartitionHash = &hash
	}
	if i < p.total-p.pending {
		completed := createdAt.Add(30 * time.Second)
		row.CompletedAt = &completed
	}
	return row
}

// sharedIntentColumns is the COPY column list, in shared_projection_intents
// order (go/internal/storage/postgres/shared_intents.go).
var sharedIntentColumns = []string{
	"intent_id", "projection_domain", "partition_key", "scope_id", "acceptance_unit_id",
	"repository_id", "source_run_id", "generation_id", "partition_hash", "payload",
	"created_at", "completed_at",
}

// sharedIntentCopySource streams SharedIntentPlan rows to pgx CopyFrom one at
// a time.
type sharedIntentCopySource struct {
	plan SharedIntentPlan
	next int
}

func newSharedIntentCopySource(plan SharedIntentPlan) *sharedIntentCopySource {
	return &sharedIntentCopySource{plan: plan}
}

// Next reports whether another row is available and advances to it.
func (s *sharedIntentCopySource) Next() bool {
	if s.next >= s.plan.Len() {
		return false
	}
	s.next++
	return true
}

// Values returns the current row in sharedIntentColumns order.
func (s *sharedIntentCopySource) Values() ([]any, error) {
	row := s.plan.Row(s.next - 1)
	var hash any
	if row.PartitionHash != nil {
		hash = *row.PartitionHash
	}
	var completed any
	if row.CompletedAt != nil {
		completed = *row.CompletedAt
	}
	return []any{
		row.IntentID, string(row.ProjectionDomain), row.PartitionKey, row.ScopeID, row.AcceptanceUnitID,
		row.RepositoryID, row.SourceRunID, row.GenerationID, hash, []byte(row.Payload),
		row.CreatedAt, completed,
	}, nil
}

// Err reports a streaming error; deriving rows cannot fail.
func (s *sharedIntentCopySource) Err() error { return nil }

// SeedSharedIntents bulk-loads the planned shared_projection_intents rows.
// Like SeedPostgres it assumes a fresh database and inserts, never upserts.
func SeedSharedIntents(ctx context.Context, pool *pgxpool.Pool, plan SharedIntentPlan) error {
	if plan.Len() == 0 {
		return nil
	}
	copied, err := pool.CopyFrom(ctx, pgx.Identifier{"shared_projection_intents"}, sharedIntentColumns, newSharedIntentCopySource(plan))
	if err != nil {
		return fmt.Errorf("seed shared_projection_intents: %w", err)
	}
	if int(copied) != plan.Len() {
		return fmt.Errorf("seed shared_projection_intents: copied %d rows, want %d", copied, plan.Len())
	}
	return nil
}

// addSharedIntentCounts extends the exact-count read-back with the intents
// table, so a seed that writes fewer rows than planned fails the gate.
func addSharedIntentCounts(counts map[string]int, plan SharedIntentPlan) {
	counts["shared_projection_intents"] = plan.Len()
}

// sharedIntentPending derives the pending row count from the percent knob.
func sharedIntentPending(total, percent int) int {
	if total <= 0 || percent <= 0 {
		return 0
	}
	return total * percent / 100
}
