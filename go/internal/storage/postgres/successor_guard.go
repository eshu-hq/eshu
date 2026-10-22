// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	linksfreshnessstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/freshness/links"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// successorGuardCountQuery sizes one generation for the #7926
// successor-emptiness backstop in a single index-only pass over
// fact_records_scope_generation_idx (#8750): the rows a collector stream can
// replace, every row, and the file rows. Reducer domains write
// reducer_-prefixed facts into the source generation after it activates; no
// collector stream carries them, so counting them inflated the prior and
// refused ordinary successors. File rows include tombstones so the count
// matches successorGuardAnchorFilePathsQuery row for row; a full generation,
// the only kind sized by files, carries no file tombstones.
const successorGuardCountQuery = `
SELECT COUNT(*) FILTER (WHERE ` + linksfreshnessstore.ExcludeReducerDerivedKinds + `),
       COUNT(*),
       COUNT(*) FILTER (WHERE fact_kind = 'file')
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
`

// successorGuardAnchorFilePathsQuery reads a delta's base full generation's
// file paths for the exact union probe (#8750). It runs only when the
// zero-query lower bound fails, a delta that may remove most of the base's
// files; it is served by fact_records_scope_generation_idx plus heap reads of
// the file rows.
const successorGuardAnchorFilePathsQuery = `
SELECT COALESCE(payload ->> 'relative_path', '')
FROM fact_records
WHERE scope_id = $1
  AND generation_id = $2
  AND fact_kind = 'file'
`

// guardFactCounts is one generation's size as the backstop sees it.
type guardFactCounts struct {
	// Collector counts the rows a collector stream emits: every row except
	// reducer-derived ones.
	Collector int
	// Total counts every row, reducer-derived ones included.
	Total int
	// Files counts the file rows.
	Files int
}

func (c guardFactCounts) add(other guardFactCounts) guardFactCounts {
	return guardFactCounts{
		Collector: c.Collector + other.Collector,
		Total:     c.Total + other.Total,
		Files:     c.Files + other.Files,
	}
}

// countGuardFacts sizes one generation for the backstop. A delta generation
// with a projected base reports base plus own rows, the same upper bound
// CountFacts reports, and unioned true; a delta whose base is missing returns
// projector.DeltaBaseMissingError so the caller can size it on its own rows
// (#8362).
func (s FactStore) countGuardFacts(ctx context.Context, scopeID, generationID string) (guardFactCounts, bool, error) {
	if s.database == nil {
		return guardFactCounts{}, false, fmt.Errorf("fact store database is required")
	}
	baseGenerationID, _, wantUnion, err := s.resolveDeltaUnionBase(ctx, scopeID, generationID)
	if err != nil {
		return guardFactCounts{}, false, err
	}
	own, err := s.countGuardFactsGeneration(ctx, scopeID, generationID)
	if err != nil || !wantUnion {
		return own, false, err
	}
	base, err := s.countGuardFactsGeneration(ctx, scopeID, baseGenerationID)
	if err != nil {
		return guardFactCounts{}, false, err
	}
	return base.add(own), true, nil
}

