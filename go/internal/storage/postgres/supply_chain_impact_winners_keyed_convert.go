// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// convertWinnersKeyedScopeRemainder runs convertWinnersKeyedScopeRemainderSQL
// in the tick transaction and returns the fact rows it wrote, inserted or
// merged into a queued row (#11145: the scope_remainder_converted_rows_total
// signal). A driver that cannot report affected rows yields 0, never an
// error: the count is telemetry, and the conversion itself already
// succeeded.
func convertWinnersKeyedScopeRemainder(ctx context.Context, tx db.Transaction) (int64, error) {
	result, err := tx.ExecContext(ctx, convertWinnersKeyedScopeRemainderSQL)
	if err != nil {
		return 0, fmt.Errorf("convert supply chain impact winners scope remainder: %w", err)
	}
	if result == nil {
		return 0, nil
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, nil
	}
	return n, nil
}
