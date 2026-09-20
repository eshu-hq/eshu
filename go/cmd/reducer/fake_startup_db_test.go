// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
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
	// Graph writer-shape marker (#6868): report the binary's own shape as
	// already applied so startup wiring tests, which only exercise runner
	// construction, short-circuit before the upgrade claim instead of
	// coupling to refinalize fixtures. The real upgrade path is covered by
	// the writershape marker and recovery handler tests.
	if strings.Contains(query, "FROM graph_writer_shape") && strings.Contains(query, "applied_version") {
		if f.writerShapeUpgradePending {
			return &fakeAppliedVersionRows{value: 0}, nil
		}
		return &fakeAppliedVersionRows{value: sourcecypher.GraphWriterShapeVersion}, nil
	}
	return nil, fmt.Errorf("unexpected query: %s", query)
}

// fakeAppliedVersionRows returns a single int row, modeling the applied
// writer-shape version lookup the reducer issues at startup (#6868).
type fakeAppliedVersionRows struct {
	value int
	read  bool
}

func (r *fakeAppliedVersionRows) Next() bool {
	if r.read {
		return false
	}
	r.read = true
	return true
}

func (r *fakeAppliedVersionRows) Scan(dest ...any) error {
	if len(dest) != 1 {
		return fmt.Errorf("scan: got %d dest, want 1", len(dest))
	}
	v, ok := dest[0].(*int)
	if !ok {
		return fmt.Errorf("unsupported scan dest type %T", dest[0])
	}
	*v = r.value
	return nil
}

func (r *fakeAppliedVersionRows) Err() error   { return nil }
func (r *fakeAppliedVersionRows) Close() error { return nil }
