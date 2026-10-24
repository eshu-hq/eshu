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
	blockScalar    = regexp.MustCompile(`^[|>][+-]?[0-9]?$`)

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

// markerState carries what crosses line boundaries: whether a PEM block is open
// and the indentation a listed key's block scalar must exceed.
type markerState struct {
	keyPattern *regexp.Regexp
	inPEM      bool
	// continuationOpen is true after a listed key whose value spills onto the
	// following lines; those lines must be indented deeper than
	// continuationIndent, the indent of the listed key line.
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
	if s.inPEM {
		if strings.TrimSpace(line) == "" {
			return line
		}
		if pemEnd.MatchString(line) {
			s.inPEM = false
		}
		s.replaced++
		return indent + markerPrefix + pemBlockKind
	}
	if s.continuationOpen {
		if strings.TrimSpace(line) == "" {
			return line
		}
		if len(indent) > s.continuationIndent {
			s.replaced++
			return indent + markerPrefix + defaultMarkerKind
		}
		s.continuationOpen = false
	}

	kind := detect.Classify(line)
	out, _ := s.redactListedKey(line, kind)
	out = s.redactDetectorSpans(out, kind)
	if pemBegin.MatchString(line) && !pemEnd.MatchString(line) {
		s.inPEM = true
	}
	return out
}

// redactListedKey replaces the value of the first listed key on the line. It
// reports whether a listed key matched.
func (s *markerState) redactListedKey(line, rawKind string) (string, bool) {
	loc := s.keyPattern.FindStringSubmatchIndex(line)
	if loc == nil {
		return line, false
	}
	keyText := line[loc[2]:loc[3]]
	valueStart := loc[1]
	value := line[valueStart:]
	kind := rawKind
	if kind == "" {
		kind = detect.Classify(keyText + "=")
	}
	if kind == "" {
		kind = defaultMarkerKind
	}
	marker := markerPrefix + kind

	trimmed := strings.TrimSpace(value)
	if trimmed == "" || blockScalar.MatchString(trimmed) {
		// The value is empty or a block scalar: it continues on the following
		// more-indented lines, so those are redacted too.
		s.continuationOpen = true
		s.continuationIndent = len(leadingWhitespace(line))
		if trimmed == "" {
			return line, true
		}
		s.replaced++
		return line[:valueStart] + marker, true
	}

	quote, inner, rest := splitQuotedValue(trimmed)
	if envReference.MatchString(inner) && (rest == "" || strings.HasPrefix(strings.TrimSpace(rest), "#")) {
		return line, true
	}
	if existingMarker.MatchString(inner) && rest == "" {
		return line, true
	}
	s.replaced++
	return line[:valueStart] + quote + marker + quote, true
}

// splitQuotedValue splits a value into its quote character, the unquoted text,
// and any text after the closing quote. An unquoted value has no quote and no
// rest; a value with an unterminated quote keeps the quote and treats the whole
// remainder as the value, which fails closed.
func splitQuotedValue(value string) (quote, inner, rest string) {
	if value == "" || (value[0] != '"' && value[0] != '\'') {
		if cut := strings.Index(value, " #"); cut >= 0 {
			return "", strings.TrimSpace(value[:cut]), value[cut:]
		}
		return "", value, ""
	}
	closing := strings.IndexByte(value[1:], value[0])
	if closing < 0 {
		return string(value[0]), value[1:], ""
	}
	return string(value[0]), value[1 : 1+closing], value[2+closing:]
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
