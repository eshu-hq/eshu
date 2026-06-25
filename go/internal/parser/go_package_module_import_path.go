// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"os"
	"path/filepath"
	"strings"
)

func goModulePath(repoRoot string) string {
	body, err := os.ReadFile(filepath.Join(repoRoot, "go.mod")) // #nosec G304 -- reads go.mod at a path derived from the scan target repo root
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return strings.TrimSpace(fields[1])
		}
	}
	return ""
}

func goImportPathForNearestModule(repoRoot string, packageDir string, modulePathsByDir map[string]string) (string, bool) {
	resolvedRepoRoot := filepath.Clean(repoRoot)
	dir := filepath.Clean(packageDir)
	for {
		rel, err := filepath.Rel(resolvedRepoRoot, dir)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return "", false
		}
		modulePath, ok := modulePathsByDir[dir]
		if !ok {
			modulePath = goModulePath(dir)
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
