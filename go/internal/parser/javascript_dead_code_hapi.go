package parser

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var javaScriptHapiHandlersPathRe = regexp.MustCompile(`handlers\s*:\s*path\.(?:resolve|join)\(\s*__dirname\s*,\s*['"]([^'"]+)['"]\s*\)`)

// javaScriptIsHapiHandlerFile reports whether the current source file sits
// under a lib-api-hapi OpenAPI handler directory declared in this repository.
func javaScriptIsHapiHandlerFile(repoRoot string, path string) bool {
	if strings.TrimSpace(repoRoot) == "" || strings.TrimSpace(path) == "" {
		return false
	}
	relativePath, ok := relativeSlashPath(repoRoot, path)
	if !ok || !strings.Contains(relativePath, "/handlers/") {
		return false
	}
	for _, handlerDir := range javaScriptHapiHandlerDirs(repoRoot) {
		if path == handlerDir || strings.HasPrefix(path, handlerDir+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func javaScriptHapiHandlerDirs(repoRoot string) []string {
	candidates := []string{
		filepath.Join(repoRoot, "server", "init", "plugins", "spec.js"),
		filepath.Join(repoRoot, "server", "init", "plugins", "spec.ts"),
		filepath.Join(repoRoot, "server", "init", "plugins", "specs.js"),
		filepath.Join(repoRoot, "server", "init", "plugins", "specs.ts"),
	}

	dirs := []string{}
	for _, candidate := range candidates {
		body, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		source := string(body)
		if !strings.Contains(source, "@dmm/lib-api-hapi/init/plugins/specs") {
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
