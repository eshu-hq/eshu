// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// syncExistingRepository resolves the delta baseline for an already-cloned
// managed checkout and updates it. A baseline lookup failure is logged and
// treated as an absent baseline, so the sync falls back to a correct full
// snapshot rather than trusting the local working-copy HEAD as a delta base.
// When the returned bool is true, the returned string is the remote HEAD SHA
// resolved during this sync (empty otherwise), carried up so the snapshot can
// skip a redundant `git rev-parse HEAD` (#4880).
func syncExistingRepository(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
	baseline gitDeltaBaseline,
	forceReconcile bool,
) (bool, GitSyncDelta, string, error) {
	if forceReconcile {
		// Force a full re-observation regardless of any usable baseline. An empty
		// baseline drives updateRepository's full-snapshot path; the nil
		// onFallback suppresses the baseline-fallback counter so the sweep is not
		// double-counted. The caller records the reconciliation metric only when
		// the forced sync actually produced a generation.
		return updateRepository(ctx, config, repoPath, token, logger, event, "", nil)
	}
	baselineSHA, err := baseline.resolveScopeBaseline(ctx, config, repoPath)
	if err != nil {
		logBaselineLookupFailed(ctx, logger, repoPath, err)
		// Classify the fallback here as a lookup error so a Postgres outage is
		// not miscounted as a fleet of legitimate first syncs, then suppress
		// updateRepository's own emission (nil onFallback) to avoid a double
		// count. The empty baseline still drives a safe full snapshot.
		baseline.recordFallback(ctx, deltaFallbackBaselineLookupError)
		return updateRepository(ctx, config, repoPath, token, logger, event, "", nil)
	}
	onFallback := func(reason string) { baseline.recordFallback(ctx, reason) }
	return updateRepository(ctx, config, repoPath, token, logger, event, baselineSHA, onFallback)
}

// DeltaBaselineResolver resolves the durable incremental-sync baseline for a
// scope and the state the reconciliation sweep decides on. The baseline is the
// source commit SHA of the scope's active generation: git sync diffs against it
// instead of the local working-copy HEAD so a projection that failed after a
// checkout advanced HEAD cannot silently skip its changes. The full reconcile
// state reports the newest activated full generation and the newest full
// generation of any status, so the sweep forces a periodic full re-observation
// without stacking one on top of a full generation still in flight (epic #2340,
// #7288). A nil resolver disables both lookups and every git update degrades to
// a safe full snapshot.
//
// UncoveredProjectionWriters reports the scope's generations that wrote the
// graph, never activated, and are not covered by a later activated full
// generation (#7389); a non-empty result is the graph_dirty reconcile reason.
type DeltaBaselineResolver interface {
	LastProjectedCommitSHA(ctx context.Context, scopeID string) (string, error)
	FullReconcileState(ctx context.Context, scopeID string) (scope.FullReconcileState, error)
	UncoveredProjectionWriters(ctx context.Context, scopeID string) ([]scope.UncoveredProjectionWriter, error)
}

// Delta-baseline fallback reasons: the closed skip_reason label set of
// eshu_dp_collector_delta_baseline_fallback_total.
const (
	// deltaFallbackNoProjectedBaseline: the scope has no active generation.
	deltaFallbackNoProjectedBaseline = "no_projected_baseline"
	// deltaFallbackBaselineUnreachable: the active commit is not in the
	// local history (shallow-clone prune or divergence).
	deltaFallbackBaselineUnreachable = "baseline_unreachable"
	// deltaFallbackBaselineLookupError: the baseline lookup failed.
	deltaFallbackBaselineLookupError = "baseline_lookup_error"
	// deltaFallbackDefaultBranchChanged: the remote default branch changed, so
	// the projected commit belongs to another branch's history (#7678).
	deltaFallbackDefaultBranchChanged = "default_branch_changed"
)

// reconcilePolicyFromConfig lifts the reconciliation knobs off RepoSyncConfig
// into the policy the git sync consumes.
func reconcilePolicyFromConfig(config RepoSyncConfig) reconcilePolicy {
	return reconcilePolicy{
		Interval:    config.ReconcileInterval,
		MaxPerCycle: config.ReconcileMaxPerCycle,
	}
}

