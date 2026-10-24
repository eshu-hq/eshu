// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/metric"

	supplychaincore "github.com/eshu-hq/eshu/go/internal/reducer/supplychain/core"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	factimpact "github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/impact"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/facts/payload"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const supplyChainImpactContainerImageIdentityFactKind = "reducer_container_image_identity"

// loadSupplyChainImpactPayloadPages fetches resolved facts by primary key in
// supplyChainImpactPayloadPageSize pages and returns them in the given order.
// Every requested ID must come back exactly once: the snapshot that resolved
// the IDs is the one that reads them.
func (s FactStore) loadSupplyChainImpactPayloadPages(
	ctx context.Context,
	queryer db.Queryer,
	ids []string,
	stats *supplychaincore.SupplyChainImpactActiveEvidenceLoadStats,
) ([]facts.Envelope, error) {
	out := make([]facts.Envelope, 0, len(ids))
	for start := 0; start < len(ids); start += supplyChainImpactPayloadPageSize {
		page := ids[start:min(start+supplyChainImpactPayloadPageSize, len(ids))]
		started := time.Now()
		byID, err := supplyChainImpactPayloadPage(ctx, queryer, page)
		if err != nil {
			return nil, err
		}
		stats.PayloadPages++
		stats.Branches = append(stats.Branches, supplychaincore.SupplyChainImpactActiveEvidenceBranchStat{
			Branch: supplychaincore.SupplyChainImpactActiveEvidencePayloadPage, Rows: len(byID), Duration: time.Since(started),
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

func supplyChainImpactPayloadPage(ctx context.Context, queryer db.Queryer, ids []string) (map[string]facts.Envelope, error) {
	rows, err := queryer.QueryContext(ctx, factimpact.PayloadPageSQL, ids)
	if err != nil {
		return nil, fmt.Errorf("list active supply chain impact facts: payload page: %w", err)
	}
	defer func() { _ = rows.Close() }()
	byID := make(map[string]facts.Envelope, len(ids))
	for rows.Next() {
		envelope, err := scanSupplyChainImpactFact(rows)
		if err != nil {
			return nil, fmt.Errorf("list active supply chain impact facts: payload page: %w", err)
		}
		if _, duplicate := byID[envelope.FactID]; duplicate {
			return nil, fmt.Errorf("list active supply chain impact facts: payload page returned %q twice", envelope.FactID)
		}
		byID[envelope.FactID] = envelope
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list active supply chain impact facts: payload page: %w", err)
	}
	if len(byID) != len(ids) {
		return nil, fmt.Errorf("list active supply chain impact facts: payload page returned %d of %d facts", len(byID), len(ids))
	}
	return byID, nil
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
		started := time.Now()
		rows, err := queryer.QueryContext(ctx, listCurrentContainerImageIdentitySupportFactsQuery,
			filter.SubjectDigests, filter.ImageRefs, filter.RepositoryIDs, filter.RepositoryIDs, filter.RepositoryIDs,
			cursorFactID, listFactsByKindPageSize,
		)
		if err != nil {
			return nil, fmt.Errorf("list active supply chain impact facts: identity page: %w", err)
		}
		count := 0
		for rows.Next() {
			envelope, scanErr := scanSupplyChainImpactFact(rows)
			if scanErr != nil {
				_ = rows.Close()
				return nil, fmt.Errorf("list active supply chain impact facts: identity page: %w", scanErr)
			}
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
		})
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
		return facts.Envelope{}, err
	}
	decoded, err := payloadstore.UnmarshalPayload(rawPayload)
	if err != nil {
		return facts.Envelope{}, err
	}
	envelope.Payload = decoded
	envelope.SourceRef = facts.Ref{
		SourceSystem: sourceSystem,
		ScopeID:      envelope.ScopeID, GenerationID: envelope.GenerationID,
		FactKey: sourceFactKey, SourceURI: sourceURI, SourceRecordID: sourceRecordID,
	}
	return envelope, nil
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
