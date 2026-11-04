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
//     for every new or content-changed finding. The transaction holds the rows
//     it appended and counts them when it commits.
//   - fact_retract: the retract statement's own INSERT (writer_retract.go). The
//     transaction holds that statement's affected rows and counts them when it
//     commits.
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
// appended and holds them to count under source fact_upsert once the
// transaction commits.
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

// RecordWinnersCapture holds the rows a statement of this transaction appended
// to the winners dirty log under source, and counts them when the transaction
// commits. A rollback, or a commit that fails, discards them: the log rows
// vanish with the transaction, and the counter must match the rows that exist
// (#9957). A zero count holds nothing. Without instruments it holds nothing.
// The transaction is used by one writer goroutine, so the hold needs no lock.
func (t *supplyChainImpactTx) RecordWinnersCapture(ctx context.Context, source string, rows int64) {
	if t.instruments == nil || rows <= 0 {
		return
	}
	if t.pendingCapture == nil {
		t.pendingCapture = make(map[string]int64, 2)
	}
	t.pendingCapture[source] += rows
	t.captureCtx = context.WithoutCancel(ctx)
}

// Commit commits the transaction and, only if the commit succeeds, counts the
// capture rows it held.
func (t *supplyChainImpactTx) Commit() error {
	err := t.tx.Commit()
	if err == nil {
		t.flushCapture()
	} else {
		t.pendingCapture = nil
	}
	return err
}

// Rollback rolls the transaction back and discards the capture rows it held.
func (t *supplyChainImpactTx) Rollback() error {
	t.pendingCapture = nil
	return t.tx.Rollback()
}

// flushCapture counts the held capture rows and clears them.
func (t *supplyChainImpactTx) flushCapture() {
	for source, rows := range t.pendingCapture {
		t.instruments.RecordSupplyChainImpactWinnersCapture(t.captureCtx, source, rows)
	}
	t.pendingCapture = nil
}
