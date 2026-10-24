// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package unread

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/securityalerts"
)

type recordedExec struct {
	query string
	args  []any
}

type recordingExecutor struct {
	calls   []recordedExec
	failAt  int
	failErr error
}

func (r *recordingExecutor) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	r.calls = append(r.calls, recordedExec{query: query, args: args})
	if r.failAt > 0 && len(r.calls) == r.failAt {
		return nil, r.failErr
	}
	return nil, nil
}

func sampleState() securityalerts.TargetReadState {
	return securityalerts.TargetReadState{
		GenerationID:      "gen-1",
		ScopeID:           "security-alert:github:org/acme",
		OrgReadTruncated:  true,
		RepositoriesTotal: 4,
		ObservedAt:        time.Date(2026, 10, 24, 1, 2, 3, 0, time.UTC),
		Unread: []securityalerts.UnreadRepository{
			{Repository: "acme/zeta", Reason: "org_repository_fanout_cap_reached"},
			{Repository: "acme/alpha", Reason: "org_repository_read_rate_limited"},
			{Repository: "acme/alpha", Reason: "org_repository_read_rate_limited"},
		},
	}
}

func TestWriterRecordsParentThenReplacesChildrenInOrder(t *testing.T) {
	exec := &recordingExecutor{}
	if err := Writer(sampleState())(context.Background(), exec); err != nil {
		t.Fatalf("Writer() error = %v", err)
	}
	if got := len(exec.calls); got != 3 {
		t.Fatalf("statements = %d, want 3 (parent upsert, child delete, child insert)", got)
	}
	if !strings.Contains(exec.calls[0].query, "INSERT INTO security_alert_target_read_states") {
		t.Errorf("statement 1 = %q, want the parent upsert", exec.calls[0].query)
	}
	if !strings.Contains(exec.calls[1].query, "DELETE FROM security_alert_unread_repositories") {
		t.Errorf("statement 2 = %q, want the child delete", exec.calls[1].query)
	}
	if !strings.Contains(exec.calls[2].query, "INSERT INTO security_alert_unread_repositories") {
		t.Errorf("statement 3 = %q, want the child insert", exec.calls[2].query)
	}
	// Duplicates collapse and the order is sorted so concurrent writers of
	// the same generation lock child rows in one order.
	repos, ok := exec.calls[2].args[1].(driver.Valuer)
	if !ok {
		t.Fatalf("child insert repositories arg = %T, want a Postgres array value", exec.calls[2].args[1])
	}
	literal, err := repos.Value()
	if err != nil {
		t.Fatalf("repositories Value() error = %v", err)
	}
	if want := `{"acme/alpha","acme/zeta"}`; literal != want {
		t.Errorf("child insert repositories = %v, want %s", literal, want)
	}
	if got := exec.calls[0].args[4]; got != 2 {
		t.Errorf("parent unread_total = %v, want 2 after de-duplication", got)
	}
}

func TestWriterSkipsChildInsertWhenNothingIsUnread(t *testing.T) {
	state := sampleState()
	state.Unread = nil
	exec := &recordingExecutor{}
	if err := Writer(state)(context.Background(), exec); err != nil {
		t.Fatalf("Writer() error = %v", err)
	}
	if got := len(exec.calls); got != 2 {
		t.Fatalf("statements = %d, want 2 (parent upsert, child delete)", got)
	}
}

func TestWriterRejectsIncompleteState(t *testing.T) {
	cases := map[string]func(*securityalerts.TargetReadState){
		"missing generation": func(s *securityalerts.TargetReadState) { s.GenerationID = " " },
		"missing scope":      func(s *securityalerts.TargetReadState) { s.ScopeID = "" },
		"missing repository": func(s *securityalerts.TargetReadState) { s.Unread[0].Repository = "" },
		"missing reason":     func(s *securityalerts.TargetReadState) { s.Unread[0].Reason = "" },
		"negative total":     func(s *securityalerts.TargetReadState) { s.RepositoriesTotal = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			state := sampleState()
			mutate(&state)
			exec := &recordingExecutor{}
			if err := Writer(state)(context.Background(), exec); err == nil {
				t.Fatal("Writer() error = nil, want a validation error")
			}
			if len(exec.calls) != 0 {
				t.Errorf("statements = %d, want none for invalid state", len(exec.calls))
			}
		})
	}
}