// reconcileBudgetRemaining reports whether another scope may be forced to a
// reconciliation snapshot this cycle. A non-positive MaxPerCycle means no
// per-cycle cap (still gated by Interval), so reconciliation is unbounded only
// when an operator opts out of the cap.
func reconcileBudgetRemaining(policy reconcilePolicy, used int) bool {
	if policy.MaxPerCycle <= 0 {
		return true
	}
	return used < policy.MaxPerCycle
}

// gitDeltaBaseline carries the optional collaborators the git sync uses to
// resolve and observe delta baselines and drive reconciliation. All fields are
// optional: a zero value keeps the legacy full-snapshot behavior with no
// telemetry and no reconciliation.
type gitDeltaBaseline struct {
	Resolver    DeltaBaselineResolver
	Instruments *telemetry.Instruments
	Reconcile   reconcilePolicy
	// ReindexRequestedAt is the fleet reindex watermark active for this cycle
	// (#7620), or zero when none is active. See resolveReindexWatermark.
	ReindexRequestedAt time.Time
	// RepositoryReindexRequestedAt holds the per-repository reindex watermarks
	// active for this cycle, keyed by scope ID (#7620). See
	// resolveRepositoryReindexWatermarks.
	RepositoryReindexRequestedAt map[string]time.Time
	Now                          func() time.Time
}

// reindexWatermarkFor returns the reindex watermark that applies to scopeID
// (the later of the fleet watermark and the scope's own entry, zero when
// neither is set) and the reason a forced full records: the repository
// reason only when the scope's own entry is strictly later than the fleet's.
func (b gitDeltaBaseline) reindexWatermarkFor(scopeID string) (time.Time, string) {
	if repository, ok := b.RepositoryReindexRequestedAt[scopeID]; ok && repository.After(b.ReindexRequestedAt) {
		return repository, reconcileReasonRepositoryReindexRequested
	}
	return b.ReindexRequestedAt, reconcileReasonReindexRequested
}

func (b gitDeltaBaseline) now() time.Time {
	if b.Now != nil {
		return b.Now().UTC()
	}
	return time.Now().UTC()
}

// reconcileDecision is the outcome of one reconcileDue evaluation: whether the
// scope is forced to a full snapshot this cycle, the bounded reason, and the
// scope state the decision read, carried to the git_reconcile_forced log.
type reconcileDecision struct {
	Due     bool
	Reason  string
	ScopeID string
	State   scope.FullReconcileState
	// Writers are the uncovered projection writers behind a graph_dirty
	// decision (#7389), carried to the git_delta_baseline_graph_dirty log.
	Writers []scope.UncoveredProjectionWriter
	// ReindexRequestedAt is the watermark a reindex decision compared against
	// (#7620), carried to the git_reconcile_forced log.
	ReindexRequestedAt time.Time
}

// reconcileDue decides whether the managed checkout at repoPath should be
// forced to a full reconciliation snapshot this cycle: reconciliation must be
// enabled or a reindex watermark active for the scope, a resolver must be
// present, and the scope must be due (see decideForScope). The caller only
// asks while the per-cycle budget lasts.
func (b gitDeltaBaseline) reconcileDue(ctx context.Context, config RepoSyncConfig, repoPath string, logger *slog.Logger) reconcileDecision {
	sweepOff := !b.Reconcile.enabled() && b.ReindexRequestedAt.IsZero()
	if b.Resolver == nil || (sweepOff && len(b.RepositoryReindexRequestedAt) == 0) {
		return reconcileDecision{}
	}
	scopeID := gitScopeIDForManagedRepo(config, repoPath)
	if scopeID == "" {
		return reconcileDecision{}
	}
	if _, requested := b.RepositoryReindexRequestedAt[scopeID]; sweepOff && !requested {
		return reconcileDecision{}
	}
	return b.decideForScope(ctx, scopeID, logger)
}

