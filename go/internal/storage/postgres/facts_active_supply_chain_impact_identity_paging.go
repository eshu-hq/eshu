// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

const supplyChainImpactContainerImageIdentityFactKind = "reducer_container_image_identity"

// listActiveSupplyChainImpactFactPagesQuery gives the legacy and canonical
// identity streams independent cursors and limits while retaining one
// statement snapshot and one database round trip per page pair. The private
// rank and ordinal columns make the stream contract explicit to the Go loader;
// they never escape the storage boundary.
const listActiveSupplyChainImpactFactPagesQuery = `
WITH legacy_page AS MATERIALIZED (
` + listActiveSupplyChainImpactFactsQuery + `
),
legacy_numbered AS MATERIALIZED (
    SELECT
        CASE WHEN fact.fact_kind = 'vulnerability.suppression' THEN 2 ELSE 0 END AS stream_rank,
        row_number() OVER (
            PARTITION BY (fact.fact_kind = 'vulnerability.suppression')
            ORDER BY convert_to(fact.fact_id, 'UTF8')
        ) AS stream_ordinal,
        fact.*
    FROM legacy_page AS fact
),
tagged AS (
    SELECT
        legacy.stream_rank,
        legacy.stream_ordinal,
        legacy.fact_id, legacy.scope_id, legacy.generation_id, legacy.fact_kind,
        legacy.stable_fact_key, legacy.schema_version, legacy.collector_kind,
        legacy.fencing_token, legacy.source_confidence, legacy.source_system,
        legacy.source_fact_key, legacy.source_uri, legacy.source_record_id,
        legacy.observed_at, legacy.is_tombstone, legacy.payload
    FROM legacy_numbered AS legacy
    UNION ALL
    SELECT
        1 AS stream_rank,
        fact.ordinality AS stream_ordinal,
        fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind,
        fact.stable_fact_key, fact.schema_version, fact.collector_kind,
        fact.fencing_token, fact.source_confidence, fact.source_system,
        fact.source_fact_key, fact.source_uri, fact.source_record_id,
        fact.observed_at, fact.is_tombstone, fact.payload
    FROM container_image_identity_current_support_facts_for(
        $5::text[], $9::text[], $8::text[], $8::text[], $8::text[],
        $20::text, $21::integer
    ) WITH ORDINALITY AS fact
)
SELECT
    stream_rank, stream_ordinal,
    fact_id, scope_id, generation_id, fact_kind, stable_fact_key,
    schema_version, collector_kind, fencing_token, source_confidence,
    source_system, source_fact_key, source_uri, source_record_id,
    observed_at, is_tombstone, payload
FROM tagged
ORDER BY stream_rank, stream_ordinal
`

type supplyChainImpactPagingState struct {
	coreFacts        []facts.Envelope
	identityFacts    []facts.Envelope
	suppressionFacts []facts.Envelope
	seenFactIDs      map[string]struct{}

	legacyCursorFactID        string
	legacyCursorIsSuppression bool
	identityCursorFactID      string
	identityCursor            *containerImageIdentitySupportCursor
	legacyDone                bool
	identityDone              bool
	suppressionLoaded         int
	suppressionTruncated      bool
}

// ListActiveSupplyChainImpactFacts loads active package, SBOM, image, and
// risk evidence for one bounded supply-chain impact reducer intent. The bool
// reports whether the suppression tail was truncated. Core and canonical
// identity evidence use independent keyset cursors, so neither stream can
// skip prefix-related FactIDs encoded in the other stream's ordering domain.
func (s FactStore) ListActiveSupplyChainImpactFacts(
	ctx context.Context,
	filter reducer.SupplyChainImpactFactFilter,
) ([]facts.Envelope, bool, error) {
	if s.db == nil {
		return nil, false, fmt.Errorf("fact store database is required")
	}
	normalizeSupplyChainImpactFactFilter(&filter)
	if supplyChainImpactFactFilterEmpty(filter) {
		return nil, false, nil
	}

	state := supplyChainImpactPagingState{seenFactIDs: make(map[string]struct{})}
	for !state.legacyDone || !state.identityDone {
		if err := s.loadActiveSupplyChainImpactFactPagePair(ctx, filter, &state); err != nil {
			return nil, false, err
		}
	}
	loaded := make([]facts.Envelope, 0,
		len(state.coreFacts)+len(state.identityFacts)+len(state.suppressionFacts))
	loaded = append(loaded, state.coreFacts...)
	loaded = append(loaded, state.identityFacts...)
	loaded = append(loaded, state.suppressionFacts...)
	return loaded, state.suppressionTruncated, nil
}

