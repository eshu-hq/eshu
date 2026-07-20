// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

func TestCodeReachabilitySchemaSQL(t *testing.T) {
	sqlStr := CodeReachabilitySchemaSQL()
	for _, want := range []string{
		"CREATE TABLE IF NOT EXISTS code_reachability_rows",
		"CREATE TABLE IF NOT EXISTS code_reachability_repository_watermarks",
		"truncated BOOLEAN NOT NULL DEFAULT FALSE",
		"ADD COLUMN IF NOT EXISTS truncated",
		"PRIMARY KEY (scope_id, generation_id, repository_id, root_entity_id, entity_id)",
		"PRIMARY KEY (scope_id, generation_id, repository_id)",
		"code_reachability_latest_lookup_idx",
		"code_reachability_entity_lookup_idx",
		"code_reachability_root_idx",
	} {
		if !strings.Contains(sqlStr, want) {
			t.Fatalf("CodeReachabilitySchemaSQL() missing %q:\n%s", want, sqlStr)
		}
	}
}

func TestCodeReachabilityStoreUpsertBatchesRows(t *testing.T) {
	now := time.Date(2026, 6, 17, 3, 0, 0, 0, time.UTC)
	db := newCodeReachabilityTestDB()
	store := NewCodeReachabilityStore(db)
	err := store.Upsert(context.Background(), []reducer.CodeReachabilityRow{{
		ScopeID:             "scope-1",
		GenerationID:        "generation-1",
		RepositoryID:        "repo-1",
		RootEntityID:        "entity:root",
		EntityID:            "entity:leaf",
		Depth:               2,
		State:               reducer.CodeReachabilityStateReachable,
		Confidence:          0.99,
		MinResolutionMethod: "scip",
		Evidence:            []string{"entity:root CALLS entity:leaf"},
		RootKinds:           []string{"go.main_function"},
		ObservedAt:          now,
		UpdatedAt:           now,
	}})
	if err != nil {
		t.Fatalf("Upsert() error = %v", err)
	}

	row, ok := db.rows["scope-1|generation-1|repo-1|entity:root|entity:leaf"]
	if !ok {
		t.Fatalf("stored rows = %#v, want entity:leaf", db.rows)
	}
	if got, want := row.MinResolutionMethod, "scip"; got != want {
		t.Fatalf("method = %q, want %q", got, want)
	}
}

func TestCodeReachabilityStoreReplaceRepositoryRowsDeletesStaleRows(t *testing.T) {
	now := time.Date(2026, 6, 17, 3, 0, 0, 0, time.UTC)
	db := newCodeReachabilityTestDB()
	db.rows["scope-1|generation-1|repo-1|entity:root|entity:stale"] = codeReachabilityStoredRow{
		ScopeID:             "scope-1",
		GenerationID:        "generation-1",
		RepositoryID:        "repo-1",
		RootEntityID:        "entity:root",
		EntityID:            "entity:stale",
		Depth:               2,
		State:               reducer.CodeReachabilityStateReachable,
		Confidence:          0.99,
		MinResolutionMethod: "scip",
		Evidence:            []string{"stale"},
		RootKinds:           []string{"go.main_function"},
		ObservedAt:          now,
		UpdatedAt:           now,
	}
	store := NewCodeReachabilityStore(db)
	err := store.ReplaceRepositoryRows(
		context.Background(),
		"scope-1",
		"generation-1",
		"repo-1",
		[]reducer.CodeReachabilityRow{{
			ScopeID:             "scope-1",
			GenerationID:        "generation-1",
			RepositoryID:        "repo-1",
			RootEntityID:        "entity:root",
			EntityID:            "entity:live",
			Depth:               1,
			State:               reducer.CodeReachabilityStateReachable,
			Confidence:          0.99,
			MinResolutionMethod: "scip",
			Evidence:            []string{"entity:root CALLS entity:live"},
			RootKinds:           []string{"go.main_function"},
			ObservedAt:          now,
			UpdatedAt:           now,
		}},
		nil,
		now.Add(time.Minute),
		false,
	)
	if err != nil {
		t.Fatalf("ReplaceRepositoryRows() error = %v", err)
	}
	if _, ok := db.rows["scope-1|generation-1|repo-1|entity:root|entity:stale"]; ok {
		t.Fatalf("stale row was not deleted: %#v", db.rows)
	}
	if _, ok := db.rows["scope-1|generation-1|repo-1|entity:root|entity:live"]; !ok {
		t.Fatalf("live replacement row missing: %#v", db.rows)
	}
	if got, want := db.watermarks["scope-1|generation-1|repo-1"].UpdatedAt, now.Add(time.Minute); !got.Equal(want) {
		t.Fatalf("watermark updated_at = %v, want %v", got, want)
	}
	if got, want := db.watermarks["scope-1|generation-1|repo-1"].Truncated, false; got != want {
		t.Fatalf("watermark truncated = %v, want %v", got, want)
	}
}

