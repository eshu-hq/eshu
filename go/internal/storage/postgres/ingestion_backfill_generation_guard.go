// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// validateGenerationInput checks scope/generation preconditions before
// opening a transaction. Per-fact validation (scope_id, generation_id match)
// happens inside upsertStreamingFacts as facts arrive from the channel.
func validateGenerationInput(
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
) error {
	if err := generation.ValidateForScope(scopeValue); err != nil {
		return err
	}
	if generation.IsTerminal() {
		return fmt.Errorf("generation %q must not be terminal before projection", generation.GenerationID)
	}
	// #7319: the one choke point for every writer. A delta must record the
	// commit it diffed from so the projector can fence it; a full generation
	// has no baseline.
	hasBaseline := strings.TrimSpace(generation.DeltaBaselineCommitSHA) != ""
	if generation.IsDelta && !hasBaseline {
		return fmt.Errorf("delta generation %q requires delta_baseline_commit_sha", generation.GenerationID)
	}
	if !generation.IsDelta && hasBaseline {
		return fmt.Errorf("full generation %q must not carry delta_baseline_commit_sha", generation.GenerationID)
	}

	return nil
}

// repositoryGenerationIdentity binds a repository to its active scope and
// generation for the deferred relationship pass.
type repositoryGenerationIdentity struct {
	RepoID       string
	ScopeID      string
	GenerationID string
}

