// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Outcome label values a delta-overlay reader records, one per delta-active
// scope the read reached (#9991, #10003). Only union_base_found adds base
// rows; every other outcome returns that scope's delta rows only, the read's
// behavior without the overlay.
const (
	// deltaOverlayBaseFound: the base resolved and contributed the matching
	// rows the delta did not override, delete, or tombstone.
	deltaOverlayBaseFound = "union_base_found"
	// deltaOverlayBaseUnmatched: the base resolved but holds no row matching
	// the read, so there is nothing to add. A base that retention drained
	// without an event (which the projector refuses by its empty read) also
	// lands here; both keep the delta-only rows.
	deltaOverlayBaseUnmatched = "union_base_unmatched"
	// deltaOverlayBaseMissing: the #7797 rule refused the base (never
	// projected, retention-pruned, or the prune probe failed).
	deltaOverlayBaseMissing = "union_base_missing"
	// deltaOverlayScopeMismatch: the delta carries no delta_generation
	// repository fact, so the overlay would run additively; the projector
	// refuses that union too.
	deltaOverlayScopeMismatch = "union_scope_mismatch"
	// deltaOverlayNoBaseline: a legacy delta with no recorded baseline, which
	// the projector also loads without a union.
	deltaOverlayNoBaseline = "union_no_baseline"
	// deltaOverlayResolveFailed: the target lookup, a base page, the base
	// resolution, or the overlay inputs read failed; the whole read returns
	// the error. It counts once per failed read, not per scope.
	deltaOverlayResolveFailed = "union_resolve_failed"
)

// deltaOverlayQueryExecMode pins a custom plan on the overlay inputs read,
// whose stable_fact_key = ANY($3) is a residual Filter (the #9586 trap; see
// manifestDependencyQueryExecMode for the mechanism).
const deltaOverlayQueryExecMode = pgx.QueryExecModeCacheDescribe

// deltaOverlayTarget is one scope whose active generation is a delta.
type deltaOverlayTarget struct {
	scopeID      string
	generationID string
	commits      deltaUnionCommits
}

// deltaOverlayCandidates is one resolved delta-active scope with base rows.
type deltaOverlayCandidates struct {
	target     deltaOverlayTarget
	baseID     string
	candidates []facts.Envelope
}

// deltaOverlaySnapshot is one reader call's delta-overlay state: a FactStore
// clone bound to one read-only repeatable-read transaction, and the
// delta-target lookup, read at most once and shared by every leg of the
// call. The active pages, the target lookup, the base pages, the base
// resolution, and the overlay inputs all read the same snapshot, so a delta
// that activates mid-call cannot tear the result.
type deltaOverlaySnapshot struct {
	store       FactStore
	targets     []deltaOverlayTarget
	targetsRead bool
}

// withDeltaOverlaySnapshot runs read on one read-only repeatable-read
// snapshot. Inside another snapshot (the impact snapshot, or an outer delta
// overlay snapshot) the transaction nests as a no-op on the outer one.
func (s FactStore) withDeltaOverlaySnapshot(ctx context.Context, read func(*deltaOverlaySnapshot) error) error {
	return withReadOnlyRepeatableRead(ctx, s.database, func(queryer db.Queryer) error {
		snapshot := &deltaOverlaySnapshot{store: s}
		snapshot.store.database = deltaOverlaySnapshotQueryer{Queryer: queryer}
		return read(snapshot)
	})
}

// deltaTargets returns every delta-active scope, running the lookup on the
// first call only. A failed lookup is not cached; the read returns its error.
func (snapshot *deltaOverlaySnapshot) deltaTargets(ctx context.Context) ([]deltaOverlayTarget, error) {
	if snapshot.targetsRead {
		return snapshot.targets, nil
	}
	targets, err := snapshot.store.readDeltaOverlayTargets(ctx)
	if err != nil {
		return nil, err
	}
	snapshot.targets, snapshot.targetsRead = targets, true
	return targets, nil
}