func TestWriterWrapsStatementFailureWithoutRepositoryNames(t *testing.T) {
	cause := errors.New("boom")
	exec := &recordingExecutor{failAt: 3, failErr: cause}
	err := Writer(sampleState())(context.Background(), exec)
	if !errors.Is(err, cause) {
		t.Fatalf("Writer() error = %v, want it to wrap the statement error", err)
	}
	if strings.Contains(err.Error(), "acme/") {
		t.Errorf("error %q names a repository", err)
	}
}

func TestWriterStoresRepositorySlugsLowercaseLikeTheReadProbe(t *testing.T) {
	state := sampleState()
	state.Unread = []securityalerts.UnreadRepository{
		{Repository: " Acme/Zeta ", Reason: "org_repository_fanout_cap_reached"},
		{Repository: "ACME/alpha", Reason: "org_repository_read_rate_limited"},
		{Repository: "acme/Alpha", Reason: "org_repository_read_rate_limited"},
	}
	exec := &recordingExecutor{}
	if err := Writer(state)(context.Background(), exec); err != nil {
		t.Fatalf("Writer() error = %v", err)
	}
	repos, ok := exec.calls[2].args[1].(driver.Valuer)
	if !ok {
		t.Fatalf("child insert repositories arg = %T, want a Postgres array value", exec.calls[2].args[1])
	}
	literal, err := repos.Value()
	if err != nil {
		t.Fatalf("repositories Value() error = %v", err)
	}
	if want := `{"acme/alpha","acme/zeta"}`; literal != want {
		t.Errorf("child insert repositories = %v, want %s (lowercase, case twins collapsed)", literal, want)
	}
}

func TestWriterRejectsCaseTwinsWithConflictingReasons(t *testing.T) {
	state := sampleState()
	state.Unread = []securityalerts.UnreadRepository{
		{Repository: "Acme/Alpha", Reason: "org_repository_read_rate_limited"},
		{Repository: "acme/alpha", Reason: "org_repository_fanout_cap_reached"},
	}
	exec := &recordingExecutor{}
	if err := Writer(state)(context.Background(), exec); err == nil {
		t.Fatal("Writer() error = nil, want a conflicting-reasons error for case twins")
	}
	if len(exec.calls) != 0 {
		t.Errorf("statements = %d, want none for invalid state", len(exec.calls))
	}
}

func TestWriterRecordsFanoutCursorOnTheParentRow(t *testing.T) {
	state := sampleState()
	state.FanoutStartRepository = " Acme/Alpha "
	state.FanoutNextRepository = "acme/zeta"
	state.FanoutLapRepository = " Acme/Omega "
	exec := &recordingExecutor{}
	if err := Writer(state)(context.Background(), exec); err != nil {
		t.Fatalf("Writer() error = %v", err)
	}
	args := exec.calls[0].args
	if len(args) != 10 {
		t.Fatalf("parent upsert args = %d, want 10 (the three fan-out cursor columns last)", len(args))
	}
	if args[7] != "acme/alpha" || args[8] != "acme/zeta" || args[9] != "acme/omega" {
		t.Errorf("fan-out cursor args = %v, %v, %v; want lowercase trimmed slugs acme/alpha, acme/zeta, acme/omega", args[7], args[8], args[9])
	}
	for _, column := range []string{"fanout_start_repository", "fanout_next_repository", "fanout_lap_repository"} {
		if !strings.Contains(exec.calls[0].query, column+" = EXCLUDED."+column) {
			t.Errorf("parent upsert does not replace %s on conflict", column)
		}
	}
}

func TestWriterStoresNoCursorAsNull(t *testing.T) {
	exec := &recordingExecutor{}
	if err := Writer(sampleState())(context.Background(), exec); err != nil {
		t.Fatalf("Writer() error = %v", err)
	}
	args := exec.calls[0].args
	if args[7] != nil || args[8] != nil || args[9] != nil {
		t.Errorf("empty cursor args = %v, %v, %v; want NULL (nil): the top, and no lap", args[7], args[8], args[9])
	}
}
