// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestSharedIntentStoreClaimPartitionLeaseSurfacesRowsError(t *testing.T) {
	t.Parallel()

	store := NewSharedIntentStore(leaseRowsErrorDB{rowsErr: context.Canceled})
	claimed, err := store.ClaimPartitionLease(
		context.Background(),
		"platform_infra",
		0,
		1,
		"worker-1",
		30*time.Second,
	)
	if claimed {
		t.Fatal("ClaimPartitionLease() claimed = true, want false")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ClaimPartitionLease() error = %v, want context.Canceled", err)
	}
}

type leaseRowsErrorDB struct {
	rowsErr error
}

func (db leaseRowsErrorDB) QueryContext(context.Context, string, ...any) (Rows, error) {
	return &leaseRowsErrorRows{err: db.rowsErr}, nil
}

func (leaseRowsErrorDB) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("unexpected ExecContext call")
}

type leaseRowsErrorRows struct {
	err error
}

func (*leaseRowsErrorRows) Next() bool { return false }

func (*leaseRowsErrorRows) Scan(...any) error {
	return errors.New("unexpected Scan call")
}

func (*leaseRowsErrorRows) Close() error { return nil }

func (r *leaseRowsErrorRows) Err() error { return r.err }
