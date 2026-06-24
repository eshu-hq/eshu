// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

const (
	bundlerGemfileName     = "gemfile"
	bundlerLockfileName    = "gemfile.lock"
	bundlerSourceRubyGems  = "rubygems"
	bundlerSourceGit       = "git"
	bundlerSourcePath      = "path"
	bundlerDefaultSection  = "default"
	bundlerLockfileSection = "gemfile.lock"
)

type bundlerDependencyRow struct {
	name              string
	value             string
	line              int
	groups            []string
	lockfile          bool
	sourceType        string
	sourcePath        string
	sourceAmbiguous   bool
	dependencyPath    []string
	dependencyDepth   int
	directDependency  *bool
	dependencySection string
}

func parseBundlerPayload(path string, source []byte, isDependency bool) (map[string]any, bool) {
	switch strings.ToLower(filepath.Base(path)) {
	case bundlerGemfileName:
		return parseBundlerGemfilePayload(path, source, isDependency), true
	case bundlerLockfileName:
		return parseBundlerLockfilePayload(path, source, isDependency), true
	default:
		return nil, false
	}
}

func newBundlerPayload(path string, isDependency bool) map[string]any {
	payload := shared.BasePayload(path, "ruby", isDependency)
	payload["modules"] = []map[string]any{}
	payload["module_inclusions"] = []map[string]any{}
	payload["framework_semantics"] = map[string]any{"frameworks": []string{}}
	return payload
}

func appendBundlerDependency(payload map[string]any, row bundlerDependencyRow) {
	row.name = strings.TrimSpace(row.name)
	if row.name == "" {
		return
	}
	section := row.dependencySection
	if section == "" {
		section = bundlerSectionForGroups(row.groups)
	}
	scope := bundlerScopeForGroups(row.groups)
	if row.lockfile {
		section = bundlerLockfileSection
		scope = bundlerLockfileSection
	}
	item := map[string]any{
		"name":                   row.name,
		"line_number":            row.line,
		"lang":                   "ruby",
		"config_kind":            "dependency",
		"package_manager":        bundlerSourceRubyGems,
		"section":                section,
		"dependency_scope":       scope,
		"development_dependency": bundlerGroupsDevelopment(row.groups),
		"value":                  strings.TrimSpace(row.value),
	}
	if row.lockfile {
		item["lockfile"] = true
	}
	sourceType := strings.TrimSpace(row.sourceType)
	if sourceType == "" {
		sourceType = bundlerSourceRubyGems
	}
	item["source_type"] = sourceType
	if sourcePath := strings.TrimSpace(row.sourcePath); sourcePath != "" {
		item["source_path"] = sourcePath
	}
	if row.sourceAmbiguous || sourceType == bundlerSourceGit || sourceType == bundlerSourcePath {
		item["source_ambiguous"] = true
	}
	if len(row.dependencyPath) > 0 {
		item["dependency_path"] = slices.Clone(row.dependencyPath)
		if row.dependencyDepth == 0 {
			row.dependencyDepth = len(row.dependencyPath)
		}
		item["dependency_depth"] = row.dependencyDepth
	}
	if row.directDependency != nil {
		item["direct_dependency"] = *row.directDependency
	}
	shared.AppendBucket(payload, "variables", item)
}

func bundlerSectionForGroups(groups []string) string {
	groups = normalizeBundlerGroups(groups)
	if len(groups) == 0 {
		return bundlerDefaultSection
	}
	return strings.Join(groups, ",")
}

func bundlerScopeForGroups(groups []string) string {
	groups = normalizeBundlerGroups(groups)
	if len(groups) == 0 {
		return "runtime"
	}
	return strings.Join(groups, ",")
}

func bundlerGroupsDevelopment(groups []string) bool {
	for _, group := range normalizeBundlerGroups(groups) {
		if group == "development" || group == "test" {
			return true
		}
	}
	return false
}

func normalizeBundlerGroups(groups []string) []string {
	seen := make(map[string]struct{}, len(groups))
	out := make([]string, 0, len(groups))
	for _, group := range groups {
		group = strings.Trim(strings.TrimSpace(strings.ToLower(group)), ":\"'")
		if group == "" || group == bundlerDefaultSection {
			continue
		}
		if _, ok := seen[group]; ok {
			continue
		}
		seen[group] = struct{}{}
		out = append(out, group)
	}
	slices.Sort(out)
	return out
}

func bundlerBool(value bool) *bool {
	return &value
}

func stripRubyLineComment(line string) string {
	var quote rune
	escaped := false
	for index, char := range line {
		switch {
		case escaped:
			escaped = false
		case char == '\\' && quote != 0:
			escaped = true
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '#':
			return line[:index]
		}
	}
	return line
}

func bundlerReadQuoted(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", false
	}
	quote := rune(raw[0])
	if quote != '\'' && quote != '"' {
		return "", raw, false
	}
	escaped := false
	for index, char := range raw[1:] {
		switch {
		case escaped:
			escaped = false
		case char == '\\':
			escaped = true
		case char == quote:
			end := index + 1
			return raw[1:end], raw[end+1:], true
		}
	}
	return "", raw, false
}

func bundlerSplitTopLevelCSV(raw string) []string {
	var tokens []string
	start := 0
	depth := 0
	var quote rune
	escaped := false
	for index, char := range raw {
		switch {
		case escaped:
			escaped = false
		case char == '\\' && quote != 0:
			escaped = true
		case quote != 0:
			if char == quote {
				quote = 0
			}
		case char == '\'' || char == '"':
			quote = char
		case char == '[' || char == '{' || char == '(':
			depth++
		case char == ']' || char == '}' || char == ')':
			if depth > 0 {
				depth--
			}
		case char == ',' && depth == 0:
			tokens = append(tokens, strings.TrimSpace(raw[start:index]))
			start = index + 1
		}
	}
	if tail := strings.TrimSpace(raw[start:]); tail != "" {
		tokens = append(tokens, tail)
	}
	return tokens
}
