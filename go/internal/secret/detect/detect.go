// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package detect

import (
	"regexp"
	"slices"
	"strings"
)

// The detector pattern is written once, as parts, so the SQL form and the RE2
// forms below cannot drift. The parts keep the POSIX bracket classes that
// Postgres ARE and Go RE2 both accept. Migration 234 (#8833) extends migration
// 138 with npmAuthIdent, JSON-quoted keys, URL userinfo, and HCL block openers.
const (
	// keyPrefixPattern matches a credential-named key, an optional closing
	// quote (a JSON-quoted key), its separator, and an optional opening quote.
	keyPrefixPattern = `(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization|npm[_-]?auth[_-]?ident)['"]?[[:space:]]*[:=][[:space:]]*['"]?`
	// valuePattern matches the literal a credential key assigns; a shorter
	// value is not a finding.
	valuePattern = `[A-Za-z0-9_./+=:@!#$%^-]{6,}`
	// shapePattern matches credentials that need no key: an AWS access key id,
	// a live Stripe key, a Slack token, and a PEM private-key header.
	shapePattern = `AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----`
	// userinfoPattern matches a user:password URL userinfo, with a scheme or
	// protocol-relative (//user:pass@host). A user alone is not matched.
	userinfoPattern = `//[^/?#@[:space:]:]+:[^/?#@[:space:]]+@`
	// blockOpenerPattern matches an HCL block or map opener whose key names a
	// secret (app_secrets = {), with the brace ending the line. ":" openers are
	// left out: JSON API schemas are full of "nextPageToken": {.
	blockOpenerPattern = `(password|passwd|pwd|api[_-]?key|apikey|token|secret|credential)s?[[:space:]]*=[[:space:]]*[{][[:space:]]*$`
)

// SQLPattern is the case-insensitive Postgres ARE pattern migration 234 applies
// twice in eshu_secret_line_findings (file prefilter and line filter), each
// time OR'd with a MarkerPrefix substring test. The query package's
// hardcodedSecretSQLPattern is this constant.
const SQLPattern = keyPrefixPattern + valuePattern + `|` + shapePattern + `|` + userinfoPattern + `|` + blockOpenerPattern

// MarkerPrefix opens every REDACTED_<finding_kind> marker the content-store
// redactors write. Migration 234 gates a line on a case-sensitive substring
// test for it, OR'd with SQLPattern, instead of a regex alternative: the
// case-insensitive alternative inside the ARE gate cost 66% more derivation
// time than the substring test (#8833 measurement).
const MarkerPrefix = "REDACTED_"

// SpaceClass is the whitespace class the Go-only regular expressions use where
// SQLPattern says [[:space:]]. Postgres ARE [[:space:]] on a UTF8 database
// matches U+2003 and U+3000 (measured on 18.6), which RE2's ASCII-only
// [[:space:]] does not. The class keeps the POSIX set (RE2's [[:space:]] has \v;
// Go's \s does not) and adds every Unicode Zs space (U+00A0 included, which
// Postgres does not match), NEL, and the line and paragraph separators. A
// superset is safe here because it can only redact more than the migration
// flags. It is substituted at compile time only: SQLPattern and the arm sources
// keep the migration's bytes.
const SpaceClass = `[[:space:]\p{Zs}\x{85}\x{2028}\x{2029}]`

const postgresSpaceClass = `[[:space:]]`

// goPattern rewrites a Postgres pattern part for RE2: the case-insensitive flag
// and the superset space class.
func goPattern(postgres string) string {
	return "(?i)" + strings.ReplaceAll(postgres, postgresSpaceClass, SpaceClass)
}

// kindArm is one WHEN arm of the migration 138 CASE.
type kindArm struct {
	regex *regexp.Regexp
	kind  string
}

// kindArmSources lists the CASE arms in migration 234 order. The marker arm
// comes first, one arm per kind, so a redacted line keeps the kind its raw line
// had; REDACTED_value matches none of them. The migration 138 arms follow
// unchanged, so every line 138 classified keeps its kind. The extras' arms come
// last.
var kindArmSources = []struct{ Regex, Kind string }{
	{MarkerPrefix + `aws_access_key`, "aws_access_key"},
	{MarkerPrefix + `private_key`, "private_key"},
	{MarkerPrefix + `slack_token`, "slack_token"},
	{MarkerPrefix + `api_token`, "api_token"},
	{MarkerPrefix + `password_literal`, "password_literal"},
	{MarkerPrefix + `secret_literal`, "secret_literal"},
	{`AKIA[0-9A-Z]{16}`, "aws_access_key"},
	{`-----BEGIN [A-Z ]*PRIVATE KEY-----`, "private_key"},
	{`xox[baprs]-[A-Za-z0-9-]{10,}`, "slack_token"},
	{`(api[_-]?key|apikey|token)[[:space:]]*[:=]`, "api_token"},
	{`(password|passwd|pwd)[[:space:]]*[:=]`, "password_literal"},
	{`(secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=]`, "secret_literal"},
	{`(api[_-]?key|apikey|token)s?['"]?[[:space:]]*[:=]`, "api_token"},
	{`(password|passwd|pwd)s?['"]?[[:space:]]*[:=]`, "password_literal"},
	{`(secret|client[_-]?secret|private[_-]?key|authorization|npm[_-]?auth[_-]?ident|credential)s?['"]?[[:space:]]*[:=]`, "secret_literal"},
	{userinfoPattern, "password_literal"},
}

