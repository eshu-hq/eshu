// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"context"
	"os"
	"strings"

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
