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
// the secret on the anchor's line, an explicit "? key" entry, an escaped key
// spelling, or a key split across lines puts the key where no line-wise match
// sees it, and a listed key in a flow collection can continue its value on a
// later line at any indent, which the redactor's indent-based continuation does
// not cover. The caller stores a metadata row instead (#8833 security review).
var errYAMLIndirection = errors.New("content body reaches a credential key through a YAML alias or an explicit key")

// checkYAMLIndirection returns errYAMLIndirection for a body marker mode cannot
// redact line by line. listed holds the credential and URL-userinfo keys, and
// keyPatterns their line matchers. covered says, per source line, whether the
// redactor replaced something on it; it is nil when the body holds no flow
// collection, so the value-coverage rule below does not run. It runs two checks:
//
//   - The YAML check, when the body parses: any alias anywhere, a mapping key
//     that decodes to a listed key (case-insensitive) on a source line where no
//     key pattern matches (an explicit key, an escape, a key split across
//     lines), or a listed key whose value reaches a line the redactor left
//     alone (valueReachesUncoveredLine).
//   - The line check, always, so a body that does not parse is still covered:
//     an explicit-key indicator or explicit-key value line, and a listed key
//     whose value starts with an alias.
//
// An anchor on a listed key's value, an alias on another key, a merge key, and
// a value on the line after its key that the redactor's continuation covers are
// left alone: the redactor reaches each.
func checkYAMLIndirection(body string, covered []bool, listed []string, keyPatterns ...*regexp.Regexp) error {
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if err := checkYAMLStructure(body, lines, covered, listed, keyPatterns); err != nil {
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
// check still runs. A document after a parse error is not walked, the ones
// before it are.
func checkYAMLStructure(body string, lines []string, covered []bool, listed []string, keyPatterns []*regexp.Regexp) error {
	// A listed key or its value can only leave the key's line through an alias
	// (*), an explicit key (?), a double-quoted escape (\), or a flow collection
	// ({ or [), where a line break is white space inside the value. A body with
	// none of them needs no parse, which keeps the common file at line-redaction
	// cost.
	if !holdsAnyByte(body, '*', '?', '\\', '{', '[') {
		return nil
	}
	decoder := yaml.NewDecoder(strings.NewReader(body))
	var documents []*yaml.Node
	parseFailed := false
	for {
		document := new(yaml.Node)
		if err := decoder.Decode(document); err != nil {
			// io.EOF ends the stream; any other error stops at the documents
			// that did parse.
			parseFailed = !errors.Is(err, io.EOF)
			break
		}
		documents = append(documents, document)
	}
	check := yamlCheck{lines: lines, covered: covered, listed: listed, keyPatterns: keyPatterns}
	if covered != nil && len(covered) == len(lines) {
		check.firstColumn = firstNodeColumns(documents, len(lines))
	}
	for _, document := range documents {
		if check.walk(document, false) {
			return errYAMLIndirection
		}
	}
	// yaml.v3 is stricter than Yarn's js-yaml in places that matter here: it
	// rejects a tab as flow indentation and a bare document after "...", both of
	// which js-yaml reads. A body it cannot parse still has a value gap when a
	// line after a listed key is neither covered nor another entry.
	if parseFailed && covered != nil && len(covered) == len(lines) {
		if lexicalValueGap(lines, covered, keyPatterns) {
			return errYAMLIndirection
		}
	}
	return nil
}

// yamlCheck carries what walk needs about the source lines.
type yamlCheck struct {
	lines       []string
	covered     []bool
	listed      []string
	keyPatterns []*regexp.Regexp
	// firstColumn[n] is the 1-based column of the first node that starts on
	// 1-based source line n, or zero when none does. It is nil when the
	// value-coverage rule does not run.
	firstColumn []int
}

// walk reports whether node holds an alias, a listed key off its line, or a
// listed key whose value reaches a line the redactor left alone. insideListed
// is true under a listed key's value, whose lines the outer key's range already
// covers.
func (c *yamlCheck) walk(node *yaml.Node, insideListed bool) bool {
	if node.Kind == yaml.AliasNode {
		return true
	}
	if node.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(node.Content); i += 2 {
			key, value := node.Content[i], node.Content[i+1]
			if !isListedKey(key.Value, c.listed) {
				continue
			}
			if !keyMatchesItsLine(key.Line, c.lines, c.keyPatterns) {
				return true
			}
			if !insideListed && c.firstColumn != nil && c.valueReachesUncoveredLine(key, value) {
				return true
			}
		}
	}
	for i, child := range node.Content {
		nested := insideListed
		if node.Kind == yaml.MappingNode && i%2 == 1 && isListedKey(node.Content[i-1].Value, c.listed) {
			nested = true
		}
		if c.walk(child, nested) {
			return true
		}
	}
	return false
}

// holdsAnyByte reports whether body holds one of the bytes. Each byte is one
// vectorized IndexByte pass, which is cheaper than ContainsAny's per-byte set
// lookup on the 1 MiB body with none of them.
func holdsAnyByte(body string, bytes ...byte) bool {
	for _, b := range bytes {
		if strings.IndexByte(body, b) >= 0 {
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