// deltaOverlaySnapshotQueryer lets a FactStore clone run its existing read
// helpers on the snapshot transaction. It refuses writes and nests a further
// snapshot as a no-op on the same transaction.
type deltaOverlaySnapshotQueryer struct{ db.Queryer }

// ExecContext refuses every statement: a delta-overlay read is read-only.
func (deltaOverlaySnapshotQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("delta overlay read snapshot is read-only")
}

// BeginReadOnlyRepeatableRead nests on the open snapshot: the returned
// transaction reads through it and its Commit and Rollback do nothing.
func (q deltaOverlaySnapshotQueryer) BeginReadOnlyRepeatableRead(context.Context) (db.Transaction, error) {
	return deltaOverlayNestedTransaction{deltaOverlaySnapshotQueryer: q}, nil
}

// deltaOverlayNestedTransaction is a snapshot nested on an open one.
type deltaOverlayNestedTransaction struct{ deltaOverlaySnapshotQueryer }

// Commit does nothing: the outer snapshot owns the transaction.
func (deltaOverlayNestedTransaction) Commit() error { return nil }

// Rollback does nothing: the outer snapshot owns the transaction.
func (deltaOverlayNestedTransaction) Rollback() error { return nil }

// overlayDeltaBase adds, for every delta-active target among the active and
// base-candidate rows' scopes, the base rows that facts.OverlayDeltaBase
// keeps. The base is the one resolveDeltaBases returns, the same rule, prune
// probe, and refusal the projector uses (#7797, #8780), resolved for every
// reached scope with at most two statements. A scope whose base is refused or
// untrustworthy keeps its delta rows only and is counted. Only reached scopes
// are resolved and counted. The result keeps the (observed_at, fact_id)
// keyset order of the envelope readers.
func (s FactStore) overlayDeltaBase(
	ctx context.Context,
	sink deltaOverlayTelemetry,
	targets []deltaOverlayTarget,
	active, base []facts.Envelope,
) ([]facts.Envelope, error) {
	reached := make(map[string]struct{})
	for _, scopeID := range deltaOverlayScopeIDs(active, base) {
		reached[scopeID] = struct{}{}
	}
	targets = slices.DeleteFunc(slices.Clone(targets), func(target deltaOverlayTarget) bool {
		_, ok := reached[target.scopeID]
		return !ok
	})
	if len(targets) == 0 {
		return active, nil
	}
	tally := newDeltaOverlayTally(sink)
	defer s.recordDeltaOverlaySpan(ctx, tally)
	baseByScope := make(map[string][]facts.Envelope)
	for _, env := range base {
		baseByScope[env.ScopeID] = append(baseByScope[env.ScopeID], env)
	}
	var requests []deltaBaseRequest
	for _, target := range targets {
		if target.commits.Baseline == "" {
			s.recordDeltaOverlay(ctx, tally, deltaOverlayNoBaseline)
			continue
		}
		requests = append(requests, deltaBaseRequest(target))
	}
	resolved, err := s.resolveDeltaBases(ctx, requests)
	if err != nil {
		s.recordDeltaOverlay(ctx, tally, deltaOverlayResolveFailed)
		return nil, err
	}
	var overlays []deltaOverlayCandidates
	for _, request := range requests {
		target := deltaOverlayTarget(request)
		resolution := resolved[target.scopeID]
		if resolution.refused != nil {
			s.recordDeltaOverlay(ctx, tally, deltaOverlayBaseMissing)
			s.warnDeltaOverlay(ctx, sink, target, "delta base missing: returning the delta's rows only")
			continue
		}
		candidates := slices.DeleteFunc(slices.Clone(baseByScope[target.scopeID]), func(env facts.Envelope) bool {
			return env.GenerationID != resolution.baseID
		})
		if len(candidates) == 0 {
			s.recordDeltaOverlay(ctx, tally, deltaOverlayBaseUnmatched)
			continue
		}
		overlays = append(overlays, deltaOverlayCandidates{target: target, baseID: resolution.baseID, candidates: candidates})
	}
	if len(overlays) == 0 {
		return active, nil
	}
	inputs, err := s.readDeltaOverlayInputs(ctx, overlays)
	if err != nil {
		s.recordDeltaOverlay(ctx, tally, deltaOverlayResolveFailed)
		return nil, err
	}
	merged := slices.Clone(active)
	seen := make(map[string]struct{}, len(active))
	for _, env := range active {
		seen[env.FactID] = struct{}{}
	}
	for _, overlay := range overlays {
		delta := inputs[overlay.target.scopeID]
		scope := facts.ParseDeltaScope(delta)
		if !scope.IsDelta {
			s.recordDeltaOverlay(ctx, tally, deltaOverlayScopeMismatch)
			s.warnDeltaOverlay(ctx, sink, overlay.target, "delta carries no delta_generation repository fact: returning the delta's rows only")
			continue
		}
		for _, env := range facts.OverlayDeltaBase(overlay.candidates, delta, scope) {
			if env.GenerationID != overlay.baseID {
				continue
			}
			if _, dup := seen[env.FactID]; dup {
				continue
			}
			seen[env.FactID] = struct{}{}
			merged = append(merged, env)
		}
		s.recordDeltaOverlay(ctx, tally, deltaOverlayBaseFound)
	}
	slices.SortStableFunc(merged, func(a, b facts.Envelope) int {
		if c := a.ObservedAt.Compare(b.ObservedAt); c != 0 {
			return c
		}
		return strings.Compare(a.FactID, b.FactID)
	})
	return merged, nil
}