// decideForScope reads the scope's full-generation state and decides. When the
// sweep obligation is not due and no throttle holds it, it probes for #7389
// uncovered projection writers: any writer forces a graph_dirty reconcile,
// subject only to the throttle. A scope still fresh after both is checked
// against the active reindex watermark (#7620, see decideReindex). With
// reconciliation disabled only the watermark is evaluated. A lookup error
// returns not-due: a transient outage must not trigger a fleet of forced
// fulls, and the scope is decided again next cycle. An in-flight or
// retry-backoff suppression increments the bounded suppression counter.
func (b gitDeltaBaseline) decideForScope(ctx context.Context, scopeID string, logger *slog.Logger) reconcileDecision {
	state, err := b.Resolver.FullReconcileState(ctx, scopeID)
	if err != nil {
		return reconcileDecision{ScopeID: scopeID}
	}
	now := b.now()
	decision := reconcileDecision{ScopeID: scopeID, State: state, Reason: reconcileReasonFresh}
	if b.Reconcile.enabled() {
		decision.Due, decision.Reason = b.Reconcile.decide(now, state)
		if !decision.Due && decision.Reason == reconcileReasonFresh {
			writers, err := b.Resolver.UncoveredProjectionWriters(ctx, scopeID)
			switch {
			case err != nil:
				logGraphDirtyLookupFailed(ctx, logger, scopeID, err)
			case len(writers) > 0:
				decision.Writers = writers
				decision.Due, decision.Reason = b.Reconcile.decideGraphDirty(now, state)
				logGraphDirty(ctx, logger, scopeID, writers, decision.Reason)
			}
		}
	}
	if watermark, reason := b.reindexWatermarkFor(scopeID); !decision.Due && decision.Reason == reconcileReasonFresh && !watermark.IsZero() {
		decision.Due, decision.Reason = b.Reconcile.decideReindex(now, watermark, state)
		if decision.Due {
			decision.Reason = reason
		}
		decision.ReindexRequestedAt = watermark
	}
	if decision.Reason == reconcileReasonInFlight || decision.Reason == reconcileReasonRetryBackoff {
		b.recordReconcileSuppressed(ctx, decision.Reason)
	}
	return decision
}

// ReconcileSweepDecision reports whether the git collector's reconciliation
// sweep, including the #7389 graph_dirty reason and the #7620 reindex
// watermarks, would force a full snapshot for scopeID at now with the given
// interval, active fleet watermark, and active per-repository watermark for
// scopeID (each zero when none), and the bounded reason. It runs the same
// decision the sync loop runs, without a per-cycle budget, so an end-to-end
// proof can derive a generation's reconcile flag from production.
func ReconcileSweepDecision(
	ctx context.Context,
	resolver DeltaBaselineResolver,
	interval time.Duration,
	reindexRequestedAt time.Time,
	repositoryReindexRequestedAt time.Time,
	now time.Time,
	scopeID string,
	logger *slog.Logger,
) (bool, string) {
	if resolver == nil || (interval <= 0 && reindexRequestedAt.IsZero() && repositoryReindexRequestedAt.IsZero()) {
		return false, ""
	}
	baseline := gitDeltaBaseline{
		Resolver:           resolver,
		Reconcile:          reconcilePolicy{Interval: interval},
		ReindexRequestedAt: reindexRequestedAt,
		Now:                func() time.Time { return now },
	}
	if !repositoryReindexRequestedAt.IsZero() {
		baseline.RepositoryReindexRequestedAt = map[string]time.Time{scopeID: repositoryReindexRequestedAt}
	}
	decision := baseline.decideForScope(ctx, scopeID, logger)
	return decision.Due, decision.Reason
}

// resolveScopeBaseline returns the last projected commit SHA for the managed
// repository checkout at repoPath, or an empty string when no resolver is
// configured, the scope identity cannot be derived, or no projected generation
// exists yet. A non-nil resolver error is returned so the caller can record it
// and fall back to a full snapshot rather than trusting a stale baseline.
func (b gitDeltaBaseline) resolveScopeBaseline(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
) (string, error) {
	if b.Resolver == nil {
		return "", nil
	}
	scopeID := gitScopeIDForManagedRepo(config, repoPath)
	if scopeID == "" {
		return "", nil
	}
	return b.Resolver.LastProjectedCommitSHA(ctx, scopeID)
}

