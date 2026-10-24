// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/secret/detect"
)

// skeletonValueMarker replaces a value, or a whole line, that the allowlist does
// not keep and the detector does not flag as a finding. The detector never
// matches it (#8833's marker arm excludes it on purpose), so it adds no finding.
const skeletonValueMarker = markerPrefix + "value"

var (
	// skeletonKeyValue splits a line into indent, key, separator, and the rest.
	// A key is an HCL identifier or a double-quoted name of letters, digits,
	// "_", ".", and "-" (a region, a host name); the separator is "=" (HCL) or
	// ":" (JSON and HCL object elements). A quoted key holding anything else (a
	// URL with userinfo, a password with symbols, a CIDR, an ARN) is free text
	// that could be a credential, so the line is not placed and goes whole.
	skeletonKeyValue = regexp.MustCompile(`^([ \t]*)([A-Za-z_][A-Za-z0-9_-]*|"[A-Za-z0-9_][A-Za-z0-9_.-]*")([ \t]*[=:][ \t]*)(.*)$`)
	// skeletonHeredoc is a heredoc opener value; group 1 is its delimiter.
	skeletonHeredoc = regexp.MustCompile(`^<<-?([A-Za-z_][A-Za-z0-9_]*)$`)
	// skeletonHeredocTail is a heredoc opened at the end of a line, after other
	// text (keys = [<<EOF, jsonencode(<<EOT); group 1 is its delimiter.
	skeletonHeredocTail = regexp.MustCompile(`<<-?([A-Za-z_][A-Za-z0-9_]*)$`)
	// The allowlist: shapes that cannot carry a credential.
	skeletonLiteral   = regexp.MustCompile(`^(true|false|null)$`)
	skeletonNumber    = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)
	skeletonReference = regexp.MustCompile(`^(var|local|module|data)(\.[A-Za-z_][A-Za-z0-9_-]*|\[[0-9]+\])+$`)
	// skeletonVersion is a quoted version or constraint list. A prerelease is
	// limited to known words, so free text cannot ride in a suffix.
	skeletonVersion = regexp.MustCompile(`^"` + versionConstraint + `(,` + versionConstraint + `)*"$`)
	// skeletonMarkerValue is a value an earlier pass already replaced, which
	// keeps the redactor a fixed point.
	skeletonMarkerValue = regexp.MustCompile(`^` + markerPrefix + `(value|` + strings.Join(detect.Kinds(), "|") + `)$`)
)

const versionConstraint = ` *(~>|>=|<=|!=|=|>|<)? *v?[0-9]+(\.[0-9]+){1,3}(-(alpha|beta|rc|pre|dev|snapshot)(\.?[0-9]+)?)? *`

// redactSkeleton stores the allowlist skeleton of an open-schema body such as
// *.tfvars or *.tfvars.json (#8813 record, "Allowlist-skeleton mode"). It is
// line-wise, so the line count and every line number are preserved:
//
//   - structural lines ({, }, [, ], key = {, key = [, heredoc opener and
//     closer) and empty lines are kept;
//   - in a key = value line whose raw text is a detector finding, the value
//     becomes REDACTED_<finding_kind>, even for a reference;
//   - a value that is a boolean, null, a number, a var./local./module./data.
//     reference, or a quoted version string is kept;
//   - any other value becomes REDACTED_value;
//   - a line the classifier cannot place as key = value (a comment, a bare list
//     element, a heredoc body line, a PEM block line, an unterminated string, a
//     credential inside the key, a value that starts with "=" or ":" such as
//     the base64 padding of a bare TOKEN==) becomes REDACTED_<finding_kind>
//     when the raw line is a finding and REDACTED_value otherwise, so it keeps
//     its finding;
//   - a heredoc opened at the end of any line (key = <<EOF, keys = [<<EOF) and
//     a block comment opened anywhere outside a string or line comment redact
//     every line to their close.
//
// A finding is what migration 138 reports for the raw line: the detector line
// gate matches and the CASE names a kind. A gate match with no kind (an
// sk_live_ key alone) is no finding, so it becomes REDACTED_value and no marker
// adds a finding the raw body did not have; it still blocks the allowlist, so
// var.sk_live_... is not kept. It errors, never returns a partial body, for a
// body that is not text or is over MaxGovernedBodyBytes.
func redactSkeleton(body string) (string, int, error) {
	if len(body) > MaxGovernedBodyBytes {
		return "", 0, errTooLarge
	}
	if !utf8.ValidString(body) {
		return "", 0, errInvalidUTF8
	}
	if strings.IndexByte(body, 0) >= 0 {
		return "", 0, errNotText
	}
	lines := strings.Split(body, "\n")
	var state skeletonState
	for i, line := range lines {
		carriage := ""
		if strings.HasSuffix(line, "\r") {
			carriage = "\r"
			line = strings.TrimSuffix(line, "\r")
		}
		lines[i] = state.line(line) + carriage
	}
	return strings.Join(lines, "\n"), state.replaced, nil
}

// skeletonState carries what crosses line boundaries: an open PEM block, an
// open heredoc, and an open block comment.
type skeletonState struct {
	inPEM   bool
	heredoc string
	// heredocCloserGoes is set for a heredoc whose opener line was redacted
	// (keys = [<<EOF). Its closer is redacted too, so a second pass, which sees
	// no opener, gives the same body and the redactor stays a fixed point.
	heredocCloserGoes bool
	inBlockComment    bool
	replaced          int
}

func (s *skeletonState) line(line string) string {
	out := s.classify(line)
	// A BEGIN header with no END after it opens a PEM block wherever it sits (a
	// comment, a heredoc, an unterminated value), and every following line is
	// key material until the footer.
	if !s.inPEM && strings.Contains(line, "-----") && pemLeftOpen(line) {
		s.inPEM = true
	}
	return out
}

