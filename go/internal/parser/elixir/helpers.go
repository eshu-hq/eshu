// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elixir

import "strings"

// expandAliasPaths expands an Elixir alias target into the list of fully
// qualified module names it introduces. A brace group such as
// `Demo.{Basic, Worker}` expands to `Demo.Basic` and `Demo.Worker`; a plain
// alias returns a single-element slice.
func expandAliasPaths(base string) []string {
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return nil
	}
	openIndex := strings.Index(trimmed, "{")
	closeIndex := strings.Index(trimmed, "}")
	if openIndex < 0 || closeIndex < 0 || closeIndex <= openIndex {
		return []string{trimmed}
	}

	prefix := strings.TrimSpace(trimmed[:openIndex])
	suffix := strings.TrimSpace(trimmed[closeIndex+1:])
	options := splitArgs(trimmed[openIndex+1 : closeIndex])
	expanded := make([]string, 0, len(options))
	for _, option := range options {
		value := strings.TrimSpace(option)
		if value == "" {
			continue
		}
		name := strings.TrimSpace(prefix + value + suffix)
		name = strings.TrimSuffix(name, ".")
		if name != "" {
			expanded = append(expanded, name)
		}
	}
	if len(expanded) == 0 {
		return []string{trimmed}
	}
	return expanded
}

// lastAliasSegment returns the final dotted segment of a module path, used to
// derive the local name introduced by an alias directive.
func lastAliasSegment(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, ".")
	return parts[len(parts)-1]
}

// splitArgs splits a comma-separated argument body into top-level fields while
// respecting nested brackets and string, charlist, and sigil quoting. It backs
// alias brace expansion where the AST exposes the group as one text node.
func splitArgs(body string) []string {
	trimmed := strings.TrimSpace(body)
	if trimmed == "" {
		return []string{}
	}

	args := make([]string, 0)
	current := strings.Builder{}
	depth := 0
	inSingle := false
	inDouble := false
	inBacktick := false

	flush := func() {
		value := strings.TrimSpace(current.String())
		if value != "" {
			args = append(args, value)
		}
		current.Reset()
	}

	for index := 0; index < len(trimmed); index++ {
		char := trimmed[index]
		switch char {
		case '\\':
			current.WriteByte(char)
			if index+1 < len(trimmed) {
				index++
				current.WriteByte(trimmed[index])
			}
			continue
		case '\'':
			if !inDouble && !inBacktick {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle && !inBacktick {
				inDouble = !inDouble
			}
		case '`':
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case '(', '[', '{':
			if !inSingle && !inDouble && !inBacktick {
				depth++
			}
		case ')', ']', '}':
			if !inSingle && !inDouble && !inBacktick && depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 && !inSingle && !inDouble && !inBacktick {
				flush()
				continue
			}
		}
		current.WriteByte(char)
	}
	flush()
	return args
}