// logBaselineLookupFailed logs a failed baseline lookup.
func logBaselineLookupFailed(ctx context.Context, logger *slog.Logger, repoPath string, err error) {
	if logger == nil {
		return
	}
	logger.WarnContext(ctx, "git_delta_baseline_lookup_failed", log.RepoPath(repoPath), log.Err(err))
}

// logGraphDirtyLookupFailed logs a failed #7389 uncovered-writer lookup; the
// scope keeps its normal path and is decided again next cycle.
func logGraphDirtyLookupFailed(ctx context.Context, logger *slog.Logger, scopeID string, err error) {
	if logger == nil {
		return
	}
	logger.WarnContext(ctx, "git_delta_baseline_graph_dirty_lookup_failed", log.ScopeID(scopeID), log.Err(err))
}

// logGraphDirty logs the git_delta_baseline_graph_dirty WARN: the generations
// that wrote the graph without activating, and the reconcile decision they
// produced (graph_dirty, or the throttle reason that holds it off).
// Generation ids and timestamps ride only on the log, never a metric label.
func logGraphDirty(ctx context.Context, logger *slog.Logger, scopeID string, writers []scope.UncoveredProjectionWriter, reason string) {
	if logger == nil {
		return
	}
	ids := make([]string, 0, len(writers))
	statuses := make([]string, 0, len(writers))
	classes := make([]string, 0, len(writers))
	started := make([]string, 0, len(writers))
	for _, writer := range writers {
		ids = append(ids, writer.GenerationID)
		statuses = append(statuses, string(writer.Status))
		classes = append(classes, writer.FailureClass)
		started = append(started, writer.ProjectionWriteStartedAt.Format(time.RFC3339Nano))
	}
	logger.WarnContext(ctx, "git_delta_baseline_graph_dirty",
		log.ScopeID(scopeID),
		slog.Any("generation_ids", ids),
		slog.Any("generation_statuses", statuses),
		slog.Any("failure_classes", classes),
		slog.Any("projection_write_started_at", started),
		slog.String("reason", reason),
	)
}

// recordFallback emits the bounded delta-baseline fallback counter so operators
// can watch the rate at which git syncs skip the delta path and re-observe a
// full snapshot. reason is one of the deltaFallback* constants. The metric is best-effort: a nil Instruments is a
// no-op so the sync still runs in instrument-free contexts and tests.
func (b gitDeltaBaseline) recordFallback(ctx context.Context, reason string) {
	if b.Instruments == nil || b.Instruments.DeltaBaselineFallbacks == nil {
		return
	}
	b.Instruments.DeltaBaselineFallbacks.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionSkipReason, reason),
	))
}

// recordReconciliation emits the bounded reconciliation counter, labeled by the
// decision reason, and the git_reconcile_forced log so operators can see how
// often the periodic sweep forces a full re-observation, why, and for which
// scope. scope_id rides only on the log: it is unbounded and never a metric
// label. The log is WARN when the forced full replaces an expired in-flight
// full or retries an unprojected one, since a repeat of either means projection
// is not keeping up. Best-effort: a nil Instruments or logger is a no-op.
func (b gitDeltaBaseline) recordReconciliation(ctx context.Context, logger *slog.Logger, decision reconcileDecision) {
	if b.Instruments != nil && b.Instruments.ReconciliationFullSnapshots != nil {
		b.Instruments.ReconciliationFullSnapshots.Add(ctx, 1, metric.WithAttributes(
			attribute.String(telemetry.MetricDimensionReason, decision.Reason),
		))
	}
	if logger == nil {
		return
	}
	level := slog.LevelInfo
	if decision.Reason == reconcileReasonInFlightExpired || decision.Reason == reconcileReasonRetryAfterUnprojected {
		level = slog.LevelWarn
	}
	attrs := []slog.Attr{
		log.ScopeID(decision.ScopeID),
		slog.String("reason", decision.Reason),
	}
	if decision.State.HasProjectedFull {
		attrs = append(attrs, slog.Time("last_projected_full_at", decision.State.LastProjectedFullAt))
	}
	if decision.State.HasLatestFull {
		attrs = append(attrs,
			slog.Time("latest_full_at", decision.State.LatestFullAt),
			slog.String("latest_full_status", string(decision.State.LatestFullStatus)),
		)
	}
	if !decision.ReindexRequestedAt.IsZero() {
		attrs = append(attrs, slog.Time("reindex_requested_at", decision.ReindexRequestedAt))
	}
	logger.LogAttrs(ctx, level, "git_reconcile_forced", attrs...)
}

