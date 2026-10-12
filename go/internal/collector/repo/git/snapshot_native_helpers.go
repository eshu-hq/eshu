// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"

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
	reason string
	set    atomic.Bool
}

// withSnapshotDegradation returns ctx carrying a fresh degradation note.
func withSnapshotDegradation(ctx context.Context) (context.Context, *snapshotDegradation) {
	note := &snapshotDegradation{}
	return context.WithValue(ctx, snapshotDegradationKey{}, note), note
}

// markSnapshotDegraded records reason on ctx's note; the first reason wins. A
// ctx without a note (a caller outside snapshotOneRepository) is a no-op.
func markSnapshotDegraded(ctx context.Context, reason string) {
	note, _ := ctx.Value(snapshotDegradationKey{}).(*snapshotDegradation)
	if note == nil {
		return
	}
	note.once.Do(func() {
		note.reason = reason
		note.set.Store(true)
	})
}

// degraded reports whether the snapshot degraded and the first reason. Call it
// after the snapshot returned, when no worker can still mark the note.
func (n *snapshotDegradation) degraded() (string, bool) {
	if !n.set.Load() {
		return "", false
	}
	return n.reason, true
}
