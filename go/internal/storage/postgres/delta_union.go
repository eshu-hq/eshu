// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// deltaUnionTargetQuery reads the claimed generation's delta identity by
// primary key: whether it carries only file-scoped changes and which commit
// it was diffed from. A missing row yields no rows; the caller then loads
// without a union.
const deltaUnionTargetQuery = `
SELECT is_delta,
       COALESCE(delta_baseline_commit_sha, '')
FROM scope_generations
WHERE scope_id = $1
  AND generation_id = $2
`

// deltaUnionBaseQuery resolves the newest projected full generation at one
// commit: non-delta, activated (activated_at set, the established projected
// predicate), of a terminal-or-active status. Active and completed generations
// sort before superseded ones: retention only ever prunes superseded
// generations, so an active or completed base is intact by construction,
// while a superseded base needs the retention-event probe below. Same-commit
// generations observe the same tree, so intactness outranks recency. The
// lookup is served by the pre-existing status-led bitmap on
// scope_generations_scope_idx (sub-millisecond, ~0.33 ms warm); it carries
// no dedicated index dependency.
const deltaUnionBaseQuery = `
SELECT generation_id, status
FROM scope_generations
WHERE scope_id = $1
  AND is_delta = false
  AND activated_at IS NOT NULL
  AND status IN ('active', 'completed', 'superseded')
  AND source_commit_sha = $2
ORDER BY (status IN ('active', 'completed')) DESC, ingested_at DESC, generation_id DESC
LIMIT 1
`

// deltaUnionBasePrunedQuery probes whether retention ever pruned the base
// generation, by the same hashed identity the retention events carry. It is
// served by generation_retention_events_generation_idx. A probe failure is
// not a refusal: the events table is a refinement signal for partial-prune
// detection, and bricking delta projection on its outage would trade a rare
// silent partial for a certain fleet-wide stall. The empty-base check in
// listFactsUnion still catches a fully pruned base when the probe is down.
const deltaUnionBasePrunedQuery = `
SELECT 1
FROM generation_retention_events
WHERE scope_id_hash = $1
  AND generation_id_hash = $2
LIMIT 1
`

// WithLogger returns a copy that logs one line per delta-base union decision.
// A nil logger disables the log; instruments and spans are unaffected.
func (s FactStore) WithLogger(logger *slog.Logger) *FactStore {
	s.logger = logger
	return &s
}

// resolveDeltaUnionBase decides whether the (scope, generation) load needs
// the delta-base union and, when it does, which base full generation to
// overlay. It returns wantUnion false for full generations, legacy deltas
// without a recorded baseline, and generations with no row: all three keep
// today's single-generation behavior. A delta with a baseline but no
// projected base full generation at that commit fails closed with
// projector.DeltaBaseMissingError: the base was never projected or retention
// pruned it, and projecting the delta alone would restore only its files.
func (s FactStore) resolveDeltaUnionBase(
	ctx context.Context,
	scopeID string,
	generationID string,
) (baseGenerationID string, baselineSHA string, wantUnion bool, err error) {
	if s.database == nil {
		return "", "", false, fmt.Errorf("fact store database is required")
	}
	rows, err := s.database.QueryContext(ctx, deltaUnionTargetQuery, scopeID, generationID)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve delta union target: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var (
		isDelta  bool
		baseline string
	)
	if rows.Next() {
		if scanErr := rows.Scan(&isDelta, &baseline); scanErr != nil {
			return "", "", false, fmt.Errorf("resolve delta union target scan: %w", scanErr)
		}
	}
	if err := rows.Err(); err != nil {
		return "", "", false, fmt.Errorf("resolve delta union target: %w", err)
	}
	baseline = strings.TrimSpace(baseline)
	if !isDelta || baseline == "" {
		return "", "", false, nil
	}
	baseRows, err := s.database.QueryContext(ctx, deltaUnionBaseQuery, scopeID, baseline)
	if err != nil {
		return "", "", false, fmt.Errorf("resolve delta union base: %w", err)
	}
	defer func() { _ = baseRows.Close() }()
	var (
		baseID     string
		baseStatus string
	)
	if baseRows.Next() {
		if scanErr := baseRows.Scan(&baseID, &baseStatus); scanErr != nil {
			return "", "", false, fmt.Errorf("resolve delta union base scan: %w", scanErr)
		}
	}
	if err := baseRows.Err(); err != nil {
		return "", "", false, fmt.Errorf("resolve delta union base: %w", err)
	}
	baseID = strings.TrimSpace(baseID)
	if baseID == "" {
		return "", baseline, false, deltaBaseMissing(scopeID, generationID, baseline)
	}
	if baseStatus == "superseded" && s.deltaUnionBasePruned(ctx, scopeID, baseID) {
		return "", baseline, false, deltaBaseMissing(scopeID, generationID, baseline)
	}
	return baseID, baseline, true, nil
}

