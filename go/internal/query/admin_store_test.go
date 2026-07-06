// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	pgstatus "github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

func TestPostgresAdminStoreReplayFailedWorkItems_UsesConsistentPlaceholderOffsets(t *testing.T) {
	t.Parallel()

	db := &recordingAdminExecQueryer{
		rows: &recordingAdminRows{},
	}
	store := &postgresAdminStore{
		db:  db,
		now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	_, err := store.ReplayFailedWorkItems(context.Background(), ReplayWorkItemFilter{
		WorkItemIDs:  []string{"wi-1"},
		OperatorNote: "retry after reducer failure",
		Limit:        10,
	})
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}

	if got, want := len(db.queryArgs), 3; got != want {
		t.Fatalf("len(queryArgs) = %d, want %d", got, want)
	}
	if got, want := maxPlaceholderIndex(db.query), len(db.queryArgs); got != want {
		t.Fatalf("max placeholder index = %d, want %d; query = %s", got, want, db.query)
	}
	if !strings.Contains(db.query, "work_item_id = ANY($2)") {
		t.Fatalf("query = %q, want work_item_id selector to use $2", db.query)
	}
	if !strings.Contains(db.query, "LIMIT $3") {
		t.Fatalf("query = %q, want limit selector to use $3", db.query)
	}
}

func TestPostgresAdminStoreReplayFailedWorkItems_PreservesRetrySemantics(t *testing.T) {
	t.Parallel()

	db := &recordingAdminExecQueryer{
		rows: &recordingAdminRows{},
	}
	store := &postgresAdminStore{
		db:  db,
		now: func() time.Time { return time.Unix(1700000000, 0).UTC() },
	}

	_, err := store.ReplayFailedWorkItems(context.Background(), ReplayWorkItemFilter{
		Stage:        "reducer",
		FailureClass: "reducer_failed",
		Limit:        25,
	})
	if err != nil {
		t.Fatalf("ReplayFailedWorkItems() error = %v, want nil", err)
	}

	if strings.Contains(db.query, "attempt_count = 0") {
		t.Fatalf("replay query resets retry evidence:\n%s", db.query)
	}
	if !strings.Contains(db.query, "attempt_count = GREATEST(work.attempt_count, 1)") {
		t.Fatalf("replay query missing retry-preserving attempt_count:\n%s", db.query)
	}
}

func TestPostgresAdminStoreListDeadLetterWorkItems_BuildsBoundedFilteredQuery(t *testing.T) {
	t.Parallel()

	after := time.Date(2026, 7, 6, 13, 0, 0, 0, time.UTC)
	before := after.Add(time.Hour)
	db := &recordingAdminExecQueryer{
		rows: &recordingAdminRows{},
	}
	store := &postgresAdminStore{db: db}

	_, err := store.ListDeadLetterWorkItems(context.Background(), DeadLetterListFilter{
		FailureClass:         "projection_bug",
		Domain:               "runtime",
		ScopeID:              "scope-a",
		CollectorKind:        "git",
		UpdatedAfter:         &after,
		UpdatedBefore:        &before,
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
		Limit:                11,
	})
	if err != nil {
		t.Fatalf("ListDeadLetterWorkItems() error = %v, want nil", err)
	}

	requiredFragments := []string{
		"FROM fact_work_items AS work",
		"JOIN ingestion_scopes AS scope ON scope.scope_id = work.scope_id",
		"work.status = 'dead_letter'",
		"work.failure_class = $1",
		"work.domain = $2",
		"work.scope_id = $3",
		"scope.collector_kind = $4",
		"work.updated_at >= $5",
		"work.updated_at < $6",
		"((scope.scope_kind = 'repository' AND scope.source_key = ANY($7)) OR work.scope_id = ANY($8))",
		"ORDER BY work.updated_at DESC, work.work_item_id ASC",
		"LIMIT $9",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(db.query, fragment) {
			t.Fatalf("query missing %q:\n%s", fragment, db.query)
		}
	}
	if got, want := maxPlaceholderIndex(db.query), len(db.queryArgs); got != want {
		t.Fatalf("max placeholder index = %d, want %d; query = %s", got, want, db.query)
	}
	if got, want := db.queryArgs[8], 11; got != want {
		t.Fatalf("limit arg = %#v, want %#v", got, want)
	}
}

type recordingAdminExecQueryer struct {
	query     string
	queryArgs []any
	rows      pgstatus.Rows
}

func (db *recordingAdminExecQueryer) QueryContext(_ context.Context, query string, args ...any) (pgstatus.Rows, error) {
	db.query = query
	db.queryArgs = append([]any(nil), args...)
	return db.rows, nil
}

func (*recordingAdminExecQueryer) ExecContext(_ context.Context, _ string, _ ...any) (sql.Result, error) {
	return nil, fmt.Errorf("unexpected ExecContext call")
}

type recordingAdminRows struct{}

func (*recordingAdminRows) Next() bool        { return false }
func (*recordingAdminRows) Scan(...any) error { return fmt.Errorf("unexpected Scan call") }
func (*recordingAdminRows) Err() error        { return nil }
func (*recordingAdminRows) Close() error      { return nil }

func maxPlaceholderIndex(query string) int {
	matches := regexp.MustCompile(`\$(\d+)`).FindAllStringSubmatch(query, -1)
	max := 0
	for _, match := range matches {
		value, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		if value > max {
			max = value
		}
	}
	return max
}
