// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/incident/model"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// cov8Rows is an in-memory db.Rows double: Next walks the canned value
// rows, Scan assigns them positionally, Err injects a cursor failure.
type cov8Rows struct {
	rows    [][]any
	pos     int
	scanErr error
	rowErr  error
	closed  int
}

func (r *cov8Rows) Next() bool {
	if r.pos < len(r.rows) {
		r.pos++
		return true
	}
	return false
}

func (r *cov8Rows) Scan(dest ...any) error {
	if r.scanErr != nil {
		return r.scanErr
	}
	values := r.rows[r.pos-1]
	if len(values) != len(dest) {
		return fmt.Errorf("cov8: row has %d values for %d destinations", len(values), len(dest))
	}
	for i, d := range dest {
		if err := cov8Assign(d, values[i]); err != nil {
			return err
		}
	}
	return nil
}

func (r *cov8Rows) Err() error { return r.rowErr }

func (r *cov8Rows) Close() error {
	r.closed++
	return nil
}

// cov8Assign mimics database/sql conversion for the destination shapes
// the store scans: plain pointers plus sql.Scanner implementations
// (sql.Null*), which parse the value themselves.
func cov8Assign(dest, value any) error {
	if scanner, ok := dest.(sql.Scanner); ok {
		return scanner.Scan(value)
	}
	switch d := dest.(type) {
	case *string:
		s, ok := value.(string)
		if !ok {
			return fmt.Errorf("cov8: cannot scan %T into *string", value)
		}
		*d = s
		return nil
	case *[]byte:
		switch v := value.(type) {
		case []byte:
			*d = append([]byte(nil), v...)
			return nil
		case string:
			*d = []byte(v)
			return nil
		default:
			return fmt.Errorf("cov8: cannot scan %T into *[]byte", value)
		}
	case *time.Time:
		t, ok := value.(time.Time)
		if !ok {
			return fmt.Errorf("cov8: cannot scan %T into *time.Time", value)
		}
		*d = t
		return nil
	case *int:
		n, ok := value.(int)
		if !ok {
			return fmt.Errorf("cov8: cannot scan %T into *int", value)
		}
		*d = n
		return nil
	default:
		return fmt.Errorf("cov8: unsupported destination %T", dest)
	}
}

// cov8ScriptQueryer serves one canned response per QueryContext call in
// order, recording the queries it saw.
type cov8ScriptQueryer struct {
	steps   []cov8Step
	queries []string
}

type cov8Step struct {
	rows [][]any
	err  error
}

func (q *cov8ScriptQueryer) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	q.queries = append(q.queries, query)
	if len(q.steps) == 0 {
		return &cov8Rows{}, nil
	}
	step := q.steps[0]
	q.steps = q.steps[1:]
	if step.err != nil {
		return nil, step.err
	}
	return &cov8Rows{rows: step.rows}, nil
}

func (q *cov8ScriptQueryer) QueryRowContext(context.Context, string, ...any) db.Row {
	return nil
}

// cov8FactRowValues renders one incidentContextFactRow scan row: the
// seven scalar columns plus the JSON payload.
func cov8FactRowValues(factID string, payload map[string]any) []any {
	raw, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return []any{
		factID, "scope-1", "gen-1", "high", "uri:1", "rec-1",
		time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), "1.0.0", raw,
	}
}

// TestCov8IncidentSelectedCommitSHA pins commit selection: the first
// commit-slot edge with exact/derived truth wins, every other shape
// selects nothing.
func TestCov8IncidentSelectedCommitSHA(t *testing.T) {
	t.Parallel()

	commit := func(truth model.IncidentTruthLabel, sha string) model.IncidentContextEvidenceEdge {
		return model.IncidentContextEvidenceEdge{
			Slot: model.IncidentSlotCommit, TruthLabel: truth,
			Value: map[string]string{"commit_sha": sha},
		}
	}
	if got := incidentSelectedCommitSHA([]model.IncidentContextEvidenceEdge{
		commit(model.IncidentTruthExact, "  abc123  "),
	}); got != "abc123" {
		t.Errorf("exact commit = %q, want trimmed abc123", got)
	}
	if got := incidentSelectedCommitSHA([]model.IncidentContextEvidenceEdge{
		commit(model.IncidentTruthDerived, "def456"),
	}); got != "def456" {
		t.Errorf("derived commit = %q, want def456", got)
	}
	for name, edges := range map[string][]model.IncidentContextEvidenceEdge{
		"ambiguous truth": {commit(model.IncidentTruthAmbiguous, "abc123")},
		"wrong slot": {{
			Slot: model.IncidentSlotDeployable, TruthLabel: model.IncidentTruthExact,
			Value: map[string]string{"commit_sha": "abc123"},
		}},
		"empty": nil,
	} {
		if got := incidentSelectedCommitSHA(edges); got != "" {
			t.Errorf("selection(%s) = %q, want empty", name, got)
		}
	}
}

