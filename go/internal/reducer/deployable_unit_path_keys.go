package reducer

import (
	"path/filepath"
	"strings"
)

var dockerfileVariantWrapperDirs = map[string]struct{}{
	"container":  {},
	"containers": {},
	"docker":     {},
}

var dockerfilePackagingVariantDirs = map[string]struct{}{
	"debug":      {},
	"dev":        {},
	"local":      {},
	"preview":    {},
	"prod":       {},
	"production": {},
	"qa":         {},
	"remote":     {},
	"sandbox":    {},
	"stage":      {},
	"staging":    {},
	"test":       {},
}

func dockerfileExactPathKey(repoName, relativePath string) string {
	cleanPath := filepath.ToSlash(filepath.Clean(strings.TrimSpace(relativePath)))
	if cleanPath == "" || cleanPath == "." {
		return repoName
	}
	dirPath := filepath.ToSlash(filepath.Dir(cleanPath))
	dir := strings.TrimSpace(filepath.Base(dirPath))
	if dir == "" || dir == "." || dir == "/" {
		return repoName
	}
	if isDockerfilePackagingVariant(dirPath) {
		return repoName
	}
	return dir
}

func isDockerfilePackagingVariant(dirPath string) bool {
	dirPath = strings.Trim(filepath.ToSlash(filepath.Clean(strings.TrimSpace(dirPath))), "/")
	if dirPath == "" || dirPath == "." {
		return false
	}
	segments := strings.Split(dirPath, "/")
	last := strings.ToLower(strings.TrimSpace(segments[len(segments)-1]))
	if _, ok := dockerfileVariantWrapperDirs[last]; ok {
		return true
	}
	if _, ok := dockerfilePackagingVariantDirs[last]; !ok {
		return false
	}
	if len(segments) < 2 {
		return false
	}
	previous := strings.ToLower(strings.TrimSpace(segments[len(segments)-2]))
	_, ok := dockerfileVariantWrapperDirs[previous]
	return ok
}
