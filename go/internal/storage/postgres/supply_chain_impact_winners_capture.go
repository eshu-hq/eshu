// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Capture of the supply-chain impact winners dirty log (#9805, ruling B).
//
// Three capture owners append rows in the transaction that makes the change:
//
//   - fact_upsert: CaptureWinnersDirtyFacts, called by the impact finding writer
//     for every new or content-changed finding. The writer counts the rows it
//     appended at capture time.
//   - fact_retract: the retract statement's own INSERT (writer_retract.go). The
//     writer counts that statement's affected rows at capture time.
//   - activation, operator and scanner: the triggers of migrations 272 and 272a, on
//     ingestion_scopes.active_generation_id and the two scanner tables. A
//     trigger cannot emit a Go metric, so the maintainer counts these sources
//     when it consumes their rows, by the row's source column.
//
// A change class no owner names is repaired by the audit and counted by
// ..._backstop_repairs_total.

// CaptureWinnersDirtyFacts appends the identifiers of the findings this
// transaction wrote to the winners dirty log, in the same transaction, so the
// log row commits or rolls back with the finding it names. It returns the rows
// appended and counts them under source fact_upsert.
func (t *supplyChainImpactTx) CaptureWinnersDirtyFacts(
	ctx context.Context,
	identifiers []supplychaincore.WinnersDirtyIdentifier,
) (int64, error) {
	if len(identifiers) == 0 {
		return 0, nil
	}
	n := len(identifiers)
	pkg, repo, digest, c0, advisory := make([]string, n), make([]string, n), make([]string, n), make([]string, n), make([]string, n)
	for i, id := range identifiers {
		pkg[i], repo[i], digest[i], c0[i], advisory[i] = id.PackageID, id.RepositoryID, id.SubjectDigest, id.C0, id.AdvisoryID
	}
	result, err := t.tx.ExecContext(ctx, captureWinnersDirtyFactsSQL, pkg, repo, digest, c0, advisory)
	if err != nil {
		return 0, fmt.Errorf("append supply chain impact winners dirty log: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count supply chain impact winners dirty log rows: %w", err)
	}
	t.RecordWinnersCapture(ctx, telemetry.SupplyChainImpactWinnersSourceFactUpsert, rows)
	return rows, nil
}

// RecordWinnersCapture counts rows a statement of this transaction appended to
// the winners dirty log under source. It is a no-op without instruments. The
// count is recorded when the statement returns, before the transaction ends: a
// rollback after it (a failed publish or final write) leaves the counter ahead
// of the log by that attempt's rows. The public telemetry page says so; the
// writer retries the whole pass, so the skew is bounded by the retries.
func (t *supplyChainImpactTx) RecordWinnersCapture(ctx context.Context, source string, rows int64) {
	t.instruments.RecordSupplyChainImpactWinnersCapture(ctx, source, rows)
}
