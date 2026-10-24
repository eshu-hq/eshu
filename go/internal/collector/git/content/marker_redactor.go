// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

// markerPrefix starts every marker. The form is REDACTED_<finding_kind>: the
// kind is the migration 138 finding kind computed on the raw line, so the stored
// finding can keep its kind once #8833's marker arm lands. [redacted] and ***
// are rejected because the detector cannot see them and they would blind
// investigate_hardcoded_secrets.
const markerPrefix = "REDACTED_"

// defaultMarkerKind marks a listed-key value on a line the CASE gives no kind,
// such as npmAuthIdent. It is a credential-named kind the detector already
// reports for a key=value line.
const defaultMarkerKind = "secret_literal"

// pemBlockKind marks every line inside a PEM BEGIN..END block.
const pemBlockKind = "private_key"

var (
	errInvalidUTF8 = errors.New("content body is not valid UTF-8")

	// envReference is the only value a listed key may keep: an exact ${NAME}.
	// A ${NAME:-literal} default carries a literal and is redacted.
	envReference = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)
	// existingMarker is a value an earlier pass already replaced, which keeps
	// the redactor a fixed point.
	existingMarker = regexp.MustCompile(`^` + markerPrefix + `(` + strings.Join(detect.Kinds(), "|") + `)$`)
	pemBegin       = regexp.MustCompile(`-----BEGIN [A-Z0-9 ]+-----`)
	pemEnd         = regexp.MustCompile(`-----END [A-Z0-9 ]+-----`)
	blockScalar    = regexp.MustCompile(`^[|>]([1-9][+-]?|[+-][1-9]?)?$`)

	keyPatternCache sync.Map // joined key list -> *regexp.Regexp
)

// redactMarker replaces credential values in body with REDACTED_<finding_kind>
// markers and reports how many values it replaced. It is line-wise, so the line
// count and every line number are preserved. Three things trigger redaction:
//
//   - a value assigned to one of keys (case-insensitive), at any length and
//     charset, except an exact ${NAME} reference;
//   - any span the migration 138 detector matches on another line;
//   - every line inside a PEM BEGIN..END block, to end of file when the block
//     never closes, and the continuation lines of a listed key's block scalar.
//
// It returns an error, never a partial body, for invalid UTF-8: the caller
// falls back to a metadata row, not the raw body.
func redactMarker(body string, keys []string) (string, int, error) {
	if !utf8.ValidString(body) {
		return "", 0, errInvalidUTF8
	}
	keyPattern, err := credentialKeyPattern(keys)
	if err != nil {
		return "", 0, err
	}
	lines := strings.Split(body, "\n")
	state := markerState{keyPattern: keyPattern}
	for i, line := range lines {
		lines[i] = state.redactLine(line)
	}
	return strings.Join(lines, "\n"), state.replaced, nil
}

// credentialKeyPattern compiles the listed-key matcher once per key list. It
// matches a key, an optional closing quote, and the separator; the value is the
// rest of the line.
func credentialKeyPattern(keys []string) (*regexp.Regexp, error) {
	if len(keys) == 0 {
		return nil, errors.New("marker redaction needs at least one credential key")
	}
	sorted := slices.Clone(keys)
	slices.SortFunc(sorted, func(a, b string) int {
		if len(a) != len(b) {
			return len(b) - len(a)
		}
		return strings.Compare(a, b)
	})
	cacheKey := strings.Join(sorted, "\x00")
	if cached, ok := keyPatternCache.Load(cacheKey); ok {
		return cached.(*regexp.Regexp), nil
	}
	quoted := make([]string, 0, len(sorted))
	for _, key := range sorted {
		if strings.TrimSpace(key) == "" {
			return nil, errors.New("marker redaction credential key is blank")
		}
		quoted = append(quoted, regexp.QuoteMeta(key))
	}
	pattern, err := regexp.Compile(`(?i)(` + strings.Join(quoted, "|") + `)(["']?[ \t]*[:=][ \t]*)`)
	if err != nil {
		return nil, fmt.Errorf("compile credential key pattern: %w", err)
	}
	keyPatternCache.Store(cacheKey, pattern)
	return pattern, nil
}

// markerState carries what crosses line boundaries: an open PEM block, an open
// quoted scalar, and the continuation lines of a listed key's value.
type markerState struct {
	keyPattern *regexp.Regexp
	inPEM      bool
	// openQuote is the quote character of a listed key's quoted value that has
	// not closed yet; every line up to and including the closing one is
	// redacted. Zero when none is open.
	openQuote byte
	// continuationOpen is true after a listed key line: YAML lets the value
	// spill onto more-indented lines (block and multi-line plain scalars) or a
	// same-indent "- " sequence. Those lines are redacted until a line that
	// does neither. continuationIndent is the listed key line's indent.
	continuationOpen   bool
	continuationIndent int
	replaced           int
}

