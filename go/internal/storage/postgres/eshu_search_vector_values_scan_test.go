// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"database/sql"
	"fmt"
	"testing"
	"time"
)

type pgxStringVectorRows struct{}

func (pgxStringVectorRows) Next() bool   { return false }
func (pgxStringVectorRows) Err() error   { return nil }
func (pgxStringVectorRows) Close() error { return nil }

func (pgxStringVectorRows) Scan(dest ...any) error {
	if len(dest) != 12 {
		return fmt.Errorf("scan arity = %d, want 12", len(dest))
	}
	strings := []struct {
		index int
		value string
	}{
		{0, "scope-1"},
		{1, "generation-1"},
		{2, "document-1"},
		{3, "local"},
		{4, "search_documents"},
		{5, "local-hash-v1"},
		{7, "sha256:document"},
		{8, "vector-v1"},
	}
	for _, item := range strings {
		target, ok := dest[item.index].(*string)
		if !ok {
			return fmt.Errorf("column %d target = %T, want *string", item.index, dest[item.index])
		}
		*target = item.value
	}
	dimensions, ok := dest[6].(*int64)
	if !ok {
		return fmt.Errorf("dimensions target = %T, want *int64", dest[6])
	}
	*dimensions = 2
	scanner, ok := dest[9].(sql.Scanner)
	if !ok {
		return fmt.Errorf("unsupported Scan, storing driver.Value type string into type %T", dest[9])
	}
	if err := scanner.Scan("{0.25,-0.5}"); err != nil {
		return err
	}
	createdAt, ok := dest[10].(*time.Time)
	if !ok {
		return fmt.Errorf("created_at target = %T, want *time.Time", dest[10])
	}
	updatedAt, ok := dest[11].(*time.Time)
	if !ok {
		return fmt.Errorf("updated_at target = %T, want *time.Time", dest[11])
	}
	*createdAt = time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC)
	*updatedAt = *createdAt
	return nil
}

func TestScanEshuSearchVectorValueDecodesPGXTextArray(t *testing.T) {
	t.Parallel()

	row, err := scanEshuSearchVectorValue(pgxStringVectorRows{})
	if err != nil {
		t.Fatalf("scanEshuSearchVectorValue error = %v", err)
	}
	if got, want := fmt.Sprint(row.VectorValues), "[0.25 -0.5]"; got != want {
		t.Fatalf("vector values = %s, want %s", got, want)
	}
	if got, want := row.EmbeddingDimensions, 2; got != want {
		t.Fatalf("embedding dimensions = %d, want %d", got, want)
	}
}