// loadActiveRepositoryGenerations returns the active (scope_id, generation_id)
// per repository. It filters to fact_kind = 'repository' and therefore excludes
// scopes with no repository fact (for example GCP cloud-relationship scopes); do
// not use it as the partition source for the corpus-wide deferred backfill.
func loadActiveRepositoryGenerations(
	ctx context.Context,
	queryer db.Queryer,
) (map[string]repositoryGenerationIdentity, error) {
	if queryer == nil {
		return nil, nil
	}

	rows, err := queryer.QueryContext(ctx, activeRepositoryGenerationsQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]repositoryGenerationIdentity)
	for rows.Next() {
		var identity repositoryGenerationIdentity
		if err := rows.Scan(&identity.RepoID, &identity.ScopeID, &identity.GenerationID); err != nil {
			return nil, err
		}
		if strings.TrimSpace(identity.RepoID) == "" {
			continue
		}
		result[identity.RepoID] = identity
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// activeScopeGenerationQuery resolves ONE scope's currently active generation.
//
// It is the single-scope projection of latestGenerationCTE, which every
// corpus-wide deferred query joins against: that CTE picks, per scope,
// COALESCE(ingestion_scopes.active_generation_id, generation.generation_id)
// from the scope_generations row ordered by (ingested_at DESC,
// generation_id DESC). Restricting the same expression, join, and ordering to
// one scope_id and taking LIMIT 1 yields exactly the row DISTINCT ON (scope_id)
// would have chosen for that scope. TestFanInActiveGenerationMatchesCorpusLoader
// pins that correspondence against the shipped corpus-wide loader so the two
// cannot drift apart silently.
//
// The fan-in needs a per-scope lookup rather than loadActiveRepositoryGenerations
// because it runs once per partition. loadActiveRepositoryGenerations scans
// fact_records for every repository fact in the corpus; at ~910 partitions that
// would be ~910 corpus-wide scans per pass. This lookup is a LIMIT 1 read served
// by scope_generations_scope_latest_lookup_idx
// (scope_id, ingested_at DESC, generation_id DESC) from migration 002.
const activeScopeGenerationQuery = `
SELECT COALESCE(scope.active_generation_id, generation.generation_id) AS generation_id
FROM scope_generations AS generation
LEFT JOIN ingestion_scopes AS scope
  ON scope.scope_id = generation.scope_id
WHERE generation.scope_id = $1
ORDER BY generation.ingested_at DESC, generation.generation_id DESC
LIMIT 1
`

// loadActiveGenerationForScope returns the scope's active generation ID. The
// empty string with a nil error means the scope has no generation row at all,
// which the caller treats the same as an advanced generation: nothing to
// publish.
func loadActiveGenerationForScope(ctx context.Context, queryer db.Queryer, scopeID string) (string, error) {
	rows, err := queryer.QueryContext(ctx, activeScopeGenerationQuery, scopeID)
	if err != nil {
		return "", err
	}
	defer func() { _ = rows.Close() }()

	if !rows.Next() {
		return "", rows.Err()
	}
	var generationID string
	if err := rows.Scan(&generationID); err != nil {
		return "", err
	}
	return generationID, rows.Err()
}

// unchangedGenerationCandidateQuery reads the generation an incoming snapshot
// is compared with (#7978): the scope's newest pending or active generation
// that carries a freshness hint, with its commit and status. It is the same
// row activeGenerationFreshnessQuery selects, so a newer pending generation
// still wins over the active one: a snapshot equal to the active generation
// but not to a newer pending one is stored, because the pending one will
// replace the active one. Served by scope_generations_scope_latest_lookup_idx.
const unchangedGenerationCandidateQuery = `
SELECT generation.generation_id,
       COALESCE(generation.freshness_hint, ''),
       COALESCE(generation.source_commit_sha, ''),
       generation.status
FROM scope_generations AS generation
WHERE generation.scope_id = $1
  AND generation.status IN ('pending', 'active')
  AND COALESCE(generation.freshness_hint, '') <> ''
ORDER BY generation.ingested_at DESC, generation.generation_id DESC
LIMIT 1
`

// lockReprojectScopeQuery takes the scope row first, the lock order every
// blocking path shares (ingestion commit, projector Ack and Fail). A projector
// Ack that would supersede the active generation therefore commits before this
// lock is granted or waits until the re-open commits, so the next statement's
// snapshot sees a settled active generation.
const lockReprojectScopeQuery = `
SELECT scope_id FROM ingestion_scopes WHERE scope_id = $1 FOR NO KEY UPDATE
`

// reprojectActiveGenerationQuery re-opens the active generation's canonical
// projector work row for a reconciliation snapshot that matched it (#7978),
// instead of storing a duplicate generation. It mirrors the #7209 zombie heal
// (healProjectorActiveGenerationQuery): same canonical work_item_id, same
// reopened columns, and the same write-time in-flight guard, so repeated
// reconciliations converge on one open row and attempt_count stays monotonic.
//
// Fences, all in one statement after the scope lock:
//   - target: the generation is still the scope's active generation, status
//     active and activated, with the same freshness hint and commit the
//     snapshot carried. Otherwise no row, and the caller drops the snapshot.
//   - stamped: the generation's payload records reconcile_reason and
//     reconcile_requested_at; fullReconcileStateQuery reads the timestamp so
//     the sweep sees the scope as reconciled for one interval. The UPDATE
//     rechecks status = 'active' on the locked row.
//   - reopened: the work row is re-opened unless it is already pending,
//     retrying, or under a live lease; the claim sweep retires a pending row
//     whose generation is superseded later, and Heartbeat, the write marker,
//     and Ack refuse a superseded generation, so a supersession after this
//     commit makes the re-projection a no-op.
//
// $1 now, $2 scope_id, $3 active generation_id, $4 freshness hint,
// $5 source commit, $6 reconcile reason. It returns one row: the stamped
// generation_id (empty when fenced) and whether the work row was re-opened.
const reprojectActiveGenerationQuery = `
WITH target AS (
    SELECT generation.generation_id
    FROM scope_generations AS generation
    JOIN ingestion_scopes AS scope
      ON scope.scope_id = generation.scope_id
     AND scope.active_generation_id = generation.generation_id
    WHERE generation.scope_id = $2
      AND generation.generation_id = $3
      AND generation.status = 'active'
      AND generation.activated_at IS NOT NULL
      AND COALESCE(generation.freshness_hint, '') = $4
      AND COALESCE(generation.source_commit_sha, '') = $5
),
stamped AS (
    UPDATE scope_generations AS generation
    SET payload = COALESCE(generation.payload, '{}'::jsonb) || jsonb_build_object(
            'reconcile_reason', $6::text,
            'reconcile_requested_at', $1::timestamptz
        )
    FROM target
    WHERE generation.generation_id = target.generation_id
      AND generation.status = 'active'
    RETURNING generation.generation_id
),
reopened AS (
    INSERT INTO fact_work_items (
        work_item_id, scope_id, generation_id, stage, domain, status,
        attempt_count, lease_owner, claim_until, visible_at, last_attempt_at,
        next_attempt_at, failure_class, failure_message, failure_details,
        payload, created_at, updated_at
    )
    SELECT
        'projector_' || $2 || '_' || stamped.generation_id, $2, stamped.generation_id,
        'projector', 'source_local', 'pending', 0, NULL, NULL, $1, NULL, NULL,
        NULL, NULL, NULL, jsonb_build_object('reconcile_reprojection_count', 1), $1, $1
    FROM stamped
    ON CONFLICT (work_item_id) DO UPDATE
    SET status = 'pending',
        lease_owner = NULL,
        claim_until = NULL,
        visible_at = EXCLUDED.visible_at,
        next_attempt_at = NULL,
        failure_class = NULL,
        failure_message = NULL,
        failure_details = NULL,
        payload = jsonb_set(
            COALESCE(fact_work_items.payload, '{}'::jsonb),
            '{reconcile_reprojection_count}',
            to_jsonb(COALESCE((fact_work_items.payload ->> 'reconcile_reprojection_count')::int, 0) + 1)
        ),
        updated_at = EXCLUDED.updated_at
    WHERE NOT (
        fact_work_items.status IN ('pending', 'retrying')
        OR (
            fact_work_items.status IN ('claimed', 'running')
            AND fact_work_items.claim_until > $1
        )
    )
    RETURNING work_item_id
)
SELECT COALESCE((SELECT generation_id FROM stamped), ''), EXISTS (SELECT 1 FROM reopened)
`

// unchangedGenerationCandidate is the generation a snapshot is compared with.
type unchangedGenerationCandidate struct {
	generationID  string
	freshnessHint string
	sourceCommit  string
	status        scope.GenerationStatus
}

// matches reports whether generation duplicates the candidate (#7978): the
// same non-empty freshness hint, which folds the collector, parser, and
// fact-schema version, and the same source commit.
func (c unchangedGenerationCandidate) matches(generation scope.ScopeGeneration) bool {
	hint := strings.TrimSpace(generation.FreshnessHint)
	return hint != "" && hint == c.freshnessHint &&
		strings.TrimSpace(generation.SourceCommitSHA) == c.sourceCommit
}

// unchangedGeneration returns the generation an incoming snapshot is compared
// with, or false when the scope has none with a freshness hint.
func (s IngestionStore) unchangedGeneration(ctx context.Context, scopeID string) (unchangedGenerationCandidate, bool, error) {
	if s.database == nil || strings.TrimSpace(scopeID) == "" {
		return unchangedGenerationCandidate{}, false, nil
	}
	rows, err := s.database.QueryContext(ctx, unchangedGenerationCandidateQuery, scopeID)
	if err != nil {
		return unchangedGenerationCandidate{}, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return unchangedGenerationCandidate{}, false, rows.Err()
	}
	var candidate unchangedGenerationCandidate
	var status string
	if err := rows.Scan(&candidate.generationID, &candidate.freshnessHint, &candidate.sourceCommit, &status); err != nil {
		return unchangedGenerationCandidate{}, false, err
	}
	candidate.freshnessHint = strings.TrimSpace(candidate.freshnessHint)
	candidate.sourceCommit = strings.TrimSpace(candidate.sourceCommit)
	candidate.status = scope.GenerationStatus(status)
	return candidate, true, rows.Err()
}

// skipUnchangedGeneration decides, before any write, whether a snapshot
// duplicates the scope's current generation (#7978). It returns true when the
// snapshot was handled without storing a generation: the fact stream is then
// drained and the skip recorded. A default-branch change (#7678) is never a
// duplicate. A reconciliation snapshot that matches the active generation
// re-opens that generation's projector work, so the sweep's drift retraction
// (#2340) and graph_dirty repair (#7389) still happen without a new
// generation, fact, document, or term row.
func (s IngestionStore) skipUnchangedGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	requireClaimFence bool,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	factStream <-chan facts.Envelope,
	factStreamErr func() error,
) (bool, error) {
	if generation.DefaultBranchChanged || strings.TrimSpace(generation.FreshnessHint) == "" {
		return false, nil
	}
	candidate, ok, err := s.unchangedGeneration(ctx, scopeValue.ScopeID)
	if err != nil {
		return true, errors.Join(
			fmt.Errorf("check active generation freshness: %w", err),
			drainFactsAndCheckStream(factStream, factStreamErr),
		)
	}
	if !ok || !candidate.matches(generation) {
		return false, nil
	}
	if err := drainFactsAndCheckStream(factStream, factStreamErr); err != nil {
		return true, fmt.Errorf("read fact stream: %w", err)
	}
	reason := telemetry.RefreshSkipReasonUnchanged
	if strings.TrimSpace(generation.ReconcileReason) != "" && candidate.status == scope.GenerationStatusActive {
		reason, err = s.reprojectActiveGeneration(ctx, mutation, requireClaimFence, scopeValue, generation, candidate)
		if err != nil {
			return true, fmt.Errorf("re-project active generation: %w", err)
		}
	}
	s.recordSkippedRefresh(ctx, scopeValue, generation, candidate.generationID, reason)
	return true, nil
}

