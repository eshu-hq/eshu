// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "time"

// scopeColumns is the ingestion_scopes column list scopeRows shapes, in
// order. scope_kind and partition_key carry the scope's own id/collector
// kind for a deterministic, human-inspectable seed corpus; the routes this
// gate exercises never filter on them.
var scopeColumns = []string{
	"scope_id", "scope_kind", "source_system", "source_key",
	"collector_kind", "partition_key", "observed_at", "ingested_at",
	"status", "active_generation_id",
}

// scopeRows shapes plan scopes into rows matching scopeColumns, ready for
// pgx.CopyFrom.
func scopeRows(scopes []SeedScope, now time.Time) [][]any {
	rows := make([][]any, 0, len(scopes))
	for _, s := range scopes {
		rows = append(rows, []any{
			s.ScopeID, "repository", string(s.CollectorKind), s.ScopeID,
			string(s.CollectorKind), s.ScopeID, now, now,
			"active", s.ActiveGenerationID,
		})
	}
	return rows
}

// generationColumns is the scope_generations column list generationRows
// shapes, in order.
var generationColumns = []string{
	"generation_id", "scope_id", "trigger_kind", "observed_at", "ingested_at",
	"status", "activated_at",
}

// generationRows shapes plan generations into rows matching
// generationColumns. Exactly the generation marked Active gets status
// "active" and a non-nil activated_at; every other generation for the same
// scope is "superseded" — matching the scope_generations_active_scope_idx
// unique-active-generation-per-scope constraint (migration
// 002_scope_generations.sql) so the corpus is loadable as-is.
func generationRows(generations []SeedGeneration, now time.Time) [][]any {
	rows := make([][]any, 0, len(generations))
	for _, g := range generations {
		status := "superseded"
		var activatedAt any
		if g.Active {
			status = "active"
			activatedAt = now
		}
		rows = append(rows, []any{
			g.GenerationID, g.ScopeID, "snapshot", now, now, status, activatedAt,
		})
	}
	return rows
}

// workItemColumns is the fact_work_items column list workItemRows shapes, in
// order. Every other NOT NULL column (attempt_count, conflict_domain,
// payload) has a schema DEFAULT that COPY applies automatically when a
// column is left out of the column list.
var workItemColumns = []string{
	"work_item_id", "scope_id", "generation_id", "stage", "domain",
	"status", "created_at", "updated_at",
}

// workItemRows shapes plan work items into rows matching workItemColumns.
func workItemRows(items []SeedWorkItem, now time.Time) [][]any {
	rows := make([][]any, 0, len(items))
	for _, wi := range items {
		rows = append(rows, []any{
			wi.WorkItemID, wi.ScopeID, wi.GenerationID, wi.Stage, wi.Domain,
			wi.Status, now, now,
		})
	}
	return rows
}
