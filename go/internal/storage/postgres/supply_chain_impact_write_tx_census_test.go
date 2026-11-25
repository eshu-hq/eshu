// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts/supply/chain"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// The #11327 census guard. The impact pass takes the shared go-evidence lock as the
// last statements of its write transaction, which is deadlock-free only if the
// transaction holds nothing the Go-modules fence needs while it waits for that lock.
// The fence, holding the exclusive lock, reopens holder work items (fact_work_items) and
// detaches the go scope (ingestion_scopes, scope_generations). So before the lock no
// statement may write to, row-lock or table-lock fact_work_items, or write to or
// explicitly row-lock ingestion_scopes or scope_generations; a wholly read-only statement
// may name any of them (arbiter ruling of 2026-11-24, plain reads). The rule lives in one
// place, supplychaincore.SupplyChainImpactPreLockStatementViolation, called here.
// The implicit FOR KEY SHARE a fact insert takes on its scope and generation rows is out of
// reach of statement text and is compatible with the fence (see the evidence note).

// censusViolation reports why one statement may not run before the go-evidence lock,
// or "" when it may.
func censusViolation(statement string) string {
	return supplychaincore.SupplyChainImpactPreLockStatementViolation(statement)
}

// censusRecorder is a transaction that records every statement and answers just enough
// reads for the whole writer path to run: the scope pass-state fence decision and the
// protected policy pins return one row, every other read returns none.
type censusRecorder struct {
	statements []string
	// recheckCurrent makes the Go-evidence recheck find its pair still active, so the
	// pass runs to its commit instead of ending at the typed refusal.
	recheckCurrent bool
	// candidateCurrent answers the suppression candidate read with the pass's own active
	// generation, so the suppression diff runs instead of skipping.
	candidateCurrent bool
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
	case strings.Contains(query, "(g.activated_at IS NOT NULL)") && r.candidateCurrent:
		return &censusRows{rows: [][]any{{"gen:census", "active", true, time.Date(2026, 11, 24, 11, 0, 0, 0, time.UTC), false, false}}}, nil
	case strings.Contains(query, "AS pair(scope_id, generation_id)") && r.recheckCurrent:
		return &censusRows{rows: [][]any{{"go-reachability://repository:r_census", "read committed"}}}, nil
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
	return runCensusPassWith(t, &censusRecorder{})
}

func runCensusPassWith(t *testing.T, recorder *censusRecorder) []string {
	t.Helper()
	return runCensusWriteWith(t, recorder, censusWrite())
}

