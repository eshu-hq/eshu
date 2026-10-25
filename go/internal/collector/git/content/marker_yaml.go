// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
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
// keyPatterns their line matchers. redacted holds the redactor's output lines,
// one per source line. It runs two checks:
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
func checkYAMLIndirection(body string, redacted, listed []string, keyPatterns ...*regexp.Regexp) error {
	lines := strings.Split(body, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	if err := checkYAMLStructure(body, lines, redacted, listed, keyPatterns); err != nil {
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
func checkYAMLStructure(body string, lines, redacted, listed []string, keyPatterns []*regexp.Regexp) error {
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
	redactedLines := make([]string, len(redacted))
	for i, line := range redacted {
		redactedLines[i] = strings.TrimSuffix(line, "\r")
	}
	check := yamlCheck{
		lines: lines, redacted: redactedLines, listed: listed, keyPatterns: keyPatterns,
		firstColumn: firstNodeColumns(documents, len(lines)),
	}
	for _, document := range documents {
		if check.walk(document, false) {
			return errYAMLIndirection
		}
	}
	// yaml.v3 is stricter than Yarn's js-yaml in places that matter here: it
	// rejects a tab as flow indentation and a bare document after "...", both of
	// which js-yaml reads. A body it cannot parse still has a value gap when a
	// line after a listed key is neither replaced nor another entry.
	if parseFailed && check.lexicalValueGap() {
		return errYAMLIndirection
	}
	return nil
}

// yamlCheck carries what walk needs about the source lines.
type yamlCheck struct {
	// lines are the source lines and redacted the redactor's output for each,
	// both without a carriage return.
	lines, redacted []string
	listed          []string
	keyPatterns     []*regexp.Regexp
	// firstColumn[n-1] is the 1-based column of the first node that starts on
	// 1-based source line n, or zero when none does.
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
			if !insideListed && c.valueReachesUnreplacedText(key, value) {
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

// valueReachesUnreplacedText reports whether a listed key's value can put text
// on a source line that the redactor did not replace (#8833, the independent
// review's P1).
//
// The redactor replaces the value on the key's line and treats the lines after
// it as the value only while each is more indented than the key's line, or is
// a same-indent "- " item. YAML does not: inside a flow collection a line break
// is white space at any indent, so `{` newline `npmAuthToken:` newline `value`
// newline `}` holds the value on a line at the key's own indent, and a plain
// scalar may continue onto a line at any indent too. yaml.v3 only reports where
// a node starts, so the value is taken to reach every line from the key's line
// to the line before the next node starts, plus the text before that next node
// on its own line. On each of those lines, the text that could be value (all of
// the text on a whole line, the text before the node on the last) must be:
//
//   - nothing: blank, a comment, flow brackets and commas, or a document
//     marker, or
//   - inside the one span the redactor replaced with a marker on that line, or
//   - already exactly a marker or an environment reference an earlier pass kept.
//
// Anything else is a value line off the redactor's reach, so the body fails
// closed. An ordinary block file never trips it: the next key starts on the
// next line, and a block scalar or quoted value spills only onto more-indented
// lines the redactor replaces whole.
func (c *yamlCheck) valueReachesUnreplacedText(key, value *yaml.Node) bool {
	last := lastNodeLine(value, key.Line)
	for line := key.Line + 1; line <= len(c.lines); line++ {
		text := c.lines[line-1]
		if line > last {
			if column := c.firstColumn[line-1]; column != 0 {
				// The next node starts here. Text before it on this line is the
				// tail of the value; the node itself is another entry.
				return !c.textHandled(line, runeOffset(text, column-1))
			}
		}
		if !c.textHandled(line, len(text)) {
			return true
		}
	}
	return false
}

// textHandled reports whether the first limit bytes of source line n (1-based)
// hold no credential text the redactor left in the clear.
func (c *yamlCheck) textHandled(n, limit int) bool {
	src := c.lines[n-1]
	if strings.HasPrefix(strings.TrimSpace(src), "#") || strings.TrimSpace(src) == "..." {
		return true
	}
	first, end := contentBounds(src[:limit])
	if first < 0 {
		return true
	}
	out := c.redacted[n-1]
	if out == src {
		// The line is unchanged: it may only be a marker or an environment
		// reference the redactor keeps, with nothing else around it.
		return keepsVerbatim(src[first:end])
	}
	// The line changed. The redactor writes one marker per line, so the span it
	// replaced runs from the marker's start in the output to the matching
	// position from the end of the line; the text before and after must be the
	// source text unchanged.
	markers := markerAnywhere.FindAllStringIndex(out, -1)
	if len(markers) != 1 {
		return false
	}
	start, stop := markers[0][0], markers[0][1]
	srcEnd := len(src) - (len(out) - stop)
	if srcEnd < start || src[:start] != out[:start] || src[srcEnd:] != out[stop:] {
		return false
	}
	return first >= start && end <= srcEnd
}

// contentBounds returns the byte range of text without the leading and trailing
// characters a value cannot be made of: white space, flow brackets, commas, and
// the dashes of a block sequence or a document start. first is -1 when nothing
// is left.
func contentBounds(text string) (first, end int) {
	trimmed := strings.TrimLeft(text, " \t,{}[]-")
	if trimmed == "" {
		return -1, -1
	}
	first = len(text) - len(trimmed)
	end = len(strings.TrimRight(text, " \t,{}[]-"))
	return first, end
}

// lastNodeLine returns the greatest start line in the subtree of node, or floor
// when none is greater. It walks without a depth cap: yaml.v3 bounds the depth
// of what it parses, and a partial maximum would shorten the range the caller
// checks.
func lastNodeLine(node *yaml.Node, floor int) int {
	last := floor
	stack := []*yaml.Node{node}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Line > last {
			last = n.Line
		}
		stack = append(stack, n.Content...)
	}
	return last
}

// firstNodeColumns maps each source line, by index line-1, to the 1-based column
// of the first node that starts on it, or zero. Document nodes are skipped:
// they report their content's position.
func firstNodeColumns(documents []*yaml.Node, lineCount int) []int {
	columns := make([]int, lineCount)
	stack := append([]*yaml.Node{}, documents...)
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n.Kind != yaml.DocumentNode && n.Line >= 1 && n.Line <= lineCount {
			if cur := columns[n.Line-1]; cur == 0 || n.Column < cur {
				columns[n.Line-1] = max(n.Column, 1)
			}
		}
		stack = append(stack, n.Content...)
	}
	return columns
}

// runeOffset returns the byte offset of the count-th rune of text; yaml.v3
// columns count characters.
func runeOffset(text string, count int) int {
	if count <= 0 {
		return 0
	}
	for i := range text {
		if count == 0 {
			return i
		}
		count--
	}
	return len(text)
}

// markerAnywhere matches a marker anywhere in a line. It finds the span the
// redactor wrote; it never makes a line safe by itself (textHandled).
var markerAnywhere = regexp.MustCompile(markerPrefix + `(?:` + strings.Join(detect.Kinds(), "|") + `)`)

// entryLine matches a line that starts a mapping entry or a sequence item after
// any flow openers and commas: a quoted or plain key, then ":" and a space or
// the end of the line. A URL such as "https://user:pw@host" has no space after
// its colon and is not an entry.
var entryLine = regexp.MustCompile(`^(?:-[ \t]+)*(?:"[^"]*"|'[^']*'|[^\s"'#,:{}\[\]][^#,:{}\[\]]*?)[ \t]*:(?:[ \t]|$)`)

// lexicalValueGap is valueReachesUnreplacedText for a body yaml.v3 cannot
// parse, where no node positions exist. After each line a listed key matches,
// it reads the following lines: a blank or comment line is skipped, a line of
// flow brackets, commas, or a document marker or a line that starts another
// entry ends the value, as does a line that starts with a flow closer or comma,
// and any other line must hold no text the redactor left in the clear.
func (c *yamlCheck) lexicalValueGap() bool {
	for k, line := range c.lines {
		if !matchesAnyKey(line, c.keyPatterns) {
			continue
		}
		for j := k + 1; j < len(c.lines); j++ {
			trimmed := strings.TrimSpace(c.lines[j])
			if trimmed == "" || trimmed[0] == '#' {
				continue
			}
			// A flow indicator at the start of a line ends a plain scalar, so
			// what follows belongs to other nodes; so does an entry.
			if first, _ := contentBounds(trimmed); first < 0 || trimmed == "..." || strings.ContainsRune("]},", rune(trimmed[0])) ||
				entryLine.MatchString(strings.TrimLeft(trimmed, "{[, \t")) {
				break
			}
			if !c.textHandled(j+1, len(c.lines[j])) {
				return true
			}
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
