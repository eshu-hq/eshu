// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

// #7388: an operator note used to replace failure_details outright, so
// dead-lettering or skipping a row with a note erased the details it failed
// with (triage details, or a skipped retrying row's retry details). The note now wraps them: the row keeps them under
// prior_failure. These recording tests pin the statement text; the live test in
// operator_note_live_test.go proves the stored result.

const noteFoldCase = "failure_details = CASE\n" +
	"        WHEN NULLIF($%d, '') IS NULL THEN work.failure_details\n" +
	"        ELSE (jsonb_build_object('operator_note', $%d::text) || "

func TestDeadLetterWorkItemsFoldsPriorFailureUnderTheOperatorNote(t *testing.T) {
	t.Parallel()

	database := &recordingAdminExecQueryer{rows: &recordingAdminRows{}}
	s := &postgresStore{database: database, now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
	if _, err := s.DeadLetterWorkItems(context.Background(), admin.DeadLetterFilter{
		WorkItemIDs: []string{"wi-1"}, OperatorNote: "  triaged by hand  ",
	}); err != nil {
		t.Fatalf("DeadLetterWorkItems() error = %v", err)
	}
	requireNoteFold(t, "dead-letter", database.query, 2)
	if got := database.queryArgs[1]; got != "triaged by hand" {
		t.Fatalf("operator note arg = %q, want the trimmed note", got)
	}
}

func TestSkipRepositoryWorkItemsFoldsPriorFailureUnderTheOperatorNote(t *testing.T) {
	t.Parallel()

	database := &recordingAdminExecQueryer{rows: &recordingAdminRows{}}
	s := &postgresStore{database: database, now: func() time.Time { return time.Unix(1700000000, 0).UTC() }}
	if _, err := s.SkipRepositoryWorkItems(context.Background(), "repo-a", "skip: scope is retired"); err != nil {
		t.Fatalf("SkipRepositoryWorkItems() error = %v", err)
	}
	requireNoteFold(t, "skip", database.query, 3)
	if !strings.Contains(database.query, "failure_class = COALESCE(NULLIF(work.failure_class, ''), 'operator_skipped')") {
		t.Fatalf("skip must treat an empty failure_class like a missing one, as dead-letter does:\n%s", database.query)
	}
}

// requireNoteFold asserts the statement wraps a non-empty note around the
// work-alias fold and leaves the details untouched when the note is empty.
func requireNoteFold(t *testing.T, label, query string, noteParam int) {
	t.Helper()
	want := strings.Replace(strings.Replace(noteFoldCase, "%d", strconv.Itoa(noteParam), 1), "%d", strconv.Itoa(noteParam), 1) +
		pgstatus.PriorFailureWorkSQL + ")::text\n    END"
	if !strings.Contains(strings.Join(strings.Fields(query), " "), strings.Join(strings.Fields(want), " ")) {
		t.Fatalf("%s statement does not wrap the operator note around the prior failure:\nwant (whitespace-normalized) %s\ngot %s", label, want, query)
	}
	if strings.Contains(query, "COALESCE(NULLIF($") {
		t.Fatalf("%s statement still replaces failure_details with the note:\n%s", label, query)
	}
}