func runCensusWriteWith(t *testing.T, recorder *censusRecorder, write supplychaincore.SupplyChainImpactWrite) []string {
	t.Helper()
	writer := supplychaincore.PostgresSupplyChainImpactWriter{
		DB:  SupplyChainImpactBeginner{Beginner: censusBeginner{tx: recorder}},
		Now: func() time.Time { return time.Date(2026, 11, 24, 12, 0, 0, 0, time.UTC) },
	}
	_, err := writer.WriteSupplyChainImpactFindings(context.Background(), write)
	if recorder.recheckCurrent {
		if err != nil {
			t.Fatalf("WriteSupplyChainImpactFindings() error = %v, want the pass to commit", err)
		}
		return recorder.statements
	}
	// The recheck read returns no row, so the pass ends on the typed refusal: every
	// statement before the lock has run, which is the census.
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
	// C2 removed the finding-fact upsert from the writer (claim 1), so no "fact
	// upsert" family remains on the pass to prove exercised: the supports upsert
	// below is the write family's vacuity witness now. The absence of the fact
	// write is asserted before the loop; the reject trigger (migration 357) and
	// TestNoGoFileWritesTheFindingKindToFactRecords enforce it elsewhere.
	for _, statement := range statements {
		if strings.Contains(statement, "INSERT INTO fact_records") ||
			strings.Contains(statement, "UPDATE fact_records") {
			t.Errorf("a pass statement writes fact_records after the C2 write stop:\n%s", statement)
		}
	}
	for family, needle := range map[string]string{
		"protected authorization": authorizeImpactPublicationQuery,
		"protected publication":   publishImpactPublicationQuery,
		"supports upsert":         "supply_chain_impact_finding_supports",
		"pass state":              "supply_chain_impact_scope_pass_state",
		"winners capture":         "supply_chain_impact_winners_dirty_staging",
		"retraction":              "WITH retracted AS",
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

// censusCandidateRead runs the suppression-family pass and returns the production
// candidate read it captured, so the seeded variants are derived from a real statement.
func censusCandidateRead(t *testing.T) string {
	t.Helper()
	for _, statement := range runSuppressionCensusPass(t) {
		if strings.Contains(statement, "(g.activated_at IS NOT NULL)") {
			return statement
		}
	}
	t.Fatal("the suppression pass never ran its candidate read")
	return ""
}

// TestCensusViolationNamesTheSeededStatements is the seeded RED/GREEN half of the guard.
// The variants of the production candidate read are captured from a real pass.
func TestCensusViolationNamesTheSeededStatements(t *testing.T) {
	t.Parallel()

	candidates := censusCandidateRead(t)
	reopen := reopenSupplyChainImpactHolderQuery
	for name, tc := range map[string]struct {
		statement string
		want      bool
	}{
		"production candidate read":         {candidates, false},
		"plain queue read":                  {"SELECT 1 FROM fact_work_items WHERE stage = 'reducer'", false},
		"plain read of the scope pointer":   {"SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $1", false},
		"plain read joining the generation": {"SELECT 1 FROM scope_generations AS g JOIN ingestion_scopes AS s ON s.scope_id = g.scope_id", false},
		"fact insert":                       {"INSERT INTO fact_records (fact_id) VALUES ($1)", false},
		"supports upsert":                   {"INSERT INTO supply_chain_impact_finding_supports (fact_id) VALUES ($1)", false},
		"candidates FOR UPDATE":             {candidates + " FOR UPDATE", true},
		"candidates FOR NO KEY UPDATE OF w": {candidates + " FOR NO KEY UPDATE OF w", true},
		"candidates FOR SHARE":              {candidates + " FOR SHARE", true},
		"candidates FOR KEY SHARE":          {candidates + " FOR KEY SHARE", true},
		"candidates FOR UPDATE SKIP LOCKED": {candidates + " FOR UPDATE SKIP LOCKED", true},
		"owed marker":                       {markSupplyChainImpactFanOutOwedQuery, true},
		"holder reopen":                     {reopen, true},
		"data-modifying CTE over the queue": {"WITH m AS (UPDATE fact_work_items SET status = 'x' RETURNING 1) SELECT 1 FROM m", true},
		"insert into the queue":             {"INSERT INTO fact_work_items (work_item_id) VALUES ($1)", true},
		"delete from the queue":             {"DELETE FROM fact_work_items WHERE work_item_id = $1", true},
		"queue table lock":                  {"LOCK TABLE fact_work_items IN EXCLUSIVE MODE", true},
		"update the scope row":              {"UPDATE ingestion_scopes SET status = 'active' WHERE scope_id = $1", true},
		"insert a generation":               {"INSERT INTO scope_generations (generation_id) VALUES ($1)", true},
		"lock the generation row":           {"SELECT 1 FROM scope_generations WHERE scope_id = $1 FOR UPDATE", true},
		"share-lock the scope row":          {"SELECT 1 FROM ingestion_scopes WHERE scope_id = $1 FOR SHARE", true},
	} {
		if got := censusViolation(tc.statement) != ""; got != tc.want {
			t.Errorf("%s: violation = %v, want %v", name, got, tc.want)
		}
	}
}

// runSuppressionCensusPass records a pass whose scope fact kinds include a suppression and
// whose candidate read is answered with the pass's own active generation, so the
// suppression diff runs for real.
func runSuppressionCensusPass(t *testing.T) []string {
	t.Helper()
	write := censusWrite()
	write.ScopeFactKinds = []string{chain.VulnerabilitySuppressionFactKind}
	return runCensusWriteWith(t, &censusRecorder{candidateCurrent: true}, write)
}

// TestImpactWriteTransactionSuppressionFamilyCensus runs the suppression diff inside the
// pass transaction and applies the census. The diff's candidate read has an EXISTS over
// fact_work_items; it is wholly read-only, so it passes, and it must be the only pre-lock
// statement that names fact_work_items. The vacuity checks prove the diff ran: the
// candidate read, the statement-timeout set, a diff read and the RESET all precede the lock.
func TestImpactWriteTransactionSuppressionFamilyCensus(t *testing.T) {
	t.Parallel()

	statements := runSuppressionCensusPass(t)
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
	before := statements[:lockAt]
	for i, statement := range before {
		if why := censusViolation(statement); why != "" {
			t.Errorf("statement %d before the lock %s:\n%s", i, why, statement)
		}
	}
	for family, needle := range map[string]string{
		"candidate read": "(g.activated_at IS NOT NULL)",
		"timeout set":    "set_config('statement_timeout'",
		"diff read":      "'" + chain.VulnerabilitySuppressionFactKind + "'",
		"timeout reset":  "RESET statement_timeout",
	} {
		found := false
		for _, statement := range before {
			found = found || strings.Contains(statement, needle)
		}
		if !found {
			t.Errorf("the suppression pass never ran its %s before the lock: the census would pass vacuously", family)
		}
	}
	naming := 0
	for _, statement := range before {
		if strings.Contains(statement, "fact_work_items") {
			naming++
			if !strings.Contains(statement, "(g.activated_at IS NOT NULL)") {
				t.Errorf("a pre-lock statement other than the candidate read names fact_work_items:\n%s", statement)
			}
		}
	}
	if naming != 1 {
		t.Fatalf("pre-lock statements naming fact_work_items = %d, want exactly the candidate read", naming)
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
		// Function bodies keep the strict form: they may not name fact_work_items at all.
		if strings.Contains(strings.ToLower(string(body)), "fact_work_items") {
			t.Errorf("%s names fact_work_items: the protected publication functions would touch the fence's rows while the pass waits for the lock", file)
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
}

// TestImpactWritePassFootprintEndsWithLockRecheckMarker records a changed pass that runs
// to its commit and pins the tail of its footprint exactly: the shared lock, the recheck,
// the owed marker, then nothing. The census above covers what comes before the lock.
func TestImpactWritePassFootprintEndsWithLockRecheckMarker(t *testing.T) {
	t.Parallel()

	statements := runCensusPassWith(t, &censusRecorder{recheckCurrent: true})
	n := len(statements)
	if n < 3 ||
		statements[n-3] != supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery ||
		!strings.Contains(statements[n-2], "AS pair(scope_id, generation_id)") ||
		statements[n-1] != markSupplyChainImpactFanOutOwedQuery {
		t.Fatalf("the pass footprint ends %q, want the shared lock, the recheck and the owed marker", statements[max(0, n-3):])
	}
	locks := 0
	for _, statement := range statements {
		if statement == supplychaincore.SupplyChainImpactGoEvidenceSharedLockQuery {
			locks++
		}
	}
	if locks != 1 {
		t.Fatalf("shared locks = %d, want one for the one go scope", locks)
	}
	// The core checker, with the production marker text, accepts the recorded footprint.
	if why := supplychaincore.SupplyChainImpactGoEvidenceEpilogueViolation(statements, markSupplyChainImpactFanOutOwedQuery); why != "" {
		t.Fatalf("the recorded footprint breaks the core epilogue contract: %s", why)
	}
	// Seeded: the same statements with the marker moved before the lock are refused.
	moved := append([]string(nil), statements[:n-3]...)
	moved = append(moved, statements[n-1], statements[n-3], statements[n-2])
	if supplychaincore.SupplyChainImpactGoEvidenceEpilogueViolation(moved, markSupplyChainImpactFanOutOwedQuery) == "" {
		t.Fatal("the core checker accepted the marker moved before the lock")
	}
}
