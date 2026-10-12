// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/preparse"
	"github.com/eshu-hq/eshu/go/internal/parser/scip"
	"github.com/eshu-hq/eshu/go/internal/telemetry"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

// effectiveSnapshotParseWorkers reports the actual parser worker cardinality
// when the zero-value configuration falls back to the sequential parser path.
func effectiveSnapshotParseWorkers(configured int) int {
	if configured <= 1 {
		return 1
	}
	return configured
}

func (s NativeRepositorySnapshotter) engine() (*parser.Engine, error) {
	if s.Engine != nil {
		return s.Engine, nil
	}
	return parser.DefaultEngine()
}

func (s NativeRepositorySnapshotter) registry() parser.Registry {
	if len(s.Registry.ParserKeys()) > 0 {
		return s.Registry
	}
	return parser.DefaultRegistry()
}

// gitTreeClean reports whether the managed checkout has no untracked or
// modified files: discovery walks the filesystem, so residue would be parsed
// although the commit does not cover it (#7978 follow-up, ruling a9-143).
func gitTreeClean(ctx context.Context, config RepoSyncConfig, token, repoPath string) (bool, error) {
	out, err := gitRun(ctx, repoPath, config, token, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) == "", nil
}

// regularFileExists reports whether path is an existing regular file.
func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// snapshotDegradationKey carries a *snapshotDegradation through one snapshot.
type snapshotDegradationKey struct{}

// snapshotDegradation records the first reason a snapshot degraded in a way a
// later identical run may not repeat (review F-2, ruling a9-143.3): a SCIP
// fallback (binary_missing, indexer_failed, parse_failed) or a file the parse
// could not read. Such a snapshot stamps no pre-parse identity, so the next
// sweep parses again. The SCIP and parse workers mark it concurrently.
type snapshotDegradation struct {
	once   sync.Once
	reason telemetry.PreparseIdentityDropReason
	set    atomic.Bool
}

// withSnapshotDegradation returns ctx carrying a fresh degradation note.
func withSnapshotDegradation(ctx context.Context) (context.Context, *snapshotDegradation) {
	note := &snapshotDegradation{}
	return context.WithValue(ctx, snapshotDegradationKey{}, note), note
}

// markSnapshotDegraded records reason on ctx's note; the first reason wins. A
// ctx without a note (a caller outside snapshotOneRepository) is a no-op.
func markSnapshotDegraded(ctx context.Context, reason telemetry.PreparseIdentityDropReason) {
	note, _ := ctx.Value(snapshotDegradationKey{}).(*snapshotDegradation)
	if note == nil {
		return
	}
	note.once.Do(func() {
		note.reason = reason
		note.set.Store(true)
	})
}

// markSnapshotDegradedOn marks ctx's note with reason when a snapshot-phase
// helper skipped readFailures files it could not read (#7978 review F7-1).
func markSnapshotDegradedOn(ctx context.Context, readFailures int, reason telemetry.PreparseIdentityDropReason) {
	if readFailures > 0 {
		markSnapshotDegraded(ctx, reason)
	}
}

// degraded reports whether the snapshot degraded and the first reason. Call it
// after the snapshot returned, when no worker can still mark the note.
func (n *snapshotDegradation) degraded() (telemetry.PreparseIdentityDropReason, bool) {
	if !n.set.Load() {
		return "", false
	}
	return n.reason, true
}

// identityDrop is one reason the git collector kept the pre-parse identity
// off a generation (#7978 review F-2, F-7): the snapshot degraded, or the fact
// stream could not read a file between the parse and the emit and skipped it.
type identityDrop struct {
	Reason       telemetry.PreparseIdentityDropReason
	RepositoryID string
	ScopeID      string
	GenerationID string
	// RelativePath and Err are set for a stream drop: the repository-relative
	// path of the file and the read error.
	RelativePath string
	Err          error
}

// streamDropRecorderKey carries the GitSource that records stream drops.
type streamDropRecorderKey struct{}

// withStreamDropRecorder returns ctx carrying s, so the fact stream, which runs
// after snapshotOneRepository returns, can log and count each drop.
func (s *GitSource) withStreamDropRecorder(ctx context.Context) context.Context {
	return context.WithValue(ctx, streamDropRecorderKey{}, s)
}

