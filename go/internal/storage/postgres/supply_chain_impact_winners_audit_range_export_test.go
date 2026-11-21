// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// WinnersAuditFirstRangeSQLForTest and WinnersAuditNextRangeSQLForTest expose
// the shipped range statements (#11131), so the differential can derive its
// seeded mutation from them instead of hand-copying.
var (
	WinnersAuditFirstRangeSQLForTest = selectWinnersAuditFirstRangeSQL
	WinnersAuditNextRangeSQLForTest  = selectWinnersAuditNextRangeSQL
)

// ReadWinnersAuditRangeWithSQLForTest runs one range with the given statement
// pair, for the seeded-RED differential.
func ReadWinnersAuditRangeWithSQLForTest(ctx context.Context, reader db.Queryer, lo *WinnersAuditRangeIdent, budget int, first, next string) (WinnersAuditRange, error) {
	return readWinnersAuditRange(ctx, reader, lo, budget, first, next)
}
