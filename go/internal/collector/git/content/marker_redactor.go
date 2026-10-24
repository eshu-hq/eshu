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
	// errNotText marks a body with a NUL byte. Valid UTF-8 can hold NUL, but a
	// BOM-less UTF-16 or UTF-32 file is NUL-interleaved, so the redactor would
	// see no key and return it unchanged. Postgres text cannot hold NUL either.
	errNotText = errors.New("content body holds a NUL byte")
	// errTooLarge marks a body over MaxGovernedBodyBytes.
	errTooLarge = errors.New("content body exceeds the governed size cap")

	// envReference is the only value a listed key may keep: an exact ${NAME}.
	// A ${NAME:-literal} default carries a literal and is redacted.
	envReference = regexp.MustCompile(`^\$\{[A-Za-z_][A-Za-z0-9_]*\}$`)
	// existingMarker is a value an earlier pass already replaced, which keeps
	// the redactor a fixed point.
	existingMarker = regexp.MustCompile(`^` + markerPrefix + `(` + strings.Join(detect.Kinds(), "|") + `)$`)
	pemBegin       = regexp.MustCompile(`(?i)-----BEGIN [A-Z0-9 ]+-----`)
	pemEnd         = regexp.MustCompile(`(?i)-----END [A-Z0-9 ]+-----`)
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
// It returns an error, never a partial body, for a body that is not text
// (invalid UTF-8 or a NUL byte) or is over MaxGovernedBodyBytes: the caller
// falls back to a metadata row, not the raw body.
func redactMarker(body string, keys []string) (string, int, error) {
	if len(body) > MaxGovernedBodyBytes {
		return "", 0, errTooLarge
	}
	if !utf8.ValidString(body) {
		return "", 0, errInvalidUTF8
	}
	if strings.IndexByte(body, 0) >= 0 {
		return "", 0, errNotText
	}
	keyPattern, err := credentialKeyPattern(keys)
	if err != nil {
		return "", 0, err
	}
	lines := strings.Split(body, "\n")
	state := markerState{keyPattern: keyPattern, lowerKeys: lowerKeys(keys)}
	for i, line := range lines {
		lines[i] = state.redactLine(line)
	}
	return strings.Join(lines, "\n"), state.replaced, nil
}

func lowerKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, strings.ToLower(key))
	}
	return out
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
	pattern, err := regexp.Compile(`(?i)(` + strings.Join(quoted, "|") + `)(["']?` + detect.SpaceClass + `*[:=]` + detect.SpaceClass + `*)`)
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
	lowerKeys  []string
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
	// A footer-less PEM header opens the block wherever the line is handled, so
	// it is read first: the in-block, open-quote, and continuation branches below
	// return the whole line replaced, and without this a flat key body after
	// their header would survive.
	headerOpen := strings.Contains(line, "-----") && pemLeftOpen(line)
	if out, handled := s.redactInState(line); handled {
		if headerOpen {
			s.inPEM = true
		}
		return out
	}
	rawLine := line

	// A PEM header on this line is replaced through its footer, or to the end of
	// the line when the footer is on a later line, which then opens the block.
	if strings.Contains(line, "-----") {
		var open bool
		line, open = s.redactInlinePEM(line)
		s.inPEM = open
	}
	// The common line holds no credential, so the finding kind (six regular
	// expressions) is computed only for a line that matched a listed key or the
	// detector.
	var keyLoc []int
	if s.mayHoldKey(line) {
		keyLoc = s.keyPattern.FindStringSubmatchIndex(line)
	}
	spans := detect.Spans(line)
	if keyLoc == nil && len(spans) == 0 {
		return line
	}
	// The kind comes from the raw line, as the ruling says, so a header the PEM
	// pass already replaced still decides it.
	kind := detect.Classify(rawLine)
	out := line
	if keyLoc != nil {
		out = s.redactListedKey(line, line[keyLoc[2]:keyLoc[3]], keyLoc[3], keyLoc[1], kind, false)
		spans = detect.Spans(out)
	}
	return s.redactDetectorMatch(out, spans, kind)
}

// mayHoldKey is a cheap necessary test for a listed key: the line, lowercased,
// holds one of the lowercased keys. It keeps the key regular expression off the
// common line.
func (s *markerState) mayHoldKey(line string) bool {
	// Go (?i) folds U+017F and U+212A where ToLower does not, so a non-ASCII
	// byte skips nothing.
	for i := 0; i < len(line); i++ {
		if line[i] >= 0x80 {
			return true
		}
	}
	lower := strings.ToLower(line)
	for _, key := range s.lowerKeys {
		if strings.Contains(lower, key) {
			return true
		}
	}
	return false
}

// redactInlinePEM replaces every PEM BEGIN header on the line with a marker,
// through the matching END footer when it is on the same line and to the end of
// the line when it is not. The second result reports a block left open: the
// following lines are the key material. Headers of every type are replaced, so
// the output holds no BEGIN header and a second pass changes nothing.
func (s *markerState) redactInlinePEM(line string) (string, bool) {
	var b strings.Builder
	rest := line
	for {
		begin := pemBegin.FindStringIndex(rest)
		if begin == nil {
			b.WriteString(rest)
			return b.String(), false
		}
		b.WriteString(rest[:begin[0]])
		b.WriteString(markerPrefix + pemBlockKind)
		s.replaced++
		end := pemEnd.FindStringIndex(rest[begin[1]:])
		if end == nil {
			// The footer is on a later line. Text after the header on this line
			// is key material and goes too; a closing quote alone is not, so
			// `key = "-----BEGIN ... KEY-----"` keeps its quote.
			if tail := rest[begin[1]:]; strings.Trim(tail, "\"' \t") == "" {
				b.WriteString(tail)
			}
			return b.String(), true
		}
		rest = rest[begin[1]+end[1]:]
	}
}

