// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
)

const (
	// supplyChainImpactRetiredOracleHash pins the frozen oracle below.
	supplyChainImpactRetiredOracleHash = "1dd09480ccdebc55d41049fc20e8d551c951356f637901c8d134cd65499707f3"
)

// supplyChainImpactRetiredOracleQuery is the pre-#8880
// listActiveSupplyChainImpactFactsQuery, byte for byte as rendered at
// eb62fa3056 (facts_active_supply_chain_impact.go blob
// ddcca90366bd96d1d941f188ccff497c3e3a9ce7). The production constant is
// retired by #8880, so the differential cannot derive its oracle from shipped
// code; it is frozen here on purpose and its hash is pinned.
//
//go:embed testdata/list_active_supply_chain_impact_facts_eb62fa3.sql
var supplyChainImpactRetiredOracleQuery string

// supplyChainImpactOracleRow is one row of the retired statement in the
// projection the differential compares.
type supplyChainImpactOracleRow struct {
	FactID, ScopeID, GenerationID, FactKind, StableFactKey string
	Payload                                                string
}

// supplyChainImpactOracleResult is the full retired-statement answer for one
// call: core rows, identity rows, and suppression rows in load order.
type supplyChainImpactOracleResult struct {
	Rows      []supplyChainImpactOracleRow
	Truncated bool
}

// supplyChainImpactRetiredOracleArgs binds the 19 retired placeholders the
// way loadActiveSupplyChainImpactFactPagePair bound them at eb62fa3056.
func supplyChainImpactRetiredOracleArgs(
	filter supplychaincore.SupplyChainImpactFactFilter,
	cursor string,
	cursorIsSuppression bool,
) []any {
	return []any{
		filter.PackageIDs, filter.PURLs, filter.CVEIDs, filter.AdvisoryIDs,
		filter.SubjectDigests, filter.ProductCriteria, filter.DocumentIDs,
		filter.RepositoryIDs, filter.ImageRefs, filter.FileRepositoryIDs,
		cursor, listFactsByKindPageSize,
		lowerCleanedStringFilterValues(filter.PackageIDs),
		lowerCleanedStringFilterValues(filter.PURLs),
		lowerCleanedStringFilterValues(filter.CVEIDs),
		lowerCleanedStringFilterValues(filter.SubjectDigests),
		lowerCleanedStringFilterValues(filter.RepositoryIDs),
		lowerCleanedStringFilterValues(filter.AdvisoryIDs),
		cursorIsSuppression,
	}
}

// walkSupplyChainImpactRetiredOracle walks the frozen retired statement to
// exhaustion with the retired cursor and sentinel-cap semantics
// (acceptLegacyFact at eb62fa3056), then pages the identity function. Every
// statement it runs is returned for buffer replay.
func walkSupplyChainImpactRetiredOracle(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	filter supplychaincore.SupplyChainImpactFactFilter,
) (supplyChainImpactOracleResult, []supplyChainImpactRecordedStatement) {
	t.Helper()
	normalizeSupplyChainImpactFactFilter(&filter)
	var result supplyChainImpactOracleResult
	var statements []supplyChainImpactRecordedStatement
	if supplyChainImpactFactFilterEmpty(filter) {
		return result, nil
	}
	var core, suppressions []supplyChainImpactOracleRow
	cursor, cursorIsSuppression := "", false
	for done := false; !done; {
		args := supplyChainImpactRetiredOracleArgs(filter, cursor, cursorIsSuppression)
		statements = append(statements, supplyChainImpactRecordedStatement{Query: supplyChainImpactRetiredOracleQuery, Args: args})
		page := supplyChainImpactOracleQueryRows(t, ctx, database, supplyChainImpactRetiredOracleQuery, args)
		for _, row := range page {
			isSuppression := row.FactKind == "vulnerability.suppression"
			cursor, cursorIsSuppression = row.FactID, isSuppression
			if !isSuppression {
				core = append(core, row)
				continue
			}
			if len(suppressions) == maxSupplyChainImpactActiveEvidenceRowsPerCall {
				result.Truncated = true
				done = true
				break
			}
			suppressions = append(suppressions, row)
		}
		if len(page) < listFactsByKindPageSize {
			done = true
		}
	}
	var identity []supplyChainImpactOracleRow
	identityCursor := ""
	for {
		args := []any{filter.SubjectDigests, filter.ImageRefs, filter.RepositoryIDs, filter.RepositoryIDs, filter.RepositoryIDs, identityCursor, listFactsByKindPageSize}
		statements = append(statements, supplyChainImpactRecordedStatement{Query: listCurrentContainerImageIdentitySupportFactsQuery, Args: args})
		page := supplyChainImpactOracleQueryRows(t, ctx, database, listCurrentContainerImageIdentitySupportFactsQuery, args)
		identity = append(identity, page...)
		if len(page) < listFactsByKindPageSize {
			break
		}
		identityCursor = page[len(page)-1].FactID
	}
	result.Rows = append(append(append(result.Rows, core...), identity...), suppressions...)
	return result, statements
}