// reprojectActiveGeneration runs reprojectActiveGenerationQuery under the
// scope lock and the workflow claim fence, and returns the closed skip reason.
func (s IngestionStore) reprojectActiveGeneration(
	ctx context.Context,
	mutation workflow.ClaimMutation,
	requireClaimFence bool,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	candidate unchangedGenerationCandidate,
) (string, error) {
	if s.beginner == nil {
		return "", errors.New("transaction beginner is required")
	}
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return "", fmt.Errorf("begin re-projection transaction: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if requireClaimFence {
		mutation.ObservedAt = s.now()
		if err := NewWorkflowControlStore(tx).HeartbeatClaim(ctx, mutation); err != nil {
			return "", fmt.Errorf("verify active workflow claim before re-projection: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, lockReprojectScopeQuery, scopeValue.ScopeID); err != nil {
		return "", fmt.Errorf("lock scope for re-projection: %w", err)
	}
	rows, err := tx.QueryContext(ctx, reprojectActiveGenerationQuery,
		s.now(), scopeValue.ScopeID, candidate.generationID, candidate.freshnessHint,
		candidate.sourceCommit, strings.TrimSpace(generation.ReconcileReason))
	if err != nil {
		return "", fmt.Errorf("re-open active generation projector work: %w", err)
	}
	var stamped string
	var reopened bool
	if rows.Next() {
		err = rows.Scan(&stamped, &reopened)
	}
	err = errors.Join(err, rows.Err(), rows.Close())
	if err != nil {
		return "", fmt.Errorf("read re-open result: %w", err)
	}
	if stamped == "" {
		return telemetry.RefreshSkipReasonReconcileSuperseded, nil
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("commit re-projection: %w", err)
	}
	committed = true
	if reopened {
		return telemetry.RefreshSkipReasonReconcileReprojected, nil
	}
	return telemetry.RefreshSkipReasonReconcileReprojectionPending, nil
}

// recordSkippedRefresh counts one skip by its closed reason and emits the
// operator log line and a span event on the caller's span.
func (s IngestionStore) recordSkippedRefresh(
	ctx context.Context,
	scopeValue scope.IngestionScope,
	generation scope.ScopeGeneration,
	matchedGenerationID string,
	reason string,
) {
	telemetry.RecordSkippedRefresh(reason)
	trace.SpanFromContext(ctx).AddEvent("ingestion.refresh_skipped", trace.WithAttributes(
		attribute.String(telemetry.LogKeyRefreshSkipReason, reason),
		attribute.String(telemetry.LogKeyScopeID, scopeValue.ScopeID),
		attribute.String("matched_generation_id", matchedGenerationID),
	))
	logger := s.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.InfoContext(ctx, "ingestion refresh skipped",
		slog.Bool(telemetry.LogKeyRefreshSkipped, true),
		slog.String(telemetry.LogKeyRefreshSkipReason, reason),
		slog.String(telemetry.LogKeyScopeID, scopeValue.ScopeID),
		slog.String(telemetry.LogKeyScopeKind, string(scopeValue.ScopeKind)),
		slog.String(telemetry.LogKeySourceSystem, scopeValue.SourceSystem),
		slog.String(telemetry.LogKeyCollectorKind, string(scopeValue.CollectorKind)),
		slog.String(telemetry.LogKeyGenerationID, generation.GenerationID),
		slog.String("matched_generation_id", matchedGenerationID),
		slog.String("reconcile_reason", generation.ReconcileReason),
		telemetry.PhaseAttr(telemetry.PhaseEmission),
	)
}