// TestCov8ReviewIssueKeysAndDistinct pins key extraction: PR titles
// yield deduplicated uppercase keys, blanks and duplicates drop.
func TestCov8ReviewIssueKeysAndDistinct(t *testing.T) {
	t.Parallel()

	keys := incidentReviewIssueKeys([]incidentPullRequestEvidence{
		{Title: "Fix OPS-123 and ops-123 again"},
		{Title: "no keys here"},
		{Title: "OPS-456: follow-up"},
	})
	if len(keys) != 2 || keys[0] != "OPS-123" || keys[1] != "OPS-456" {
		t.Errorf("issue keys = %v, want [OPS-123 OPS-456]", keys)
	}
	if got := appendIncidentIssueKey([]string{"OPS-1"}, "  "); len(got) != 1 {
		t.Errorf("append blank = %v, want unchanged", got)
	}
	if got := appendIncidentIssueKey([]string{"OPS-1"}, "OPS-1"); len(got) != 1 {
		t.Errorf("append duplicate = %v, want unchanged", got)
	}
	records := []incidentWorkItemRecord{
		{WorkItemKey: "OPS-1", ProjectID: "p1", StatusID: "s1"},
		{WorkItemKey: "OPS-1", ProjectID: "p1", StatusID: "s2"},
		{WorkItemKey: "OPS-2", ProjectID: "", StatusID: ""},
	}
	if ids := incidentWorkItemProjectIDs(records); len(ids) != 1 || ids[0] != "p1" {
		t.Errorf("project ids = %v, want [p1]", ids)
	}
	if ids := incidentWorkItemStatusIDs(records); len(ids) != 2 {
		t.Errorf("status ids = %v, want [s1 s2]", ids)
	}
}

// TestCov8ReviewWorkItemEvidenceFullPath drives the whole review read
// through the scripted queryer: one PR names the commit, its title key
// joins one work item, and both evidence edges come back.
func TestCov8ReviewWorkItemEvidenceFullPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// No commit edge at all: honest empty without touching the store.
	empty, err := NewStore(nil).readIncidentReviewWorkItemEvidence(ctx, nil)
	if err != nil || empty != nil {
		t.Errorf("no-commit read = %v, %v; want nil, nil", empty, err)
	}

	queryer := &cov8ScriptQueryer{steps: []cov8Step{
		{rows: [][]any{{
			"trigger-1", "github", "org/repo", "abc123", "42",
			"https://example.com/pr/42", "Fix OPS-123",
		}}},
		{rows: [][]any{cov8FactRowValues("fact-wi-1", map[string]any{
			"provider": "jira_cloud", "provider_work_item_id": "10001",
			"work_item_key": "OPS-123", "project_id": "p1", "status_id": "s1",
		})}},
		{rows: [][]any{cov8FactRowValues("fact-proj-1", map[string]any{
			"provider": "jira_cloud", "project_id": "p1",
		})}},
		{rows: [][]any{cov8FactRowValues("fact-status-1", map[string]any{
			"provider": "jira_cloud", "status_id": "s1",
		})}},
	}}
	store := NewStoreWithReadStore(queryer)
	edges, err := store.readIncidentReviewWorkItemEvidence(ctx, []model.IncidentContextEvidenceEdge{{
		Slot: model.IncidentSlotCommit, TruthLabel: model.IncidentTruthExact,
		Value: map[string]string{"commit_sha": "abc123"},
	}})
	if err != nil {
		t.Fatalf("review read = %v, want nil", err)
	}
	if len(edges) != 2 {
		t.Fatalf("review edges = %d, want the PR edge plus the work-item edge", len(edges))
	}
	if edges[0].Slot != model.IncidentSlotPullRequest {
		t.Errorf("first edge slot = %q, want the pull request edge", edges[0].Slot)
	}
	if len(queryer.queries) != 4 {
		t.Errorf("queries issued = %d, want PR + work item + project + status", len(queryer.queries))
	}
}

// TestCov8ReviewReadErrorArms pins the failure propagation: a query
// failure, a scan failure, and a cursor failure each surface wrapped.
func TestCov8ReviewReadErrorArms(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	commitEdges := []model.IncidentContextEvidenceEdge{{
		Slot: model.IncidentSlotCommit, TruthLabel: model.IncidentTruthExact,
		Value: map[string]string{"commit_sha": "abc123"},
	}}

	want := errors.New("query down")
	store := NewStoreWithReadStore(&cov8ScriptQueryer{steps: []cov8Step{{err: want}}})
	if _, err := store.readIncidentReviewWorkItemEvidence(ctx, commitEdges); !errors.Is(err, want) {
		t.Errorf("query failure = %v, want wrap of %v", err, want)
	}

	badScan := NewStoreWithReadStore(&cov8ScriptQueryer{steps: []cov8Step{
		{rows: [][]any{{42, "github", "org/repo", "abc123", "42", "https://example.com", "t"}}},
	}})
	if _, err := badScan.readIncidentReviewWorkItemEvidence(ctx, commitEdges); err == nil {
		t.Error("scan failure = nil, want wrapped scan error")
	}

	rows := &cov8Rows{rows: [][]any{{
		"trigger-1", "github", "org/repo", "abc123", "42", "https://example.com", "t",
	}}, rowErr: errors.New("cursor lost")}
	storeRows := NewStoreWithReadStore(&cov8RowsQueryer{rows: rows})
	if _, err := storeRows.readIncidentPullRequestsByCommit(ctx, "abc123"); err == nil {
		t.Error("cursor failure = nil, want wrapped rows error")
	}
}

// cov8RowsQueryer serves one fixed rows cursor.
type cov8RowsQueryer struct {
	rows db.Rows
}

func (q *cov8RowsQueryer) QueryContext(context.Context, string, ...any) (db.Rows, error) {
	return q.rows, nil
}

func (q *cov8RowsQueryer) QueryRowContext(context.Context, string, ...any) db.Row {
	return nil
}
