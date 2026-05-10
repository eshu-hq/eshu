package rust

import (
	"path/filepath"
	"strings"
)

const rustMacroExpansionUnavailableBlocker = "macro_expansion_unavailable"

// ModuleResolution reports filesystem module candidates and exactness blockers
// derived from a single parser-emitted Rust module row.
type ModuleResolution struct {
	CandidatePaths []string
	Blockers       []string
}

// ResolveModuleRowFileCandidates resolves one parser module row without probing
// the filesystem. Direct declarations use Rust's current-file module anchoring,
// path attributes stay relative to the declaring file directory, inline modules
// have no external files, and macro-origin rows remain blocked.
func ResolveModuleRowFileCandidates(currentFile string, row map[string]any) ModuleResolution {
	if len(row) == 0 {
		return ModuleResolution{}
	}

	blockers := rustModuleRowBlockers(row)
	if rustStringField(row, "module_origin") == "macro_invocation" {
		blockers = appendUniqueString(blockers, rustMacroExpansionUnavailableBlocker)
		return ModuleResolution{Blockers: blockers}
	}
	if containsString(blockers, rustMacroExpansionUnavailableBlocker) {
		return ModuleResolution{Blockers: blockers}
	}

	if rustStringField(row, "module_kind") != "declaration" {
		return ModuleResolution{Blockers: blockers}
	}

	baseDir := filepath.Dir(currentFile)
	if rustStringField(row, "module_path_source") == "path_attribute" {
		return ModuleResolution{
			CandidatePaths: rustJoinModulePaths(baseDir, rustStringSliceField(row, "declared_path_candidates")),
			Blockers:       blockers,
		}
	}

	name := strings.TrimSpace(rustStringField(row, "name"))
	if name == "" {
		return ModuleResolution{Blockers: blockers}
	}
	moduleDir := rustModuleDeclarationBaseDir(currentFile)
	return ModuleResolution{
		CandidatePaths: []string{
			filepath.Join(moduleDir, name+".rs"),
			filepath.Join(moduleDir, name, "mod.rs"),
		},
		Blockers: blockers,
	}
}

func rustModuleDeclarationBaseDir(currentFile string) string {
	dir := filepath.Dir(currentFile)
	base := filepath.Base(currentFile)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	switch name {
	case "lib", "main", "mod":
		return dir
	default:
		return filepath.Join(dir, name)
	}
}

func rustJoinModulePaths(baseDir string, candidates []string) []string {
	if len(candidates) == 0 {
		return nil
	}
	paths := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		paths = append(paths, filepath.Join(baseDir, candidate))
	}
	return paths
}

func rustModuleRowBlockers(row map[string]any) []string {
	return rustStringSliceField(row, "exactness_blockers")
}

func rustStringField(row map[string]any, key string) string {
	value, _ := row[key].(string)
	return value
}

func rustStringSliceField(row map[string]any, key string) []string {
	switch value := row[key].(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		values := make([]string, 0, len(value))
		for _, item := range value {
			text, ok := item.(string)
			if ok && text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