func TestCodeReachabilityStoreReplaceRepositoryRowsRecordsEmptyWatermark(t *testing.T) {
	now := time.Date(2026, 6, 17, 4, 0, 0, 0, time.UTC)
	db := newCodeReachabilityTestDB()
	store := NewCodeReachabilityStore(db)
	err := store.ReplaceRepositoryRows(
		context.Background(),
		"scope-empty",
		"generation-empty",
		"repo-empty",
		nil,
		nil,
		now,
		true,
	)
	if err != nil {
		t.Fatalf("ReplaceRepositoryRows() error = %v", err)
	}
	if len(db.rows) != 0 {
		t.Fatalf("rows = %#v, want empty replacement", db.rows)
	}
	if got, want := db.watermarks["scope-empty|generation-empty|repo-empty"].UpdatedAt, now; !got.Equal(want) {
		t.Fatalf("watermark updated_at = %v, want %v", got, want)
	}
	if got, want := db.watermarks["scope-empty|generation-empty|repo-empty"].Truncated, true; got != want {
		t.Fatalf("watermark truncated = %v, want %v", got, want)
	}
	// #5376 P1: the runner stamps the current verdict schema epoch even for an
	// empty (no-Ruby / zero-verdict) replacement, so the upgrade-backfill
	// predicate cannot loop on it.
	if got, want := db.watermarks["scope-empty|generation-empty|repo-empty"].VerdictEpoch, CodeReachabilityVerdictSchemaEpoch; got != want {
		t.Fatalf("watermark verdict_schema_epoch = %d, want %d", got, want)
	}
}

// TestCodeReachabilityStoreReplaceRepositoryRowsReplacesVerdicts proves the
// #5376 verdict rows are written and stale verdicts for the same partition are
// cleared in the same replacement, keeping the verdict set consistent with the
// reachability rows written in the same transaction.
func TestCodeReachabilityStoreReplaceRepositoryRowsReplacesVerdicts(t *testing.T) {
	now := time.Date(2026, 7, 20, 3, 0, 0, 0, time.UTC)
	db := newCodeReachabilityTestDB()
	db.verdicts["scope-1|generation-1|repo-1|entity:stale|ruby.rails_controller_action"] = codeRootVerdictStoredRow{
		ScopeID: "scope-1", GenerationID: "generation-1", RepositoryID: "repo-1",
		EntityID: "entity:stale", RootKind: "ruby.rails_controller_action", Verdict: "downgraded",
		Basis: []byte("{}"), ObservedAt: now, UpdatedAt: now,
	}
	store := NewCodeReachabilityStore(db)
	err := store.ReplaceRepositoryRows(
		context.Background(),
		"scope-1", "generation-1", "repo-1",
		nil,
		[]reducer.CodeRootVerdictRow{{
			ScopeID: "scope-1", GenerationID: "generation-1", RepositoryID: "repo-1",
			EntityID: "entity:live", RootKind: "ruby.rails_controller_action",
			Verdict: reducer.CodeRootVerdictDowngraded,
			Basis: reducer.CodeRootVerdictBasis{
				Chain:    []string{"OrdersController", "ApplicationRecord", "ActiveRecord::Base"},
				Terminal: "unresolved_base:ActiveRecord::Base",
				Reason:   "unresolved_non_controller",
			},
			ObservedAt: now, UpdatedAt: now,
		}},
		now.Add(time.Minute),
		false,
	)
	if err != nil {
		t.Fatalf("ReplaceRepositoryRows() error = %v", err)
	}
	if _, ok := db.verdicts["scope-1|generation-1|repo-1|entity:stale|ruby.rails_controller_action"]; ok {
		t.Fatalf("stale verdict was not deleted: %#v", db.verdicts)
	}
	live, ok := db.verdicts["scope-1|generation-1|repo-1|entity:live|ruby.rails_controller_action"]
	if !ok {
		t.Fatalf("live verdict row missing: %#v", db.verdicts)
	}
	if live.Verdict != "downgraded" {
		t.Fatalf("verdict = %q, want downgraded", live.Verdict)
	}
	var basis map[string]any
	if err := json.Unmarshal(live.Basis, &basis); err != nil {
		t.Fatalf("basis is not valid JSON: %v", err)
	}
	if basis["reason"] != "unresolved_non_controller" {
		t.Fatalf("basis reason = %v, want unresolved_non_controller", basis["reason"])
	}
}

