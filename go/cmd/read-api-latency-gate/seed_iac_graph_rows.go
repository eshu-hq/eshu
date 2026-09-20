// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// IaCGraphNodeRow is one planned graph node, correlated by identity with a
// SeedIaCFact so go/internal/query/iac/resources.go's Postgres-then-graph
// hydration (searchHydrationMatches) can find a matching row for every
// Postgres-selected IaC inventory candidate. Without a correlated graph node,
// that hydration always disagrees and the handler 500s (issue #6797
// live-gate incident) -- this is separate from, and additional to, SeedGraph's
// anonymous bulk infraLabels nodes, which exist only to give the #6793
// per-label scan cost realistic volume and intentionally carry no uid.
type IaCGraphNodeRow struct {
	Label        string
	UID          string
	ID           string
	Name         string
	GenerationID string
	Provider     string
	ResourceType string
}

// buildIaCGraphNodeRows derives one IaCGraphNodeRow per fact, carrying
// EntityID/EntityName/GenerationID through unchanged so they match what
// Postgres' SearchActive (inventory_postgres.go) returns as an
// InventoryCandidate for the same fact.
func buildIaCGraphNodeRows(facts []SeedIaCFact) []IaCGraphNodeRow {
	rows := make([]IaCGraphNodeRow, 0, len(facts))
	for _, f := range facts {
		rows = append(rows, IaCGraphNodeRow{
			Label:        f.EntityType,
			UID:          f.EntityID,
			ID:           f.EntityID,
			Name:         f.EntityName,
			GenerationID: f.GenerationID,
			Provider:     f.Provider,
			ResourceType: f.ResourceType,
		})
	}
	return rows
}

// groupIaCGraphNodeRowsByLabel buckets rows by Label so the caller can issue
// one UNWIND CREATE per label (this gate's bulk-write convention, see
// AGENTS.md) instead of a per-row CREATE.
func groupIaCGraphNodeRowsByLabel(rows []IaCGraphNodeRow) map[string][]IaCGraphNodeRow {
	byLabel := make(map[string][]IaCGraphNodeRow, len(iacEntityTypes))
	for _, r := range rows {
		byLabel[r.Label] = append(byLabel[r.Label], r)
	}
	return byLabel
}

// batchIaCGraphNodeRows splits rows into consecutive batches of at most size
// rows each, preserving order, so the graph write sends bounded UNWIND
// parameter lists instead of one enormous one per label. A non-positive size
// yields a single batch of every row.
func batchIaCGraphNodeRows(rows []IaCGraphNodeRow, size int) [][]IaCGraphNodeRow {
	if len(rows) == 0 {
		return nil
	}
	if size <= 0 {
		return [][]IaCGraphNodeRow{rows}
	}
	batches := make([][]IaCGraphNodeRow, 0, (len(rows)+size-1)/size)
	for start := 0; start < len(rows); start += size {
		end := start + size
		if end > len(rows) {
			end = len(rows)
		}
		batches = append(batches, rows[start:end])
	}
	return batches
}