// recordStreamDrop records a fact-stream drop through the GitSource ctx
// carries. A ctx without a recorder is a no-op; the stream marks the
// generation either way.
func recordStreamDrop(ctx context.Context, drop identityDrop) {
	if s, _ := ctx.Value(streamDropRecorderKey{}).(*GitSource); s != nil {
		s.recordIdentityDrop(ctx, drop)
	}
}

// recordIdentityDrop counts one identity drop by its normalized closed reason
// and logs WARN git_snapshot_preparse_identity_dropped with the repository,
// scope, and generation ids, and, for a stream drop, the repository-relative
// path and the error without the host path. Snapshot and stream drops share
// this one log shape (review P3-r3-b).
func (s *GitSource) recordIdentityDrop(ctx context.Context, drop identityDrop) {
	reason := telemetry.NormalizePreparseIdentityDropReason(drop.Reason)
	if s.Instruments != nil && s.Instruments.PreparseIdentityDropped != nil {
		s.Instruments.PreparseIdentityDropped.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", string(reason))))
	}
	if s.Logger == nil {
		return
	}
	attrs := []any{
		slog.String("reason", string(reason)),
		slog.String("repository_id", drop.RepositoryID),
		slog.String(telemetry.LogKeyScopeID, drop.ScopeID),
		slog.String(telemetry.LogKeyGenerationID, drop.GenerationID),
	}
	if drop.RelativePath != "" {
		attrs = append(attrs, slog.String("relative_path", drop.RelativePath))
	}
	if drop.Err != nil {
		attrs = append(attrs, slog.String("error", errorWithoutPath(drop.Err)))
	}
	s.Logger.WarnContext(ctx, "git_snapshot_preparse_identity_dropped", attrs...)
}

// errorWithoutPath renders err without the absolute host path an
// *fs.PathError carries, so the log names only the repository-relative path.
func errorWithoutPath(err error) string {
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return pathErr.Op + ": " + pathErr.Err.Error()
	}
	return err.Error()
}

// PreparseConfigFor builds the pre-parse configuration from the same values
// the snapshotter parses with, plus the dependency-mode settings the fact
// builder uses (#7978 follow-up, ruling a9-143). lookPath and stat resolve the
// SCIP indexer binaries; both default to the os/exec and os versions when nil.
// TestPreparseConfigForCoversTheSnapshotter pins that every snapshotter field
// is either folded here or classified as not changing output.
func PreparseConfigFor(
	snapshotter NativeRepositorySnapshotter, sync RepoSyncConfig,
	lookPath func(string) (string, error), stat func(string) (os.FileInfo, error),
) preparse.Config {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	if stat == nil {
		stat = os.Stat
	}
	config := preparse.Config{
		EmitDataflow:       snapshotter.EmitDataflow,
		SCIPEnabled:        snapshotter.SCIP.Enabled,
		DependencyMode:     sync.DependencyMode,
		DependencyName:     sync.DependencyName,
		DependencyLanguage: sync.DependencyLanguage,
		GitlabHost:         effectiveGitlabHost(sync),
	}
	for _, rule := range snapshotter.DiscoveryOptions.IgnoredPathGlobs {
		config.IgnoredPathGlobs = append(config.IgnoredPathGlobs, rule.Pattern+"="+rule.Reason)
	}
	config.PreservedPathGlobs = append(config.PreservedPathGlobs, snapshotter.DiscoveryOptions.PreservedPathGlobs...)
	if !snapshotter.SCIP.Enabled {
		return config
	}
	config.SCIPLanguages = append(config.SCIPLanguages, snapshotter.SCIP.Languages...)
	for _, language := range snapshotter.SCIP.Languages {
		binary, ok := scip.BinaryForLanguage(language)
		if !ok {
			continue
		}
		statResult := preparse.BinaryStat{Language: language, Missing: true}
		if path, err := lookPath(binary); err == nil {
			if info, err := stat(path); err == nil {
				statResult = preparse.BinaryStat{Language: language, Path: path, Size: info.Size(), ModTime: info.ModTime()}
			}
		}
		config.SCIPBinaries = append(config.SCIPBinaries, statResult)
	}
	return config
}
