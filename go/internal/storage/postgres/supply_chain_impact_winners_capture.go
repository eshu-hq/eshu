// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"sort"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// Capture of the supply-chain impact winners dirty log (#9805, ruling B).
//
// Three capture owners mark keys in the transaction that makes the change,
// each as a key-ordered upsert that bumps only dirty_version (#10827):
//
//   - fact_upsert: CaptureWinnersDirtyFacts, called by the impact finding writer
//     for every new or content-changed finding. The transaction holds the rows
//     the upsert touched and counts them when it commits.
//   - fact_retract: the retract statement's own upsert (writer_retract.go). The
//     transaction holds that statement's affected rows and counts them when it
//     commits.
//   - activation, operator and scanner: the triggers of migrations 272 and 272a, on
//     ingestion_scopes.active_generation_id and the two scanner tables. A
//     trigger cannot emit a Go metric, so the maintainer counts these sources
//     when it consumes their rows, by the row's source column.
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
// advisory_id) with exact duplicates removed. The writer upsert takes row
// locks in this same order (#10827), so sorting here keeps concurrent
// multi-row statements from contending out of order; the dedup keeps one
// statement from marking one unit twice. The order binds one statement,
// not the whole writer transaction (#10851).
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
