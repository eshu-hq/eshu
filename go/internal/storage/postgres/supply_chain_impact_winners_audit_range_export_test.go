// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
)

// WinnersAuditFirstRangeSQLForTest and WinnersAuditNextRangeSQLForTest expose
// the shipped range statements (#11131), so the differential can derive its
// seeded mutation from them instead of hand-copying. They are the statements the
// audit runs: over the supports table since #10880 PR-C2 l.
var (
	WinnersAuditFirstRangeSQLForTest = selectWinnersAuditFirstRangeFromSupportsSQL
	WinnersAuditNextRangeSQLForTest  = selectWinnersAuditNextRangeFromSupportsSQL
	WinnersAuditNullProbeSQLForTest  = probeWinnersAuditNullKeysFromSupportsSQL
)

// WinnersAuditSingleScanFirstRangeSQLForTest and
// WinnersAuditSingleScanNextRangeSQLForTest expose the single-scan range
// statements (#10972), so the differential derives its seeded mutation from
// them instead of hand-copying.
var (
	WinnersAuditSingleScanFirstRangeSQLForTest = selectWinnersAuditSingleScanFirstRangeSQL
	WinnersAuditSingleScanNextRangeSQLForTest  = selectWinnersAuditSingleScanNextRangeSQL
)

// WinnersAuditRangeStoredIdentExprsForTest exposes the stored-winner
// identifier expressions (#11131) so the canonical-ident index pin can
// compare the migration text against the code without a hand copy.
var WinnersAuditRangeStoredIdentExprsForTest = winnersAuditRangeStoredIdentExprs

// ReadWinnersAuditRangeWithSQLForTest runs one range with the given statement
// pair, for the seeded-RED differential.
func ReadWinnersAuditRangeWithSQLForTest(ctx context.Context, reader db.Queryer, lo *WinnersAuditRangeIdent, budget int, first, next string) (WinnersAuditRange, error) {
	return readWinnersAuditRange(ctx, reader, lo, budget, first, next)
}
