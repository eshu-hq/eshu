// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// errYAMLIndirection marks a body whose credential value marker mode cannot
// reach on the key's own line: a listed key whose value is a YAML alias leaves
// the secret on the anchor's line, and an explicit "? key" entry, an escaped
// key spelling, or a key split across lines puts the key where no line-wise
// match sees it. The caller stores a metadata row instead (#8833 security
// review).
var errYAMLIndirection = errors.New("content body reaches a credential key through a YAML alias or an explicit key")

// checkYAMLIndirection returns errYAMLIndirection for a body marker mode cannot
// redact line by line. listed holds the credential and URL-userinfo keys, and
// keyPatterns their line matchers. It runs two checks:
//
//   - The YAML check, when the body parses: any alias anywhere, or a mapping
//     key that decodes to a listed key (case-insensitive) on a source line
//     where no key pattern matches (an explicit key, an escape, a key split
//     across lines).
//   - The line check, always, so a body that does not parse is still covered:
//     an explicit-key indicator or explicit-key value line, and a listed key
//     whose value starts with an alias.
//
// An anchor on a listed key's value, an alias on another key, a merge key, and
// a value on the line after its key are left alone: the redactor reaches each.
func checkYAMLIndirection(body string, listed []string, keyPatterns ...*regexp.Regexp) error {
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if err := checkYAMLStructure(body, lines, listed, keyPatterns); err != nil {
		return err
	}
	for _, line := range lines {
		if explicitKeyLine(line) {
			return errYAMLIndirection
		}
		for _, pattern := range keyPatterns {
			if pattern == nil {
				continue
			}
			for _, loc := range pattern.FindAllStringIndex(line, -1) {
				if strings.HasPrefix(strings.TrimLeft(line[loc[1]:], " \t"), "*") {
					return errYAMLIndirection
				}
			}
		}
	}
	return nil
}

// checkYAMLStructure walks every document of body. A body that does not parse
// returns nil: it has no YAML semantics to hide a value behind, and the line
// check still runs.
func checkYAMLStructure(body string, lines, listed []string, keyPatterns []*regexp.Regexp) error {
	// A listed key or its value can only leave the key's line through an alias
	// (*), an explicit key (?), or a double-quoted escape (\). A body with none
	// of them needs no parse, which keeps the common file at line-redaction cost.
	if !strings.ContainsAny(body, "*?\\") {
		return nil
	}
	decoder := yaml.NewDecoder(strings.NewReader(body))
	for {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return nil
		}
		if walkYAML(&document, lines, listed, keyPatterns) {
			return errYAMLIndirection
		}
	}
}

// walkYAML reports whether node holds an alias or a listed key off its line.
func walkYAML(node *yaml.Node, lines, listed []string, keyPatterns []*regexp.Regexp) bool {
	if node.Kind == yaml.AliasNode {
		return true
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i]
			if isListedKey(key.Value, listed) && !keyMatchesItsLine(key.Line, lines, keyPatterns) {
				return true
			}
		}
	}
	for _, child := range node.Content {
		if walkYAML(child, lines, listed, keyPatterns) {
			return true
		}
	}
	return false
}

func isListedKey(value string, listed []string) bool {
	for _, key := range listed {
		if strings.EqualFold(value, key) {
			return true
		}
	}
	return false
}

// keyMatchesItsLine reports whether a key pattern matches the 1-based source
// line, which is where the redactor looks for the key.
func keyMatchesItsLine(line int, lines []string, keyPatterns []*regexp.Regexp) bool {
	if line < 1 || line > len(lines) {
		return false
	}
	for _, pattern := range keyPatterns {
		if pattern != nil && pattern.MatchString(lines[line-1]) {
			return true
		}
	}
	return false
}

// explicitKeyLine reports whether line holds an explicit-key indicator or is
// the value line of an explicit key.
func explicitKeyLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	for strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "-\t") {
		trimmed = strings.TrimLeft(trimmed[2:], " \t")
	}
	if indicatorAt(trimmed, 0, ':') {
		return true
	}
	quote := byte(0)
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t'):
			return false
		case c == '?' && indicatorAt(line, i, '?') && opensEntry(line[:i]):
			return true
		}
	}
	return false
}

// indicatorAt reports whether s[i] is the indicator byte followed by white
// space or the end of s.
func indicatorAt(s string, i int, indicator byte) bool {
	return i < len(s) && s[i] == indicator && (i+1 == len(s) || s[i+1] == ' ' || s[i+1] == '\t')
}

// opensEntry reports whether the text before an indicator ends where a new
// mapping entry can start: nothing, a list marker, or a flow "{", "[", or ",".
func opensEntry(before string) bool {
	before = strings.TrimRight(before, " \t")
	if before == "" {
		return true
	}
	switch before[len(before)-1] {
	case '-', '{', '[', ',':
		return true
	}
	return false
}
