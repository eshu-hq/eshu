// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"regexp"
	"slices"
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
	// skeletonHeredoc is a heredoc opener value; a key = <<ID line is kept.
	skeletonHeredoc = regexp.MustCompile(`^<<-?([A-Za-z_][A-Za-z0-9_]*)$`)
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
//   - the text of a heredoc, a quoted template that holds an interpolation, and
//     a block comment redacts every line to its close, however they nest: a
//     heredoc opens at the end of a line that ends in code (key = <<EOF,
//     keys = [<<EOF, or inside a ${ }), and a <<ID in a # or // comment or a
//     string is text. One lexical stack (scanState, after hclsyntax's lexer)
//     decides all of it;
//   - lexing failure is sticky: nesting deeper than maxScanDepth, or a line
//     that ends inside a quoted template, redacts every later line whole. The
//     body is still returned, with its line count and its findings.
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

// skeletonState carries what crosses line boundaries: an open PEM block and the
// lexical state of the template, heredoc, and comment stack (scanState).
type skeletonState struct {
	inPEM bool
	scan  scanState
	// keptOpener is set by keyValue for a key = <<ID line it keeps, so the
	// scan marks that heredoc's closer to be kept too.
	keptOpener bool
	replaced   int
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

// classify stores one line. A line that begins inside a template, a comment, a
// PEM block, or a failed scan is redacted whole; the scan still reads it so the
// state after it is right.
func (s *skeletonState) classify(line string) string {
	trimmed := strings.TrimSpace(line)
	inside, wasPEM := s.scan.inside(), s.inPEM
	var out string
	switch {
	case wasPEM:
		if pemEnd.MatchString(line) {
			s.inPEM = false
		}
		out = s.wholeLine(line)
	case !inside:
		out = s.place(line, trimmed)
	}
	closerKept := s.scan.line(line, s.keptOpener)
	s.keptOpener = false
	if inside && !wasPEM {
		if closerKept {
			return line
		}
		out = s.wholeLine(line)
	}
	return out
}

// place stores one line that begins in code.
func (s *skeletonState) place(line, trimmed string) string {
	switch {
	case trimmed == "" || skeletonMarkerValue.MatchString(trimmed) || isSkeletonStructural(trimmed):
		return line
	case strings.HasPrefix(trimmed, "#"), strings.HasPrefix(trimmed, "//"):
		return s.lineComment(line, trimmed)
	case strings.HasPrefix(trimmed, "/*"):
		return s.wholeLine(line)
	}
	return s.keyValue(line)
}

// lineComment stores a # or // comment line. It keeps only the github.com URLs
// the comment names (the raw Terraform evidence reads them there); its text
// goes. A finding comes first.
func (s *skeletonState) lineComment(line, trimmed string) string {
	if findingKind(line) == "" {
		if urls := githubURLsIn(trimmed); len(urls) > 0 {
			opener := "#"
			if strings.HasPrefix(trimmed, "//") {
				opener = "//"
			}
			kept := opener + " " + strings.Join(urls, " ")
			if kept != trimmed {
				s.replaced++
			}
			return leadingWhitespace(line) + kept
		}
	}
	return s.wholeLine(line)
}

// keyValue handles a line that may be key = value; anything it cannot place
// becomes a whole-line marker. A stored line must never be a finding its raw
// line is not: api_token = "abc" is no finding (the value is under the
// detector's six-character floor), but api_token = REDACTED_value would be,
// so such a line becomes the whole-line REDACTED_value instead.
func (s *skeletonState) keyValue(line string) string {
	replaced := s.replaced
	keptOpener := s.keptOpener
	out := s.placeKeyValue(line)
	if out != line && findingKind(out) != "" && findingKind(line) == "" {
		s.replaced = replaced
		s.keptOpener = keptOpener
		return s.wholeLine(line)
	}
	return out
}

// placeKeyValue is keyValue before the no-new-finding check.
func (s *skeletonState) placeKeyValue(line string) string {
	parts := skeletonKeyValue.FindStringSubmatch(line)
	if parts == nil {
		return s.bareElement(line)
	}
	indent, key, separator, rest := parts[1], parts[2], parts[3], strings.TrimRight(parts[4], " \t")
	valueStart := len(indent) + len(key) + len(separator)
	// A detector match that starts inside the key or separator is a credential
	// in the key itself, so the key cannot be kept. A block-opener match
	// (app_secrets = {) reads the key name by design and holds no value.
	for _, span := range detect.Spans(line) {
		if span.Start < valueStart && !strings.HasSuffix(strings.TrimRight(line[span.Start:span.End], " \t"), "{") {
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
	if skeletonHeredoc.MatchString(value) && !strings.HasSuffix(line, " ") && !strings.HasSuffix(line, "\t") {
		s.keptOpener = true
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
	// token-shaped string, so no allowlist or evidence shape (var.sk_live_...)
	// keeps it and no text from the line survives.
	if len(detect.Spans(line)) > 0 {
		s.replaced++
		return head + skeletonValueMarker + comma
	}
	if kept, ok := skeletonEvidence(key, value); ok {
		if kept == value {
			return line
		}
		s.replaced++
		return head + kept + comma
	}
	if skeletonAllowed(value) {
		return line
	}
	return s.valueWithText(line, head, key, value, comma)
}

// valueWithText stores a value the allowlist does not keep whole: a value with
// a trailing comment, or a string with free text. The value part is kept when
// it is an allowlist or evidence shape (or an earlier marker) and becomes
// REDACTED_value otherwise. The github.com URLs the comment, or the replaced
// value, names survive as a "# https://github.com/<path>" comment; all other
// text goes.
func (s *skeletonState) valueWithText(line, head, key, value, comma string) string {
	valuePart, comment, hasComment := splitTrailingComment(value)
	if !hasComment {
		valuePart = value
	}
	stored := skeletonValueMarker
	if kept, ok := skeletonEvidence(key, valuePart); ok {
		stored = kept
	} else if skeletonAllowed(valuePart) || skeletonMarkerValue.MatchString(valuePart) {
		stored = valuePart
	}
	var urls []string
	if stored == skeletonValueMarker {
		urls = githubURLsIn(valuePart)
	}
	for _, url := range githubURLsIn(comment) {
		if !slices.Contains(urls, url) {
			urls = append(urls, url)
		}
	}
	out := head + stored
	if len(urls) > 0 {
		out += " # " + strings.Join(urls, " ")
	}
	out += comma
	if out != line {
		s.replaced++
	}
	return out
}

// bareElement keeps a list element that is exactly a github.com URL or an OIDC
// subject (skeletonEvidenceValue), unless the line is a finding; every other
// line it cannot place becomes a whole-line marker.
func (s *skeletonState) bareElement(line string) string {
	trimmed := strings.TrimSpace(line)
	value, comma := trimmed, ""
	if strings.HasSuffix(value, ",") {
		value, comma = strings.TrimRight(strings.TrimSuffix(value, ","), " \t"), ","
	}
	// Any detector match, with or without a kind, takes the line.
	if len(detect.Spans(line)) == 0 {
		if kept, ok := skeletonEvidenceValue(value); ok {
			if kept == value {
				return line
			}
			s.replaced++
			return leadingWhitespace(line) + kept + comma
		}
	}
	return s.wholeLine(line)
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

// findingKind returns the kind the detector migration stores for one raw line,
// or "" when the line is no finding (detect.Finding).
func findingKind(line string) string {
	return detect.Finding(line)
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
