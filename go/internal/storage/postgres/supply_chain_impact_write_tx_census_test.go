// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The #11327 census guard. The impact pass takes the shared go-evidence lock as the
// last statements of its write transaction, which is deadlock-free only if the
// transaction holds nothing the Go-modules fence needs while it waits for that lock.
// The fence, holding the exclusive lock, reopens holder work items (fact_work_items) and
// detaches the go scope (ingestion_scopes, scope_generations). So no statement before
// the lock may name fact_work_items, or write or row-lock ingestion_scopes or
// scope_generations. A plain read of those tables takes no row lock and is allowed.

var (
	censusForbiddenTable = regexp.MustCompile(`(?i)\bfact_work_items\b`)
	censusScopeTables    = regexp.MustCompile(`(?i)\b(ingestion_scopes|scope_generations)\b`)
	censusScopeWrite     = regexp.MustCompile(`(?i)\b(UPDATE|INSERT\s+INTO|DELETE\s+FROM)\s+(public\.)?(ingestion_scopes|scope_generations)\b`)
	censusRowLock        = regexp.MustCompile(`(?i)\bFOR\s+(NO\s+KEY\s+UPDATE|UPDATE|SHARE|KEY\s+SHARE)\b`)
)

// censusViolation reports why one statement may not run before the go-evidence lock,
// or "" when it may. A statement that names a scope table is judged on whether it
// writes or locks; a statement naming fact_work_items never may.
func censusViolation(statement string) string {
	switch {
	case censusForbiddenTable.MatchString(statement):
		return "names fact_work_items"
	case censusScopeWrite.MatchString(statement),
		censusScopeTables.MatchString(statement) && censusRowLock.MatchString(statement):
		return "writes or row-locks ingestion_scopes or scope_generations"
	}
	return ""
}

// censusRecorder is a transaction that records every statement and answers just enough
// reads for the whole writer path to run: the scope pass-state fence decision and the
// protected policy pins return one row, every other read returns none.
type censusRecorder struct {
	statements []string
}

func (r *censusRecorder) ExecContext(_ context.Context, query string, _ ...any) (sql.Result, error) {
	r.statements = append(r.statements, query)
	return fakeResultWithRowsAffected{rowsAffected: 1}, nil
}

func (r *censusRecorder) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	r.statements = append(r.statements, query)
	switch {
	case query == supplyChainImpactPolicyPinsQuery:
		return &censusRows{rows: [][]any{{int64(7), "database-identity"}}}, nil
	case strings.Contains(query, "supply_chain_impact_scope_pass_state"):
		return &censusRows{rows: [][]any{{false, int64(1)}}}, nil
	case strings.Contains(query, "WITH retracted AS"):
		return &censusRows{rows: [][]any{{int64(0), int64(0)}}}, nil
	}
	return &censusRows{}, nil
}

func (*censusRecorder) Commit() error   { return nil }
func (*censusRecorder) Rollback() error { return nil }

type censusBeginner struct{ tx *censusRecorder }

func (b censusBeginner) Begin(context.Context) (db.Transaction, error) { return b.tx, nil }

type censusRows struct {
	rows [][]any
	at   int
}

func (r *censusRows) Next() bool   { r.at++; return r.at <= len(r.rows) }
func (r *censusRows) Err() error   { return nil }
func (r *censusRows) Close() error { return nil }
func (r *censusRows) Scan(dest ...any) error {
	for i, value := range r.rows[r.at-1] {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(value))
	}
	return nil
}

// censusWrite is a pass that exercises every statement family of the writer
// transaction: findings (fact upsert, supports, winners capture, retraction), the
// protected-scan authorization and publication, the owed marker, and Go evidence.
func censusWrite() supplychaincore.SupplyChainImpactWrite {
	intent := reducercontract.Intent{IntentID: "work-item-census"}
	factID := "fact:census"
	return supplychaincore.SupplyChainImpactWrite{
		FencingToken: 7, IntentID: "intent-census", ScopeID: "scope:census", GenerationID: "gen:census",
		SourceSystem: "vulnerability_intelligence",
		Findings: []supplychaincore.SupplyChainImpactFinding{{
			CVEID: "CVE-2026-0001", PackageID: "pkg:npm/census", Status: supplychaincore.SupplyChainImpactAffectedExact,
			RepositoryID: "repo://census/api", EvidenceFactIDs: []string{factID},
		}},
		StaticNonAdvisoryEvidenceFactIDs: []string{factID},
		ProtectedCompleteSet:             true,
		GoEvidence:                       []supplychaincore.GoEvidenceGeneration{{ScopeID: "go-reachability://repository:r_census", GenerationID: "gen:go"}},
		Owed:                             SupplyChainImpactFanOutStore{LeaseOwner: "census"}.OwedRecorder(intent),
	}
}

// runCensusPass runs the real writer over the real storage transaction and returns the
// statements it issued, in order.
func runCensusPass(t *testing.T) []string {
	t.Helper()
	recorder := &censusRecorder{}
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  SupplyChainImpactBeginner{Beginner: censusBeginner{tx: recorder}},
		Now: func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) },
	}
	// The recheck read returns no row, so the pass ends on the typed refusal: every
	// statement before the lock has run, which is the census.
	_, err := writer.WriteSupplyChainImpactFindings(context.Background(), censusWrite())
	var changed *supplychaincore.SupplyChainImpactGoEvidenceChangedError
	if err == nil || !asGoEvidenceChanged(err, &changed) {
		t.Fatalf("WriteSupplyChainImpactFindings() error = %v, want the go-evidence refusal that ends the census at the recheck", err)
	}
	return recorder.statements
}

