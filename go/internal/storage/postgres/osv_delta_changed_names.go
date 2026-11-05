// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
)

// changedNamesQuery reads the (advisory, ecosystem, package, withdrawn,
// feed timestamp) rows the delta feed reported, from active generations
// only (#9777 step 2).
//
// The active-generation joins are the ones every active reader uses (the
// scope's active_generation_id and an active scope_generations row), so a
// fact in a superseded or pending generation is not change truth. The read
// selects the raw feed spelling verbatim — case, separators, and the Maven
// group and Swift namespace all stay as the satellite wrote them — because
// any fold here would bake a miss into the intersection instead of leaving
// the mismatch visible there. The withdrawn flag arrives as text and is cast
// strictly: 'true' and 'false' are the only spellings the emitter writes, so
// anything else fails the read loudly instead of silently flipping a
// retraction.
const changedNamesQuery = `
SELECT DISTINCT
    BTRIM(COALESCE(fact.payload->>'advisory_id', '')) AS advisory_id,
    BTRIM(COALESCE(fact.payload->>'ecosystem', '')) AS ecosystem,
    BTRIM(COALESCE(fact.payload->>'package_name', '')) AS package_name,
    CASE BTRIM(COALESCE(fact.payload->>'withdrawn', 'false'))
        WHEN 'true' THEN TRUE
        WHEN 'false' THEN FALSE
    END AS withdrawn,
    BTRIM(COALESCE(fact.payload->>'feed_timestamp', '')) AS feed_timestamp
FROM fact_records AS fact
JOIN ingestion_scopes AS scope
  ON scope.scope_id = fact.scope_id
 AND scope.active_generation_id = fact.generation_id
JOIN scope_generations AS generation
  ON generation.scope_id = fact.scope_id
 AND generation.generation_id = fact.generation_id
WHERE fact.fact_kind = 'vulnerability.changed_name'
  AND fact.is_tombstone = FALSE
  AND generation.status = 'active'
`

// VulnerabilityChangedName is one changed_name fact as stored: the raw feed
// spelling of all five fields, exactly as the satellite emitted them. The
// coordinator intersection folds and validates these; this read never does.
// All values are trimmed; a row with a blank advisory, ecosystem, package,
// or feed timestamp is skipped, never returned as a lookup.
type VulnerabilityChangedName struct {
	AdvisoryID    string
	Ecosystem     string
	PackageName   string
	Withdrawn     bool
	FeedTimestamp string
}

// ListVulnerabilityChangedNames returns the distinct changed_name facts from
// active generations, in no particular order. The change-driven re-query
// path unions these with the names Eshu already stores per advisory id, so
// a record that dropped a name still re-queries the old one. A nil result
// means no change was reported: the drain plans nothing and the watermark
// holds. A query failure is returned, never answered with empty: planning
// nothing on an error would stall the replan queue while hiding the store
// outage.
func (s *FactStore) ListVulnerabilityChangedNames(
	ctx context.Context,
) ([]VulnerabilityChangedName, error) {
	if s.database == nil {
		return nil, fmt.Errorf("fact store database is required")
	}

	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(ctx, contract.SpanChangedNamesRead)
	defer span.End()

	rows, err := s.database.QueryContext(ctx, changedNamesQuery)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "changed names read failed")
		return nil, fmt.Errorf("list vulnerability changed names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var changed []VulnerabilityChangedName
	for rows.Next() {
		var advisoryID, ecosystem, packageName, feedTimestamp string
		var withdrawn bool
		if err := rows.Scan(&advisoryID, &ecosystem, &packageName, &withdrawn, &feedTimestamp); err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "changed names read failed")
			return nil, fmt.Errorf("scan vulnerability changed names: %w", err)
		}
		if strings.TrimSpace(advisoryID) == "" || strings.TrimSpace(ecosystem) == "" ||
			strings.TrimSpace(packageName) == "" || strings.TrimSpace(feedTimestamp) == "" {
			continue
		}
		changed = append(changed, VulnerabilityChangedName{
			AdvisoryID:    strings.TrimSpace(advisoryID),
			Ecosystem:     strings.TrimSpace(ecosystem),
			PackageName:   strings.TrimSpace(packageName),
			Withdrawn:     withdrawn,
			FeedTimestamp: strings.TrimSpace(feedTimestamp),
		})
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "changed names read failed")
		return nil, fmt.Errorf("iterate vulnerability changed names: %w", err)
	}
	span.SetAttributes(
		attribute.Int("changed_names.facts", len(changed)),
	)
	return changed, nil
}
