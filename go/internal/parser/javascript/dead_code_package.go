// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import "github.com/eshu-hq/eshu/go/internal/parser/javascript/project"

func javaScriptPackageFileRootKinds(repoRoot string, path string) []string {
	return project.PackageFileRootKinds(repoRoot, path)
}

func nearestJavaScriptPackageRoot(repoRoot string, path string) (string, bool) {
	return project.NearestPackageRoot(repoRoot, path)
}
