// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

// #7385: the admin listing reads failure_details of each row and exposes the
// operator note and the prior failure that #7388 and #7320 keep there.

func TestApplyWorkItemDetailsParsesNoteAndPriorFailure(t *testing.T) {
	t.Parallel()

	var item admin.WorkItem
	applyWorkItemDetails(&item, sql.NullString{Valid: true, String: `{"operator_note":"triaged","prior_failure":` +
		`{"status":"dead_letter","failure_class":"graph_write_timeout","failure_message":"timed out",` +
		`"failure_details":"phase=semantic","updated_at":"2026-06-09T09:00:00.5+00:00"}}`})

	if item.OperatorNote == nil || *item.OperatorNote != "triaged" {
		t.Fatalf("OperatorNote = %v, want triaged", item.OperatorNote)
	}
	want := admin.PriorFailure{Status: "dead_letter", FailureClass: "graph_write_timeout", FailureMessage: "timed out", UpdatedAt: "2026-06-09T09:00:00Z"}
	if item.PriorFailure == nil || *item.PriorFailure != want {
		t.Fatalf("PriorFailure = %+v, want %+v", item.PriorFailure, want)
	}
}

func TestApplyWorkItemDetailsLeavesFieldsNilWithoutAnObject(t *testing.T) {
	t.Parallel()

	for name, details := range map[string]sql.NullString{
		"null":          {},
		"empty":         {Valid: true, String: ""},
		"free text":     {Valid: true, String: "phase=semantic rows=500"},
		"array":         {Valid: true, String: `[1]`},
		"object no key": {Valid: true, String: `{"scope_id":"s"}`},
		"bad prior":     {Valid: true, String: `{"prior_failure":"oops"}`},
	} {
		var item admin.WorkItem
		applyWorkItemDetails(&item, details)
		if item.OperatorNote != nil || item.PriorFailure != nil {
			t.Errorf("%s: note=%v prior=%+v, want both nil", name, item.OperatorNote, item.PriorFailure)
		}
	}
}

func TestListWorkItemsQuerySelectsFailureDetails(t *testing.T) {
	t.Parallel()

	database := &recordingAdminExecQueryer{rows: &recordingAdminRows{}}
	s := &postgresStore{database: database, now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
	if _, err := s.ListWorkItems(context.Background(), admin.WorkItemFilter{Limit: 5}); err != nil {
		t.Fatalf("ListWorkItems() error = %v", err)
	}
	if !strings.Contains(database.query, "failure_details") {
		t.Fatalf("list query does not select failure_details:\n%s", database.query)
	}
	mutating, _ := buildMutatingWorkItemsQuery(nil, "", "", "", 10, 0, false, false, "SET status = 'dead_letter'\n")
	if !strings.Contains(mutating, "work.failure_details") {
		t.Fatalf("mutating RETURNING does not carry failure_details:\n%s", mutating)
	}
}

// TestApplyWorkItemDetailsDecodesTheKeysIndependently: one malformed key must not
// discard the other, valid one.
func TestApplyWorkItemDetailsDecodesTheKeysIndependently(t *testing.T) {
	t.Parallel()

	var badPrior admin.WorkItem
	applyWorkItemDetails(&badPrior, sql.NullString{Valid: true, String: `{"operator_note":"triaged","prior_failure":{"status":5}}`})
	if badPrior.OperatorNote == nil || *badPrior.OperatorNote != "triaged" || badPrior.PriorFailure != nil {
		t.Fatalf("wrong-typed prior_failure: note=%v prior=%+v, want the note kept and no prior", badPrior.OperatorNote, badPrior.PriorFailure)
	}
	var badNote admin.WorkItem
	applyWorkItemDetails(&badNote, sql.NullString{Valid: true, String: `{"operator_note":5,"prior_failure":{"status":"failed","failure_class":"c"}}`})
	if badNote.OperatorNote != nil || badNote.PriorFailure == nil || badNote.PriorFailure.FailureClass != "c" {
		t.Fatalf("wrong-typed operator_note: note=%v prior=%+v, want the prior kept and no note", badNote.OperatorNote, badNote.PriorFailure)
	}
}