func TestCodeReachabilityStoreListLatestByEntitiesUsesActiveGeneration(t *testing.T) {
	db := newCodeReachabilityTestDB()
	now := time.Date(2026, 6, 17, 3, 0, 0, 0, time.UTC)
	db.rows["scope-1|generation-1|repo-1|entity:root|entity:leaf"] = codeReachabilityStoredRow{
		ScopeID:             "scope-1",
		GenerationID:        "generation-1",
		RepositoryID:        "repo-1",
		RootEntityID:        "entity:root",
		EntityID:            "entity:leaf",
		Depth:               2,
		State:               "reachable",
		Confidence:          0.99,
		MinResolutionMethod: "scip",
		Evidence:            []string{"entity:root CALLS entity:leaf"},
		RootKinds:           []string{"go.main_function"},
		ObservedAt:          now,
		UpdatedAt:           now.Add(time.Minute),
	}
	store := NewCodeReachabilityStore(db)
	got, err := store.ListLatestByEntities(context.Background(), "repo-1", []string{"entity:leaf"})
	if err != nil {
		t.Fatalf("ListLatestByEntities() error = %v", err)
	}
	if got["entity:leaf"].RootEntityID != "entity:root" {
		t.Fatalf("root = %q, want entity:root", got["entity:leaf"].RootEntityID)
	}
	if !strings.Contains(db.lastQuery, "JOIN ingestion_scopes AS scope") {
		t.Fatalf("query did not use active generation join:\n%s", db.lastQuery)
	}
}

type codeReachabilityTestDB struct {
	rows       map[string]codeReachabilityStoredRow
	verdicts   map[string]codeRootVerdictStoredRow
	watermarks map[string]codeReachabilityWatermark
	lastQuery  string
}

type codeRootVerdictStoredRow struct {
	ScopeID      string
	GenerationID string
	RepositoryID string
	EntityID     string
	RootKind     string
	Verdict      string
	Basis        []byte
	ObservedAt   time.Time
	UpdatedAt    time.Time
}

type codeReachabilityWatermark struct {
	UpdatedAt    time.Time
	Truncated    bool
	VerdictEpoch int
}

type codeReachabilityStoredRow struct {
	ScopeID             string
	GenerationID        string
	RepositoryID        string
	RootEntityID        string
	EntityID            string
	Depth               int
	State               string
	Confidence          float64
	MinResolutionMethod string
	Evidence            []string
	RootKinds           []string
	ObservedAt          time.Time
	UpdatedAt           time.Time
}

func newCodeReachabilityTestDB() *codeReachabilityTestDB {
	return &codeReachabilityTestDB{
		rows:       map[string]codeReachabilityStoredRow{},
		verdicts:   map[string]codeRootVerdictStoredRow{},
		watermarks: map[string]codeReachabilityWatermark{},
	}
}

