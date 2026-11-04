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
// primary key: whether it carries only file-scoped changes, its #7319 fence
// commit, and the #8780 union-base commit its changed set was diffed from.
// Both columns come back raw; the base resolves at
// COALESCE(union base, baseline) in Go (deltaUnionCommitsFor), so a refusal
// can name the fence commit and the commit the lookup used. A missing row
// yields no rows; the caller then loads without a union.
const deltaUnionTargetQuery = `
SELECT is_delta,
       COALESCE(delta_baseline_commit_sha, ''),
       COALESCE(delta_union_base_commit_sha, '')
FROM scope_generations
WHERE scope_id = $1
  AND generation_id = $2
`

// deltaUnionCommits names the two commits a delta records (#8780): Baseline,
// its #7319 fence commit (the active commit when it was diffed), and
// UnionBase, the commit its base full generation resolves at.
type deltaUnionCommits struct {
	Baseline  string
	UnionBase string
}

// deltaUnionCommitsFor builds the commits from the raw columns, resolving the
// union base at COALESCE(union base, baseline). The fallback is sound for a
// delta stored before the union-base column existed: only a delta diffed from
// an active full could activate then, so its baseline is its base full's
// commit.
func deltaUnionCommitsFor(baseline, unionBase string) deltaUnionCommits {
	baseline, unionBase = strings.TrimSpace(baseline), strings.TrimSpace(unionBase)
	if unionBase == "" {
		unionBase = baseline
	}
	return deltaUnionCommits{Baseline: baseline, UnionBase: unionBase}
}

// chained reports that the delta was diffed over a prior delta in the chain:
// the active commit at diff time was not its union base's commit. An active
// delta at the union base's own commit cannot exist, because a full at the
// active delta's head that activated would be the active generation, so the
// inequality is exact.
func (c deltaUnionCommits) chained() bool {
	return c.UnionBase != "" && c.UnionBase != c.Baseline
}

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
) (baseGenerationID string, commits deltaUnionCommits, wantUnion bool, err error) {
	if s.database == nil {
		return "", deltaUnionCommits{}, false, fmt.Errorf("fact store database is required")
	}
	isDelta, commits, err := s.readDeltaUnionTarget(ctx, scopeID, generationID)
	if err != nil {
		return "", deltaUnionCommits{}, false, err
	}
	if !isDelta || commits.Baseline == "" {
		return "", deltaUnionCommits{}, false, nil
	}
	baseID, err := s.resolveDeltaBaseAtCommit(ctx, scopeID, generationID, commits)
	if err != nil {
		return "", commits, false, err
	}
	return baseID, commits, true, nil
}

