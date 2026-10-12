// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/jackc/pgx/v5"
)

// diagnosticStatement retains only the numeric pg_stat_statements identity and
// counters. Query text, plan literals, credentials and database names are absent.
type diagnosticStatement struct {
	StatsSinceUnixMicro int64   `json:"stats_since_unix_micro"`
	DBID                int64   `json:"dbid"`
	UserID              int64   `json:"userid"`
	QueryID             int64   `json:"queryid"`
	TopLevel            bool    `json:"toplevel"`
	Calls               int64   `json:"calls"`
	Rows                int64   `json:"rows"`
	Blocks              int64   `json:"blocks"`
	ExecMS              float64 `json:"exec_ms"`
}

type diagnosticStatementKey struct {
	dbid, userid, queryid int64
	toplevel              bool
}

func statementKey(row diagnosticStatement) diagnosticStatementKey {
	return diagnosticStatementKey{row.DBID, row.UserID, row.QueryID, row.TopLevel}
}

func diagnosticStatementMap(rows []diagnosticStatement) (map[diagnosticStatementKey]diagnosticStatement, error) {
	if len(rows) > 10000 {
		return nil, fmt.Errorf("statement capacity exceeded")
	}
	result := make(map[diagnosticStatementKey]diagnosticStatement, len(rows))
	for _, row := range rows {
		if row.StatsSinceUnixMicro <= 0 || row.DBID <= 0 || row.UserID <= 0 || row.Calls < 0 || row.Rows < 0 || row.Blocks < 0 || row.ExecMS < 0 || math.IsNaN(row.ExecMS) || math.IsInf(row.ExecMS, 0) {
			return nil, fmt.Errorf("invalid numeric statement counter")
		}
		key := statementKey(row)
		if _, exists := result[key]; exists {
			return nil, fmt.Errorf("duplicate statement identity")
		}
		result[key] = row
	}
	return result, nil
}

// diagnosticStatementDeltas refuses reset, eviction or malformed evidence.
// New statement identities use a zero baseline; totals describe the entire
// database interval and must not be attributed to an individual request.
func diagnosticStatementDeltas(before, after []diagnosticStatement) ([]diagnosticStatement, error) {
	previous, err := diagnosticStatementMap(before)
	if err != nil {
		return nil, err
	}
	current, err := diagnosticStatementMap(after)
	if err != nil {
		return nil, err
	}
	for key := range previous {
		if _, exists := current[key]; !exists {
			return nil, fmt.Errorf("statement disappeared")
		}
	}
	result := make([]diagnosticStatement, 0, len(current))
	for key, row := range current {
		prior, existed := previous[key]
		if existed && row.StatsSinceUnixMicro != prior.StatsSinceUnixMicro {
			return nil, errors.New("statement collection epoch changed")
		}
		if row.Calls < prior.Calls || row.Rows < prior.Rows || row.Blocks < prior.Blocks || row.ExecMS < prior.ExecMS {
			return nil, fmt.Errorf("statement counter decreased")
		}
		row.Calls -= prior.Calls
		row.Rows -= prior.Rows
		row.Blocks -= prior.Blocks
		row.ExecMS -= prior.ExecMS
		result = append(result, row)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := result[i], result[j]
		if a.DBID != b.DBID {
			return a.DBID < b.DBID
		}
		if a.UserID != b.UserID {
			return a.UserID < b.UserID
		}
		if a.QueryID != b.QueryID {
			return a.QueryID < b.QueryID
		}
		return !a.TopLevel && b.TopLevel
	})
	return result, nil
}

// This exact numeric-epoch read was exercised over the full seed before implementation.
// It performs no reset and excludes diagnostic and meter statements. Its work
// happens outside the serial meter and concurrent request/wall intervals.
const diagnosticStatementSQL = `SELECT dbid::bigint, userid::bigint, queryid, toplevel,
 calls, rows, (shared_blks_hit + shared_blks_read + local_blks_hit + local_blks_read + temp_blks_read + temp_blks_written)::bigint,
 total_exec_time, (extract(epoch FROM stats_since)*1000000)::bigint FROM pg_stat_statements
 WHERE dbid = (SELECT oid FROM pg_database WHERE datname = current_database())
 AND query NOT LIKE '%pg_stat_statements%' LIMIT 10001`