// deltaOverlayBaseScopes returns the scope ids of the targets a base scan can
// overlay: those with a recorded baseline. A legacy delta has no base.
func deltaOverlayBaseScopes(targets []deltaOverlayTarget) []string {
	var scopeIDs []string
	for _, target := range targets {
		if target.commits.Baseline != "" {
			scopeIDs = append(scopeIDs, target.scopeID)
		}
	}
	return scopeIDs
}

// deltaOverlayScopeIDs returns the sorted distinct scope ids of the rows.
func deltaOverlayScopeIDs(groups ...[]facts.Envelope) []string {
	var ids []string
	for _, group := range groups {
		for _, env := range group {
			ids = append(ids, env.ScopeID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// readDeltaOverlayTargets returns every delta-active scope.
func (s FactStore) readDeltaOverlayTargets(ctx context.Context) ([]deltaOverlayTarget, error) {
	rows, err := s.database.QueryContext(ctx, deltaOverlayTargetsQuery)
	if err != nil {
		return nil, fmt.Errorf("read delta overlay targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var targets []deltaOverlayTarget
	for rows.Next() {
		var target deltaOverlayTarget
		var baseline, unionBase string
		if err := rows.Scan(&target.scopeID, &target.generationID, &baseline, &unionBase); err != nil {
			return nil, fmt.Errorf("read delta overlay targets: %w", err)
		}
		target.commits = deltaUnionCommitsFor(baseline, unionBase)
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read delta overlay targets: %w", err)
	}
	return targets, nil
}

// readDeltaOverlayInputs loads each overlay's delta repository fact and
// stable-key collisions in one statement, grouped by scope.
func (s FactStore) readDeltaOverlayInputs(ctx context.Context, overlays []deltaOverlayCandidates) (map[string][]facts.Envelope, error) {
	scopeIDs := make([]string, 0, len(overlays))
	generationIDs := make([]string, 0, len(overlays))
	var keys []string
	for _, overlay := range overlays {
		scopeIDs = append(scopeIDs, overlay.target.scopeID)
		generationIDs = append(generationIDs, overlay.target.generationID)
		for _, env := range overlay.candidates {
			if key := strings.TrimSpace(env.StableFactKey); key != "" {
				keys = append(keys, key)
			}
		}
	}
	slices.Sort(keys)
	keys = slices.Compact(keys)
	rows, err := s.database.QueryContext(ctx, deltaOverlayInputsQuery, deltaOverlayQueryExecMode, scopeIDs, generationIDs, keys)
	if err != nil {
		return nil, fmt.Errorf("read delta overlay inputs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	inputs := make(map[string][]facts.Envelope, len(overlays))
	for rows.Next() {
		env, err := backfill.ScanFactEnvelope(rows)
		if err != nil {
			return nil, fmt.Errorf("read delta overlay inputs: %w", err)
		}
		inputs[env.ScopeID] = append(inputs[env.ScopeID], env)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read delta overlay inputs: %w", err)
	}
	return inputs, nil
}

// deltaOverlayTelemetry names where one reader records its overlay decisions:
// the read label, the subject its WARN lines start with, its per-read span
// event, and its outcome counter (nil skips the metric).
type deltaOverlayTelemetry struct {
	read      string
	subject   string
	spanEvent string
	counter   metric.Int64Counter
}

// deltaOverlayTally holds one read's overlay decisions by outcome, for the
// single span event the read emits.
type deltaOverlayTally struct {
	sink     deltaOverlayTelemetry
	outcomes map[string]int
}

// newDeltaOverlayTally starts an empty tally for one read.
func newDeltaOverlayTally(sink deltaOverlayTelemetry) *deltaOverlayTally {
	return &deltaOverlayTally{sink: sink, outcomes: map[string]int{}}
}

// recordDeltaOverlay counts one delta-active scope's overlay decision. Scope
// ids, generation ids, and commit SHAs never become metric labels.
func (s FactStore) recordDeltaOverlay(ctx context.Context, tally *deltaOverlayTally, outcome string) {
	tally.outcomes[outcome]++
	if tally.sink.counter != nil {
		tally.sink.counter.Add(context.WithoutCancel(ctx), 1,
			metric.WithAttributes(telemetry.AttrRead(tally.sink.read), telemetry.AttrOutcome(outcome)))
	}
}

// recordDeltaOverlayFailure counts union_resolve_failed once for a read whose
// target lookup or base pages failed before any scope decision.
func (s FactStore) recordDeltaOverlayFailure(ctx context.Context, sink deltaOverlayTelemetry) {
	tally := newDeltaOverlayTally(sink)
	s.recordDeltaOverlay(ctx, tally, deltaOverlayResolveFailed)
	s.recordDeltaOverlaySpan(ctx, tally)
}

// recordDeltaOverlaySpan adds one span event per read that reached a
// delta-active scope, carrying the decision count of each outcome. One event
// per read, not per scope, keeps a broad read under the SDK's per-span event
// cap.
func (s FactStore) recordDeltaOverlaySpan(ctx context.Context, tally *deltaOverlayTally) {
	if len(tally.outcomes) == 0 {
		return
	}
	attrs := []attribute.KeyValue{attribute.String(telemetry.MetricDimensionRead, tally.sink.read)}
	for _, outcome := range slices.Sorted(maps.Keys(tally.outcomes)) {
		attrs = append(attrs, attribute.Int(outcome, tally.outcomes[outcome]))
	}
	trace.SpanFromContext(ctx).AddEvent(tally.sink.spanEvent, trace.WithAttributes(attrs...))
}

// warnDeltaOverlay logs one delta-active scope whose base rows the read could
// not add, with both commits so an operator can match it to the projector's
// own refusal for that generation.
func (s FactStore) warnDeltaOverlay(ctx context.Context, sink deltaOverlayTelemetry, target deltaOverlayTarget, message string) {
	if s.logger == nil {
		return
	}
	s.logger.WarnContext(context.WithoutCancel(ctx), sink.subject+" "+message,
		log.ScopeID(target.scopeID),
		slog.String("generation_id", target.generationID),
		slog.String("delta_baseline_commit_sha", target.commits.Baseline),
		slog.String("delta_union_base_commit_sha", target.commits.UnionBase),
		slog.Bool("chained", target.commits.chained()),
	)
}