func supplyChainImpactOracleQueryRows(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	query string,
	args []any,
) []supplyChainImpactOracleRow {
	t.Helper()
	rows, err := database.QueryContext(ctx,
		"SELECT fact_id, scope_id, generation_id, fact_kind, stable_fact_key, payload::text FROM ("+query+") AS oracle", args...)
	if err != nil {
		t.Fatalf("run retired oracle statement: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []supplyChainImpactOracleRow
	for rows.Next() {
		var row supplyChainImpactOracleRow
		if err := rows.Scan(&row.FactID, &row.ScopeID, &row.GenerationID, &row.FactKind, &row.StableFactKey, &row.Payload); err != nil {
			t.Fatalf("scan retired oracle row: %v", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate retired oracle rows: %v", err)
	}
	return out
}

// supplyChainImpactRecordedStatement is one statement the loader sent.
type supplyChainImpactRecordedStatement struct {
	Query string
	Args  []any
}

// supplyChainImpactRecordingDB wraps SQLDB and records every statement run on
// the read-only snapshot transactions it opens, so a test can replay the
// production loader's exact statements under EXPLAIN.
type supplyChainImpactRecordingDB struct {
	SQLDB
	mu         sync.Mutex
	statements []supplyChainImpactRecordedStatement
}

// BeginReadOnlyRepeatableRead opens the real snapshot and wraps it.
func (database *supplyChainImpactRecordingDB) BeginReadOnlyRepeatableRead(ctx context.Context) (db.Transaction, error) {
	tx, err := database.SQLDB.BeginReadOnlyRepeatableRead(ctx)
	if err != nil {
		return nil, err
	}
	return &supplyChainImpactRecordingTx{Transaction: tx, recorder: database}, nil
}

func (database *supplyChainImpactRecordingDB) record(query string, args []any) {
	database.mu.Lock()
	defer database.mu.Unlock()
	database.statements = append(database.statements, supplyChainImpactRecordedStatement{Query: query, Args: append([]any(nil), args...)})
}

func (database *supplyChainImpactRecordingDB) take() []supplyChainImpactRecordedStatement {
	database.mu.Lock()
	defer database.mu.Unlock()
	out := database.statements
	database.statements = nil
	return out
}

type supplyChainImpactRecordingTx struct {
	db.Transaction
	recorder *supplyChainImpactRecordingDB
}

// QueryContext records the statement before running it on the snapshot.
func (tx *supplyChainImpactRecordingTx) QueryContext(ctx context.Context, query string, args ...any) (db.Rows, error) {
	tx.recorder.record(query, args)
	return tx.Transaction.QueryContext(ctx, query, args...)
}

// supplyChainImpactReplay is one replayed statement: its warm shared buffers
// and its JSON plan.
type supplyChainImpactReplay struct {
	Buffers int64
	Plan    string
	Query   string
}

// supplyChainImpactStatementBuffers replays every statement under
// PREPARE + EXPLAIN (ANALYZE, BUFFERS) EXECUTE with the given plan_cache_mode
// and returns the summed shared buffers (hit + read) of the warm second run,
// plus each statement's replay for shape assertions.
func supplyChainImpactStatementBuffers(
	t *testing.T,
	ctx context.Context,
	database *sql.DB,
	statements []supplyChainImpactRecordedStatement,
	mode string,
) (int64, []supplyChainImpactReplay) {
	t.Helper()
	conn, err := database.Conn(ctx)
	if err != nil {
		t.Fatalf("dedicated replay session: %v", err)
	}
	defer func() {
		_, _ = conn.ExecContext(ctx, "RESET plan_cache_mode")
		_ = conn.Close()
	}()
	if _, err := conn.ExecContext(ctx, "SET plan_cache_mode = "+mode); err != nil {
		t.Fatalf("set plan_cache_mode: %v", err)
	}
	var total int64
	plans := make([]supplyChainImpactReplay, 0, len(statements))
	for i, statement := range statements {
		name := "scie_replay_" + strconv.Itoa(i)
		if _, err := conn.ExecContext(ctx, "PREPARE "+name+" AS "+statement.Query); err != nil {
			t.Fatalf("prepare replay %d: %v\n%s", i, err, statement.Query)
		}
		literals := make([]string, 0, len(statement.Args))
		for _, arg := range statement.Args {
			literals = append(literals, supplyChainImpactSQLLiteral(t, arg))
		}
		execute := "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) EXECUTE " + name
		if len(literals) > 0 {
			execute += "(" + strings.Join(literals, ", ") + ")"
		}
		var raw []byte
		for run := 0; run < 2; run++ {
			if err := conn.QueryRowContext(ctx, execute).Scan(&raw); err != nil {
				t.Fatalf("explain replay %d: %v", i, err)
			}
		}
		var decoded []struct {
			Plan struct {
				SharedHitBlocks  int64 `json:"Shared Hit Blocks"`
				SharedReadBlocks int64 `json:"Shared Read Blocks"`
			} `json:"Plan"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded) != 1 {
			t.Fatalf("decode replay plan %d: count=%d err=%v", i, len(decoded), err)
		}
		buffers := decoded[0].Plan.SharedHitBlocks + decoded[0].Plan.SharedReadBlocks
		total += buffers
		plans = append(plans, supplyChainImpactReplay{Buffers: buffers, Plan: string(raw), Query: statement.Query})
		if _, err := conn.ExecContext(ctx, "DEALLOCATE "+name); err != nil {
			t.Fatalf("deallocate replay %d: %v", i, err)
		}
	}
	return total, plans
}

// supplyChainImpactSQLLiteral renders one bound argument as a SQL literal for
// EXECUTE. Only the argument types the supply-chain loaders bind are accepted.
func supplyChainImpactSQLLiteral(t *testing.T, arg any) string {
	t.Helper()
	quote := func(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }
	switch value := arg.(type) {
	case nil:
		return "NULL"
	case []string:
		if value == nil {
			return "NULL::text[]"
		}
		quoted := make([]string, 0, len(value))
		for _, item := range value {
			quoted = append(quoted, quote(item))
		}
		return "ARRAY[" + strings.Join(quoted, ", ") + "]::text[]"
	case string:
		return quote(value)
	case int:
		return strconv.Itoa(value)
	case int64:
		return strconv.FormatInt(value, 10)
	case bool:
		return strconv.FormatBool(value)
	default:
		t.Fatalf("unsupported replay argument type %T", arg)
		return ""
	}
}

// supplyChainImpactProductionRows loads one call through the production
// FactStore and projects it the same way as the oracle.
func supplyChainImpactProductionRows(
	t *testing.T,
	ctx context.Context,
	recorder *supplyChainImpactRecordingDB,
	filter supplychaincore.SupplyChainImpactFactFilter,
) (supplyChainImpactOracleResult, []supplyChainImpactRecordedStatement) {
	t.Helper()
	recorder.take()
	envelopes, truncated, err := NewFactStore(recorder).ListActiveSupplyChainImpactFacts(ctx, filter)
	if err != nil {
		t.Fatalf("ListActiveSupplyChainImpactFacts(): %v", err)
	}
	result := supplyChainImpactOracleResult{Truncated: truncated}
	seen := make(map[string]struct{}, len(envelopes))
	for _, envelope := range envelopes {
		if _, duplicate := seen[envelope.FactID]; duplicate {
			t.Fatalf("production loader returned fact %q twice", envelope.FactID)
		}
		seen[envelope.FactID] = struct{}{}
		payload, err := json.Marshal(envelope.Payload)
		if err != nil {
			t.Fatalf("marshal payload for %s: %v", envelope.FactID, err)
		}
		result.Rows = append(result.Rows, supplyChainImpactOracleRow{
			FactID: envelope.FactID, ScopeID: envelope.ScopeID, GenerationID: envelope.GenerationID,
			FactKind: envelope.FactKind, StableFactKey: envelope.StableFactKey, Payload: string(payload),
		})
	}
	return result, recorder.take()
}

// canonicalSupplyChainImpactPayload re-encodes a JSON document so jsonb::text
// and encoding/json renderings compare equal.
func canonicalSupplyChainImpactPayload(t *testing.T, raw string) string {
	t.Helper()
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("decode payload %q: %v", raw, err)
	}
	encoded, err := json.Marshal(decoded)
	if err != nil {
		t.Fatalf("encode payload: %v", err)
	}
	return string(encoded)
}

// supplyChainImpactRowLines renders a result for a readable diff.
func supplyChainImpactRowLines(t *testing.T, result supplyChainImpactOracleResult) []string {
	t.Helper()
	lines := make([]string, 0, len(result.Rows)+1)
	for _, row := range result.Rows {
		lines = append(lines, strings.Join([]string{
			row.FactID, row.FactKind, row.ScopeID, row.GenerationID, row.StableFactKey,
			canonicalSupplyChainImpactPayload(t, row.Payload),
		}, "|"))
	}
	return append(lines, "truncated="+strconv.FormatBool(result.Truncated))
}

// sortedSupplyChainImpactFactIDs returns the fact IDs of a result, sorted.
func sortedSupplyChainImpactFactIDs(result supplyChainImpactOracleResult) []string {
	ids := make([]string, 0, len(result.Rows))
	for _, row := range result.Rows {
		ids = append(ids, row.FactID)
	}
	sort.Strings(ids)
	return ids
}

// supplyChainImpactPlanNode is the subset of an EXPLAIN JSON node the plan
// assertions and failure summaries read.
type supplyChainImpactPlanNode struct {
	NodeType    string                      `json:"Node Type"`
	IndexName   string                      `json:"Index Name"`
	Relation    string                      `json:"Relation Name"`
	IndexCond   string                      `json:"Index Cond"`
	Filter      string                      `json:"Filter"`
	ActualRows  float64                     `json:"Actual Rows"`
	ActualLoops float64                     `json:"Actual Loops"`
	Rechecked   float64                     `json:"Rows Removed by Index Recheck"`
	SharedHit   int64                       `json:"Shared Hit Blocks"`
	SharedRead  int64                       `json:"Shared Read Blocks"`
	Plans       []supplyChainImpactPlanNode `json:"Plans"`
}

// supplyChainImpactPlanNodes flattens one EXPLAIN (FORMAT JSON) document.
func supplyChainImpactPlanNodes(t *testing.T, raw string) []supplyChainImpactPlanNode {
	t.Helper()
	var decoded []struct {
		Plan supplyChainImpactPlanNode `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil || len(decoded) != 1 {
		t.Fatalf("decode plan: count=%d err=%v", len(decoded), err)
	}
	var out []supplyChainImpactPlanNode
	var walk func(node supplyChainImpactPlanNode)
	walk = func(node supplyChainImpactPlanNode) {
		out = append(out, node)
		for _, child := range node.Plans {
			walk(child)
		}
	}
	walk(decoded[0].Plan)
	return out
}

// supplyChainImpactPlanSummary renders one line per plan node.
func supplyChainImpactPlanSummary(t *testing.T, raw string) string {
	t.Helper()
	var lines []string
	for _, node := range supplyChainImpactPlanNodes(t, raw) {
		lines = append(lines, fmt.Sprintf("%s rel=%s idx=%s loops=%.0f rows=%.0f rechecked=%.0f buffers=%d cond=%.160s filter=%.160s",
			node.NodeType, node.Relation, node.IndexName, node.ActualLoops, node.ActualRows, node.Rechecked,
			node.SharedHit+node.SharedRead, node.IndexCond, node.Filter))
	}
	return strings.Join(lines, "\n")
}

// supplyChainImpactDefinitionsWithAnchors returns the required bootstrap
// definitions plus every definition that creates an index a #8880 branch
// anchors on, in bootstrap order, so an isolated proof schema plans on the
// shipped indexes.
func supplyChainImpactDefinitionsWithAnchors(t *testing.T, required map[string]bool) []Definition {
	t.Helper()
	want := map[string]bool{}
	for name := range required {
		want[name] = true
	}
	for _, branch := range factimpact.Branches() {
		for _, definition := range BootstrapDefinitions() {
			if strings.Contains(definition.SQL, "IF NOT EXISTS "+branch.Anchor+"\n") {
				want[definition.Name] = true
			}
		}
	}
	var definitions []Definition
	for _, definition := range BootstrapDefinitions() {
		if want[definition.Name] {
			definitions = append(definitions, definition)
		}
	}
	if len(definitions) != len(want) {
		t.Fatalf("isolated schema definitions = %d, want %d", len(definitions), len(want))
	}
	return definitions
}
