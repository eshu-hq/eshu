// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const containerImageIdentitySupportFactIDPrefix = "reducer_container_image_identity_support:"

const listCurrentContainerImageIdentitySupportFactsQuery = `
SELECT
    fact.fact_id,
    fact.scope_id,
    fact.generation_id,
    fact.fact_kind,
    fact.stable_fact_key,
    fact.schema_version,
    fact.collector_kind,
    fact.fencing_token,
    fact.source_confidence,
    fact.source_system,
    fact.source_fact_key,
    fact.source_uri,
    fact.source_record_id,
    fact.observed_at,
    fact.is_tombstone,
    fact.payload
FROM container_image_identity_current_support_facts_for(
    $1::text[],
    $2::text[],
    $3::text[],
    $4::text[],
    $5::text[],
    $6::text,
    $7::integer
) AS fact
`

type containerImageIdentitySupportFactFilter struct {
	digests             []string
	imageRefs           []string
	repositoryIDs       []string
	sourceRepositoryIDs []string
	scopeIDs            []string
}

func (filter *containerImageIdentitySupportFactFilter) normalize() {
	filter.digests = cleanStringFilterValues(filter.digests)
	filter.imageRefs = cleanStringFilterValues(filter.imageRefs)
	filter.repositoryIDs = cleanStringFilterValues(filter.repositoryIDs)
	filter.sourceRepositoryIDs = cleanStringFilterValues(filter.sourceRepositoryIDs)
	filter.scopeIDs = cleanStringFilterValues(filter.scopeIDs)
}

func (filter containerImageIdentitySupportFactFilter) empty() bool {
	return len(filter.digests) == 0 && len(filter.imageRefs) == 0 &&
		len(filter.repositoryIDs) == 0 && len(filter.sourceRepositoryIDs) == 0 &&
		len(filter.scopeIDs) == 0
}

type containerImageIdentitySupportCursor struct {
	scopeID   []byte
	digest    []byte
	supportID []byte
}

func parseContainerImageIdentitySupportCursor(factID string) (containerImageIdentitySupportCursor, error) {
	if !strings.HasPrefix(factID, containerImageIdentitySupportFactIDPrefix) {
		return containerImageIdentitySupportCursor{}, fmt.Errorf("missing support namespace")
	}
	parts := strings.Split(factID, ":")
	if len(parts) != 4 {
		return containerImageIdentitySupportCursor{}, fmt.Errorf("support cursor has %d parts, want 4", len(parts))
	}
	scopeID, err := hex.DecodeString(parts[1])
	if err != nil || len(scopeID) == 0 || !utf8.Valid(scopeID) {
		return containerImageIdentitySupportCursor{}, fmt.Errorf("support cursor has invalid scope component")
	}
	digest, err := hex.DecodeString(parts[2])
	if err != nil || len(digest) == 0 || !utf8.Valid(digest) {
		return containerImageIdentitySupportCursor{}, fmt.Errorf("support cursor has invalid digest component")
	}
	supportID, err := hex.DecodeString(parts[3])
	if err != nil || len(supportID) != 32 {
		return containerImageIdentitySupportCursor{}, fmt.Errorf("support cursor has invalid support component")
	}
	return containerImageIdentitySupportCursor{
		scopeID:   scopeID,
		digest:    digest,
		supportID: supportID,
	}, nil
}

func compareContainerImageIdentitySupportCursors(
	left containerImageIdentitySupportCursor,
	right containerImageIdentitySupportCursor,
) int {
	if compared := bytes.Compare(left.scopeID, right.scopeID); compared != 0 {
		return compared
	}
	if compared := bytes.Compare(left.digest, right.digest); compared != 0 {
		return compared
	}
	return bytes.Compare(left.supportID, right.supportID)
}

func (s FactStore) listCurrentContainerImageIdentitySupportFacts(
	ctx context.Context,
	filter containerImageIdentitySupportFactFilter,
) ([]facts.Envelope, error) {
	if s.db == nil {
		return nil, fmt.Errorf("fact store database is required")
	}
	filter.normalize()
	if filter.empty() {
		return nil, nil
	}

	var loaded []facts.Envelope
	var cursorFactID string
	var priorCursor *containerImageIdentitySupportCursor
	for {
		page, err := s.listCurrentContainerImageIdentitySupportFactsPage(ctx, filter, cursorFactID)
		if err != nil {
			return nil, err
		}
		for _, envelope := range page {
			cursor, parseErr := parseContainerImageIdentitySupportCursor(envelope.FactID)
			if parseErr != nil {
				return nil, fmt.Errorf("list current container image identity supports: invalid fact ID %q: %w", envelope.FactID, parseErr)
			}
			if priorCursor != nil && compareContainerImageIdentitySupportCursors(*priorCursor, cursor) >= 0 {
				return nil, fmt.Errorf("list current container image identity supports: cursor did not advance after %q", cursorFactID)
			}
			priorCursor = &cursor
		}
		loaded = append(loaded, page...)
		if len(page) < listFactsByKindPageSize {
			return loaded, nil
		}
		nextCursor := page[len(page)-1].FactID
		if nextCursor == "" || nextCursor == cursorFactID {
			return nil, fmt.Errorf("list current container image identity supports: cursor did not advance after %q", cursorFactID)
		}
		cursorFactID = nextCursor
	}
}

func (s FactStore) listCurrentContainerImageIdentitySupportFactsPage(
	ctx context.Context,
	filter containerImageIdentitySupportFactFilter,
	cursorFactID string,
) ([]facts.Envelope, error) {
	rows, err := s.db.QueryContext(
		ctx,
		listCurrentContainerImageIdentitySupportFactsQuery,
		filter.digests,
		filter.imageRefs,
		filter.repositoryIDs,
		filter.sourceRepositoryIDs,
		filter.scopeIDs,
		cursorFactID,
		listFactsByKindPageSize,
	)
	if err != nil {
		return nil, fmt.Errorf("list current container image identity supports: %w", err)
	}
	defer func() { _ = rows.Close() }()

	loaded := make([]facts.Envelope, 0, listFactsByKindPageSize)
	for rows.Next() {
		envelope, scanErr := scanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list current container image identity supports: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list current container image identity supports: %w", err)
	}
	return loaded, nil
}

func combineDistinctFactStreams(streams ...[]facts.Envelope) ([]facts.Envelope, error) {
	total := 0
	for _, stream := range streams {
		total += len(stream)
	}
	combined := make([]facts.Envelope, 0, total)
	seen := make(map[string]struct{}, total)
	for _, stream := range streams {
		for _, envelope := range stream {
			if envelope.FactID == "" {
				return nil, fmt.Errorf("combine fact streams: empty fact ID")
			}
			if _, duplicate := seen[envelope.FactID]; duplicate {
				return nil, fmt.Errorf("combine fact streams: duplicate fact ID %q", envelope.FactID)
			}
			seen[envelope.FactID] = struct{}{}
			combined = append(combined, envelope)
		}
	}
	return combined, nil
}
