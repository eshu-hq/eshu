package javascript

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
