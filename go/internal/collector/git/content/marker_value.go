// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"regexp"
	"strings"
)

// keepsVerbatim reports whether the whole remainder of the line, after the
// separator, is exactly an environment reference or an earlier marker,
// optionally in one matching pair of quotes, with nothing after it. Anything
// else, a trailing comment included, is redacted from the separator on.
func keepsVerbatim(value string) bool {
	inner := value
	if n := len(value); n >= 2 && (value[0] == '"' || value[0] == '\'') && value[n-1] == value[0] {
		inner = value[1 : n-1]
		// An empty or blank quoted value holds nothing to redact.
		if strings.TrimSpace(inner) == "" {
			return true
		}
	}
	return envReference.MatchString(inner) || existingMarker.MatchString(inner)
}

// stripCommentTagAnchor removes a YAML comment and leading !tag and &anchor
// tokens from a value, returning what is left of the node itself. A tag or
// anchor token is only stripped when more value follows it: a lone "!abc" or
// "&abc" is returned unchanged, so it is redacted as a value, not mistaken for
// an empty one. A comment needs whitespace before "#": spacedAfterColon says
// the separator supplied it for a "#" at the start of the value.
func stripCommentTagAnchor(value string, spacedAfterColon bool) string {
	if strings.HasPrefix(value, "#") && spacedAfterColon {
		return ""
	}
	if cut := strings.Index(value, " #"); cut >= 0 && !startsQuoted(value) {
		value = strings.TrimSpace(value[:cut])
	}
	for value != "" && (value[0] == '!' || value[0] == '&') {
		space := strings.IndexAny(value, " \t")
		if space < 0 {
			return value
		}
		rest := strings.TrimSpace(value[space:])
		if rest == "" || strings.HasPrefix(rest, "#") {
			return value
		}
		value = rest
	}
	return value
}

func startsQuoted(value string) bool {
	return value != "" && (value[0] == '"' || value[0] == '\'')
}

// splitQuotedValue splits a value into its quote character (zero when
// unquoted), the unquoted text, any text after the closing quote, and whether
// the quote closed on this line. An unquoted value is one token; an unterminated
// quote takes the rest of the line as the value, which fails closed.
func splitQuotedValue(value string) (quote byte, inner, rest string, closed bool) {
	if !startsQuoted(value) {
		return 0, value, "", true
	}
	quote = value[0]
	end := quoteEnd(value[1:], quote)
	if end < 0 {
		return quote, value[1:], "", false
	}
	return quote, value[1 : 1+end], value[2+end:], true
}

// quoteEnd returns the index of the first unescaped closing quote in text, or
// -1. Double quotes escape with a backslash; single quotes escape by doubling.
func quoteEnd(text string, quote byte) int {
	for i := 0; i < len(text); i++ {
		switch {
		case quote == '"' && text[i] == '\\':
			i++
		case text[i] == quote && quote == '\'' && i+1 < len(text) && text[i+1] == '\'':
			i++
		case text[i] == quote:
			return i
		}
	}
	return -1
}

func closesQuote(line string, quote byte) bool {
	return quoteEnd(line, quote) >= 0
}

// entryLine matches a line that starts a mapping entry or a sequence item after
// any flow openers and commas: a quoted or plain key, then ":" and a space or
// the end of the line. A URL such as "https://user:pw@host" has no space after
// its colon and is not an entry.
var entryLine = regexp.MustCompile(`^(?:-[ \t]+)*(?:"[^"]*"|'[^']*'|[^\s"'#,:{}\[\]][^#,:{}\[\]]*?)[ \t]*:(?:[ \t]|$)`)

// flowScan follows flow brackets and quotes across lines, enough to know when a
// listed key's flow value is still open. It is lexical on purpose: it serves
// only a body yaml.v3 cannot parse, and it errs toward staying open.
type flowScan struct {
	depth int
	quote byte
}

// scan advances the state over one line.
func (f *flowScan) scan(text string) {
	for i := 0; i < len(text); i++ {
		c := text[i]
		if f.quote != 0 {
			switch {
			case f.quote == '"' && c == '\\':
				i++
			case c == f.quote && f.quote == '\'' && i+1 < len(text) && text[i+1] == '\'':
				i++
			case c == f.quote:
				f.quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			f.quote = c
		case '{', '[':
			f.depth++
		case '}', ']':
			f.depth--
		case '#':
			if i == 0 || text[i-1] == ' ' || text[i-1] == '\t' {
				return
			}
		}
	}
}

// lexicalValueGap is valueReachesUnreplacedText for a body yaml.v3 cannot
// parse, where no node positions exist. After the separator of each line a
// listed key matches, it follows the flow brackets and quotes the value opens
// and reads the lines after it. Outside a quote a blank or comment line is
// skipped. While a bracket the value opened is still open, or a quote is, every
// other line is value. Once both are closed, a line of only flow brackets and
// commas, a document marker, or a line that starts another entry ends the value.
// Every value line must hold no text the redactor left in the clear; inside a
// quote a leading "#" is text.
func (c *yamlCheck) lexicalValueGap() bool {
	for k, line := range c.lines {
		after := -1
		for _, pattern := range c.keyPatterns {
			if pattern == nil {
				continue
			}
			if loc := pattern.FindStringIndex(line); loc != nil && (after < 0 || loc[1] < after) {
				after = loc[1]
			}
		}
		if after < 0 {
			continue
		}
		var state flowScan
		state.scan(line[after:])
		for j := k + 1; j < len(c.lines); j++ {
			text := c.lines[j]
			trimmed := strings.TrimSpace(text)
			inQuote := state.quote != 0
			if !inQuote {
				if trimmed == "" || trimmed[0] == '#' {
					continue
				}
				if first, _ := contentBounds(trimmed); state.depth <= 0 &&
					(first < 0 || trimmed == "..." || entryLine.MatchString(strings.TrimLeft(trimmed, "{[, \t"))) {
					break
				}
			}
			if !c.textHandled(j+1, len(text), inQuote) {
				return true
			}
			state.scan(text)
		}
	}
	return false
}