// redactInState handles a line that belongs to an open PEM block, an open
// quoted scalar, or the continuation of a listed key. It reports false when the
// line is outside all three and needs the ordinary pass.
func (s *markerState) redactInState(line string) (string, bool) {
	indent := leadingWhitespace(line)
	trimmed := strings.TrimSpace(line)
	if s.inPEM {
		if trimmed == "" {
			return line, true
		}
		if pemEnd.MatchString(line) {
			s.inPEM = false
		}
		s.replaced++
		return indent + markerPrefix + pemBlockKind, true
	}
	if s.openQuote != 0 {
		// Inside a listed key's multi-line quoted scalar: the whole line is
		// value, including the line that closes the quote.
		if closesQuote(line, s.openQuote) {
			s.openQuote = 0
		}
		s.replaced++
		return indent + markerPrefix + defaultMarkerKind, true
	}
	if s.continuationOpen {
		if trimmed == "" {
			return line, true
		}
		if len(indent) > s.continuationIndent || (len(indent) == s.continuationIndent && isSequenceItem(trimmed)) {
			s.replaced++
			return indent + markerPrefix + defaultMarkerKind, true
		}
		s.continuationOpen = false
	}
	return line, false
}

// pemLeftOpen reports whether the last PEM BEGIN header on the line has no END
// footer after it on the same line, so the block continues on the next line.
func pemLeftOpen(line string) bool {
	begins := pemBegin.FindAllStringIndex(line, -1)
	if len(begins) == 0 {
		return false
	}
	last := begins[len(begins)-1]
	return pemEnd.FindStringIndex(line[last[1]:]) == nil
}

func isSequenceItem(trimmed string) bool {
	return trimmed == "-" || strings.HasPrefix(trimmed, "- ")
}

// redactListedKey replaces the value of the first listed key on the line and
// opens the continuation state its value needs. A value that is empty, only a
// comment, tag, or anchor, or a block scalar indicator carries its text on the
// following lines; a quote that does not close on the line carries it too.
func (s *markerState) redactListedKey(line, keyText string, keyEnd, valueStart int, rawKind string, detectorMatch bool) string {
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
	separator := line[keyEnd:valueStart]
	// Comment, tag, and anchor syntax is YAML's: it applies after a colon, and a
	// comment needs whitespace after the separator. After "=" (.npmrc, ini,
	// .env) the whole remainder is the value, "#" and "!" included.
	// A detector key match is never a comment, tag, or anchor: the detector's
	// value class holds "#", "!" and "&", and it flagged the line, so the whole
	// remainder is the value.
	head := value
	if !detectorMatch && strings.Contains(separator, ":") {
		head = stripCommentTagAnchor(value, strings.HasSuffix(separator, " ") || strings.HasSuffix(separator, "\t"))
	}
	if head == "" {
		// Nothing but whitespace, a comment, a tag, or an anchor: the value is
		// on the following lines, which the continuation redacts.
		return line
	}
	if blockScalar.MatchString(head) {
		s.replaced++
		return line[:valueStart] + marker
	}
	if keepsVerbatim(value) {
		return line
	}

	quote, _, _, closed := splitQuotedValue(head)
	if !closed {
		// An unterminated quote spills onto the following lines until it closes.
		s.openQuote = quote
	}
	s.replaced++
	quoteText := ""
	if quote != 0 {
		quoteText = string(quote)
	}
	return line[:valueStart] + quoteText + marker + quoteText
}

// keepsVerbatim reports whether the whole remainder of the line, after the
// separator, is exactly an environment reference or an earlier marker,
// optionally in one matching pair of quotes, with nothing after it. Anything
// else, a trailing comment included, is redacted from the separator on.
func keepsVerbatim(value string) bool {
	inner := value
	if n := len(value); n >= 2 && (value[0] == '"' || value[0] == '\'') && value[n-1] == value[0] {
		inner = value[1 : n-1]
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

// redactDetectorMatch redacts the leftmost detector match on a line (arbiter
// ruling B1, 2026-10-24). A key match applies the
// listed-key rule at the detector's key: everything from the separator to the
// end of the line is replaced, so a Bearer scheme word, text after a comma, or
// a trailing comment cannot survive, and the continuation state opens as for a
// listed key. A value-shape match (an AWS key, a Stripe key, a Slack token) is
// replaced from its start to the end of the line. One marker per line, so a
// second pass finds only markers and returns the line unchanged.
func (s *markerState) redactDetectorMatch(line string, spans []detect.Span, rawKind string) string {
	if len(spans) == 0 {
		return line
	}
	// The leftmost match decides. A key span whose value is already a marker is
	// not skipped: redactListedKey keeps it only when nothing follows the marker
	// (keepsVerbatim on the whole remainder), so `key=REDACTED_x tail` loses its
	// tail and a marker alone is a fixed point.
	span := spans[0]
	kind := rawKind
	if kind == "" {
		kind = defaultMarkerKind
	}
	if span.IsKey {
		return s.redactListedKey(line, line[span.KeyStart:span.KeyEnd], span.KeyEnd, span.SepEnd, kind, true)
	}
	s.replaced++
	return line[:span.Start] + markerPrefix + kind
}

func leadingWhitespace(line string) string {
	return line[:len(line)-len(strings.TrimLeft(line, " \t"))]
}