// readDeltaUnionTarget reads one generation's delta identity. Each read
// closes its cursor before the caller issues the next statement: the
// destructive-successor guard runs these reads on its commit transaction
// (#8750 window), one connection, where pgx refuses a statement while an
// earlier cursor is open ("conn busy").
func (s FactStore) readDeltaUnionTarget(ctx context.Context, scopeID, generationID string) (bool, deltaUnionCommits, error) {
	rows, err := s.database.QueryContext(ctx, deltaUnionTargetQuery, scopeID, generationID)
	if err != nil {
		return false, deltaUnionCommits{}, fmt.Errorf("resolve delta union target: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var (
		isDelta             bool
		baseline, unionBase string
	)
	if rows.Next() {
		if scanErr := rows.Scan(&isDelta, &baseline, &unionBase); scanErr != nil {
			return false, deltaUnionCommits{}, fmt.Errorf("resolve delta union target scan: %w", scanErr)
		}
	}
	if err := rows.Err(); err != nil {
		return false, deltaUnionCommits{}, fmt.Errorf("resolve delta union target: %w", err)
	}
	return isDelta, deltaUnionCommitsFor(baseline, unionBase), nil
}

// listFactsUnion loads one delta generation overlaid on its base full
// generation: one base fact query plus the Go overlay in facts.OverlayDeltaBase
// (delta wins per stable key, tombstones and touched paths remove base rows).
// The delta repository fact survives, so downstream retracts stay file-scoped
// while writes carry the full set. It returns the union outcome with the
// facts; the projector projects only DeltaUnionBaseFound (#8779).
func (s FactStore) listFactsUnion(
	ctx context.Context,
	scopeID string,
	generationID string,
	baseGenerationID string,
	commits deltaUnionCommits,
	delta []facts.Envelope,
) ([]facts.Envelope, projector.DeltaUnionOutcome, error) {
	base, err := s.listFactsGeneration(ctx, scopeID, baseGenerationID)
	if err != nil {
		return nil, "", err
	}
	if len(base) == 0 {
		// The base row exists but carries no facts: retention drained it
		// without an event this probe could see, or its facts never
		// persisted. A stored full generation always carries at least its
		// repository fact, so an empty read is a pruned base, and overlaying
		// the delta on it would restore only the delta's files.
		s.recordDeltaUnionRefused(ctx, scopeID, generationID, commits, len(delta))
		return nil, "", deltaBaseMissing(scopeID, generationID, commits)
	}
	scope := facts.ParseDeltaScope(delta)
	if !scope.IsDelta {
		// The store resolved a union but the delta carries no
		// delta_generation repository fact, so the overlay below runs
		// additively (base plus delta concatenation, no dedup). The
		// facts load for gate and replay readers, but the outcome rides
		// the result and the projector refuses it before any write
		// (#8779): an additive set has duplicate stable keys and no delta
		// marker, so projecting it would run a full retract over it.
		merged := facts.OverlayDeltaBase(base, delta, scope)
		if s.logger != nil {
			s.logger.WarnContext(ctx, "projector delta union scope mismatch: generation resolves as delta with a baseline but carries no delta_generation repository fact; the projector refuses the additive overlay",
				log.ScopeID(scopeID),
				slog.String("generation_id", generationID),
				slog.Int("base_fact_count", len(base)),
				slog.Int("delta_fact_count", len(delta)),
				slog.Int("union_fact_count", len(merged)),
			)
		}
		s.recordDeltaUnion(ctx, scopeID, generationID, commits, projector.DeltaUnionScopeMismatch,
			len(base), len(delta), len(merged))
		return merged, projector.DeltaUnionScopeMismatch, nil
	}
	merged := facts.OverlayDeltaBase(base, delta, scope)
	s.recordDeltaUnion(ctx, scopeID, generationID, commits, projector.DeltaUnionBaseFound,
		len(base), len(delta), len(merged))
	return merged, projector.DeltaUnionBaseFound, nil
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
// a span event to the active projection span, and logs one line. The span
// event and log carry both commits and whether the delta is chained (#8780);
// commit SHAs and chained never become metric labels. Nil instruments skip
// the metrics; a span without a recorder drops the event; a nil logger skips
// the log.
func (s FactStore) recordDeltaUnion(
	ctx context.Context,
	scopeID string,
	generationID string,
	commits deltaUnionCommits,
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
		attribute.String("delta_baseline_commit_sha", commits.Baseline),
		attribute.String("delta_union_base_commit_sha", commits.UnionBase),
		attribute.Bool("chained", commits.chained()),
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
		slog.String("delta_baseline_commit_sha", commits.Baseline),
		slog.String("delta_union_base_commit_sha", commits.UnionBase),
		slog.Bool("chained", commits.chained()),
	)
}

// recordDeltaUnionRefused records a refused union decision: the load failed
// closed before reading any base facts, so all counts are zero except the
// delta rows already read.
func (s FactStore) recordDeltaUnionRefused(
	ctx context.Context,
	scopeID string,
	generationID string,
	commits deltaUnionCommits,
	deltaCount int,
) {
	s.recordDeltaUnion(ctx, scopeID, generationID, commits,
		projector.DeltaUnionBaseMissingRefused, 0, deltaCount, 0)
}