// deltaBaseMissing builds the fail-closed refusal for a delta with no usable
// base: never projected, fully pruned, or retention-pruned while superseded.
func deltaBaseMissing(scopeID, generationID, baseline string) *projector.DeltaBaseMissingError {
	return &projector.DeltaBaseMissingError{
		ScopeID:           scopeID,
		GenerationID:      generationID,
		BaselineCommitSHA: baseline,
	}
}

// deltaUnionBasePruned reports whether retention recorded any prune event
// for the base generation. Only superseded bases are probed: active and
// completed generations are never prune-eligible, so probing them would only
// add a query. An event means the base lost facts to retention (wholly or
// partially) and the union would project short of the full tree.
func (s FactStore) deltaUnionBasePruned(ctx context.Context, scopeID, baseID string) bool {
	rows, err := s.database.QueryContext(ctx, deltaUnionBasePrunedQuery,
		retentionHashID("scope", scopeID), retentionHashID("generation", baseID))
	if err != nil {
		return false
	}
	defer func() { _ = rows.Close() }()
	pruned := rows.Next()
	if err := rows.Err(); err != nil {
		return false
	}
	return pruned
}

// listFactsUnion loads one delta generation overlaid on its base full
// generation: one base fact query plus the Go overlay in facts.OverlayDeltaBase
// (delta wins per stable key, tombstones and touched paths remove base rows).
// The delta repository fact survives, so downstream retracts stay file-scoped
// while writes carry the full set.
func (s FactStore) listFactsUnion(
	ctx context.Context,
	scopeID string,
	generationID string,
	baseGenerationID string,
	baselineSHA string,
	delta []facts.Envelope,
) ([]facts.Envelope, error) {
	base, err := s.listFactsGeneration(ctx, scopeID, baseGenerationID)
	if err != nil {
		return nil, err
	}
	if len(base) == 0 {
		// The base row exists but carries no facts: retention drained it
		// without an event this probe could see, or its facts never
		// persisted. A stored full generation always carries at least its
		// repository fact, so an empty read is a pruned base, and overlaying
		// the delta on it would restore only the delta's files.
		s.recordDeltaUnionRefused(ctx, scopeID, generationID, len(delta))
		return nil, deltaBaseMissing(scopeID, generationID, baselineSHA)
	}
	scope := facts.ParseDeltaScope(delta)
	if !scope.IsDelta {
		// The store resolved a union but the delta carries no
		// delta_generation repository fact, so the overlay below runs
		// additively (base plus delta concatenation, no dedup): the
		// DB-versus-payload divergence needs a signal, and the load
		// proceeds. Fail-closed refusal was considered and rejected
		// here: it would stall projections that succeed today on
		// legacy or mis-marked generations (see the outcome contract
		// on projector.DeltaUnionScopeMismatch).
		merged := facts.OverlayDeltaBase(base, delta, scope)
		if s.logger != nil {
			s.logger.WarnContext(ctx, "projector delta union scope mismatch: generation resolves as delta with a baseline but carries no delta_generation repository fact; loading base plus delta additively",
				log.ScopeID(scopeID),
				slog.String("generation_id", generationID),
				slog.Int("base_fact_count", len(base)),
				slog.Int("delta_fact_count", len(delta)),
				slog.Int("union_fact_count", len(merged)),
			)
		}
		s.recordDeltaUnion(ctx, scopeID, generationID, projector.DeltaUnionScopeMismatch,
			len(base), len(delta), len(merged))
		return merged, nil
	}
	merged := facts.OverlayDeltaBase(base, delta, scope)
	s.recordDeltaUnion(ctx, scopeID, generationID, projector.DeltaUnionBaseFound,
		len(base), len(delta), len(merged))
	return merged, nil
}

