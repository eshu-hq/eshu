// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

func (f *fakeReducerDB) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	// countFailedGenerationRepositoryScopesSQL (SeedSearchVectorScopeState):
	// report zero failed scopes so startup wiring tests, which only exercise
	// runner construction, aren't coupled to seed-count fixtures. Checked
	// first because this query also matches the broader
	// active_generation_id/ingestion_scopes substring check below.
	if strings.Contains(query, "SELECT count(*)") && strings.Contains(query, "ingestion_scopes") {
		return &fakeCountRows{value: 0}, nil
	}
	// Generation freshness check: return a row matching the intent's generation
	// so the guard treats the intent as current.
	if strings.Contains(query, "active_generation_id") && strings.Contains(query, "ingestion_scopes") {
		scopeGenID := ""
		if len(args) > 0 {
			// Look up what generation the intent carries — fake DB always reports
			// the intent's generation as active so execution proceeds.
			scopeGenID = "generation-456"
		}
		return &fakeGenerationRows{value: &scopeGenID, read: false}, nil
	}
	if strings.Contains(query, "FROM fact_records") {
		return &fakeEmptyRows{}, nil
	}
	if strings.Contains(query, "FROM scope_generations") {
		return &fakeExistsRows{value: false}, nil
	}
	// ProjectedSourceEdgeBackfiller has no count guard, so it always checks
	// backfill-state completion at startup; report "not complete".
	if strings.Contains(query, "code_value_flow_backfill_state") {
		return &fakeExistsRows{value: false}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", query)
}
