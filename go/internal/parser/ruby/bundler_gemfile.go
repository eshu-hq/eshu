// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

var (
	bundlerGroupBlockPattern  = regexp.MustCompile(`^group\s+(.+)\s+do\s*$`)
	bundlerSourceBlockPattern = regexp.MustCompile(`^(source|git|path|github)\s+(.+)\s+do\s*$`)
	bundlerGroupValuePattern  = regexp.MustCompile(`:([A-Za-z_]\w*)|['"]([^'"]+)['"]`)
)

type bundlerGemfileContext struct {
	groups     []string
	sourceType string
	sourcePath string
}

func parseBundlerGemfilePayload(path string, source []byte, isDependency bool) map[string]any {
	payload := newBundlerPayload(path, isDependency)
	contexts := make([]bundlerGemfileContext, 0)
	lines := strings.Split(string(source), "\n")

	for index, rawLine := range lines {
		lineNumber := index + 1
		trimmed := strings.TrimSpace(stripRubyLineComment(rawLine))
		if trimmed == "" {
			continue
		}
		if trimmed == "end" {
			if len(contexts) > 0 {
				contexts = contexts[:len(contexts)-1]
			}
			continue
		}
		if context, ok := parseBundlerGemfileContext(trimmed); ok {
			contexts = append(contexts, context)
			continue
		}
		if row, ok := parseBundlerGemfileDependency(trimmed, lineNumber, contexts); ok {
			appendBundlerDependency(payload, row)
			continue
		}
		if rubyStartsOpaqueBlock(trimmed) {
			contexts = append(contexts, bundlerGemfileContext{})
		}
	}

	sharedSortBundlerVariables(payload)
	return payload
}

func parseBundlerGemfileContext(line string) (bundlerGemfileContext, bool) {
	if matches := bundlerGroupBlockPattern.FindStringSubmatch(line); len(matches) == 2 {
		return bundlerGemfileContext{groups: parseBundlerGroupValues(matches[1])}, true
	}
	if matches := bundlerSourceBlockPattern.FindStringSubmatch(line); len(matches) == 3 {
		value, _, ok := bundlerReadQuoted(strings.TrimSpace(matches[2]))
		if !ok || value == "" {
			return bundlerGemfileContext{}, false
		}
		sourceType := bundlerSourceRubyGems
		if matches[1] == bundlerSourceGit || matches[1] == "github" {
			sourceType = bundlerSourceGit
		}
		if matches[1] == bundlerSourcePath {
			sourceType = bundlerSourcePath
		}
		return bundlerGemfileContext{sourceType: sourceType, sourcePath: value}, true
	}
	return bundlerGemfileContext{}, false
}

func parseBundlerGemfileDependency(
	line string,
	lineNumber int,
	contexts []bundlerGemfileContext,
) (bundlerDependencyRow, bool) {
	name, rest, ok := parseBundlerGemCall(line)
	if !ok {
		return bundlerDependencyRow{}, false
	}
	groups, sourceType, sourcePath := bundlerGemfileContextValues(contexts)
	groups = append(groups, parseBundlerGemOptionGroups(rest)...)
	if value, ok := bundlerGemOptionValue(rest, "git", "github"); ok {
		sourceType = bundlerSourceGit
		sourcePath = value
	}
	if value, ok := bundlerGemOptionValue(rest, "path"); ok {
		sourceType = bundlerSourcePath
		sourcePath = value
	}
	if value, ok := bundlerGemOptionValue(rest, "source"); ok &&
		sourceType != bundlerSourceGit &&
		sourceType != bundlerSourcePath {
		sourceType = bundlerSourceRubyGems
		sourcePath = value
	}

	return bundlerDependencyRow{
		name:            name,
		value:           strings.Join(parseBundlerVersionRequirements(rest), ", "),
		line:            lineNumber,
		groups:          groups,
		sourceType:      sourceType,
		sourcePath:      sourcePath,
		sourceAmbiguous: sourceType == bundlerSourceGit || sourceType == bundlerSourcePath,
	}, true
}

func parseBundlerGemCall(line string) (string, string, bool) {
	if !strings.HasPrefix(line, "gem") {
		return "", "", false
	}
	rest := strings.TrimSpace(strings.TrimPrefix(line, "gem"))
	if rest == "" {
		return "", "", false
	}
	if rest[0] != '(' && rest[0] != '\'' && rest[0] != '"' && rest[0] != ' ' && rest[0] != '\t' {
		return "", "", false
	}
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, "("))
	}
	name, rest, ok := bundlerReadQuoted(rest)
	if !ok {
		return "", "", false
	}
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, ",") {
		rest = strings.TrimSpace(strings.TrimPrefix(rest, ","))
	}
	if strings.HasSuffix(rest, ")") {
		rest = strings.TrimSpace(strings.TrimSuffix(rest, ")"))
	}
	return strings.TrimSpace(name), rest, strings.TrimSpace(name) != ""
}

func bundlerGemfileContextValues(contexts []bundlerGemfileContext) ([]string, string, string) {
	groups := make([]string, 0)
	sourceType := bundlerSourceRubyGems
	sourcePath := ""
	for _, context := range contexts {
		groups = append(groups, context.groups...)
		if context.sourceType != "" {
			sourceType = context.sourceType
			sourcePath = context.sourcePath
		}
	}
	return groups, sourceType, sourcePath
}

func parseBundlerVersionRequirements(rest string) []string {
	var versions []string
	for _, token := range bundlerSplitTopLevelCSV(rest) {
		value, _, ok := bundlerReadQuoted(token)
		if !ok {
			continue
		}
		versions = append(versions, strings.TrimSpace(value))
	}
	return versions
}

func parseBundlerGemOptionGroups(rest string) []string {
	var groups []string
	for _, token := range bundlerSplitTopLevelCSV(rest) {
		key, value, ok := parseBundlerOptionToken(token)
		if !ok || (key != "group" && key != "groups") {
			continue
		}
		groups = append(groups, parseBundlerGroupValues(value)...)
	}
	return groups
}

func bundlerGemOptionValue(rest string, keys ...string) (string, bool) {
	for _, token := range bundlerSplitTopLevelCSV(rest) {
		key, value, ok := parseBundlerOptionToken(token)
		if !ok {
			continue
		}
		for _, candidate := range keys {
			if key != candidate {
				continue
			}
			if parsed, _, quoted := bundlerReadQuoted(value); quoted {
				return parsed, true
			}
			value = strings.Trim(strings.TrimSpace(value), ":")
			return value, value != ""
		}
	}
	return "", false
}

func parseBundlerOptionToken(token string) (string, string, bool) {
	token = strings.TrimSpace(token)
	for _, separator := range []string{"=>", ":"} {
		left, right, ok := strings.Cut(token, separator)
		if !ok {
			continue
		}
		key := strings.Trim(strings.TrimSpace(left), ":")
		key = strings.ToLower(key)
		if key == "" {
			return "", "", false
		}
		return key, strings.TrimSpace(right), true
	}
	return "", "", false
}

func parseBundlerGroupValues(raw string) []string {
	matches := bundlerGroupValuePattern.FindAllStringSubmatch(raw, -1)
	groups := make([]string, 0, len(matches))
	for _, match := range matches {
		group := match[1]
		if group == "" {
			group = match[2]
		}
		groups = append(groups, group)
	}
	return normalizeBundlerGroups(groups)
}

func sharedSortBundlerVariables(payload map[string]any) {
	shared.SortNamedBucket(payload, "variables")
}
