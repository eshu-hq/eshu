// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/preparse"
	"github.com/eshu-hq/eshu/go/internal/scope"
	log "github.com/eshu-hq/eshu/go/pkg/log"
)

// skipParse runs the pre-parse decision after the fetch (#7978 follow-up,
// ruling a9-143). It reports true when the store re-projected the active
// generation, so the caller selects nothing for repoPath. A resolver without
// preparse.Reprojector, a refusal, or a store error reports false and the
// repository is parsed as before. It also reports the attempt's clean-tree
// observation for the identity stamp: nil when the decision never reached
// the check or the check errored (#8038 item 2).
//
// When the active generation recorded stream drops (#8185), the probe runs
// first: a recorded path that opens now means the next parse would emit facts
// the active generation lacks, so the scope parses instead of skipping. A
// skip that carries still-failing drops counts on the persistent-drops
// counter and logs the recorded set for the operator.
func (b gitDeltaBaseline) skipParse(
	ctx context.Context, logger *slog.Logger, config RepoSyncConfig, token, repoPath string,
	decision reconcileDecision, sourceSHA string, refs []GitRef,
) (bool, *bool) {
	reprojector, ok := b.Resolver.(preparse.Reprojector)
	if b.Preparse == nil || !ok {
		return false, nil
	}
	drops := decision.State.ActiveStreamDroppedPaths
	if decision.State.ActiveStreamDroppedTruncated {
		// The recorded set overflowed its cap: unrecorded drops cannot be
		// re-checked, so the scope parses instead of risking a skip over a
		// healed file. The parse re-drops and re-records as usual.
		return false, nil
	}
	if len(drops) > 0 && !recordedDropsStillFail(repoPath, drops) {
		// A recorded drop opens again: the next parse heals the generation,
		// so the scope parses. Logged so an unexpected parse explains
		// itself; the parse's own outcome is decided and logged as usual.
		if logger != nil {
			logger.InfoContext(ctx, "git_reconcile_parse_skip_refused_drop_returned",
				log.ScopeID(decision.ScopeID),
				slog.String("generation_id", decision.State.ActiveGenerationID),
				slog.String("commit_sha", decision.State.ActiveCommitSHA),
				slog.Any("dropped_paths", scope.NormalizeStreamDroppedPaths(drops)))
		}
		return false, nil
	}
	var observed *bool
	clean := func() (bool, error) {
		isClean, err := gitTreeClean(ctx, config, token, repoPath)
		if err == nil {
			observed = &isClean
		}
		return isClean, err
	}
	skipped := preparse.Run(ctx, preparse.Attempt{
		ScopeID: decision.ScopeID, Reason: decision.Reason, State: decision.State, RemoteCommit: sourceSHA,
		Identity:      preparse.Identity(GenerationContentVersion(), sourceSHA, repositoryDefaultBranch(refs), *b.Preparse),
		HasSubmodules: regularFileExists(filepath.Join(repoPath, ".gitmodules")),
		MaxSkips:      b.ParseMaxSkips, ConfigDigest: preparse.ConfigDigest(*b.Preparse),
		StampWithoutParse: b.StampWithoutParse, StampProven: false, // unproven: no store proof producer yet (#8095)
	}, clean, reprojector,
		preparse.Recorder{Instruments: b.Instruments, Logger: logger})
	if skipped && len(drops) > 0 {
		b.recordSkipWithPersistentDrops(ctx, logger, decision, drops)
	}
	return skipped, observed
}

// recordedDropsStillFail reports whether every recorded stream-drop path
// still fails to open under repoPath (#8185). Any open error counts as still
// failing: the probe asks only whether the drop recurs, and a file that
// cannot be opened drops the same way again. Blank entries are ignored; when
// nothing checkable remains it reports false, so any doubt parses.
func recordedDropsStillFail(repoPath string, relativePaths []string) bool {
	checked := false
	for _, relativePath := range relativePaths {
		if strings.TrimSpace(relativePath) == "" {
			continue
		}
		checked = true
		// #nosec G304 -- opens a recorded repository path under the scan target, not user-supplied input
		file, err := os.Open(filepath.Join(repoPath, filepath.FromSlash(relativePath)))
		if err == nil {
			_ = file.Close()
			return false
		}
	}
	return checked
}

// recordSkipWithPersistentDrops counts one skipped parse that carried the
// active generation's recorded stream drops, and logs the recorded set with
// the scope (#8185). The per-interval rate of the counter is the operator's
// count of scopes in drop debt; the log names which scopes and files.
func (b gitDeltaBaseline) recordSkipWithPersistentDrops(
	ctx context.Context, logger *slog.Logger, decision reconcileDecision, drops []string,
) {
	paths := scope.NormalizeStreamDroppedPaths(drops)
	if b.Instruments != nil && b.Instruments.ReconcileParseSkipPersistentDrops != nil {
		b.Instruments.ReconcileParseSkipPersistentDrops.Add(ctx, 1)
	}
	if logger == nil {
		return
	}
	logger.InfoContext(ctx, "git_reconcile_parse_skipped_with_persistent_drops",
		log.ScopeID(decision.ScopeID),
		slog.String("generation_id", decision.State.ActiveGenerationID),
		slog.String("commit_sha", decision.State.ActiveCommitSHA),
		slog.Int("drop_count", len(paths)),
		slog.Any("dropped_paths", paths))
}

// snapshotIdentity returns the pre-parse identity a full snapshot of repoPath
// at sourceSHA will carry, or an empty string when the feature is off or the
// checkout is not clean (the identity describes the commit's tree only).
// knownClean reuses the skip attempt's observation for the same cycle
// instead of running git status again; nil checks the tree itself. Nothing
// touches the checkout between the attempt and the stamp.
func (b gitDeltaBaseline) snapshotIdentity(ctx context.Context, config RepoSyncConfig, token, repoPath, sourceSHA string, refs []GitRef, knownClean *bool) string {
	if b.Preparse == nil {
		return ""
	}
	if knownClean != nil {
		if !*knownClean {
			return ""
		}
	} else if clean, err := gitTreeClean(ctx, config, token, repoPath); err != nil || !clean {
		return ""
	}
	return preparse.Identity(GenerationContentVersion(), sourceSHA, repositoryDefaultBranch(refs), *b.Preparse)
}
