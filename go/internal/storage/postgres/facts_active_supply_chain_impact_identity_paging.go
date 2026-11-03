// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/reducer/factload"
	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/filters"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const supplyChainImpactContainerImageIdentityFactKind = "reducer_container_image_identity"

// supplyChainImpactPayloadBudgetSpent reports whether the raw payload bytes the
// call has kept so far exceed limit (zero is unlimited). It is checked before
// each payload page and identity page, so the page that crosses the limit is
// kept and no later page is fetched (#9781).
func supplyChainImpactPayloadBudgetSpent(stats *supplychaincore.SupplyChainImpactActiveEvidenceLoadStats, limit int64) bool {
	return limit > 0 && stats.RetainedPayloadBytes > limit
}

// loadSupplyChainImpactPayloadPages fetches resolved facts by primary key in
// supplyChainImpactPayloadPageSize pages and returns them in the given order.
// Every requested ID must come back exactly once: the snapshot that resolved
// the IDs is the one that reads them. When retainedLimit is positive it stops
// before the first page after the call has kept more than that many raw payload
// bytes, and returns the facts it has.
func (s FactStore) loadSupplyChainImpactPayloadPages(
	ctx context.Context,
	queryer db.Queryer,
	ids []string,
	stats *supplychaincore.SupplyChainImpactActiveEvidenceLoadStats,
	retainedLimit int64,
) ([]facts.Envelope, error) {
	out := make([]facts.Envelope, 0, len(ids))
	for start := 0; start < len(ids); start += supplyChainImpactPayloadPageSize {
		if supplyChainImpactPayloadBudgetSpent(stats, retainedLimit) {
			// The previous page crossed the byte budget: keep it, read no more.
			return out, nil
		}
		page := ids[start:min(start+supplyChainImpactPayloadPageSize, len(ids))]
		started := time.Now()
		byID, pageBytes, err := supplyChainImpactPayloadPage(ctx, queryer, page)
		if err != nil {
			return nil, err
		}
		stats.RetainedPayloadBytes += pageBytes
		stats.PayloadPages++
		stats.Branches = append(stats.Branches, supplychaincore.SupplyChainImpactActiveEvidenceBranchStat{
			Branch: supplychaincore.SupplyChainImpactActiveEvidencePayloadPage, Rows: len(byID), Duration: time.Since(started),
			PayloadBytes: pageBytes,
		})
		for _, id := range page {
			envelope, ok := byID[id]
			if !ok {
				return nil, fmt.Errorf("list active supply chain impact facts: payload page is missing resolved fact %q", id)
			}
			out = append(out, envelope)
		}
	}
	return out, nil
}

