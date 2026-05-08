package parser

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var javaScriptHapiHandlersPathRe = regexp.MustCompile(`handlers\s*:\s*path\.(?:resolve|join)\(\s*__dirname\s*,\s*['"]([^'"]+)['"]\s*\)`)

// javaScriptIsHapiHandlerFile reports whether the current source file sits
// under a Hapi OpenAPI handler directory declared in this repository.
func javaScriptIsHapiHandlerFile(repoRoot string, path string) bool {
	if strings.TrimSpace(repoRoot) == "" || strings.TrimSpace(path) == "" {
		return false
	}
	relativePath, ok := relativeSlashPath(repoRoot, path)
	if !ok || !strings.Contains(relativePath, "/handlers/") {
		return false
	}
	for _, handlerDir := range javaScriptHapiHandlerDirs(repoRoot, path) {
		if path == handlerDir || strings.HasPrefix(path, handlerDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func javaScriptHapiHandlerDirs(repoRoot string, path string) []string {
	serviceRoots := []string{repoRoot}
	if packageRoot, ok := nearestJavaScriptPackageRoot(repoRoot, path); ok {
		serviceRoots = appendUniqueString(serviceRoots, packageRoot)
	}

	candidates := []string{}
	for _, serviceRoot := range serviceRoots {
		candidates = append(candidates,
			filepath.Join(serviceRoot, "server", "init", "plugins", "spec.js"),
			filepath.Join(serviceRoot, "server", "init", "plugins", "spec.ts"),
			filepath.Join(serviceRoot, "server", "init", "plugins", "specs.js"),
			filepath.Join(serviceRoot, "server", "init", "plugins", "specs.ts"),
		)
	}

	dirs := []string{}
	for _, candidate := range candidates {
		body, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		source := string(body)
		if !javaScriptLooksLikeHapiSpecsPlugin(source) {
			continue
		}
		for _, match := range javaScriptHapiHandlersPathRe.FindAllStringSubmatch(source, -1) {
			if len(match) != 2 {
				continue
			}
			resolved := filepath.Clean(filepath.Join(filepath.Dir(candidate), match[1]))
			dirs = appendUniqueString(dirs, resolved)
		}
	}
	return dirs
}

func javaScriptLooksLikeHapiSpecsPlugin(source string) bool {
	normalized := strings.ToLower(source)
	return strings.Contains(normalized, "openapi")
}