func (s *markerState) redactLine(line string) string {
	carriage := ""
	if strings.HasSuffix(line, "\r") {
		carriage = "\r"
		line = strings.TrimSuffix(line, "\r")
	}
	return s.redactText(line) + carriage
}

func (s *markerState) redactText(line string) string {
	indent := leadingWhitespace(line)
	trimmed := strings.TrimSpace(line)
	if s.inPEM {
		if trimmed == "" {
			return line
		}
		if pemEnd.MatchString(line) {
			s.inPEM = false
		}
		s.replaced++
		return indent + markerPrefix + pemBlockKind
	}
	if s.openQuote != 0 {
		// Inside a listed key's multi-line quoted scalar: the whole line is
		// value, including the line that closes the quote.
		if closesQuote(line, s.openQuote) {
			s.openQuote = 0
		}
		s.replaced++
		return indent + markerPrefix + defaultMarkerKind
	}
	if s.continuationOpen {
		if trimmed == "" {
			return line
		}
		if len(indent) > s.continuationIndent || (len(indent) == s.continuationIndent && isSequenceItem(trimmed)) {
			s.replaced++
			return indent + markerPrefix + defaultMarkerKind
		}
		s.continuationOpen = false
	}

	kind := detect.Classify(line)
	out := s.redactListedKey(line, kind)
	out = s.redactDetectorSpans(out, kind)
	if pemBegin.MatchString(line) && !pemEnd.MatchString(line) {
		s.inPEM = true
	}
	return out
}

func isSequenceItem(trimmed string) bool {
	return trimmed == "-" || strings.HasPrefix(trimmed, "- ")
}

// redactListedKey replaces the value of the first listed key on the line and
// opens the continuation state its value needs. A value that is empty, only a
// comment, tag, or anchor, or a block scalar indicator carries its text on the
// following lines; a quote that does not close on the line carries it too.
func (s *markerState) redactListedKey(line, rawKind string) string {
	loc := s.keyPattern.FindStringSubmatchIndex(line)
	if loc == nil {
		return line
	}
	keyText := line[loc[2]:loc[3]]
	valueStart := loc[1]
	kind := rawKind
	if kind == "" {
		kind = detect.Classify(keyText + "=")
	}
	if kind == "" {
		kind = defaultMarkerKind
	}
	marker := markerPrefix + kind

	// Whatever the value looks like, more-indented lines after a listed key
	// belong to it (a multi-line plain scalar, a block scalar, a sequence).
	s.continuationOpen = true
	s.continuationIndent = len(leadingWhitespace(line))

	value := strings.TrimSpace(line[valueStart:])
	head := stripCommentTagAnchor(value)
	if head == "" {
		// Nothing but whitespace, a comment, a tag, or an anchor: the value is
		// on the following lines, which the continuation redacts.
		return line
	}
	if blockScalar.MatchString(head) {
		s.replaced++
		return line[:valueStart] + marker
	}

	quote, inner, rest, closed := splitQuotedValue(head)
	if !closed {
		// An unterminated quote spills onto the following lines until it closes.
		s.openQuote = quote
	}
	if closed && envReference.MatchString(inner) && (rest == "" || strings.HasPrefix(strings.TrimSpace(rest), "#")) {
		return line
	}
	if closed && existingMarker.MatchString(inner) && rest == "" {
		return line
	}
	s.replaced++
	quoteText := ""
	if quote != 0 {
		quoteText = string(quote)
	}
	return line[:valueStart] + quoteText + marker + quoteText
}

// stripCommentTagAnchor removes a trailing " #" comment and leading !tag and
// &anchor tokens from a YAML value, returning what is left of the node itself.
func stripCommentTagAnchor(value string) string {
	if strings.HasPrefix(value, "#") {
		return ""
	}
	if cut := strings.Index(value, " #"); cut >= 0 && !startsQuoted(value) {
		value = strings.TrimSpace(value[:cut])
	}
	for value != "" && (value[0] == '!' || value[0] == '&') {
		space := strings.IndexAny(value, " \t")
		if space < 0 {
			return ""
		}
		value = strings.TrimSpace(value[space:])
		if strings.HasPrefix(value, "#") {
			return ""
		}
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

// redactDetectorSpans replaces every span the migration 138 detector matches.
// A span that is already a marker is left alone, so the pass is a fixed point.
func (s *markerState) redactDetectorSpans(line, rawKind string) string {
	spans := detect.Spans(line)
	if len(spans) == 0 {
		return line
	}
	kind := rawKind
	if kind == "" {
		kind = defaultMarkerKind
	}
	var b strings.Builder
	last := 0
	for _, span := range spans {
		if existingMarker.MatchString(line[span.Start:span.End]) {
			continue
		}
		b.WriteString(line[last:span.Start])
		b.WriteString(markerPrefix + kind)
		last = span.End
		s.replaced++
	}
	b.WriteString(line[last:])
	return b.String()
}

func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
