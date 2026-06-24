// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package golang

import (
	"regexp"
	"sort"
	"strings"
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

func goIdentifierShadowedBeforeOffset(source string, identifier string, offset int) bool {
	prefix := source[:offset]
	shortDeclaration := regexp.MustCompile(`\b` + regexp.QuoteMeta(identifier) + `\s*:=`)
	varDeclaration := regexp.MustCompile(`\bvar\s+` + regexp.QuoteMeta(identifier) + `\b`)
	return shortDeclaration.MatchString(prefix) || varDeclaration.MatchString(prefix)
}
