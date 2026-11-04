// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/backfill"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// Outcome label values of eshu_dp_package_manifest_dependency_read_delta_union_total
// (#9991), one per delta-active scope a manifest dependency read reached.
// Only union_base_found adds base rows; every other outcome returns that
// scope's delta rows only, the read's behavior before #9991.
const (
	// manifestDeltaUnionBaseFound: the base resolved and contributed the
	// matching pins the delta did not override, delete, or tombstone.
	manifestDeltaUnionBaseFound = "union_base_found"
	// manifestDeltaUnionBaseUnmatched: the base resolved but holds no row
	// matching the read's names, so there is nothing to add.
	manifestDeltaUnionBaseUnmatched = "union_base_unmatched"
	// manifestDeltaUnionBaseMissing: the #7797 rule refused the base (never
	// projected, retention-pruned, or the prune probe failed).
	manifestDeltaUnionBaseMissing = "union_base_missing"
	// manifestDeltaUnionScopeMismatch: the delta carries no delta_generation
	// repository fact, so the overlay would run additively; the projector
	// refuses that union too.
	manifestDeltaUnionScopeMismatch = "union_scope_mismatch"
	// manifestDeltaUnionNoBaseline: a legacy delta with no recorded baseline,
	// which the projector also loads without a union.
	manifestDeltaUnionNoBaseline = "union_no_baseline"
	// manifestDeltaUnionResolveFailed: a base or overlay read failed; the
	// whole read returns the error.
	manifestDeltaUnionResolveFailed = "union_resolve_failed"
)

// Fragments deltaBasePackageManifestDependencyQuery swaps in every branch of
// an active manifest dependency query. The base branch keeps the active
// text's filters, keyset cursor, ordering, and limit, so it is driven by the
// same partial indexes (fact_records_active_package_dependency_entity_idx
// and its folded twin, migration 247). Its generation predicate is a strict
// superset of deltaUnionBaseQuery: every activated full generation of a
// delta-active scope at the delta's union-base commit (COALESCE union base,
// baseline). resolveDeltaBaseAtCommit then picks the one base the projector
// overlays, so the base rule keeps one definition.
const (
	manifestActiveScopeJoin  = "AND scope.active_generation_id = fact.generation_id"
	manifestDeltaBaseJoin    = "AND scope.active_generation_id <> fact.generation_id\nJOIN scope_generations AS active_delta\n  ON active_delta.scope_id = scope.scope_id\n AND active_delta.generation_id = scope.active_generation_id"
	manifestActiveGeneration = "AND generation.status = 'active'"
	manifestDeltaBaseFull    = "AND generation.is_delta = false\n  AND generation.activated_at IS NOT NULL\n  AND generation.source_commit_sha = COALESCE(NULLIF(TRIM(active_delta.delta_union_base_commit_sha), ''), NULLIF(TRIM(active_delta.delta_baseline_commit_sha), ''))\n  AND active_delta.is_delta = true\n  AND active_delta.status = 'active'"
)

// deltaBasePackageManifestDependencyQuery derives the base-candidate query
// from one active manifest dependency query text. It fails closed when a
// branch does not carry exactly one of each swapped fragment.
func deltaBasePackageManifestDependencyQuery(active string) (string, error) {
	branches := strings.Count(active, "FROM fact_records AS fact")
	if branches == 0 ||
		strings.Count(active, manifestActiveScopeJoin) != branches ||
		strings.Count(active, manifestActiveGeneration) != branches {
		return "", fmt.Errorf("manifest dependency delta base query shape is unsupported")
	}
	base := strings.ReplaceAll(active, manifestActiveScopeJoin, manifestDeltaBaseJoin)
	return strings.ReplaceAll(base, manifestActiveGeneration, manifestDeltaBaseFull), nil
}

// manifestDeltaTargetsQuery returns the scopes among $1 whose active
// generation is a delta, with the raw baseline and union-base commits that
// deltaUnionCommitsFor resolves. It is the batched form of
// deltaUnionTargetQuery for one read's scope set, served by the
// ingestion_scopes and scope_generations primary keys.
const manifestDeltaTargetsQuery = `
SELECT scope.scope_id,
       active.generation_id,
       COALESCE(active.delta_baseline_commit_sha, ''),
       COALESCE(active.delta_union_base_commit_sha, '')
FROM ingestion_scopes AS scope
JOIN scope_generations AS active
  ON active.scope_id = scope.scope_id
 AND active.generation_id = scope.active_generation_id
WHERE scope.scope_id = ANY($1::text[])
  AND active.is_delta = true
  AND active.status = 'active'
ORDER BY scope.scope_id
`

