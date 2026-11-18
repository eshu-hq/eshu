// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgconn"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// winnersWriteConflictClass returns the failure class for a retryable write
// conflict, or "" when err is nil or not a deadlock/serialization failure.
// It follows the content_files and projector-claim classifiers
// (contentFileWriteConflictClass, claimConflictClass): 40P01 counts as
// deadlock, 40001 as serialization_failure. The counter below classifies
// the victim only and never retries.
func winnersWriteConflictClass(err error) string {
	var pgErr *pgconn.PgError
	if err == nil || !errors.As(err, &pgErr) {
		return ""
	}
	switch pgErr.Code {
	case "40P01":
		return "deadlock"
	case "40001":
		return "serialization_failure"
	default:
		return ""
	}
}

// Capture of the supply-chain impact winners dirty log (#9805, ruling B).
//
// Three capture owners mark keys in the transaction that makes the change,
// each as plain staging INSERTs into the shared queue (#10837 direction C):
// no owner touches the versioned dirty log, so no capture statement takes a
// dirty-log row lock and concurrent writers over the same hot keys never
// wait on a conflict arbiter.
//
//   - fact_upsert: CaptureWinnersDirtyFacts, called by the impact finding writer
//     for every new or content-changed finding. The transaction holds the rows
//     the staging insert touched and counts them when it commits.
//   - fact_retract: the retract statement's own staging insert
//     (writer_retract.go). The transaction holds that statement's staged rows
//     and counts them when it commits.
//   - activation, operator and scanner: the staging triggers of migrations
//     310 and 311, on ingestion_scopes.active_generation_id and the two
//     scanner tables. A trigger cannot emit a Go metric, so the maintainer
//     counts these sources when it consumes their rows, by the row's source
//     column.
//
// Source attribution is first-marker-wins (#10827): one row per recompute
// unit, and a re-mark by a later owner bumps only dirty_version, leaving the
// first marker's source on the row. The maintainer's per-source counts
// therefore credit the owner that queued the unit, not every owner that
// re-marked it; the version-sum still counts every mark.
//
// A change class no owner names is repaired by the audit and counted by
// ..._backstop_repairs_total.

// CaptureWinnersDirtyFacts stages the identifiers of the findings this
// transaction wrote in the winners staging queue, in the same transaction, so
// the staged rows commit or roll back with the finding they name. The
// maintainer tick collapses staged rows into the versioned dirty log (#10837:
// one GROUP BY merge-upsert per key, version bumps by mark count), so the
// hot path takes no dirty-log row locks. Identifiers are sorted and
// deduplicated by the conflict key before the statement runs. It returns the
// rows the staging insert touched and holds them to count under source
// fact_upsert once the transaction commits.
func (t *supplyChainImpactTx) CaptureWinnersDirtyFacts(
	ctx context.Context,
	identifiers []supplychaincore.WinnersDirtyIdentifier,
) (int64, error) {
	identifiers = sortDedupWinnersDirtyIdentifiers(identifiers)
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
		t.recordWinnersWriteConflict(telemetry.SupplyChainImpactWinnersConflictSiteWriterCapture, err)
		return 0, fmt.Errorf("stage supply chain impact winners dirty keys: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count supply chain impact winners staged rows: %w", err)
	}
	t.RecordWinnersCapture(ctx, telemetry.SupplyChainImpactWinnersSourceFactUpsert, rows)
	return rows, nil
}

// sortDedupWinnersDirtyIdentifiers returns a copy of ids sorted by the dirty
// log's fact conflict key (package_id, repository_id, subject_digest, c0,
// advisory_id) with exact duplicates removed. The staging INSERT takes no
// row locks (#10837 direction C: no unique index, no arbiter), so the sort
// buys no lock order — it keeps the statement's row batch deterministic and
// the dedup keeps one statement from staging one unit twice. The order binds
// one statement, not the whole writer transaction (#10851).
func sortDedupWinnersDirtyIdentifiers(ids []supplychaincore.WinnersDirtyIdentifier) []supplychaincore.WinnersDirtyIdentifier {
	if len(ids) == 0 {
		return nil
	}
	out := make([]supplychaincore.WinnersDirtyIdentifier, len(ids))
	copy(out, ids)
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.PackageID != b.PackageID {
			return a.PackageID < b.PackageID
		}
		if a.RepositoryID != b.RepositoryID {
			return a.RepositoryID < b.RepositoryID
		}
		if a.SubjectDigest != b.SubjectDigest {
			return a.SubjectDigest < b.SubjectDigest
		}
		if a.C0 != b.C0 {
			return a.C0 < b.C0
		}
		return a.AdvisoryID < b.AdvisoryID
	})
	kept := out[:1]
	for _, id := range out[1:] {
		if id != kept[len(kept)-1] {
			kept = append(kept, id)
		}
	}
	return kept
}

// RecordWinnersCapture holds the rows a statement of this transaction staged
// in the winners staging queue under source, and counts them when the
// transaction commits. A rollback, or a commit that fails, discards them: the
// staged rows vanish with the transaction, and the counter must match the
// rows that exist (#9957). A zero count holds nothing. Without instruments it
// holds nothing.
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
		t.recordWinnersWriteConflict(telemetry.SupplyChainImpactWinnersConflictSiteWriterCommit, err)
	}
	return err
}

// recordWinnersWriteConflict counts one aborted writer statement or commit
// under its failure class (#10851). It classifies the victim only: the error
// still returns to the caller unchanged and nothing retries. A nil error or
// a non-conflict error records nothing.
func (t *supplyChainImpactTx) recordWinnersWriteConflict(site string, err error) {
	if t.instruments == nil {
		return
	}
	ctx := t.captureCtx
	if ctx == nil {
		ctx = context.Background()
	}
	t.instruments.RecordSupplyChainImpactWinnersWriteConflict(ctx, site, winnersWriteConflictClass(err))
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
