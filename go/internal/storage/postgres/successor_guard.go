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
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

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

// guardPriorSide is one generation sized for the guard's prior: the
// duplicate-check candidate or the scope's active generation (#8750 F1).
type guardPriorSide struct {
	id    string
	sized bool
	// counts are the generation's guard counts: the base+own union for a
	// delta with a base, own rows otherwise.
	counts guardFactCounts
	// orphan (#8362): a delta whose base is missing, sized on its own rows.
	orphan bool
	// unioned reports that counts are not the generation's own file rows,
	// so they cannot stand in for an anchor's file count.
	unioned bool
}

// collector is the side's collector row count for the guard log, -1 when
// the side was not sized.
func (side guardPriorSide) collector() int {
	if !side.sized {
		return -1
	}
	return side.counts.Collector
}

// successorPrior carries the candidate past the duplicate check so the
// guard compares without re-reading. counts are the larger of the candidate
// and the active generation (pickGuardPrior); a full successor is compared
// with counts.Collector. orphanFallback (#8362): the deciding side holds
// only a base-missing delta's own rows; an empty successor still refuses.
// For a delta successor the anchor fields name the base full generation the
// projector will overlay it on (#8750). activeRead, hasActive, and activeID
// record the active read as it ran, so the in-tx recheck compares it exactly
// (#8750 window).
type successorPrior struct {
	candidate      backfill.UnchangedGenerationCandidate
	found          bool
	counts         guardFactCounts
	orphanFallback bool
	fromActive     bool
	sizingID       string
	candidateSide  guardPriorSide
	activeSide     guardPriorSide
	anchorID       string
	anchorMissing  bool
	anchorFiles    int
	activeRead     bool
	hasActive      bool
	activeID       string
}

// sizing names how counts was derived, for the guard log.
func (p successorPrior) sizing() string {
	switch {
	case p.orphanFallback:
		return "own_rows_base_missing"
	case p.fromActive:
		return "active"
	default:
		return "candidate"
	}
}

// pickGuardPrior chooses the guard's prior from the sized sides (#8750 F1
// ruling): the side with more collector rows decides, the candidate on a
// tie. An unsized side never decides. When both sides are empty the prior
// is orphan if either side is (#8362: an empty successor still refuses).
func pickGuardPrior(candidate, active guardPriorSide) (chosen guardPriorSide, fromActive bool) {
	if !active.sized {
		return candidate, false
	}
	if !candidate.sized || active.counts.Collector > candidate.counts.Collector {
		return active, true
	}
	if candidate.counts.Collector <= 0 && active.orphan {
		candidate.orphan = true
	}
	return candidate, false
}