func (s *skeletonState) classify(line string) string {
	trimmed := strings.TrimSpace(line)
	if s.inPEM {
		if pemEnd.MatchString(line) {
			s.inPEM = false
		}
		return s.wholeLine(line)
	}
	if s.heredoc != "" {
		if trimmed == s.heredoc {
			s.heredoc = ""
			if s.heredocCloserGoes {
				s.heredocCloserGoes = false
				return s.wholeLine(line)
			}
			return line
		}
		return s.wholeLine(line)
	}
	if s.inBlockComment {
		if strings.Contains(line, "*/") {
			s.inBlockComment = false
		}
		return s.wholeLine(line)
	}
	if trimmed == "" || skeletonMarkerValue.MatchString(trimmed) || isSkeletonStructural(trimmed) {
		return line
	}
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
		return s.wholeLine(line)
	}
	if strings.HasPrefix(trimmed, "/*") {
		// A comment that starts the line runs to its */.
		s.inBlockComment = !strings.Contains(trimmed[2:], "*/")
		return s.wholeLine(line)
	}
	out := s.keyValue(line)
	// A heredoc or block comment opened after other text still swallows the
	// lines below it, so its body is classified whole, never as key = value.
	if s.heredoc == "" {
		if tail := skeletonHeredocTail.FindStringSubmatch(trimmed); tail != nil {
			s.heredoc, s.heredocCloserGoes = tail[1], true
		}
	}
	if s.heredoc == "" && blockCommentLeftOpen(line) {
		s.inBlockComment = true
	}
	return out
}

// blockCommentLeftOpen reports whether line opens a /* outside a double-quoted
// string and a line comment, with no */ after it on the same line.
func blockCommentLeftOpen(line string) bool {
	inString := false
	for i := 0; i < len(line); i++ {
		switch c := line[i]; {
		case inString && c == '\\':
			i++
		case c == '"':
			inString = !inString
		case inString:
		case c == '#', c == '/' && strings.HasPrefix(line[i:], "//"):
			return false
		case c == '/' && strings.HasPrefix(line[i:], "/*"):
			end := strings.Index(line[i+2:], "*/")
			if end < 0 {
				return true
			}
			i += 2 + end + 1
		}
	}
	return false
}

// keyValue handles a line that may be key = value; anything it cannot place
// becomes a whole-line marker.
func (s *skeletonState) keyValue(line string) string {
	parts := skeletonKeyValue.FindStringSubmatch(line)
	if parts == nil {
		return s.wholeLine(line)
	}
	indent, key, separator, rest := parts[1], parts[2], parts[3], strings.TrimRight(parts[4], " \t")
	valueStart := len(indent) + len(key) + len(separator)
	// A detector match that starts inside the key or separator is a credential
	// in the key itself, so the key cannot be kept.
	for _, span := range detect.Spans(line) {
		if span.Start < valueStart {
			return s.wholeLine(line)
		}
	}
	value, comma := rest, ""
	if strings.HasSuffix(value, ",") {
		value, comma = strings.TrimRight(strings.TrimSuffix(value, ","), " \t"), ","
	}
	// No value, or one that starts with a separator (TOKEN==, a::b), is no
	// assignment the classifier can place.
	if value == "" || value[0] == '=' || value[0] == ':' {
		return s.wholeLine(line)
	}
	if value == "{" || value == "[" || value == "{}" || value == "[]" || skeletonMarkerValue.MatchString(value) {
		return line
	}
	if heredoc := skeletonHeredoc.FindStringSubmatch(value); heredoc != nil {
		s.heredoc = heredoc[1]
		return line
	}
	if strings.HasPrefix(value, `"`) && quoteEnd(value[1:], '"') < 0 {
		// An unterminated string fails closed: the whole line goes.
		return s.wholeLine(line)
	}
	head := indent + key + separator
	if kind := findingKind(line); kind != "" {
		s.replaced++
		return head + markerPrefix + kind + comma
	}
	// A detector match with no kind is no finding, but the value holds a
	// token-shaped string, so no allowlist shape (var.sk_live_...) keeps it.
	if len(detect.Spans(line)) == 0 && skeletonAllowed(value) {
		return line
	}
	s.replaced++
	return head + skeletonValueMarker + comma
}

// wholeLine replaces a line it cannot keep, preserving its indent.
func (s *skeletonState) wholeLine(line string) string {
	// An exact marker from an earlier pass stays, inside a heredoc or PEM block
	// too, so the redactor is a fixed point.
	if trimmed := strings.TrimSpace(line); trimmed == "" || skeletonMarkerValue.MatchString(trimmed) {
		return line
	}
	s.replaced++
	kind := findingKind(line)
	if kind == "" {
		return leadingWhitespace(line) + skeletonValueMarker
	}
	return leadingWhitespace(line) + markerPrefix + kind
}

// findingKind returns the kind migration 138 reports for one raw line, or ""
// when the line is no finding: the line gate must match and the CASE must name
// a kind.
func findingKind(line string) string {
	if len(detect.Spans(line)) == 0 {
		return ""
	}
	return detect.Classify(line)
}

func skeletonAllowed(value string) bool {
	return skeletonLiteral.MatchString(value) || skeletonNumber.MatchString(value) ||
		skeletonReference.MatchString(value) || skeletonVersion.MatchString(value)
}

func isSkeletonStructural(trimmed string) bool {
	switch trimmed {
	case "{", "}", "[", "]", "},", "],", "{}", "[]", "{},", "[],":
		return true
	}
	return false
}
