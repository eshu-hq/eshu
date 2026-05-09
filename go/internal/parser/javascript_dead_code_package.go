package parser

import (
	jsparser "github.com/eshu-hq/eshu/go/internal/parser/javascript"
)

func javaScriptPackageFileRootKinds(repoRoot string, path string) []string {
	return jsparser.PackageFileRootKinds(repoRoot, path)
}

func nearestJavaScriptPackageRoot(repoRoot string, path string) (string, bool) {
	return jsparser.NearestPackageRoot(repoRoot, path)
}