func asGoEvidenceChanged(err error, target **supplychaincore.SupplyChainImpactGoEvidenceChangedError) bool {
	for err != nil {
		if changed, ok := err.(*supplychaincore.SupplyChainImpactGoEvidenceChangedError); ok {
			*target = changed
			return true
		}
		unwrapped, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapped.Unwrap()
	}
	return false
}

// TestImpactWriteTransactionTakesNothingTheFenceNeedsBeforeTheLock is gate (1) of the
// #11327 lock-last ruling, on the production statements: every statement the real
// writer and the real storage transaction issue before the shared go-evidence lock,
// protected-scan authorization, winners capture and supports included, passes the
// census. The vacuity checks prove the pass really ran those families.
func TestImpactWriteTransactionTakesNothingTheFenceNeedsBeforeTheLock(t *testing.T) {
	t.Parallel()

	statements := runCensusPass(t)
	lockAt := -1
	for i, statement := range statements {
		if statement == supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery {
			lockAt = i
			break
		}
	}
	if lockAt < 0 {
		t.Fatalf("no shared go-evidence lock among %d statements", len(statements))
	}
	for i, statement := range statements[:lockAt] {
		if why := censusViolation(statement); why != "" {
			t.Errorf("statement %d before the lock %s:\n%s", i, why, statement)
		}
	}
	for family, needle := range map[string]string{
		"protected authorization": authorizeImpactPublicationQuery,
		"protected publication":   publishImpactPublicationQuery,
		"fact upsert":             "INSERT INTO fact_records",
		"supports upsert":         "supply_chain_impact_finding_supports",
		"pass state":              "supply_chain_impact_scope_pass_state",
	} {
		found := false
		for _, statement := range statements[:lockAt] {
			if strings.Contains(statement, needle) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("the census pass never ran the %s statement before the lock: the guard would pass vacuously", family)
		}
	}
	// The only statement that follows the lock and names fact_work_items is the owed
	// marker; the pass ends at the failed recheck here, so assert the marker is held
	// back until after the recheck by its absence before the lock.
	for _, statement := range statements[:lockAt] {
		if statement == markSupplyChainImpactFanOutOwedQuery {
			t.Fatal("the owed marker ran before the go-evidence lock: it updates the pass's own fact_work_items row and can deadlock with a fence")
		}
	}
}

// TestCensusViolationNamesTheSeededStatements is the seeded RED/GREEN half of the guard.
func TestCensusViolationNamesTheSeededStatements(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		statement string
		want      bool
	}{
		"owed marker (fact_work_items)":     {markSupplyChainImpactFanOutOwedQuery, true},
		"select from fact_work_items":       {"SELECT 1 FROM fact_work_items WHERE stage = 'reducer'", true},
		"update the scope row":              {"UPDATE ingestion_scopes SET status = 'active' WHERE scope_id = $1", true},
		"insert a generation":               {"INSERT INTO scope_generations (generation_id) VALUES ($1)", true},
		"lock the generation row":           {"SELECT 1 FROM scope_generations WHERE scope_id = $1 FOR UPDATE", true},
		"share-lock the scope row":          {"SELECT 1 FROM ingestion_scopes WHERE scope_id = $1 FOR SHARE", true},
		"plain read of the scope pointer":   {"SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1", false},
		"plain read joining the generation": {"SELECT 1 FROM scope_generations AS g JOIN ingestion_scopes AS s ON s.scope_id = g.scope_id", false},
		"fact insert":                       {"INSERT INTO fact_records (fact_id) VALUES ($1)", false},
		"supports upsert":                   {"INSERT INTO supply_chain_impact_finding_supports (fact_id) VALUES ($1)", false},
	} {
		if got := censusViolation(tc.statement) != ""; got != tc.want {
			t.Errorf("%s: violation = %v, want %v", name, got, tc.want)
		}
	}
}

// TestProtectedPublicationFunctionsTouchNoFenceRows covers the part of the census the
// statement text cannot show: the protected-scan authorization and publication run as
// SQL functions whose bodies take the SHARE locks. Their migration text must name none
// of the rows the fence needs.
func TestProtectedPublicationFunctionsTouchNoFenceRows(t *testing.T) {
	t.Parallel()

	for _, file := range []string{"migrations/268_scanner_impact_publication.sql", "migrations/269_scanner_impact_publish.sql"} {
		body, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if why := censusViolation(string(body)); why != "" {
			t.Errorf("%s %s: the protected publication functions would hold fence rows while the pass waits for the lock", file, why)
		}
	}
	for name, body := range map[string]string{
		"seeded RED: a function that locks the scope row": "SELECT 1 FROM ingestion_scopes WHERE scope_id = x FOR SHARE;",
		"seeded RED: a function that touches the queue":   "UPDATE fact_work_items SET status = 'pending';",
	} {
		if censusViolation(body) == "" {
			t.Errorf("%s was not flagged", name)
		}
	}
	_ = fmt.Sprint
}
