// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fake_test

import (
	"database/sql"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/fake"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/pgarray"
)

func TestRowsScanConvertsEachSupportedDestinationType(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 12, 11, 0, 0, 0, time.UTC)
	row := []any{
		"a-string",
		true,
		[]byte("blob"),
		now,
		42,
		int64(43),
		1.5,
		[]float64{1, 2},
		[]float64{3, 4},
		sql.NullString{String: "ns", Valid: true},
		sql.NullBool{Bool: true, Valid: true},
		sql.NullInt64{Int64: 7, Valid: true},
		sql.NullTime{Time: now, Valid: true},
	}
	rows := &fake.Rows{Data: [][]any{row}}

	var (
		gotString    string
		gotBool      bool
		gotBytes     []byte
		gotTime      time.Time
		gotInt       int
		gotInt64     int64
		gotFloat64   float64
		gotFloatSl   []float64
		gotPgArray   pgarray.Float64Array
		gotNullStr   sql.NullString
		gotNullBool  sql.NullBool
		gotNullInt64 sql.NullInt64
		gotNullTime  sql.NullTime
	)

	if !rows.Next() {
		t.Fatal("Next() = false, want true")
	}
	err := rows.Scan(
		&gotString, &gotBool, &gotBytes, &gotTime, &gotInt, &gotInt64,
		&gotFloat64, &gotFloatSl, &gotPgArray, &gotNullStr, &gotNullBool,
		&gotNullInt64, &gotNullTime,
	)
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	if gotString != "a-string" {
		t.Errorf("string = %q, want %q", gotString, "a-string")
	}
	if !gotBool {
		t.Errorf("bool = %v, want true", gotBool)
	}
	if string(gotBytes) != "blob" {
		t.Errorf("bytes = %q, want %q", gotBytes, "blob")
	}
	if !gotTime.Equal(now) {
		t.Errorf("time = %v, want %v", gotTime, now)
	}
	if gotInt != 42 {
		t.Errorf("int = %d, want 42", gotInt)
	}
	if gotInt64 != 43 {
		t.Errorf("int64 = %d, want 43", gotInt64)
	}
	if gotFloat64 != 1.5 {
		t.Errorf("float64 = %v, want 1.5", gotFloat64)
	}
	if len(gotFloatSl) != 2 || gotFloatSl[0] != 1 || gotFloatSl[1] != 2 {
		t.Errorf("[]float64 = %v, want [1 2]", gotFloatSl)
	}
	if len(gotPgArray) != 2 || gotPgArray[0] != 3 || gotPgArray[1] != 4 {
		t.Errorf("pgarray.Float64Array = %v, want [3 4]", gotPgArray)
	}
	if !gotNullStr.Valid || gotNullStr.String != "ns" {
		t.Errorf("sql.NullString = %+v, want {ns true}", gotNullStr)
	}
	if !gotNullBool.Valid || !gotNullBool.Bool {
		t.Errorf("sql.NullBool = %+v, want {true true}", gotNullBool)
	}
	if !gotNullInt64.Valid || gotNullInt64.Int64 != 7 {
		t.Errorf("sql.NullInt64 = %+v, want {7 true}", gotNullInt64)
	}
	if !gotNullTime.Valid || !gotNullTime.Time.Equal(now) {
		t.Errorf("sql.NullTime = %+v, want {%v true}", gotNullTime, now)
	}

	if rows.Next() {
		t.Fatal("Next() = true after last row, want false")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
	if err := rows.Close(); err != nil {
		t.Fatalf("Close() = %v, want nil", err)
	}
}

func TestRowsScanRejectsMismatchedDestinationCount(t *testing.T) {
	t.Parallel()

	rows := &fake.Rows{Data: [][]any{{"one", "two"}}}
	rows.Next()
	var dest string
	if err := rows.Scan(&dest); err == nil {
		t.Fatal("Scan() error = nil, want a destination-count mismatch error")
	}
}

func TestRowsScanRejectsWrongType(t *testing.T) {
	t.Parallel()

	rows := &fake.Rows{Data: [][]any{{42}}}
	rows.Next()
	var dest string
	if err := rows.Scan(&dest); err == nil {
		t.Fatal("Scan() error = nil, want a type-mismatch error")
	}
}

func TestRowsScanAfterLastRowErrors(t *testing.T) {
	t.Parallel()

	rows := &fake.Rows{Data: [][]any{{"one"}}}
	var dest string
	if !rows.Next() {
		t.Fatal("Next() = false, want true for the first row")
	}
	if err := rows.Scan(&dest); err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	if rows.Next() {
		t.Fatal("Next() = true after the only row, want false")
	}
	if err := rows.Scan(&dest); err == nil {
		t.Fatal("Scan() error = nil, want an error once rows are exhausted")
	}
}

func TestResultDefaults(t *testing.T) {
	t.Parallel()

	result := fake.Result{}
	if id, err := result.LastInsertId(); err != nil || id != 0 {
		t.Fatalf("LastInsertId() = (%d, %v), want (0, nil)", id, err)
	}
	if affected, err := result.RowsAffected(); err != nil || affected != 1 {
		t.Fatalf("RowsAffected() = (%d, %v), want (1, nil)", affected, err)
	}
}