// manifestDeltaOverlayInputsQuery reads, for each (scope, delta generation)
// pair, the rows facts.OverlayDeltaBase needs from the delta: its repository
// fact (the changed and deleted paths) and every row, tombstones included,
// whose stable key collides with a base candidate. The active read never
// returns tombstones, so they must come from here. It probes
// fact_records_scope_generation_idx by (scope_id, generation_id) and reads no
// payload of non-matching rows.
const manifestDeltaOverlayInputsQuery = `
SELECT fact.fact_id, fact.scope_id, fact.generation_id, fact.fact_kind,
       fact.stable_fact_key, fact.schema_version, fact.collector_kind,
       fact.fencing_token, fact.source_confidence, fact.source_system,
       fact.source_fact_key, COALESCE(fact.source_uri, ''),
       COALESCE(fact.source_record_id, ''), fact.observed_at,
       fact.is_tombstone, fact.payload
FROM unnest($1::text[], $2::text[]) AS target(scope_id, generation_id)
JOIN fact_records AS fact
  ON fact.scope_id = target.scope_id
 AND fact.generation_id = target.generation_id
WHERE fact.fact_kind IN ('repository', 'repositoryFact')
   OR fact.stable_fact_key = ANY($3::text[])
`

// manifestDeltaTarget is one delta-active scope a read reached.
type manifestDeltaTarget struct {
	scopeID      string
	generationID string
	commits      deltaUnionCommits
}

// manifestDeltaOverlay is one resolved delta-active scope with base pins.
type manifestDeltaOverlay struct {
	target     manifestDeltaTarget
	baseID     string
	candidates []facts.Envelope
}

// snapshotReadQueryer lets a FactStore clone run its existing read helpers
// on one read-only repeatable-read transaction; it refuses writes.
type snapshotReadQueryer struct{ db.Queryer }

// ExecContext refuses every statement: the manifest dependency read is
// read-only.
func (snapshotReadQueryer) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, fmt.Errorf("manifest dependency read snapshot is read-only")
}

// overlayDeltaManifestBase adds, for every delta-active scope among the
// active and base-candidate rows, the base pins that facts.OverlayDeltaBase
// keeps: the base is the one resolveDeltaBaseAtCommit returns, the same
// rule, prune probe, and refusal the projector uses (#7797, #8780). A scope
// whose base is refused or untrustworthy keeps its delta rows only and is
// counted. The result keeps the (observed_at, fact_id) order.
func (s FactStore) overlayDeltaManifestBase(ctx context.Context, read string, active, base []facts.Envelope) ([]facts.Envelope, error) {
	scopeIDs := manifestScopeIDs(active, base)
	if len(scopeIDs) == 0 {
		return active, nil
	}
	targets, err := s.readManifestDeltaTargets(ctx, scopeIDs)
	if err != nil {
		s.recordManifestDeltaUnion(ctx, read, manifestDeltaUnionResolveFailed)
		return nil, err
	}
	baseByScope := make(map[string][]facts.Envelope)
	for _, env := range base {
		baseByScope[env.ScopeID] = append(baseByScope[env.ScopeID], env)
	}
	var overlays []manifestDeltaOverlay
	for _, target := range targets {
		if target.commits.Baseline == "" {
			s.recordManifestDeltaUnion(ctx, read, manifestDeltaUnionNoBaseline)
			continue
		}
		baseID, err := s.resolveDeltaBaseAtCommit(ctx, target.scopeID, target.generationID, target.commits)
		var missing *projector.DeltaBaseMissingError
		if errors.As(err, &missing) {
			s.recordManifestDeltaUnion(ctx, read, manifestDeltaUnionBaseMissing)
			s.warnManifestDeltaUnion(ctx, target, "manifest dependency read delta base missing: returning the delta's rows only")
			continue
		}
		if err != nil {
			s.recordManifestDeltaUnion(ctx, read, manifestDeltaUnionResolveFailed)
			return nil, err
		}
		candidates := slices.DeleteFunc(slices.Clone(baseByScope[target.scopeID]), func(env facts.Envelope) bool {
			return env.GenerationID != baseID
		})
		if len(candidates) == 0 {
			s.recordManifestDeltaUnion(ctx, read, manifestDeltaUnionBaseUnmatched)
			continue
		}
		overlays = append(overlays, manifestDeltaOverlay{target: target, baseID: baseID, candidates: candidates})
	}
	if len(overlays) == 0 {
		return active, nil
	}
	inputs, err := s.readManifestDeltaOverlayInputs(ctx, overlays)
	if err != nil {
		s.recordManifestDeltaUnion(ctx, read, manifestDeltaUnionResolveFailed)
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
			s.recordManifestDeltaUnion(ctx, read, manifestDeltaUnionScopeMismatch)
			s.warnManifestDeltaUnion(ctx, overlay.target, "manifest dependency read delta carries no delta_generation repository fact: returning the delta's rows only")
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
		s.recordManifestDeltaUnion(ctx, read, manifestDeltaUnionBaseFound)
	}
	slices.SortStableFunc(merged, func(a, b facts.Envelope) int {
		if c := a.ObservedAt.Compare(b.ObservedAt); c != 0 {
			return c
		}
		return strings.Compare(a.FactID, b.FactID)
	})
	return merged, nil
}

