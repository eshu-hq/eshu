package collector

import (
	"context"
	"log/slog"
	"path/filepath"

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
) (bool, GitSyncDelta, error) {
	baselineSHA, err := baseline.resolveScopeBaseline(ctx, config, repoPath)
	if err != nil {
		if logger != nil {
			logger.WarnContext(ctx, "git_delta_baseline_lookup_failed",
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
// projected state. Git sync diffs against this commit instead of the local
// working-copy HEAD so a projection that failed after a checkout advanced HEAD
// cannot silently skip its changes (epic #2340). A nil resolver disables the
// baseline lookup and every git update degrades to a safe full snapshot.
type DeltaBaselineResolver interface {
	LastProjectedCommitSHA(ctx context.Context, scopeID string) (string, error)
}

// gitDeltaBaseline carries the optional collaborators the git sync uses to
// resolve and observe delta baselines. Both fields are optional: a zero value
// keeps the legacy full-snapshot behavior with no telemetry.
type gitDeltaBaseline struct {
	Resolver    DeltaBaselineResolver
	Instruments *telemetry.Instruments
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