// supplyChainImpactPayloadPage returns the page's envelopes by fact ID and the
// raw payload bytes they were decoded from.
func supplyChainImpactPayloadPage(ctx context.Context, queryer db.Queryer, ids []string) (map[string]facts.Envelope, int64, error) {
	rows, err := queryer.QueryContext(ctx, factimpact.PayloadPageSQL, ids)
	if err != nil {
		return nil, 0, fmt.Errorf("list active supply chain impact facts: payload page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[string]facts.Envelope, len(ids))
	var pageBytes int64
	for rows.Next() {
		envelope, size, err := scanSizedSupplyChainImpactFact(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("list active supply chain impact facts: payload page: %w", err)
		}
		if _, duplicate := byID[envelope.FactID]; duplicate {
			return nil, 0, fmt.Errorf("list active supply chain impact facts: payload page returned %q twice", envelope.FactID)
		}
		byID[envelope.FactID] = envelope
		pageBytes += int64(size)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list active supply chain impact facts: payload page: %w", err)
	}
	if len(byID) != len(ids) {
		return nil, 0, fmt.Errorf("list active supply chain impact facts: payload page returned %d of %d facts", len(byID), len(ids))
	}
	return byID, pageBytes, nil
}

// foldSupplyChainImpactFilePages fetches resolved file facts in byte-ordered
// primary-key pages and folds each row into fold as it is scanned, so no page
// of decoded file payloads is ever held (#9781). The ids must already be in
// byte order: the fold's tie-breaks depend on file order, and the ordered
// statement returns rows in the order the loader sorted them. A page that
// comes back out of order, short, or with a different fact fails the call, so
// a snapshot or collation drift cannot silently change reachability evidence.
func (s FactStore) foldSupplyChainImpactFilePages(
	ctx context.Context,
	queryer db.Queryer,
	ids []string,
	fold *supplychaincore.FileReachabilityFold,
	stats *supplychaincore.SupplyChainImpactActiveEvidenceLoadStats,
) error {
	for start := 0; start < len(ids); start += supplyChainImpactPayloadPageSize {
		page := ids[start:min(start+supplyChainImpactPayloadPageSize, len(ids))]
		started := time.Now()
		pageBytes, err := foldSupplyChainImpactFilePage(ctx, queryer, page, fold, stats)
		if err != nil {
			return err
		}
		stats.PayloadPages++
		stats.Branches = append(stats.Branches, supplychaincore.SupplyChainImpactActiveEvidenceBranchStat{
			Branch: supplychaincore.SupplyChainImpactActiveEvidenceFileFold, Rows: len(page), Duration: time.Since(started),
			PayloadBytes: pageBytes,
		})
	}
	return nil
}

func foldSupplyChainImpactFilePage(
	ctx context.Context,
	queryer db.Queryer,
	page []string,
	fold *supplychaincore.FileReachabilityFold,
	stats *supplychaincore.SupplyChainImpactActiveEvidenceLoadStats,
) (int64, error) {
	rows, err := queryer.QueryContext(ctx, factimpact.PayloadPageOrderedSQL, page)
	if err != nil {
		return 0, fmt.Errorf("list active supply chain impact facts: file fold page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	scanned := 0
	var pageBytes int64
	for rows.Next() {
		envelope, size, err := scanTypedSizedSupplyChainImpactFact(rows)
		if err != nil {
			return 0, fmt.Errorf("list active supply chain impact facts: file fold page: %w", err)
		}
		if scanned >= len(page) || envelope.FactID != page[scanned] {
			return 0, fmt.Errorf("list active supply chain impact facts: file fold page returned %q out of order at row %d", envelope.FactID, scanned)
		}
		scanned++
		pageBytes += int64(size)
		stats.FoldedPayloadBytes += int64(size)
		if fold.Add(envelope) {
			stats.FoldedFiles++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, fmt.Errorf("list active supply chain impact facts: file fold page: %w", err)
	}
	if scanned != len(page) {
		return 0, fmt.Errorf("list active supply chain impact facts: file fold page returned %d of %d facts", scanned, len(page))
	}
	return pageBytes, nil
}

// loadSupplyChainImpactIdentityStream pages the canonical container-image
// identity support stream with its own keyset cursor, in the same snapshot.
// It is unchanged by #8880 apart from no longer sharing a statement with the
// legacy rows.
func (s FactStore) loadSupplyChainImpactIdentityStream(
	ctx context.Context,
	queryer db.Queryer,
	filter supplychaincore.SupplyChainImpactFactFilter,
	stats *supplychaincore.SupplyChainImpactActiveEvidenceLoadStats,
) ([]facts.Envelope, error) {
	var out []facts.Envelope
	var cursor *containerImageIdentitySupportCursor
	cursorFactID := ""
	for {
		if supplyChainImpactPayloadBudgetSpent(stats, filter.PayloadBytesLimit) {
			return out, nil
		}
		started := time.Now()
		rows, err := queryer.QueryContext(ctx, listCurrentContainerImageIdentitySupportFactsQuery,
			filter.SubjectDigests, filter.ImageRefs, filter.RepositoryIDs, filter.RepositoryIDs, filter.RepositoryIDs,
			cursorFactID, listFactsByKindPageSize,
		)
		if err != nil {
			return nil, fmt.Errorf("list active supply chain impact facts: identity page: %w", err)
		}
		count := 0
		var pageBytes int64
		for rows.Next() {
			envelope, size, scanErr := scanSizedSupplyChainImpactFact(rows)
			if scanErr != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("list active supply chain impact facts: identity page: %w", scanErr)
			}
			pageBytes += int64(size)
			next, acceptErr := acceptSupplyChainImpactIdentityFact(envelope, cursor, cursorFactID)
			if acceptErr != nil {
				_ = rows.Close()
				return nil, acceptErr
			}
			cursor, cursorFactID = &next, envelope.FactID
			out = append(out, envelope)
			count++
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("list active supply chain impact facts: identity page: %w", err)
		}
		_ = rows.Close()
		stats.Branches = append(stats.Branches, supplychaincore.SupplyChainImpactActiveEvidenceBranchStat{
			Branch: supplychaincore.SupplyChainImpactActiveEvidenceIdentity, Rows: count, Duration: time.Since(started),
			PayloadBytes: pageBytes,
		})
		stats.RetainedPayloadBytes += pageBytes
		if count > listFactsByKindPageSize {
			return nil, fmt.Errorf("list active supply chain impact facts: identity page returned %d rows, limit %d", count, listFactsByKindPageSize)
		}
		if count < listFactsByKindPageSize {
			return out, nil
		}
	}
}

func acceptSupplyChainImpactIdentityFact(
	envelope facts.Envelope,
	previous *containerImageIdentitySupportCursor,
	previousFactID string,
) (containerImageIdentitySupportCursor, error) {
	if envelope.FactKind != supplyChainImpactContainerImageIdentityFactKind {
		return containerImageIdentitySupportCursor{}, fmt.Errorf(
			"list active supply chain impact facts: identity stream fact kind = %q", envelope.FactKind)
	}
	cursor, err := parseContainerImageIdentitySupportCursor(envelope.FactID)
	if err != nil {
		return containerImageIdentitySupportCursor{}, fmt.Errorf(
			"list active supply chain impact facts: invalid identity fact ID %q: %w", envelope.FactID, err)
	}
	if previous != nil && compareContainerImageIdentitySupportCursors(*previous, cursor) >= 0 {
		return containerImageIdentitySupportCursor{}, fmt.Errorf(
			"list active supply chain impact facts: identity cursor did not advance after %q", previousFactID)
	}
	return cursor, nil
}

// scanSupplyChainImpactFact decodes the sixteen columns both the payload page
// and the identity function return.
func scanSupplyChainImpactFact(rows db.Rows) (facts.Envelope, error) {
	envelope, _, err := scanSizedSupplyChainImpactFact(rows)
	return envelope, err
}

// scanSizedSupplyChainImpactFact is scanSupplyChainImpactFact plus the raw
// payload size in bytes, which the evidence budget charges (#9781).
func scanSizedSupplyChainImpactFact(rows db.Rows) (facts.Envelope, int, error) {
	var envelope facts.Envelope
	var sourceSystem, sourceFactKey, sourceURI, sourceRecordID string
	var rawPayload []byte
	if err := rows.Scan(
		&envelope.FactID, &envelope.ScopeID, &envelope.GenerationID,
		&envelope.FactKind, &envelope.StableFactKey, &envelope.SchemaVersion,
		&envelope.CollectorKind, &envelope.FencingToken, &envelope.SourceConfidence,
		&sourceSystem, &sourceFactKey, &sourceURI, &sourceRecordID,
		&envelope.ObservedAt, &envelope.IsTombstone, &rawPayload,
	); err != nil {
		return facts.Envelope{}, 0, err
	}
	decoded, err := payloadstore.UnmarshalPayload(rawPayload)
	if err != nil {
		return facts.Envelope{}, 0, err
	}
	envelope.Payload = decoded
	envelope.SourceRef = facts.Ref{
		SourceSystem: sourceSystem,
		ScopeID:      envelope.ScopeID, GenerationID: envelope.GenerationID,
		FactKey: sourceFactKey, SourceURI: sourceURI, SourceRecordID: sourceRecordID,
	}
	return envelope, len(rawPayload), nil
}

// recordSupplyChainImpactLoad emits the per-statement instruments and, when
// the foreign-key guard returned rows, a WARN line naming the drifted kinds.
func (s FactStore) recordSupplyChainImpactLoad(
	ctx context.Context,
	filter supplychaincore.SupplyChainImpactFactFilter,
	stats supplychaincore.SupplyChainImpactActiveEvidenceLoadStats,
) {
	if s.instruments != nil {
		for _, branch := range stats.Branches {
			attrs := metric.WithAttributes(telemetry.AttrBranch(branch.Branch))
			if s.instruments.SupplyChainImpactActiveEvidenceBranchDuration != nil {
				s.instruments.SupplyChainImpactActiveEvidenceBranchDuration.Record(ctx, branch.Duration.Seconds(), attrs)
			}
			if s.instruments.SupplyChainImpactActiveEvidenceBranchRows != nil {
				s.instruments.SupplyChainImpactActiveEvidenceBranchRows.Add(ctx, int64(branch.Rows), attrs)
			}
			if branch.PayloadBytes > 0 && s.instruments.SupplyChainImpactActiveEvidencePayloadBytes != nil {
				s.instruments.SupplyChainImpactActiveEvidencePayloadBytes.Record(ctx, branch.PayloadBytes, attrs)
			}
		}
		if stats.GuardRows > 0 && s.instruments.SupplyChainImpactActiveEvidenceGuardRows != nil {
			s.instruments.SupplyChainImpactActiveEvidenceGuardRows.Add(ctx, int64(stats.GuardRows))
		}
	}
	if stats.GuardRows == 0 {
		return
	}
	logger := s.logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.WarnContext(ctx, "supply chain impact foreign-key guard returned rows; a payload contract carries an identity key its branch does not probe",
		slog.Int("guard_rows", stats.GuardRows),
		slog.Any("guard_kinds", stats.GuardKinds),
		slog.Int("resolved_fact_ids", stats.ResolvedFactIDs),
		slog.Int("file_repository_ids", len(filter.FileRepositoryIDs)),
		slog.String("guard_index", factimpact.IndexSupplyChainImpactKeyGuard),
	)
}

// FoldPythonReachabilityFiles pages one scope generation's file facts filtered
// by a top-level payload key/value allowlist and folds each row into fold,
// dropping the decoded envelope. It returns the folded file count and the raw
// payload bytes read. Live memory stays bounded by the index plus one page
// instead of every decoded payload (#9860). It mirrors the page loop of
// ListFactsByKindAndPayloadValue, including the snapshot variants, so the
// engaged and disengaged loops read the same rows.
func (s FactStore) FoldPythonReachabilityFiles(
	ctx context.Context,
	scopeID string,
	generationID string,
	payloadKey string,
	payloadValues []string,
	fold *supplychaincore.FileReachabilityFold,
) (int, int64, error) {
	if s.database == nil {
		return 0, 0, fmt.Errorf("fact store database is required")
	}
	payloadKey = strings.TrimSpace(payloadKey)
	payloadValues = filters.CleanFactKinds(payloadValues)
	if payloadKey == "" || len(payloadValues) == 0 || fold == nil {
		return 0, 0, nil
	}

	var files int
	var payloadBytes int64
	var cursorObservedAt *time.Time
	var cursorFactID string
	// One read-only repeatable-read snapshot covers every page, so the pages
	// see one state even as concurrent writers advance the generation.
	err := withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		for {
			page, err := s.foldPythonReachabilityFilePage(
				ctx,
				queryer,
				scopeID,
				generationID,
				payloadKey,
				payloadValues,
				cursorObservedAt,
				cursorFactID,
				fold,
			)
			if err != nil {
				return err
			}
			files += page.files
			payloadBytes += page.payloadBytes
			if page.rows < listFactsByKindPageSize {
				return nil
			}
			cursorObservedAt = page.lastObservedAt
			cursorFactID = page.lastFactID
		}
	})
	if err != nil {
		return 0, 0, err
	}
	return files, payloadBytes, nil
}

type pythonReachabilityFilePage struct {
	rows           int
	files          int
	payloadBytes   int64
	lastObservedAt *time.Time
	lastFactID     string
}

// foldPythonReachabilityFilePage reads one stable-order page of file facts
// and folds each row, so no page of decoded file payloads is ever held.
func (s FactStore) foldPythonReachabilityFilePage(
	ctx context.Context,
	queryer db.Queryer,
	scopeID string,
	generationID string,
	payloadKey string,
	payloadValues []string,
	cursorObservedAt *time.Time,
	cursorFactID string,
	fold *supplychaincore.FileReachabilityFold,
) (pythonReachabilityFilePage, error) {
	query := listFactsByKindAndPayloadValueQuery
	if s.impactStaticSnapshot {
		query = protectedStaticFactsByPayloadQuery
	}
	args := []any{
		scopeID,
		generationID,
		factload.FactKindFile,
		payloadKey,
		payloadValues,
		listFactsByKindPageSize,
	}
	if cursorObservedAt != nil {
		query = listFactsByKindAndPayloadValueCursorQuery
		if s.impactStaticSnapshot {
			query = protectedStaticFactsByPayloadCursorQuery
		}
		args = []any{
			scopeID,
			generationID,
			factload.FactKindFile,
			payloadKey,
			payloadValues,
			cursorObservedAt.UTC(),
			cursorFactID,
			listFactsByKindPageSize,
		}
	}

	rows, err := queryer.QueryContext(ctx, query, args...)
	if err != nil {
		return pythonReachabilityFilePage{}, fmt.Errorf("fold python reachability files: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var page pythonReachabilityFilePage
	for rows.Next() {
		// The row shape has one home in backfill: same SELECT list, plus the
		// raw payload byte size for the operator signals. The payload decodes
		// through the typed file-payload decode (#9861), carrying only the
		// keys the reachability readers consume; the size stays raw bytes.
		envelope, size, scanErr := backfill.ScanTypedFileEnvelopeWithPayloadSize(rows)
		if scanErr != nil {
			return pythonReachabilityFilePage{}, fmt.Errorf("fold python reachability files: %w", scanErr)
		}
		observedAt := envelope.ObservedAt.UTC()
		page.lastObservedAt = &observedAt
		page.lastFactID = envelope.FactID
		page.rows++
		page.payloadBytes += int64(size)
		if fold.Add(envelope) {
			page.files++
		}
	}
	if err := rows.Err(); err != nil {
		return pythonReachabilityFilePage{}, fmt.Errorf("fold python reachability files: %w", err)
	}
	return page, nil
}
