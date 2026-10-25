// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"path/filepath"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// GoModulePath reports the module path declared by the go.mod in repoRoot,
// or "" when no readable go.mod names a module. See issue #6306 for the
// bare-CR normalization this read depends on.
func GoModulePath(repoRoot string) string {
	body, err := shared.ReadFileCapped(filepath.Join(repoRoot, "go.mod")) // #nosec G304 -- reads go.mod at a path derived from the scan target repo root; capped at shared.SourceReadCap (#8986)
	if err != nil {
		return ""
	}
	// go.mod is read here directly, not through shared.ReadSource, so it does
	// not inherit that boundary's bare-CR normalization and needs its own. A
	// classic-Mac go.mod would otherwise collapse into one line, Fields would
	// see far more than two, and the module path would come back empty --
	// silently costing every Go package in the module its import path
	// (issue #6306).
	for _, line := range strings.Split(string(shared.NormalizeLineEndings(body)), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}

// GoImportPathForNearestModule resolves packageDir to its Go import path by
// walking up to the nearest enclosing module, caching go.mod reads in
// modulePathsByDir.
func GoImportPathForNearestModule(repoRoot string, packageDir string, modulePathsByDir map[string]string) (string, bool) {
	resolvedRepoRoot := filepath.Clean(repoRoot)
	dir := filepath.Clean(packageDir)
	for {
		rel, err := filepath.Rel(resolvedRepoRoot, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return "", false
		}
		modulePath, ok := modulePathsByDir[dir]
		if !ok {
			modulePath = GoModulePath(dir)
			modulePathsByDir[dir] = modulePath
		}
		if modulePath != "" {
			return goImportPathForDir(dir, modulePath, packageDir)
		}
		if dir == resolvedRepoRoot {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

func goImportPathForDir(repoRoot string, modulePath string, packageDir string) (string, bool) {
	rel, err := filepath.Rel(repoRoot, packageDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	if rel == "." {
		return modulePath, true
	}
	return modulePath + "/" + filepath.ToSlash(rel), true
}