func (db *codeReachabilityTestDB) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	switch {
	case strings.Contains(query, "DELETE FROM code_reachability_rows"):
		scopeID := args[0].(string)
		generationID := args[1].(string)
		repositoryID := args[2].(string)
		for key, row := range db.rows {
			if row.ScopeID == scopeID && row.GenerationID == generationID && row.RepositoryID == repositoryID {
				delete(db.rows, key)
			}
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "INSERT INTO code_reachability_rows"):
		const columnsPerRow = 13
		for i := 0; i < len(args); i += columnsPerRow {
			evidence, err := decodeStringArrayJSON(args[i+9])
			if err != nil {
				return nil, err
			}
			rootKinds, err := decodeStringArrayJSON(args[i+10])
			if err != nil {
				return nil, err
			}
			row := codeReachabilityStoredRow{
				ScopeID:             args[i+0].(string),
				GenerationID:        args[i+1].(string),
				RepositoryID:        args[i+2].(string),
				RootEntityID:        args[i+3].(string),
				EntityID:            args[i+4].(string),
				Depth:               args[i+5].(int),
				State:               args[i+6].(string),
				Confidence:          args[i+7].(float64),
				MinResolutionMethod: args[i+8].(string),
				Evidence:            evidence,
				RootKinds:           rootKinds,
				ObservedAt:          args[i+11].(time.Time),
				UpdatedAt:           args[i+12].(time.Time),
			}
			db.rows[strings.Join([]string{row.ScopeID, row.GenerationID, row.RepositoryID, row.RootEntityID, row.EntityID}, "|")] = row
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "DELETE FROM code_root_verdicts"):
		scopeID := args[0].(string)
		generationID := args[1].(string)
		repositoryID := args[2].(string)
		for key, row := range db.verdicts {
			if row.ScopeID == scopeID && row.GenerationID == generationID && row.RepositoryID == repositoryID {
				delete(db.verdicts, key)
			}
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "INSERT INTO code_root_verdicts"):
		const columnsPerRow = 9
		for i := 0; i < len(args); i += columnsPerRow {
			row := codeRootVerdictStoredRow{
				ScopeID:      args[i+0].(string),
				GenerationID: args[i+1].(string),
				RepositoryID: args[i+2].(string),
				EntityID:     args[i+3].(string),
				RootKind:     args[i+4].(string),
				Verdict:      args[i+5].(string),
				Basis:        args[i+6].([]byte),
				ObservedAt:   args[i+7].(time.Time),
				UpdatedAt:    args[i+8].(time.Time),
			}
			db.verdicts[strings.Join([]string{row.ScopeID, row.GenerationID, row.RepositoryID, row.EntityID, row.RootKind}, "|")] = row
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "INSERT INTO code_reachability_repository_watermarks"):
		scopeID := args[0].(string)
		generationID := args[1].(string)
		repositoryID := args[2].(string)
		truncated := args[3].(bool)
		updatedAt := args[4].(time.Time)
		verdictEpoch := args[5].(int)
		db.watermarks[strings.Join([]string{scopeID, generationID, repositoryID}, "|")] = codeReachabilityWatermark{
			UpdatedAt:    updatedAt,
			Truncated:    truncated,
			VerdictEpoch: verdictEpoch,
		}
		return sharedIntentResult{}, nil
	case strings.Contains(query, "CREATE TABLE") || strings.Contains(query, "CREATE INDEX"):
		return sharedIntentResult{}, nil
	default:
		return nil, fmt.Errorf("unexpected exec query: %s", query)
	}
}

func (db *codeReachabilityTestDB) QueryContext(_ context.Context, query string, args ...any) (Rows, error) {
	db.lastQuery = query
	if !strings.Contains(query, "FROM code_reachability_rows") {
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
	repoID := args[0].(string)
	entityIDs := make(map[string]struct{}, len(args)-1)
	for _, arg := range args[1:] {
		entityIDs[arg.(string)] = struct{}{}
	}

	var matches [][]any
	for _, row := range db.rows {
		if row.RepositoryID != repoID {
			continue
		}
		if _, ok := entityIDs[row.EntityID]; !ok {
			continue
		}
		evidence, _ := json.Marshal(row.Evidence)
		rootKinds, _ := json.Marshal(row.RootKinds)
		matches = append(matches, []any{
			row.ScopeID, row.GenerationID, row.RepositoryID, row.RootEntityID,
			row.EntityID, row.Depth, row.State, row.Confidence,
			row.MinResolutionMethod, evidence, rootKinds, row.ObservedAt, row.UpdatedAt,
		})
	}
	sort.Slice(matches, func(i, j int) bool {
		return matches[i][4].(string) < matches[j][4].(string)
	})
	return &codeReachabilityRows{data: matches, idx: -1}, nil
}

type codeReachabilityRows struct {
	data [][]any
	idx  int
}

func (r *codeReachabilityRows) Next() bool {
	r.idx++
	return r.idx < len(r.data)
}

func (r *codeReachabilityRows) Scan(dest ...any) error {
	if r.idx < 0 || r.idx >= len(r.data) {
		return fmt.Errorf("scan out of range")
	}
	row := r.data[r.idx]
	if len(dest) != len(row) {
		return fmt.Errorf("scan: got %d dest, have %d cols", len(dest), len(row))
	}
	for i, val := range row {
		switch d := dest[i].(type) {
		case *string:
			*d = val.(string)
		case *int:
			*d = val.(int)
		case *float64:
			*d = val.(float64)
		case *[]byte:
			*d = val.([]byte)
		case *time.Time:
			*d = val.(time.Time)
		default:
			return fmt.Errorf("unsupported scan dest type %T", dest[i])
		}
	}
	return nil
}

func (r *codeReachabilityRows) Err() error   { return nil }
func (r *codeReachabilityRows) Close() error { return nil }
