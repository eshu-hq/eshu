// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

var (
	goExecImportPattern = regexp.MustCompile(`(?m)^\s*(?:(?P<alias>[A-Za-z_]\w*)\s+)?["]os/exec["]`)
	goExecCallPattern   = regexp.MustCompile(`\b(?P<alias>[A-Za-z_]\w*)\s*\.\s*(?P<call>CommandContext|Command)\s*\(`)
)

// EmbeddedShellCommand records a Go os/exec command-construction call without
// retaining raw command text or arguments.
type EmbeddedShellCommand struct {
	FunctionName       string
	FunctionLineNumber int
	LineNumber         int
	API                string
	Language           string
}

// EmbeddedShellCommands extracts bounded os/exec Command and CommandContext
// call sites from Go source. It records only structural metadata needed to
// materialize Function-[:EXECUTES_SHELL]->ShellCommand and deliberately omits
// command text, arguments, and environment data.
func EmbeddedShellCommands(source string) []EmbeddedShellCommand {
	aliases := goExecImportAliases(source)
	if len(aliases) == 0 {
		return nil
	}

	var commands []EmbeddedShellCommand
	for _, function := range iterGoFunctionBodies(source) {
		matches := goExecCallPattern.FindAllStringSubmatchIndex(function.body, -1)
		aliasIndex := goExecCallPattern.SubexpIndex("alias")
		callIndex := goExecCallPattern.SubexpIndex("call")
		for _, match := range matches {
			alias := function.body[match[2*aliasIndex]:match[2*aliasIndex+1]]
			if _, ok := aliases[alias]; !ok {
				continue
			}
			if goIdentifierShadowedBeforeOffset(function.body, alias, match[0]) {
				continue
			}
			callName := function.body[match[2*callIndex]:match[2*callIndex+1]]
			commands = append(commands, EmbeddedShellCommand{
				FunctionName:       function.name,
				FunctionLineNumber: function.lineNumber,
				LineNumber:         lineNumberForOffset(source, function.startOffset+match[0]),
				API:                "os/exec." + callName,
				Language:           "go",
			})
		}
	}

	sort.Slice(commands, func(i, j int) bool {
		if commands[i].LineNumber != commands[j].LineNumber {
			return commands[i].LineNumber < commands[j].LineNumber
		}
		return commands[i].API < commands[j].API
	})
	return commands
}

func goExecImportAliases(source string) map[string]struct{} {
	matches := goExecImportPattern.FindAllStringSubmatchIndex(source, -1)
	if len(matches) == 0 {
		return nil
	}
	aliasIndex := goExecImportPattern.SubexpIndex("alias")
	aliases := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		alias := ""
		if aliasIndex >= 0 && match[2*aliasIndex] >= 0 {
			alias = strings.TrimSpace(source[match[2*aliasIndex]:match[2*aliasIndex+1]])
		}
		if alias == "" {
			alias = "exec"
		}
		if alias == "." || alias == "_" {
			continue
		}
		aliases[alias] = struct{}{}
	}
	return aliases
}

// identifierShadowPatterns holds the compiled short-declaration and
// var-declaration regexes for one identifier.
type identifierShadowPatterns struct {
	shortDeclaration *regexp.Regexp
	varDeclaration   *regexp.Regexp
}

// identifierShadowPatternCacheLimit bounds identifierShadowPatternCache so an
// ingester processing a large multi-repo corpus cannot grow the cache
// unboundedly. In practice the identifier is almost always "exec" (the
// default os/exec import name), so this ceiling is far above any realistic
// per-process working set while still bounding worst-case memory.
const identifierShadowPatternCacheLimit = 20_000

// identifierShadowPatternCache caches identifierShadowPatterns per identifier
// so repeated goIdentifierShadowedBeforeOffset calls for the same alias
// reuse the compiled regexes instead of recompiling two patterns per call. A
// *regexp.Regexp is safe for concurrent use, and sync.Map.LoadOrStore makes
// first-compile-per-identifier race-safe. identifierShadowPatternCacheSize is
// a soft bound: concurrent callers racing at the limit may overshoot it
// slightly, which is acceptable for a memory ceiling that only needs to be
// approximately enforced.
var (
	identifierShadowPatternCache     sync.Map // identifier -> identifierShadowPatterns
	identifierShadowPatternCacheSize atomic.Int64
)

func identifierShadowPatternsFor(identifier string) identifierShadowPatterns {
	if cached, ok := identifierShadowPatternCache.Load(identifier); ok {
		return cached.(identifierShadowPatterns)
	}
	compiled := identifierShadowPatterns{
		shortDeclaration: regexp.MustCompile(`\b` + regexp.QuoteMeta(identifier) + `\s*:=`),
		varDeclaration:   regexp.MustCompile(`\bvar\s+` + regexp.QuoteMeta(identifier) + `\b`),
	}
	if identifierShadowPatternCacheSize.Load() >= identifierShadowPatternCacheLimit {
		// Cache is at its bound: fall back to compile-per-call for the long
		// tail of distinct identifiers instead of growing memory unboundedly.
		return compiled
	}
	actual, loaded := identifierShadowPatternCache.LoadOrStore(identifier, compiled)
	if !loaded {
		identifierShadowPatternCacheSize.Add(1)
	}
	return actual.(identifierShadowPatterns)
}

func goIdentifierShadowedBeforeOffset(source string, identifier string, offset int) bool {
	prefix := source[:offset]
	patterns := identifierShadowPatternsFor(identifier)
	return patterns.shortDeclaration.MatchString(prefix) || patterns.varDeclaration.MatchString(prefix)
}
