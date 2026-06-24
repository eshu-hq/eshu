// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

func javaScriptPackageFileRootKinds(repoRoot string, path string) []string {
	return PackageFileRootKinds(repoRoot, path)
}

func nearestJavaScriptPackageRoot(repoRoot string, path string) (string, bool) {
	return NearestPackageRoot(repoRoot, path)
}
