// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

var (
	javaScriptStaticStringVarRe = regexp.MustCompile(`\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*["']([^"']+)["']`)
	javaScriptStaticObjectRe    = regexp.MustCompile(`(?s)\b(?:const|let|var)\s+([A-Za-z_$][A-Za-z0-9_$]*)\s*=\s*\{(.*?)\}`)
	javaScriptObjectEntryRe     = regexp.MustCompile(`(?:^|,)\s*(?:([A-Za-z_$][A-Za-z0-9_$]*)|["']([^"']+)["'])\s*:\s*([A-Za-z_$][A-Za-z0-9_$]*(?:\.[A-Za-z_$][A-Za-z0-9_$]*)*)`)
	javaScriptDestructureRe     = regexp.MustCompile(`(?s)\b(?:const|let|var)\s*\{(.*?)\}\s*=\s*([A-Za-z_$][A-Za-z0-9_$]*)`)
	javaScriptDestructureItemRe = regexp.MustCompile(`(?:^|,)\s*([A-Za-z_$][A-Za-z0-9_$]*)\s*(?::\s*([A-Za-z_$][A-Za-z0-9_$]*))?`)
)

// JavaScriptAliasSet is the cached result of scanning a JavaScript/TypeScript
// function's source for static local-name aliases and string-literal
// variables. It is obtained through [EntityIndex.JavaScriptAliasesByPath] or
// [JavaScriptStaticAliases]; the javascript leaf's dynamic-call resolver
// reads it to normalize otherwise-dynamic call expressions.
type JavaScriptAliasSet struct {
	Aliases       map[string]string
	StaticStrings map[string]string
	Scanned       bool
}

func codeCallJavaScriptSourceFile(fileData map[string]any, rawPath string, relativePath string) bool {
	language := payloadcore.AnyToString(fileData["language"])
	if language == "" {
		language = payloadcore.AnyToString(fileData["lang"])
	}
	if JavaScriptFamily(language) {
		return true
	}
	switch strings.ToLower(filepath.Ext(PreferredPath(rawPath, relativePath))) {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		return true
	default:
		return false
	}
}

func cacheJavaScriptStaticAliasSpan(
	index EntityIndex,
	pathKeys []string,
	startLine int,
	endLine int,
	source string,
) {
	if len(pathKeys) == 0 || startLine <= 0 || strings.TrimSpace(source) == "" {
		return
	}
	aliases, staticStrings := JavaScriptStaticAliases(source)
	aliasSet := JavaScriptAliasSet{
		Aliases:       aliases,
		StaticStrings: staticStrings,
		Scanned:       true,
	}
	for _, pathKey := range pathKeys {
		if pathKey == "" {
			continue
		}
		index.javaScriptAliasesByPath[pathKey] = append(
			index.javaScriptAliasesByPath[pathKey],
			javaScriptStaticAliasSpan{
				StartLine: startLine,
				EndLine:   endLine,
				Aliases:   aliasSet,
			},
		)
	}
}

// JavaScriptContainingFunctionSource returns the narrowest function's source
// text containing line, or "" when line is non-positive or no function
// contains it.
func JavaScriptContainingFunctionSource(fileData map[string]any, line int) string {
	if line <= 0 {
		return ""
	}
	var source string
	bestWidth := 0
	for _, item := range payloadcore.MapSlice(fileData["functions"]) {
		startLine := PayloadInt(item["line_number"], item["start_line"])
		endLine := PayloadInt(item["end_line"])
		if startLine <= 0 || line < startLine {
			continue
		}
		if endLine < startLine {
			endLine = startLine
		}
		if line > endLine {
			continue
		}
		width := endLine - startLine
		if source == "" || width < bestWidth {
			source = payloadcore.AnyToString(item["source"])
			bestWidth = width
		}
	}
	return source
}

// JavaScriptStaticAliases scans source for static local-name aliases
// (destructured or object-property assignments) and string-literal
// variables.
func JavaScriptStaticAliases(source string) (map[string]string, map[string]string) {
	aliases := make(map[string]string)
	staticStrings := make(map[string]string)
	for _, match := range javaScriptStaticStringVarRe.FindAllStringSubmatch(source, -1) {
		if len(match) == 3 {
			staticStrings[match[1]] = match[2]
		}
	}

	for _, match := range javaScriptStaticObjectRe.FindAllStringSubmatch(source, -1) {
		if len(match) != 3 {
			continue
		}
		objectName := match[1]
		body := match[2]
		for _, entry := range javaScriptObjectEntryRe.FindAllStringSubmatch(body, -1) {
			if len(entry) != 4 {
				continue
			}
			key := entry[1]
			if key == "" {
				key = entry[2]
			}
			target := entry[3]
			if key == "" || target == "" {
				continue
			}
			aliases[objectName+"."+key] = target
		}
	}

	for _, match := range javaScriptDestructureRe.FindAllStringSubmatch(source, -1) {
		if len(match) != 3 {
			continue
		}
		body := match[1]
		objectName := match[2]
		for _, item := range javaScriptDestructureItemRe.FindAllStringSubmatch(body, -1) {
			if len(item) != 3 {
				continue
			}
			key := item[1]
			local := item[2]
			if local == "" {
				local = key
			}
			if target := aliases[objectName+"."+key]; target != "" {
				aliases[local] = target
			}
		}
	}

	return aliases, staticStrings
}
