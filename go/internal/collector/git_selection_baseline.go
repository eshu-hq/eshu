package collector

import (
	"context"
	"log/slog"
	"path/filepath"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// syncExistingRepository resolves the delta baseline for an already-cloned
// managed checkout and updates it. A baseline lookup failure is logged and
// treated as an absent baseline, so the sync falls back to a correct full
// snapshot rather than trusting the local working-copy HEAD as a delta base.
func syncExistingRepository(
	ctx context.Context,
	config RepoSyncConfig,
	repoPath string,
	token string,
	logger *slog.Logger,
	event gitSyncLogEvent,
	baseline gitDeltaBaseline,
	forceReconcile bool,
) (bool, GitSyncDelta, error) {
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
				slog.String("repo_path", repoPath),
				slog.String("error", err.Error()),
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
// scope: the source commit SHA of the most recent generation that reached a
// projected state, and the ingest time of the most recent full projection. Git
// sync diffs against the projected commit instead of the local working-copy HEAD
// so a projection that failed after a checkout advanced HEAD cannot silently
// skip its changes, and forces a periodic full re-observation when a scope has
// gone too long without one (epic #2340). A nil resolver disables both lookups
// and every git update degrades to a safe full snapshot.
type DeltaBaselineResolver interface {
	LastProjectedCommitSHA(ctx context.Context, scopeID string) (string, error)
	LastFullProjectionAt(ctx context.Context, scopeID string) (time.Time, bool, error)
}

// reconcilePolicy bounds the periodic full-snapshot reconciliation sweep. A
// scope is due for reconciliation when it has gone Interval without a projected
// full observation; MaxPerCycle caps how many scopes a single selection cycle
// may force to full so a fleet does not stampede into simultaneous full
// snapshots. A zero Interval disables reconciliation entirely.
type reconcilePolicy struct {
	Interval    time.Duration
	MaxPerCycle int
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

// reconcileDue reports whether the managed checkout at repoPath should be forced
// to a full reconciliation snapshot this cycle: reconciliation must be enabled,
// a resolver must be present, and the scope must have no projected full
// observation within Interval. A lookup error returns false (no reconciliation)
// — the baseline path already degrades safely on resolver errors, so a transient
// outage should not also trigger a fleet of forced full snapshots.
func (b gitDeltaBaseline) reconcileDue(ctx context.Context, config RepoSyncConfig, repoPath string) bool {
	if !b.Reconcile.enabled() || b.Resolver == nil {
		return false
	}
	scopeID := gitScopeIDForManagedRepo(config, repoPath)
	if scopeID == "" {
		return false
	}
	lastFull, ok, err := b.Resolver.LastFullProjectionAt(ctx, scopeID)
	if err != nil {
		return false
	}
	if !ok {
		return true
	}
	return b.now().Sub(lastFull.UTC()) >= b.Reconcile.Interval
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

// recordReconciliation emits the bounded reconciliation counter so operators can
// see how often the periodic sweep forces a full re-observation to retract drift
// the delta path could not. Best-effort: a nil Instruments is a no-op.
func (b gitDeltaBaseline) recordReconciliation(ctx context.Context) {
	if b.Instruments == nil || b.Instruments.ReconciliationFullSnapshots == nil {
		return
	}
	b.Instruments.ReconciliationFullSnapshots.Add(ctx, 1)
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
	return buildScope(metadata).ScopeID
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