// manifestScopeIDs returns the sorted distinct scope ids of the rows.
func manifestScopeIDs(groups ...[]facts.Envelope) []string {
	var ids []string
	for _, group := range groups {
		for _, env := range group {
			ids = append(ids, env.ScopeID)
		}
	}
	slices.Sort(ids)
	return slices.Compact(ids)
}

// readManifestDeltaTargets returns the delta-active scopes among scopeIDs.
func (s FactStore) readManifestDeltaTargets(ctx context.Context, scopeIDs []string) ([]manifestDeltaTarget, error) {
	rows, err := s.database.QueryContext(ctx, manifestDeltaTargetsQuery, scopeIDs)
	if err != nil {
		return nil, fmt.Errorf("read manifest dependency delta targets: %w", err)
	}
	defer func() { _ = rows.Close() }()
	var targets []manifestDeltaTarget
	for rows.Next() {
		var target manifestDeltaTarget
		var baseline, unionBase string
		if err := rows.Scan(&target.scopeID, &target.generationID, &baseline, &unionBase); err != nil {
			return nil, fmt.Errorf("read manifest dependency delta targets: %w", err)
		}
		target.commits = deltaUnionCommitsFor(baseline, unionBase)
		targets = append(targets, target)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read manifest dependency delta targets: %w", err)
	}
	return targets, nil
}

// readManifestDeltaOverlayInputs loads each overlay's delta repository fact
// and stable-key collisions in one statement, grouped by scope.
func (s FactStore) readManifestDeltaOverlayInputs(ctx context.Context, overlays []manifestDeltaOverlay) (map[string][]facts.Envelope, error) {
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
	rows, err := s.database.QueryContext(ctx, manifestDeltaOverlayInputsQuery, scopeIDs, generationIDs, keys)
	if err != nil {
		return nil, fmt.Errorf("read manifest dependency delta overlay inputs: %w", err)
	}
	defer func() { _ = rows.Close() }()
	inputs := make(map[string][]facts.Envelope, len(overlays))
	for rows.Next() {
		env, err := backfill.ScanFactEnvelope(rows)
		if err != nil {
			return nil, fmt.Errorf("read manifest dependency delta overlay inputs: %w", err)
		}
		inputs[env.ScopeID] = append(inputs[env.ScopeID], env)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read manifest dependency delta overlay inputs: %w", err)
	}
	return inputs, nil
}

// recordManifestDeltaUnion counts one delta-active scope's union decision
// and adds a span event to the caller's span. Scope ids, generation ids, and
// commit SHAs never become metric labels.
func (s FactStore) recordManifestDeltaUnion(ctx context.Context, read, outcome string) {
	if s.instruments != nil && s.instruments.PackageManifestDependencyReadDeltaUnion != nil {
		s.instruments.PackageManifestDependencyReadDeltaUnion.Add(context.WithoutCancel(ctx), 1,
			metric.WithAttributes(telemetry.AttrRead(read), telemetry.AttrOutcome(outcome)))
	}
	trace.SpanFromContext(ctx).AddEvent("package_manifest_dependency_read.delta_union", trace.WithAttributes(
		attribute.String(telemetry.MetricDimensionRead, read),
		attribute.String(telemetry.MetricDimensionOutcome, outcome),
	))
}

// warnManifestDeltaUnion logs one delta-active scope whose base pins the
// read could not add, with both commits so an operator can match it to the
// projector's own refusal for that generation.
func (s FactStore) warnManifestDeltaUnion(ctx context.Context, target manifestDeltaTarget, message string) {
	if s.logger == nil {
		return
	}
	s.logger.WarnContext(context.WithoutCancel(ctx), message,
		log.ScopeID(target.scopeID),
		slog.String("generation_id", target.generationID),
		slog.String("delta_baseline_commit_sha", target.commits.Baseline),
		slog.String("delta_union_base_commit_sha", target.commits.UnionBase),
		slog.Bool("chained", target.commits.chained()),
	)
}
