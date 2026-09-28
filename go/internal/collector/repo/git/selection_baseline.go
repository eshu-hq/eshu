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
		if logger != nil {
			logger.WarnContext(
				ctx, "git_delta_baseline_lookup_failed",
				log.RepoPath(repoPath),
				log.Err(err),
			)
		}
		// Classify the fallback here as a lookup error so a Postgres outage is
		// not miscounted as a fleet of legitimate first syncs, then suppress
		// updateRepository's own emission (nil onFallback) to avoid a double
		// count. The empty baseline still drives a safe full snapshot.
		baseline.recordFallback(ctx, "baseline_lookup_error")
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
type DeltaBaselineResolver interface {
	LastProjectedCommitSHA(ctx context.Context, scopeID string) (string, error)
	FullReconcileState(ctx context.Context, scopeID string) (scope.FullReconcileState, error)
}

// reconcilePolicy bounds the periodic full-snapshot reconciliation sweep. A
// scope is due for reconciliation when it has gone Interval without a projected
// full observation and no recent full attempt is still in flight or backing off
// (see decide); MaxPerCycle caps how many scopes a single selection cycle may
// force to full so a fleet does not stampede into simultaneous full snapshots.
// A zero Interval disables reconciliation entirely.
type reconcilePolicy struct {
	Interval    time.Duration
	MaxPerCycle int
}

// Reconcile decision reasons. They are a closed set used as the bounded reason
// label on the reconciliation counters and in the git_reconcile_forced log.
const (
	reconcileReasonFresh                 = "fresh"
	reconcileReasonInFlight              = "reconcile_in_flight"
	reconcileReasonInFlightExpired       = "in_flight_expired"
	reconcileReasonRetryBackoff          = "reconcile_retry_backoff"
	reconcileReasonRetryAfterUnprojected = "retry_after_unprojected"
	reconcileReasonNeverReconciled       = "never_reconciled"
	reconcileReasonIntervalElapsed       = "interval_elapsed"
)

// decide reports whether a scope in state s is due for a forced full
// reconciliation snapshot at now, and the bounded reason. It is pure: no clock
// read and no I/O.
//
// The obligation is measured on the last projected (activated) full
// generation; the throttle on the latest full attempt of any status:
//   - a projected full younger than Interval keeps the scope fresh;
//   - a pending full younger than Interval is still in flight, so forcing
//     another full of the same commit would only supersede it; at Interval it
//     stops suppressing and one new full is forced;
//   - a failed, or superseded-before-activation, full holds the sweep off for
//     Interval/4 (retry backoff), then one new full is forced.
//
// A negative age (clock skew) compares as less than every bound and
// suppresses. No single generation suppresses the sweep for longer than
// Interval: the worst case per scope is one forced full per Interval while
// fulls stay pending, and four per Interval while they keep failing.
func (p reconcilePolicy) decide(now time.Time, s scope.FullReconcileState) (bool, string) {
	if s.HasProjectedFull && now.Sub(s.LastProjectedFullAt) < p.Interval {
		return false, reconcileReasonFresh
	}
	if s.HasLatestFull && !s.LatestFullProjected {
		age := now.Sub(s.LatestFullAt)
		if s.LatestFullStatus == scope.GenerationStatusPending {
			if age < p.Interval {
				return false, reconcileReasonInFlight
			}
			return true, reconcileReasonInFlightExpired
		}
		if age < p.Interval/4 {
			return false, reconcileReasonRetryBackoff
		}
		return true, reconcileReasonRetryAfterUnprojected
	}
	if !s.HasProjectedFull {
		return true, reconcileReasonNeverReconciled
	}
	return true, reconcileReasonIntervalElapsed
}

func (p reconcilePolicy) enabled() bool {
	return p.Interval > 0
}

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
	Now         func() time.Time
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
}

// reconcileDue decides whether the managed checkout at repoPath should be
// forced to a full reconciliation snapshot this cycle: reconciliation must be
// enabled, a resolver must be present, and decide must find the scope due. A
// lookup error returns not-due (no reconciliation) — the baseline path already
// degrades safely on resolver errors, so a transient outage should not also
// trigger a fleet of forced full snapshots. An in-flight or retry-backoff
// suppression increments the bounded suppression counter.
func (b gitDeltaBaseline) reconcileDue(ctx context.Context, config RepoSyncConfig, repoPath string) reconcileDecision {
	if !b.Reconcile.enabled() || b.Resolver == nil {
		return reconcileDecision{}
	}
	scopeID := gitScopeIDForManagedRepo(config, repoPath)
	if scopeID == "" {
		return reconcileDecision{}
	}
	state, err := b.Resolver.FullReconcileState(ctx, scopeID)
	if err != nil {
		return reconcileDecision{ScopeID: scopeID}
	}
	due, reason := b.Reconcile.decide(b.now(), state)
	if reason == reconcileReasonInFlight || reason == reconcileReasonRetryBackoff {
		b.recordReconcileSuppressed(ctx, reason)
	}
	return reconcileDecision{Due: due, Reason: reason, ScopeID: scopeID, State: state}
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

// recordFallback emits the bounded delta-baseline fallback counter so operators
// can watch the rate at which git syncs skip the delta path and re-observe a
// full snapshot. reason is a closed enum (no_projected_baseline,
// baseline_unreachable). The metric is best-effort: a nil Instruments is a
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
	absRepoPath, err := filepath.Abs(repoPath)
	if err != nil {
		return ""
	}
	managedRepoID := repoIDFromManagedPath(config.ReposDir, absRepoPath)
	remoteURL := repoRemoteURL(config, managedRepoID)
	metadata, err := repositoryidentity.MetadataFor(filepath.Base(absRepoPath), absRepoPath, remoteURL)
	if err != nil {
		return ""
	}
	return buildScope(metadata, "").ScopeID
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