// countGuardFactsGeneration runs successorGuardCountQuery for one
// (scope, generation), with no delta-union resolution.
func (s FactStore) countGuardFactsGeneration(ctx context.Context, scopeID, generationID string) (guardFactCounts, error) {
	rows, err := s.database.QueryContext(ctx, successorGuardCountQuery, scopeID, generationID)
	if err != nil {
		return guardFactCounts{}, fmt.Errorf("count guard facts: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var counts guardFactCounts
	if rows.Next() {
		if err := rows.Scan(&counts.Collector, &counts.Total, &counts.Files); err != nil {
			return guardFactCounts{}, fmt.Errorf("count guard facts scan: %w", err)
		}
	}
	return counts, rows.Err()
}

// successorPrior carries the candidate past the duplicate check so the
// guard compares without re-reading. counts are the candidate's guard
// counts; a full successor is compared with counts.Collector. orphanFallback
// (#8362): counts holds only the candidate's own rows; an empty successor
// still refuses. For a delta successor the anchor fields name the base full
// generation the projector will overlay it on (#8750).
type successorPrior struct {
	candidate      backfill.UnchangedGenerationCandidate
	found          bool
	counts         guardFactCounts
	orphanFallback bool
	anchorID       string
	anchorMissing  bool
	anchorFiles    int
}

// sizing names how counts was derived, for the guard log.
func (p successorPrior) sizing() string {
	if p.orphanFallback {
		return "own_rows_base_missing"
	}
	return "candidate"
}

// successorPriorForGuard reads the guard counts on the base connection
// before the commit transaction opens. Missing-base (#8362) falls back to
// the candidate's own rows; other failures fail closed. For a delta
// successor it also resolves the anchor — the newest projected full
// generation at the delta's baseline commit, through the projector's own
// resolver — and the anchor's file count.
func (s IngestionStore) successorPriorForGuard(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	candidate backfill.UnchangedGenerationCandidate,
	found bool,
	generation scope.ScopeGeneration,
) (successorPrior, error) {
	prior := successorPrior{candidate: candidate, found: found}
	if !found || generation.TrustedDeletion {
		return prior, nil
	}
	store := NewFactStore(s.database)
	counts, unioned, err := store.countGuardFacts(ctx, scopeValue.ScopeID, candidate.GenerationID)
	switch {
	case err == nil:
		prior.counts = counts
	case isDeltaBaseMissing(err):
		// The prior is a delta with no base full generation: the union
		// size is unknowable, so size the guard on the candidate's own
		// fact_records rows instead. A fallback failure fails closed.
		fallback, fallbackErr := store.countGuardFactsGeneration(ctx, scopeValue.ScopeID, candidate.GenerationID)
		if fallbackErr != nil {
			return successorPrior{}, fmt.Errorf("count prior generation facts fallback for scope %s: %w", scopeValue.ScopeID, fallbackErr)
		}
		s.logCommitStage(ctx, scopeValue, generation, "successor_prior_orphan_fallback", time.Now(),
			slog.String("candidate_generation_id", candidate.GenerationID),
			slog.Int("prior_fact_count_fallback", fallback.Collector))
		prior.counts, prior.orphanFallback, unioned = fallback, true, true
	default:
		return successorPrior{}, fmt.Errorf("count prior generation facts for scope %s: %w", scopeValue.ScopeID, err)
	}
	if !generation.IsDelta {
		return prior, nil
	}
	anchorID, err := store.resolveDeltaBaseAtCommit(ctx, scopeValue.ScopeID, generation.GenerationID, generation.DeltaBaselineCommitSHA)
	if err != nil {
		if isDeltaBaseMissing(err) {
			prior.anchorMissing = true
			return prior, nil
		}
		return successorPrior{}, fmt.Errorf("resolve delta successor anchor for scope %s: %w", scopeValue.ScopeID, err)
	}
	prior.anchorID = anchorID
	if anchorID == candidate.GenerationID && !unioned {
		prior.anchorFiles = prior.counts.Files
		return prior, nil
	}
	anchorCounts, err := store.countGuardFactsGeneration(ctx, scopeValue.ScopeID, anchorID)
	if err != nil {
		return successorPrior{}, fmt.Errorf("count delta successor anchor facts for scope %s: %w", scopeValue.ScopeID, err)
	}
	prior.anchorFiles = anchorCounts.Files
	return prior, nil
}

func isDeltaBaseMissing(err error) bool {
	var baseMissing *projector.DeltaBaseMissingError
	return errors.As(err, &baseMissing)
}

// successorObserver collects, while the fact stream lands, what the guard
// needs to size a delta successor: its delta_generation repository fact and
// the relative paths of its live file facts. It costs one map lookup per
// envelope and keeps file paths only for delta generations.
type successorObserver struct {
	keepFiles         bool
	scope             facts.DeltaScope
	liveRelativePaths []string
}

func newSuccessorObserver(generation scope.ScopeGeneration) *successorObserver {
	return &successorObserver{keepFiles: generation.IsDelta}
}

// observe records one streamed envelope. The repository check is a cheap
// prefix filter; facts.ParseDeltaScope applies the overlay's own kind
// normalization (it accepts the legacy "repositoryFact" kind).
func (o *successorObserver) observe(envelope facts.Envelope) {
	if !o.scope.IsDelta && strings.HasPrefix(envelope.FactKind, "repository") {
		if parsed := facts.ParseDeltaScope([]facts.Envelope{envelope}); parsed.IsDelta {
			o.scope = parsed
		}
		return
	}
	if !o.keepFiles {
		return
	}
	if rel, ok := facts.LiveFileRelativePath(envelope); ok {
		o.liveRelativePaths = append(o.liveRelativePaths, rel)
	}
}

// unionSets returns the qualified touched-or-live path set and the count of
// distinct qualified live file paths, matched exactly as OverlayDeltaBase
// matches base rows.
func (o *successorObserver) unionSets() (map[string]struct{}, int) {
	touchedOrLive := o.scope.Touched()
	live := make(map[string]struct{}, len(o.liveRelativePaths))
	for _, rel := range o.liveRelativePaths {
		qualified := o.scope.FilePath(facts.Envelope{FactKind: "file", Payload: map[string]any{"relative_path": rel}})
		if qualified == "" {
			continue
		}
		live[qualified] = struct{}{}
		touchedOrLive[qualified] = struct{}{}
	}
	return touchedOrLive, len(live)
}

// countAnchorFilesTouched counts the anchor's file rows inside touchedOrLive,
// qualified with the delta's repository path exactly as OverlayDeltaBase
// qualifies base rows.
func (s IngestionStore) countAnchorFilesTouched(
	ctx context.Context,
	scopeID string,
	anchorID string,
	deltaScope facts.DeltaScope,
	touchedOrLive map[string]struct{},
) (int, error) {
	rows, err := s.database.QueryContext(ctx, successorGuardAnchorFilePathsQuery, scopeID, anchorID)
	if err != nil {
		return 0, fmt.Errorf("read delta successor anchor file paths: %w", err)
	}
	defer func() { _ = rows.Close() }()
	touched := 0
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return 0, fmt.Errorf("read delta successor anchor file paths scan: %w", err)
		}
		qualified := deltaScope.FilePath(facts.Envelope{FactKind: "file", Payload: map[string]any{"relative_path": rel}})
		if _, ok := touchedOrLive[qualified]; ok && qualified != "" {
			touched++
		}
	}
	return touched, rows.Err()
}

