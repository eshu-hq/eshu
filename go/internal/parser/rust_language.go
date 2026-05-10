package parser

import (
	"os"
	"path/filepath"
	"slices"

	rustparser "github.com/eshu-hq/eshu/go/internal/parser/rust"
)

func (e *Engine) parseRust(
	repoRoot string,
	path string,
	isDependency bool,
	options Options,
) (map[string]any, error) {
	parser, err := e.runtime.Parser("rust")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	payload, err := rustparser.Parse(path, isDependency, sharedOptions(options), parser)
	if err != nil {
		return nil, err
	}
	enrichRustModuleResolution(payload, repoRoot, path)
	return payload, nil
}

func (e *Engine) preScanRust(path string) ([]string, error) {
	parser, err := e.runtime.Parser("rust")
	if err != nil {
		return nil, err
	}
	defer parser.Close()

	names, err := rustparser.PreScan(path, parser)
	if err != nil {
		return nil, err
	}
	slices.Sort(names)
	return names, nil
}

// enrichRustModuleResolution annotates direct Rust module rows with bounded
// filesystem evidence when ParsePath has a repo root and current file path.
func enrichRustModuleResolution(payload map[string]any, repoRoot string, currentFile string) {
	modules, ok := payload["modules"].([]map[string]any)
	if !ok {
		return
	}
	for _, module := range modules {
		resolution := rustparser.ResolveModuleRowFileCandidates(currentFile, module)
		if len(resolution.Blockers) > 0 {
			module["module_resolution_status"] = "blocked"
			continue
		}
		if len(resolution.CandidatePaths) == 0 {
			continue
		}
		module["resolved_path_candidates"] = rustRepoRelativePaths(repoRoot, resolution.CandidatePaths)
		resolvedPath := rustFirstExistingPath(resolution.CandidatePaths)
		if resolvedPath == "" {
			module["module_resolution_status"] = "unresolved"
			continue
		}
		if rel := rustRepoRelativePath(repoRoot, resolvedPath); rel != "" {
			module["resolved_path"] = rel
		}
		module["module_resolution_status"] = "resolved"
	}
}

// rustFirstExistingPath returns the first candidate present on disk.
func rustFirstExistingPath(paths []string) string {
	for _, path := range paths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	return ""
}

// rustRepoRelativePaths converts module candidates to repo-relative slash paths.
func rustRepoRelativePaths(repoRoot string, paths []string) []string {
	values := make([]string, 0, len(paths))
	for _, path := range paths {
		if rel := rustRepoRelativePath(repoRoot, path); rel != "" {
			values = append(values, rel)
		}
	}
	return values
}

// rustRepoRelativePath returns a slash path only when path stays under repoRoot.
func rustRepoRelativePath(repoRoot string, path string) string {
	rel, err := filepath.Rel(repoRoot, path)
	if err != nil || rel == "." || rel == "" || rel == ".." || filepath.IsAbs(rel) {
		return ""
	}
	if len(rel) >= 3 && rel[:3] == "../" {
		return ""
	}
	return filepath.ToSlash(rel)
}
