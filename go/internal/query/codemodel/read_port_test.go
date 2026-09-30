// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

type flowReadPort struct {
	query string
	args  []any
	rows  db.Rows
	err   error
}

func (r *flowReadPort) QueryContext(_ context.Context, query string, args ...any) (db.Rows, error) {
	r.query, r.args = query, args
	return r.rows, r.err
}

func TestCodeFlowGuardedReadPortPreservesRefusalAndArguments(t *testing.T) {
	want := errors.New("reader is stale")
	guard := &flowReadPort{err: want}
	store := NewPostgresCodeFlowStoreWithReadStore(guard)
	_, err := store.ListCodeFlow(t.Context(), CodeFlowFilter{Kind: CodeFlowKindCFGSummary, RepoID: " repo ", Language: "JS", FilePath: " flow.js ", Symbol: " f ", Line: 7, Limit: 3})
	if !errors.Is(err, want) || guard.query != ListActiveCodeFlowFactsSQL {
		t.Fatalf("query %q, error %v; want unchanged SQL and guarded refusal", guard.query, err)
	}
	wantArgs := []any{CodeFlowFactKinds(CodeFlowKindCFGSummary), "repo", "javascript", "flow.js", "f", 7, 3}
	if !reflect.DeepEqual(guard.args, wantArgs) {
		t.Fatalf("args %v, want %v", guard.args, wantArgs)
	}
}

type flowReadRows struct {
	next   bool
	closed bool
}

func (r *flowReadRows) Next() bool {
	if r.next {
		return false
	}
	r.next = true
	return true
}

func (r *flowReadRows) Scan(dest ...any) error {
	*dest[0].(*string) = "fact-1"
	*dest[1].(*string) = "generation-1"
	*dest[2].(*string) = facts.CodeDataflowFunctionFactKind
	*dest[3].(*time.Time) = time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)
	*dest[4].(*[]byte) = []byte(`{"repo_id":"repo","relative_path":"flow.js","function_name":"f","language":"js"}`)
	return nil
}
func (*flowReadRows) Err() error     { return nil }
func (r *flowReadRows) Close() error { r.closed = true; return nil }

func TestCodeFlowGuardedReadPortDecodesAndCloses(t *testing.T) {
	rows := &flowReadRows{}
	store := NewPostgresCodeFlowStoreWithReadStore(&flowReadPort{rows: rows})
	got, err := store.ListCodeFlow(t.Context(), CodeFlowFilter{Kind: CodeFlowKindCFGSummary, RepoID: "repo"})
	if err != nil || len(got.Functions) != 1 || !rows.closed {
		t.Fatalf("result %v, error %v, closed %v", got, err, rows.closed)
	}
	if got.Functions[0].EvidenceHandle != "fact://"+facts.CodeDataflowFunctionFactKind+"/fact-1" || got.Functions[0].Language != "javascript" {
		t.Fatalf("decoded function %v", got.Functions[0])
	}
}