func (s FactStore) loadActiveSupplyChainImpactFactPagePair(
	ctx context.Context,
	filter reducer.SupplyChainImpactFactFilter,
	state *supplyChainImpactPagingState,
) error {
	legacyLimit := listFactsByKindPageSize
	if state.legacyDone {
		legacyLimit = 0
	}
	identityLimit := listFactsByKindPageSize
	if state.identityDone {
		identityLimit = 0
	}
	rows, err := s.db.QueryContext(
		ctx,
		listActiveSupplyChainImpactFactPagesQuery,
		filter.PackageIDs,
		filter.PURLs,
		filter.CVEIDs,
		filter.AdvisoryIDs,
		filter.SubjectDigests,
		filter.ProductCriteria,
		filter.DocumentIDs,
		filter.RepositoryIDs,
		filter.ImageRefs,
		filter.FileRepositoryIDs,
		state.legacyCursorFactID,
		legacyLimit,
		lowerCleanedStringFilterValues(filter.PackageIDs),
		lowerCleanedStringFilterValues(filter.PURLs),
		lowerCleanedStringFilterValues(filter.CVEIDs),
		lowerCleanedStringFilterValues(filter.SubjectDigests),
		lowerCleanedStringFilterValues(filter.RepositoryIDs),
		lowerCleanedStringFilterValues(filter.AdvisoryIDs),
		state.legacyCursorIsSuppression,
		state.identityCursorFactID,
		identityLimit,
	)
	if err != nil {
		return fmt.Errorf("list active supply chain impact facts: %w", err)
	}
	defer func() { _ = rows.Close() }()

	legacyCount, identityCount := 0, 0
	legacyStoppedBySentinel := false
	expectedOrdinal := [3]int64{1, 1, 1}
	previousRank := -1
	for rows.Next() {
		rank, ordinal, envelope, scanErr := scanTaggedSupplyChainImpactFact(rows)
		if scanErr != nil {
			return fmt.Errorf("list active supply chain impact facts: %w", scanErr)
		}
		if rank < 0 || rank >= len(expectedOrdinal) {
			return fmt.Errorf("list active supply chain impact facts: invalid stream rank %d", rank)
		}
		if rank < previousRank {
			return fmt.Errorf("list active supply chain impact facts: stream rank regressed from %d to %d", previousRank, rank)
		}
		previousRank = rank
		if ordinal != expectedOrdinal[rank] {
			return fmt.Errorf(
				"list active supply chain impact facts: stream %d ordinal = %d, want %d",
				rank, ordinal, expectedOrdinal[rank],
			)
		}
		expectedOrdinal[rank]++
		if envelope.FactID == "" {
			return fmt.Errorf("list active supply chain impact facts: empty fact ID in stream %d", rank)
		}
		if _, duplicate := state.seenFactIDs[envelope.FactID]; duplicate {
			return fmt.Errorf("list active supply chain impact facts: duplicate fact ID %q", envelope.FactID)
		}
		state.seenFactIDs[envelope.FactID] = struct{}{}

		switch rank {
		case 0, 2:
			if state.legacyDone {
				return fmt.Errorf("list active supply chain impact facts: legacy stream returned rows after completion")
			}
			if err := state.acceptLegacyFact(rank, envelope, &legacyStoppedBySentinel); err != nil {
				return err
			}
			legacyCount++
		case 1:
			if state.identityDone {
				return fmt.Errorf("list active supply chain impact facts: identity stream returned rows after completion")
			}
			if err := state.acceptIdentityFact(envelope); err != nil {
				return err
			}
			identityCount++
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list active supply chain impact facts: %w", err)
	}
	if legacyCount > legacyLimit || identityCount > identityLimit {
		return fmt.Errorf(
			"list active supply chain impact facts: page counts legacy=%d/%d identity=%d/%d",
			legacyCount, legacyLimit, identityCount, identityLimit,
		)
	}
	if legacyStoppedBySentinel || legacyCount < legacyLimit {
		state.legacyDone = true
	}
	if identityCount < identityLimit {
		state.identityDone = true
	}
	return nil
}

func (state *supplyChainImpactPagingState) acceptLegacyFact(
	rank int,
	envelope facts.Envelope,
	stoppedBySentinel *bool,
) error {
	isSuppression := envelope.FactKind == facts.VulnerabilitySuppressionFactKind
	if rank == 2 && !isSuppression {
		return fmt.Errorf("list active supply chain impact facts: suppression stream fact kind = %q", envelope.FactKind)
	}
	if rank == 0 && isSuppression {
		return fmt.Errorf("list active supply chain impact facts: core stream contains suppression fact %q", envelope.FactID)
	}
	if rank == 0 && envelope.FactKind == supplyChainImpactContainerImageIdentityFactKind {
		return fmt.Errorf("list active supply chain impact facts: core stream contains identity fact %q", envelope.FactID)
	}
	if state.legacyCursorFactID != "" {
		if state.legacyCursorIsSuppression && !isSuppression ||
			state.legacyCursorIsSuppression == isSuppression &&
				bytes.Compare([]byte(state.legacyCursorFactID), []byte(envelope.FactID)) >= 0 {
			return fmt.Errorf(
				"list active supply chain impact facts: legacy cursor did not advance after %q",
				state.legacyCursorFactID,
			)
		}
	}
	state.legacyCursorFactID = envelope.FactID
	state.legacyCursorIsSuppression = isSuppression
	if !isSuppression {
		state.coreFacts = append(state.coreFacts, envelope)
		return nil
	}
	if state.suppressionLoaded == maxSupplyChainImpactActiveEvidenceRowsPerCall {
		state.suppressionTruncated = true
		*stoppedBySentinel = true
		return nil
	}
	state.suppressionLoaded++
	state.suppressionFacts = append(state.suppressionFacts, envelope)
	return nil
}

func (state *supplyChainImpactPagingState) acceptIdentityFact(envelope facts.Envelope) error {
	if envelope.FactKind != supplyChainImpactContainerImageIdentityFactKind {
		return fmt.Errorf("list active supply chain impact facts: identity stream fact kind = %q", envelope.FactKind)
	}
	cursor, err := parseContainerImageIdentitySupportCursor(envelope.FactID)
	if err != nil {
		return fmt.Errorf("list active supply chain impact facts: invalid identity fact ID %q: %w", envelope.FactID, err)
	}
	if state.identityCursor != nil &&
		compareContainerImageIdentitySupportCursors(*state.identityCursor, cursor) >= 0 {
		return fmt.Errorf(
			"list active supply chain impact facts: identity cursor did not advance after %q",
			state.identityCursorFactID,
		)
	}
	state.identityCursor = &cursor
	state.identityCursorFactID = envelope.FactID
	state.identityFacts = append(state.identityFacts, envelope)
	return nil
}

func scanTaggedSupplyChainImpactFact(rows Rows) (int, int64, facts.Envelope, error) {
	var rank int
	var ordinal int64
	var envelope facts.Envelope
	var sourceSystem string
	var sourceFactKey string
	var sourceURI string
	var sourceRecordID string
	var rawPayload []byte
	if err := rows.Scan(
		&rank, &ordinal,
		&envelope.FactID, &envelope.ScopeID, &envelope.GenerationID,
		&envelope.FactKind, &envelope.StableFactKey, &envelope.SchemaVersion,
		&envelope.CollectorKind, &envelope.FencingToken, &envelope.SourceConfidence,
		&sourceSystem, &sourceFactKey, &sourceURI, &sourceRecordID,
		&envelope.ObservedAt, &envelope.IsTombstone, &rawPayload,
	); err != nil {
		return 0, 0, facts.Envelope{}, err
	}
	payload, err := unmarshalPayload(rawPayload)
	if err != nil {
		return 0, 0, facts.Envelope{}, err
	}
	envelope.Payload = payload
	envelope.SourceRef = facts.Ref{
		SourceSystem: sourceSystem,
		ScopeID:      envelope.ScopeID, GenerationID: envelope.GenerationID,
		FactKey: sourceFactKey, SourceURI: sourceURI, SourceRecordID: sourceRecordID,
	}
	return rank, ordinal, envelope, nil
}

func normalizeSupplyChainImpactFactFilter(filter *reducer.SupplyChainImpactFactFilter) {
	filter.PackageIDs = cleanStringFilterValues(filter.PackageIDs)
	filter.PURLs = cleanStringFilterValues(filter.PURLs)
	filter.CVEIDs = cleanStringFilterValues(filter.CVEIDs)
	filter.AdvisoryIDs = cleanStringFilterValues(filter.AdvisoryIDs)
	filter.SubjectDigests = cleanStringFilterValues(filter.SubjectDigests)
	filter.DocumentIDs = cleanStringFilterValues(filter.DocumentIDs)
	filter.ProductCriteria = cleanStringFilterValues(filter.ProductCriteria)
	filter.RepositoryIDs = cleanStringFilterValues(filter.RepositoryIDs)
	filter.FileRepositoryIDs = cleanStringFilterValues(filter.FileRepositoryIDs)
	filter.ImageRefs = cleanStringFilterValues(filter.ImageRefs)
}

func supplyChainImpactFactFilterEmpty(filter reducer.SupplyChainImpactFactFilter) bool {
	return len(filter.PackageIDs) == 0 && len(filter.PURLs) == 0 &&
		len(filter.CVEIDs) == 0 && len(filter.AdvisoryIDs) == 0 && len(filter.SubjectDigests) == 0 &&
		len(filter.DocumentIDs) == 0 && len(filter.ProductCriteria) == 0 &&
		len(filter.RepositoryIDs) == 0 && len(filter.FileRepositoryIDs) == 0 &&
		len(filter.ImageRefs) == 0
}