var (
	kindArms = func() []kindArm {
		arms := make([]kindArm, 0, len(kindArmSources))
		for _, source := range kindArmSources {
			arms = append(arms, kindArm{regex: regexp.MustCompile(goPattern(source.Regex)), kind: source.Kind})
		}
		return arms
	}()
	// spanPattern captures the value of a key match in group 2 so the redactor
	// can replace the value alone; a shape, userinfo, or block-opener match has
	// no group. A marker is not a credential and is not a span.
	spanPattern = regexp.MustCompile(goPattern(keyPrefixPattern + "(" + valuePattern + ")|" + shapePattern + "|" + userinfoPattern + "|" + blockOpenerPattern))
)

// ArmSources returns the migration 138 CASE arms as (regex, kind) pairs in
// order, so a test can compare them with the migration text.
func ArmSources() []struct{ Regex, Kind string } {
	out := make([]struct{ Regex, Kind string }, len(kindArmSources))
	copy(out, kindArmSources)
	return out
}

// Kinds returns the six finding kinds in CASE order of first appearance.
func Kinds() []string {
	kinds := make([]string, 0, 6)
	for _, arm := range kindArms {
		if !slices.Contains(kinds, arm.kind) {
			kinds = append(kinds, arm.kind)
		}
	}
	return kinds
}

// Finding returns the finding kind migration 234 stores for one line, or ""
// when the line is no finding: the line gate (a MarkerPrefix substring or an
// SQLPattern match) must pass and the CASE must name a kind.
func Finding(line string) string {
	if !strings.Contains(line, MarkerPrefix) && len(Spans(line)) == 0 {
		return ""
	}
	return Classify(line)
}

// Classify returns the finding kind migration 234 gives one line, or "" when no
// arm matches. It is the CASE only: the line gate is Finding.
func Classify(line string) string {
	for _, arm := range kindArms {
		if arm.regex.MatchString(line) {
			return arm.kind
		}
	}
	return ""
}

// Span is a byte range of one line that holds a credential. For a key match it
// is the value and IsKey is true; KeyStart and KeyEnd bound the key word, and
// SepEnd is the end of the separator and the white space after it, before an
// opening quote. For a value-shape match it is the whole shape.
type Span struct {
	Start, End int
	IsKey      bool
	KeyStart   int
	KeyEnd     int
	SepEnd     int
}

// Spans returns the credential spans SQLPattern matches in one line: the
// value (with the key geometry) for a key match, the whole shape for a
// value-shape match.
// The Go and Postgres engines differ in both directions: Go (?i) folds U+017F
// and U+212A where Postgres in a C collation does not, and Postgres
// [[:space:]] matches U+2003 and U+3000 where RE2's does not. SpaceClass makes
// the second direction a superset; the first already is one. The engine-parity
// corpus test in #8833 must include Unicode spaces and the fold-sensitive runes.
func Spans(line string) []Span {
	if !mayMatch(line) {
		return nil
	}
	matches := spanPattern.FindAllStringSubmatchIndex(line, -1)
	spans := make([]Span, 0, len(matches))
	for _, match := range matches {
		// Group 1 is the key word inside keyPrefixPattern; group 2 is the value.
		if match[4] >= 0 {
			sepEnd := match[4]
			if sepEnd > match[0] && (line[sepEnd-1] == '"' || line[sepEnd-1] == '\'') {
				sepEnd--
			}
			spans = append(spans, Span{
				Start: match[4], End: match[5], IsKey: true,
				KeyStart: match[2], KeyEnd: match[3], SepEnd: sepEnd,
			})
			continue
		}
		start := match[0]
		if strings.HasPrefix(line[start:], "//") {
			// A userinfo span starts at its URL scheme, so a redactor that
			// replaces from the span start leaves no "https:" stub behind.
			for start > 0 && line[start-1] == ':' {
				start--
				for start > 0 && isSchemeByte(line[start-1]) {
					start--
				}
				break
			}
		}
		spans = append(spans, Span{Start: start, End: match[1]})
	}
	return spans
}

func isSchemeByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '+' || b == '.' || b == '-'
}

// matchWords are lowercase substrings every SQLPattern match contains: the
// credential key words and the value-shape anchors. A line holding none of
// them cannot match, so Spans skips the regular expression, which costs about
// 80 ns per byte, for the common clean line.
var matchWords = []string{
	"pass", "pwd", "key", "token", "secret", "authorization", "akia", "sk_live_", "xox", "-----begin",
	"ident", "credential", "//",
}

func mayMatch(line string) bool {
	// A non-ASCII byte skips nothing: Go (?i) folds U+017F to s and U+212A to k,
	// which strings.ToLower does not, so the lowercase test below is only a
	// superset for ASCII lines.
	for i := 0; i < len(line); i++ {
		if line[i] >= 0x80 {
			return true
		}
	}
	lower := strings.ToLower(line)
	for _, word := range matchWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}
