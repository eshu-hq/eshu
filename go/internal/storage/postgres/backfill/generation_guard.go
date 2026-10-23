// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backfill

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// validateGenerationInput checks scope/generation preconditions before
// opening a transaction. Per-fact validation (scope_id, generation_id match)
// happens inside upsertStreamingFacts as facts arrive from the channel.
func ValidateGenerationInput(
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

// RepositoryGenerationIdentity binds a repository to its active scope and
// generation for the deferred relationship pass.
type RepositoryGenerationIdentity struct {
	RepoID       string
	ScopeID      string
	GenerationID string
}

// LoadActiveRepositoryGenerations returns the active (scope_id, generation_id)
// per repository. It filters to fact_kind = 'repository' and therefore excludes
// scopes with no repository fact (for example GCP cloud-relationship scopes); do
// not use it as the partition source for the corpus-wide deferred backfill.
func LoadActiveRepositoryGenerations(
	ctx context.Context,
	queryer db.Queryer,
) (map[string]RepositoryGenerationIdentity, error) {
	if queryer == nil {
		return nil, nil
	}

	rows, err := queryer.QueryContext(ctx, ActiveRepositoryGenerationsQuery)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	result := make(map[string]RepositoryGenerationIdentity)
	for rows.Next() {
		var identity RepositoryGenerationIdentity
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

// LoadAllActiveRepositoryGenerations is the whole pass's batch loader: the
// shipped LoadActiveRepositoryGenerations, which reads every repository.
func LoadAllActiveRepositoryGenerations(
	ctx context.Context,
	queryer db.Queryer,
	_ []string,
) (map[string]RepositoryGenerationIdentity, error) {
	return LoadActiveRepositoryGenerations(ctx, queryer)
}

// ActiveRepositoryGenerationsQuery lists the active generation per repository
// through the shared LatestGenerationCTE.
const ActiveRepositoryGenerationsQuery = LatestGenerationCTE + `
SELECT DISTINCT ON (repo_id)
    repo_id,
    fact.scope_id,
    fact.generation_id
FROM (
    SELECT
        COALESCE(
            fact.payload->>'repo_id',
            fact.payload->>'graph_id',
            fact.payload->>'name',
            ''
        ) AS repo_id,
        fact.scope_id,
        fact.generation_id,
        fact.observed_at,
        fact.fact_id
    FROM fact_records AS fact
    JOIN latest_generations AS latest
      ON latest.scope_id = fact.scope_id
     AND latest.generation_id = fact.generation_id
    WHERE fact.fact_kind = 'repository'
) AS fact
WHERE repo_id <> ''
ORDER BY repo_id, observed_at DESC, fact_id DESC
`

// ActiveScopeGenerationQuery resolves ONE scope's currently active generation.
//
// It is the single-scope projection of LatestGenerationCTE, which every
// corpus-wide deferred query joins against: that CTE picks, per scope,
// COALESCE(ingestion_scopes.active_generation_id, generation.generation_id)
// from the scope_generations row ordered by (ingested_at DESC,
// generation_id DESC). Restricting the same expression, join, and ordering to
// one scope_id and taking LIMIT 1 yields exactly the row DISTINCT ON (scope_id)
// would have chosen for that scope. TestFanInActiveGenerationMatchesCorpusLoader
// pins that correspondence against the shipped corpus-wide loader so the two
// cannot drift apart silently.
//
// The fan-in needs a per-scope lookup rather than LoadActiveRepositoryGenerations
// because it runs once per partition. LoadActiveRepositoryGenerations scans
// fact_records for every repository fact in the corpus; at ~910 partitions that
// would be ~910 corpus-wide scans per pass. This lookup is a LIMIT 1 read served
// by scope_generations_scope_latest_lookup_idx
// (scope_id, ingested_at DESC, generation_id DESC) from migration 002.
const ActiveScopeGenerationQuery = `
SELECT COALESCE(scope.active_generation_id, generation.generation_id) AS generation_id
FROM scope_generations AS generation
LEFT JOIN ingestion_scopes AS scope
  ON scope.scope_id = generation.scope_id
WHERE generation.scope_id = $1
ORDER BY generation.ingested_at DESC, generation.generation_id DESC
LIMIT 1
`

// LoadActiveGenerationForScope returns the scope's active generation ID. The
// empty string with a nil error means the scope has no generation row at all,
// which the caller treats the same as an advanced generation: nothing to
// publish.
func LoadActiveGenerationForScope(ctx context.Context, queryer db.Queryer, scopeID string) (string, error) {
	rows, err := queryer.QueryContext(ctx, ActiveScopeGenerationQuery, scopeID)
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

// storeReasonStreamDroppedCandidate is the reason DecideUnchangedGeneration
// gives for storing over a candidate whose fact stream dropped a file
// (ruling a9-143.20). The store logs it as the commit stage
// store_over_stream_dropped_candidate; a store is not a skip, so it has no
// skip-reason metric value.
const storeReasonStreamDroppedCandidate = "stream_dropped_candidate"

// UnchangedGenerationAction is what the store does with an incoming snapshot.
type UnchangedGenerationAction int

const (
	// UnchangedActionStore stores the snapshot as a new generation.
	UnchangedActionStore UnchangedGenerationAction = iota
	// UnchangedActionSkip stores nothing.
	UnchangedActionSkip
	// UnchangedActionReprojectActive stores nothing and re-opens the active
	// generation's projector work.
	UnchangedActionReprojectActive
)

// GenerationNeedsDuplicateCheck reports whether a snapshot can be a duplicate
// at all. A snapshot with no hint cannot match, and a default-branch change
// (#7678) is always stored: its repository fact names another branch.
func GenerationNeedsDuplicateCheck(generation scope.ScopeGeneration) bool {
	return !generation.DefaultBranchChanged && strings.TrimSpace(generation.FreshnessHint) != ""
}

// DecideUnchangedGeneration is the one decision seam for #7978: what the store
// does with a snapshot given the scope's current generation (candidate, found).
// It is pure. Every entry point that forces a full snapshot (the reconcile
// sweep, graph_dirty, reindex watermarks, webhook and scheduled sync) reaches
// it through ScopeGeneration.ReconcileReason, so a new rule is a change here
// and nowhere else.
//
//   - No candidate, or a different hint or commit: store.
//   - Same identity as a pending generation (ruling a9-120): skip while its
//     projector work is live; store when it is not (ruling a9-126).
//   - Same identity as the active generation, ordinary snapshot: skip.
//   - Same identity as the active generation, reconcile reason
//     reconcile_request_stale (ruling a9-117 Q3): store; the re-projection was
//     not acknowledged within one interval, so take today's heal.
//   - Same identity as the active generation, an explicit reindex reason
//     (reindex_requested, repository_reindex_requested): store. It is the
//     operator remedy for a parser change the content-version gate did not
//     catch (ruling a9-125).
//   - Same identity as the active generation, any other reconcile reason:
//     re-project the active generation. Ruling a9-117 Q1 accepts that this
//     does not retract graph state stamped with the active generation id
//     itself; switching a reason back to storing is a change on this line.
func DecideUnchangedGeneration(
	generation scope.ScopeGeneration,
	candidate UnchangedGenerationCandidate,
	found bool,
) (UnchangedGenerationAction, string) {
	if !found || !candidate.Matches(generation) {
		return UnchangedActionStore, ""
	}
	if candidate.StreamDropped {
		// Ruling a9-143.20: the candidate's own stream dropped a file, so an
		// equal snapshot stores a fresh generation, active or pending alike.
		return UnchangedActionStore, storeReasonStreamDroppedCandidate
	}
	if candidate.Status == scope.GenerationStatusPending {
		if !candidate.WorkLive {
			// Ruling a9-126: no worker can activate it; store the snapshot
			// so it supersedes the wedged generation at activation.
			return UnchangedActionStore, ""
		}
		return UnchangedActionSkip, telemetry.RefreshSkipReasonPendingSameIdentity
	}
	switch strings.TrimSpace(generation.ReconcileReason) {
	case "":
		return UnchangedActionSkip, telemetry.RefreshSkipReasonUnchanged
	case scope.ReconcileReasonRequestStale,
		scope.ReconcileReasonReindexRequested, scope.ReconcileReasonRepositoryReindexRequested:
		return UnchangedActionStore, ""
	default:
		return UnchangedActionReprojectActive, telemetry.RefreshSkipReasonReconcileReprojected
	}
}

// RefuseDestructiveSuccessor is the #7926 successor-emptiness backstop: a
// pure predicate over the scope's current generation and the incoming
// successor's fact count. It refuses exactly when storing would retract
// content-bearing truth with no operator attestation behind the deletion:
//
//   - a trusted-deletion signal always stores: the escape hatch for
//     legitimate mass-resolves;
//   - with no prior generation, or a prior generation that holds no facts,
//     there is nothing to retract, so the successor stores;
//   - an empty successor over a content-bearing prior is refused;
//   - a successor holding less than half the prior's facts is a sharp
//     shrink and is refused; exactly half or milder drift stores.
//
// Counts must be like for like (#8750): the successor's streamed envelopes
// against the prior's collector rows, never the prior's reducer-derived rows,
// which no collector stream carries. A delta successor is not sized here at
// all; DecideSuccessor sizes it by its projected file union. The comparison
// is deliberately coarse — it is a backstop against provider outages and
// broken queries wiping a scope, not a precision drift detector. A refusal
// keeps the prior generation active; the caller records it under
// telemetry.RefreshSkipReasonDestructiveSuccessor and commits nothing.
func RefuseDestructiveSuccessor(
	priorFound bool,
	priorFacts int,
	successorFacts int,
	trustedDeletion bool,
) (refuse bool, reason string) {
	if trustedDeletion {
		return false, ""
	}
	if !priorFound || priorFacts <= 0 {
		return false, ""
	}
	if successorFacts <= 0 {
		return true, telemetry.RefreshSkipReasonDestructiveSuccessor
	}
	if successorFacts*2 < priorFacts {
		return true, telemetry.RefreshSkipReasonDestructiveSuccessor
	}
	return false, ""
}

// UnchangedGenerationCandidate is the generation a snapshot is compared with.
type UnchangedGenerationCandidate struct {
	GenerationID  string
	FreshnessHint string
	SourceCommit  string
	Status        scope.GenerationStatus
	// WorkLive reports whether the candidate has a projector work row a
	// worker can still run (pending, retrying, claimed, running); ruling
	// a9-126 skips a pending match only then.
	WorkLive bool
	// StreamDropped reports that the candidate's own fact stream dropped a
	// file (#7978 review F7-4, ruling a9-143.20): its facts are incomplete, so
	// an equal snapshot stores a new generation instead of matching it.
	StreamDropped bool
}

// Matches reports whether generation duplicates the candidate (#7978): the
// same non-empty freshness hint, which folds the collector, parser, and
// fact-schema version, and the same source commit.
func (c UnchangedGenerationCandidate) Matches(generation scope.ScopeGeneration) bool {
	hint := strings.TrimSpace(generation.FreshnessHint)
	return hint != "" && hint == c.FreshnessHint &&
		strings.TrimSpace(generation.SourceCommitSHA) == c.SourceCommit
}

// RecordSkippedRefresh counts one skip by its closed reason and emits the
// operator log line and a span event on the caller's span.
func (s Store) RecordSkippedRefresh(
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
