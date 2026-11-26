// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The transaction footprint of one changed impact pass, by recording (#10880 PR-C2; arbiter-test-reality
// section (a)3): the real writer, the real transaction wrapper and the real statements run against a
// recording transaction that answers each statement with the minimum a pass needs, and the set of
// tables the pass writes and the locks it takes is derived from what was actually issued, not from reading
// the code. The pin is a claim of C2: a pass writes no finding fact row. Roles: none (a recording
// transaction); N: one changed pass of two findings.

// footprintTx records every statement of a transaction and scripts its answers.
type footprintTx struct {
	mu         sync.Mutex
	statements []string
}

func (t *footprintTx) record(query string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.statements = append(t.statements, query)
}

func (t *footprintTx) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	t.record(query)
	return fakeResultWithRowsAffected{rowsAffected: 1}, nil
}

func (t *footprintTx) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	t.record(query)
	switch {
	case strings.Contains(query, "INSERT INTO supply_chain_impact_finding_supports"):
		// One inserted row per bound fact id.
		return &flagRows{flags: make([]bool, len(upsertArgStrings(nil, args[0])))}, nil
	case strings.Contains(query, "WITH retracted AS"):
		return &scriptedRows{rows: [][]any{{int64(1), int64(1)}}}, nil
	case strings.Contains(query, "INSERT INTO supply_chain_impact_scope_pass_state"):
		return &scriptedRows{rows: [][]any{{false, int64(1)}}}, nil
	}
	return &scriptedRows{}, nil
}

func (t *footprintTx) Commit() error   { return nil }
func (t *footprintTx) Rollback() error { return nil }

type footprintBeginner struct{ tx *footprintTx }

func (b footprintBeginner) Begin(context.Context) (db.Transaction, error) { return b.tx, nil }

// scriptedRows is a db.Rows over literal rows of bool and int64 cells.
type scriptedRows struct {
	rows [][]any
	at   int
}

func (r *scriptedRows) Next() bool   { r.at++; return r.at <= len(r.rows) }
func (r *scriptedRows) Err() error   { return nil }
func (r *scriptedRows) Close() error { return nil }
func (r *scriptedRows) Scan(dest ...any) error {
	row := r.rows[r.at-1]
	if len(dest) != len(row) {
		return fmt.Errorf("scan %d cells into %d destinations", len(row), len(dest))
	}
	for i, cell := range row {
		switch d := dest[i].(type) {
		case *bool:
			*d = cell.(bool)
		case *int64:
			*d = cell.(int64)
		case *string:
			*d = cell.(string)
		default:
			return fmt.Errorf("scriptedRows: unsupported destination %T", dest[i])
		}
	}
	return nil
}

var footprintWrite = regexp.MustCompile(`(?is)(INSERT\s+INTO|DELETE\s+FROM|\bUPDATE)\s+([a-z_][a-z0-9_]*)`)

// footprintOf returns the sorted set of tables the statements write and the lock forms they take. An
// UPDATE that is part of ON CONFLICT DO UPDATE or FOR UPDATE names no table and is skipped.
func footprintOf(statements []string) (writes, locks []string) {
	written := map[string]bool{}
	locked := map[string]bool{}
	for _, statement := range statements {
		for _, match := range footprintWrite.FindAllStringSubmatchIndex(statement, -1) {
			verb := strings.ToUpper(strings.Fields(statement[match[2]:match[3]])[0])
			if verb == "UPDATE" {
				before := strings.ToUpper(strings.TrimSpace(statement[:match[2]]))
				if strings.HasSuffix(before, "DO") || strings.HasSuffix(before, "FOR") {
					continue
				}
			}
			written[strings.ToLower(statement[match[4]:match[5]])] = true
		}
		if strings.Contains(statement, "pg_advisory_xact_lock") {
			locked["advisory lock (transaction scoped)"] = true
		}
		if strings.Contains(strings.ToUpper(statement), "FOR UPDATE") {
			locked["row lock (FOR UPDATE)"] = true
		}
		if strings.Contains(strings.ToUpper(statement), "LOCK TABLE") {
			locked["table lock"] = true
		}
	}
	for table := range written {
		writes = append(writes, table)
	}
	for lock := range locked {
		locks = append(locks, lock)
	}
	sort.Strings(writes)
	sort.Strings(locks)
	return writes, locks
}

