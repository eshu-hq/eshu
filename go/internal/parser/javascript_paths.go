package parser

import (
	"path/filepath"
	"strings"
)

func relativeSlashPath(repoRoot string, path string) (string, bool) {
	relativePath, err := filepath.Rel(repoRoot, path)
	if err != nil || strings.HasPrefix(relativePath, "..") {
		return "", false
	}
	return filepath.ToSlash(filepath.Clean(relativePath)), true
}

func cleanJavaScriptPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(abs)
}

func javaScriptPathWithin(root string, path string) bool {
	root = cleanJavaScriptPath(root)
	path = cleanJavaScriptPath(path)
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}
