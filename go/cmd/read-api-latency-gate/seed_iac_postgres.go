// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// iacFactColumns is the fact_records column list iacFactRows shapes, in
// order. Every other NOT NULL column (schema_version, collector_kind,
// fencing_token, source_confidence, is_tombstone) has a schema DEFAULT.
var iacFactColumns = []string{
	"fact_id", "scope_id", "generation_id", "fact_kind", "stable_fact_key",
	"source_system", "source_fact_key", "observed_at", "ingested_at", "payload",
}

// iacFactRows shapes facts into rows matching iacFactColumns, marshaling
// each fact's payload (iacFactPayload) to JSON for the jsonb column.
func iacFactRows(facts []SeedIaCFact, now time.Time) ([][]any, error) {
	rows := make([][]any, 0, len(facts))
	for _, f := range facts {
		payload, err := json.Marshal(iacFactPayload(f))
		if err != nil {
			return nil, fmt.Errorf("marshal payload for fact %s: %w", f.FactID, err)
		}
		rows = append(rows, []any{
			f.FactID, f.ScopeID, f.GenerationID, "content_entity", f.FactID,
			"terraform_state", f.FactID, now, now, payload,
		})
	}
	return rows, nil
}

// SeedIaCFacts bulk-inserts facts into fact_records via COPY. Callers
// typically anchor facts on one designated scope's active generation (see
// main.go's seed function) so currentInventoryCTE
// (go/internal/query/iac/inventory_postgres.go) finds them through its
// active-generation join.
func SeedIaCFacts(ctx context.Context, pool *pgxpool.Pool, facts []SeedIaCFact, now time.Time) error {
	rows, err := iacFactRows(facts, now)
	if err != nil {
		return err
	}
	if _, err := pool.CopyFrom(ctx,
		pgx.Identifier{"fact_records"},
		iacFactColumns,
		pgx.CopyFromRows(rows),
	); err != nil {
		return fmt.Errorf("seed fact_records: %w", err)
	}
	return nil
}