// footprintWrite builds one changed pass: two findings, a full (not partial) pass so the retraction
// runs, and the real owed recorder.
func footprintPass() supplychaincore.SupplyChainImpactWrite {
	return supplychaincore.SupplyChainImpactWrite{
		FencingToken: 7,
		IntentID:     "intent-footprint",
		ScopeID:      "vuln-intel://footprint",
		GenerationID: "generation-footprint",
		SourceSystem: "vulnerability_intelligence",
		Findings: []supplychaincore.SupplyChainImpactFinding{
			{CVEID: "CVE-2026-0001", PackageID: "pkg:npm/footprint", Status: supplychaincore.SupplyChainImpactAffectedExact, RepositoryID: "repository:footprint", CanonicalWrites: 1},
			{CVEID: "CVE-2026-0002", PackageID: "pkg:npm/footprint", Status: supplychaincore.SupplyChainImpactAffectedExact, RepositoryID: "repository:footprint", CanonicalWrites: 1},
		},
		Owed: supplyChainImpactOwedRecorder{workItemID: "intent-footprint", leaseOwner: "reducer-1", claimedAt: time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC)},
	}
}

// pinnedChangedPassWrites is the tables one changed pass writes. It does not contain fact_records:
// the write stop (#10880 PR-C2) moved the finding to the supports table. fact_work_items is the
// pass's own owed-marker update, inside the same transaction.
// supply_chain_impact_finding_first_seen is #11031's per-pass first-seen claim,
// orthogonal to the write stop.
var pinnedChangedPassWrites = []string{
	"fact_work_items",
	"supply_chain_impact_finding_first_seen",
	"supply_chain_impact_finding_supports",
	"supply_chain_impact_retracted_finding_keys",
	"supply_chain_impact_scope_pass_state",
	"supply_chain_impact_winners_dirty_staging",
	"supply_chain_impact_write_admission",
}

// TestChangedImpactPassFootprintByRecording runs the real writer through one changed pass over a recording
// transaction and pins the set of tables it writes and the locks it takes, with its seeded violation: a
// statement that writes fact_records, or a table added to the footprint, fails the pin.
func TestChangedImpactPassFootprintByRecording(t *testing.T) {
	t.Parallel()

	tx := &footprintTx{}
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  SupplyChainImpactBeginner{Beginner: footprintBeginner{tx: tx}},
		Now: func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) },
	}
	if _, err := writer.WriteSupplyChainImpactFindings(context.Background(), footprintPass()); err != nil {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v", err)
	}
	writes, locks := footprintOf(tx.statements)
	if !reflect.DeepEqual(writes, pinnedChangedPassWrites) {
		t.Fatalf("a changed pass writes %v, want %v", writes, pinnedChangedPassWrites)
	}
	if want := []string{"advisory lock (transaction scoped)"}; !reflect.DeepEqual(locks, want) {
		t.Fatalf("a changed pass takes locks %v, want %v", locks, want)
	}

	// Seeded violation: the same pass plus a fact write and a table lock must not match the pin.
	planted := append(append([]string(nil), tx.statements...),
		"INSERT INTO fact_records (fact_id) VALUES ($1)",
		"UPDATE fact_records SET is_tombstone = TRUE WHERE fact_id = $1",
		"LOCK TABLE supply_chain_impact_finding_supports IN SHARE MODE",
	)
	plantedWrites, plantedLocks := footprintOf(planted)
	if reflect.DeepEqual(plantedWrites, pinnedChangedPassWrites) || !footprintHas(plantedLocks, "table lock") {
		t.Fatalf("the planted fact write and table lock were not reported: writes %v locks %v", plantedWrites, plantedLocks)
	}
}

func footprintHas(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
