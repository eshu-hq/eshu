// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package submodule

import "strings"

// Parse reads one ".gitmodules" file body and returns its declared submodule
// entries in file order.
//
// ".gitmodules" is a git-config file
// (https://git-scm.com/docs/git-config#_syntax): every submodule is declared
// in its own extended section, `[submodule "<name>"]`, followed by
// indented `key = value` lines. Parse implements the subset of that grammar
// this collector needs:
//
//   - Blank lines (including whitespace-only lines) are ignored.
//   - A line whose first non-whitespace character is '#' or ';' is a
//     whole-line comment and is ignored. git-config's inline trailing-comment
//     syntax (a bare '#'/';' after a value on the same line) is out of scope;
//     only whole-line comments are recognized.
//   - A line whose first non-whitespace character is '[' opens a new
//     section, ending whatever section preceded it. Only a section whose
//     name is "submodule" (case-insensitively) and that carries a
//     double-quoted subsection — `[submodule "<name>"]` — is treated as a
//     submodule section; every other section (including a bare
//     `[submodule]` with no subsection, which git-config treats as a
//     different key namespace entirely) is skipped, and no key/value line
//     is read until the next recognized submodule section opens.
//   - Inside a submodule section, a `key = value` line (key is
//     case-insensitive; "path" and "value" are the only keys read here) sets
//     that key for the CURRENT section. A key repeated within the same
//     section overwrites the previous value (last one wins), matching
//     git-config's own last-value-wins semantics for a simple key. A `value`
//     wrapped in double quotes has its surrounding quotes stripped and
//     `\"`/`\\` escapes unescaped; git-config's fuller escape grammar
//     (`\n`, `\t`, line continuation) is out of scope.
//   - A submodule section is emitted as one Entry only when BOTH "path" and
//     "url" were set somewhere in that section; a section missing either key
//     yields no entry (see sdk/go/factschema/submodule/v1.Pin's doc comment:
//     a collector emits a pin only once it has at least the join identity,
//     which for the ".gitmodules"-only view means both fields).
//
// CRLF line endings are normalized transparently: the trailing '\r' of a
// CRLF-terminated line is trimmed before every check above runs.
func Parse(body string) []Entry {
	var entries []Entry
	inSubmoduleSection := false
	var path, url string

	flush := func() {
		if inSubmoduleSection && path != "" && url != "" {
			entries = append(entries, Entry{Path: path, URL: url})
		}
		path, url = "", ""
	}

	for _, rawLine := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			flush()
			inSubmoduleSection = isSubmoduleSectionHeader(trimmed)
			continue
		}
		if !inSubmoduleSection {
			continue
		}

		key, value, ok := parseConfigKeyValueLine(trimmed)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "path":
			path = value
		case "url":
			url = value
		}
	}
	flush()

	return entries
}

// isSubmoduleSectionHeader reports whether a trimmed line opens a
// `[submodule "<name>"]` extended section: the section name (first
// whitespace-separated token inside the brackets) must equal "submodule"
// case-insensitively, and a double-quoted subsection must follow it. A bare
// `[submodule]` (no subsection) or any other section name does not match.
func isSubmoduleSectionHeader(trimmed string) bool {
	inner := strings.TrimPrefix(trimmed, "[")
	closeIdx := strings.Index(inner, "]")
	if closeIdx < 0 {
		return false
	}
	header := inner[:closeIdx]

	fields := strings.Fields(header)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "submodule") {
		return false
	}
	subsection := strings.Join(fields[1:], " ")
	return len(subsection) >= 2 && strings.HasPrefix(subsection, `"`) && strings.HasSuffix(subsection, `"`)
}

// parseConfigKeyValueLine splits a trimmed git-config body line into its key
// and value around the first '=', unquoting a double-quoted value. ok is
// false when the line carries no '=' or the key is blank.
func parseConfigKeyValueLine(trimmed string) (key, value string, ok bool) {
	idx := strings.Index(trimmed, "=")
	if idx < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(trimmed[:idx])
	if key == "" {
		return "", "", false
	}
	value = unquoteConfigValue(strings.TrimSpace(trimmed[idx+1:]))
	return key, value, true
}

// unquoteConfigValue strips a value's surrounding double quotes and
// unescapes `\"` and `\\`, mirroring git-config's basic quoting. A value not
// wrapped in a matching pair of double quotes is returned unchanged.
func unquoteConfigValue(value string) string {
	if len(value) < 2 || !strings.HasPrefix(value, `"`) || !strings.HasSuffix(value, `"`) {
		return value
	}
	inner := value[1 : len(value)-1]
	inner = strings.ReplaceAll(inner, `\"`, `"`)
	inner = strings.ReplaceAll(inner, `\\`, `\`)
	return inner
}