type diagnosticStatementSnapshot struct {
	Phase         string                `json:"phase"`
	StartUnixNano int64                 `json:"start_unix_nano"`
	EndUnixNano   int64                 `json:"end_unix_nano"`
	Records       []diagnosticStatement `json:"records"`
}

// diagnosticStatementCapture finalizes outside the gate's ordinary verdict.
// A failed capture must remain an error even when the ordinary samples pass.
type diagnosticStatementCapture interface {
	capture(phase string)
	finish() error
}

type diagnosticStatements struct {
	conn      *pgx.Conn
	path      string
	snapshots []diagnosticStatementSnapshot
	failures  []string
}

func newDiagnosticStatements(dsn, path string) (*diagnosticStatements, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, errors.New("diagnostic statement connection failed")
	}
	return &diagnosticStatements{conn: conn, path: path, failures: []string{}}, nil
}

// capture retains a closed failure code rather than masking it. finish joins
// diagnostic failure with the original gate result, after the ordinary burst.
func (d *diagnosticStatements) capture(phase string) {
	if len(d.snapshots) >= 2 || phase != []string{"before_concurrent", "after_concurrent"}[len(d.snapshots)] {
		d.failures = append(d.failures, "invalid_statement_boundary")
		return
	}
	start := time.Now().UnixNano()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	records, err := readDiagnosticStatements(ctx, d.conn)
	cancel()
	d.snapshots = append(d.snapshots, diagnosticStatementSnapshot{phase, start, time.Now().UnixNano(), records})
	if err != nil {
		d.failures = append(d.failures, "statement_snapshot_failed")
	}
}

func readDiagnosticStatements(ctx context.Context, conn *pgx.Conn) ([]diagnosticStatement, error) {
	rows, err := conn.Query(ctx, diagnosticStatementSQL)
	if err != nil {
		return nil, errors.New("statement read failed")
	}
	defer rows.Close()
	records := make([]diagnosticStatement, 0)
	for rows.Next() {
		var row diagnosticStatement
		if err := rows.Scan(&row.DBID, &row.UserID, &row.QueryID, &row.TopLevel, &row.Calls, &row.Rows, &row.Blocks, &row.ExecMS, &row.StatsSinceUnixMicro); err != nil {
			return nil, errors.New("statement decode failed")
		}
		if len(records) >= 10000 {
			return nil, errors.New("statement capacity exceeded")
		}
		records = append(records, row)
	}
	if rows.Err() != nil {
		return nil, errors.New("statement read incomplete")
	}
	if _, err := diagnosticStatementMap(records); err != nil {
		return nil, err
	}
	return records, nil
}

