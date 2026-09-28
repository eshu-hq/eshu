// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/query/admin"
)

func TestBuildListChangedSincePoisonedLinksQuery_BuildsBoundedFilteredQuery(t *testing.T) {
	t.Parallel()

	query, args := buildListChangedSincePoisonedLinksQuery(admin.ChangedSincePoisonedLinkFilter{
		Status:               "poisoned",
		ScopeID:              "scope-a",
		Cursor:               "scope-0",
		AllowedRepositoryIDs: []string{"repo-a"},
		AllowedScopeIDs:      []string{"scope-a"},
		Limit:                11,
	})

	requiredFragments := []string{
		"FROM changed_since_scope_cursor AS cursor",
		"JOIN ingestion_scopes AS scope ON scope.scope_id = cursor.scope_id",
		"(cursor.poisoned_activation_seq IS NOT NULL OR cursor.attempt_count > 0)",
		"cursor.poisoned_activation_seq IS NOT NULL",
		"cursor.scope_id = $1",
		"cursor.scope_id > $2",
		"((scope.scope_kind = 'repository' AND scope.source_key = ANY($3)) OR cursor.scope_id = ANY($4))",
		"ORDER BY cursor.scope_id ASC",
		"LIMIT $5",
	}
	for _, fragment := range requiredFragments {
		if !strings.Contains(query, fragment) {
			t.Fatalf("query missing %q:\n%s", fragment, query)
		}
	}
	if got, want := maxPlaceholderIndex(query), len(args); got != want {
		t.Fatalf("max placeholder index = %d, want %d; query = %s", got, want, query)
	}
	if got, want := args[4], 11; got != want {
		t.Fatalf("limit arg = %#v, want %#v", got, want)
	}
}

func TestBuildListChangedSincePoisonedLinksQuery_RetryingStatusExcludesPoisoned(t *testing.T) {
	t.Parallel()

	query, _ := buildListChangedSincePoisonedLinksQuery(admin.ChangedSincePoisonedLinkFilter{
		Status: "retrying",
		Limit:  10,
	})
	if !strings.Contains(query, "cursor.poisoned_activation_seq IS NULL AND cursor.attempt_count > 0") {
		t.Fatalf("query missing retrying predicate:\n%s", query)
	}
	if strings.Contains(query, "cursor.poisoned_activation_seq IS NOT NULL\n") {
		t.Fatalf("retrying filter unexpectedly also applies the poisoned predicate:\n%s", query)
	}
}

func TestBuildListChangedSincePoisonedLinksQuery_NoGrantsOmitsAuthorizationClause(t *testing.T) {
	t.Parallel()

	query, args := buildListChangedSincePoisonedLinksQuery(admin.ChangedSincePoisonedLinkFilter{
		Limit: 10,
	})
	if strings.Contains(query, "scope.source_key") {
		t.Fatalf("query unexpectedly contains an authorization clause with no grants:\n%s", query)
	}
	if got, want := maxPlaceholderIndex(query), len(args); got != want {
		t.Fatalf("max placeholder index = %d, want %d; query = %s", got, want, query)
	}
}

// TestScanChangedSincePoisonedLinks_DerivesStatusAndActivationSeq proves the
// scan derives Status and ActivationSeq from the two nullable activation-seq
// columns rather than trusting a status column the schema does not carry
// (changed_since_scope_cursor has no status column, #7127 migration 136):
// poisoned_activation_seq set means poisoned and uses that value;
// otherwise attempt_count > 0 means retrying and uses
// attempt_activation_seq.
func TestScanChangedSincePoisonedLinks_DerivesStatusAndActivationSeq(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	rows := &fakeChangedSincePoisonedLinkRows{
		rows: [][]any{
			{"scope-a", int64(42), int64(40), 5, nil, "statement_timeout", now.Add(-time.Minute), now},
			{"scope-b", nil, int64(7), 2, now.Add(time.Minute), "sql_error", nil, now},
		},
	}
	database := &recordingAdminExecQueryer{rows: rows}

	items, err := scanChangedSincePoisonedLinks(context.Background(), database, "SELECT 1")
	if err != nil {
		t.Fatalf("scanChangedSincePoisonedLinks() error = %v, want nil", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}

	poisoned := items[0]
	if poisoned.Status != "poisoned" || poisoned.ActivationSeq != 42 {
		t.Fatalf("poisoned row = %#v, want status=poisoned activation_seq=42", poisoned)
	}
	if poisoned.PoisonedAt == nil || poisoned.NextAttemptAt != nil {
		t.Fatalf("poisoned row = %#v, want PoisonedAt set and NextAttemptAt nil", poisoned)
	}

	retrying := items[1]
	if retrying.Status != "retrying" || retrying.ActivationSeq != 7 {
		t.Fatalf("retrying row = %#v, want status=retrying activation_seq=7", retrying)
	}
	if retrying.NextAttemptAt == nil || retrying.PoisonedAt != nil {
		t.Fatalf("retrying row = %#v, want NextAttemptAt set and PoisonedAt nil", retrying)
	}
}

// fakeChangedSincePoisonedLinkRows scans fixed column values in the exact
// order scanChangedSincePoisonedLinks reads them: scope_id,
// poisoned_activation_seq, attempt_activation_seq, attempt_count,
// next_attempt_at, last_failure_class, poisoned_at, updated_at.
type fakeChangedSincePoisonedLinkRows struct {
	rows [][]any
	idx  int
}

func (r *fakeChangedSincePoisonedLinkRows) Next() bool {
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeChangedSincePoisonedLinkRows) Scan(dest ...any) error {
	row := r.rows[r.idx-1]
	return scanInto(row, dest)
}

func (r *fakeChangedSincePoisonedLinkRows) Err() error   { return nil }
func (r *fakeChangedSincePoisonedLinkRows) Close() error { return nil }

// scanInto copies row's fixed-order values into dest, converting a nil
// column value into the zero value of the sql.Null* dest type it targets.
// It exists only to drive scanChangedSincePoisonedLinks from an in-memory
// fixture; it supports exactly the dest types that function scans.
func scanInto(row []any, dest []any) error {
	if len(row) != len(dest) {
		return fmt.Errorf("scanInto: row has %d columns, dest has %d", len(row), len(dest))
	}
	for i, d := range dest {
		v := row[i]
		switch ptr := d.(type) {
		case *string:
			*ptr = v.(string)
		case *int:
			*ptr = v.(int)
		case *sql.NullInt64:
			if v == nil {
				*ptr = sql.NullInt64{}
			} else {
				*ptr = sql.NullInt64{Int64: v.(int64), Valid: true}
			}
		case *sql.NullString:
			if v == nil {
				*ptr = sql.NullString{}
			} else {
				*ptr = sql.NullString{String: v.(string), Valid: true}
			}
		case *sql.NullTime:
			if v == nil {
				*ptr = sql.NullTime{}
			} else {
				*ptr = sql.NullTime{Time: v.(time.Time), Valid: true}
			}
		case *time.Time:
			*ptr = v.(time.Time)
		default:
			return fmt.Errorf("scanInto: unsupported dest type %T", d)
		}
	}
	return nil
}