// refuseDestructiveSuccessor evaluates the #7926 successor-emptiness
// backstop after the fact stream lands, when the successor's size is known,
// and before any projector work is enqueued; commitScopeGeneration is the
// only caller. A refusal rolls the commit transaction back — mirroring the
// finalized-generation replay — so the prior generation stays active,
// records the refusal under the destructive-successor reason, and reports
// handled: the next collection recollects and is re-evaluated, instead of
// retrying a successor the store will never accept. The projector never
// sees the refused generation, so observation-preserving projection
// behavior is unchanged.
//
// Sizing is like for like (#8750): a full successor is compared with the
// prior's collector rows, and a delta successor with the file-level union
// it will project over its anchor (backfill.DecideSuccessor). Only a delta
// whose zero-query bound fails reads anything here: one probe of the
// anchor's file paths on the base connection. Every decision logs the
// successor_guard stage and counts
// eshu_dp_ingestion_successor_guard_decisions_total. The caller marks its
// transaction closed and returns success on a refusal.
func (s IngestionStore) refuseDestructiveSuccessor(
	ctx context.Context,
	tx db.Transaction,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	prior successorPrior,
	successorFacts int,
	observer *successorObserver,
) (bool, error) {
	guardStart := time.Now()
	input := backfill.SuccessorGuardInput{
		PriorFound:      prior.found,
		PriorCollector:  prior.counts.Collector,
		PriorOrphan:     prior.orphanFallback,
		SuccessorFacts:  successorFacts,
		TrustedDeletion: generation.TrustedDeletion,
	}
	kind := telemetry.SuccessorGuardKindFull
	var touchedOrLive map[string]struct{}
	if generation.IsDelta {
		kind = telemetry.SuccessorGuardKindDelta
	}
	if generation.IsDelta != observer.scope.IsDelta {
		// The generation and its stream disagree on whether it is a delta.
		// Without the touched paths the union is unknowable, so it is sized
		// as a full: the stricter rule.
		s.logCommitStage(ctx, scopeValue, generation, "successor_delta_scope_mismatch", guardStart,
			slog.Bool("generation_is_delta", generation.IsDelta),
			slog.Bool("stream_delta_generation", observer.scope.IsDelta))
	} else if generation.IsDelta {
		input.Delta = true
		input.AnchorMissing = prior.anchorMissing
		input.AnchorFiles = prior.anchorFiles
		touchedOrLive, input.LiveFiles = observer.unionSets()
		input.TouchedOrLive = len(touchedOrLive)
	}
	decision := backfill.DecideSuccessor(input)
	refuse := decision.Refuse
	unionFiles := -1
	var probeDuration time.Duration
	if decision.NeedsProbe {
		probeStart := time.Now()
		touched, err := s.countAnchorFilesTouched(ctx, scopeValue.ScopeID, prior.anchorID, observer.scope, touchedOrLive)
		probeDuration = time.Since(probeStart)
		if err != nil {
			return false, err
		}
		refuse, unionFiles = backfill.DecideDeltaFileUnion(input.AnchorFiles, touched, input.LiveFiles)
	}
	s.recordSuccessorGuardDecision(ctx, kind, decision.Rule, refuse)
	guardAttrs := []any{
		slog.String("successor_kind", kind),
		slog.String("rule", decision.Rule),
		slog.Bool("refused", refuse),
		slog.Int("prior_collector_fact_count", prior.counts.Collector),
		slog.Int("prior_reducer_fact_count", prior.counts.Total-prior.counts.Collector),
		slog.String("prior_sizing", prior.sizing()),
		slog.Int("successor_fact_count", successorFacts),
	}
	if input.Delta {
		guardAttrs = append(guardAttrs,
			slog.Int("anchor_file_count", input.AnchorFiles),
			slog.Int("touched_path_count", len(observer.scope.Touched())),
			slog.Int("successor_live_file_count", input.LiveFiles),
			slog.Int("union_file_lower_bound", decision.UnionLowerBound),
		)
	}
	if decision.NeedsProbe {
		guardAttrs = append(guardAttrs,
			slog.Int("union_file_count", unionFiles),
			slog.Float64("probe_duration_seconds", probeDuration.Seconds()),
		)
	}
	s.logCommitStage(ctx, scopeValue, generation, "successor_guard", guardStart, guardAttrs...)
	if !refuse {
		return false, nil
	}
	if rollbackErr := tx.Rollback(); rollbackErr != nil {
		return false, fmt.Errorf("rollback destructive successor: %w", rollbackErr)
	}
	s.backfillStore().RecordSkippedRefresh(ctx, scopeValue, generation, prior.candidate.GenerationID,
		telemetry.RefreshSkipReasonDestructiveSuccessor)
	s.logCommitStage(ctx, scopeValue, generation, "refuse_destructive_successor", guardStart,
		slog.String("candidate_generation_id", prior.candidate.GenerationID),
		slog.String("rule", decision.Rule),
		slog.Int("prior_fact_count", prior.counts.Collector),
		slog.Int("prior_reducer_fact_count", prior.counts.Total-prior.counts.Collector),
		slog.String("prior_sizing", prior.sizing()),
		slog.Bool("prior_orphan_fallback", prior.orphanFallback),
		slog.Int("successor_fact_count", successorFacts),
	)
	return true, nil
}

// recordSuccessorGuardDecision counts one guard decision. Nil instruments
// skip the metric.
func (s IngestionStore) recordSuccessorGuardDecision(ctx context.Context, kind, rule string, refused bool) {
	if s.Instruments == nil || s.Instruments.IngestionSuccessorGuardDecisions == nil {
		return
	}
	outcome := telemetry.SuccessorGuardOutcomeStored
	if refused {
		outcome = telemetry.SuccessorGuardOutcomeRefused
	}
	s.Instruments.IngestionSuccessorGuardDecisions.Add(context.WithoutCancel(ctx), 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionKind, kind),
		attribute.String(telemetry.MetricDimensionRule, telemetry.SuccessorGuardRule(rule)),
		telemetry.AttrOutcome(outcome),
	))
}