func (d *diagnosticStatements) finish() error {
	if len(d.snapshots) == 2 && len(d.failures) == 0 {
		if err := d.writeSupplementaryPlan(); err != nil {
			d.failures = append(d.failures, "supplementary_plan_failed")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if err := d.conn.Close(ctx); err != nil {
		d.failures = append(d.failures, "statement_connection_close_failed")
	}
	cancel()
	var deltas []diagnosticStatement
	if len(d.snapshots) != 2 {
		d.failures = append(d.failures, "missing_statement_boundary")
	} else {
		var err error
		deltas, err = diagnosticStatementDeltas(d.snapshots[0].Records, d.snapshots[1].Records)
		if err != nil {
			d.failures = append(d.failures, "invalid_statement_deltas")
		}
	}
	report := struct {
		SchemaVersion int                           `json:"schema_version"`
		Complete      bool                          `json:"complete"`
		Scope         string                        `json:"scope"`
		Snapshots     []diagnosticStatementSnapshot `json:"snapshots"`
		Deltas        []diagnosticStatement         `json:"deltas"`
		Errors        []string                      `json:"errors"`
	}{1, len(d.failures) == 0, "database aggregate; no per-request attribution", d.snapshots, deltas, d.failures}
	file, err := os.CreateTemp(filepath.Dir(d.path), ".statement-diagnostic-*")
	if err != nil {
		return errors.New("create statement diagnostic report failed")
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := json.NewEncoder(file).Encode(report); err != nil {
		_ = file.Close()
		return errors.New("encode statement diagnostic report failed")
	}
	if err := file.Close(); err != nil {
		return errors.New("close statement diagnostic report failed")
	}
	if err := os.Rename(file.Name(), d.path); err != nil {
		return errors.New("publish statement diagnostic report failed")
	}
	if !report.Complete {
		return fmt.Errorf("diagnostic statement report incomplete")
	}
	return nil
}

// diagnosticSQLSelector deliberately stops the production read at its query
// adapter boundary. It captures the compiled statement and arguments without
// inventing business rows or executing it before the supplementary plan phase.
type diagnosticSQLSelector struct {
	query string
	args  []any
	calls int
}

var errDiagnosticSQLSelected = errors.New("diagnostic statement selected")

func (s *diagnosticSQLSelector) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	s.calls++
	s.query = query
	s.args = append([]any(nil), args...)
	return nil, errDiagnosticSQLSelected
}

func diagnosticActiveWorkSQL(asOf time.Time) (string, []any, error) {
	selector := &diagnosticSQLSelector{}
	_, err := postgres.ReadActiveWorkSummaryEntries(context.Background(), selector, asOf.UTC())
	sum := sha256.Sum256([]byte(selector.query))
	if selector.calls != 1 || !errors.Is(err, errDiagnosticSQLSelected) || len(selector.args) != 1 || hex.EncodeToString(sum[:]) != postgres.ActiveWorkSummarySourceSHA256() {
		return "", nil, errors.New("diagnostic production statement selection failed")
	}
	return selector.query, selector.args, nil
}

// sanitizeDiagnosticPlan keeps only closed operator names and numeric evidence.
// Buffer counts include child work; consumers must not sum them over the tree.
func sanitizeDiagnosticPlan(raw []byte) (map[string]any, error) {
	if len(raw) > 4<<20 {
		return nil, errors.New("diagnostic plan capacity exceeded")
	}
	var plans []map[string]any
	if err := json.Unmarshal(raw, &plans); err != nil || len(plans) != 1 {
		return nil, errors.New("invalid diagnostic plan envelope")
	}
	result := make(map[string]any)
	for _, field := range []string{"Planning Time", "Execution Time"} {
		value, ok := plans[0][field].(float64)
		if !ok || !validPlanNumber(value) {
			return nil, errors.New("invalid diagnostic plan timing")
		}
		result[field] = value
	}
	count := 0
	node, err := sanitizeDiagnosticPlanNode(plans[0]["Plan"], 0, &count)
	if err != nil {
		return nil, err
	}
	result["Plan"] = node
	return result, nil
}

func validPlanNumber(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func sanitizeDiagnosticPlanNode(input any, depth int, count *int) (map[string]any, error) {
	*count++
	if depth > 64 || *count > 2048 {
		return nil, errors.New("diagnostic plan tree capacity exceeded")
	}
	source, ok := input.(map[string]any)
	if !ok {
		return nil, errors.New("invalid diagnostic plan node")
	}
	kind, ok := source["Node Type"].(string)
	if !ok {
		return nil, errors.New("missing diagnostic plan operator")
	}
	switch kind {
	case "Result", "ProjectSet", "ModifyTable", "Append", "Merge Append", "Recursive Union", "BitmapAnd", "BitmapOr",
		"Seq Scan", "Sample Scan", "Index Scan", "Index Only Scan", "Bitmap Index Scan", "Bitmap Heap Scan", "Tid Scan", "Tid Range Scan",
		"Subquery Scan", "Function Scan", "Table Function Scan", "Values Scan", "CTE Scan", "Named Tuplestore Scan", "WorkTable Scan", "Foreign Scan",
		"Custom Scan", "Nested Loop", "Merge Join", "Hash Join", "Materialize", "Memoize", "Sort", "Incremental Sort", "Group", "Aggregate",
		"WindowAgg", "Unique", "Gather", "Gather Merge", "Hash", "SetOp", "LockRows", "Limit":
	default:
		return nil, errors.New("unknown diagnostic plan operator")
	}
	result := map[string]any{"Node Type": kind}
	required := []string{"Actual Rows", "Actual Loops", "Actual Startup Time", "Actual Total Time"}
	optional := []string{"Shared Hit Blocks", "Shared Read Blocks", "Shared Dirtied Blocks", "Shared Written Blocks", "Local Hit Blocks", "Local Read Blocks", "Local Dirtied Blocks", "Local Written Blocks", "Temp Read Blocks", "Temp Written Blocks"}
	for _, field := range append(required, optional...) {
		value, exists := source[field]
		if !exists {
			if field == "Actual Rows" || field == "Actual Loops" || field == "Actual Startup Time" || field == "Actual Total Time" {
				return nil, errors.New("missing diagnostic plan metric")
			}
			continue
		}
		number, ok := value.(float64)
		if !ok || !validPlanNumber(number) {
			return nil, errors.New("invalid diagnostic plan metric")
		}
		result[field] = number
	}
	if value, exists := source["Plans"]; exists {
		children, ok := value.([]any)
		if !ok {
			return nil, errors.New("invalid diagnostic child plans")
		}
		clean := make([]map[string]any, 0, len(children))
		for _, child := range children {
			node, err := sanitizeDiagnosticPlanNode(child, depth+1, count)
			if err != nil {
				return nil, err
			}
			clean = append(clean, node)
		}
		result["Plans"] = clean
	}
	return result, nil
}

// diagnosticPlanQuery runs only after the final statement snapshot. ANALYZE
// executes the SELECT, so its work is supplementary and excluded from budgets.
type diagnosticPlanQuery interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

func readDiagnosticPlan(ctx context.Context, queryer diagnosticPlanQuery, asOf time.Time) (map[string]any, error) {
	query, args, err := diagnosticActiveWorkSQL(asOf)
	if err != nil {
		return nil, err
	}
	var raw []byte
	if err := queryer.QueryRow(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
		return nil, errors.New("diagnostic plan execution failed")
	}
	return sanitizeDiagnosticPlan(raw)
}

func (d *diagnosticStatements) writeSupplementaryPlan() error {
	asOf := time.Now().UTC()
	start := time.Now().UnixNano()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	plan, readErr := readDiagnosticPlan(ctx, d.conn, asOf)
	cancel()
	failures := []string{}
	if readErr != nil {
		failures = append(failures, "supplementary_plan_failed")
	}
	report := struct {
		SchemaVersion int            `json:"schema_version"`
		Complete      bool           `json:"complete"`
		Scope         string         `json:"scope"`
		SourceSHA256  string         `json:"source_sha256"`
		AsOfUnixNano  int64          `json:"as_of_unix_nano"`
		StartUnixNano int64          `json:"start_unix_nano"`
		EndUnixNano   int64          `json:"end_unix_nano"`
		Plan          map[string]any `json:"plan"`
		Errors        []string       `json:"errors"`
	}{1, readErr == nil, "supplementary execution; no request attribution", postgres.ActiveWorkSummarySourceSHA256(), asOf.UnixNano(), start, time.Now().UnixNano(), plan, failures}
	file, err := os.CreateTemp(filepath.Dir(d.path), ".plan-diagnostic-*")
	if err != nil {
		return errors.New("create diagnostic plan report failed")
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err := json.NewEncoder(file).Encode(report); err != nil {
		_ = file.Close()
		return errors.New("encode diagnostic plan report failed")
	}
	if err := file.Close(); err != nil {
		return errors.New("close diagnostic plan report failed")
	}
	if err := os.Rename(file.Name(), filepath.Join(filepath.Dir(d.path), "plan.json")); err != nil {
		return errors.New("publish diagnostic plan report failed")
	}
	return readErr
}