// listFactsGeneration runs listFactsQuery for one (scope, generation).
func (s FactStore) listFactsGeneration(
	ctx context.Context,
	scopeID string,
	generationID string,
) ([]facts.Envelope, error) {
	rows, err := s.database.QueryContext(ctx, listFactsQuery, scopeID, generationID)
	if err != nil {
		return nil, fmt.Errorf("list facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var loaded []facts.Envelope
	for rows.Next() {
		envelope, scanErr := backfill.ScanFactEnvelope(rows)
		if scanErr != nil {
			return nil, fmt.Errorf("list facts: %w", scanErr)
		}
		loaded = append(loaded, envelope)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list facts: %w", err)
	}
	return loaded, nil
}

// recordDeltaUnion counts one union decision, records its fact counts, adds
// a span event to the active projection span, and logs one line. Nil
// instruments skip the metrics; a span without a recorder drops the event;
// a nil logger skips the log. Commit SHAs never become labels.
func (s FactStore) recordDeltaUnion(
	ctx context.Context,
	scopeID string,
	generationID string,
	outcome projector.DeltaUnionOutcome,
	baseCount int,
	deltaCount int,
	unionCount int,
) {
	detached := context.WithoutCancel(ctx)
	if s.instruments != nil {
		if s.instruments.ProjectorDeltaUnion != nil {
			s.instruments.ProjectorDeltaUnion.Add(detached, 1, metric.WithAttributes(
				telemetry.AttrOutcome(string(outcome)),
			))
		}
		if s.instruments.ProjectorDeltaUnionFacts != nil {
			histogram := s.instruments.ProjectorDeltaUnionFacts
			outcomeAttr := telemetry.AttrOutcome(string(outcome))
			histogram.Record(detached, int64(baseCount), metric.WithAttributes(
				outcomeAttr, attribute.String(telemetry.MetricDimensionKind, "base"),
			))
			histogram.Record(detached, int64(deltaCount), metric.WithAttributes(
				outcomeAttr, attribute.String(telemetry.MetricDimensionKind, "delta"),
			))
			histogram.Record(detached, int64(unionCount), metric.WithAttributes(
				outcomeAttr, attribute.String(telemetry.MetricDimensionKind, "union"),
			))
		}
	}
	trace.SpanFromContext(ctx).AddEvent("projector.delta_union", trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionOutcome, string(outcome)),
		attribute.String("scope_id", scopeID),
		attribute.String("generation_id", generationID),
		attribute.Int("base_fact_count", baseCount),
		attribute.Int("delta_fact_count", deltaCount),
		attribute.Int("union_fact_count", unionCount),
	))
	if s.logger == nil {
		return
	}
	s.logger.InfoContext(detached, "projector delta union fact load",
		log.ScopeID(scopeID),
		slog.String("generation_id", generationID),
		slog.String("outcome", string(outcome)),
		slog.Int("base_fact_count", baseCount),
		slog.Int("delta_fact_count", deltaCount),
		slog.Int("union_fact_count", unionCount),
	)
}

// recordDeltaUnionRefused records a refused union decision: the load failed
// closed before reading any base facts, so all counts are zero except the
// delta rows already read.
func (s FactStore) recordDeltaUnionRefused(
	ctx context.Context,
	scopeID string,
	generationID string,
	deltaCount int,
) {
	s.recordDeltaUnion(ctx, scopeID, generationID,
		projector.DeltaUnionBaseMissingRefused, 0, deltaCount, 0)
}
