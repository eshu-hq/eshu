// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package summary

import (
	"context"
	"errors"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// WriterLockKey is the single-argument advisory lock key a writer pass takes
// with pg_try_advisory_xact_lock before it computes and upserts, so exactly one
// reducer replica writes per tick. It is the ASCII bytes of "ESHUSSUM", the
// same scheme as the other single-key advisory locks in this repository, and
// a test fails if it ever equals another advisory key constant.
const WriterLockKey int64 = 0x455348555353554d

// TryLock takes the writer's transaction-scoped advisory lock without waiting
// and reports whether this caller now holds it. It must run on the transaction
// that will also call Upsert: the lock is released when that transaction ends,
// so a crashed holder frees it with its backend and no lease table or expiry is
// needed. A false result means another writer holds the lock; the caller skips
// the pass.
func TryLock(ctx context.Context, tx db.Queryer) (bool, error) {
	rows, err := tx.QueryContext(ctx, tryLockSQL, WriterLockKey)
	if err != nil {
		return false, fmt.Errorf("try status summary writer lock: %w", err)
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return false, fmt.Errorf("try status summary writer lock: %w", err)
		}
		return false, errors.New("try status summary writer lock: no result row")
	}
	var acquired bool
	if err := rows.Scan(&acquired); err != nil {
		return false, fmt.Errorf("scan status summary writer lock: %w", err)
	}
	return acquired, rows.Err()
}
