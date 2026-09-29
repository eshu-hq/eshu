// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/codequery"
)

var codeTopicParallelDriverSequence atomic.Uint64

type codeTopicParallelRecorder struct {
	mu           sync.Mutex
	active       int
	maxActive    int
	imports      int
	probes       int
	probeTerms   [][]string
	assemblyJSON string
	assemblyPage []driver.Value
	beginOptions []driver.TxOptions
	probeFailure error
	emptyProbes  bool
}

func (r *codeTopicParallelRecorder) recordBegin(opts driver.TxOptions) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.active++
	if r.active > r.maxActive {
		r.maxActive = r.active
	}
	r.beginOptions = append(r.beginOptions, opts)
}

type codeTopicParallelDriver struct{ recorder *codeTopicParallelRecorder }

func (d *codeTopicParallelDriver) Open(string) (driver.Conn, error) {
	return &codeTopicParallelConn{recorder: d.recorder}, nil
}

type codeTopicParallelConn struct{ recorder *codeTopicParallelRecorder }

func (*codeTopicParallelConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}
func (*codeTopicParallelConn) Close() error              { return nil }
func (*codeTopicParallelConn) Begin() (driver.Tx, error) { return nil, errors.New("unexpected Begin") }

func (c *codeTopicParallelConn) BeginTx(_ context.Context, opts driver.TxOptions) (driver.Tx, error) {
	c.recorder.recordBegin(opts)
	return &codeTopicParallelTx{recorder: c.recorder}, nil
}

type codeTopicParallelTx struct {
	recorder *codeTopicParallelRecorder
	once     sync.Once
}

func (tx *codeTopicParallelTx) finish() {
	tx.once.Do(func() {
		tx.recorder.mu.Lock()
		defer tx.recorder.mu.Unlock()
		tx.recorder.active--
	})
}

func (tx *codeTopicParallelTx) Commit() error   { tx.finish(); return nil }
func (tx *codeTopicParallelTx) Rollback() error { tx.finish(); return nil }

func (c *codeTopicParallelConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if !strings.HasPrefix(query, "SET TRANSACTION SNAPSHOT '") {
		return nil, fmt.Errorf("unexpected exec: %s", query)
	}
	c.recorder.mu.Lock()
	c.recorder.imports++
	c.recorder.mu.Unlock()
	return driver.RowsAffected(0), nil
}

func (c *codeTopicParallelConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	switch {
	case query == "SELECT pg_export_snapshot()":
		return &codeTopicParallelRows{columns: []string{"pg_export_snapshot"}, values: [][]driver.Value{{"00000003-0000001B-1"}}}, nil
	case strings.Contains(query, "jsonb_to_recordset"):
		if len(args) != 3 {
			return nil, fmt.Errorf("assembly args = %d", len(args))
		}
		c.recorder.mu.Lock()
		c.recorder.assemblyJSON = args[0].Value.(string)
		c.recorder.assemblyPage = []driver.Value{args[1].Value, args[2].Value}
		c.recorder.mu.Unlock()
		return &codeTopicParallelRows{columns: []string{"source_kind", "repo_id", "relative_path", "entity_id", "entity_name", "entity_type", "language", "start_line", "end_line", "matched_terms", "score", "pool_truncated"}, values: [][]driver.Value{{"entity", "repo", "a.go", "id", "Name", "Function", "go", int64(1), int64(2), "same", int64(1), true}}}, nil
	case strings.Contains(query, "WITH terms(term) AS"):
		c.recorder.mu.Lock()
		c.recorder.probes++
		failure := c.recorder.probeFailure
		empty := c.recorder.emptyProbes
		boundTerms := make([]string, 0, len(args))
		for _, arg := range args {
			if term, ok := arg.Value.(string); ok {
				boundTerms = append(boundTerms, term)
			}
		}
		c.recorder.probeTerms = append(c.recorder.probeTerms, boundTerms)
		c.recorder.mu.Unlock()
		if failure != nil {
			return nil, failure
		}
		if empty {
			return &codeTopicParallelRows{columns: []string{"source_kind", "matched_term", "repo_id", "relative_path", "entity_id", "entity_name", "entity_type", "language", "start_line", "end_line"}}, nil
		}
		return &codeTopicParallelRows{columns: []string{"source_kind", "matched_term", "repo_id", "relative_path", "entity_id", "entity_name", "entity_type", "language", "start_line", "end_line"}, values: [][]driver.Value{{"entity", "same", "repo", "a.go", "id", nil, "Function", "go", int64(1), int64(2)}}}, nil
	default:
		return nil, fmt.Errorf("unexpected query: %s", query)
	}
}

type codeTopicParallelRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func (r *codeTopicParallelRows) Columns() []string { return r.columns }
func (*codeTopicParallelRows) Close() error        { return nil }
func (r *codeTopicParallelRows) Next(dest []driver.Value) error {
	if r.index == len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func openCodeTopicParallelDB(t *testing.T, recorder *codeTopicParallelRecorder) *sql.DB {
	t.Helper()
	name := fmt.Sprintf("code-topic-parallel-%d", codeTopicParallelDriverSequence.Add(1))
	sql.Register(name, &codeTopicParallelDriver{recorder: recorder})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(4)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestInvestigateCodeTopicParallelUsesSnapshotAndNullableSQLAssembly(t *testing.T) {
	recorder := &codeTopicParallelRecorder{}
	reader := NewContentReader(openCodeTopicParallelDB(t, recorder))
	terms := []string{"same", "other", "same", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	result, err := reader.InvestigateCodeTopic(context.Background(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 1, Offset: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || !result[0].PoolTruncated {
		t.Fatalf("result = %#v", result)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.active != 0 || recorder.imports != 3 || recorder.probes != 4 || recorder.maxActive > 4 {
		t.Fatalf("active=%d imports=%d probes=%d maxActive=%d", recorder.active, recorder.imports, recorder.probes, recorder.maxActive)
	}
	if len(recorder.beginOptions) != 4 {
		t.Fatalf("transactions = %d, want 4", len(recorder.beginOptions))
	}
	if len(recorder.assemblyPage) != 2 || recorder.assemblyPage[0] != int64(1) || recorder.assemblyPage[1] != int64(1) {
		t.Fatalf("assembly page args = %#v, want limit=1 offset=1", recorder.assemblyPage)
	}
	termCounts := make(map[string]int)
	for _, group := range recorder.probeTerms {
		for _, term := range group {
			termCounts[term]++
		}
	}
	if termCounts["same"] != 2 || len(recorder.probeTerms) != 4 {
		t.Fatalf("partitioned terms = %#v", recorder.probeTerms)
	}
	for _, opts := range recorder.beginOptions {
		if !opts.ReadOnly || opts.Isolation != driver.IsolationLevel(sql.LevelRepeatableRead) {
			t.Fatalf("transaction options = %+v", opts)
		}
	}
	var transfer []map[string]any
	if err := json.Unmarshal([]byte(recorder.assemblyJSON), &transfer); err != nil {
		t.Fatal(err)
	}
	if len(transfer) != 4 || transfer[0]["entity_name"] != nil {
		t.Fatalf("nullable transfer = %#v", transfer)
	}
}

func TestInvestigateCodeTopicParallelEmptyProbeSendsJSONArray(t *testing.T) {
	recorder := &codeTopicParallelRecorder{emptyProbes: true}
	reader := NewContentReader(openCodeTopicParallelDB(t, recorder))
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	_, err := reader.InvestigateCodeTopic(context.Background(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 26})
	if err != nil {
		t.Fatal(err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.assemblyJSON != "[]" || recorder.active != 0 {
		t.Fatalf("empty assembly=%q active=%d", recorder.assemblyJSON, recorder.active)
	}
}

func TestInvestigateCodeTopicParallelRollsBackAfterProbeError(t *testing.T) {
	recorder := &codeTopicParallelRecorder{probeFailure: errors.New("injected probe failure")}
	reader := NewContentReader(openCodeTopicParallelDB(t, recorder))
	terms := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15"}
	_, err := reader.InvestigateCodeTopic(context.Background(), codequery.CodeTopicInvestigationRequest{Terms: terms, Limit: 26})
	if err == nil || !strings.Contains(err.Error(), "injected probe failure") {
		t.Fatalf("error = %v, want injected failure", err)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.active != 0 || recorder.assemblyJSON != "" {
		t.Fatalf("active=%d assembly=%q", recorder.active, recorder.assemblyJSON)
	}
}
