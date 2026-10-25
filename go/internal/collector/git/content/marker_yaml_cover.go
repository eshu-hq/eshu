// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

// valueReachesUncoveredLine reports whether a listed key's value can put text
// on a source line the redactor left alone (#8833, the independent review's P1).
//
// The redactor replaces the value on the key's line and treats the lines after
// it as the value only while each is more indented than the key's line, or is
// a same-indent "- " item. YAML does not: inside a flow collection a line break
// is white space at any indent, so `{` newline `npmAuthToken:` newline `value`
// newline `}` holds the value on a line at the key's own indent, and a plain
// scalar may continue onto a line at any indent too. yaml.v3 only reports where
// a node starts, so the value is taken to reach every line from the key's line
// to the line before the next node starts, plus the text before that next node
// on its own line. Each of those lines must be:
//
//   - one the redactor replaced (covered), or
//   - holding nothing a value can be made of: blank, a comment, flow brackets
//     and commas, or a document marker.
//
// Anything else is a value line off the redactor's reach, so the body fails
// closed. An ordinary block file never trips it: the next key starts on the
// next line, and a block scalar or quoted value spills only onto more-indented
// lines the redactor covers.
func (c *yamlCheck) valueReachesUncoveredLine(key, value *yaml.Node) bool {
	last := lastNodeLine(value, key.Line)
	for line := key.Line + 1; line <= len(c.lines); line++ {
		text := c.lines[line-1]
		handled := c.covered[line-1] || markerInLine.MatchString(text)
		if line > last {
			if column := c.firstColumn[line-1]; column != 0 {
				// The next node starts here. Text before it on this line is the
				// tail of the value; the node itself is another entry.
				return !harmlessValueText(runePrefix(text, column-1)) && !handled
			}
		}
		if !harmlessValueLine(text) && !handled {
			return true
		}
	}
	return false
}

// lastNodeLine returns the greatest start line in the subtree of node, or floor
// when none is greater.
func lastNodeLine(node *yaml.Node, floor int) int {
	last := floor
	var visit func(*yaml.Node, int)
	visit = func(n *yaml.Node, depth int) {
		if n.Line > last {
			last = n.Line
		}
		if depth > 64 {
			return
		}
		for _, child := range n.Content {
			visit(child, depth+1)
		}
	}
	visit(node, 0)
	return last
}

// firstNodeColumns maps each source line, by index line-1, to the 1-based column
// of the first node that starts on it, or zero. Document nodes are skipped:
// they report their content's position.
func firstNodeColumns(documents []*yaml.Node, lineCount int) []int {
	columns := make([]int, lineCount)
	var visit func(*yaml.Node, int)
	visit = func(n *yaml.Node, depth int) {
		if n.Kind != yaml.DocumentNode && n.Line >= 1 && n.Line <= lineCount {
			if cur := columns[n.Line-1]; cur == 0 || n.Column < cur {
				columns[n.Line-1] = max(n.Column, 1)
			}
		}
		if depth > 10000 {
			return
		}
		for _, child := range n.Content {
			visit(child, depth+1)
		}
	}
	for _, document := range documents {
		visit(document, 0)
	}
	return columns
}

// runePrefix returns the first count runes of text; yaml.v3 columns count
// characters.
func runePrefix(text string, count int) string {
	if count <= 0 {
		return ""
	}
	for i := range text {
		if count == 0 {
			return text[:i]
		}
		count--
	}
	return text
}

// harmlessValueLine reports whether a whole line holds nothing a value can be
// made of: blank, a comment, flow brackets and commas, or a document
// marker.
func harmlessValueLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || trimmed[0] == '#' || trimmed == "---" || trimmed == "..." ||
		strings.Trim(trimmed, ",{}[]") == ""
}

// harmlessValueText reports whether the text before a node on its line holds
// nothing a value can be made of: white space, flow brackets, commas, and the
// dashes of a block sequence or document start.
func harmlessValueText(text string) bool {
	return strings.Trim(text, " \t,{}[]-") == ""
}

// markerInLine matches a marker an earlier pass wrote anywhere on a line. Such a
// line was already redacted, which keeps a second pass over the output from
// failing closed on a body the first pass accepted.
var markerInLine = regexp.MustCompile(markerPrefix + `(?:` + strings.Join(detect.Kinds(), "|") + `)`)

// entryLine matches a line that starts a mapping entry or a sequence item after
// any flow openers and commas: a quoted or plain key, then ":" and a space or
// the end of the line. A URL such as "https://user:pw@host" has no space after
// its colon and is not an entry.
var entryLine = regexp.MustCompile(`^(?:-[ \t]+)*(?:"[^"]*"|'[^']*'|[^\s"'#,:{}\[\]][^#,:{}\[\]]*?)[ \t]*:(?:[ \t]|$)`)

// lexicalValueGap is valueReachesUncoveredLine for a body yaml.v3 cannot parse,
// where no node positions exist. After each line a listed key matches, it reads
// the following lines: a blank or comment line is skipped, a covered line is
// part of the value, and a line of flow brackets, commas, or a document marker
// or a line that starts another entry ends it. Any other line is text the
// redactor left alone and may be the rest of the value.
func lexicalValueGap(lines []string, covered []bool, keyPatterns []*regexp.Regexp) bool {
	for k, line := range lines {
		if !matchesAnyKey(line, keyPatterns) {
			continue
		}
		for j := k + 1; j < len(lines); j++ {
			trimmed := strings.TrimSpace(lines[j])
			if trimmed == "" || trimmed[0] == '#' || covered[j] || markerInLine.MatchString(trimmed) {
				continue
			}
			if harmlessValueLine(lines[j]) || entryLine.MatchString(strings.TrimLeft(trimmed, "{[, \t")) {
				break
			}
			return true
		}
	}
	return false
}

func matchesAnyKey(line string, keyPatterns []*regexp.Regexp) bool {
	for _, pattern := range keyPatterns {
		if pattern != nil && pattern.MatchString(line) {
			return true
		}
	}
	return false
}