// recordReconcileSuppressed emits the bounded suppression counter when the
// sweep holds a due scope off because a full generation is still in flight
// (reconcile_in_flight) or recently failed to project (reconcile_retry_backoff).
// A sustained in-flight rate with no forced reconciles means projection is not
// keeping up. Best-effort: a nil Instruments is a no-op.
func (b gitDeltaBaseline) recordReconcileSuppressed(ctx context.Context, reason string) {
	if b.Instruments == nil || b.Instruments.ReconciliationSuppressed == nil {
		return
	}
	b.Instruments.ReconciliationSuppressed.Add(ctx, 1, metric.WithAttributes(
		attribute.String(telemetry.MetricDimensionReason, reason),
	))
}

// gitScopeIDForManagedRepo derives the ingestion scope ID for a managed git
// checkout the same way the snapshot path does, so a baseline lookup keyed on
// this ID matches the generation the snapshotter persisted. Identity derives
// from the canonical remote URL, so it is stable across checkouts and shallow
// refetches. Returns an empty string when identity cannot be derived.
func gitScopeIDForManagedRepo(config RepoSyncConfig, repoPath string) string {
	scopeID, _ := gitScopeIdentityForManagedRepo(config, repoPath)
	return scopeID
}

// gitScopeIdentityForManagedRepo returns the scope ID and the stored repo_slug
// of the scope gitScopeIDForManagedRepo derives. Both are empty when identity
// cannot be derived; the slug is empty when the checkout has no remote.
func gitScopeIdentityForManagedRepo(config RepoSyncConfig, repoPath string) (scopeID, repoSlug string) {
	absRepoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return "", ""
	}
	managedRepoID := repoIDFromManagedPath(config.ReposDir, absRepoPath)
	remoteURL := repoRemoteURL(config, managedRepoID)
	metadata, err := repositoryidentity.MetadataFor(filepath.Base(absRepoPath), absRepoPath, remoteURL)
	if err != nil {
		return "", ""
	}
	built := buildScope(metadata, "")
	return built.ScopeID, built.Metadata["repo_slug"]
}

// gitScopeIDForRepositoryID derives the default-branch scope ID a git sync of
// repoID writes, from the managed checkout path it would use, without needing
// the checkout to exist. Returns an empty string for an invalid or reserved
// repository identifier.
func gitScopeIDForRepositoryID(config RepoSyncConfig, repoID string) string {
	scopeID, _ := gitScopeIdentityForRepositoryID(config, repoID)
	return scopeID
}

// gitScopeIdentityForRepositoryID returns the scope ID and stored repo_slug a
// git sync of repoID writes, through gitScopeIdentityForManagedRepo.
func gitScopeIdentityForRepositoryID(config RepoSyncConfig, repoID string) (scopeID, repoSlug string) {
	checkoutName, err := repoCheckoutName(repoID)
	if err != nil {
		return "", ""
	}
	return gitScopeIdentityForManagedRepo(config, filepath.Join(config.ReposDir, filepath.FromSlash(checkoutName)))
}

// isGitCommitReachable reports whether sha resolves to a commit object present
// in the local checkout. A baseline that a shallow fetch has pruned (or that a
// diverged local tree never contained) is unreachable; diffing against it would
// be wrong, so the caller falls back to a full snapshot.
func isGitCommitReachable(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	sha string,
) bool {
	_, err := gitRun(ctx, repoPath, config, token, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// notifyDeltaFallback invokes onFallback when it is non-nil, decoupling
// updateRepository from any specific telemetry sink.
func notifyDeltaFallback(onFallback func(reason string), reason string) {
	if onFallback != nil {
		onFallback(reason)
	}
}