// successorPriorForGuard reads the guard counts on database: the base
// connection before the commit transaction opens, and the commit transaction
// when the in-tx recheck re-sizes (#8750 window). The prior is the larger of the
// duplicate-check candidate and the scope's active generation (#8750 F1): a
// pending candidate is not yet in the graph, and the claim sweep lets a
// newer full supersede it unprojected, so neither side alone bounds what a
// successor retracts. A pending candidate counts only while its projector
// work is live; a wedged one cannot activate, so next to an active
// generation it is not sized. An active candidate is the active generation:
// no extra read. Every read failure fails closed. For a delta successor it
// also resolves the anchor — the newest projected full generation at the
// delta's baseline commit, through the projector's own resolver — and the
// anchor's file count.
func (s IngestionStore) successorPriorForGuard(
	ctx context.Context,
	database db.ExecQueryer,
	scopeValue scope.IngestionScope,
	candidate backfill.UnchangedGenerationCandidate,
	found bool,
	generation scope.ScopeGeneration,
) (successorPrior, error) {
	prior := successorPrior{candidate: candidate, found: found}
	if !found || generation.TrustedDeletion {
		return prior, nil
	}
	store := NewFactStore(database)
	if candidate.Status != scope.GenerationStatusActive {
		activeID, hasActive, err := activeGenerationForGuard(ctx, database, scopeValue.ScopeID)
		if err != nil {
			return successorPrior{}, fmt.Errorf("read active generation for successor guard on scope %s: %w", scopeValue.ScopeID, err)
		}
		prior.activeRead, prior.hasActive, prior.activeID = true, hasActive, activeID
		if hasActive && activeID != candidate.GenerationID {
			if prior.activeSide, err = s.sizeGuardGeneration(ctx, store, scopeValue, generation, candidate.GenerationID, activeID); err != nil {
				return successorPrior{}, err
			}
		}
	}
	if candidate.WorkLive || !prior.activeSide.sized {
		var err error
		if prior.candidateSide, err = s.sizeGuardGeneration(ctx, store, scopeValue, generation, candidate.GenerationID, candidate.GenerationID); err != nil {
			return successorPrior{}, err
		}
	}
	chosen, fromActive := pickGuardPrior(prior.candidateSide, prior.activeSide)
	prior.counts, prior.orphanFallback, prior.fromActive, prior.sizingID = chosen.counts, chosen.orphan, fromActive, chosen.id
	if !generation.IsDelta {
		return prior, nil
	}
	// The anchor is the successor delta's union base (#8780): the full its
	// changed set was diffed from, COALESCEd to the baseline for a delta
	// without a recorded union base, exactly as the projector resolves it.
	anchorID, err := store.resolveDeltaBaseAtCommit(ctx, scopeValue.ScopeID, generation.GenerationID,
		deltaUnionCommitsFor(generation.DeltaBaselineCommitSHA, generation.DeltaUnionBaseCommitSHA))
	if err != nil {
		if isDeltaBaseMissing(err) {
			prior.anchorMissing = true
			return prior, nil
		}
		return successorPrior{}, fmt.Errorf("resolve delta successor anchor for scope %s: %w", scopeValue.ScopeID, err)
	}
	prior.anchorID = anchorID
	// The anchor is a full generation, usually one a prior side already
	// sized: the candidate when it is active, the active side when the
	// candidate is pending (review N1). Reuse that count instead of reading
	// it again.
	for _, side := range []guardPriorSide{prior.candidateSide, prior.activeSide} {
		if side.sized && side.id == anchorID && !side.unioned {
			prior.anchorFiles = side.counts.Files
			return prior, nil
		}
	}
	anchorCounts, err := store.countGuardFactsGeneration(ctx, scopeValue.ScopeID, anchorID)
	if err != nil {
		return successorPrior{}, fmt.Errorf("count delta successor anchor facts for scope %s: %w", scopeValue.ScopeID, err)
	}
	prior.anchorFiles = anchorCounts.Files
	return prior, nil
}

// sizeGuardGeneration sizes one prior generation, candidate or active, so
// the two sides cannot drift: a delta with a base reports its union, a delta
// whose base is missing (#8362) falls back to its own rows and logs the
// successor_prior_orphan_fallback stage (candidate_generation_id stays the
// duplicate-check candidate; prior_sizing_generation_id names the generation
// sized), and any other failure (the
// fallback's included) fails closed.
func (s IngestionStore) sizeGuardGeneration(
	ctx context.Context,
	store *FactStore,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	candidateID string,
	generationID string,
) (guardPriorSide, error) {
	counts, unioned, err := store.countGuardFacts(ctx, scopeValue.ScopeID, generationID)
	if err == nil {
		return guardPriorSide{id: generationID, sized: true, counts: counts, unioned: unioned}, nil
	}
	if !isDeltaBaseMissing(err) {
		return guardPriorSide{}, fmt.Errorf("count prior generation facts for scope %s generation %s: %w", scopeValue.ScopeID, generationID, err)
	}
	// The prior is a delta with no base full generation: the union size is
	// unknowable, so size it on its own fact_records rows instead.
	fallback, err := store.countGuardFactsGeneration(ctx, scopeValue.ScopeID, generationID)
	if err != nil {
		return guardPriorSide{}, fmt.Errorf("count prior generation facts fallback for scope %s generation %s: %w", scopeValue.ScopeID, generationID, err)
	}
	s.logCommitStage(ctx, scopeValue, generation, "successor_prior_orphan_fallback", time.Now(),
		slog.String("candidate_generation_id", candidateID),
		slog.String("prior_sizing_generation_id", generationID),
		slog.Int("prior_fact_count_fallback", fallback.Collector))
	return guardPriorSide{id: generationID, sized: true, counts: fallback, orphan: true, unioned: true}, nil
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
	keepFiles              bool
	scope                  facts.DeltaScope
	liveRelativePaths      []string
	tombstoneRelativePaths []string
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
	rel, ok := facts.FileFactRelativePath(envelope)
	if !ok {
		return
	}
	if envelope.IsTombstone {
		o.tombstoneRelativePaths = append(o.tombstoneRelativePaths, rel)
		return
	}
	o.liveRelativePaths = append(o.liveRelativePaths, rel)
}

// unionSets returns the qualified touched-or-live path set and the count of
// distinct qualified live file paths, matched exactly as OverlayDeltaBase
// matches base rows. A file tombstone joins the set even when the repository
// fact does not list its path (review F3): the overlay drops a base row by
// tombstone stable key too. That is exact when the keys match and stricter
// otherwise.
func (o *successorObserver) unionSets() (map[string]struct{}, int) {
	touchedOrLive := o.scope.Touched()
	for _, rel := range o.tombstoneRelativePaths {
		if qualified := o.qualify(rel); qualified != "" {
			touchedOrLive[qualified] = struct{}{}
		}
	}
	live := make(map[string]struct{}, len(o.liveRelativePaths))
	for _, rel := range o.liveRelativePaths {
		qualified := o.qualify(rel)
		if qualified == "" {
			continue
		}
		live[qualified] = struct{}{}
		touchedOrLive[qualified] = struct{}{}
	}
	return touchedOrLive, len(live)
}

func (o *successorObserver) qualify(rel string) string {
	return o.scope.FilePath(facts.Envelope{FactKind: "file", Payload: map[string]any{"relative_path": rel}})
}

// countAnchorFilesTouched counts the anchor's file rows inside touchedOrLive,
// qualified with the delta's repository path exactly as OverlayDeltaBase
// qualifies base rows. The caller passes its open commit transaction: the
// probe runs while that transaction is open, and reading on it keeps a
// probing commit to one connection (review F2). The anchor is an older,
// committed generation, so READ COMMITTED visibility is the same as on the
// pool.
func (s IngestionStore) countAnchorFilesTouched(
	ctx context.Context,
	querier db.Queryer,
	scopeID string,
	anchorID string,
	deltaScope facts.DeltaScope,
	touchedOrLive map[string]struct{},
) (int, error) {
	rows, err := querier.QueryContext(ctx, successorGuardAnchorFilePathsQuery, scopeID, anchorID)
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
// anchor's file paths on the commit transaction. Every decision logs the
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
		touched, err := s.countAnchorFilesTouched(ctx, tx, scopeValue.ScopeID, prior.anchorID, observer.scope, touchedOrLive)
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
		slog.String("prior_sizing_generation_id", prior.sizingID),
		slog.Int("prior_active_collector_fact_count", prior.activeSide.collector()),
		slog.Int("prior_candidate_collector_fact_count", prior.candidateSide.collector()),
		slog.Int("successor_fact_count", successorFacts),
	}
	if input.Delta {
		guardAttrs = append(guardAttrs,
			slog.Int("anchor_file_count", input.AnchorFiles),
			slog.Int("touched_path_count", input.TouchedOrLive),
			slog.Int("manifest_path_count", len(observer.scope.Touched())),
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
	consecutive := s.recordSuccessorRefusal(ctx, scopeValue, generation)
	s.logCommitStage(ctx, scopeValue, generation, "refuse_destructive_successor", guardStart,
		slog.Int("consecutive_refusals", consecutive),
		slog.String("candidate_generation_id", prior.candidate.GenerationID),
		slog.String("rule", decision.Rule),
		slog.Int("prior_fact_count", prior.counts.Collector),
		slog.Int("prior_reducer_fact_count", prior.counts.Total-prior.counts.Collector),
		slog.String("prior_sizing", prior.sizing()),
		slog.String("prior_sizing_generation_id", prior.sizingID),
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
